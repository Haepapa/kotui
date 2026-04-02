package main

import (
	"context"
	"flag"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/haepapa/kotui/internal/config"
	"github.com/haepapa/kotui/internal/dispatcher"
	"github.com/haepapa/kotui/internal/logging"
	"github.com/haepapa/kotui/internal/memory"
	"github.com/haepapa/kotui/internal/ollama"
	"github.com/haepapa/kotui/internal/orchestrator"
	"github.com/haepapa/kotui/internal/relay"
	"github.com/haepapa/kotui/internal/store"
)

func main() {
	cfgPath := flag.String("config", "", "Path to config.toml (default: /data/config.toml)")
	headless := flag.Bool("headless", false, "Run without a UI window (Docker/server mode)")
	flag.Parse()

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Fatalf("kotui: failed to load config: %v", err)
	}
	if *headless {
		cfg.App.Headless = true
	}

	logging.Console.Info("kotui starting", "headless", cfg.App.Headless, "data_dir", cfg.App.DataDir)

	dbPath := filepath.Join(cfg.App.DataDir, "kotui.db")
	if err := os.MkdirAll(cfg.App.DataDir, 0o755); err != nil {
		log.Fatalf("kotui: cannot create data dir %s: %v", cfg.App.DataDir, err)
	}
	db, err := store.Open(dbPath)
	if err != nil {
		log.Fatalf("kotui: failed to open database: %v", err)
	}
	defer db.Close()
	logging.Console.Info("database ready", "path", dbPath)

	if cfg.App.Headless {
		logging.Console.Info("running in headless mode — UI suppressed")
		runHeadless(cfg, db)
		return
	}

	runGUI(cfg, db)
}

// runHeadless runs the full backend without a UI window.
// It blocks until SIGTERM or SIGINT is received, then shuts down gracefully.
func runHeadless(cfg config.Config, db *store.DB) {
	disp := dispatcher.New()
	ollamaClient := ollama.New(cfg.Ollama.Endpoint)

	var memStore *memory.Store
	if cfg.Models.Embedder != "" {
		memStore = memory.New(db, ollamaClient, cfg.Models.Embedder, slog.Default())
	}

	orchCfg := orchestrator.OrchestratorConfig{
		LeadModel:           cfg.Models.Lead,
		WorkerModel:         cfg.Models.Specialist,
		EmbedderModel:       cfg.Models.Embedder,
		DataDir:             cfg.App.DataDir,
		SandboxRoot:         filepath.Join(cfg.App.DataDir, "sandbox"),
		CompanyIdentityPath: "COMPANY_IDENTITY.md",
		AppConfig:           cfg,
	}
	orch, orchErr := orchestrator.New(orchCfg, orchestrator.NewClientAdapter(ollamaClient), disp, db, slog.Default())
	if orchErr != nil {
		logging.Console.Warn("orchestrator init failed — relay gateway running without AI backend", "err", orchErr)
		orch = nil
	}

	if cfg.Project.ActiveProjectID != "" && orch != nil {
		if err := orch.SetProject(context.Background(), cfg.Project.ActiveProjectID); err != nil {
			logging.Console.Warn("could not restore active project", "err", err)
		}
	}

	// Start relay gateway — Phase 12 relay adapters registered below.
	gw := relay.New(disp, slog.Default())
	defer gw.Close()

	// Command handler for inbound relay commands (/status, /approve, /summary).
	cmdFn := relay.NewCommandHandler(db, orch, slog.Default())

	// Webhook server (shared by Slack + WhatsApp) — start if either is configured.
	var webhookSrv *relay.WebhookServer
	relayLog := slog.Default()
	if cfg.Relay.SlackBotToken != "" || cfg.Relay.WhatsAppToken != "" {
		port := cfg.Relay.WebhookPort
		if port == 0 {
			port = 8080
		}
		webhookSrv = relay.NewWebhookServer(port, relayLog)
	}

	// Telegram relay.
	if cfg.Relay.TelegramBotToken != "" {
		tg := relay.NewTelegramRelay(cfg.Relay.TelegramBotToken, cfg.Relay.TelegramChatID, cmdFn, relayLog)
		tg.Start()
		defer tg.Stop()
		gw.Register(tg)
	}

	// Slack relay.
	if cfg.Relay.SlackBotToken != "" {
		sl := relay.NewSlackRelay(cfg.Relay.SlackBotToken, cfg.Relay.SlackChannelID, cfg.Relay.SlackSigningSecret, cmdFn, relayLog)
		if webhookSrv != nil {
			webhookSrv.Register(sl)
		}
		gw.Register(sl)
	}

	// WhatsApp relay.
	if cfg.Relay.WhatsAppToken != "" {
		wa := relay.NewWhatsAppRelay(cfg.Relay.WhatsAppToken, cfg.Relay.WhatsAppPhoneID, cfg.Relay.WhatsAppVerifyToken, cfg.Relay.WhatsAppToken, cmdFn, relayLog)
		if webhookSrv != nil {
			webhookSrv.Register(wa)
		}
		gw.Register(wa)
	}

	// Start webhook server after all handlers are registered.
	if webhookSrv != nil {
		if err := webhookSrv.Start(); err != nil {
			logging.Console.Warn("webhook server failed to start", "err", err)
		} else {
			defer webhookSrv.Stop()
		}
	}

	logging.Console.Info("headless backend ready",
		"data_dir", cfg.App.DataDir,
		"lead_model", cfg.Models.Lead,
		"relays", gw.RelayCount(),
		"ai_backend", orch != nil,
		"memory", memStore != nil,
	)
	_ = orch
	_ = memStore

	// Block until OS termination signal.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGTERM, syscall.SIGINT)
	sig := <-quit
	logging.Console.Info("shutdown signal received", "signal", sig)
	logging.Console.Info("kotui headless: graceful shutdown complete")
}
