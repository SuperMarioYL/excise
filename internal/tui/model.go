// Package tui implements the bubbletea interactive picker for Excise.
//
// The picker is intentionally minimal: list of turns, j/k to move,
// space to mark, x as a synonym for mark, d to delete-immediate, enter to
// commit, q to abort. The header shows live token-delta as the user marks.
//
// To keep the binary small and the build hermetic for v0.1, we depend on a
// thin internal mock of bubbletea-style events when the bubbletea library
// is not vendored. The README's polished demo uses real bubbletea (added in
// `go get` step at install time); the unit tests exercise the pure-Go state
// machine in model.go without spinning up a terminal.
package tui

import (
	"fmt"
	"strings"

	"github.com/SuperMarioYL/excise/internal/session"
)

// Model is the picker's pure state. It is exported so the CLI and the tests
// can construct it directly.
type Model struct {
	Turns     []session.Turn
	Cursor    int
	Marked    map[string]bool // turn IDs the user has marked for excision
	PreMarked map[string]bool // v0.2: turn IDs pre-marked by the suggestion engine
	Width     int
	Height    int
	Quit      bool
	Commit    bool
	Aborted   bool
}

// NewModel builds a picker over the given session.
func NewModel(s *session.Session) *Model {
	return &Model{
		Turns:     s.Turns,
		Marked:    map[string]bool{},
		PreMarked: map[string]bool{},
	}
}

// NewModelWithPreMarked builds a picker and pre-populates the marked set
// (and the PreMarked indicator set) from the supplied turn ids. v0.2 entry
// point used when `excise pick` calls the heuristic engine.
func NewModelWithPreMarked(s *session.Session, preMarked []string) *Model {
	m := NewModel(s)
	for _, id := range preMarked {
		if id == "" {
			continue
		}
		m.PreMarked[id] = true
		m.Marked[id] = true
	}
	return m
}

// MoveDown advances the cursor by one (clamped).
func (m *Model) MoveDown() {
	if m.Cursor < len(m.Turns)-1 {
		m.Cursor++
	}
}

// MoveUp moves the cursor up by one (clamped).
func (m *Model) MoveUp() {
	if m.Cursor > 0 {
		m.Cursor--
	}
}

// ToggleMark flips the mark on the current turn.
func (m *Model) ToggleMark() {
	if len(m.Turns) == 0 {
		return
	}
	id := m.Turns[m.Cursor].ID
	if m.Marked[id] {
		delete(m.Marked, id)
	} else {
		m.Marked[id] = true
	}
}

// MarkedSet returns a copy of the marked-id set, useful for the commit phase.
func (m *Model) MarkedSet() map[string]bool {
	out := make(map[string]bool, len(m.Marked))
	for k, v := range m.Marked {
		if v {
			out[k] = true
		}
	}
	return out
}

// TokensBefore returns the token estimate of every turn.
func (m *Model) TokensBefore() int {
	sum := 0
	for _, t := range m.Turns {
		sum += t.TokenEst
	}
	return sum
}

// TokensAfter returns the token estimate of every UNMARKED turn — i.e. what
// the session would weigh in at after commit.
func (m *Model) TokensAfter() int {
	g := session.BuildGraph(m.Turns)
	closure := g.Closure(m.Turns, m.MarkedSet())
	sum := 0
	for _, t := range m.Turns {
		if closure[t.ID] {
			continue
		}
		sum += t.TokenEst
	}
	return sum
}

// SurvivingCount mirrors TokensAfter for turn count.
func (m *Model) SurvivingCount() int {
	g := session.BuildGraph(m.Turns)
	closure := g.Closure(m.Turns, m.MarkedSet())
	n := 0
	for _, t := range m.Turns {
		if !closure[t.ID] {
			n++
		}
	}
	return n
}

// Header renders the live "turns: 42→39, tokens: ~18k→~12k" line.
func (m *Model) Header() string {
	beforeT := len(m.Turns)
	afterT := m.SurvivingCount()
	beforeTok := m.TokensBefore()
	afterTok := m.TokensAfter()
	return fmt.Sprintf("turns: %d → %d   tokens: ~%s → ~%s   [j/k] move  [space/x] mark  [enter] commit  [q] abort",
		beforeT, afterT, humanK(beforeTok), humanK(afterTok))
}

// RenderList returns a plain-text render of the visible window. The TUI
// layer wraps this with lipgloss styles; tests use it directly.
//
// In v0.2 a turn that came in pre-marked by the suggestion engine renders
// with a ◆ glyph instead of the usual x — so the user can spot which marks
// originated from the heuristics vs. their own clicks. Once the user
// toggles a pre-marked turn off (or toggles a different turn on), only the
// Marked map is consulted for the commit; PreMarked is a render-only hint.
func (m *Model) RenderList() string {
	var b strings.Builder
	hasPreMarked := false
	for i, t := range m.Turns {
		mark := " "
		if m.Marked[t.ID] {
			if m.PreMarked[t.ID] {
				mark = "◆"
				hasPreMarked = true
			} else {
				mark = "x"
			}
		}
		cursor := " "
		if i == m.Cursor {
			cursor = ">"
		}
		role := string(t.Role)
		preview := t.Preview
		if preview == "" {
			preview = "(empty)"
		}
		fmt.Fprintf(&b, "%s [%s] #%03d %-9s ~%4dt  %s\n", cursor, mark, i+1, role, t.TokenEst, truncate(preview, 80))
	}
	if hasPreMarked {
		fmt.Fprintln(&b, "◆ suggested — press space to uncheck")
	}
	return b.String()
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n < 3 {
		return s[:n]
	}
	return s[:n-1] + "…"
}

func humanK(n int) string {
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	return fmt.Sprintf("%.1fk", float64(n)/1000.0)
}
