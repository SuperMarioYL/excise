package session

import (
	"os"
	"path/filepath"
	"testing"
)

const fixtureSimple = "../../testdata/claude_session_simple.jsonl"
const fixtureTools = "../../testdata/claude_session_with_tools.jsonl"

// m1 — loader smoke test against testdata/claude_session_simple.jsonl.
func TestClaudeLoaderSimple(t *testing.T) {
	l := &ClaudeLoader{}
	if !l.Detect(fixtureSimple) {
		t.Fatalf("Detect(%s) = false", fixtureSimple)
	}
	s, err := l.Load(fixtureSimple)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got, want := len(s.Turns), 5; got != want {
		t.Fatalf("turns = %d, want %d", got, want)
	}
	if s.Tool != ToolClaude {
		t.Errorf("Tool = %v, want claude", s.Tool)
	}
	if s.SessionID == "" {
		t.Errorf("empty SessionID")
	}
	// Spot-check role mapping
	if s.Turns[0].Role != RoleUser {
		t.Errorf("turn 0 role = %v, want user", s.Turns[0].Role)
	}
	if s.Turns[1].Role != RoleAssistant {
		t.Errorf("turn 1 role = %v, want assistant", s.Turns[1].Role)
	}
}

// m1 — loader correctly indexes tool_use ↔ tool_result blocks.
func TestClaudeLoaderToolPairs(t *testing.T) {
	s, err := (&ClaudeLoader{}).Load(fixtureTools)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(s.Turns) != 8 {
		t.Fatalf("turns = %d, want 8", len(s.Turns))
	}
	// turn a-100 should expose one tool_use; u-101 should expose its result
	calls := s.Turns[1].ToolCalls
	if len(calls) != 1 || calls[0].ID != "toolu_01ABC" || calls[0].Name != "Bash" {
		t.Errorf("a-100 tool calls = %#v", calls)
	}
	results := s.Turns[2].ToolResults
	if len(results) != 1 || results[0].ToolUseID != "toolu_01ABC" {
		t.Errorf("u-101 tool results = %#v", results)
	}
}

// m1 — round-trip: load → write → reload yields the same Turn count.
func TestClaudeRoundTrip(t *testing.T) {
	src, err := os.ReadFile(fixtureSimple)
	if err != nil {
		t.Fatal(err)
	}
	tmp := filepath.Join(t.TempDir(), "rt.jsonl")
	if err := os.WriteFile(tmp, src, 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := (&ClaudeLoader{}).Load(tmp)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := len(s.Turns)
	if err := (&ClaudeWriter{}).Write(s); err != nil {
		t.Fatalf("Write: %v", err)
	}
	s2, err := (&ClaudeLoader{}).Load(tmp)
	if err != nil {
		t.Fatalf("Reload: %v", err)
	}
	if got := len(s2.Turns); got != want {
		t.Errorf("round-trip turns = %d, want %d", got, want)
	}
}
