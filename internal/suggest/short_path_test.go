package suggest

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/SuperMarioYL/excise/internal/session"
)

// fix-shortpath-multibyte-slice: shortPath used to tail-truncate a >40-byte
// path with the byte slice `p[len(p)-39:]`. For a multibyte UTF-8 path (the
// zh-CN-first user base) the 39-byte tail can start mid-rune, so the
// repeated_file_edit trigger reason embedded invalid UTF-8 — verified at the
// v0.8.0 tag: 6 of 10 byte-alignment offsets produced e.g.
// "…\x98\x8e明明a/src/…" (leading bytes 98 8e = the orphaned tail of a split
// 明 rune). The corrupted reason surfaced in `excise suggest --json` (mangled
// to U+FFFD by encoding/json) and any trigger-reason rendering. These tests
// pin the rune-boundary fix; they fail on the old byte-slice implementation.

func TestShortPathStaysValidUTF8AcrossByteAlignments(t *testing.T) {
	// Shift an ASCII filler between the CJK run and the fixed suffix so the
	// 39-byte tail window starts at every intra-rune byte offset — this is
	// the exact probe that reproduced the invalid output at v0.8.0.
	for k := 0; k <= 9; k++ {
		p := "/Users/x/" + strings.Repeat("明", 14) + strings.Repeat("a", k) + "/src/组件渲染器面板.tsx"
		got := shortPath(p)
		if !utf8.ValidString(got) {
			t.Errorf("k=%d: shortPath produced INVALID UTF-8: %q (leading bytes %x)", k, got, []byte(got[:6]))
		}
		for _, r := range got {
			if r == utf8.RuneError {
				t.Errorf("k=%d: shortPath output contains U+FFFD: %q", k, got)
			}
		}
	}
}

func TestShortPathTruncatesOnRuneBoundaries(t *testing.T) {
	cases := []struct {
		name string
		in   string
	}{
		{
			name: "long CJK path keeps a valid rune tail",
			in:   "/Users/小明/开发目录/一个很长的前端项目名称/src/组件渲染器面板.tsx",
		},
		{
			name: "long ASCII path keeps the last 39 runes",
			in:   "/Users/x/really/long/path/that/exceeds/forty/runes/src/main.go",
		},
	}
	for _, c := range cases {
		got := shortPath(c.in)
		if !utf8.ValidString(got) {
			t.Errorf("%s: INVALID UTF-8: %q", c.name, got)
		}
		if got == c.in {
			t.Errorf("%s: expected truncation of a >40-rune path", c.name)
		}
		if !strings.HasPrefix(got, "…") {
			t.Errorf("%s: truncated path should start with the ellipsis marker: %q", c.name, got)
		}
		// The tail must be a rune-aligned suffix of the input.
		if !strings.HasSuffix(c.in, strings.TrimPrefix(got, "…")) {
			t.Errorf("%s: truncated tail is not a suffix of the input: %q", c.name, got)
		}
	}
}

func TestShortPathPassthroughShortPaths(t *testing.T) {
	for _, p := range []string{
		"",
		"src/main.go",
		"/Users/小明/短.tsx", // multibyte but short
	} {
		if got := shortPath(p); got != p {
			t.Errorf("shortPath(%q) = %q, want identity for short paths", p, got)
		}
	}
}

// TestRepeatedFileEditReasonValidUTF8 drives the fix end-to-end through the
// public Score entry point: three consecutive Edit tool_use turns on a long
// CJK path must yield a repeated_file_edit trigger whose Reason is valid
// UTF-8 (it is serialized into `excise suggest --json` output).
func TestRepeatedFileEditReasonValidUTF8(t *testing.T) {
	path := "/Users/小明/开发目录/一个很长的前端项目名称/src/组件渲染器面板.tsx"
	if utf8.RuneCountInString(path) <= 40 {
		t.Fatalf("fixture path must exceed 40 runes: got %d", utf8.RuneCountInString(path))
	}
	turns := []session.Turn{
		repeatedEditTurnForTest("t1", path),
		repeatedEditTurnForTest("t2", path),
		repeatedEditTurnForTest("t3", path),
	}
	s := &session.Session{Tool: session.ToolClaude, SessionID: "shortpath-e2e", Turns: turns}
	scored := Score(s)
	found := false
	for _, ts := range scored {
		for _, trig := range ts.Triggers {
			if trig.ID != HRepeatedFileEdit {
				continue
			}
			found = true
			if !utf8.ValidString(trig.Reason) {
				t.Errorf("repeated_file_edit reason is INVALID UTF-8: %q (bytes %x)", trig.Reason, trig.Reason)
			}
			for _, r := range trig.Reason {
				if r == utf8.RuneError {
					t.Errorf("reason contains U+FFFD: %q", trig.Reason)
				}
			}
		}
	}
	if !found {
		t.Fatal("repeated_file_edit did not fire on 3 consecutive edits to the same path")
	}
}

func repeatedEditTurnForTest(id, path string) session.Turn {
	raw, _ := json.Marshal(map[string]any{
		"type": "assistant",
		"uuid": id,
		"message": map[string]any{
			"role": "assistant",
			"content": []map[string]any{{
				"type":  "tool_use",
				"id":    id + "-toolu",
				"name":  "Edit",
				"input": map[string]any{"file_path": path},
			}},
		},
	})
	return session.Turn{
		ID:        id,
		Role:      session.RoleAssistant,
		ToolCalls: []session.ToolCall{{ID: id + "-toolu", Name: "Edit"}},
		Raw:       raw,
	}
}
