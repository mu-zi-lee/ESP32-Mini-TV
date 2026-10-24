package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
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
	// The panel is small, but text benefits from a little extra JPEG quality.
	// The size guard below falls back to a lower quality for unusually noisy
	// frames so one frame can never monopolize the UDP receive buffer.
	jpegQuality    = 88
	minJPEGQuality = 82
	// This is a target idle time between completed frames, not a ticker period.
	// It prevents capture/encode work from piling up when macOS is busy.
	// The ESP32 decodes and flushes a full 160x128 frame synchronously. A
	// modest gap prevents its UDP queue from filling while it is drawing.
	mirrorInterval = 120 * time.Millisecond
)

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
	if index == "" {
		// AVFoundation device order varies with connected cameras and displays.
		// Override this with MINI_TV_SCREEN_INDEX when the second display is used.
		index = "3"
	}
	cmd := exec.CommandContext(ctx, "ffmpeg", "-hide_banner", "-loglevel", "error",
		"-f", "avfoundation", "-framerate", "15", "-pixel_format", "uyvy422",
		"-i", index+":none",
		"-vf", "scale=160:128:force_original_aspect_ratio=increase,crop=160:128",
		"-f", "image2pipe", "-vcodec", "mjpeg", "-q:v", "5", "pipe:1")
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
	var encoded bytes.Buffer
	encoded.Grow(24 * 1024)
	for quality := jpegQuality; quality >= minJPEGQuality; quality -= 2 {
		encoded.Reset()
		if err := jpeg.Encode(&encoded, canvas, &jpeg.Options{Quality: quality}); err != nil {
			return nil, err
		}
		if encoded.Len() <= 48*1024 || quality == minJPEGQuality {
			break
		}
	}
	if encoded.Len() > 48*1024 {
		return nil, fmt.Errorf("镜像帧过大（%d 字节）", encoded.Len())
	}
	return encoded.Bytes(), nil
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
			return stream.nextFrame(ctx)
		}
		return captureFrame(ctx, capturePath)
	}
	frameID := uint16(0)
	packet := make([]byte, chunkPayload+14)
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
	if err := sendFrame(conn, address, frameID, frame, packet); err != nil {
		return err
	}
	frameID++
	for {
		started := time.Now()
		frame, err := capture()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		if err := sendFrame(conn, address, frameID, frame, packet); err != nil {
			return err
		}
		frameID++
		// The stream naturally paces itself. The fallback path needs a small
		// guard so a fast screencapture command cannot flood the ESP32.
		if stream == nil {
			if wait := mirrorInterval - time.Since(started); wait > 0 {
				timer := time.NewTimer(wait)
				select {
				case <-ctx.Done():
					timer.Stop()
					return nil
				case <-timer.C:
				}
			}
		}
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
