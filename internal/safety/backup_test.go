package safety

import (
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// m3 — snapshot creates a gzip'd copy and Rollback restores it byte-for-byte.
func TestSnapshotRollbackRoundTrip(t *testing.T) {
	// Redirect ~/.excise to a temp dir so we don't litter the real HOME.
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	src := filepath.Join(t.TempDir(), "sess.jsonl")
	payload := []byte(`{"hello":"world"}` + "\n" + `{"second":"line"}` + "\n")
	if err := os.WriteFile(src, payload, 0o644); err != nil {
		t.Fatal(err)
	}

	snap, err := BeforeWrite("sess-test", src)
	if err != nil {
		t.Fatalf("BeforeWrite: %v", err)
	}
	if snap.Path == "" || snap.ID == "" {
		t.Fatalf("snapshot has empty fields: %+v", snap)
	}

	// Verify the snapshot can be decompressed back to the original bytes.
	f, err := os.Open(snap.Path)
	if err != nil {
		t.Fatal(err)
	}
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(gz)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(payload) {
		t.Fatalf("snapshot content mismatch")
	}
	_ = gz.Close()
	_ = f.Close()

	// Mutate the source, then roll back.
	if err := os.WriteFile(src, []byte("CORRUPTED\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Rollback(snap.ID, src); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	restored, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	if string(restored) != string(payload) {
		t.Fatalf("rollback content mismatch: got %q", restored)
	}
}

// m3 — LogEdit appends a parseable JSON line.
func TestLogEdit(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	if err := LogEdit(map[string]any{"ts": "now", "removed_ids": []string{"a", "b"}}); err != nil {
		t.Fatalf("LogEdit: %v", err)
	}
	logPath := filepath.Join(tmpHome, ".excise", "edit_log.jsonl")
	if _, err := os.Stat(logPath); err != nil {
		t.Fatalf("expected edit_log at %s: %v", logPath, err)
	}
}
