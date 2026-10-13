package safety

import (
	"os"
	"path/filepath"
	"testing"
)

// TestRollbackPreservesFileMode locks in the v0.10.0 fix: Rollback materializes
// the snapshot through os.CreateTemp (mode 0600) + rename, which used to reset
// the destination's permission bits on every restore. Rollback keeps whatever
// mode the destination currently has — the snapshot records content, not mode.
func TestRollbackPreservesFileMode(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// A session file whose owner has customized the mode, snapshotted the
	// normal way.
	src := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(src, []byte("turn one\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	snap, err := BeforeWrite("sess-rollback-mode", src)
	if err != nil {
		t.Fatalf("BeforeWrite: %v", err)
	}
	if err := Rollback(snap.ID, src); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	fi, err := os.Stat(src)
	if err != nil {
		t.Fatal(err)
	}
	if got := fi.Mode().Perm(); got != 0o640 {
		t.Errorf("mode after rollback = %v, want the destination's existing 0640", got)
	}
	content, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "turn one\n" {
		t.Errorf("rollback content = %q, want original", content)
	}
}

// TestRollbackFirstRestoreGetsSaneMode covers a restore to a path that does
// not exist yet: the file must get the conventional 0644, not CreateTemp 0600.
func TestRollbackFirstRestoreGetsSaneMode(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	src := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(src, []byte("payload\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	snap, err := BeforeWrite("sess-rollback-fresh", src)
	if err != nil {
		t.Fatalf("BeforeWrite: %v", err)
	}
	dest := filepath.Join(t.TempDir(), "restored.jsonl")
	if err := Rollback(snap.ID, dest); err != nil {
		t.Fatalf("Rollback to fresh dest: %v", err)
	}
	fi, err := os.Stat(dest)
	if err != nil {
		t.Fatal(err)
	}
	if got := fi.Mode().Perm(); got != 0o644 {
		t.Errorf("first restore mode = %v, want 0644", got)
	}
}
