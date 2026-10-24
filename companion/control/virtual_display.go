package main

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	virtualDisplayWidth  = 160
	virtualDisplayHeight = 128
	virtualFrameBytes    = virtualDisplayWidth * virtualDisplayHeight * 4
)

type VirtualDisplayStatus struct {
	Active    bool   `json:"active"`
	DisplayID uint32 `json:"displayId"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	Mode      string `json:"mode"`
	Message   string `json:"message,omitempty"`
}

type virtualDisplaySession struct {
	mu        sync.Mutex
	cmd       *exec.Cmd
	socket    string
	displayID uint32
}

var virtualDisplay = &virtualDisplaySession{}

func virtualHelperPath() string {
	if configured := strings.TrimSpace(os.Getenv("MINI_TV_VIRTUAL_HELPER")); configured != "" {
		return configured
	}
	candidates := make([]string, 0, 4)
	if executable, err := os.Executable(); err == nil {
		base := filepath.Dir(executable)
		candidates = append(candidates,
			filepath.Join(base, "..", "Resources", "virtual-display-helper"),
			filepath.Join(base, "resources", "virtual-display-helper"),
		)
	}
	if working, err := os.Getwd(); err == nil {
		candidates = append(candidates, filepath.Join(working, "resources", "virtual-display-helper"))
	}
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && info.Mode().Perm()&0111 != 0 {
			return candidate
		}
	}
	return ""
}

func (v *virtualDisplaySession) Start() (VirtualDisplayStatus, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.cmd != nil && v.cmd.Process != nil {
		return VirtualDisplayStatus{Active: true, DisplayID: v.displayID, Width: 640, Height: 512, Mode: "系统虚拟屏幕", Message: "虚拟屏幕已存在"}, nil
	}
	helper := virtualHelperPath()
	if helper == "" {
		return VirtualDisplayStatus{}, errors.New("找不到虚拟屏幕助手，请重新构建 Control 应用")
	}
	socket := filepath.Join(os.TempDir(), fmt.Sprintf("mini-tv-virtual-%d.sock", os.Getpid()))
	_ = os.Remove(socket)
	cmd := exec.Command(helper, "--socket", socket)
	cmd.Dir = filepath.Dir(helper)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return VirtualDisplayStatus{}, err
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return VirtualDisplayStatus{}, fmt.Errorf("启动虚拟屏幕失败：%w", err)
	}
	ready := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if strings.HasPrefix(line, "READY ") {
				ready <- line
				return
			}
		}
		ready <- ""
	}()
	select {
	case line := <-ready:
		parts := strings.Fields(line)
		if len(parts) != 2 {
			_ = cmd.Process.Kill()
			return VirtualDisplayStatus{}, errors.New("虚拟屏幕助手没有返回显示器编号")
		}
		displayID, parseErr := strconv.ParseUint(parts[1], 10, 32)
		if parseErr != nil {
			_ = cmd.Process.Kill()
			return VirtualDisplayStatus{}, fmt.Errorf("虚拟屏幕编号无效：%w", parseErr)
		}
		v.cmd, v.socket, v.displayID = cmd, socket, uint32(displayID)
		return VirtualDisplayStatus{Active: true, DisplayID: v.displayID, Width: 640, Height: 512, Mode: "系统虚拟屏幕", Message: "已创建，可在系统显示器设置中拖动窗口"}, nil
	case <-time.After(5 * time.Second):
		_ = cmd.Process.Kill()
		return VirtualDisplayStatus{}, errors.New("创建虚拟屏幕超时，请检查 macOS 版本和屏幕录制权限")
	}
}

func (v *virtualDisplaySession) Connect(ctx context.Context) (io.ReadWriteCloser, error) {
	v.mu.Lock()
	socket := v.socket
	active := v.cmd != nil && v.cmd.Process != nil
	v.mu.Unlock()
	if !active || socket == "" {
		return nil, errors.New("虚拟屏幕尚未创建")
	}
	log.Printf("virtual display: connecting socket=%s", socket)
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	for {
		conn, err := dialUnix(socket)
		if err == nil {
			log.Printf("virtual display: socket connected")
			return conn, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-deadline.C:
			log.Printf("virtual display: socket timeout: %v", err)
			return nil, fmt.Errorf("连接虚拟屏幕帧流失败：%w", err)
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func dialUnix(path string) (io.ReadWriteCloser, error) {
	return net.Dial("unix", path)
}

func (v *virtualDisplaySession) Stop() VirtualDisplayStatus {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.cmd != nil && v.cmd.Process != nil {
		_ = v.cmd.Process.Signal(os.Interrupt)
		_ = v.cmd.Wait()
	}
	if v.socket != "" {
		_ = os.Remove(v.socket)
	}
	v.cmd, v.socket, v.displayID = nil, "", 0
	return VirtualDisplayStatus{Active: false, Width: 640, Height: 512, Mode: "未创建", Message: "虚拟屏幕已关闭"}
}

func readVirtualFrame(reader io.Reader) ([]byte, error) {
	header := make([]byte, 20)
	if _, err := io.ReadFull(reader, header); err != nil {
		return nil, err
	}
	if string(header[:4]) != "VDF1" {
		return nil, errors.New("虚拟屏幕帧头无效")
	}
	width := binary.LittleEndian.Uint16(header[4:6])
	height := binary.LittleEndian.Uint16(header[6:8])
	payloadLength := binary.LittleEndian.Uint32(header[8:12])
	if width != virtualDisplayWidth || height != virtualDisplayHeight || payloadLength != virtualFrameBytes {
		return nil, fmt.Errorf("虚拟屏幕帧尺寸异常：%dx%d / %d", width, height, payloadLength)
	}
	bgra := make([]byte, virtualFrameBytes)
	if _, err := io.ReadFull(reader, bgra); err != nil {
		return nil, err
	}
	canvas := image.NewRGBA(image.Rect(0, 0, virtualDisplayWidth, virtualDisplayHeight))
	for index := 0; index < len(bgra); index += 4 {
		canvas.Pix[index] = bgra[index+2]
		canvas.Pix[index+1] = bgra[index+1]
		canvas.Pix[index+2] = bgra[index]
		canvas.Pix[index+3] = bgra[index+3]
	}
	return encodeMirrorJPEG(canvas)
}

func virtualDisplayStatus() VirtualDisplayStatus {
	virtualDisplay.mu.Lock()
	defer virtualDisplay.mu.Unlock()
	active := virtualDisplay.cmd != nil && virtualDisplay.cmd.Process != nil
	status := VirtualDisplayStatus{Active: active, DisplayID: virtualDisplay.displayID, Width: 640, Height: 512, Mode: "未创建"}
	if active {
		status.Mode = "系统虚拟屏幕"
	}
	return status
}

func virtualMirrorLoop(ctx context.Context, deviceAddress string) error {
	log.Printf("virtual mirror: loop starting device=%s", deviceAddress)
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
	stream, err := virtualDisplay.Connect(ctx)
	if err != nil {
		return err
	}
	defer stream.Close()
	// Closing the Unix connection on cancellation unblocks io.ReadFull even
	// when the virtual display is idle and has not emitted a new frame.
	stopReader := make(chan struct{})
	if conn, ok := stream.(net.Conn); ok {
		go func() {
			select {
			case <-ctx.Done():
				_ = conn.Close()
			case <-stopReader:
			}
		}()
		defer close(stopReader)
	}
	// CGDisplayStream emits frames when the virtual display changes. A static
	// desktop therefore produces no callbacks after the first frame, while the
	// ESP32 receiver intentionally expires a silent stream. Keep the newest
	// encoded frame and resend it as a lightweight heartbeat so a static screen
	// remains visible and the receiver can keep its mirror lease alive.
	frames := make(chan []byte, 1)
	readErrors := make(chan error, 1)
	go func() {
		for {
			frame, readErr := readVirtualFrame(stream)
			if readErr != nil {
				readErrors <- readErr
				return
			}
			select {
			case frames <- frame:
			default:
				// Drop an older pending frame when macOS produces updates faster
				// than the network can commit them.
				select {
				case <-frames:
				default:
				}
				select {
				case frames <- frame:
				default:
				}
			}
		}
	}()
	frameID := uint16(0)
	packet := make([]byte, chunkPayload+14)
	delta := mirrorDeltaState{}
	var latest []byte
	lastHeartbeatAt := time.Time{}
	ticker := time.NewTicker(mirrorInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-readErrors:
			if ctx.Err() != nil {
				return nil
			}
			return err
		case frame := <-frames:
			latest = frame
		case <-ticker.C:
			if len(latest) == 0 {
				continue
			}
			changed := delta.lastCanvas == nil || mirrorFrameHash(latest) != delta.lastHash
			heartbeat := lastHeartbeatAt.IsZero() || time.Since(lastHeartbeatAt) >= mirrorHeartbeatInterval
			if !changed && !heartbeat {
				continue
			}
			forceKeyframe := time.Since(delta.lastKeyframe) >= mirrorKeyframeInterval
			full, tiles, prepareErr := delta.prepare(latest, forceKeyframe)
			if prepareErr != nil {
				log.Printf("virtual mirror: frame decode failed: %v", prepareErr)
				return prepareErr
			}
			if full {
				if err := sendPreferredFullFrame(conn, address, frameID, latest, packet); err != nil {
					log.Printf("virtual mirror: udp send failed: %v", err)
					return err
				}
				delta.commitKeyframe(time.Now())
			} else if len(tiles) > 0 {
				for _, tile := range tiles {
					if err := sendMirrorTile(conn, address, frameID, tile, packet); err != nil {
						log.Printf("virtual mirror: udp tile send failed: %v", err)
						return err
					}
					frameID++
				}
			} else if err := sendFrame(conn, address, frameID, latest, packet); err != nil {
				log.Printf("virtual mirror: udp heartbeat failed: %v", err)
				return err
			}
			if full || len(tiles) == 0 {
				frameID++
			}
			lastHeartbeatAt = time.Now()
		}
	}
}
