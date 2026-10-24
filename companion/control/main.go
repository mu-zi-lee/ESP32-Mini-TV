package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"
)

type DeviceStatus struct {
	OK               bool   `json:"ok"`
	Mode             string `json:"mode"`
	StationConnected bool   `json:"stationConnected"`
	StationIP        string `json:"stationIp"`
	DeviceIP         string `json:"deviceIp"`
	SetupSSID        string `json:"setupSsid"`
	SetupIP          string `json:"setupIp"`
	RSSI             int    `json:"rssi"`
	TelemetryValid   bool   `json:"telemetryValid"`
	Playing          bool   `json:"playing"`
	Track            string `json:"track"`
	Artist           string `json:"artist"`
	MirrorActive     bool   `json:"mirrorActive"`
	MirrorFrames     int    `json:"mirrorFrames"`
}
type Network struct {
	SSID   string `json:"ssid"`
	RSSI   int    `json:"rssi"`
	Secure bool   `json:"secure"`
}
type NetworkList struct {
	OK       bool      `json:"ok"`
	Networks []Network `json:"networks"`
}
type Result struct {
	OK      bool   `json:"ok"`
	Message string `json:"message,omitempty"`
	Mode    string `json:"mode,omitempty"`
}
type TelemetryPayload struct {
	CPU      int    `json:"cpu"`
	Memory   int    `json:"memory"`
	Battery  int    `json:"battery"`
	Charging bool   `json:"charging"`
	Playing  bool   `json:"playing"`
	Track    string `json:"track"`
	Artist   string `json:"artist"`
	Position int    `json:"position"`
	Duration int    `json:"duration"`
}

func telemetryPayload(stats ComputerTelemetry) TelemetryPayload {
	return TelemetryPayload{
		CPU: stats.CPU, Memory: stats.Memory, Battery: stats.Battery, Charging: stats.Charging,
		Playing: stats.Playing, Track: stats.Track, Artist: stats.Artist,
		Position: stats.Position, Duration: stats.Duration,
	}
}

type DeviceService struct{}

const deviceRequestTimeout = 4 * time.Second

type mirrorTask struct {
	cancel context.CancelFunc
	kind   string
	token  uint64
}

var backgroundTasks = struct {
	sync.Mutex
	sync       map[string]context.CancelFunc
	mirror     map[string]mirrorTask
	nextMirror uint64
}{sync: make(map[string]context.CancelFunc), mirror: make(map[string]mirrorTask)}

func (DeviceService) base(address string) string {
	address = strings.TrimSpace(address)
	if address == "" {
		address = "http://192.168.4.1"
	}
	if !strings.HasPrefix(address, "http://") && !strings.HasPrefix(address, "https://") {
		address = "http://" + address
	}
	return strings.TrimRight(address, "/")
}
func (d DeviceService) call(address, path, method string, payload any, out any) error {
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		body = bytes.NewReader(encoded)
	}
	req, err := http.NewRequest(method, d.base(address)+path, body)
	if err != nil {
		return err
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := (&http.Client{Timeout: deviceRequestTimeout}).Do(req)
	if err != nil {
		return fmt.Errorf("device request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("device returned %s: %s", resp.Status, strings.TrimSpace(string(data)))
	}
	if out != nil {
		if err := json.NewDecoder(io.LimitReader(resp.Body, 16*1024)).Decode(out); err != nil {
			return fmt.Errorf("invalid device response: %w", err)
		}
	}
	return nil
}
func (d DeviceService) Status(address string) (DeviceStatus, error) {
	var r DeviceStatus
	err := d.call(address, "/api/status", http.MethodGet, nil, &r)
	return r, err
}
func (d DeviceService) ScanNetworks(address string) (NetworkList, error) {
	var r NetworkList
	err := d.call(address, "/api/scan", http.MethodGet, nil, &r)
	return r, err
}
func (d DeviceService) SetWiFi(address, ssid, password string) (Result, error) {
	if strings.TrimSpace(ssid) == "" {
		return Result{}, errors.New("SSID cannot be empty")
	}
	var r Result
	err := d.call(address, "/api/wifi", http.MethodPost, map[string]string{"ssid": ssid, "password": password}, &r)
	return r, err
}
func (d DeviceService) SetMode(address, mode string) (Result, error) {
	var r Result
	err := d.call(address, "/api/mode", http.MethodPost, map[string]string{"mode": mode}, &r)
	return r, err
}

func (d DeviceService) SendTelemetry(address string) (Result, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	stats := collectTelemetry(ctx)
	var result Result
	err := d.call(address, "/api/telemetry", http.MethodPost, telemetryPayload(stats), &result)
	return result, err
}

// ComputerStatus reads the current Mac state for the control surface. It only
// inspects already-running processes and never launches a media application.
func (d DeviceService) ComputerStatus() TelemetryPayload {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return telemetryPayload(collectTelemetry(ctx))
}

func (d DeviceService) setMirror(address string, active bool) error {
	var result Result
	return d.call(address, "/api/mirror", http.MethodPost, map[string]bool{"active": active}, &result)
}

// startMirrorTask owns the device mirror lease for one capture source. There
// must never be two producers sending frames to the same ESP32: both streams
// use independent frame counters and the older producer can otherwise turn
// off the newer one when it exits.
func (d DeviceService) startMirrorTask(address, kind string, loop func(context.Context, string) error) (Result, error) {
	address = d.base(address)
	var previous mirrorTask
	backgroundTasks.Lock()
	if current, exists := backgroundTasks.mirror[address]; exists {
		if current.kind == kind {
			backgroundTasks.Unlock()
			return Result{OK: true, Message: "镜像已在运行"}, nil
		}
		previous = current
		delete(backgroundTasks.mirror, address)
	}
	backgroundTasks.Unlock()
	if previous.cancel != nil {
		previous.cancel()
		// Clear the device state before enabling the replacement producer. The
		// old task's deferred cleanup is guarded by its lease token below.
		_ = d.setMirror(address, false)
	}
	if err := d.setMirror(address, true); err != nil {
		return Result{}, err
	}
	if _, err := d.SetMode(address, "mirror"); err != nil {
		_ = d.setMirror(address, false)
		return Result{}, err
	}

	ctx, cancel := context.WithCancel(context.Background())
	backgroundTasks.Lock()
	backgroundTasks.nextMirror++
	token := backgroundTasks.nextMirror
	backgroundTasks.mirror[address] = mirrorTask{cancel: cancel, kind: kind, token: token}
	backgroundTasks.Unlock()
	go func() {
		if err := loop(ctx, address); err != nil {
			log.Printf("%s 已停止：%v", kind, err)
		}
		backgroundTasks.Lock()
		current, ownsLease := backgroundTasks.mirror[address]
		if ownsLease && current.token == token {
			delete(backgroundTasks.mirror, address)
		}
		backgroundTasks.Unlock()
		if ownsLease && current.token == token {
			// Only the current producer may turn the device mirror off. A stale
			// producer must never interrupt a replacement task.
			_ = d.setMirror(address, false)
		}
	}()
	return Result{OK: true, Message: kind + "已启动"}, nil
}

func (d DeviceService) stopMirrorTask(address, kind string) {
	address = d.base(address)
	var task mirrorTask
	backgroundTasks.Lock()
	if current, exists := backgroundTasks.mirror[address]; exists && (kind == "" || current.kind == kind) {
		task = current
		delete(backgroundTasks.mirror, address)
	}
	backgroundTasks.Unlock()
	if task.cancel != nil {
		task.cancel()
		_ = d.setMirror(address, false)
	}
}

func (d DeviceService) StartSync(address string) (Result, error) {
	address = d.base(address)
	backgroundTasks.Lock()
	if _, exists := backgroundTasks.sync[address]; exists {
		backgroundTasks.Unlock()
		return Result{OK: true, Message: "同步已在运行"}, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	backgroundTasks.sync[address] = cancel
	backgroundTasks.Unlock()
	go func() {
		ticker := time.NewTicker(3 * time.Second)
		defer ticker.Stop()
		defer func() {
			backgroundTasks.Lock()
			delete(backgroundTasks.sync, address)
			backgroundTasks.Unlock()
		}()
		for {
			_, _ = d.SendTelemetry(address)
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	return Result{OK: true, Message: "电脑状态同步已启动"}, nil
}

func (d DeviceService) StopSync(address string) (Result, error) {
	address = d.base(address)
	backgroundTasks.Lock()
	if cancel, exists := backgroundTasks.sync[address]; exists {
		cancel()
		delete(backgroundTasks.sync, address)
	}
	backgroundTasks.Unlock()
	return Result{OK: true, Message: "电脑状态同步已停止"}, nil
}

func (d DeviceService) StartMirror(address string) (Result, error) {
	return d.startMirrorTask(address, "镜像", mirrorLoop)
}

func (d DeviceService) StopMirror(address string) (Result, error) {
	d.stopMirrorTask(address, "镜像")
	return Result{OK: true, Message: "镜像已停止"}, nil
}

func (d DeviceService) StartVirtualMirror(address string) (Result, error) {
	address = d.base(address)
	log.Printf("virtual mirror: start requested address=%s display=%+v", address, virtualDisplayStatus())
	if status := virtualDisplayStatus(); !status.Active {
		return Result{}, errors.New("请先创建系统虚拟屏幕")
	}
	result, err := d.startMirrorTask(address, "虚拟屏幕镜像", virtualMirrorLoop)
	if err != nil {
		log.Printf("virtual mirror: start failed: %v", err)
	}
	return result, err
}

func (d DeviceService) StopVirtualMirror(address string) (Result, error) {
	d.stopMirrorTask(address, "虚拟屏幕镜像")
	return Result{OK: true, Message: "虚拟屏幕镜像已停止"}, nil
}

func (DeviceService) VirtualDisplayStatus() VirtualDisplayStatus {
	return virtualDisplayStatus()
}

func (DeviceService) StartVirtualDisplay() (VirtualDisplayStatus, error) {
	return virtualDisplay.Start()
}

func (DeviceService) StopVirtualDisplay() VirtualDisplayStatus {
	return virtualDisplay.Stop()
}

func stopBackgroundTasks() {
	backgroundTasks.Lock()
	for _, cancel := range backgroundTasks.sync {
		cancel()
	}
	for _, task := range backgroundTasks.mirror {
		task.cancel()
	}
	backgroundTasks.sync = make(map[string]context.CancelFunc)
	backgroundTasks.mirror = make(map[string]mirrorTask)
	backgroundTasks.Unlock()
	_ = virtualDisplay.Stop()
}

func main() {
	mygo.Bind(DeviceService{})
	mygo.App.WhenReady(func() {
		window := mygo.NewWindow(mygo.WindowOptions{Title: "ESP32 Mini TV Control", Width: 900, Height: 620, MinWidth: 620, MinHeight: 480, URL: "/", StateKey: "main", BackgroundColor: "light-dark(#f4f7fb, #0c1118)"})
		window.OnClose(func(*mygo.CloseEvent) { stopBackgroundTasks() })
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
