package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SuperMarioYL/excise/internal/safety"
	"github.com/SuperMarioYL/excise/internal/session"
)

// TestCommitExciseWarnsOnLogEditFailure locks in
// fix-logedit-swallow-breaks-rollback-inference: commitExcise used to
// swallow safety.LogEdit's error with `_ =` immediately AFTER advertising
// `excise rollback <id>`. The edit-log entry is the only link between a
// snapshot id and its source_path, so a failed LogEdit silently broke the
// bare `excise rollback <id>` path (lookupSourcePath returns "" when the
// entry is absent, and Rollback then errors "no destination path; pass
// --to"). The fix surfaces a non-fatal stderr warning that tells the user
// the snapshot is still valid and to roll back with the explicit
// `excise rollback <snapID> --to <writeTarget>` workaround.
func TestCommitExciseWarnsOnLogEditFailure(t *testing.T) {
	// Isolate ~/.excise into a temp HOME so the real edit log is untouched.
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")

	// Snapshots must succeed (so a snap id is advertised) while the edit log
	// must fail. Sabotage edit_log.jsonl as a directory: OpenFile with
	// O_WRONLY|O_APPEND on a directory returns EISDIR, so LogEdit errors
	// while the snapshots/ subtree (a sibling path) stays writable.
	if err := os.MkdirAll(filepath.Join(home, ".excise"), 0o755); err != nil {
		t.Fatalf("mkdir ~/.excise: %v", err)
	}
	logPath := filepath.Join(home, ".excise", "edit_log.jsonl")
	if err := os.Mkdir(logPath, 0o755); err != nil {
		t.Fatalf("sabotage edit log dir: %v", err)
	}
	// Precondition: LogEdit really fails under this layout. If it doesn't,
	// the test can't exercise the warning path — fail loudly, not silently.
	if err := safety.LogEdit(map[string]any{"probe": true}); err == nil {
		t.Fatal("test precondition failed: LogEdit did not error with a directory at edit_log.jsonl")
	}

	// A real, loadable, writable Claude session on disk so commitExcise can
	// snapshot, excise, and rewrite it end-to-end.
	src := filepath.Join(t.TempDir(), "session.jsonl")
	fixture := filepath.Join("..", "..", "testdata", "claude_session_simple.jsonl")
	data, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	if err := os.WriteFile(src, data, 0o644); err != nil {
		t.Fatalf("write src copy: %v", err)
	}
	s, err := session.LoadAuto(src)
	if err != nil {
		t.Fatalf("LoadAuto: %v", err)
	}
	writeTarget := session.WriteTarget(s)

	// commitExcise writes its warning + commit lines directly to os.Stderr;
	// swap in a temp file to capture them (restored before any t.Log call).
	origStderr := os.Stderr
	capFile, err := os.CreateTemp("", "excise-stderr-*.txt")
	if err != nil {
		t.Fatalf("create capture: %v", err)
	}
	capPath := capFile.Name()
	t.Cleanup(func() { _ = os.Remove(capPath) })
	os.Stderr = capFile

	// Mark a plain assistant text turn (a-0002 has no tool_use, so excising
	// it is dependency-clean). force=true keeps dependency warnings from
	// aborting; yes=true skips the stdin confirmation prompt.
	gf := &globalFlags{yes: true, force: true}
	seeds := map[string]bool{"a-0002": true}
	commitErr := commitExcise(s, seeds, gf, "cut test")

	// Restore stderr, then read the capture.
	os.Stderr = origStderr
	capFile.Close()
	if err := commitErr; err != nil {
		t.Fatalf("commitExcise returned %v; a LogEdit failure must stay non-fatal (exit 0)", err)
	}

	captured, err := os.ReadFile(capPath)
	if err != nil {
		t.Fatalf("read captured stderr: %v", err)
	}
	stderrText := string(captured)

	// Extract the advertised snapshot id from the normal commit line, then
	// assert the warning echoes it back with the explicit --to workaround.
	var snapID string
	for _, line := range strings.Split(stderrText, "\n") {
		if i := strings.Index(line, "snapshot:"); i >= 0 {
			snapID = strings.TrimSpace(line[i+len("snapshot:"):])
			break
		}
	}
	if snapID == "" {
		t.Fatalf("commit message did not advertise a snapshot id; stderr:\n%s", stderrText)
	}
	wantCmd := fmt.Sprintf("excise rollback %s --to %s", snapID, writeTarget)
	if !strings.Contains(stderrText, wantCmd) {
		t.Errorf("LogEdit failure must warn with the exact --to workaround %q; stderr:\n%s", wantCmd, stderrText)
	}
	if !strings.Contains(stderrText, "could not write edit log") {
		t.Errorf("warning must state the edit log could not be written; stderr:\n%s", stderrText)
	}
}
