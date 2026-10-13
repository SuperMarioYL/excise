package session

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func mustTime(t *testing.T, s string) time.Time {
	t.Helper()
	tm, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("parse %s: %v", s, err)
	}
	return tm
}

// TestClaudeWritePreservesFileMode locks in the v0.10.0 fix: every writer
// replaces the destination via os.CreateTemp (mode 0600) + rename, which used
// to silently tighten the transcript's permission bits on every cut.
func TestClaudeWritePreservesFileMode(t *testing.T) {
	for _, mode := range []os.FileMode{0o644, 0o600, 0o640} {
		t.Run(fmt.Sprintf("mode_%04o", mode), func(t *testing.T) {
			src := filepath.Join(t.TempDir(), "session.jsonl")
			content := `{"type":"user","uuid":"u-1","parentUuid":null,"timestamp":"2026-05-17T12:00:00.000Z","sessionId":"s","message":{"role":"user","content":"hello"}}`
			if err := os.WriteFile(src, []byte(content), mode); err != nil {
				t.Fatal(err)
			}
			s, err := LoadAuto(src)
			if err != nil {
				t.Fatal(err)
			}
			if err := (&ClaudeWriter{}).Write(s); err != nil {
				t.Fatal(err)
			}
			fi, err := os.Stat(src)
			if err != nil {
				t.Fatal(err)
			}
			if got := fi.Mode().Perm(); got != mode {
				t.Errorf("file mode after write = %v, want %v (os.CreateTemp default leaked)", got, mode)
			}
		})
	}
}

// TestCursorJSONLWritePreservesFileMode covers the Cursor jsonl branch, whose
// rename target is the live session file.
func TestCursorJSONLWritePreservesFileMode(t *testing.T) {
	src := filepath.Join(t.TempDir(), "cursor_session.jsonl")
	content := strings.Join([]string{
		`{"composerId":"comp-x","bubble":{"bubbleId":"b-001","type":1,"text":"hello"}}`,
		`{"composerId":"comp-x","bubble":{"bubbleId":"b-002","type":2,"text":"hi"}}`,
	}, "\n")
	if err := os.WriteFile(src, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := LoadAuto(src)
	if err != nil {
		t.Fatal(err)
	}
	s.Turns = Excise(s.Turns, map[string]bool{"b-002": true})
	if err := (&CursorWriter{}).Write(s); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(src)
	if err != nil {
		t.Fatal(err)
	}
	if got := fi.Mode().Perm(); got != 0o644 {
		t.Errorf("file mode after cursor jsonl write = %v, want 0644", got)
	}
}

// TestCursorSidecarWriteDefaultMode covers the first sidecar write, where no
// destination exists yet: the file must get the conventional 0644, not the
// CreateTemp 0600.
func TestCursorSidecarWriteDefaultMode(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "state.vscdb")
	s := &Session{
		Tool:       ToolCursor,
		SourcePath: db,
		ComposerID: "comp-x",
		Turns: []Turn{
			{ID: "b-001", Raw: []byte(`{"bubbleId":"b-001","type":1,"text":"hello"}`)},
		},
	}
	if err := (&CursorWriter{}).Write(s); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(db + ".excised.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	if got := fi.Mode().Perm(); got != 0o644 {
		t.Errorf("first sidecar mode = %v, want 0644", got)
	}
}
