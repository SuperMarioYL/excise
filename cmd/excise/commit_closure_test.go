package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SuperMarioYL/excise/internal/session"
)

// TestCommitExciseLogsFullClosure locks in the v0.10.0 audit fix:
// commitExcise used to write removed_ids from the raw seeds only, while the
// write removes the full dependency closure (a cut tool_use turn pulls in its
// paired tool_result turn) and the confirmation preview already advertised
// that closure. The edit log — the "audit every cut" contract of
// internal/safety — must record what was actually removed.
func TestCommitExciseLogsFullClosure(t *testing.T) {
	// Isolate ~/.excise into a temp HOME so the real edit log is untouched.
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")

	// A real, loadable, writable Claude session: cutting assistant turn a-100
	// (which owns a tool_use) must also remove its paired tool_result turn
	// u-101 via the dependency closure.
	src := filepath.Join(t.TempDir(), "session.jsonl")
	fixture := filepath.Join("..", "..", "testdata", "claude_session_with_tools.jsonl")
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

	// Capture stderr (commitExcise prints there); force/yes skip the
	// dependency abort and the stdin prompt.
	origStderr := os.Stderr
	capFile, err := os.CreateTemp("", "excise-stderr-*.txt")
	if err != nil {
		t.Fatalf("create capture: %v", err)
	}
	capPath := capFile.Name()
	t.Cleanup(func() { _ = os.Remove(capPath) })
	os.Stderr = capFile

	gf := &globalFlags{yes: true, force: true}
	commitErr := commitExcise(s, map[string]bool{"a-100": true}, gf, "cut test")

	os.Stderr = origStderr
	capFile.Close()
	if commitErr != nil {
		t.Fatalf("commitExcise returned %v", commitErr)
	}

	// The surviving file must show the closure took effect: both a-100 (owner)
	// and u-101 (paired tool_result) are gone. Compare uuid FIELDS — raw
	// substring matching would hit surviving turns' parentUuid references.
	after, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	surviving := map[string]bool{}
	for _, line := range strings.Split(string(after), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var row struct {
			UUID string `json:"uuid"`
		}
		if json.Unmarshal([]byte(line), &row) == nil && row.UUID != "" {
			surviving[row.UUID] = true
		}
	}
	if surviving["a-100"] || surviving["u-101"] {
		t.Errorf("dependency closure not applied to the file: a-100/u-101 still present (surviving=%v)", surviving)
	}

	// The edit log must list BOTH ids under removed_ids and keep the raw seed
	// under seed_ids.
	logPath := filepath.Join(home, ".excise", "edit_log.jsonl")
	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read edit log: %v", err)
	}
	var entry map[string]any
	if err := json.Unmarshal(raw, &entry); err != nil {
		t.Fatalf("decode edit log entry: %v", err)
	}
	removed := idSet(entry["removed_ids"])
	if !removed["a-100"] || !removed["u-101"] {
		t.Errorf("removed_ids = %v, want it to contain the full closure {a-100, u-101}", entry["removed_ids"])
	}
	seeds := idSet(entry["seed_ids"])
	if len(seeds) != 1 || !seeds["a-100"] {
		t.Errorf("seed_ids = %v, want exactly [a-100]", entry["seed_ids"])
	}
}

func idSet(v any) map[string]bool {
	out := map[string]bool{}
	arr, ok := v.([]any)
	if !ok {
		return out
	}
	for _, e := range arr {
		if s, ok := e.(string); ok {
			out[s] = true
		}
	}
	return out
}
