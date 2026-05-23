package suggest

import (
	"sort"
	"strings"

	"github.com/SuperMarioYL/excise/internal/session"
)

// TurnScore is one row in the scorer's output — a turn id, its preview, its
// total score, and the list of heuristics that fired on it.
//
// LLMReason is empty in v0.2 output and populated only when the v0.3
// `--llm` rerank ran. Downstream renderers MUST treat an empty value as
// "no LLM verdict" rather than as a missing field.
type TurnScore struct {
	TurnID    string             `json:"turn_id"`
	Index     int                `json:"index"`     // 1-based position in the session
	Role      string             `json:"role"`
	Tokens    int                `json:"tokens"`
	Score     float64            `json:"score"`
	Triggers  []HeuristicTrigger `json:"triggers"`
	Preview   string             `json:"preview"`
	LLMReason string             `json:"llm_reason,omitempty"`
}

// Score runs the 5 heuristics over s.Turns and returns one TurnScore per
// turn that fired at least one trigger, sorted by Score descending and
// ties broken by original position (earlier first).
//
// Score is a pure function — it never mutates the Session.
func Score(s *session.Session) []TurnScore {
	if s == nil || len(s.Turns) == 0 {
		return nil
	}
	turns := s.Turns

	merged := map[string][]HeuristicTrigger{}
	for id, ts := range detectHighTokenCost(turns) {
		merged[id] = append(merged[id], ts...)
	}
	for id, ts := range detectRepeatedFileEdit(turns) {
		merged[id] = append(merged[id], ts...)
	}
	for id, ts := range detectUserCorrectionFollowsUp(turns) {
		merged[id] = append(merged[id], ts...)
	}
	for id, ts := range detectToolUseErrorFollowedByCorrection(turns) {
		merged[id] = append(merged[id], ts...)
	}
	for id, ts := range detectLongDriftNoToolCalls(turns) {
		merged[id] = append(merged[id], ts...)
	}

	indexOf := make(map[string]int, len(turns))
	for i, t := range turns {
		indexOf[t.ID] = i
	}

	out := make([]TurnScore, 0, len(merged))
	for _, t := range turns {
		trigs, ok := merged[t.ID]
		if !ok || len(trigs) == 0 {
			continue
		}
		// Deduplicate trigger IDs — same heuristic firing twice on the same
		// turn (e.g. two RepeatedFileEdit windows overlapping) only counts
		// once; we keep the highest-score copy.
		trigs = dedupeTriggers(trigs)
		total := 0.0
		for _, tr := range trigs {
			total += tr.Score
		}
		out = append(out, TurnScore{
			TurnID:   t.ID,
			Index:    indexOf[t.ID] + 1,
			Role:     string(t.Role),
			Tokens:   t.TokenEst,
			Score:    total,
			Triggers: trigs,
			Preview:  t.Preview,
		})
	}

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].Index < out[j].Index
	})
	return out
}

// TopK returns the first k TurnScore entries with Score ≥ minScore. If
// k ≤ 0, all entries are returned.
func TopK(scores []TurnScore, k int, minScore float64) []TurnScore {
	out := make([]TurnScore, 0, len(scores))
	for _, s := range scores {
		if s.Score < minScore {
			continue
		}
		out = append(out, s)
		if k > 0 && len(out) >= k {
			break
		}
	}
	return out
}

// TopKIDs is the same as TopK but returns just the turn ids — used to seed
// the TUI's preMarked set.
func TopKIDs(scores []TurnScore, k int, minScore float64) []string {
	picks := TopK(scores, k, minScore)
	ids := make([]string, 0, len(picks))
	for _, p := range picks {
		ids = append(ids, p.TurnID)
	}
	return ids
}

// TriggerSummary joins trigger IDs with " + " in a stable order, suitable
// for table output.
func TriggerSummary(t TurnScore) string {
	if len(t.Triggers) == 0 {
		return ""
	}
	ids := make([]string, 0, len(t.Triggers))
	seen := map[string]bool{}
	for _, tr := range t.Triggers {
		if seen[tr.ID] {
			continue
		}
		seen[tr.ID] = true
		ids = append(ids, tr.ID)
	}
	return strings.Join(ids, " + ")
}

func dedupeTriggers(in []HeuristicTrigger) []HeuristicTrigger {
	best := map[string]HeuristicTrigger{}
	order := []string{}
	for _, t := range in {
		cur, ok := best[t.ID]
		if !ok {
			best[t.ID] = t
			order = append(order, t.ID)
			continue
		}
		if t.Score > cur.Score {
			best[t.ID] = t
		}
	}
	out := make([]HeuristicTrigger, 0, len(order))
	for _, id := range order {
		out = append(out, best[id])
	}
	return out
}
