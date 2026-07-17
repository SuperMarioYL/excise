package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SuperMarioYL/excise/internal/config"
	"github.com/SuperMarioYL/excise/internal/llm"
	"github.com/SuperMarioYL/excise/internal/session"
	"github.com/SuperMarioYL/excise/internal/suggest"
)

// TestEmitSuggestTableFooterReportsRealBackend locks in
// fix_backend_label_host_echo: when the remote backend ran, the stdout footer
// must render res.Backend ("remote") + the REAL remote destination host (via
// llm.RemoteHost), not a hardcoded "ollama" + the Ollama localhost default
// that used to contradict the stderr trust echo.
func TestEmitSuggestTableFooterReportsRealBackend(t *testing.T) {
	s := &session.Session{
		Tool:       session.ToolClaude,
		SessionID:  "s1",
		SourcePath: "x.jsonl",
		Turns: []session.Turn{{
			ID:       "t1",
			Role:     session.RoleAssistant,
			TokenEst: 3000,
			Preview:  "p",
		}},
	}
	picks := []suggest.TurnScore{{
		TurnID:    "t1",
		Index:     1,
		Role:      "assistant",
		Tokens:    3000,
		Score:     3.0,
		Triggers:  []suggest.HeuristicTrigger{{ID: "high_token_cost", Score: 1.4, Reason: "r"}},
		Preview:   "p",
		LLMReason: "model said: cut",
	}}
	res := &rankResult{
		Picks:   picks,
		UsedLLM: true,
		Backend: config.BackendRemote,
		Model:   "gpt-4o-mini",
		Host:    llm.RemoteHost(config.ProviderOpenAI, ""), // resolve to https://api.openai.com
	}

	out := filepath.Join(t.TempDir(), "out.txt")
	f, err := os.OpenFile(out, os.O_CREATE|os.O_TRUNC|os.O_RDWR, 0o644)
	if err != nil {
		t.Fatalf("open out: %v", err)
	}
	if err := emitSuggestTable(f, s, res); err != nil {
		t.Fatalf("emitSuggestTable: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	body := string(got)
	want := "reranked by remote:gpt-4o-mini (host=https://api.openai.com)"
	if !strings.Contains(body, want) {
		t.Errorf("footer must report the real backend/host; want %q\n got:\n%s", want, body)
	}
	if strings.Contains(body, "ollama") {
		t.Errorf("footer must NOT hardcode ollama when the remote backend ran; got:\n%s", body)
	}
	if strings.Contains(body, "localhost") {
		t.Errorf("footer must NOT show the Ollama localhost host on a remote call; got:\n%s", body)
	}
}

// TestEmitSuggestTableFooterOllamaUnchanged guards the bit-identical Ollama
// path: the footer still reads "ollama:..." with the Ollama host when the
// default backend ran (no regression from the remote fix).
func TestEmitSuggestTableFooterOllamaUnchanged(t *testing.T) {
	s := &session.Session{
		Tool:       session.ToolClaude,
		SessionID:  "s1",
		SourcePath: "x.jsonl",
		Turns: []session.Turn{{
			ID:       "t1",
			Role:     session.RoleAssistant,
			TokenEst: 3000,
			Preview:  "p",
		}},
	}
	picks := []suggest.TurnScore{{
		TurnID:    "t1",
		Index:     1,
		Role:      "assistant",
		Tokens:    3000,
		Score:     3.0,
		Triggers:  []suggest.HeuristicTrigger{{ID: "high_token_cost", Score: 1.4, Reason: "r"}},
		Preview:   "p",
		LLMReason: "local model said: cut",
	}}
	res := &rankResult{
		Picks:   picks,
		UsedLLM: true,
		Backend: config.BackendOllama,
		Model:   config.DefaultLLMModel,
		Host:    config.DefaultLLMHost,
	}

	out := filepath.Join(t.TempDir(), "out.txt")
	f, err := os.OpenFile(out, os.O_CREATE|os.O_TRUNC|os.O_RDWR, 0o644)
	if err != nil {
		t.Fatalf("open out: %v", err)
	}
	if err := emitSuggestTable(f, s, res); err != nil {
		t.Fatalf("emitSuggestTable: %v", err)
	}
	f.Close()
	got, _ := os.ReadFile(out)
	body := string(got)
	want := "reranked by ollama:" + config.DefaultLLMModel + " (host=" + config.DefaultLLMHost + ")"
	if !strings.Contains(body, want) {
		t.Errorf("ollama footer must be unchanged; want %q\n got:\n%s", want, body)
	}
}
