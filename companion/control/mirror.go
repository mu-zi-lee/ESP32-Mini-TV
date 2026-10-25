package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"image"
	imageDraw "image/draw"
	"image/jpeg"
	"io"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"golang.org/x/image/draw"
)

const (
	mirrorWidth  = 160
	mirrorHeight = 128
	mirrorPort   = 8765
	// Stay below the usual 1500-byte Ethernet MTU after the small protocol
	// header. This cuts the number of Wi-Fi datagrams by roughly 15-20% versus
	// the old 1186-byte payload without relying on IP fragmentation.
	chunkPayload = 1186
	// Tile packets carry a small rectangle descriptor after the normal frame
	// header, so keep their payload just below the same Ethernet-safe budget.
	tileChunkPayload = 1182
	// The panel is small, but text benefits from a little extra JPEG quality.
	// The size guard below falls back to a lower quality for unusually noisy
	// frames so one frame can never monopolize the UDP receive buffer.
	jpegQuality    = 88
	minJPEGQuality = 82
	// TJpg_Decoder and the synchronous ST7735 flush are the limiting stages on
	// the ESP32. Keep a small, shared pacing interval for every capture source;
	// without this guard a live FFmpeg pipe can flood UDP while the panel is
	// still drawing the previous frame.
	// Keep the original high-refresh mirror profile. Resource reductions are
	// handled by telemetry deduplication and request pacing, not by dropping
	// frames from the live mirror stream.
	mirrorInterval          = 30 * time.Millisecond
	mirrorHeartbeatInterval = 900 * time.Millisecond
	mirrorKeyframeInterval  = 3 * time.Second
	mirrorTileSize          = 32
	mirrorTileMaxChunks     = 16
	// TJpg_Decoder runs synchronously on the ESP32. Leave a small gap between
	// tiles so a 20-tile keyframe does not overflow the UDP receive queue.
	mirrorTileInterval = 8 * time.Millisecond
	// Status packets use the same UDP socket as frames, but a distinct magic
	// byte so the ESP32 can update its overlay without touching the framebuffer.
	agentStatusPacketMode = 3
)

// Raw RGB565 avoids JPEG decode cost on the ESP32, but it sends a fixed 41KB
// frame (roughly 35 UDP datagrams) and can therefore be slower on a busy
// Wi-Fi link than a small JPEG. Keep JPEG as the default; enable raw mode only
// when the extra sharpness is worth the larger transfer:
// MINI_TV_RAW_FULL_FRAME=1.
func useRawFullFrame() bool {
	value := strings.ToLower(strings.TrimSpace(os.Getenv("MINI_TV_RAW_FULL_FRAME")))
	return value == "1" || value == "true" || value == "yes"
}

type mirrorTilePayload struct {
	rect image.Rectangle
	data []byte
}

type mirrorDeltaState struct {
	lastHash     uint32
	lastCanvas   *image.RGBA
	tileHashes   []uint32
	lastKeyframe time.Time
}

// screenCaptureStream keeps one AVFoundation capture process alive for the
// whole mirror session. Starting screencapture once per frame costs more time
// than the ESP32 needs to receive and draw the image, so a persistent MJPEG
// pipe gives us lower latency without changing the wire protocol.
type screenCaptureStream struct {
	cmd    *exec.Cmd
	stdout io.ReadCloser
	reader *bufio.Reader
	buffer []byte
}

func startScreenCapture(ctx context.Context) (*screenCaptureStream, error) {
	index := strings.TrimSpace(os.Getenv("MINI_TV_SCREEN_INDEX"))
	args := []string{"-hide_banner", "-loglevel", "error"}
	switch runtime.GOOS {
	case "darwin":
		if index == "" {
			// AVFoundation device order varies with connected cameras and displays.
			// Override this with MINI_TV_SCREEN_INDEX when the second display is used.
			index = "3"
		}
		args = append(args, "-f", "avfoundation", "-framerate", "40", "-pixel_format", "uyvy422", "-i", index+":none")
	case "linux":
		display := strings.TrimSpace(os.Getenv("DISPLAY"))
		if display == "" {
			return nil, fmt.Errorf("找不到 Linux 图形会话，请设置 DISPLAY")
		}
		input := display
		if index != "" {
			input += "+" + index
		}
		args = append(args, "-f", "x11grab", "-framerate", "30", "-i", input)
	case "windows":
		if index == "" {
			index = "desktop"
		}
		args = append(args, "-f", "gdigrab", "-framerate", "30", "-i", index)
	default:
		return nil, fmt.Errorf("当前系统不支持屏幕采集：%s", runtime.GOOS)
	}
	args = append(args,
		"-vf", "scale=160:128:force_original_aspect_ratio=increase,crop=160:128",
		"-f", "image2pipe", "-vcodec", "mjpeg", "-q:v", "5", "pipe:1")
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		_ = stdout.Close()
		return nil, err
	}
	return &screenCaptureStream{cmd: cmd, stdout: stdout, reader: bufio.NewReaderSize(stdout, 64*1024)}, nil
}

func (stream *screenCaptureStream) close() {
	if stream == nil {
		return
	}
	_ = stream.stdout.Close()
	_ = stream.cmd.Wait()
}

func (stream *screenCaptureStream) nextFrame(ctx context.Context) ([]byte, error) {
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		chunk := make([]byte, 32*1024)
		n, err := stream.reader.Read(chunk)
		if n > 0 {
			stream.buffer = append(stream.buffer, chunk[:n]...)
			var latest []byte
			for {
				start := bytes.Index(stream.buffer, []byte{0xff, 0xd8})
				if start < 0 {
					if len(stream.buffer) > 1 {
						stream.buffer = stream.buffer[len(stream.buffer)-1:]
					}
					break
				}
				if start > 0 {
					stream.buffer = stream.buffer[start:]
				}
				end := bytes.Index(stream.buffer[2:], []byte{0xff, 0xd9})
				if end < 0 {
					break
				}
				end += 4
				latest = append(latest[:0], stream.buffer[:end]...)
				stream.buffer = stream.buffer[end:]
			}
			// If capture produced several frames while the network was busy,
			// return only the newest complete image. Dropping stale frames keeps
			// the display close to real time instead of showing old desktop state.
			if len(latest) > 0 {
				return latest, nil
			}
		}
		if err != nil {
			if err == io.EOF {
				return nil, fmt.Errorf("屏幕采集流已结束")
			}
			return nil, err
		}
	}
}

func captureFrame(ctx context.Context, path string) ([]byte, error) {
	// Remove the previous capture before asking screencapture to write the
	// next one; this works consistently across macOS versions and avoids a
	// stale frame if the command exits early.
	_ = os.Remove(path)
	cmd := exec.CommandContext(ctx, "screencapture", "-x", "-t", "jpg", "-m", path)
	if output, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("屏幕录制失败：%s", string(output))
	}
	file, err := os.Open(filepath.Clean(path))
	if err != nil {
		return nil, err
	}
	defer file.Close()
	source, err := jpeg.Decode(file)
	if err != nil {
		return nil, err
	}
	canvas := image.NewRGBA(image.Rect(0, 0, mirrorWidth, mirrorHeight))
	srcBounds := source.Bounds()
	srcRatio := float64(srcBounds.Dx()) / float64(srcBounds.Dy())
	dstRatio := float64(mirrorWidth) / float64(mirrorHeight)
	var crop image.Rectangle
	if srcRatio > dstRatio {
		width := int(float64(srcBounds.Dy()) * dstRatio)
		crop = image.Rect(srcBounds.Min.X+(srcBounds.Dx()-width)/2, srcBounds.Min.Y, srcBounds.Min.X+(srcBounds.Dx()+width)/2, srcBounds.Max.Y)
	} else {
		height := int(float64(srcBounds.Dx()) / dstRatio)
		crop = image.Rect(srcBounds.Min.X, srcBounds.Min.Y+(srcBounds.Dy()-height)/2, srcBounds.Max.X, srcBounds.Min.Y+(srcBounds.Dy()+height)/2)
	}
	// Bilinear keeps text readable while using much less CPU than Catmull-Rom;
	// the smaller encode/decode budget is more valuable than sub-pixel sharpness
	// on a 1.8-inch 160x128 panel.
	draw.BiLinear.Scale(canvas, canvas.Bounds(), source, crop, draw.Src, nil)
	return encodeMirrorJPEG(canvas)
}

// encodeMirrorJPEG keeps all mirror sources on the same quality/size budget.
// A few larger quality steps are enough for a 160x128 panel and avoid doing
// many almost-identical encodes when a noisy desktop frame exceeds the UDP
// buffer budget.
func encodeMirrorJPEG(canvas image.Image) ([]byte, error) {
	var encoded bytes.Buffer
	encoded.Grow(24 * 1024)
	for quality := jpegQuality; ; {
		encoded.Reset()
		if err := jpeg.Encode(&encoded, canvas, &jpeg.Options{Quality: quality}); err != nil {
			return nil, err
		}
		if encoded.Len() <= 48*1024 || quality == minJPEGQuality {
			break
		}
		quality -= 4
		if quality < minJPEGQuality {
			quality = minJPEGQuality
		}
	}
	if encoded.Len() > 48*1024 {
		return nil, fmt.Errorf("镜像帧过大（%d 字节）", encoded.Len())
	}
	return append([]byte(nil), encoded.Bytes()...), nil
}

func normalizeMirrorFrame(data []byte) ([]byte, error) {
	if len(data) <= 48*1024 {
		return data, nil
	}
	canvas, err := decodeMirrorImage(data)
	if err != nil {
		return nil, err
	}
	return encodeMirrorJPEG(canvas)
}

func encodeMirrorRGB565(canvas *image.RGBA) []byte {
	raw := make([]byte, mirrorWidth*mirrorHeight*2)
	index := 0
	for y := 0; y < mirrorHeight; y++ {
		row := canvas.Pix[y*canvas.Stride:]
		for x := 0; x < mirrorWidth; x++ {
			pixel := row[x*4:]
			value := uint16(pixel[0]>>3)<<11 | uint16(pixel[1]>>2)<<5 | uint16(pixel[2]>>3)
			raw[index] = byte(value >> 8)
			raw[index+1] = byte(value)
			index += 2
		}
	}
	return raw
}

func mirrorFrameHash(data []byte) uint32 {
	// FNV-1a is intentionally tiny here: it is only used to decide whether a
	// newly captured JPEG is identical to the last one sent over the wire.
	hash := uint32(2166136261)
	for _, value := range data {
		hash ^= uint32(value)
		hash *= 16777619
	}
	return hash
}

func decodeMirrorImage(data []byte) (*image.RGBA, error) {
	source, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	if source.Bounds().Dx() != mirrorWidth || source.Bounds().Dy() != mirrorHeight {
		return nil, fmt.Errorf("镜像帧尺寸异常：%dx%d", source.Bounds().Dx(), source.Bounds().Dy())
	}
	canvas := image.NewRGBA(image.Rect(0, 0, mirrorWidth, mirrorHeight))
	imageDraw.Draw(canvas, canvas.Bounds(), source, source.Bounds().Min, imageDraw.Src)
	return canvas, nil
}

func mirrorTileRects() []image.Rectangle {
	rects := make([]image.Rectangle, 0, (mirrorWidth/mirrorTileSize)*(mirrorHeight/mirrorTileSize))
	for y := 0; y < mirrorHeight; y += mirrorTileSize {
		for x := 0; x < mirrorWidth; x += mirrorTileSize {
			rects = append(rects, image.Rect(x, y,
				min(x+mirrorTileSize, mirrorWidth), min(y+mirrorTileSize, mirrorHeight)))
		}
	}
	return rects
}

func mirrorTileHashes(canvas *image.RGBA, rects []image.Rectangle) []uint32 {
	hashes := make([]uint32, len(rects))
	for index, rect := range rects {
		hash := uint32(2166136261)
		for y := rect.Min.Y; y < rect.Max.Y; y++ {
			row := canvas.Pix[y*canvas.Stride+rect.Min.X*4 : y*canvas.Stride+rect.Max.X*4]
			for _, value := range row {
				hash ^= uint32(value)
				hash *= 16777619
			}
		}
		hashes[index] = hash
	}
	return hashes
}

func encodeMirrorTile(canvas *image.RGBA, rect image.Rectangle) ([]byte, error) {
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, canvas.SubImage(rect), &jpeg.Options{Quality: jpegQuality}); err != nil {
		return nil, err
	}
	if encoded.Len() == 0 || (encoded.Len()+tileChunkPayload-1)/tileChunkPayload > mirrorTileMaxChunks {
		return nil, fmt.Errorf("镜像局部区域过大：%d 字节", encoded.Len())
	}
	return append([]byte(nil), encoded.Bytes()...), nil
}

func sendMirrorTile(conn *net.UDPConn, address *net.UDPAddr, frameID, tileIndex, tileCount uint16,
	tile mirrorTilePayload, packet []byte) error {
	jpegData := tile.data
	chunks := (len(jpegData) + tileChunkPayload - 1) / tileChunkPayload
	if chunks == 0 || chunks > mirrorTileMaxChunks || tileCount == 0 || tileIndex >= tileCount || len(packet) < tileChunkPayload+22 {
		return fmt.Errorf("镜像局部分块数量异常：%d", chunks)
	}
	for index := 0; index < chunks; index++ {
		start := index * tileChunkPayload
		end := min(start+tileChunkPayload, len(jpegData))
		packet = packet[:22+end-start]
		packet[0], packet[1], packet[2] = 'M', 'T', 1
		binary.LittleEndian.PutUint16(packet[3:5], frameID)
		packet[5], packet[6] = mirrorWidth, mirrorHeight
		binary.LittleEndian.PutUint16(packet[7:9], uint16(index))
		binary.LittleEndian.PutUint16(packet[9:11], uint16(chunks))
		binary.LittleEndian.PutUint16(packet[11:13], uint16(end-start))
		packet[13] = 1 // dirty-tile packet
		packet[14], packet[15] = uint8(tile.rect.Min.X), uint8(tile.rect.Min.Y)
		packet[16], packet[17] = uint8(tile.rect.Dx()), uint8(tile.rect.Dy())
		binary.LittleEndian.PutUint16(packet[18:20], tileIndex)
		binary.LittleEndian.PutUint16(packet[20:22], tileCount)
		copy(packet[22:], jpegData[start:end])
		if _, err := conn.WriteToUDP(packet, address); err != nil {
			return err
		}
	}
	return nil
}

func (state *mirrorDeltaState) prepare(frame []byte, forceKeyframe bool) ([]mirrorTilePayload, error) {
	hash := mirrorFrameHash(frame)
	rects := mirrorTileRects()
	if state.lastCanvas == nil {
		canvas, err := decodeMirrorImage(frame)
		if err != nil {
			return nil, err
		}
		state.lastHash = hash
		state.lastCanvas = canvas
		state.tileHashes = mirrorTileHashes(canvas, rects)
		// Even the first frame is sent as tiles. This avoids painting a whole
		// 160x128 JPEG in one visible burst when entering mirror mode.
		forceKeyframe = true
	}
	if !forceKeyframe && hash == state.lastHash {
		return nil, nil
	}
	canvas := state.lastCanvas
	if hash != state.lastHash || state.lastCanvas == nil {
		var err error
		canvas, err = decodeMirrorImage(frame)
		if err != nil {
			return nil, err
		}
	}
	hashes := mirrorTileHashes(canvas, rects)
	changed := make([]int, 0, len(rects))
	if forceKeyframe || len(state.tileHashes) == 0 {
		for index := range rects {
			changed = append(changed, index)
		}
	} else {
		for index := range rects {
			if index >= len(state.tileHashes) || hashes[index] != state.tileHashes[index] {
				changed = append(changed, index)
			}
		}
	}
	if len(changed) == 0 {
		state.lastHash = hash
		state.lastCanvas = canvas
		state.tileHashes = hashes
		return nil, nil
	}
	tiles := make([]mirrorTilePayload, 0, len(changed))
	for _, index := range changed {
		data, encodeErr := encodeMirrorTile(canvas, rects[index])
		if encodeErr != nil {
			state.lastHash = hash
			state.lastCanvas = canvas
			state.tileHashes = hashes
			return nil, fmt.Errorf("镜像局部块编码失败")
		}
		tiles = append(tiles, mirrorTilePayload{rect: rects[index], data: data})
	}
	state.lastHash = hash
	state.lastCanvas = canvas
	state.tileHashes = hashes
	return tiles, nil
}

func sendMirrorTiles(conn *net.UDPConn, address *net.UDPAddr, frameID *uint16,
	tiles []mirrorTilePayload, packet []byte) error {
	if len(tiles) == 0 || len(tiles) > 64 {
		return nil
	}
	batchID := *frameID
	tileCount := uint16(len(tiles))
	for index, tile := range tiles {
		if err := sendMirrorTile(conn, address, batchID, uint16(index), tileCount, tile, packet); err != nil {
			return err
		}
		if index+1 < len(tiles) {
			time.Sleep(mirrorTileInterval)
		}
	}
	(*frameID)++
	return nil
}

func heartbeatTile(canvas *image.RGBA) (mirrorTilePayload, error) {
	const rectSize = 32
	rect := image.Rect(0, 0, rectSize, rectSize)
	data, err := encodeMirrorTile(canvas, rect)
	if err != nil {
		return mirrorTilePayload{}, err
	}
	return mirrorTilePayload{rect: rect, data: data}, nil
}

func (state *mirrorDeltaState) commitKeyframe(now time.Time) {
	state.lastKeyframe = now
}

func waitMirrorInterval(ctx context.Context, lastSent time.Time) error {
	return waitMirrorIntervalFor(ctx, lastSent, mirrorInterval)
}

func waitMirrorIntervalFor(ctx context.Context, lastSent time.Time, interval time.Duration) error {
	if lastSent.IsZero() {
		return nil
	}
	if wait := interval - time.Since(lastSent); wait > 0 {
		timer := time.NewTimer(wait)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
		}
	}
	return nil
}

func mirrorBatchInterval(tileCount int) time.Duration {
	if tileCount <= 1 {
		return mirrorInterval
	}
	interval := time.Duration(tileCount) * 10 * time.Millisecond
	if interval < mirrorInterval {
		return mirrorInterval
	}
	return interval
}

func sendFrame(conn *net.UDPConn, address *net.UDPAddr, frameID uint16, jpegData []byte, packet []byte) error {
	chunks := (len(jpegData) + chunkPayload - 1) / chunkPayload
	if chunks == 0 || chunks > 64 || len(packet) < chunkPayload+14 {
		return fmt.Errorf("镜像帧分包数量异常：%d", chunks)
	}
	for index := 0; index < chunks; index++ {
		start := index * chunkPayload
		end := start + chunkPayload
		if end > len(jpegData) {
			end = len(jpegData)
		}
		packet = packet[:14+end-start]
		packet[0], packet[1], packet[2] = 'M', 'T', 1
		binary.LittleEndian.PutUint16(packet[3:5], frameID)
		packet[5], packet[6] = mirrorWidth, mirrorHeight
		binary.LittleEndian.PutUint16(packet[7:9], uint16(index))
		binary.LittleEndian.PutUint16(packet[9:11], uint16(chunks))
		binary.LittleEndian.PutUint16(packet[11:13], uint16(end-start))
		packet[13] = 0
		copy(packet[14:], jpegData[start:end])
		if _, err := conn.WriteToUDP(packet, address); err != nil {
			return err
		}
	}
	return nil
}

func sendAgentStatus(conn *net.UDPConn, address *net.UDPAddr, status agentStatus) error {
	packet := []byte{'M', 'T', 1, 0, 0, mirrorWidth, mirrorHeight, 0, 0, 1, 0, 1, 0, agentStatusPacketMode, byte(status)}
	_, err := conn.WriteToUDP(packet, address)
	return err
}

func startAgentStatusPublisher(ctx context.Context, conn *net.UDPConn, address *net.UDPAddr) {
	go func() {
		tracker := newAgentStatusTracker()
		ticker := time.NewTicker(200 * time.Millisecond)
		defer ticker.Stop()
		for {
			status, changed := tracker.poll(time.Now())
			if changed {
				if err := sendAgentStatus(conn, address, status); err != nil {
					return
				}
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

func sendRawFrame(conn *net.UDPConn, address *net.UDPAddr, frameID uint16,
	raw []byte, packet []byte) error {
	if len(raw) != mirrorWidth*mirrorHeight*2 {
		return fmt.Errorf("RGB565 帧尺寸异常：%d 字节", len(raw))
	}
	chunks := (len(raw) + chunkPayload - 1) / chunkPayload
	if chunks == 0 || chunks > 64 || len(packet) < chunkPayload+14 {
		return fmt.Errorf("RGB565 帧分包数量异常：%d", chunks)
	}
	for index := 0; index < chunks; index++ {
		start := index * chunkPayload
		end := start + chunkPayload
		if end > len(raw) {
			end = len(raw)
		}
		packet = packet[:14+end-start]
		packet[0], packet[1], packet[2] = 'M', 'T', 1
		binary.LittleEndian.PutUint16(packet[3:5], frameID)
		packet[5], packet[6] = mirrorWidth, mirrorHeight
		binary.LittleEndian.PutUint16(packet[7:9], uint16(index))
		binary.LittleEndian.PutUint16(packet[9:11], uint16(chunks))
		binary.LittleEndian.PutUint16(packet[11:13], uint16(end-start))
		packet[13] = 2 // raw RGB565 full-frame packet
		copy(packet[14:], raw[start:end])
		if _, err := conn.WriteToUDP(packet, address); err != nil {
			return err
		}
	}
	return nil
}

func sendPreferredFullFrame(conn *net.UDPConn, address *net.UDPAddr, frameID uint16,
	jpegData []byte, packet []byte) error {
	return sendPreferredFullFrameCanvas(conn, address, frameID, jpegData, nil, packet)
}

// sendPreferredFullFrameCanvas accepts the canvas already produced by the
// delta encoder. Raw mode used to decode the same JPEG a second time just
// before converting it to RGB565, which made the supposedly faster path
// noticeably slower on animated desktop content.
func sendPreferredFullFrameCanvas(conn *net.UDPConn, address *net.UDPAddr, frameID uint16,
	jpegData []byte, canvas *image.RGBA, packet []byte) error {
	if !useRawFullFrame() {
		return sendFrame(conn, address, frameID, jpegData, packet)
	}
	if canvas == nil {
		var err error
		canvas, err = decodeMirrorImage(jpegData)
		if err != nil {
			return err
		}
	}
	return sendRawFrame(conn, address, frameID, encodeMirrorRGB565(canvas), packet)
}

func mirrorLoop(ctx context.Context, deviceAddress string) error {
	host := deviceHost(deviceAddress)
	address, err := net.ResolveUDPAddr("udp", net.JoinHostPort(host, fmt.Sprint(mirrorPort)))
	if err != nil {
		return err
	}
	conn, err := net.ListenUDP("udp", nil)
	if err != nil {
		return err
	}
	defer conn.Close()
	stream, streamErr := startScreenCapture(ctx)
	if streamErr == nil {
		defer stream.close()
	}
	var capturePath string
	if streamErr != nil {
		// Keep the command-based path as a compatibility fallback for Macs where
		// ffmpeg cannot access Screen Recording permission yet.
		temp, err := os.CreateTemp("", "mini-tv-capture-*.jpg")
		if err != nil {
			return err
		}
		capturePath = temp.Name()
		temp.Close()
		defer os.Remove(capturePath)
	}
	capture := func() ([]byte, error) {
		if stream != nil {
			frame, err := stream.nextFrame(ctx)
			if err != nil {
				return nil, err
			}
			// FFmpeg's MJPEG quality is intentionally tuned for capture speed,
			// so a noisy desktop can occasionally exceed the ESP32 frame buffer.
			// Normalize only those rare oversized frames; normal frames avoid an
			// extra decode/encode pass.
			return normalizeMirrorFrame(frame)
		}
		return captureFrame(ctx, capturePath)
	}
	frameID := uint16(0)
	packet := make([]byte, chunkPayload+22)
	delta := mirrorDeltaState{}
	// Send the first frame immediately so the device does not sit on the mirror placeholder.
	frame, err := capture()
	if err != nil && stream != nil {
		// ffmpeg can start successfully and then fail when Screen Recording
		// permission is denied. Fall back to Apple's one-shot utility instead of
		// leaving the mirror session stuck on a blank frame.
		stream.close()
		stream = nil
		temp, tempErr := os.CreateTemp("", "mini-tv-capture-*.jpg")
		if tempErr != nil {
			return tempErr
		}
		capturePath = temp.Name()
		temp.Close()
		defer os.Remove(capturePath)
		frame, err = capture()
	}
	if err != nil {
		return err
	}
	if err := sendPreferredFullFrameCanvas(conn, address, frameID, frame, delta.lastCanvas, packet); err != nil {
		return err
	}
	delta.lastHash = mirrorFrameHash(frame)
	delta.commitKeyframe(time.Now())
	frameID++
	lastSentAt := time.Now()
	lastHeartbeatAt := lastSentAt
	lastCaptureAt := time.Now()
	startAgentStatusPublisher(ctx, conn, address)
	for {
		if stream == nil {
			if err := waitMirrorInterval(ctx, lastCaptureAt); err != nil {
				return nil
			}
		}
		frame, err := capture()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		lastCaptureAt = time.Now()
		hash := mirrorFrameHash(frame)
		changed := hash != delta.lastHash
		heartbeat := time.Since(lastHeartbeatAt) >= mirrorHeartbeatInterval
		// Repeated desktop frames are common when the user is idle. Keep the
		// receiver lease alive with an occasional heartbeat, but do not decode
		// and draw the same JPEG on every capture tick.
		if !changed && !heartbeat {
			continue
		}
		if changed {
			if err := waitMirrorInterval(ctx, lastSentAt); err != nil {
				return nil
			}
			if err := sendPreferredFullFrame(conn, address, frameID, frame, packet); err != nil {
				return err
			}
			delta.lastHash = hash
			frameID++
		} else if heartbeat {
			if err := waitMirrorIntervalFor(ctx, lastSentAt, mirrorBatchInterval(1)); err != nil {
				return nil
			}
			if err := sendPreferredFullFrame(conn, address, frameID, frame, packet); err != nil {
				return err
			}
			frameID++
		} else {
			continue
		}
		lastSentAt = time.Now()
		lastHeartbeatAt = lastSentAt
	}
}

func deviceHost(address string) string {
	base := strings.TrimSpace(address)
	if !strings.Contains(base, "://") {
		base = "http://" + base
	}
	if parsed, err := url.Parse(base); err == nil && parsed.Hostname() != "" {
		return parsed.Hostname()
	}
	// Keep a conservative fallback for malformed user input.
	base = strings.TrimPrefix(strings.TrimPrefix(base, "http://"), "https://")
	if index := strings.IndexByte(base, '/'); index >= 0 {
		base = base[:index]
	}
	if host, _, err := net.SplitHostPort(base); err == nil {
		return host
	}
	return base
}
