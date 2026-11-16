package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SuperMarioYL/excise/internal/safety"
	"github.com/SuperMarioYL/excise/internal/session"
)

// fix-rollback-relative-source-path: commitExcise used to log the write target
// verbatim, so a relative CLI path produced a relative edit-log source_path.
// `excise rollback <id>` resolves its destination from that entry, meaning a
// rollback run from a different cwd wrote a stray copy and still exited 0
// while the real transcript stayed cut. This test drives the real commit path
// with a relative argument and then rolls back from another directory.
func TestCommitLogsAbsoluteSourcePathAndRollbackFromOtherCwd(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")

	fixture := filepath.Join("..", "..", "testdata", "claude_session_simple.jsonl")
	data, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	work := t.TempDir()
	t.Chdir(work)
	if err := os.WriteFile("s.jsonl", data, 0o644); err != nil {
		t.Fatalf("write relative session: %v", err)
	}
	absOriginal, err := filepath.Abs("s.jsonl")
	if err != nil {
		t.Fatalf("abs: %v", err)
	}

	s, err := session.LoadAuto("s.jsonl")
	if err != nil {
		t.Fatalf("LoadAuto: %v", err)
	}
	gf := &globalFlags{yes: true, force: true}
	if err := commitExcise(s, map[string]bool{"u-0003": true}, gf, "cut test"); err != nil {
		t.Fatalf("commitExcise: %v", err)
	}

	entry := lastEditLogEntry(t, home)
	src, _ := entry["source_path"].(string)
	if !filepath.IsAbs(src) {
		t.Fatalf("edit log source_path = %q; must be absolute so rollback is cwd-independent", src)
	}
	if src != absOriginal {
		t.Fatalf("edit log source_path = %q, want %q", src, absOriginal)
	}
	snapID, _ := entry["snapshot"].(string)
	if snapID == "" {
		t.Fatalf("expected a snapshot id in the edit log; got %v", entry)
	}

	// The cut really happened.
	cut, err := os.ReadFile(absOriginal)
	if err != nil {
		t.Fatalf("read cut session: %v", err)
	}
	if strings.Contains(string(cut), "u-0003") {
		t.Fatalf("cut session still contains u-0003; commit did not remove it")
	}

	// Roll back from an unrelated cwd: the destination must come from the log.
	elsewhere := t.TempDir()
	t.Chdir(elsewhere)
	if err := safety.Rollback(snapID, ""); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	restored, err := os.ReadFile(absOriginal)
	if err != nil {
		t.Fatalf("read restored session: %v", err)
	}
	if !strings.Contains(string(restored), "u-0003") {
		t.Errorf("rollback did not restore the original transcript")
	}
	if _, err := os.Stat(filepath.Join(elsewhere, "s.jsonl")); err == nil {
		t.Errorf("rollback created a stray %s in the invocation cwd", filepath.Join(elsewhere, "s.jsonl"))
	}
}

func lastEditLogEntry(t *testing.T, home string) map[string]any {
	t.Helper()
	path := filepath.Join(home, ".excise", "edit_log.jsonl")
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open edit log: %v", err)
	}
	defer f.Close()
	dec := json.NewDecoder(f)
	var last map[string]any
	for {
		var entry map[string]any
		if err := dec.Decode(&entry); err != nil {
			break
		}
		last = entry
	}
	if last == nil {
		t.Fatalf("edit log %s has no entries", path)
	}
	return last
}
