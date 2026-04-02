// Package config loads and validates application configuration from a TOML file.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/BurntSushi/toml"
)

// ConfigPath returns the platform-appropriate config file path (public alias for defaultConfigPath).
func ConfigPath() string {
	return defaultConfigPath()
}

// defaultConfigPath returns the platform-appropriate config file path.
// On Linux/Docker it uses /data/config.toml (the container data volume).
// On all other platforms it falls back to $XDG_CONFIG_HOME/kotui/config.toml
// (typically ~/.config/kotui/config.toml on macOS/Windows).
func defaultConfigPath() string {
	// Honour an explicit environment variable first.
	if p := os.Getenv("KOTUI_CONFIG"); p != "" {
		return p
	}
	// Docker / Linux production: use the /data volume.
	if _, err := os.Stat("/data"); err == nil {
		return "/data/config.toml"
	}
	// macOS / Windows dev: use the OS user config directory.
	if dir, err := os.UserConfigDir(); err == nil {
		return filepath.Join(dir, "kotui", "config.toml")
	}
	// Last resort.
	return filepath.Join(os.TempDir(), "kotui", "config.toml")
}

// Config is the top-level application configuration.
type Config struct {
	App              AppConfig              `toml:"app"`
	Ollama           OllamaConfig           `toml:"ollama"`
	Models           ModelsConfig           `toml:"models"`
	SeniorConsultant SeniorConsultantConfig `toml:"senior_consultant"`
	Project          ProjectConfig          `toml:"project"`
	Relay            RelayConfig            `toml:"relay"`
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

// SeniorConsultantConfig defines the on-demand Senior Consultant model.
// This model is invoked only when the Lead signals a capability_escalation.
// If SSHHost is set, Kotui will attempt to wake the remote machine before
// sending the first request.
type SeniorConsultantConfig struct {
	// Model is the Ollama model name on the remote (or local) endpoint.
	Model string `toml:"model"`
	// Endpoint is the Ollama API URL for the Senior Consultant.
	// Leave empty to use the same Ollama instance as the Lead.
	Endpoint string `toml:"endpoint"`
	// SSHHost is an optional SSH alias (from ~/.ssh/config) used to wake
	// the remote machine. Leave empty to disable wake-on-demand.
	SSHHost string `toml:"ssh_host"`
	// SSHStartCmd is the command to run on the remote host after SSH connection.
	SSHStartCmd string `toml:"ssh_start_cmd"`
}

// Fields are optional; an empty token disables that relay.
type RelayConfig struct {
	// Telegram
	TelegramBotToken string `toml:"telegram_bot_token"`
	TelegramChatID   string `toml:"telegram_chat_id"`
	// Slack
	SlackBotToken     string `toml:"slack_bot_token"`
	SlackChannelID    string `toml:"slack_channel_id"`
	SlackSigningSecret string `toml:"slack_signing_secret"`
	// WhatsApp Cloud API
	WhatsAppToken       string `toml:"whatsapp_token"`
	WhatsAppPhoneID     string `toml:"whatsapp_phone_number_id"`
	WhatsAppVerifyToken string `toml:"whatsapp_verify_token"`
	// Shared
	WebhookSecret string `toml:"webhook_secret"` // kept for backward compat
	WebhookPort   int    `toml:"webhook_port"`   // default 8080; used by Slack/WhatsApp webhooks
}

// defaultDataDir returns the platform-appropriate data directory.
func defaultDataDir() string {
	if _, err := os.Stat("/data"); err == nil {
		return "/data"
	}
	if dir, err := os.UserHomeDir(); err == nil {
		return filepath.Join(dir, ".kotui", "data")
	}
	return filepath.Join(os.TempDir(), "kotui", "data")
}


func Defaults() Config {
	return Config{
		App: AppConfig{
			DataDir:  defaultDataDir(),
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
		SeniorConsultant: SeniorConsultantConfig{
			Model:       "qwen2.5-coder:32b",
			Endpoint:    "",
			SSHHost:     "",
			SSHStartCmd: "ollama serve",
		},
		Project: ProjectConfig{},
		Relay:   RelayConfig{WebhookPort: 8080},
	}
}

// Load reads the config file at the given path, falling back to defaults for
// any unset fields. If the file does not exist it is created with defaults.
func Load(path string) (Config, error) {
	cfg := Defaults()

	if path == "" {
		path = defaultConfigPath()
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
