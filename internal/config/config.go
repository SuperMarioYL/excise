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
)

// LLM holds the Ollama-side knobs.
type LLM struct {
	Host       string `toml:"host"`
	Model      string `toml:"model"`
	TopN       int    `toml:"top_n"`
	TimeoutSec int    `toml:"timeout_sec"`
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
	cfg.SourcePath = path

	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// Validate checks fields that must be well-formed. Currently only the host.
func (c *Config) Validate() error {
	u, err := url.Parse(c.LLM.Host)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("config: invalid llm.host %q (expected e.g. http://localhost:11434)", c.LLM.Host)
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
