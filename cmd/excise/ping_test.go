package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/SuperMarioYL/excise/internal/config"
	"github.com/SuperMarioYL/excise/internal/llm"
)

// stubPingGen is a pingGen stub for tests. It returns the configured reply
// and error, capturing the last prompt it was called with.
type stubPingGen struct {
	reply string
	err   error
	last  string
}

func (s *stubPingGen) Generate(_ context.Context, prompt string) (string, error) {
	s.last = prompt
	return s.reply, s.err
}

// withStubPing swaps the package-level ping factory for the duration of a
// test and restores it on cleanup, mirroring withStubReranker.
func withStubPing(t *testing.T, gen pingGen, meta pingMeta) {
	t.Helper()
	orig := pingGeneratorFactory
	pingGeneratorFactory = func(_ *config.Config) (pingGen, pingMeta, error) {
		return gen, meta, nil
	}
	t.Cleanup(func() { pingGeneratorFactory = orig })
}

func TestPingRequiresLLMFlag(t *testing.T) {
	gf := &globalFlags{llm: false}
	var stderr bytes.Buffer
	if err := runPing(gf, &stderr); err != nil {
		t.Fatalf("ping without --llm should exit 0; got %v", err)
	}
	if !strings.Contains(stderr.String(), "only with --llm") {
		t.Errorf("expected a hint to pass --llm; got %q", stderr.String())
	}
}

func TestPingHappy(t *testing.T) {
	// Default config (backend=ollama) validates clean; swap the factory so
	// no real network call is made.
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	if cfg.LLM.Backend != config.BackendOllama {
		t.Fatalf("default backend should be ollama, got %q", cfg.LLM.Backend)
	}
	stub := &stubPingGen{reply: "OK"}
	withStubPing(t, stub, pingMeta{
		Backend: config.BackendOllama,
		Host:    cfg.LLM.Host,
		Model:   cfg.LLM.Model,
		Label:   config.BackendOllama,
	})

	gf := &globalFlags{llm: true}
	var stderr bytes.Buffer
	if err := runPing(gf, &stderr); err != nil {
		t.Fatalf("happy ping should exit 0; got %v", err)
	}
	out := stderr.String()
	if !strings.Contains(out, "ping ollama backend") {
		t.Errorf("expected pre-call banner naming ollama; got %q", out)
	}
	if !strings.Contains(out, "ping OK") {
		t.Errorf("expected success line; got %q", out)
	}
	if !strings.Contains(stub.last, "OK") {
		t.Errorf("stub should have been called with a prompt; got %q", stub.last)
	}
}

func TestPingFailureIsFailSoft(t *testing.T) {
	// A backend failure must NOT propagate as a non-zero exit (ping is a
	// diagnostic, not a gate); it is reported on stderr and exit 0.
	stub := &stubPingGen{err: fmtPingErr("missing api key")}
	withStubPing(t, stub, pingMeta{
		Backend: config.BackendRemote,
		Host:    "https://api.openai.com",
		Model:   "gpt-4o-mini",
		Label:   config.ProviderOpenAI,
	})

	gf := &globalFlags{llm: true}
	var stderr bytes.Buffer
	if err := runPing(gf, &stderr); err != nil {
		t.Fatalf("ping failure should be fail-soft (exit 0); got %v", err)
	}
	out := stderr.String()
	if !strings.Contains(out, "ping remote backend") {
		t.Errorf("expected pre-call banner naming remote; got %q", out)
	}
	if !strings.Contains(out, "ping FAILED") {
		t.Errorf("expected failure line; got %q", out)
	}
	if !strings.Contains(out, "https://api.openai.com") {
		t.Errorf("expected the real remote host in the banner; got %q", out)
	}
}

func TestPingConfigErrorExitsNonZero(t *testing.T) {
	// A malformed excise.toml (backend=remote with no key/provider) must
	// surface as a real error so the user sees the config problem, unlike a
	// transport failure which is fail-soft.
	gf := &globalFlags{llm: true, llmBackend: config.BackendRemote}
	var stderr bytes.Buffer
	err := runPing(gf, &stderr)
	if err == nil {
		t.Fatalf("config Validate error should return non-nil; got nil")
	}
	if !strings.Contains(err.Error(), "backend=remote") {
		t.Errorf("expected a backend=remote validation message; got %v", err)
	}
}

func TestPingFactoryResolvesRemoteHost(t *testing.T) {
	// The factory must produce the REAL remote destination host (not the
	// Ollama localhost default) when backend=remote, so the ping banner
	// matches the trust echo inside RemoteClient.Generate.
	cfg := config.Default()
	cfg.LLM.Backend = config.BackendRemote
	cfg.LLM.Provider = config.ProviderOpenAI
	cfg.LLM.APIKey = "sk-test"
	cfg.LLM.Model = "gpt-4o-mini"
	if vErr := cfg.Validate(); vErr != nil {
		t.Fatalf("config should validate: %v", vErr)
	}
	gen, meta, err := defaultPingGeneratorFactory(cfg)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	if meta.Backend != config.BackendRemote {
		t.Errorf("backend should be remote; got %q", meta.Backend)
	}
	if meta.Host != "https://api.openai.com" {
		t.Errorf("host should be the OpenAI default, not %q", meta.Host)
	}
	if meta.Label != config.ProviderOpenAI {
		t.Errorf("label should be the provider; got %q", meta.Label)
	}
	// Sanity: the constructed generator is a *RemoteClient (same transport a
	// real rerank uses), so the stderr trust echo fires on Generate.
	if _, ok := gen.(*llm.RemoteClient); !ok {
		t.Errorf("expected *llm.RemoteClient for backend=remote; got %T", gen)
	}
}

func TestPingFactoryResolvesOllama(t *testing.T) {
	// Default backend stays local Ollama; the factory must hand back an
	// *OllamaClient and the Ollama localhost host.
	cfg := config.Default()
	gen, meta, err := defaultPingGeneratorFactory(cfg)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	if meta.Backend != config.BackendOllama {
		t.Errorf("backend should be ollama; got %q", meta.Backend)
	}
	if meta.Host != config.DefaultLLMHost {
		t.Errorf("host should be the Ollama default %q; got %q", config.DefaultLLMHost, meta.Host)
	}
	if _, ok := gen.(*llm.OllamaClient); !ok {
		t.Errorf("expected *llm.OllamaClient for default backend; got %T", gen)
	}
}

// fmtPingErr wraps a message in ErrLLMUnavailable so the fail-soft branch
// matches what a real transport failure looks like.
func fmtPingErr(msg string) error {
	return errors.New(llm.ErrLLMUnavailable.Error() + ": " + msg)
}
