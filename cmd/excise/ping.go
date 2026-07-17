// ping.go — v0.5.0 `excise ping` subcommand.
//
// Read-only diagnostic: sends a trivial one-token prompt to the configured
// LLM backend (Ollama or the v0.4 opt-in remote backend) and reports whether
// it answered. Closes the v0.4 usability gap where the only way to verify a
// remote setup (api key / base_url / provider / model) was to run a full
// `excise suggest --llm` on a real session and watch for the silent stderr
// fallback warning — which conflated "my config is wrong" with "this session
// had nothing to rerank" and required a session file.
//
// Trust contract (unchanged): ping reuses the SAME OllamaClient.Generate /
// RemoteClient.Generate transport as a real rerank call, so for a remote
// backend the destination host is echoed to stderr by the existing path
// inside RemoteClient.Generate — no new outbound surface. Default backend
// (local Ollama) is unchanged; ping writes nothing and touches no session.
//
// Fail-soft (matches the rerank fallback): a backend failure (auth / timeout /
// unreachable) is reported to stderr and exit 0 — ping is a diagnostic, not a
// gate. A malformed excise.toml (Validate error) still exits 1 so the config
// error is visible.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/SuperMarioYL/excise/internal/config"
	"github.com/SuperMarioYL/excise/internal/llm"
)

// pingGen is the minimal Generate contract both OllamaClient and
// RemoteClient already satisfy. Declared locally so tests can swap in a
// stub without spinning up an HTTP server.
type pingGen interface {
	Generate(ctx context.Context, prompt string) (string, error)
}

// pingMeta is the resolved transport summary the command prints before the
// call, so the user can confirm which backend/host ping is about to contact.
type pingMeta struct {
	Backend string // ollama | remote
	Host    string // destination host (scheme://host)
	Model   string // model tag/id
	Label   string // provider tag for remote (openai|anthropic|openrouter), else "ollama"
}

// pingGeneratorFactory builds a client + meta from the resolved config. It is
// a var so tests can replace it with a stub. Production wires through
// internal/llm exactly like the reranker factories do.
var pingGeneratorFactory = defaultPingGeneratorFactory

func defaultPingGeneratorFactory(cfg *config.Config) (pingGen, pingMeta, error) {
	timeout := time.Duration(cfg.LLM.TimeoutSec) * time.Second
	if cfg.LLM.IsRemote() {
		client := llm.NewRemoteClient(llm.RemoteConfig{
			Provider: cfg.LLM.Provider,
			BaseURL:  cfg.LLM.BaseURL,
			Model:    cfg.LLM.Model,
			APIKey:   cfg.LLM.ResolveAPIKey(),
			Timeout:  timeout,
			Stderr:   os.Stderr,
		})
		return client, pingMeta{
			Backend: config.BackendRemote,
			Host:    llm.RemoteHost(cfg.LLM.Provider, cfg.LLM.BaseURL),
			Model:   cfg.LLM.Model,
			Label:   cfg.LLM.Provider,
		}, nil
	}
	client := llm.NewOllamaClient(cfg.LLM.Host, cfg.LLM.Model, timeout)
	return client, pingMeta{
		Backend: config.BackendOllama,
		Host:    cfg.LLM.Host,
		Model:   cfg.LLM.Model,
		Label:   config.BackendOllama,
	}, nil
}

func newPingCmd(gf *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "ping",
		Short: "Verify the configured LLM backend answers (no session needed, opt-in).",
		Long: `Send a trivial prompt to the configured LLM backend (Ollama or the opt-in
remote backend) and report whether it answered.

Use this to verify a remote setup (API key, base_url, provider, model) in
isolation, without running a full 'excise suggest --llm' on a real session.

The destination host is echoed to stderr on every remote call, same as a real
rerank. Default backend (local Ollama) is unchanged. Writes nothing; touches no
session file. A backend failure (auth/timeout/unreachable) is reported and
exits 0 — ping is a diagnostic, not a gate. A malformed excise.toml exits 1.

Requires --llm, consistent with 'suggest --llm' / 'pick --llm'.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPing(gf, os.Stderr)
		},
	}
}

// runPing resolves the backend (config + CLI overrides), prints which
// backend/host it is about to contact, then sends a one-token prompt. Any
// transport error is reported to stderr and swallowed (exit 0); only a config
// Validate error or a missing model returns a non-zero exit.
func runPing(gf *globalFlags, stderr io.Writer) error {
	if !gf.llm {
		fmt.Fprintln(stderr, "excise: ping is meaningful only with --llm (pass --llm to ping the configured backend)")
		return nil
	}
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("read excise.toml: %w", err)
	}
	if gf.llmModel != "" {
		cfg.LLM.Model = gf.llmModel
	}
	if gf.llmHost != "" {
		cfg.LLM.Host = gf.llmHost
	}
	if gf.llmBackend != "" {
		cfg.LLM.Backend = gf.llmBackend
	}
	if gf.llmProvider != "" {
		cfg.LLM.Provider = gf.llmProvider
	}
	if vErr := cfg.Validate(); vErr != nil {
		return vErr
	}
	if cfg.LLM.Model == "" {
		return fmt.Errorf("config: llm.model is empty (set [llm].model in excise.toml or pass --llm-model)")
	}

	gen, meta, err := pingGeneratorFactory(cfg)
	if err != nil {
		return err
	}
	fmt.Fprintf(stderr, "[excise] ping %s backend (host=%s, model=%s)...\n", meta.Backend, meta.Host, meta.Model)

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(cfg.LLM.TimeoutSec)*time.Second)
	defer cancel()
	if _, err := gen.Generate(ctx, "Reply with the single token: OK"); err != nil {
		fmt.Fprintf(stderr, "[excise] ping FAILED: %v\n", err)
		return nil
	}
	fmt.Fprintf(stderr, "[excise] ping OK — %s backend answered.\n", meta.Backend)
	return nil
}
