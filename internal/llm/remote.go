// remote.go — v0.4 opt-in remote rerank backend.
//
// This file adds a SECOND implementation of the existing Reranker interface
// (see rerank.go) for the three remote providers the plan calls out:
//
//   - openai     — POST <base>/v1/chat/completions  (OpenAI chat shape)
//   - openrouter — same wire shape as OpenAI (OpenRouter is OpenAI-compatible)
//   - anthropic  — POST <base>/v1/messages          (Anthropic messages shape)
//
// It is a NEW package member, not a refactor: cmd/excise selects which
// concrete Reranker to construct, and the rerank call site is unchanged.
//
// Trust contract (README + plan §v0.4 trust contract):
//
//   - The remote path is reachable ONLY when the user sets backend=remote AND
//     supplies a key. The default backend stays local Ollama.
//   - The destination host is echoed to stderr on EVERY remote call so an
//     outbound call is never silent.
//   - Any failure (auth, timeout, non-2xx, parse) maps to ErrLLMUnavailable —
//     the SAME sentinel the Ollama path returns — so the caller's existing
//     fallback branch works unchanged.
//
// We use net/http with no provider SDK: each provider is one POST with a
// small JSON body, and a dependency would be more to audit than the code
// below.
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/SuperMarioYL/excise/internal/session"
	"github.com/SuperMarioYL/excise/internal/suggest"
)

// Default provider hosts, used when the user does not set base_url.
const (
	defaultOpenAIBase     = "https://api.openai.com"
	defaultOpenRouterBase = "https://openrouter.ai/api"
	defaultAnthropicBase  = "https://api.anthropic.com"

	anthropicVersion = "2023-06-01"
)

// RemoteConfig is the resolved transport configuration for a remote call.
// It is deliberately flat so cmd/excise can build it straight from the
// config.LLM block + CLI overrides.
type RemoteConfig struct {
	Provider string        // openai | anthropic | openrouter
	BaseURL  string        // optional override; empty → provider default
	Model    string        // model id, e.g. gpt-4o-mini / claude-3-5-haiku-latest
	APIKey   string        // already-resolved key (never logged)
	Timeout  time.Duration // per-call timeout; 0 means no limit
	HTTP     *http.Client  // optional injection for tests
	Stderr   io.Writer     // where the host-echo line is written; nil → os.Stderr
}

// baseURL returns the effective base host for the provider.
func (c RemoteConfig) baseURL() string {
	if c.BaseURL != "" {
		return strings.TrimRight(c.BaseURL, "/")
	}
	switch c.Provider {
	case "anthropic":
		return defaultAnthropicBase
	case "openrouter":
		return defaultOpenRouterBase
	default:
		return defaultOpenAIBase
	}
}

// host returns just the host portion of the destination, for the stderr
// trust echo. Falls back to the full base on parse failure.
func (c RemoteConfig) host() string {
	b := c.baseURL()
	if u, err := url.Parse(b); err == nil && u.Host != "" {
		return u.Scheme + "://" + u.Host
	}
	return b
}

// RemoteHost returns the destination host (scheme://host) for a remote
// backend given the provider and optional base_url override, without
// constructing a client. Used by callers that need to report the
// trust-echo host on stdout (e.g. the `excise suggest` table footer and
// the `excise pick` pre-mark line) in addition to the stderr echo emitted
// inside Generate. This reuses the same host() logic so the two surfaces
// can never disagree about where the outbound call is going.
func RemoteHost(provider, baseURL string) string {
	return RemoteConfig{Provider: provider, BaseURL: baseURL}.host()
}

// RemoteClient is the tiny HTTP shim around a provider's chat endpoint.
type RemoteClient struct {
	cfg RemoteConfig
}

// NewRemoteClient is the canonical constructor.
func NewRemoteClient(cfg RemoteConfig) *RemoteClient {
	return &RemoteClient{cfg: cfg}
}

// RemoteReranker implements the same Reranker interface as OllamaReranker.
// It renders the identical rerank prompt (prompt.go) and parses the reply
// with the identical parser, so the only difference from the Ollama path is
// transport + auth.
type RemoteReranker struct {
	Client *RemoteClient
}

// NewRemoteReranker is the canonical constructor.
func NewRemoteReranker(client *RemoteClient) *RemoteReranker {
	return &RemoteReranker{Client: client}
}

// Rerank implements the Reranker contract.
func (r *RemoteReranker) Rerank(ctx context.Context, s *session.Session, shortlist []suggest.TurnScore) ([]suggest.TurnScore, error) {
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

// Generate sends the prompt to the configured provider and returns the
// model's text response. Every error path maps to ErrLLMUnavailable (wrapped
// with the cause for diagnostics) so the caller falls back deterministically.
//
// The destination host is echoed to stderr BEFORE the request is sent — the
// trust posture is that an outbound call is never silent, even if it then
// fails.
func (c *RemoteClient) Generate(ctx context.Context, prompt string) (string, error) {
	if c == nil || c.cfg.Model == "" {
		return "", fmt.Errorf("%w: missing model", ErrLLMUnavailable)
	}
	if c.cfg.APIKey == "" {
		return "", fmt.Errorf("%w: missing api key", ErrLLMUnavailable)
	}

	stderr := c.cfg.Stderr
	if stderr == nil {
		stderr = os.Stderr
	}
	// Trust echo — every remote call announces its destination host.
	fmt.Fprintf(stderr, "[excise] remote rerank via %s (host=%s) — model %s\n",
		c.cfg.Provider, c.cfg.host(), c.cfg.Model)

	if c.cfg.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.cfg.Timeout)
		defer cancel()
	}

	endpoint, body, headers, err := c.buildRequest(prompt)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("%w: build request: %v", ErrLLMUnavailable, err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	client := c.cfg.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrLLMUnavailable, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode/100 != 2 {
		buf, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		// 401/403 are auth failures; they collapse into the same sentinel so
		// the caller's single fallback branch handles them.
		return "", fmt.Errorf("%w: %s http %d: %s", ErrLLMUnavailable, c.cfg.Provider, resp.StatusCode, bytes.TrimSpace(buf))
	}

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1 MiB ceiling
	if err != nil {
		return "", fmt.Errorf("%w: read body: %v", ErrLLMUnavailable, err)
	}

	text, err := c.extractText(raw)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(text) == "" {
		return "", fmt.Errorf("%w: empty response from %s", ErrLLMUnavailable, c.cfg.Model)
	}
	return text, nil
}

// buildRequest returns the endpoint, JSON body, and auth headers for the
// configured provider.
func (c *RemoteClient) buildRequest(prompt string) (endpoint string, body []byte, headers map[string]string, err error) {
	base := c.cfg.baseURL()
	switch c.cfg.Provider {
	case "anthropic":
		endpoint = base + "/v1/messages"
		payload := anthropicRequest{
			Model:     c.cfg.Model,
			MaxTokens: 1024,
			Messages: []anthropicMessage{
				{Role: "user", Content: prompt},
			},
		}
		body, err = json.Marshal(payload)
		headers = map[string]string{
			"x-api-key":         c.cfg.APIKey,
			"anthropic-version": anthropicVersion,
		}
	default: // openai + openrouter share the OpenAI chat shape
		endpoint = base + "/v1/chat/completions"
		payload := openAIRequest{
			Model:       c.cfg.Model,
			Temperature: 0.1,
			Messages: []openAIMessage{
				{Role: "user", Content: prompt},
			},
		}
		body, err = json.Marshal(payload)
		headers = map[string]string{
			"Authorization": "Bearer " + c.cfg.APIKey,
		}
	}
	if err != nil {
		return "", nil, nil, fmt.Errorf("%w: marshal request: %v", ErrLLMUnavailable, err)
	}
	return endpoint, body, headers, nil
}

// extractText pulls the assistant's text out of the provider response shape.
func (c *RemoteClient) extractText(raw []byte) (string, error) {
	if c.cfg.Provider == "anthropic" {
		var out anthropicResponse
		if err := json.Unmarshal(raw, &out); err != nil {
			return "", fmt.Errorf("%w: decode body: %v", ErrLLMUnavailable, err)
		}
		var b strings.Builder
		for _, blk := range out.Content {
			if blk.Type == "text" {
				b.WriteString(blk.Text)
			}
		}
		return b.String(), nil
	}
	var out openAIResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("%w: decode body: %v", ErrLLMUnavailable, err)
	}
	if len(out.Choices) == 0 {
		return "", fmt.Errorf("%w: no choices in response", ErrLLMUnavailable)
	}
	return out.Choices[0].Message.Content, nil
}

// --- OpenAI / OpenRouter wire types (only the fields we use) ---

type openAIRequest struct {
	Model       string          `json:"model"`
	Temperature float64         `json:"temperature"`
	Messages    []openAIMessage `json:"messages"`
}

type openAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAIResponse struct {
	Choices []struct {
		Message openAIMessage `json:"message"`
	} `json:"choices"`
}

// --- Anthropic wire types (only the fields we use) ---

type anthropicRequest struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"`
	Messages  []anthropicMessage `json:"messages"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicResponse struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
}
