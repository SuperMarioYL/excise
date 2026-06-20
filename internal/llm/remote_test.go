package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SuperMarioYL/excise/internal/session"
	"github.com/SuperMarioYL/excise/internal/suggest"
)

// remoteFixtureShortlist loads the v0.4 remote-rerank fixture and builds a real
// shortlist from it, so the stubbed remote backend is exercised against an
// actual (zh-CN-containing) Claude transcript rather than a synthetic slice.
func remoteFixtureShortlist(t *testing.T) (*session.Session, []suggest.TurnScore) {
	t.Helper()
	path := filepath.Join("..", "..", "testdata", "claude_session_remote_rerank.jsonl")
	s, err := session.LoadWithTool(session.ToolClaude, path)
	if err != nil {
		t.Fatalf("load remote fixture: %v", err)
	}
	scored := suggest.Score(s)
	if len(scored) == 0 {
		t.Fatalf("fixture produced no scored turns")
	}
	if len(scored) > 3 {
		scored = scored[:3]
	}
	return s, scored
}

// TestRemoteRerankerOpenAIHappyPath drives the OpenAI/OpenRouter wire shape via
// an httptest server (no live key) and asserts the reply is parsed + merged.
func TestRemoteRerankerOpenAIHappyPath(t *testing.T) {
	s, shortlist := remoteFixtureShortlist(t)
	first := shortlist[0].TurnID

	reply := `[{"turn_id":"` + first + `","score":9.9,"reason":"stubbed remote rerank"}]`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer sk-test" {
			t.Errorf("missing/wrong auth header: %q", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]string{"role": "assistant", "content": reply}},
			},
		})
	}))
	defer srv.Close()

	var echo bytes.Buffer
	client := NewRemoteClient(RemoteConfig{
		Provider: "openai",
		BaseURL:  srv.URL,
		Model:    "gpt-4o-mini",
		APIKey:   "sk-test",
		Timeout:  2 * time.Second,
		Stderr:   &echo,
	})
	rr := NewRemoteReranker(client)

	out, err := rr.Rerank(context.Background(), s, shortlist)
	if err != nil {
		t.Fatalf("remote rerank: %v", err)
	}
	if out[0].TurnID != first {
		t.Errorf("expected %s promoted to front, got %s", first, out[0].TurnID)
	}
	// Trust contract: the destination host must be echoed to stderr.
	if !strings.Contains(echo.String(), "host=") {
		t.Errorf("remote call did not echo destination host; stderr=%q", echo.String())
	}
}

// TestRemoteRerankerAnthropicHappyPath drives the Anthropic messages shape.
func TestRemoteRerankerAnthropicHappyPath(t *testing.T) {
	s, shortlist := remoteFixtureShortlist(t)
	first := shortlist[0].TurnID

	reply := `[{"turn_id":"` + first + `","score":8.0,"reason":"anthropic stub"}]`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		if got := r.Header.Get("x-api-key"); got != "sk-ant" {
			t.Errorf("missing/wrong x-api-key: %q", got)
		}
		if got := r.Header.Get("anthropic-version"); got == "" {
			t.Errorf("missing anthropic-version header")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"content": []map[string]any{
				{"type": "text", "text": reply},
			},
		})
	}))
	defer srv.Close()

	client := NewRemoteClient(RemoteConfig{
		Provider: "anthropic",
		BaseURL:  srv.URL,
		Model:    "claude-3-5-haiku-latest",
		APIKey:   "sk-ant",
		Timeout:  2 * time.Second,
		Stderr:   io.Discard,
	})
	out, err := NewRemoteReranker(client).Rerank(context.Background(), s, shortlist)
	if err != nil {
		t.Fatalf("anthropic remote rerank: %v", err)
	}
	if out[0].TurnID != first {
		t.Errorf("expected %s promoted, got %s", first, out[0].TurnID)
	}
}

// TestRemoteRerankerAuthFailureFallsBack asserts a 401 collapses into
// ErrLLMUnavailable so the caller's single fallback branch handles it.
func TestRemoteRerankerAuthFailureFallsBack(t *testing.T) {
	s, shortlist := remoteFixtureShortlist(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"invalid api key"}`, http.StatusUnauthorized)
	}))
	defer srv.Close()

	client := NewRemoteClient(RemoteConfig{
		Provider: "openai",
		BaseURL:  srv.URL,
		Model:    "gpt-4o-mini",
		APIKey:   "sk-bad",
		Timeout:  2 * time.Second,
		Stderr:   io.Discard,
	})
	_, err := NewRemoteReranker(client).Rerank(context.Background(), s, shortlist)
	if !errors.Is(err, ErrLLMUnavailable) {
		t.Fatalf("401 must map to ErrLLMUnavailable, got %v", err)
	}
}

// TestRemoteRerankerMissingKeyFallsBack — no key supplied → fallback sentinel,
// and crucially no outbound request is made.
func TestRemoteRerankerMissingKeyFallsBack(t *testing.T) {
	s, shortlist := remoteFixtureShortlist(t)
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	defer srv.Close()

	client := NewRemoteClient(RemoteConfig{
		Provider: "openai",
		BaseURL:  srv.URL,
		Model:    "gpt-4o-mini",
		APIKey:   "", // missing
		Stderr:   io.Discard,
	})
	_, err := NewRemoteReranker(client).Rerank(context.Background(), s, shortlist)
	if !errors.Is(err, ErrLLMUnavailable) {
		t.Fatalf("missing key must map to ErrLLMUnavailable, got %v", err)
	}
	if called {
		t.Errorf("no outbound request should be made when the api key is missing")
	}
}

// TestRemoteRerankerTimeoutFallsBack — a slow endpoint maps to the sentinel.
func TestRemoteRerankerTimeoutFallsBack(t *testing.T) {
	s, shortlist := remoteFixtureShortlist(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		_, _ = w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	client := NewRemoteClient(RemoteConfig{
		Provider: "openai",
		BaseURL:  srv.URL,
		Model:    "gpt-4o-mini",
		APIKey:   "sk-test",
		Timeout:  25 * time.Millisecond,
		Stderr:   io.Discard,
	})
	_, err := NewRemoteReranker(client).Rerank(context.Background(), s, shortlist)
	if !errors.Is(err, ErrLLMUnavailable) {
		t.Fatalf("timeout must map to ErrLLMUnavailable, got %v", err)
	}
}

// TestLiveRemote exercises a real remote provider. Skipped unless
// EXCISE_LIVE_REMOTE=1 — CI never runs this (no live key in CI).
func TestLiveRemote(t *testing.T) {
	if os.Getenv("EXCISE_LIVE_REMOTE") != "1" {
		t.Skip("set EXCISE_LIVE_REMOTE=1 (and provider env) to run against a real remote backend")
	}
	provider := os.Getenv("EXCISE_LIVE_REMOTE_PROVIDER")
	if provider == "" {
		provider = "openai"
	}
	key := os.Getenv("EXCISE_LIVE_REMOTE_KEY")
	if key == "" {
		t.Fatal("EXCISE_LIVE_REMOTE=1 but EXCISE_LIVE_REMOTE_KEY is unset")
	}
	model := os.Getenv("EXCISE_LIVE_REMOTE_MODEL")
	if model == "" {
		model = "gpt-4o-mini"
	}
	s, shortlist := remoteFixtureShortlist(t)
	client := NewRemoteClient(RemoteConfig{
		Provider: provider,
		BaseURL:  os.Getenv("EXCISE_LIVE_REMOTE_BASE"),
		Model:    model,
		APIKey:   key,
		Timeout:  30 * time.Second,
	})
	out, err := NewRemoteReranker(client).Rerank(context.Background(), s, shortlist)
	if err != nil {
		t.Fatalf("live remote rerank: %v", err)
	}
	if len(out) != len(shortlist) {
		t.Errorf("expected %d turns back, got %d", len(shortlist), len(out))
	}
}
