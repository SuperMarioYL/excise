package tui

import (
	"strings"
	"testing"

	"github.com/SuperMarioYL/excise/internal/session"
)

func TestRenderLLMSidebarEmpty(t *testing.T) {
	m := NewModel(&session.Session{Turns: []session.Turn{{ID: "t1"}}})
	if got := renderLLMSidebar(m); got != "" {
		t.Errorf("no reasons → empty sidebar; got %q", got)
	}
}

func TestRenderLLMSidebarShowsReasonForCursor(t *testing.T) {
	s := &session.Session{Turns: []session.Turn{
		{ID: "t1"}, {ID: "t2"}, {ID: "t3"},
	}}
	m := NewModelWithReasons(s, []string{"t2"}, map[string]string{
		"t2": "kept regenerating the same buggy hunk",
	})
	m.Cursor = 1 // sits on t2
	out := renderLLMSidebar(m)
	if !strings.Contains(out, "buggy hunk") {
		t.Errorf("expected sidebar to render reason for cursor turn; got %q", out)
	}

	m.Cursor = 0
	if renderLLMSidebar(m) != "" {
		t.Errorf("cursor not on a reason'd turn → empty sidebar")
	}
}

func TestNewModelWithReasonsBackwardCompatible(t *testing.T) {
	s := &session.Session{Turns: []session.Turn{{ID: "t1"}}}
	m1 := NewModelWithPreMarked(s, []string{"t1"})
	m2 := NewModelWithReasons(s, []string{"t1"}, nil)
	if len(m1.PreMarked) != len(m2.PreMarked) || len(m1.LLMReasons) != 0 || len(m2.LLMReasons) != 0 {
		t.Errorf("backward-compat broken: %+v vs %+v", m1, m2)
	}
}
