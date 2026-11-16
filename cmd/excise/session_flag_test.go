package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/SuperMarioYL/excise/internal/session"
)

// fix-session-flag-loses-to-positional: `--session` is documented as
// "explicit path to a session file (overrides positional arg)"
// (cmd/excise/main.go persistent flag help), but every subcommand used to
// overwrite it with the positional argument. These tests pin the documented
// precedence end-to-end through the real cobra tree and, for cut (whose first
// positional is the range), through the shared resolver.
func TestSessionFlagOverridesPositionalPath(t *testing.T) {
	flagPath := filepath.Join("..", "..", "testdata", "claude_session_simple.jsonl")
	posPath := filepath.Join("..", "..", "testdata", "claude_session_polluted.jsonl")

	out := runListCapturingStdout(t, []string{"list", "--session", flagPath, posPath})
	if !strings.Contains(out, "source : "+flagPath) {
		t.Errorf("--session must override the positional path; got:\n%s", out)
	}
	if strings.Contains(out, "source : "+posPath) {
		t.Errorf("positional path won over the explicit --session flag; got:\n%s", out)
	}

	// Without --session the positional argument is still honored.
	out = runListCapturingStdout(t, []string{"list", posPath})
	if !strings.Contains(out, "source : "+posPath) {
		t.Errorf("positional-only invocation should load its path; got:\n%s", out)
	}
}

func TestSessionPathFromArgs(t *testing.T) {
	newCmd := func(setFlag bool) *cobra.Command {
		cmd := &cobra.Command{Use: "x"}
		cmd.Flags().String("session", "", "")
		if setFlag {
			if err := cmd.Flags().Set("session", "/from/flag.jsonl"); err != nil {
				t.Fatalf("set session flag: %v", err)
			}
		}
		return cmd
	}

	if got := sessionPathFromArgs(newCmd(true), []string{"/from/positional.jsonl"}, "/from/flag.jsonl"); got != "/from/flag.jsonl" {
		t.Errorf("explicit --session must win; got %q", got)
	}
	if got := sessionPathFromArgs(newCmd(false), []string{"/from/positional.jsonl"}, ""); got != "/from/positional.jsonl" {
		t.Errorf("positional path should be used when --session is unset; got %q", got)
	}
	if got := sessionPathFromArgs(newCmd(false), nil, "/already/set.jsonl"); got != "/already/set.jsonl" {
		t.Errorf("no args should keep the current value; got %q", got)
	}
	// cut passes the path slice after the range.
	if got := sessionPathFromArgs(newCmd(false), []string{"/cut/path.jsonl"}, ""); got != "/cut/path.jsonl" {
		t.Errorf("cut's path slice should resolve; got %q", got)
	}
}

// runListCapturingStdout executes the real root command with args and returns
// everything printList wrote to os.Stdout.
func runListCapturingStdout(t *testing.T, args []string) string {
	t.Helper()
	capture, err := os.CreateTemp(t.TempDir(), "stdout-*.txt")
	if err != nil {
		t.Fatalf("create capture: %v", err)
	}
	origStdout := os.Stdout
	os.Stdout = capture
	root := newRootCmd()
	root.SetArgs(args)
	runErr := root.Execute()
	os.Stdout = origStdout
	if err := capture.Close(); err != nil {
		t.Fatalf("close capture: %v", err)
	}
	if runErr != nil {
		t.Fatalf("execute %v: %v", args, runErr)
	}
	data, err := os.ReadFile(capture.Name())
	if err != nil {
		t.Fatalf("read capture: %v", err)
	}
	if len(data) == 0 {
		t.Fatalf("list produced no output for %v", args)
	}
	return string(data)
}

// Guard against the fixtures drifting: both must be loadable so the precedence
// assertion above is about flag handling, not about a missing file.
func TestPrecedenceFixturesLoad(t *testing.T) {
	for _, name := range []string{"claude_session_simple.jsonl", "claude_session_polluted.jsonl"} {
		if _, err := session.LoadAuto(filepath.Join("..", "..", "testdata", name)); err != nil {
			t.Fatalf("fixture %s: %v", name, err)
		}
	}
}
