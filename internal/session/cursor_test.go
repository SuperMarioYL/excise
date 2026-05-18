package session

import (
	"testing"
)

const fixtureCursor = "../../testdata/cursor_session_simple.jsonl"

// m2 — Cursor JSONL fixture loads and the role/type mapping is correct.
func TestCursorLoaderFixture(t *testing.T) {
	l := &CursorLoader{}
	if !l.Detect(fixtureCursor) {
		t.Fatalf("Detect(%s) = false", fixtureCursor)
	}
	s, err := l.Load(fixtureCursor)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if s.Tool != ToolCursor {
		t.Errorf("Tool = %v, want cursor", s.Tool)
	}
	if got, want := len(s.Turns), 5; got != want {
		t.Fatalf("turns = %d, want %d", got, want)
	}
	if s.Turns[0].Role != RoleUser {
		t.Errorf("turn 0 role = %v, want user", s.Turns[0].Role)
	}
	if s.Turns[1].Role != RoleAssistant {
		t.Errorf("turn 1 role = %v, want assistant", s.Turns[1].Role)
	}
	if s.Turns[2].Role != RoleTool {
		t.Errorf("turn 2 role = %v, want tool", s.Turns[2].Role)
	}
}

// m2 — auto-loader picks Cursor when given the cursor fixture.
func TestAutoLoaderPicksCursor(t *testing.T) {
	s, err := LoadAuto(fixtureCursor)
	if err != nil {
		t.Fatalf("LoadAuto: %v", err)
	}
	if s.Tool != ToolCursor {
		t.Errorf("Tool = %v, want cursor", s.Tool)
	}
}

// m2 — auto-loader picks Claude when given a Claude fixture.
func TestAutoLoaderPicksClaude(t *testing.T) {
	s, err := LoadAuto(fixtureSimple)
	if err != nil {
		t.Fatalf("LoadAuto: %v", err)
	}
	if s.Tool != ToolClaude {
		t.Errorf("Tool = %v, want claude", s.Tool)
	}
}
