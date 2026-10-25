package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// The wire protocol only needs one aggregate state for the ESP32, while the
// desktop app keeps each provider separate for debugging concurrent sessions.
type agentStatus uint8

const (
	agentIdle agentStatus = iota
	agentWorking
	agentDone
	agentError
	agentQuestion
)

func (status agentStatus) String() string {
	switch status {
	case agentWorking:
		return "working"
	case agentDone:
		return "done"
	case agentError:
		return "error"
	case agentQuestion:
		return "question"
	default:
		return "idle"
	}
}

type agentStatusEntry struct {
	State     string `json:"state"`
	Source    string `json:"source"`
	SessionID string `json:"sessionId,omitempty"`
	Event     string `json:"event,omitempty"`
	Message   string `json:"message,omitempty"`
	UpdatedAt int64  `json:"updatedAt"`
}

type agentStatusEvent struct {
	State     string `json:"state"`
	Source    string `json:"source"`
	SessionID string `json:"sessionId,omitempty"`
	Event     string `json:"event,omitempty"`
	Message   string `json:"message,omitempty"`
	UpdatedAt int64  `json:"updatedAt"`
}

type agentStatusDocument struct {
	Agents    map[string]agentStatusEntry `json:"agents"`
	Events    []agentStatusEvent          `json:"events"`
	UpdatedAt int64                       `json:"updatedAt"`
	// Backwards compatibility with the original single-agent hook format.
	State     string `json:"state"`
	Source    string `json:"source"`
	SessionID string `json:"sessionId"`
	Event     string `json:"event"`
	Message   string `json:"message"`
}

type AgentStatusInfo struct {
	State     string `json:"state"`
	Source    string `json:"source"`
	SessionID string `json:"sessionId,omitempty"`
	Event     string `json:"event,omitempty"`
	Message   string `json:"message,omitempty"`
	UpdatedAt int64  `json:"updatedAt"`
}

type AgentStatusEventInfo struct {
	State     string `json:"state"`
	Source    string `json:"source"`
	SessionID string `json:"sessionId,omitempty"`
	Event     string `json:"event,omitempty"`
	Message   string `json:"message,omitempty"`
	UpdatedAt int64  `json:"updatedAt"`
}

type AgentStatusSnapshot struct {
	Agents       map[string]AgentStatusInfo `json:"agents"`
	Events       []AgentStatusEventInfo     `json:"events"`
	Aggregate    string                     `json:"aggregate"`
	AggregateSrc string                     `json:"aggregateSource,omitempty"`
	UpdatedAt    int64                      `json:"updatedAt"`
	Available    bool                       `json:"available"`
}

func parseAgentStatus(value string) agentStatus {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "working", "running", "in_progress", "in-progress", "thinking", "processing":
		return agentWorking
	case "done", "complete", "completed", "success", "finished":
		return agentDone
	case "error", "failed", "failure":
		return agentError
	case "question", "needs_input", "needs-input", "waiting", "approval", "input":
		return agentQuestion
	default:
		return agentIdle
	}
}

func defaultAgentStatusPath() string {
	if configured := strings.TrimSpace(os.Getenv("MINI_TV_AGENT_STATUS_FILE")); configured != "" {
		return configured
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".config", "mini-tv", "agent-status.json")
	}
	return filepath.Join(os.TempDir(), "mini-tv-agent-status.json")
}

func normalizeAgentDocument(document agentStatusDocument) agentStatusDocument {
	if document.Agents == nil {
		document.Agents = make(map[string]agentStatusEntry)
	}
	if document.State != "" || document.Source != "" {
		source := strings.ToLower(strings.TrimSpace(document.Source))
		if source == "" {
			source = "agent"
		}
		document.Agents[source] = agentStatusEntry{State: document.State, Source: source, SessionID: document.SessionID,
			Event: document.Event, Message: document.Message, UpdatedAt: document.UpdatedAt}
	}
	return document
}

func aggregateAgentStatus(agents map[string]AgentStatusInfo) (string, string) {
	priority := []agentStatus{agentError, agentQuestion, agentWorking, agentDone, agentIdle}
	for _, wanted := range priority {
		for source, info := range agents {
			if parseAgentStatus(info.State) == wanted {
				return wanted.String(), source
			}
		}
	}
	return agentIdle.String(), ""
}

func readAgentStatusSnapshot(path string) (AgentStatusSnapshot, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return AgentStatusSnapshot{Agents: map[string]AgentStatusInfo{}, Events: []AgentStatusEventInfo{}, Aggregate: "idle"}, err
	}
	var document agentStatusDocument
	if err := json.Unmarshal(data, &document); err != nil {
		return AgentStatusSnapshot{Agents: map[string]AgentStatusInfo{}, Events: []AgentStatusEventInfo{}, Aggregate: "idle"}, err
	}
	document = normalizeAgentDocument(document)
	agents := make(map[string]AgentStatusInfo, len(document.Agents))
	for source, entry := range document.Agents {
		if source == "" {
			continue
		}
		agents[source] = AgentStatusInfo{State: parseAgentStatus(entry.State).String(), Source: source,
			SessionID: entry.SessionID, Event: entry.Event, Message: entry.Message, UpdatedAt: entry.UpdatedAt}
	}
	events := make([]AgentStatusEventInfo, 0, len(document.Events))
	for _, event := range document.Events {
		events = append(events, AgentStatusEventInfo{State: parseAgentStatus(event.State).String(), Source: event.Source,
			SessionID: event.SessionID, Event: event.Event, Message: event.Message, UpdatedAt: event.UpdatedAt})
	}
	if len(events) > 24 {
		events = events[len(events)-24:]
	}
	aggregate, source := aggregateAgentStatus(agents)
	return AgentStatusSnapshot{Agents: agents, Events: events, Aggregate: aggregate, AggregateSrc: source,
		UpdatedAt: document.UpdatedAt, Available: true}, nil
}

type agentStatusTracker struct {
	path      string
	lastCheck time.Time
	modTime   time.Time
	status    agentStatus
	changed   bool
	mu        sync.Mutex
}

func newAgentStatusTracker() agentStatusTracker {
	return agentStatusTracker{path: defaultAgentStatusPath(), status: agentIdle, changed: true}
}

func (tracker *agentStatusTracker) poll(now time.Time) (agentStatus, bool) {
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	if !tracker.lastCheck.IsZero() && now.Sub(tracker.lastCheck) < 200*time.Millisecond {
		changed := tracker.changed
		tracker.changed = false
		return tracker.status, changed
	}
	tracker.lastCheck = now
	snapshot, err := readAgentStatusSnapshot(tracker.path)
	if err != nil {
		if tracker.status != agentIdle {
			tracker.status, tracker.changed = agentIdle, true
		}
		changed := tracker.changed
		tracker.changed = false
		return tracker.status, changed
	}
	parsed := parseAgentStatus(snapshot.Aggregate)
	if parsed != tracker.status {
		tracker.status, tracker.changed = parsed, true
	}
	changed := tracker.changed
	tracker.changed = false
	return tracker.status, changed
}
