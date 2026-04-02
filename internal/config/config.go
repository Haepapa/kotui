// Package config loads and validates application configuration from a TOML file.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/BurntSushi/toml"
)

const defaultConfigPath = "/data/config.toml"

// Config is the top-level application configuration.
type Config struct {
	App      AppConfig      `toml:"app"`
	Ollama   OllamaConfig   `toml:"ollama"`
	Models   ModelsConfig   `toml:"models"`
	Project  ProjectConfig  `toml:"project"`
	Relay    RelayConfig    `toml:"relay"`
}

// AppConfig holds general application settings.
type AppConfig struct {
	DataDir  string `toml:"data_dir"`
	Timezone string `toml:"timezone"`
	Headless bool   `toml:"headless"`
}

// OllamaConfig defines connectivity to the local Ollama instance.
type OllamaConfig struct {
	Endpoint        string        `toml:"endpoint"`
	HeartbeatInterval time.Duration `toml:"heartbeat_interval"`
	RequestTimeout  time.Duration `toml:"request_timeout"`
}

// ModelsConfig specifies which models are used for each agent role.
type ModelsConfig struct {
	Lead       string `toml:"lead"`
	Specialist string `toml:"specialist"`
	Embedder   string `toml:"embedder"`
}

// ProjectConfig holds the ID of the last active project.
type ProjectConfig struct {
	ActiveProjectID string `toml:"active_project_id"`
}

// RelayConfig holds credentials for remote messaging integrations (Telegram, Slack, etc.).
// Fields are optional; an empty token disables that relay.
type RelayConfig struct {
	TelegramBotToken string `toml:"telegram_bot_token"`
	SlackBotToken    string `toml:"slack_bot_token"`
	SlackChannelID   string `toml:"slack_channel_id"`
	WebhookSecret    string `toml:"webhook_secret"`
}

// Defaults returns a Config populated with safe default values.
func Defaults() Config {
	return Config{
		App: AppConfig{
			DataDir:  "/data",
			Timezone: "Pacific/Auckland",
			Headless: false,
		},
		Ollama: OllamaConfig{
			Endpoint:          "http://localhost:11434",
			HeartbeatInterval: 10 * time.Second,
			RequestTimeout:    90 * time.Second,
		},
		Models: ModelsConfig{
			Lead:       "qwen2.5-coder:32b",
			Specialist: "llama3.1:8b",
			Embedder:   "nomic-embed-text",
		},
		Project: ProjectConfig{},
		Relay:   RelayConfig{},
	}
}

// Load reads the config file at the given path, falling back to defaults for
// any unset fields. If the file does not exist it is created with defaults.
func Load(path string) (Config, error) {
	cfg := Defaults()

	if path == "" {
		path = defaultConfigPath
	}

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		if writeErr := writeDefaults(path, cfg); writeErr != nil {
			return cfg, fmt.Errorf("config: could not write defaults to %s: %w", path, writeErr)
		}
		return cfg, nil
	}
	if err != nil {
		return cfg, fmt.Errorf("config: read %s: %w", path, err)
	}

	if _, err := toml.Decode(string(data), &cfg); err != nil {
		return cfg, fmt.Errorf("config: parse %s: %w", path, err)
	}

	return cfg, nil
}

func writeDefaults(path string, cfg Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return toml.NewEncoder(f).Encode(cfg)
}
