package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SuperMarioYL/excise/internal/config"
	"github.com/SuperMarioYL/excise/internal/llm"
	"github.com/SuperMarioYL/excise/internal/session"
	"github.com/SuperMarioYL/excise/internal/suggest"
)

// loadFixture is a tiny shim around the claude loader for tests that need a
// real-shaped Session.
func loadFixture(t *testing.T, name string) *session.Session {
	t.Helper()
	path := filepath.Join("..", "..", "testdata", name)
	s, err := session.LoadAuto(path)
	if err != nil {
		t.Fatalf("load %s: %v", path, err)
	}
	return s
}

// withStubReranker swaps the package-level factory for the duration of a
// test and restores it on cleanup.
func withStubReranker(t *testing.T, r llm.Reranker) {
	t.Helper()
	orig := rerankerFactory
	rerankerFactory = func(_ *config.Config) llm.Reranker { return r }
	t.Cleanup(func() { rerankerFactory = orig })
}

func TestRankCandidatesHeuristicOnly(t *testing.T) {
	s := loadFixture(t, "claude_session_llm_rerank.jsonl")
	gf := &globalFlags{}
	res, err := rankCandidates(context.Background(), gf, s, 5, 0.0, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("rankCandidates: %v", err)
	}
	if res.UsedLLM {
		t.Fatalf("UsedLLM should be false when --llm not set")
	}
	if len(res.Picks) == 0 {
		t.Fatalf("fixture should produce heuristic picks; got 0")
	}
	for _, p := range res.Picks {
		if p.LLMReason != "" {
			t.Errorf("LLMReason should be empty when --llm off; got %q on %s", p.LLMReason, p.TurnID)
		}
	}
}

func TestRankCandidatesLLMHappy(t *testing.T) {
	s := loadFixture(t, "claude_session_llm_rerank.jsonl")
	heur := suggest.TopK(suggest.Score(s), 5, 0.0)
	if len(heur) == 0 {
		t.Fatal("heuristic produced 0 candidates; fixture is wrong")
	}
	// Stub: promote u-032 (the REAL failure) above the heuristic top.
	picks := []struct {
		TurnID string
		Score  float64
		Reason string
	}{
		{TurnID: "u-032", Score: 9.5, Reason: "kept regenerating the same buggy hunk"},
	}
	// Cover every heuristic-shortlisted turn with a synthetic reason so
	// merge dedupes don't drop any.
	for _, h := range heur {
		if h.TurnID == "u-032" {
			continue
		}
		picks = append(picks, struct {
			TurnID string
			Score  float64
			Reason string
		}{TurnID: h.TurnID, Score: h.Score - 1.0, Reason: "heuristic ok, lower priority"})
	}
	withStubReranker(t, llm.NewStubReranker(nil, picks))

	gf := &globalFlags{llm: true}
	var stderr bytes.Buffer
	res, err := rankCandidates(context.Background(), gf, s, 5, 0.0, &stderr)
	if err != nil {
		t.Fatalf("rankCandidates: %v", err)
	}
	if !res.UsedLLM {
		t.Fatalf("UsedLLM should be true; got false")
	}
	if res.Fallback {
		t.Errorf("Fallback should be false on happy path")
	}
	if got := res.Picks[0].TurnID; got != "u-032" {
		t.Errorf("expected u-032 promoted to top, got %s", got)
	}
	if !strings.Contains(res.Picks[0].LLMReason, "buggy") {
		t.Errorf("LLMReason should contain reason; got %q", res.Picks[0].LLMReason)
	}
	if stderr.Len() != 0 {
		t.Errorf("happy path should not write to stderr; got %q", stderr.String())
	}
}

func TestRankCandidatesFallbackOnUnavailable(t *testing.T) {
	s := loadFixture(t, "claude_session_llm_rerank.jsonl")
	withStubReranker(t, llm.NewStubReranker(llm.ErrLLMUnavailable, nil))

	gf := &globalFlags{llm: true}
	var stderr bytes.Buffer
	res, err := rankCandidates(context.Background(), gf, s, 5, 0.0, &stderr)
	if err != nil {
		t.Fatalf("rankCandidates: %v (fallback should swallow it)", err)
	}
	if res.UsedLLM {
		t.Errorf("UsedLLM should stay false on fallback")
	}
	if !res.Fallback {
		t.Errorf("Fallback should be true")
	}
	if !errors.Is(res.FallbackErr, llm.ErrLLMUnavailable) {
		t.Errorf("FallbackErr should wrap ErrLLMUnavailable, got %v", res.FallbackErr)
	}
	if len(res.Picks) == 0 {
		t.Errorf("fallback must still surface heuristic picks")
	}
	msg := stderr.String()
	if !strings.Contains(msg, "LLM unavailable") {
		t.Errorf("expected stderr warning, got %q", msg)
	}
	if !strings.Contains(msg, "heuristic ranking") {
		t.Errorf("expected fallback hint, got %q", msg)
	}
}

func TestRankCandidatesJSONParseFailureFallsBack(t *testing.T) {
	// A reranker returning a generic wrapped ErrLLMUnavailable (which is
	// what parseRerankReply does on garbage) must trip the same fallback
	// path as a network failure.
	s := loadFixture(t, "claude_session_llm_rerank.jsonl")
	wrapped := fmt.Errorf("%w: could not parse reply", llm.ErrLLMUnavailable)
	withStubReranker(t, llm.NewStubReranker(wrapped, nil))

	gf := &globalFlags{llm: true}
	var stderr bytes.Buffer
	res, err := rankCandidates(context.Background(), gf, s, 5, 0.0, &stderr)
	if err != nil {
		t.Fatalf("rankCandidates: %v", err)
	}
	if !res.Fallback {
		t.Errorf("JSON-parse failure should fall back")
	}
	if !strings.Contains(stderr.String(), "LLM unavailable") {
		t.Errorf("expected stderr warning, got %q", stderr.String())
	}
}

func TestSuggestLLMTableOutputViaBinary(t *testing.T) {
	// This is a smoke test that the --llm path doesn't blow up at the CLI
	// boundary. We can't easily inject a stub through a built binary, so
	// the test just exercises a fallback (no Ollama running) and asserts
	// that we exit 0 with the warning on stderr.
	bin := buildBin(t)
	fixture := filepath.Join("..", "..", "testdata", "claude_session_llm_rerank.jsonl")

	// Point --llm-host at a port nothing's listening on so the call fails
	// fast and we go through the fallback path.
	cmd := exec.Command(bin, "suggest", "--llm",
		"--llm-host=http://127.0.0.1:1", "--llm-model=missing",
		fixture)
	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("excise suggest --llm fallback should exit 0; got %v, stderr=%s", err, stderr.String())
	}
	if !strings.Contains(stderr.String(), "LLM unavailable") {
		t.Errorf("expected fallback warning on stderr; got %q", stderr.String())
	}
	// Heuristic table should still render.
	if !strings.Contains(out.String(), "heuristic") {
		t.Errorf("expected heuristic table; got %q", out.String())
	}
}

// TestReasonsForFiltersEmpty is a tiny sanity check for the reasons map
// builder used by the TUI.
func TestReasonsForFiltersEmpty(t *testing.T) {
	in := []suggest.TurnScore{
		{TurnID: "t1", LLMReason: "bad"},
		{TurnID: "t2", LLMReason: ""},
		{TurnID: "t3", LLMReason: "worse"},
	}
	got := reasonsFor(in)
	if len(got) != 2 {
		t.Fatalf("expected 2 entries, got %d (%v)", len(got), got)
	}
	if got["t1"] != "bad" || got["t3"] != "worse" {
		t.Errorf("unexpected map contents: %v", got)
	}
	if _, ok := got["t2"]; ok {
		t.Errorf("empty reason should be filtered")
	}
}

// ensure json package stays referenced (for any future test that decodes
// `excise suggest --llm --json` output).
var _ = json.Marshal
