package session

import "testing"

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
