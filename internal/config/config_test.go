package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/haepapa/kotui/internal/config"
)

func TestDefaults(t *testing.T) {
	cfg := config.Defaults()
	if cfg.Ollama.Endpoint == "" {
		t.Error("expected a default Ollama endpoint")
	}
	if cfg.Models.Lead == "" {
		t.Error("expected a default lead model")
	}
	if cfg.Models.Specialist == "" {
		t.Error("expected a default specialist model")
	}
	if cfg.Models.Embedder == "" {
		t.Error("expected a default embedder model")
	}
}

func TestLoadCreatesDefaultFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.Ollama.Endpoint == "" {
		t.Error("expected endpoint from defaults")
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Error("expected default config file to be created on disk")
	}
}

func TestLoadParsesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	content := `
[app]
data_dir = "/custom/data"
timezone = "UTC"
headless = true

[ollama]
endpoint = "http://ollama:11434"

[models]
lead = "llama3:70b"
specialist = "mistral:7b"
embedder = "nomic-embed-text"
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.App.DataDir != "/custom/data" {
		t.Errorf("expected /custom/data, got %s", cfg.App.DataDir)
	}
	if !cfg.App.Headless {
		t.Error("expected headless=true")
	}
	if cfg.Ollama.Endpoint != "http://ollama:11434" {
		t.Errorf("unexpected endpoint: %s", cfg.Ollama.Endpoint)
	}
	if cfg.Models.Lead != "llama3:70b" {
		t.Errorf("unexpected lead model: %s", cfg.Models.Lead)
	}
}
