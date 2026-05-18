package tui

import (
	"fmt"
	"strings"

	"github.com/SuperMarioYL/excise/internal/session"
)

// DiffSummary renders a before/after summary suitable for the
// confirm-before-commit prompt and the dry-run output.
func DiffSummary(s *session.Session, toCut map[string]bool) string {
	g := session.BuildGraph(s.Turns)
	closure := g.Closure(s.Turns, toCut)
	warns := g.Verify(s.Turns, toCut)

	var b strings.Builder
	fmt.Fprintf(&b, "Will remove %d turn(s) (including %d pulled in by tool-call dependency):\n",
		len(closure), len(closure)-len(toCut))
	for _, t := range s.Turns {
		if !closure[t.ID] {
			continue
		}
		seed := ""
		if !toCut[t.ID] {
			seed = " (dep)"
		}
		fmt.Fprintf(&b, "  - %s  [%s] ~%dt  %s%s\n", short(t.ID), t.Role, t.TokenEst, truncate(t.Preview, 60), seed)
	}
	if len(warns) > 0 {
		fmt.Fprintln(&b, "\nWarnings (dependency-aware):")
		for _, w := range warns {
			fmt.Fprintf(&b, "  ! %s: %s\n", short(w.TurnID), w.Reason)
		}
	}
	return b.String()
}

func short(id string) string {
	if len(id) <= 8 {
		return id
	}
	return id[:8]
}
