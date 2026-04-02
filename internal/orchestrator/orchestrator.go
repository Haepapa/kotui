// Package orchestrator is the top-level coordinator of the Kotui Virtual Company.
//
// It integrates all prior phases:
//   - Phase 1 (Ollama) — model inference via the Inferrer interface
//   - Phase 2 (Dispatcher) — message routing
//   - Phase 3 (MCP) — tool calling
//   - Phase 4 (Core tools) — actual work
//   - Phase 5 (Agent identity) — system prompts, journaling
//
// The Orchestrator drives the Lead → Worker → Verify loop, manages VRAM
// coordination, routes capability escalations, and handles the hiring workflow.
package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/haepapa/kotui/internal/agent"
	"github.com/haepapa/kotui/internal/config"
	"github.com/haepapa/kotui/internal/dispatcher"
	"github.com/haepapa/kotui/internal/mcp"
	"github.com/haepapa/kotui/internal/memory"
	"github.com/haepapa/kotui/internal/ollama"
	"github.com/haepapa/kotui/internal/store"
	"github.com/haepapa/kotui/internal/tools"
	"github.com/haepapa/kotui/pkg/models"
)

// OrchestratorConfig is the runtime configuration for the Orchestrator.
// It is derived from config.Config at startup.
type OrchestratorConfig struct {
	LeadModel           string
	WorkerModel         string
	EmbedderModel       string // e.g. "nomic-embed-text"; empty disables memory
	DataDir             string
	SandboxRoot         string
	CompanyIdentityPath string
	AppConfig           config.Config
}

// Orchestrator coordinates all agents, tools, and communication.
type Orchestrator struct {
	cfg     OrchestratorConfig
	disp    *dispatcher.Dispatcher
	db      *store.DB
	mcpEng  *mcp.Engine
	log     *slog.Logger

	inferrer      Inferrer
	scInferrer    Inferrer // Senior Consultant endpoint (may be same as inferrer)

	lead    *RunningAgent
	leadAgent *agent.Agent
	leadMu  sync.Mutex

	vram      *VRAMCoordinator
	escalator *EscalationRouter
	hiring    *HiringManager

	memory *memory.Store // nil if EmbedderModel is empty

	// dmAgents holds one isolated RunningAgent per agentID for DM conversations.
	// These are separate from the war-room Lead so their histories don't mix.
	dmAgents   map[string]*RunningAgent
	dmAgentsMu sync.Mutex

	projectID string
	convID    string
}

// New creates and wires up a fully initialised Orchestrator.
// It registers all core MCP tools, spawns the Lead agent, and detects
// the VRAM profile.
func New(
	cfg OrchestratorConfig,
	inferrer Inferrer,
	disp *dispatcher.Dispatcher,
	db *store.DB,
	log *slog.Logger,
) (*Orchestrator, error) {
	if log == nil {
		log = slog.Default()
	}

	// Build MCP engine sandboxed to the project workspace.
	mcpEng := mcp.New(cfg.SandboxRoot)
	if err := tools.RegisterAll(mcpEng, cfg.AppConfig); err != nil {
		return nil, fmt.Errorf("orchestrator: register tools: %w", err)
	}

	// Spawn Lead agent.
	leadAgent, err := agent.Spawn(agent.SpawnConfig{
		ID:                  "lead",
		Name:                "Lead",
		Role:                models.RoleLead,
		Model:               cfg.LeadModel,
		DataDir:             cfg.DataDir,
		CompanyIdentityPath: cfg.CompanyIdentityPath,
		MCPFragment:         mcpEng.SystemPromptFragment(models.ClearanceLead),
	})
	if err != nil {
		return nil, fmt.Errorf("orchestrator: spawn lead: %w", err)
	}

	lead := newRunningAgent(
		"lead", "Lead", cfg.LeadModel,
		models.ClearanceLead,
		ollama.Forever(), // Lead is persistent
		leadAgent.SystemPrompt(),
		inferrer, mcpEng,
	)

	// Detect VRAM profile.
	profile := models.VRAMSwap // safe default
	if vp, err := detectVRAMProfile(inferrer, cfg.LeadModel, cfg.WorkerModel, log); err == nil {
		profile = vp
	} else {
		log.Warn("VRAM detection failed; defaulting to swap mode", "err", err)
	}
	log.Info("VRAM profile selected", "profile", profile)

	vram := newVRAMCoordinator(profile, inferrer, cfg.LeadModel)
	escalator := newEscalationRouter(cfg.AppConfig, disp, db, log)
	hiringMgr := newHiringManager(cfg, inferrer, mcpEng, disp, db, log)

	o := &Orchestrator{
		cfg:       cfg,
		disp:      disp,
		db:        db,
		mcpEng:    mcpEng,
		log:       log,
		inferrer:  inferrer,
		lead:      lead,
		leadAgent: leadAgent,
		vram:      vram,
		escalator: escalator,
		hiring:    hiringMgr,
	}

	// Build memory store if embedder model is configured.
	if cfg.EmbedderModel != "" && db != nil {
		type embedder interface {
			Embed(ctx context.Context, model, text string) ([]float32, error)
		}
		if emb, ok := inferrer.(embedder); ok {
			o.memory = memory.New(db, emb, cfg.EmbedderModel, log)
		}
	}

	return o, nil
}

// SetProject sets the active project ID and creates a conversation record.
func (o *Orchestrator) SetProject(ctx context.Context, projectID string) error {
	o.projectID = projectID
	o.disp.SetProject(projectID)

	if o.db != nil {
		convID, err := o.db.CreateConversation(ctx, projectID, "war-room")
		if err != nil {
			return fmt.Errorf("orchestrator: create conversation: %w", err)
		}
		o.convID = convID
	}
	return nil
}

// HandleBossCommand is the main entry point for a Boss instruction.
// It runs the full Lead → task decomposition → Worker loop.
func (o *Orchestrator) HandleBossCommand(ctx context.Context, command string) error {
	o.disp.DispatchSummary(models.Message{
		ProjectID: o.projectID,
		ConversationID: o.convID,
		Kind:      models.KindBossCommand,
		Tier:      models.TierSummary,
		Content:   command,
	})

	o.log.Info("boss command received", "command", truncate(command, 80))

	// Step 1: Lead decomposes the command into sub-tasks.
	// Augment with relevant memories if available.
	augmented := decomposePrompt(command)
	if o.memory != nil && o.projectID != "" {
		entries, err := o.memory.Recall(ctx, "lead", o.projectID, command, 5)
		if err == nil && len(entries) > 0 {
			augmented = memory.FormatRecall(entries) + "\n\n" + augmented
		}
	}
	decomposed, err := o.lead.Turn(ctx, augmented)
	if err != nil {
		var escErr *EscalationNeededError
		if errors.As(err, &escErr) {
			return o.handleEscalation(ctx, escErr, command)
		}
		return fmt.Errorf("lead decomposition: %w", err)
	}

	o.disp.DispatchRaw(models.Message{
		ProjectID: o.projectID,
		Kind:      models.KindDraft,
		Tier:      models.TierRaw,
		Content:   "Lead plan: " + decomposed,
	})

	tasks := parseTaskList(decomposed)
	if len(tasks) == 0 {
		// Lead may have just answered directly (simple tasks).
		o.disp.DispatchSummary(models.Message{
			ProjectID: o.projectID,
			ConversationID: o.convID,
			Kind:      models.KindAgentMessage,
			Tier:      models.TierSummary,
			AgentID:   "lead",
			Content:   decomposed,
		})
		return nil
	}

	// Step 2: Persist tasks to SQLite.
	if o.db != nil && o.projectID != "" {
		for _, t := range tasks {
			_ = o.db.CreateTask(ctx, models.Task{
				ID:          t.ID,
				ProjectID:   o.projectID,
				Title:       t.Title,
				Description: t.Description,
				AssigneeID:  t.Assignee,
			})
		}
	}

	// Step 3: Execute specialist tasks via the Worker loop.
	for _, task := range tasks {
		if task.Assignee != "specialist" {
			continue // Lead handles its own tasks inline
		}

		job := WorkerJob{
			TaskID:      task.ID,
			Instruction: fmt.Sprintf("Task: %s\n\n%s", task.Title, task.Description),
			ProjectID:   o.projectID,
			ConvID:      o.convID,
		}

		result, workerErr := runWorkerTask(
			ctx, o.cfg, o.inferrer, o.mcpEng,
			o.lead, o.vram, o.db, o.disp, job, o.memory, o.log,
		)
		if workerErr != nil {
			var escErr *EscalationNeededError
			if errors.As(workerErr, &escErr) {
				if routeErr := o.handleEscalation(ctx, escErr, job.Instruction); routeErr != nil {
					return routeErr
				}
				continue
			}
			o.log.Error("worker task failed", "task", task.ID, "err", workerErr)
			o.disp.DispatchSummary(models.Message{
				ProjectID: o.projectID,
				Kind:      models.KindSystemEvent,
				Tier:      models.TierSummary,
				Content:   fmt.Sprintf("⚠️ Task %s failed: %v", task.ID, workerErr),
			})
		}
		_ = result
	}

	// Step 4: Lead produces final summary milestone.
	o.leadMu.Lock()
	summary, sumErr := o.lead.Turn(ctx, "All sub-tasks are complete. Provide a brief summary of what was accomplished for the Boss.")
	o.leadMu.Unlock()
	if sumErr == nil {
		o.disp.DispatchSummary(models.Message{
			ProjectID:      o.projectID,
			ConversationID: o.convID,
			Kind:           models.KindMilestone,
			Tier:           models.TierSummary,
			AgentID:        "lead",
			Content:        summary,
		})
	}

	return nil
}

// HandleDirectMessage sends a message directly to a named agent and dispatches
// the response back to the supplied DM conversation ID.
//
// Unlike HandleBossCommand, this bypasses Lead task-decomposition entirely.
// Each agentID gets its own isolated RunningAgent with the agent's real system
// prompt so DM history never mixes with the war-room conversation.
//
// onChunk, if non-nil, is called for every streamed token so the caller can
// forward chunks to the frontend for a live typing effect. Ollama API timings
// are dispatched as raw-tier log messages for the dev console.
func (o *Orchestrator) HandleDirectMessage(ctx context.Context, agentID, message, convID string, onChunk func(string)) error {
	// Resolve the DM agent — create on first use, reuse thereafter.
	o.dmAgentsMu.Lock()
	if o.dmAgents == nil {
		o.dmAgents = make(map[string]*RunningAgent)
	}
	ra, exists := o.dmAgents[agentID]
	if !exists {
		model, clearance, sysPrompt := o.agentIdentityForDM(agentID)
		ra = newRunningAgent(
			agentID, agentID,
			model, clearance,
			ollama.Forever(),
			sysPrompt,
			o.inferrer, o.mcpEng,
		)
		o.dmAgents[agentID] = ra
	}
	o.dmAgentsMu.Unlock()

	// Optionally augment the message with recalled memories.
	augmented := message
	if o.memory != nil && o.projectID != "" {
		if entries, err := o.memory.Recall(ctx, agentID, o.projectID, message, 5); err == nil && len(entries) > 0 {
			augmented = memory.FormatRecall(entries) + "\n\n" + message
		}
	}

	// Log the outbound Ollama API call to the dev console.
	o.disp.Dispatch(models.Message{
		ProjectID:      o.projectID,
		ConversationID: convID,
		AgentID:        agentID,
		Kind:           models.KindSystemEvent,
		Tier:           models.TierRaw,
		Content:        fmt.Sprintf("→ POST /api/chat  model=%s  agent=%s", ra.Model, agentID),
	})

	start := time.Now()
	response, err := ra.TurnStream(ctx, augmented, onChunk)
	elapsed := time.Since(start)

	if err != nil {
		// Log the error to the dev console before returning.
		o.disp.Dispatch(models.Message{
			ProjectID:      o.projectID,
			ConversationID: convID,
			AgentID:        agentID,
			Kind:           models.KindSystemEvent,
			Tier:           models.TierRaw,
			Content:        fmt.Sprintf("✗ ollama error after %.2fs: %v", elapsed.Seconds(), err),
		})
		return fmt.Errorf("dm %s: %w", agentID, err)
	}

	// Log timing to the dev console.
	o.disp.Dispatch(models.Message{
		ProjectID:      o.projectID,
		ConversationID: convID,
		AgentID:        agentID,
		Kind:           models.KindSystemEvent,
		Tier:           models.TierRaw,
		Content:        fmt.Sprintf("← /api/chat done  %.2fs  agent=%s", elapsed.Seconds(), agentID),
	})

	// Dispatch the agent reply to the DM conversation (not the war-room convID).
	o.disp.Dispatch(models.Message{
		ProjectID:      o.projectID,
		ConversationID: convID,
		AgentID:        agentID,
		Kind:           models.KindAgentMessage,
		Tier:           models.TierSummary,
		Content:        response,
	})
	return nil
}

// agentIdentityForDM returns the model, clearance, and compiled system prompt
// for the named agent. Falls back to Lead identity when the agent is unknown.
func (o *Orchestrator) agentIdentityForDM(agentID string) (model string, clearance models.Clearance, sysPrompt string) {
	switch agentID {
	case "lead":
		o.leadMu.Lock()
		sysPrompt = o.leadAgent.SystemPrompt()
		o.leadMu.Unlock()
		return o.cfg.LeadModel, models.ClearanceLead, sysPrompt

	default:
		// Try to load the agent's compiled instruction.md from its data directory.
		spawnCfg := agent.SpawnConfig{
			ID:                  agentID,
			Name:                agentID,
			Role:                models.RoleSpecialist,
			Model:               o.cfg.WorkerModel,
			DataDir:             o.cfg.DataDir,
			CompanyIdentityPath: o.cfg.CompanyIdentityPath,
			MCPFragment:         o.mcpEng.SystemPromptFragment(models.ClearanceSpecialist),
		}
		if a, err := agent.Spawn(spawnCfg); err == nil {
			return o.cfg.WorkerModel, models.ClearanceSpecialist, a.SystemPrompt()
		}
		// Fallback: simple helpful assistant.
		return o.cfg.WorkerModel, models.ClearanceSpecialist,
			"You are a helpful AI assistant. Respond clearly and concisely."
	}
}

// CultureBroadcast forces a full context reset on all active agents.
// Called when COMPANY_IDENTITY.md is edited.
func (o *Orchestrator) CultureBroadcast(newIdentityPath string) error {
	o.leadMu.Lock()
	defer o.leadMu.Unlock()

	mcpFragment := o.mcpEng.SystemPromptFragment(models.ClearanceLead)
	if err := o.leadAgent.CultureUpdate(newIdentityPath, mcpFragment); err != nil {
		return fmt.Errorf("culture broadcast: lead update: %w", err)
	}
	o.lead.ResetContext(o.leadAgent.SystemPrompt())

	o.disp.DispatchSummary(models.Message{
		ProjectID: o.projectID,
		Kind:      models.KindSystemEvent,
		Tier:      models.TierSummary,
		Content:   "🔄 Culture Update broadcast — all active agents have received updated values",
	})
	return nil
}

// Hiring returns the HiringManager for managing Trial agent interviews.
func (o *Orchestrator) Hiring() *HiringManager { return o.hiring }

// VRAMProfile returns the detected VRAM profile (dual or swap).
func (o *Orchestrator) VRAMProfile() models.VRAMProfile { return o.vram.Profile() }

// handleEscalation delegates to the EscalationRouter and returns a
// BossNotifiedError if no Senior Consultant is available.
func (o *Orchestrator) handleEscalation(ctx context.Context, escErr *EscalationNeededError, task string) error {
	result, routeErr := o.escalator.Route(ctx, escErr, task, o.projectID)
	if routeErr != nil {
		return routeErr
	}
	// Inject the Senior Consultant's result back into the Lead's context.
	o.leadMu.Lock()
	_, _ = o.lead.Turn(ctx, fmt.Sprintf("The Senior Consultant has provided guidance:\n%s\nPlease continue with the task.", result))
	o.leadMu.Unlock()
	return nil
}

// detectVRAMProfile uses the Inferrer to detect whether Lead + Worker fit
// in available memory. Falls back to VRAMSwap on any error.
func detectVRAMProfile(inferrer Inferrer, leadModel, workerModel string, log *slog.Logger) (models.VRAMProfile, error) {
	// We cannot call DetectVRAMProfile directly (it's on *ollama.Client).
	// The safe approach: if the Inferrer is our ClientAdapter, unwrap and call it.
	// Otherwise default to swap mode (safe for constrained hardware).
	type vrammer interface {
		VRAMProfile(ctx context.Context, leadModel, workerModel string) (models.VRAMProfile, error)
	}
	if vm, ok := inferrer.(vrammer); ok {
		return vm.VRAMProfile(context.Background(), leadModel, workerModel)
	}
	return models.VRAMSwap, nil
}
