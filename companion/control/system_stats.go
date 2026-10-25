package main

import (
	"bufio"
	"context"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

type ComputerTelemetry struct {
	CPU      int
	Memory   int
	Battery  int
	Charging bool
	Playing  bool
	Track    string
	Artist   string
	Position int
	Duration int
}

func commandOutput(ctx context.Context, name string, args ...string) (string, error) {
	output, err := exec.CommandContext(ctx, name, args...).Output()
	return string(output), err
}

func shortAppleScript(ctx context.Context, script string) (string, error) {
	// A missing automation permission can make osascript wait for several
	// seconds. Keep one unresponsive player from blocking browser/media
	// fallbacks and from delaying the rest of the telemetry sample.
	queryCtx, cancel := context.WithTimeout(ctx, 800*time.Millisecond)
	defer cancel()
	return commandOutput(queryCtx, "osascript", "-e", script)
}

func readCPU(ctx context.Context) int {
	// `top -l 1` takes roughly two seconds on macOS and can consume a full
	// core while it samples every process. `ps` exposes the same per-process
	// CPU values with a much smaller and faster snapshot, which is sufficient
	// for the coarse dashboard metric.
	out, err := commandOutput(ctx, "ps", "-A", "-o", "%cpu=")
	if err != nil {
		return 0
	}
	var total float64
	for _, line := range strings.Fields(out) {
		value, parseErr := strconv.ParseFloat(line, 64)
		if parseErr == nil {
			total += value
		}
	}
	return clampPercent(int(total))
}

func readMemory(ctx context.Context) int {
	vm, err := commandOutput(ctx, "vm_stat")
	if err != nil {
		return 0
	}
	totalOut, err := commandOutput(ctx, "sysctl", "-n", "hw.memsize")
	if err != nil {
		return 0
	}
	total, _ := strconv.ParseFloat(strings.TrimSpace(totalOut), 64)
	if total <= 0 {
		return 0
	}
	pageSize := 4096.0
	if match := regexp.MustCompile(`page size of (\d+) bytes`).FindStringSubmatch(vm); len(match) == 2 {
		pageSize, _ = strconv.ParseFloat(match[1], 64)
	}
	usedPages := 0.0
	scanner := bufio.NewScanner(strings.NewReader(vm))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "Pages free") || strings.HasPrefix(line, "Pages inactive") || strings.HasPrefix(line, "Pages speculative") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		value := strings.TrimRight(parts[len(parts)-1], ".")
		pages, err := strconv.ParseFloat(value, 64)
		if err == nil {
			usedPages += pages
		}
	}
	return clampPercent(int((usedPages * pageSize / total) * 100))
}

func readBattery(ctx context.Context) (int, bool) {
	out, err := commandOutput(ctx, "pmset", "-g", "batt")
	if err != nil {
		return 0, false
	}
	match := regexp.MustCompile(`([0-9]+)%`).FindStringSubmatch(out)
	percent := 0
	if len(match) == 2 {
		percent, _ = strconv.Atoi(match[1])
	}
	lower := strings.ToLower(out)
	return clampPercent(percent), strings.Contains(lower, "charging") || strings.Contains(lower, "charged")
}

func readMusic(ctx context.Context) (string, string, bool, int, int) {
	// Never use `tell application` for a process that is not running: AppleScript
	// would launch the player just to answer a status query.
	for _, app := range []string{"Music", "Spotify"} {
		if _, err := exec.LookPath("pgrep"); err != nil {
			break
		}
		if _, err := commandOutput(ctx, "pgrep", "-x", app); err != nil {
			continue
		}
		script := `tell application "` + app + `"
if player state is stopped then return ""
if not (exists current track) then return ""
return (get name of current track) & "\t" & (get artist of current track) & "\t" & (get player state) & "\t" & (get player position) & "\t" & (get duration of current track)
end tell`
		out, err := shortAppleScript(ctx, script)
		if err != nil {
			continue
		}
		parts := strings.Split(strings.TrimSpace(out), "\t")
		if len(parts) < 5 || strings.TrimSpace(parts[0]) == "" {
			continue
		}
		position, _ := strconv.ParseFloat(strings.TrimSpace(parts[3]), 64)
		duration, _ := strconv.ParseFloat(strings.TrimSpace(parts[4]), 64)
		return trimText(parts[0], 47), trimText(parts[1], 39), strings.Contains(strings.ToLower(parts[2]), "playing"), int(position), int(duration)
	}

	// Lightweight fallbacks for players that expose their current item through
	// the front tab/window title rather than the Music scripting dictionary.
	for _, player := range []struct {
		process string
		app     string
		script  string
	}{
		{"IINA", "IINA", `tell application "IINA" to if playing then (get name of current player item) & "\tIINA\tplaying"`},
		{"VLC", "VLC", `tell application "VLC" to if playing then (get name of current item) & "\tVLC\tplaying"`},
	} {
		if _, err := commandOutput(ctx, "pgrep", "-x", player.process); err != nil {
			continue
		}
		out, err := shortAppleScript(ctx, player.script)
		if err != nil || strings.TrimSpace(out) == "" {
			continue
		}
		parts := strings.Split(strings.TrimSpace(out), "\t")
		if len(parts) >= 2 && strings.TrimSpace(parts[0]) != "" {
			return trimText(parts[0], 47), trimText(parts[1], 39), true, 0, 0
		}
	}

	// Browser players do not expose a standard media API to a sandboxed
	// desktop app. The active tab title is still useful for YouTube, SoundCloud
	// and similar services, and is deliberately reported as paused/unknown
	// rather than pretending it is a native player.
	for _, browser := range []struct {
		process string
		app     string
		script  string
	}{
		{"Microsoft Edge", "Microsoft Edge", `tell application "Microsoft Edge" to get title of active tab of front window`},
		{"Google Chrome", "Google Chrome", `tell application "Google Chrome" to get title of active tab of front window`},
		{"Safari", "Safari", `tell application "Safari" to get name of current tab of front window`},
	} {
		if _, err := commandOutput(ctx, "pgrep", "-x", browser.process); err != nil {
			continue
		}
		out, err := shortAppleScript(ctx, browser.script)
		if err != nil {
			continue
		}
		title := strings.TrimSpace(out)
		if title != "" {
			return trimText(title, 47), "浏览器媒体", true, 0, 0
		}
	}
	return "", "", false, 0, 0
}

func collectTelemetryFresh(ctx context.Context) ComputerTelemetry {
	var result ComputerTelemetry
	var group sync.WaitGroup
	group.Add(4)
	go func() { defer group.Done(); result.CPU = readCPU(ctx) }()
	go func() { defer group.Done(); result.Memory = readMemory(ctx) }()
	go func() { defer group.Done(); result.Battery, result.Charging = readBattery(ctx) }()
	go func() {
		defer group.Done()
		result.Track, result.Artist, result.Playing, result.Position, result.Duration = readMusic(ctx)
	}()
	group.Wait()
	return result
}

// Telemetry is requested by both the visible dashboard and the background
// device sync loop. Keep one short-lived sample and let concurrent callers
// share the same work instead of spawning multiple `ps`, `vm_stat` and
// AppleScript processes at once.
var telemetryCache = struct {
	sync.Mutex
	value     ComputerTelemetry
	fetchedAt time.Time
	inFlight  bool
	done      chan struct{}
}{}

const telemetryCacheTTL = 2 * time.Second

func collectTelemetry(ctx context.Context) ComputerTelemetry {
	for {
		telemetryCache.Lock()
		if !telemetryCache.fetchedAt.IsZero() && time.Since(telemetryCache.fetchedAt) < telemetryCacheTTL {
			value := telemetryCache.value
			telemetryCache.Unlock()
			return value
		}
		if telemetryCache.inFlight {
			done := telemetryCache.done
			telemetryCache.Unlock()
			select {
			case <-done:
				continue
			case <-ctx.Done():
				return ComputerTelemetry{}
			}
		}
		telemetryCache.inFlight = true
		telemetryCache.done = make(chan struct{})
		done := telemetryCache.done
		telemetryCache.Unlock()

		value := collectTelemetryFresh(ctx)
		telemetryCache.Lock()
		telemetryCache.value = value
		telemetryCache.fetchedAt = time.Now()
		telemetryCache.inFlight = false
		close(done)
		telemetryCache.Unlock()
		return value
	}
}

func clampPercent(value int) int {
	if value < 0 {
		return 0
	}
	if value > 100 {
		return 100
	}
	return value
}

func trimText(value string, maxBytes int) string {
	value = strings.TrimSpace(value)
	if len(value) <= maxBytes {
		return value
	}
	for len(value) > maxBytes {
		value = value[:len(value)-1]
	}
	for len(value) > 0 && (value[len(value)-1]&0xC0) == 0x80 {
		value = value[:len(value)-1]
	}
	return value
}
