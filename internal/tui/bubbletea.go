package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/SuperMarioYL/excise/internal/session"
)

// teaModel adapts Model to bubbletea.Model so we can drive a real terminal
// UI without giving up the pure state machine used in tests.
type teaModel struct {
	M *Model
}

var (
	styleHeader      = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("212"))
	styleSep         = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	styleCursor      = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("33"))
	styleMarked      = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	stylePreMarked   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214")) // v0.2: amber
	styleSuggestHint = lipgloss.NewStyle().Italic(true).Foreground(lipgloss.Color("214"))
	styleRoleU       = lipgloss.NewStyle().Foreground(lipgloss.Color("39"))
	styleRoleA       = lipgloss.NewStyle().Foreground(lipgloss.Color("46"))
	styleRoleT       = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	styleRoleS       = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	styleHelp        = lipgloss.NewStyle().Italic(true).Foreground(lipgloss.Color("241"))
	styleLLMReason   = lipgloss.NewStyle().Italic(true).Foreground(lipgloss.Color("147")) // v0.3: pale lavender
	styleLLMPanel    = lipgloss.NewStyle().
				BorderStyle(lipgloss.NormalBorder()).
				BorderForeground(lipgloss.Color("147")).
				Padding(0, 1).
				MarginLeft(2)
)

func (tm teaModel) Init() tea.Cmd { return nil }

func (tm teaModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	m := tm.M
	switch ev := msg.(type) {
	case tea.WindowSizeMsg:
		m.Width = ev.Width
		m.Height = ev.Height
	case tea.KeyMsg:
		switch ev.String() {
		case "ctrl+c", "q":
			m.Aborted = true
			return tm, tea.Quit
		case "j", "down":
			m.MoveDown()
		case "k", "up":
			m.MoveUp()
		case "g":
			m.Cursor = 0
		case "G":
			m.Cursor = len(m.Turns) - 1
		case " ", "x":
			m.ToggleMark()
		case "d":
			m.ToggleMark()
			m.MoveDown()
		case "enter":
			m.Commit = true
			return tm, tea.Quit
		case "?":
			// Help is always visible in the footer; nothing to do.
		}
	}
	return tm, nil
}

func (tm teaModel) View() string {
	m := tm.M
	var b strings.Builder
	b.WriteString(styleHeader.Render(m.Header()))
	b.WriteString("\n")
	b.WriteString(styleSep.Render(strings.Repeat("─", min(78, max(20, m.Width-1)))))
	b.WriteString("\n")

	// Show a viewport window around the cursor so very large sessions don't
	// flood the terminal.
	window := 20
	if m.Height > 10 {
		window = m.Height - 6
	}
	start := 0
	if m.Cursor >= window {
		start = m.Cursor - window + 1
	}
	end := start + window
	if end > len(m.Turns) {
		end = len(m.Turns)
	}

	for i := start; i < end; i++ {
		t := m.Turns[i]
		cursor := "  "
		if i == m.Cursor {
			cursor = styleCursor.Render("❯ ")
		}
		mark := "[ ]"
		if m.Marked[t.ID] {
			if m.PreMarked[t.ID] {
				mark = stylePreMarked.Render("[◆]")
			} else {
				mark = styleMarked.Render("[x]")
			}
		}
		role := renderRole(t.Role)
		preview := t.Preview
		if preview == "" {
			preview = "(empty)"
		}
		fmt.Fprintf(&b, "%s%s #%03d %s ~%4dt  %s\n", cursor, mark, i+1, role, t.TokenEst, truncate(preview, 70))
	}

	if hasAnyPreMarked(m) {
		b.WriteString("\n")
		b.WriteString(styleSuggestHint.Render("◆ suggested — press space to uncheck"))
	}
	// v0.3 sidebar: if the cursor sits on a turn that the LLM rerank
	// annotated, show the one-line reason. Stays out of the way when no
	// --llm was passed (LLMReasons is empty).
	if panel := renderLLMSidebar(m); panel != "" {
		b.WriteString("\n")
		b.WriteString(panel)
	}
	b.WriteString("\n")
	b.WriteString(styleHelp.Render("[j/k] move  [space/x] mark  [d] mark+next  [g/G] top/bot  [enter] commit  [q] abort"))
	b.WriteString("\n")
	return b.String()
}

// renderLLMSidebar returns the boxed rationale for whichever turn the
// cursor is on. Empty string means "render nothing" — caller is expected
// to skip the surrounding separator in that case.
func renderLLMSidebar(m *Model) string {
	if len(m.LLMReasons) == 0 || len(m.Turns) == 0 {
		return ""
	}
	id := m.Turns[m.Cursor].ID
	reason, ok := m.LLMReasons[id]
	if !ok || reason == "" {
		return ""
	}
	body := styleLLMReason.Render("LLM: " + reason)
	return styleLLMPanel.Render(body)
}

func hasAnyPreMarked(m *Model) bool {
	for id := range m.PreMarked {
		if m.Marked[id] {
			return true
		}
	}
	return false
}

func renderRole(r session.Role) string {
	switch r {
	case session.RoleUser:
		return styleRoleU.Render("user     ")
	case session.RoleAssistant:
		return styleRoleA.Render("assistant")
	case session.RoleTool:
		return styleRoleT.Render("tool     ")
	default:
		return styleRoleS.Render(string(r) + strings.Repeat(" ", max(0, 9-len(string(r)))))
	}
}

// RunBubbletea drives the picker on a real terminal. Returns the model
// (with Commit / Aborted flags set) so the CLI can act on the user's
// choice.
func RunBubbletea(s *session.Session) (*Model, error) {
	return RunBubbleteaWithPreMarked(s, nil)
}

// RunBubbleteaWithPreMarked is the v0.2 entry point — same as RunBubbletea
// but seeds the picker's marked set from the supplied turn ids.
func RunBubbleteaWithPreMarked(s *session.Session, preMarked []string) (*Model, error) {
	return RunBubbleteaWithReasons(s, preMarked, nil)
}

// RunBubbleteaWithReasons is the v0.3 entry point. It additionally renders
// a per-turn LLM rationale in a side panel when the cursor is on a turn
// that the rerank annotated. Passing reasons=nil is identical to the v0.2
// behavior.
func RunBubbleteaWithReasons(s *session.Session, preMarked []string, reasons map[string]string) (*Model, error) {
	m := NewModelWithReasons(s, preMarked, reasons)
	p := tea.NewProgram(teaModel{M: m})
	if _, err := p.Run(); err != nil {
		return nil, err
	}
	return m, nil
}

func min(a, b int) int { if a < b { return a }; return b }
func max(a, b int) int { if a > b { return a }; return b }
