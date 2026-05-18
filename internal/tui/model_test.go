package tui

import (
	"strings"
	"testing"

	"github.com/SuperMarioYL/excise/internal/session"
)

func fakeSession() *session.Session {
	return &session.Session{
		Tool: session.ToolClaude,
		Turns: []session.Turn{
			{ID: "u-1", Role: session.RoleUser, TokenEst: 10, Preview: "hi"},
			{ID: "a-1", Role: session.RoleAssistant, TokenEst: 100, Preview: "hello there friend"},
			{ID: "u-2", Role: session.RoleUser, TokenEst: 5, Preview: "thanks"},
		},
	}
}

func TestModelMoveAndMark(t *testing.T) {
	m := NewModel(fakeSession())
	if m.Cursor != 0 {
		t.Fatalf("cursor = %d, want 0", m.Cursor)
	}
	m.MoveDown()
	m.MoveDown()
	m.MoveDown() // clamp
	if m.Cursor != 2 {
		t.Errorf("cursor = %d, want 2", m.Cursor)
	}
	m.ToggleMark()
	if !m.Marked["u-2"] {
		t.Errorf("expected u-2 marked")
	}
	m.ToggleMark()
	if m.Marked["u-2"] {
		t.Errorf("expected u-2 unmarked")
	}
}

func TestModelTokensAfter(t *testing.T) {
	m := NewModel(fakeSession())
	m.Cursor = 1
	m.ToggleMark() // mark a-1 (100 tokens)
	if got, want := m.TokensBefore(), 115; got != want {
		t.Errorf("before = %d, want %d", got, want)
	}
	if got, want := m.TokensAfter(), 15; got != want {
		t.Errorf("after = %d, want %d", got, want)
	}
}

func TestRenderListHasCursorAndMark(t *testing.T) {
	m := NewModel(fakeSession())
	m.Cursor = 1
	m.ToggleMark()
	out := m.RenderList()
	if !strings.Contains(out, "> [x]") {
		t.Errorf("expected cursor+mark in render, got:\n%s", out)
	}
}
