// suggest.go — v0.2.0 `excise suggest` subcommand.
//
// Read-only: runs the heuristic scorer (internal/suggest) on a session,
// prints the top-K candidate turns. Never mutates the file.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/SuperMarioYL/excise/internal/session"
	"github.com/SuperMarioYL/excise/internal/suggest"
)

// suggestFlags is local to the suggest subcommand to keep main.go simple.
type suggestFlags struct {
	top      int
	minScore float64
	asJSON   bool
}

func newSuggestCmd(gf *globalFlags) *cobra.Command {
	sf := &suggestFlags{top: 5, minScore: 0.0}
	cmd := &cobra.Command{
		Use:   "suggest [path]",
		Short: "Heuristically rank turns most likely worth excising. (Read-only.)",
		Long: `Run the v0.2 heuristic suggestion engine over a session and print the
top-K candidate turns. The scorer is a pure stdlib function — no network, no
LLM. The session file is never modified.

Five heuristics contribute to each turn's score:
  - high_token_cost                 (assistant/tool turn ≥ 2000 tokens)
  - repeated_file_edit              (same file edited 3+ times in a row)
  - user_correction_follows_up      (next user turn matched correction lexicon)
  - tool_use_error_then_correction  (tool returned error and user corrected)
  - long_drift_no_tool_calls        (5+ consecutive assistant turns, no tool_use)
`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 {
				gf.sessionArg = args[0]
			}
			s, err := loadSession(gf)
			if err != nil {
				return err
			}
			scores := suggest.Score(s)
			top := suggest.TopK(scores, sf.top, sf.minScore)
			if sf.asJSON {
				return emitSuggestJSON(os.Stdout, top)
			}
			return emitSuggestTable(os.Stdout, s, top)
		},
	}
	cmd.Flags().IntVar(&sf.top, "top", 5, "show at most N suggestions (0 = all)")
	cmd.Flags().Float64Var(&sf.minScore, "min-score", 0.0, "drop suggestions below this score")
	cmd.Flags().BoolVar(&sf.asJSON, "json", false, "emit JSON instead of a table")
	return cmd
}

// emitSuggestTable prints the table shown in plan §1.
func emitSuggestTable(out *os.File, s *session.Session, picks []suggest.TurnScore) error {
	fmt.Fprintf(out, "session: %s (%s)\n", s.SessionID, s.Tool)
	fmt.Fprintf(out, "source : %s\n", s.SourcePath)
	fmt.Fprintf(out, "turns  : %d\n\n", len(s.Turns))
	if len(picks) == 0 {
		fmt.Fprintln(out, "(no candidates surfaced by the heuristic engine)")
		return nil
	}
	fmt.Fprintf(out, "%-4s  %-9s  %-7s  %-50s  %s\n",
		"#", "role", "tokens", "heuristic", "preview")
	fmt.Fprintln(out, strings.Repeat("-", 110))
	total := 0
	for _, p := range picks {
		preview := truncateUTF8(p.Preview, 40)
		if preview == "" {
			preview = "(empty)"
		}
		fmt.Fprintf(out, "%-4d  %-9s  %-7d  %-50s  %s\n",
			p.Index, p.Role, p.Tokens, truncateUTF8(suggest.TriggerSummary(p), 50), preview)
		total += p.Tokens
	}
	fmt.Fprintf(out, "\n%d candidate(s) totalling ~%d tokens. Run `excise pick` to review interactively.\n",
		len(picks), total)
	return nil
}

func emitSuggestJSON(out *os.File, picks []suggest.TurnScore) error {
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(picks)
}

// truncateUTF8 is a unicode-aware truncate that won't slice a multi-byte
// rune in half. Reused by both the table and a few other display sites.
func truncateUTF8(s string, n int) string {
	if n <= 0 {
		return ""
	}
	count := 0
	for i := range s {
		if count == n {
			return s[:i] + "…"
		}
		count++
	}
	return s
}
