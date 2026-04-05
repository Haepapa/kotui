//go:build !headless

// gui.go compiles only when the "headless" build tag is NOT set.
// It contains all Wails-specific code (imports GTK/WebKit via CGO on Linux).
// The Docker image uses -tags headless, so this file is excluded entirely,
// allowing a pure-Go, CGO-free binary suitable for alpine/scratch containers.
package main

import (
	"context"
	"embed"
	"log"
	"log/slog"
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

// runGUI starts the full Wails desktop application.
func runGUI(cfg config.Config, db *store.DB) {
	disp := dispatcher.New()
	ollamaClient := ollama.New(cfg.Ollama.Endpoint)

	var memStore *memory.Store
	if cfg.Models.Embedder != "" {
		memStore = memory.New(db, ollamaClient, cfg.Models.Embedder, slog.Default())
	}

	// brainUpdateFn and queueStateFn are set after svc is created; the
	// orchestrator callbacks reach through these closures to notify the service.
	var brainUpdateFn func(agentID, file, summary string)
	var queueStateFn func(qs orchestrator.QueueState)

	orchCfg := orchestrator.OrchestratorConfig{
		LeadModel:           cfg.Models.Lead,
		WorkerModel:         cfg.Models.Specialist,
		EmbedderModel:       cfg.Models.Embedder,
		DataDir:             cfg.App.DataDir,
		SandboxRoot:         filepath.Join(cfg.App.DataDir, "sandbox"),
		CompanyIdentityPath: "COMPANY_IDENTITY.md",
		HandbookPath:        filepath.Join(cfg.App.DataDir, "handbook.md"),
		AppConfig:           cfg,
		OnBrainUpdate: func(agentID, file, summary string) {
			if brainUpdateFn != nil {
				brainUpdateFn(agentID, file, summary)
			}
		},
		OnQueueState: func(qs orchestrator.QueueState) {
			if queueStateFn != nil {
				queueStateFn(qs)
			}
		},
	}
	orch, orchErr := orchestrator.New(orchCfg, orchestrator.NewClientAdapter(ollamaClient), disp, db, slog.Default())
	if orchErr != nil {
		logging.Console.Warn("orchestrator init failed — running without AI backend", "err", orchErr)
		orch = nil
	}

	// Wire up the StorePersister so every Dispatcher message is saved to SQLite.
	// This ensures agent responses, tool calls, and thinking are persisted and
	// can be reloaded when the user switches conversations.
	persister := store.NewStorePersister(db, slog.Default())
	disp.Subscribe("", persister.Handle)

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

	wrService := warroom.New(app, db, orch, disp, cfg, config.ConfigPath(), "COMPANY_IDENTITY.md", memStore)
	// Wire callbacks now that the service is available.
	brainUpdateFn = func(agentID, file, summary string) {
		wrService.EmitBrainUpdate(context.Background(), agentID, file, summary)
	}
	queueStateFn = func(qs orchestrator.QueueState) {
		wrService.EmitQueueState(qs.P0, qs.P1, qs.P2, qs.P3, qs.Active)
	}
	app.RegisterService(application.NewServiceWithOptions(wrService, application.ServiceOptions{
		Name: "WarRoom",
	}))
	app.OnShutdown(wrService.Shutdown)

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
