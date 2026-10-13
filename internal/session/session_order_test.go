package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCursorNoTimestampKeepsInsertionOrder locks in the v0.10.0 ordering fix.
//
// Real Cursor bubbles carry NO timestamp field (verified 2026-10-14 against
// the live state.vscdb on the dev machine: sampled cursorDiskKV bubble values
// contain bubbleId/type/text/toolResults keys but no timestamp), so every
// Turn loads with a zero Timestamp. SortByTimestamp used to break ties with
// `ID < ID` and order zero timestamps first, which re-sorted the whole
// conversation into lexicographic bubbleId (UUID) order — scrambling the
// transcript before the TUI, cut indexes, or sidecar write ever saw it.
// Zero-timestamp turns must stay at their load position (invariant 3).
func TestCursorNoTimestampKeepsInsertionOrder(t *testing.T) {
	// Insertion order z-001..b-004 is deliberately reverse-alphabetical so a
	// lexicographic ID sort cannot accidentally agree with it.
	src := filepath.Join(t.TempDir(), "cursor_session.jsonl")
	content := strings.Join([]string{
		`{"composerId":"comp-x","bubble":{"bubbleId":"z-001","type":1,"text":"first user turn"}}`,
		`{"composerId":"comp-x","bubble":{"bubbleId":"m-002","type":2,"text":"assistant reply"}}`,
		`{"composerId":"comp-x","bubble":{"bubbleId":"a-003","type":1,"text":"second user turn"}}`,
		`{"composerId":"comp-x","bubble":{"bubbleId":"b-004","type":2,"text":"final reply"}}`,
	}, "\n")
	if err := os.WriteFile(src, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := LoadAuto(src)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, turn := range s.Turns {
		got = append(got, turn.ID)
	}
	want := []string{"z-001", "m-002", "a-003", "b-004"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("zero-timestamp bubbles reordered: got %v, want %v", got, want)
	}
}

// TestClaudeMidfileCorruptLineStaysInPlace pins the Claude side of the same
// root cause: a preserved verbatim row (unparseable line) loads with a zero
// Timestamp and used to float to the TOP of the file on write-back, violating
// invariant 3 (ordering of surviving turns is preserved).
func TestClaudeMidfileCorruptLineStaysInPlace(t *testing.T) {
	src := filepath.Join(t.TempDir(), "session.jsonl")
	content := strings.Join([]string{
		`{"type":"user","uuid":"u-1","parentUuid":null,"timestamp":"2026-05-17T12:00:00.000Z","sessionId":"s","message":{"role":"user","content":"hello"}}`,
		`{corrupt truncated line`,
		`{"type":"assistant","uuid":"u-3","parentUuid":"u-1","timestamp":"2026-05-17T12:00:05.000Z","sessionId":"s","message":{"role":"assistant","content":"hi"}}`,
	}, "\n")
	if err := os.WriteFile(src, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := LoadAuto(src)
	if err != nil {
		t.Fatal(err)
	}
	// Cut the assistant turn; the preserved corrupt line must survive in place.
	s.Turns = Excise(s.Turns, map[string]bool{"u-3": true})
	if err := (&ClaudeWriter{}).Write(s); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(string(after), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 surviving lines, got %d: %q", len(lines), string(after))
	}
	if !strings.HasPrefix(lines[0], `{"type":"user"`) {
		t.Errorf("line 0 must be the original user turn, got: %.60s", lines[0])
	}
	if !strings.HasPrefix(lines[1], "{corrupt") {
		t.Errorf("mid-file corrupt line moved to wrong position, got: %.60s", lines[1])
	}
}

// TestClaudeSameMillisecondTurnsKeepLoadOrder guards the other half of the
// tie-break change: two turns with identical timestamps must keep their file
// order (the old UUID tie-break could swap same-millisecond neighbors).
func TestClaudeSameMillisecondTurnsKeepLoadOrder(t *testing.T) {
	src := filepath.Join(t.TempDir(), "session.jsonl")
	ts := "2026-05-17T12:00:00.000Z"
	content := strings.Join([]string{
		`{"type":"user","uuid":"zzz-first","parentUuid":null,"timestamp":"` + ts + `","sessionId":"s","message":{"role":"user","content":"one"}}`,
		`{"type":"assistant","uuid":"aaa-second","parentUuid":"zzz-first","timestamp":"` + ts + `","sessionId":"s","message":{"role":"assistant","content":"two"}}`,
	}, "\n")
	if err := os.WriteFile(src, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := LoadAuto(src)
	if err != nil {
		t.Fatal(err)
	}
	if s.Turns[0].ID != "zzz-first" || s.Turns[1].ID != "aaa-second" {
		t.Errorf("same-millisecond turns reordered by UUID tie-break: got [%s, %s], want [zzz-first, aaa-second]",
			s.Turns[0].ID, s.Turns[1].ID)
	}
}

// TestTimestampedTurnsStillSortByTime makes sure the fix did not throw away
// timestamp ordering for sessions that do carry timestamps.
func TestTimestampedTurnsStillSortByTime(t *testing.T) {
	turns := []Turn{
		{ID: "late", Timestamp: mustTime(t, "2026-05-17T12:00:10Z")},
		{ID: "early", Timestamp: mustTime(t, "2026-05-17T12:00:00Z")},
		{ID: "mid", Timestamp: mustTime(t, "2026-05-17T12:00:05Z")},
	}
	SortByTimestamp(turns)
	if turns[0].ID != "early" || turns[1].ID != "mid" || turns[2].ID != "late" {
		t.Errorf("timestamped turns not sorted: got [%s %s %s]", turns[0].ID, turns[1].ID, turns[2].ID)
	}
}
