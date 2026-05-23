// llm_pipeline.go — v0.3.0 glue between the v0.2 heuristic scorer and the
// optional Ollama rerank. Both `excise suggest --llm` and `excise pick
// --llm` enter through rankCandidates, which decides whether to invoke the
// LLM and degrades to the heuristic result on any failure.
//
// The Reranker is injected via a package-level factory so tests can swap
// in a stub without spinning up an HTTP server. Production code always
// uses ollamaRerankerFactory which wires through internal/llm.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/SuperMarioYL/excise/internal/config"
	"github.com/SuperMarioYL/excise/internal/llm"
	"github.com/SuperMarioYL/excise/internal/session"
	"github.com/SuperMarioYL/excise/internal/suggest"
)

// rerankerFactory builds a Reranker from the resolved config. It is a var
// so tests can replace it with a stub.
var rerankerFactory func(*config.Config) llm.Reranker = ollamaRerankerFactory

func ollamaRerankerFactory(cfg *config.Config) llm.Reranker {
	client := llm.NewOllamaClient(
		cfg.LLM.Host,
		cfg.LLM.Model,
		time.Duration(cfg.LLM.TimeoutSec)*time.Second,
	)
	return llm.NewOllamaReranker(client)
}

// rankResult is the union output of the heuristic+optional-LLM pipeline.
type rankResult struct {
	Picks        []suggest.TurnScore // ordered (highest score first)
	UsedLLM      bool                // true if the rerank actually ran (and merged)
	Fallback     bool                // true if --llm was passed but LLM was unreachable
	FallbackErr  error               // the failure reason (for the stderr warning)
	Model        string              // model tag actually used (for the table footer)
	Host         string              // host actually used
	SourceConfig string              // excise.toml path or "" for defaults
}

// rankCandidates runs the v0.2 heuristic scorer, optionally followed by
// the v0.3 LLM rerank. Errors from the LLM path are NOT propagated to the
// caller — they're recorded on rankResult.FallbackErr so the caller can
// emit a single stderr warning and continue with the heuristic ordering.
//
// stderr is the writer the caller would normally use for the warning;
// pass os.Stderr in production. We don't write here so the caller controls
// formatting and ordering relative to the rest of its output.
func rankCandidates(ctx context.Context, gf *globalFlags, s *session.Session, top int, minScore float64, stderr io.Writer) (*rankResult, error) {
	heur := suggest.TopK(suggest.Score(s), top, minScore)
	res := &rankResult{Picks: heur}

	if !gf.llm {
		return res, nil
	}

	cfg, err := config.Load()
	if err != nil {
		return res, fmt.Errorf("read excise.toml: %w", err)
	}
	if gf.llmModel != "" {
		cfg.LLM.Model = gf.llmModel
	}
	if gf.llmHost != "" {
		cfg.LLM.Host = gf.llmHost
		if vErr := cfg.Validate(); vErr != nil {
			return res, vErr
		}
	}
	res.Model = cfg.LLM.Model
	res.Host = cfg.LLM.Host
	res.SourceConfig = cfg.SourcePath

	if len(heur) == 0 {
		// Nothing to rerank — but record that --llm was honored.
		res.UsedLLM = true
		return res, nil
	}

	r := rerankerFactory(cfg)
	reranked, err := r.Rerank(ctx, s, heur)
	if err != nil {
		// Single stderr warning, then carry on with the heuristic result.
		if errors.Is(err, llm.ErrLLMUnavailable) || stderr == nil {
			res.Fallback = true
			res.FallbackErr = err
			if stderr != nil {
				fmt.Fprintf(stderr, "[excise] LLM unavailable (%v) — falling back to heuristic ranking\n", err)
			}
			return res, nil
		}
		// Non-Unavailable errors are still treated as recoverable to keep
		// the user un-blocked, but we flag them differently so a future
		// `--strict` mode could escalate.
		res.Fallback = true
		res.FallbackErr = err
		fmt.Fprintf(stderr, "[excise] LLM rerank failed (%v) — falling back to heuristic ranking\n", err)
		return res, nil
	}
	res.Picks = reranked
	res.UsedLLM = true
	return res, nil
}

// reasonsFor lifts the LLMReason fields off a rankResult into a map. Used
// to feed the TUI sidebar — the bubbletea model only needs id→string.
func reasonsFor(picks []suggest.TurnScore) map[string]string {
	out := map[string]string{}
	for _, p := range picks {
		if p.LLMReason == "" {
			continue
		}
		out[p.TurnID] = p.LLMReason
	}
	return out
}

// idsFor returns just the turn ids in score order. Replaces the inline
// suggest.TopKIDs call so we can feed an LLM-reranked result through the
// same TUI seed.
func idsFor(picks []suggest.TurnScore, k int, minScore float64) []string {
	out := make([]string, 0, len(picks))
	for _, p := range picks {
		if p.Score < minScore {
			continue
		}
		out = append(out, p.TurnID)
		if k > 0 && len(out) >= k {
			break
		}
	}
	return out
}

// rerankBackgroundContext is the default context used by the CLI entry
// points. The OllamaClient applies its own timeout, so we don't wrap.
func rerankBackgroundContext() (context.Context, context.CancelFunc) {
	// Hard ceiling: timeout_sec already applied inside OllamaClient, but
	// give the surrounding pipeline a generous absolute bound so a hung
	// goroutine eventually unblocks.
	return context.WithTimeout(context.Background(), 5*time.Minute)
}
