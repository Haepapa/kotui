package main

import (
	"context"
	"embed"
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
	"github.com/haepapa/kotui/internal/warroom"
	"github.com/wailsapp/wails/v3/pkg/application"
)

//go:embed all:frontend/dist
var assets embed.FS

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

	// --- Backend wiring -----------------------------------------------
	disp := dispatcher.New()

	ollamaClient := ollama.New(cfg.Ollama.Endpoint)

	// Create memory store (non-fatal if embedder model not configured).
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
		logging.Console.Warn("orchestrator init failed — running without AI backend", "err", orchErr)
		orch = nil
	}

	// Activate the last-used project (if any).
	if cfg.Project.ActiveProjectID != "" && orch != nil {
		if err := orch.SetProject(context.Background(), cfg.Project.ActiveProjectID); err != nil {
			logging.Console.Warn("could not restore active project", "err", err)
		}
	}

	app := application.New(application.Options{
		Name:        "Kotui",
		Description: "AgentFlow Orchestrator — Virtual Company AI",
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})

	app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title: "Kotui — War Room",
		Mac: application.MacWindow{
			InvisibleTitleBarHeight: 50,
			Backdrop:                application.MacBackdropTranslucent,
			TitleBar:                application.MacTitleBarHiddenInset,
		},
		BackgroundColour: application.NewRGB(15, 20, 30),
		URL:              "/",
		Width:            1400,
		Height:           900,
		MinWidth:         1024,
		MinHeight:        640,
	})

	// --- War Room service (Wails RPC + event bridge) -------------------
	wrService := warroom.New(app, db, orch, disp, cfg, config.ConfigPath(), "COMPANY_IDENTITY.md", memStore)
	app.RegisterService(application.NewServiceWithOptions(wrService, application.ServiceOptions{
		Name: "WarRoom",
	}))
	app.OnShutdown(wrService.Shutdown)

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
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

	// Start relay gateway — subscribers for Phase 12 relays registered here.
	gw := relay.New(disp, slog.Default())
	defer gw.Close()

	logging.Console.Info("headless backend ready",
		"data_dir", cfg.App.DataDir,
		"lead_model", cfg.Models.Lead,
		"relays", gw.RelayCount(),
		"ai_backend", orch != nil,
		"memory", memStore != nil,
	)
	_ = orch    // suppress unused warning; used via event bus
	_ = memStore

	// Block until OS termination signal.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGTERM, syscall.SIGINT)
	sig := <-quit
	logging.Console.Info("shutdown signal received", "signal", sig)
	logging.Console.Info("kotui headless: graceful shutdown complete")
}
