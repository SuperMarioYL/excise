// Package config loads Excise's optional excise.toml file.
//
// Discovery order (first hit wins):
//
//  1. ./excise.toml                          (project-local)
//  2. $XDG_CONFIG_HOME/excise/excise.toml    (XDG explicit)
//  3. ~/.config/excise/excise.toml           (XDG default)
//
// A missing file is NOT an error — Load returns a Config populated entirely
// from defaults. The [llm] section governs v0.3's opt-in Ollama rerank; the
// table is optional, individual fields are optional, and any field omitted
// from the file inherits its default. This keeps the binary usable with no
// configuration at all.
package config

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// Defaults — kept package-level so tests and the CLI can reference them
// directly, and so the README "what are the defaults" question has one
// place to point at.
const (
	DefaultLLMHost       = "http://localhost:11434"
	DefaultLLMModel      = "llama3.2"
	DefaultLLMTopN       = 5
	DefaultLLMTimeoutSec = 20
	DefaultLLMBackend    = "ollama"
)

// Backend identifiers for the [llm].backend field.
const (
	BackendOllama = "ollama"
	BackendRemote = "remote"
)

// Known remote providers. OpenRouter speaks the OpenAI-compatible
// chat/completions shape, so it shares the OpenAI request/response path.
const (
	ProviderOpenAI     = "openai"
	ProviderAnthropic  = "anthropic"
	ProviderOpenRouter = "openrouter"
)

// LLM holds the rerank-backend knobs. Fields prefixed in comments with
// (v0.3) are the original Ollama-only set; the rest are the v0.4 remote
// backend additions. The default backend stays "ollama" (local-only) so
// behaviour is bit-identical to v0.3 unless the user opts in.
type LLM struct {
	Host       string `toml:"host"`        // (v0.3) Ollama base URL
	Model      string `toml:"model"`       // model tag (Ollama) or model id (remote)
	TopN       int    `toml:"top_n"`       // (v0.3) LLM shortlist size
	TimeoutSec int    `toml:"timeout_sec"` // (v0.3) per-call timeout

	// v0.4 remote backend.
	Backend   string `toml:"backend"`     // ollama | remote (default ollama)
	Provider  string `toml:"provider"`    // openai | anthropic | openrouter
	APIKey    string `toml:"api_key"`     // inline key (prefer api_key_env)
	APIKeyEnv string `toml:"api_key_env"` // env var holding the key
	BaseURL   string `toml:"base_url"`    // override the provider's default host
}

// ResolveAPIKey returns the effective API key for a remote backend: the
// inline api_key if set, otherwise the value of the api_key_env variable.
// Empty when neither resolves.
func (l LLM) ResolveAPIKey() string {
	if l.APIKey != "" {
		return l.APIKey
	}
	if l.APIKeyEnv != "" {
		return os.Getenv(l.APIKeyEnv)
	}
	return ""
}

// IsRemote reports whether the resolved backend is the remote one.
func (l LLM) IsRemote() bool {
	return l.Backend == BackendRemote
}

// Config mirrors the top-level structure of excise.toml. Additional sections
// can be added without breaking older configs — unknown keys are ignored.
type Config struct {
	LLM LLM `toml:"llm"`

	// SourcePath records where the config was loaded from (empty if no file
	// was found and defaults are in use). Useful for debug output.
	SourcePath string `toml:"-"`
}

// Default returns a Config populated with the v0.3 defaults.
func Default() *Config {
	return &Config{
		LLM: LLM{
			Host:       DefaultLLMHost,
			Model:      DefaultLLMModel,
			TopN:       DefaultLLMTopN,
			TimeoutSec: DefaultLLMTimeoutSec,
			Backend:    DefaultLLMBackend,
		},
	}
}

// Load discovers excise.toml on the standard search path, parses it, and
// fills missing fields with defaults. If no file is found, the defaults are
// returned with SourcePath="".
//
// Validation: the host must parse as an absolute URL. TopN<=0 and
// TimeoutSec<=0 are coerced back to defaults rather than erroring — a typo
// shouldn't hard-block a mid-session run.
func Load() (*Config, error) {
	return load(discover())
}

// LoadFrom reads from an explicit path (used by tests and an eventual
// `--config` flag). Empty path → same as Load.
func LoadFrom(path string) (*Config, error) {
	if path == "" {
		return Load()
	}
	return load(path)
}

func load(path string) (*Config, error) {
	cfg := Default()
	if path == "" {
		return cfg, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, fmt.Errorf("config: read %s: %w", path, err)
	}
	// Parse into a partial overlay so omitted fields keep their defaults.
	var overlay Config
	if _, err := toml.Decode(string(data), &overlay); err != nil {
		return nil, fmt.Errorf("config: parse %s: %w", path, err)
	}
	if overlay.LLM.Host != "" {
		cfg.LLM.Host = overlay.LLM.Host
	}
	if overlay.LLM.Model != "" {
		cfg.LLM.Model = overlay.LLM.Model
	}
	if overlay.LLM.TopN > 0 {
		cfg.LLM.TopN = overlay.LLM.TopN
	}
	if overlay.LLM.TimeoutSec > 0 {
		cfg.LLM.TimeoutSec = overlay.LLM.TimeoutSec
	}
	if overlay.LLM.Backend != "" {
		cfg.LLM.Backend = overlay.LLM.Backend
	}
	if overlay.LLM.Provider != "" {
		cfg.LLM.Provider = overlay.LLM.Provider
	}
	if overlay.LLM.APIKey != "" {
		cfg.LLM.APIKey = overlay.LLM.APIKey
	}
	if overlay.LLM.APIKeyEnv != "" {
		cfg.LLM.APIKeyEnv = overlay.LLM.APIKeyEnv
	}
	if overlay.LLM.BaseURL != "" {
		cfg.LLM.BaseURL = overlay.LLM.BaseURL
	}
	cfg.SourcePath = path

	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// Validate checks fields that must be well-formed.
//
//   - The Ollama host must always parse as an absolute URL (it has a default,
//     so this only catches a genuinely malformed override).
//   - backend must be "ollama" or "remote".
//   - When backend=remote: the provider must be a known one, a key must be
//     resolvable (inline api_key or via api_key_env), and base_url — if set —
//     must be an absolute URL.
func (c *Config) Validate() error {
	u, err := url.Parse(c.LLM.Host)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("config: invalid llm.host %q (expected e.g. http://localhost:11434)", c.LLM.Host)
	}

	switch c.LLM.Backend {
	case "", BackendOllama:
		// local path; nothing more to check.
	case BackendRemote:
		switch c.LLM.Provider {
		case ProviderOpenAI, ProviderAnthropic, ProviderOpenRouter:
			// ok
		case "":
			return fmt.Errorf("config: llm.backend=remote requires llm.provider (one of openai|anthropic|openrouter)")
		default:
			return fmt.Errorf("config: unknown llm.provider %q (expected openai|anthropic|openrouter)", c.LLM.Provider)
		}
		if c.LLM.ResolveAPIKey() == "" {
			if c.LLM.APIKeyEnv != "" {
				return fmt.Errorf("config: llm.backend=remote: env %s is empty (set the key or use inline api_key)", c.LLM.APIKeyEnv)
			}
			return fmt.Errorf("config: llm.backend=remote requires an api_key (inline) or api_key_env")
		}
		if c.LLM.BaseURL != "" {
			bu, err := url.Parse(c.LLM.BaseURL)
			if err != nil || bu.Scheme == "" || bu.Host == "" {
				return fmt.Errorf("config: invalid llm.base_url %q (expected e.g. https://api.openai.com)", c.LLM.BaseURL)
			}
		}
	default:
		return fmt.Errorf("config: unknown llm.backend %q (expected ollama|remote)", c.LLM.Backend)
	}
	return nil
}

// discover returns the first existing config path on the standard search
// list, or "" if none exists. Errors stat-ing a candidate are treated as
// "not present" — we never panic during discovery.
func discover() string {
	for _, p := range candidates() {
		if p == "" {
			continue
		}
		fi, err := os.Stat(p)
		if err == nil && !fi.IsDir() {
			return p
		}
	}
	return ""
}

// candidates returns the search list in priority order.
func candidates() []string {
	out := []string{"excise.toml"}
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		out = append(out, filepath.Join(xdg, "excise", "excise.toml"))
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		out = append(out, filepath.Join(home, ".config", "excise", "excise.toml"))
	}
	return out
}
