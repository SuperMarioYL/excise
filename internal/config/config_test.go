package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultValues(t *testing.T) {
	c := Default()
	if c.LLM.Host != DefaultLLMHost {
		t.Errorf("host = %q, want %q", c.LLM.Host, DefaultLLMHost)
	}
	if c.LLM.Model != DefaultLLMModel {
		t.Errorf("model = %q, want %q", c.LLM.Model, DefaultLLMModel)
	}
	if c.LLM.TopN != DefaultLLMTopN {
		t.Errorf("top_n = %d, want %d", c.LLM.TopN, DefaultLLMTopN)
	}
	if c.LLM.TimeoutSec != DefaultLLMTimeoutSec {
		t.Errorf("timeout = %d, want %d", c.LLM.TimeoutSec, DefaultLLMTimeoutSec)
	}
}

func TestLoadFromMissingFileReturnsDefaults(t *testing.T) {
	c, err := LoadFrom(filepath.Join(t.TempDir(), "does-not-exist.toml"))
	if err != nil {
		t.Fatalf("missing file should not error, got %v", err)
	}
	if c.LLM.Host != DefaultLLMHost {
		t.Errorf("expected defaults, got %+v", c.LLM)
	}
	if c.SourcePath != "" {
		t.Errorf("SourcePath = %q on missing file, want empty", c.SourcePath)
	}
}

func TestLoadFromOverridesPartial(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "excise.toml")
	body := `
[llm]
model = "qwen2.5:7b"
top_n = 8
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if c.LLM.Model != "qwen2.5:7b" {
		t.Errorf("model override missed: %q", c.LLM.Model)
	}
	if c.LLM.TopN != 8 {
		t.Errorf("top_n override missed: %d", c.LLM.TopN)
	}
	// Untouched fields kept defaults.
	if c.LLM.Host != DefaultLLMHost {
		t.Errorf("host should default, got %q", c.LLM.Host)
	}
	if c.LLM.TimeoutSec != DefaultLLMTimeoutSec {
		t.Errorf("timeout should default, got %d", c.LLM.TimeoutSec)
	}
	if c.SourcePath != path {
		t.Errorf("SourcePath = %q, want %q", c.SourcePath, path)
	}
}

func TestLoadFromInvalidHostFails(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "excise.toml")
	body := `
[llm]
host = "not a url"
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadFrom(path); err == nil {
		t.Fatal("expected validation error for malformed host")
	}
}

func TestLoadFromMalformedTOML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "excise.toml")
	body := `[llm]
host = "http://localhost:11434"
unterminated = "
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadFrom(path); err == nil {
		t.Fatal("expected parse error for malformed TOML")
	}
}
