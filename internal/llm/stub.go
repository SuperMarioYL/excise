// stub.go — exported test helper visible across packages.
//
// Go's _test.go files compile only for their own package's test binary, so a
// helper defined in rerank_test.go is NOT visible to cmd/excise's tests. Test
// helpers that need to be reused from another package must live in a regular
// (non-_test) source file. That is this file.
//
// Behaviour is unchanged from the v0.3.0 builder's original definition; only
// the file location moved.

package llm

import (
	"context"

	"github.com/SuperMarioYL/excise/internal/session"
	"github.com/SuperMarioYL/excise/internal/suggest"
)

// stubReranker lets cmd-level tests inject a deterministic reordering
// without spinning up an HTTP server.
type stubReranker struct {
	replies []rerankReply
	err     error
}

// NewStubReranker is a test helper that returns a Reranker which:
//   - on err != nil, returns err immediately (use to exercise the fallback path)
//   - otherwise merges the supplied (turn_id, score, reason) tuples onto the
//     incoming shortlist via the same mergeReplies path the real reranker uses.
func NewStubReranker(err error, picks []struct {
	TurnID string
	Score  float64
	Reason string
}) Reranker {
	r := &stubReranker{err: err}
	for _, p := range picks {
		r.replies = append(r.replies, rerankReply{TurnID: p.TurnID, Score: p.Score, Reason: p.Reason})
	}
	return r
}

func (s *stubReranker) Rerank(_ context.Context, _ *session.Session, shortlist []suggest.TurnScore) ([]suggest.TurnScore, error) {
	if s.err != nil {
		return nil, s.err
	}
	return mergeReplies(shortlist, s.replies), nil
}
