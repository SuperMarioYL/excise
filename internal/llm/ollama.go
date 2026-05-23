// Package llm implements Excise's v0.3 opt-in rerank against a local Ollama
// host. Everything in this package is reachable only when the user passes
// `--llm`; the v0.2 heuristic path never imports it.
//
// The trust contract documented in the README is enforced here:
//
//   - the only outbound HTTP call is to the host configured in excise.toml
//     (default http://localhost:11434), nothing else
//   - timeouts are bounded by the caller-supplied context (default 20s)
//   - any failure (network, HTTP status, JSON parse) is mapped to
//     ErrLLMUnavailable so callers can fall back deterministically without
//     having to recognise every failure mode
//
// We intentionally use net/http with no third-party SDK — Ollama's
// /api/generate is a single endpoint, the JSON shape is tiny, and adding a
// dependency is more code to audit than the 30 lines below.
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// ErrLLMUnavailable is the single sentinel error this package returns when
// anything goes wrong: HTTP failure, non-2xx response, timeout, body too
// short, JSON parse failure. Callers compare with errors.Is.
var ErrLLMUnavailable = errors.New("llm: backend unavailable")

// OllamaClient is the tiny HTTP shim around Ollama's /api/generate.
// Construction is cheap and concurrency-safe; one instance per rerank call
// is fine.
type OllamaClient struct {
	Host    string        // base URL, e.g. http://localhost:11434
	Model   string        // model tag, e.g. llama3.2
	Timeout time.Duration // per-call timeout; 0 means no limit (not recommended)
	HTTP    *http.Client  // optional injection for tests
}

// NewOllamaClient is the canonical constructor.
func NewOllamaClient(host, model string, timeout time.Duration) *OllamaClient {
	return &OllamaClient{
		Host:    host,
		Model:   model,
		Timeout: timeout,
	}
}

// generateRequest mirrors Ollama's documented payload. We pin
// stream=false because the rerank is a one-shot request/response and the
// caller wants a complete body, not chunks.
type generateRequest struct {
	Model  string                 `json:"model"`
	Prompt string                 `json:"prompt"`
	Stream bool                   `json:"stream"`
	Format string                 `json:"format,omitempty"`
	Options map[string]any        `json:"options,omitempty"`
}

// generateResponse covers the fields we actually use. Ollama returns more
// (eval_count, prompt_eval_duration, …) but we ignore the rest.
type generateResponse struct {
	Model    string `json:"model"`
	Response string `json:"response"`
	Done     bool   `json:"done"`
}

// Generate sends a single prompt and returns the model's full text response.
// Any error path maps to ErrLLMUnavailable wrapped with the original cause
// for diagnostic logging, so callers can `errors.Is(err, ErrLLMUnavailable)`
// without checking every leaf.
func (c *OllamaClient) Generate(ctx context.Context, prompt string) (string, error) {
	if c == nil || c.Host == "" || c.Model == "" {
		return "", fmt.Errorf("%w: missing host or model", ErrLLMUnavailable)
	}

	if c.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.Timeout)
		defer cancel()
	}

	body, err := json.Marshal(generateRequest{
		Model:  c.Model,
		Prompt: prompt,
		Stream: false,
		Format: "json",
		Options: map[string]any{
			// Low temperature: we want consistent JSON, not creativity.
			"temperature": 0.1,
		},
	})
	if err != nil {
		return "", fmt.Errorf("%w: marshal request: %v", ErrLLMUnavailable, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Host+"/api/generate", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("%w: build request: %v", ErrLLMUnavailable, err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := c.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrLLMUnavailable, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode/100 != 2 {
		// Drain a short snippet for diagnostics; cap to keep logs bounded.
		buf, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", fmt.Errorf("%w: ollama http %d: %s", ErrLLMUnavailable, resp.StatusCode, bytes.TrimSpace(buf))
	}

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1 MiB hard ceiling
	if err != nil {
		return "", fmt.Errorf("%w: read body: %v", ErrLLMUnavailable, err)
	}

	var out generateResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("%w: decode body: %v", ErrLLMUnavailable, err)
	}
	if out.Response == "" {
		return "", fmt.Errorf("%w: empty response from %s", ErrLLMUnavailable, c.Model)
	}
	return out.Response, nil
}
