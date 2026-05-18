// Command excise — surgical context editing for coding-agent transcripts.
//
// Usage examples:
//
//	excise                       # auto-discover newest Claude session, open TUI
//	excise list                  # render the turn table (no edits)
//	excise pick <path>           # open the TUI on a specific session
//	excise cut 5-7,9 <path>      # non-interactive cut by 1-based turn index
//	excise rollback --list
//	excise rollback <snapshot-id>
//
// See the README for the full primitive description and asciinema demo.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/SuperMarioYL/excise/internal/safety"
	"github.com/SuperMarioYL/excise/internal/session"
	"github.com/SuperMarioYL/excise/internal/suggest"
	"github.com/SuperMarioYL/excise/internal/tui"
)

var version = "0.2.0"

func main() {
	root := newRootCmd()
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "excise:", err)
		os.Exit(1)
	}
}

// flags shared by most commands
type globalFlags struct {
	toolFlag   string
	sessionArg string
	force      bool
	dryRun     bool
	yes        bool
	noSuggest  bool // v0.2: disable heuristic pre-mark in `pick`
}

func newRootCmd() *cobra.Command {
	var gf globalFlags

	root := &cobra.Command{
		Use:   "excise [path]",
		Short: "Surgical context editing for coding-agent transcripts",
		Long: `Excise surgically removes failed turns from a polluted coding-agent
session, instead of /clear-and-restart. Supports Claude Code (JSONL) and
Cursor (sqlite/vscdb) v0.1.

The primitive: Excise(Session, set<turn_id>) -> Session', preserving
ordering, stable ids, and atomic tool_use ↔ tool_result pairs.`,
		Version: version,
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 {
				gf.sessionArg = args[0]
			}
			return runPick(&gf)
		},
	}

	root.PersistentFlags().StringVar(&gf.toolFlag, "tool", "auto", "transcript format: auto|claude|cursor")
	root.PersistentFlags().StringVar(&gf.sessionArg, "session", "", "explicit path to a session file (overrides positional arg)")
	root.PersistentFlags().BoolVar(&gf.force, "force", false, "proceed even when dependency-aware warnings fire")
	root.PersistentFlags().BoolVar(&gf.dryRun, "dry-run", false, "print the diff but do not write")
	root.PersistentFlags().BoolVarP(&gf.yes, "yes", "y", false, "skip the confirmation prompt")
	root.PersistentFlags().BoolVar(&gf.noSuggest, "no-suggest", false, "skip the v0.2 heuristic pre-mark in the picker (restore v0.1 behavior)")

	root.AddCommand(newListCmd(&gf))
	root.AddCommand(newPickCmd(&gf))
	root.AddCommand(newCutCmd(&gf))
	root.AddCommand(newRollbackCmd(&gf))
	root.AddCommand(newSuggestCmd(&gf))

	return root
}

func newListCmd(gf *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "list [path]",
		Short: "Print the turn table for a session (no edits).",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 {
				gf.sessionArg = args[0]
			}
			s, err := loadSession(gf)
			if err != nil {
				return err
			}
			printList(s)
			return nil
		},
	}
}

func newPickCmd(gf *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "pick [path]",
		Short: "Open the interactive bubbletea picker.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 {
				gf.sessionArg = args[0]
			}
			return runPick(gf)
		},
	}
}

func newCutCmd(gf *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "cut <range> [path]",
		Short: "Non-interactive cut by 1-based turn range (e.g. 5-7,9).",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			rangeSpec := args[0]
			if len(args) == 2 {
				gf.sessionArg = args[1]
			}
			s, err := loadSession(gf)
			if err != nil {
				return err
			}
			idxs, err := parseRange(rangeSpec, len(s.Turns))
			if err != nil {
				return err
			}
			seeds := map[string]bool{}
			for _, i := range idxs {
				seeds[s.Turns[i-1].ID] = true
			}
			return commitExcise(s, seeds, gf, "cut "+rangeSpec)
		},
	}
}

func newRollbackCmd(gf *globalFlags) *cobra.Command {
	var listOnly bool
	var to string
	cmd := &cobra.Command{
		Use:   "rollback [snapshot-id]",
		Short: "List snapshots, or restore one.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if listOnly || len(args) == 0 {
				snaps, err := safety.ListSnapshots()
				if err != nil {
					return err
				}
				if len(snaps) == 0 {
					fmt.Println("(no snapshots)")
					return nil
				}
				for _, s := range snaps {
					fmt.Printf("%s  %s\n", s.CreatedAt.Format(time.RFC3339), s.ID)
				}
				return nil
			}
			id := args[0]
			if err := safety.Rollback(id, to); err != nil {
				return err
			}
			fmt.Printf("restored %s\n", id)
			return nil
		},
	}
	cmd.Flags().BoolVar(&listOnly, "list", false, "list snapshots and exit")
	cmd.Flags().StringVar(&to, "to", "", "destination path (defaults to original)")
	return cmd
}

// loadSession resolves the session path and dispatches to the right loader.
func loadSession(gf *globalFlags) (*session.Session, error) {
	path := gf.sessionArg
	if path == "" {
		p, err := session.DiscoverNewestClaude()
		if err != nil {
			return nil, fmt.Errorf("no session path given and auto-discovery failed: %w", err)
		}
		path = p
		fmt.Fprintln(os.Stderr, "excise: auto-discovered", path)
	}
	switch strings.ToLower(gf.toolFlag) {
	case "claude":
		return session.LoadWithTool(session.ToolClaude, path)
	case "cursor":
		return session.LoadWithTool(session.ToolCursor, path)
	default:
		return session.LoadAuto(path)
	}
}

func printList(s *session.Session) {
	fmt.Printf("session: %s (%s)\n", s.SessionID, s.Tool)
	fmt.Printf("source : %s\n", s.SourcePath)
	fmt.Printf("turns  : %d\n\n", len(s.Turns))
	fmt.Printf("%-4s  %-9s  %-6s  %-8s  %s\n", "#", "role", "tokens", "ts", "preview")
	fmt.Printf("%s\n", strings.Repeat("─", 78))
	for i, t := range s.Turns {
		ts := ""
		if !t.Timestamp.IsZero() {
			ts = t.Timestamp.Format("15:04:05")
		}
		preview := t.Preview
		if preview == "" {
			preview = "(empty)"
		}
		if len(preview) > 50 {
			preview = preview[:49] + "…"
		}
		fmt.Printf("%-4d  %-9s  %-6d  %-8s  %s\n", i+1, t.Role, t.TokenEst, ts, preview)
	}
}

func runPick(gf *globalFlags) error {
	s, err := loadSession(gf)
	if err != nil {
		return err
	}
	if len(s.Turns) == 0 {
		fmt.Fprintln(os.Stderr, "excise: session has no turns")
		return nil
	}
	var preMarked []string
	if !gf.noSuggest {
		scores := suggest.Score(s)
		preMarked = suggest.TopKIDs(scores, 5, 0.0)
		if len(preMarked) > 0 {
			fmt.Fprintf(os.Stderr, "excise: %d turn(s) pre-marked by the suggestion engine (--no-suggest to disable)\n", len(preMarked))
		}
	}
	m, err := tui.RunBubbleteaWithPreMarked(s, preMarked)
	if err != nil {
		return err
	}
	if m.Aborted || !m.Commit {
		fmt.Fprintln(os.Stderr, "aborted")
		return nil
	}
	seeds := m.MarkedSet()
	if len(seeds) == 0 {
		fmt.Fprintln(os.Stderr, "no turns marked; nothing to do")
		return nil
	}
	return commitExcise(s, seeds, gf, "pick")
}

func commitExcise(s *session.Session, seeds map[string]bool, gf *globalFlags, source string) error {
	g := session.BuildGraph(s.Turns)
	warns := g.Verify(s.Turns, seeds)
	if len(warns) > 0 && !gf.force {
		fmt.Fprintln(os.Stderr, "excise: dependency warnings (re-run with --force to override):")
		for _, w := range warns {
			fmt.Fprintf(os.Stderr, "  ! %s: %s\n", w.TurnID, w.Reason)
		}
		return fmt.Errorf("aborted by dependency-aware check")
	}

	fmt.Print(tui.DiffSummary(s, seeds))

	if gf.dryRun {
		fmt.Fprintln(os.Stderr, "dry-run; no files written")
		return nil
	}

	if !gf.yes {
		fmt.Print("\nCommit? [y/N] ")
		var resp string
		_, _ = fmt.Scanln(&resp)
		if !strings.EqualFold(strings.TrimSpace(resp), "y") {
			fmt.Println("aborted")
			return nil
		}
	}

	snap, err := safety.BeforeWrite(s.SessionID, s.SourcePath)
	if err != nil {
		return fmt.Errorf("snapshot: %w", err)
	}

	s.Turns = session.Excise(s.Turns, seeds)

	w, err := session.WriterFor(s)
	if err != nil {
		return err
	}
	if err := w.Write(s); err != nil {
		return err
	}

	removed := make([]string, 0, len(seeds))
	for id := range seeds {
		removed = append(removed, id)
	}
	sort.Strings(removed)
	_ = safety.LogEdit(map[string]any{
		"ts":          time.Now().UTC().Format(time.RFC3339),
		"session_id":  s.SessionID,
		"source_path": s.SourcePath,
		"snapshot":    snap.ID,
		"removed_ids": removed,
		"command":     source,
		"tool":        string(s.Tool),
	})

	fmt.Fprintf(os.Stderr, "✔ committed. snapshot: %s\n", snap.ID)
	fmt.Fprintf(os.Stderr, "  rollback with: excise rollback %s\n", snap.ID)
	return nil
}

// parseRange parses "5-7,9,12-13" into [5,6,7,9,12,13], 1-based, validated
// against `count` (the number of turns).
func parseRange(spec string, count int) ([]int, error) {
	seen := map[int]bool{}
	var out []int
	parts := strings.Split(spec, ",")
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if strings.Contains(p, "-") {
			ab := strings.SplitN(p, "-", 2)
			a, err := strconv.Atoi(strings.TrimSpace(ab[0]))
			if err != nil {
				return nil, fmt.Errorf("bad range %q", p)
			}
			b, err := strconv.Atoi(strings.TrimSpace(ab[1]))
			if err != nil {
				return nil, fmt.Errorf("bad range %q", p)
			}
			if a > b {
				a, b = b, a
			}
			for i := a; i <= b; i++ {
				if !seen[i] {
					seen[i] = true
					out = append(out, i)
				}
			}
		} else {
			n, err := strconv.Atoi(p)
			if err != nil {
				return nil, fmt.Errorf("bad turn %q", p)
			}
			if !seen[n] {
				seen[n] = true
				out = append(out, n)
			}
		}
	}
	for _, i := range out {
		if i < 1 || i > count {
			return nil, fmt.Errorf("turn %d out of range (have %d turns)", i, count)
		}
	}
	sort.Ints(out)
	return out, nil
}

// Make json package importable (used for some debug paths). Keeps the
// compiler honest if we add `--json` output in v0.2.
var _ = json.Marshal
