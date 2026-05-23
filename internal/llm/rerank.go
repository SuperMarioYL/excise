package llm

import (
	"context"
	"sort"

	"github.com/SuperMarioYL/excise/internal/session"
	"github.com/SuperMarioYL/excise/internal/suggest"
)

// Reranker is the v0.3 interface — Rerank takes the v0.2 heuristic shortlist
// and returns a reordered copy with each entry's LLMReason populated.
//
// The interface is intentionally tiny so adding a v0.4 remote backend (or a
// stub for tests) is a single method, not a re-wiring of the CLI.
type Reranker interface {
	Rerank(ctx context.Context, s *session.Session, shortlist []suggest.TurnScore) ([]suggest.TurnScore, error)
}

// OllamaReranker is the production implementation. It defers all transport
// to OllamaClient and all parsing to parseRerankReply, then merges the
// returned scores/reasons back onto the original TurnScore records.
//
// If the LLM returns a turn id we don't recognise, that entry is dropped
// (we never invent turns). If the LLM omits a turn id from the input, we
// keep it at its heuristic position with an empty reason — better partial
// information than total fallback.
type OllamaReranker struct {
	Client *OllamaClient
}

// NewOllamaReranker is the canonical constructor.
func NewOllamaReranker(client *OllamaClient) *OllamaReranker {
	return &OllamaReranker{Client: client}
}

// Rerank implements the Reranker contract.
func (r *OllamaReranker) Rerank(ctx context.Context, s *session.Session, shortlist []suggest.TurnScore) ([]suggest.TurnScore, error) {
	if r == nil || r.Client == nil {
		return nil, ErrLLMUnavailable
	}
	if len(shortlist) == 0 {
		return shortlist, nil
	}
	prompt := renderRerankPrompt(s, shortlist)
	raw, err := r.Client.Generate(ctx, prompt)
	if err != nil {
		return nil, err
	}
	replies, err := parseRerankReply(raw)
	if err != nil {
		return nil, err
	}
	return mergeReplies(shortlist, replies), nil
}

// mergeReplies builds the reordered TurnScore slice. It is split out for
// unit-testability — the test feeds canned replies in and asserts the
// merged output ordering without needing a real LLM.
func mergeReplies(shortlist []suggest.TurnScore, replies []rerankReply) []suggest.TurnScore {
	byID := make(map[string]suggest.TurnScore, len(shortlist))
	for _, ts := range shortlist {
		byID[ts.TurnID] = ts
	}

	out := make([]suggest.TurnScore, 0, len(shortlist))
	seen := map[string]bool{}
	for _, r := range replies {
		ts, ok := byID[r.TurnID]
		if !ok || seen[r.TurnID] {
			continue
		}
		seen[r.TurnID] = true
		// Override the score but keep the heuristic triggers intact — the
		// table renderer still wants to show what fired.
		ts.Score = r.Score
		ts.LLMReason = r.Reason
		out = append(out, ts)
	}
	// Tack on any turns the LLM forgot to mention, in their original order.
	for _, ts := range shortlist {
		if seen[ts.TurnID] {
			continue
		}
		out = append(out, ts)
	}

	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Score > out[j].Score
	})
	return out
}
