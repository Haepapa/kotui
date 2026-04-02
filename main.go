package main

import (
	"context"
	"embed"
	"flag"
	"log"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/haepapa/kotui/internal/config"
	"github.com/haepapa/kotui/internal/dispatcher"
	"github.com/haepapa/kotui/internal/logging"
	"github.com/haepapa/kotui/internal/memory"
	"github.com/haepapa/kotui/internal/ollama"
	"github.com/haepapa/kotui/internal/orchestrator"
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
		// Phase 11: relay gateway will be initialised here.
		select {} // block until signal
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
