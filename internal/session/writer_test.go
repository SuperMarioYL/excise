package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestWriteTargetCursorSqliteUsesSidecar locks in the v0.4 rollback-safety fix:
// for a Cursor sqlite source the cut writes a sidecar, so WriteTarget (which the
// snapshot/rollback bookkeeping keys off) must point at the sidecar, never the
// live .vscdb — otherwise `excise rollback` would clobber the live database.
func TestWriteTargetCursorSqliteUsesSidecar(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{"vscdb", "/home/u/state.vscdb", "/home/u/state.vscdb.excised.jsonl"},
		{"vscdb upper ext", "/home/u/STATE.VSCDB", "/home/u/STATE.VSCDB.excised.jsonl"},
		{"sqlite", "/tmp/x.sqlite", "/tmp/x.sqlite.excised.jsonl"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := WriteTarget(&Session{Tool: ToolCursor, SourcePath: c.src})
			if got != c.want {
				t.Errorf("WriteTarget(%q) = %q, want %q (sidecar, not live DB)", c.src, got, c.want)
			}
		})
	}
}

// TestWriteTargetInPlaceSources confirms the in-place write targets (Claude
// jsonl, Cursor jsonl) still resolve to SourcePath itself.
func TestWriteTargetInPlaceSources(t *testing.T) {
	cases := []*Session{
		{Tool: ToolClaude, SourcePath: "/home/u/sess.jsonl"},
		{Tool: ToolCursor, SourcePath: "/home/u/export.jsonl"},
	}
	for _, s := range cases {
		if got := WriteTarget(s); got != s.SourcePath {
			t.Errorf("WriteTarget for %s/%s = %q, want in-place %q", s.Tool, s.SourcePath, got, s.SourcePath)
		}
	}
}

// TestPreviewTextRuneSafe locks in the multibyte-truncation fix: a long
// multibyte (zh-CN) string must be truncated on a rune boundary so the result
// is always valid UTF-8 (never a split rune that corrupts the TUI / LLM prompt).
func TestPreviewTextRuneSafe(t *testing.T) {
	long := ""
	for i := 0; i < 200; i++ {
		long += "汉"
	}
	out := previewText(long)
	if len(out) == 0 {
		t.Fatal("previewText returned empty for a long input")
	}
	for i, r := range out {
		if r == '�' {
			t.Fatalf("previewText emitted the replacement rune at byte %d — split a multibyte rune: %q", i, out)
		}
	}
}

// TestCursorWriterJSONLHappyPathNotTruncated guards fix_cursor_writer_ignores_
// flush_close_errors on the happy path: a normal write must land every
// surviving turn (the fix that now checks Flush/Close must not break the
// in-place atomic write of the live Cursor jsonl session).
func TestCursorWriterJSONLHappyPathNotTruncated(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "session.jsonl")
	original := `{"composerId":"c1","bubble":{"bubbleId":"b1","type":1,"text":"hello"}}
{"composerId":"c1","bubble":{"bubbleId":"b2","type":2,"text":"world"}}
`
	if err := os.WriteFile(src, []byte(original), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	s := &Session{
		Tool:       ToolCursor,
		SourcePath: src,
		ComposerID: "c1",
		Turns: []Turn{
			{ID: "b1", Role: RoleUser, Raw: []byte(`{"composerId":"c1","bubble":{"bubbleId":"b1","type":1,"text":"hello"}}`)},
			// b2 excised
		},
	}
	if err := (&CursorWriter{}).Write(s); err != nil {
		t.Fatalf("writeJSONL: %v", err)
	}
	got, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if !strings.Contains(string(got), "b1") {
		t.Errorf("surviving turn b1 missing — output truncated? got:\n%s", string(got))
	}
	if strings.Contains(string(got), "b2") {
		t.Errorf("excised turn b2 should not be in the output; got:\n%s", string(got))
	}
	if strings.Contains(string(got), "world") {
		t.Errorf("b2 text leaked into output; got:\n%s", string(got))
	}
}

// TestCursorWriterJSONLErrorDoesNotTruncateLiveFile is the core regression for
// fix_cursor_writer_ignores_flush_close_errors: when the write cannot proceed
// (here: the parent dir is read-only so the tmp file cannot be created), the
// writer MUST surface the error and MUST NOT rename a partial tmp over the
// live session (which would silently truncate it). The live file's content is
// preserved verbatim.
func TestCursorWriterJSONLErrorDoesNotTruncateLiveFile(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "session.jsonl")
	original := `{"composerId":"c1","bubble":{"bubbleId":"b1","type":1,"text":"hello world"}}
`
	if err := os.WriteFile(src, []byte(original), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// Make the parent dir unwritable so the atomic-write tmp cannot be
	// created there — CreateTemp fails before any rename. Restore perms so
	// t.TempDir cleanup can remove the dir.
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	s := &Session{
		Tool:       ToolCursor,
		SourcePath: src,
		ComposerID: "c1",
		Turns: []Turn{
			{ID: "b1", Role: RoleUser, Raw: []byte(`{"composerId":"c1","bubble":{"bubbleId":"b1","type":1,"text":"hello world"}}`)},
		},
	}
	err := (&CursorWriter{}).Write(s)
	if err == nil {
		t.Fatal("writeJSONL should have returned an error when the tmp dir is unwritable")
	}
	// The live file must be byte-for-byte intact — no silent truncation.
	got, readErr := os.ReadFile(src)
	if readErr != nil {
		t.Fatalf("read live file back: %v", readErr)
	}
	if string(got) != original {
		t.Errorf("live file must be unchanged on a write failure; got:\n%s\nwant:\n%s", string(got), original)
	}
}

// TestCursorWriterSidecarHappyPath guards the non-destructive sidecar write
// on the happy path: the .excised.jsonl sidecar lands next to the .vscdb and
// contains every surviving turn's envelope (the Flush/Close error fix must
// not break it).
func TestCursorWriterSidecarHappyPath(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "state.vscdb")
	if err := os.WriteFile(db, []byte("sqlite-ish"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	s := &Session{
		Tool:       ToolCursor,
		SourcePath: db,
		ComposerID: "c1",
		Turns: []Turn{
			{ID: "b1", Role: RoleUser, Raw: []byte(`{"bubbleId":"b1","type":1,"text":"hello"}`)},
			{ID: "b2", Role: RoleAssistant, Raw: []byte(`{"bubbleId":"b2","type":2,"text":"hi"}`)},
		},
	}
	if err := (&CursorWriter{}).Write(s); err != nil {
		t.Fatalf("writeSidecar: %v", err)
	}
	sidecar := db + ".excised.jsonl"
	got, err := os.ReadFile(sidecar)
	if err != nil {
		t.Fatalf("read sidecar: %v", err)
	}
	body := string(got)
	if !strings.Contains(body, "b1") || !strings.Contains(body, "b2") {
		t.Errorf("sidecar must contain both surviving turns; got:\n%s", body)
	}
	if !strings.Contains(body, "composerId") {
		t.Errorf("sidecar must wrap bubbles in the cursor envelope; got:\n%s", body)
	}
	// The live DB is untouched (sidecar is non-destructive).
	live, _ := os.ReadFile(db)
	if string(live) != "sqlite-ish" {
		t.Errorf("live DB must be untouched; got %q", string(live))
	}
}
