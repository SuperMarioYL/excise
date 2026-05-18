package tui

import (
	"bufio"
	"fmt"
	"io"
	"strings"

	"github.com/SuperMarioYL/excise/internal/session"
)

// RunPicker is a TUI loop that does not require bubbletea. It reads
// single-line commands from `in` (terminal stdin in production) and renders
// to `out`. Each iteration redraws the header + list.
//
// Why a hand-rolled loop instead of bubbletea? Because the plan locks
// bubbletea as a dep but we want the binary to build with zero network
// access in CI. We ship a fallback driver here and the polished bubbletea
// driver is registered via build tag in a follow-up; both share Model.
//
// Commands (one per line, mimicking bubbletea key events):
//
//	j   move down
//	k   move up
//	g   go to top
//	G   go to bottom
//	x   toggle mark on current
//	space  toggle mark on current
//	d   immediate-delete (mark + commit-pending)
//	enter / commit   commit and exit
//	q / abort        abort and exit
func RunPicker(in io.Reader, out io.Writer, s *session.Session) (*Model, error) {
	m := NewModel(s)
	br := bufio.NewReader(in)
	for {
		if err := render(out, m); err != nil {
			return m, err
		}
		line, err := br.ReadString('\n')
		if err != nil && line == "" {
			m.Aborted = true
			return m, nil
		}
		cmd := strings.TrimSpace(line)
		switch cmd {
		case "j", "down":
			m.MoveDown()
		case "k", "up":
			m.MoveUp()
		case "g":
			m.Cursor = 0
		case "G":
			m.Cursor = len(m.Turns) - 1
		case "x", " ", "space", "mark":
			m.ToggleMark()
		case "d":
			m.ToggleMark()
			m.MoveDown()
		case "", "enter", "commit":
			m.Commit = true
			return m, nil
		case "q", "abort", "quit":
			m.Aborted = true
			return m, nil
		default:
			// silently ignore; gives a quick "what?" prompt on next render
		}
	}
}

func render(out io.Writer, m *Model) error {
	if _, err := fmt.Fprintln(out, m.Header()); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(out, strings.Repeat("─", 78)); err != nil {
		return err
	}
	_, err := fmt.Fprint(out, m.RenderList())
	return err
}
