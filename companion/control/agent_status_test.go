package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAggregateAgentStatusUsesErrorQuestionWorkingPriority(t *testing.T) {
	agents := map[string]AgentStatusInfo{
		"codex":  {State: "working", Source: "codex"},
		"claude": {State: "question", Source: "claude"},
	}
	state, source := aggregateAgentStatus(agents)
	if state != "question" || source != "claude" {
		t.Fatalf("question should outrank working: %s/%s", state, source)
	}
	agents["codex"] = AgentStatusInfo{State: "error", Source: "codex"}
	state, source = aggregateAgentStatus(agents)
	if state != "error" || source != "codex" {
		t.Fatalf("error should outrank question: %s/%s", state, source)
	}
}

func TestReadAgentStatusSnapshotSupportsMultipleSources(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent-status.json")
	data := []byte(`{"agents":{"codex":{"state":"done","source":"codex","updatedAt":10},"claude":{"state":"working","source":"claude","updatedAt":11}},"events":[{"state":"done","source":"codex","event":"Stop","updatedAt":10}],"updatedAt":11}`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	snapshot, err := readAgentStatusSnapshot(path)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Aggregate != "working" || snapshot.AggregateSrc != "claude" || len(snapshot.Agents) != 2 || len(snapshot.Events) != 1 {
		t.Fatalf("unexpected snapshot: %+v", snapshot)
	}
}
