package llm

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/SuperMarioYL/excise/internal/suggest"
)

// stubReranker + NewStubReranker moved to stub.go (non-_test file) so
// cross-package callers (cmd/excise tests) can resolve the symbol. See
// stub.go for the implementation.

func shortlistFixture() []suggest.TurnScore {
	return []suggest.TurnScore{
		{TurnID: "t1", Index: 1, Role: "assistant", Tokens: 100, Score: 3.0, Preview: "a"},
		{TurnID: "t2", Index: 2, Role: "assistant", Tokens: 200, Score: 2.5, Preview: "b"},
		{TurnID: "t3", Index: 3, Role: "assistant", Tokens: 300, Score: 2.0, Preview: "c"},
	}
}

func TestMergeRepliesReorders(t *testing.T) {
	replies := []rerankReply{
		{TurnID: "t3", Score: 9.0, Reason: "actually the worst"},
		{TurnID: "t1", Score: 3.5, Reason: "still relevant"},
		{TurnID: "t2", Score: 1.0, Reason: "fine to keep"},
	}
	out := mergeReplies(shortlistFixture(), replies)
	if got := out[0].TurnID; got != "t3" {
		t.Errorf("expected t3 first, got %s", got)
	}
	if out[0].LLMReason == "" {
		t.Errorf("expected LLMReason populated, got empty")
	}
	if out[2].TurnID != "t2" {
		t.Errorf("expected t2 last, got %s", out[2].TurnID)
	}
}

func TestMergeRepliesKeepsTurnsLLMForgot(t *testing.T) {
	// LLM only mentions t2; t1 and t3 must still appear in the output.
	replies := []rerankReply{{TurnID: "t2", Score: 5.0, Reason: "very bad"}}
	out := mergeReplies(shortlistFixture(), replies)
	if len(out) != 3 {
		t.Fatalf("expected 3 turns, got %d", len(out))
	}
	if out[0].TurnID != "t2" {
		t.Errorf("expected t2 promoted, got %s", out[0].TurnID)
	}
	// t1 and t3 retain their heuristic scores (3.0, 2.0) and slot accordingly.
	if out[1].TurnID != "t1" || out[2].TurnID != "t3" {
		t.Errorf("unexpected tail ordering: %s, %s", out[1].TurnID, out[2].TurnID)
	}
}

func TestMergeRepliesDropsInventedTurnIDs(t *testing.T) {
	replies := []rerankReply{
		{TurnID: "t99", Score: 100, Reason: "model hallucinated this id"},
		{TurnID: "t1", Score: 5, Reason: "ok"},
	}
	out := mergeReplies(shortlistFixture(), replies)
	for _, ts := range out {
		if ts.TurnID == "t99" {
			t.Fatalf("merge should drop invented turn ids; got %+v", ts)
		}
	}
}

func TestParseRerankReplyBareArray(t *testing.T) {
	raw := `[{"turn_id":"t1","score":1.0,"reason":"meh"}]`
	out, err := parseRerankReply(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(out) != 1 || out[0].TurnID != "t1" {
		t.Fatalf("unexpected parsed output: %+v", out)
	}
}

func TestParseRerankReplyFencedJSON(t *testing.T) {
	raw := "```json\n[{\"turn_id\":\"t1\",\"score\":1,\"reason\":\"x\"}]\n```"
	out, err := parseRerankReply(raw)
	if err != nil {
		t.Fatalf("parse fenced: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("expected 1, got %d", len(out))
	}
}

func TestParseRerankReplyWrappedResults(t *testing.T) {
	raw := `{"results":[{"turn_id":"t1","score":2,"reason":"ok"}]}`
	out, err := parseRerankReply(raw)
	if err != nil {
		t.Fatalf("parse wrapped: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("expected 1, got %d", len(out))
	}
}

func TestParseRerankReplyGarbageFailsAsUnavailable(t *testing.T) {
	raw := "I think turn 1 is the worst, but turn 2 isn't great either."
	_, err := parseRerankReply(raw)
	if !errors.Is(err, ErrLLMUnavailable) {
		t.Fatalf("expected ErrLLMUnavailable, got %v", err)
	}
}

func TestParseRerankReplyEmpty(t *testing.T) {
	_, err := parseRerankReply("")
	if !errors.Is(err, ErrLLMUnavailable) {
		t.Fatalf("empty body must map to ErrLLMUnavailable, got %v", err)
	}
}

func TestStubRerankerErrorPath(t *testing.T) {
	r := NewStubReranker(ErrLLMUnavailable, nil)
	_, err := r.Rerank(context.Background(), nil, shortlistFixture())
	if !errors.Is(err, ErrLLMUnavailable) {
		t.Fatalf("expected unavailable error, got %v", err)
	}
}

func TestOllamaClientHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "model not found", http.StatusNotFound)
	}))
	defer srv.Close()

	c := NewOllamaClient(srv.URL, "missing-model", 2*time.Second)
	_, err := c.Generate(context.Background(), "hi")
	if !errors.Is(err, ErrLLMUnavailable) {
		t.Fatalf("expected ErrLLMUnavailable on 404, got %v", err)
	}
}

func TestOllamaClientHappyPath(t *testing.T) {
	want := `[{"turn_id":"t1","score":1.0,"reason":"ok"}]`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/generate" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model":    "stub",
			"response": want,
			"done":     true,
		})
	}))
	defer srv.Close()

	c := NewOllamaClient(srv.URL, "stub", 2*time.Second)
	got, err := c.Generate(context.Background(), "rerank please")
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if !strings.Contains(got, "t1") {
		t.Errorf("unexpected response %q", got)
	}
}

func TestOllamaClientTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		_, _ = w.Write([]byte(`{"response":"too late"}`))
	}))
	defer srv.Close()

	c := NewOllamaClient(srv.URL, "stub", 25*time.Millisecond)
	_, err := c.Generate(context.Background(), "rerank")
	if !errors.Is(err, ErrLLMUnavailable) {
		t.Fatalf("timeout must map to ErrLLMUnavailable, got %v", err)
	}
}

// TestLiveOllama exercises a real local Ollama. Skipped unless
// EXCISE_LIVE_OLLAMA=1 is set in the environment — CI does not run this.
func TestLiveOllama(t *testing.T) {
	if os.Getenv("EXCISE_LIVE_OLLAMA") != "1" {
		t.Skip("set EXCISE_LIVE_OLLAMA=1 to run against the real local Ollama")
	}
	host := os.Getenv("EXCISE_LIVE_OLLAMA_HOST")
	if host == "" {
		host = "http://localhost:11434"
	}
	model := os.Getenv("EXCISE_LIVE_OLLAMA_MODEL")
	if model == "" {
		model = "llama3.2"
	}
	c := NewOllamaClient(host, model, 30*time.Second)
	out, err := c.Generate(context.Background(), `Reply with the JSON array: [{"turn_id":"t1","score":1.0,"reason":"hi"}]`)
	if err != nil {
		t.Fatalf("live ollama: %v", err)
	}
	if !strings.Contains(out, "t1") {
		t.Errorf("live ollama did not echo expected token; response: %q", out)
	}
}
