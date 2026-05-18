package suggest

import (
	"testing"

	"github.com/SuperMarioYL/excise/internal/session"
)

func TestScorePolluted(t *testing.T) {
	s, err := session.LoadAuto("../../testdata/claude_session_polluted.jsonl")
	if err != nil {
		t.Fatalf("load polluted fixture: %v", err)
	}
	if len(s.Turns) != 47 {
		t.Fatalf("expected 47 turns, got %d", len(s.Turns))
	}
	scores := Score(s)
	if len(scores) == 0 {
		t.Fatal("scorer returned no candidates on polluted fixture")
	}

	// Top-5 must include the planted failure ids: u-017, u-019, u-032, u-033, u-047.
	expected := map[string]bool{
		"u-017": true,
		"u-019": true,
		"u-032": true,
		"u-033": true,
		"u-047": true,
	}
	top := TopK(scores, 5, 0.0)
	got := map[string]bool{}
	for _, p := range top {
		got[p.TurnID] = true
	}
	for id := range expected {
		if !got[id] {
			t.Errorf("expected top-5 to include %s; got %v", id, ids(top))
		}
	}
}

func TestScoreClean(t *testing.T) {
	// On a clean simple session the scorer may surface 0–1 candidates;
	// it must NEVER panic and must NEVER mutate the input.
	s, err := session.LoadAuto("../../testdata/claude_session_simple.jsonl")
	if err != nil {
		t.Fatalf("load clean fixture: %v", err)
	}
	before := len(s.Turns)
	scores := Score(s)
	if len(s.Turns) != before {
		t.Errorf("scorer mutated input length: %d → %d", before, len(s.Turns))
	}
	// Clean session shouldn't have ANY 2000+ token turns, repeated edits,
	// corrections, or long drift, so we expect zero candidates.
	if len(scores) > 0 {
		t.Logf("clean session surfaced %d candidates (acceptable but unexpected): %+v", len(scores), scores)
	}
}

func TestTopK(t *testing.T) {
	in := []TurnScore{
		{TurnID: "a", Score: 3.0, Index: 1},
		{TurnID: "b", Score: 2.0, Index: 2},
		{TurnID: "c", Score: 1.0, Index: 3},
		{TurnID: "d", Score: 0.5, Index: 4},
	}
	if got := TopK(in, 2, 0); len(got) != 2 {
		t.Errorf("k=2 want 2 picks, got %d", len(got))
	}
	if got := TopK(in, 0, 0); len(got) != 4 {
		t.Errorf("k=0 want all, got %d", len(got))
	}
	if got := TopK(in, 5, 1.5); len(got) != 2 {
		t.Errorf("min-score=1.5 want 2 picks (a,b), got %d", len(got))
	}
}

func TestTopKIDs(t *testing.T) {
	in := []TurnScore{
		{TurnID: "a", Score: 3},
		{TurnID: "b", Score: 2},
	}
	ids := TopKIDs(in, 5, 0)
	if len(ids) != 2 || ids[0] != "a" || ids[1] != "b" {
		t.Errorf("TopKIDs = %v", ids)
	}
}

func TestTriggerSummary(t *testing.T) {
	ts := TurnScore{Triggers: []HeuristicTrigger{
		{ID: HHighTokenCost},
		{ID: HUserCorrectionFollowsUp},
		{ID: HHighTokenCost}, // dup
	}}
	got := TriggerSummary(ts)
	if got != HHighTokenCost+" + "+HUserCorrectionFollowsUp {
		t.Errorf("TriggerSummary = %q", got)
	}
}

func TestScoreCursorPolluted(t *testing.T) {
	// Cursor polluted fixture should also surface its planted failures.
	s, err := session.LoadAuto("../../testdata/cursor_session_polluted.jsonl")
	if err != nil {
		t.Fatalf("load cursor polluted: %v", err)
	}
	if len(s.Turns) != 47 {
		t.Fatalf("expected 47 cursor bubbles, got %d", len(s.Turns))
	}
	scores := Score(s)
	if len(scores) == 0 {
		t.Fatal("scorer returned no candidates on cursor polluted fixture")
	}
	top := TopK(scores, 5, 0.0)
	want := map[string]bool{
		"b-017": true,
		"b-032": true,
		"b-047": true,
	}
	got := map[string]bool{}
	for _, p := range top {
		got[p.TurnID] = true
	}
	missing := 0
	for id := range want {
		if !got[id] {
			t.Logf("cursor: top-5 missing %s (have %v)", id, ids(top))
			missing++
		}
	}
	if missing > 1 {
		t.Errorf("cursor top-5 missed %d planted failures (>1); IDs were %v", missing, ids(top))
	}
}

func ids(in []TurnScore) []string {
	out := make([]string, len(in))
	for i, p := range in {
		out[i] = p.TurnID
	}
	return out
}
