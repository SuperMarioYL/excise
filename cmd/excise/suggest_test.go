package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// We exercise the suggest subcommand end-to-end by building the binary in a
// temp dir and running it. This is the same pattern v0.1's existing
// integration coverage uses where applicable; for this codebase there is no
// prior integration harness so we keep it self-contained.

func buildBin(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "excise")
	cmd := exec.Command("go", "build", "-o", bin, "./cmd/excise")
	// run from repo root (two levels up from cmd/excise)
	cwd, _ := os.Getwd()
	cmd.Dir = filepath.Join(cwd, "..", "..")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("go build failed: %v\nstderr: %s", err, stderr.String())
	}
	return bin
}

func TestSuggestTableOutput(t *testing.T) {
	bin := buildBin(t)
	cwd, _ := os.Getwd()
	fixture := filepath.Join(cwd, "..", "..", "testdata", "claude_session_polluted.jsonl")
	cmd := exec.Command(bin, "suggest", fixture)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("excise suggest failed: %v\n%s", err, string(out))
	}
	s := string(out)
	for _, want := range []string{"role", "tokens", "heuristic", "preview", "high_token_cost"} {
		if !strings.Contains(s, want) {
			t.Errorf("expected %q in table output; got:\n%s", want, s)
		}
	}
	// Should mention some candidates totalling tokens
	if !strings.Contains(s, "candidate") {
		t.Errorf("expected totals line; got:\n%s", s)
	}
}

func TestSuggestJSONOutput(t *testing.T) {
	bin := buildBin(t)
	cwd, _ := os.Getwd()
	fixture := filepath.Join(cwd, "..", "..", "testdata", "claude_session_polluted.jsonl")
	cmd := exec.Command(bin, "suggest", "--json", fixture)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("excise suggest --json failed: %v", err)
	}
	var picks []map[string]any
	if err := json.Unmarshal(out, &picks); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, string(out))
	}
	if len(picks) == 0 {
		t.Fatal("--json returned empty array on polluted fixture")
	}
	// Each pick must carry a turn_id and score field.
	for _, p := range picks {
		if _, ok := p["turn_id"]; !ok {
			t.Errorf("pick missing turn_id: %+v", p)
		}
		if _, ok := p["score"]; !ok {
			t.Errorf("pick missing score: %+v", p)
		}
	}
}

func TestSuggestTopFlag(t *testing.T) {
	bin := buildBin(t)
	cwd, _ := os.Getwd()
	fixture := filepath.Join(cwd, "..", "..", "testdata", "claude_session_polluted.jsonl")
	cmd := exec.Command(bin, "suggest", "--top=2", "--json", fixture)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("excise suggest --top=2 failed: %v", err)
	}
	var picks []map[string]any
	if err := json.Unmarshal(out, &picks); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(picks) != 2 {
		t.Errorf("--top=2 want 2 picks, got %d", len(picks))
	}
}
