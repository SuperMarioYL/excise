package session

import (
	"os"
	"path/filepath"
	"testing"
)

// m1 invariant 1 — cutting a tool_use turn pulls in its tool_result turn.
func TestExciseClosurePullsToolResult(t *testing.T) {
	s, err := (&ClaudeLoader{}).Load(fixtureTools)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// a-100 owns toolu_01ABC; u-101 contains its result.
	seeds := map[string]bool{"a-100": true}
	out := Excise(s.Turns, seeds)
	for _, t2 := range out {
		if t2.ID == "u-101" {
			t.Errorf("u-101 (orphaned tool_result) survived excision of a-100")
		}
	}
	if got, want := len(out), len(s.Turns)-2; got != want {
		t.Errorf("after excise turns = %d, want %d", got, want)
	}
}

// m3 — Verify() flags orphaned tool_result turns (invariant 2).
func TestVerifyOrphanedToolResultWarns(t *testing.T) {
	s, _ := (&ClaudeLoader{}).Load(fixtureTools)
	g := BuildGraph(s.Turns)
	// Cut only the tool_result u-101 (NOT its owner a-100) → should warn.
	warns := g.Verify(s.Turns, map[string]bool{"u-101": true})
	if len(warns) == 0 {
		t.Fatalf("expected ≥1 warning, got 0")
	}
}

// m1 round-trip after excision: the resulting file re-parses cleanly.
func TestExciseRoundTripReparse(t *testing.T) {
	src, _ := os.ReadFile(fixtureTools)
	tmp := filepath.Join(t.TempDir(), "tools.jsonl")
	if err := os.WriteFile(tmp, src, 0o644); err != nil {
		t.Fatal(err)
	}
	s, _ := (&ClaudeLoader{}).Load(tmp)
	seeds := map[string]bool{"a-102": true} // pulls u-103 too
	s.Turns = Excise(s.Turns, seeds)
	if err := (&ClaudeWriter{}).Write(s); err != nil {
		t.Fatalf("Write: %v", err)
	}
	s2, err := (&ClaudeLoader{}).Load(tmp)
	if err != nil {
		t.Fatalf("Reload: %v", err)
	}
	for _, t2 := range s2.Turns {
		if t2.ID == "a-102" || t2.ID == "u-103" {
			t.Errorf("expected %s to be gone after round-trip", t2.ID)
		}
	}
}
