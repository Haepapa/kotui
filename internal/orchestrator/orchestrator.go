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
	"encoding/json"
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
	HandbookPath        string
	AppConfig           config.Config

	// OnBrainUpdate, if non-nil, is called whenever an agent successfully
	// writes to one of its own brain files via the update_self MCP tool.
	// The caller (typically WarRoomService) uses this to recompose
	// instruction.md and emit a brain-update notification to the frontend.
	OnBrainUpdate func(agentID, file, summary string)

	// OnQueueState, if non-nil, is called after every cognition queue state
	// change (enqueue, dequeue, execution start/stop). Use it to push a
	// kotui:queue_state event to the frontend.
	OnQueueState func(QueueState)
	// OnApproval, if non-nil, is called (from a goroutine) when the Lead
	// Optimizer creates a new handbook proposal approval. The projectID
	// identifies which project's pending approval queue should be refreshed.
	OnApproval func(projectID string)
}
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

	// cogQueue serialises all Ollama inference calls with P0→P3 priority.
	// Replaces the old dmInferenceMu/dmQueueCount pair.
	cogQueue *CogQueue

	// optimizer runs periodic journal reviews and proposes handbook amendments.
	optimizer *LeadOptimizer

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

	// Use a dispatchRef indirection so the file-write hook can reference the
	// orchestrator (o) even though it is created after tool registration.
	var dispatchFileWrite func(path string)
	if err := tools.RegisterAllWithHooks(mcpEng, cfg.AppConfig, cfg.OnBrainUpdate, func(path string) {
		if dispatchFileWrite != nil {
			dispatchFileWrite(path)
		}
	}); err != nil {
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
		HandbookPath:        cfg.HandbookPath,
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
		cogQueue:  NewCogQueue(cfg.OnQueueState, ollama.NewSystemMonitor()),
	}

	// Now that o exists, wire the file-write hook to dispatch KindFileCreated messages.
	dispatchFileWrite = func(path string) {
		if o.projectID == "" || o.convID == "" {
			return
		}
		o.disp.DispatchSummary(models.Message{
			ProjectID:      o.projectID,
			ConversationID: o.convID,
			Kind:           models.KindFileCreated,
			Tier:           models.TierSummary,
			Content:        path,
		})
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

	// Start the cognition queue and system pressure monitor together.
	// Both run for the lifetime of the process.
	bgCtx := context.Background()
	o.cogQueue.sysmon.Start(bgCtx)
	o.cogQueue.Start(bgCtx)

	// Wire the Lead Optimizer if an approval callback is configured.
	if cfg.OnApproval != nil && db != nil {
		o.optimizer = newLeadOptimizer(cfg, inferrer, db, log,
			func(projectID, proposalText string) {
				_ = db.CreateApproval(context.Background(), models.Approval{
					ProjectID:   projectID,
					Kind:        "handbook_proposal",
					SubjectID:   "handbook",
					Description: proposalText,
				})
				cfg.OnApproval(projectID)
			},
		)
	}

	return o, nil
}

// SetProject sets the active project ID and resolves (or creates) the stable
// war-room conversation for that project. Reuses an existing conversation so
// that chat history is preserved across channel switches and app restarts.
func (o *Orchestrator) SetProject(ctx context.Context, projectID string) error {
	o.projectID = projectID
	o.disp.SetProject(projectID)

	if o.db != nil {
		convID, err := o.db.GetOrCreateWarRoomConversation(ctx, projectID)
		if err != nil {
			return fmt.Errorf("orchestrator: get or create war-room conversation: %w", err)
		}
		o.convID = convID
	}
	return nil
}

// channelRawFn returns a function that routes raw activity events to the
// channel EngineRoom (war-room convID).  Set ra.OnRaw = o.channelRawFn(agentID)
// before calling Turn/TurnStream on a channel-scoped agent.
func (o *Orchestrator) channelRawFn(agentID string) func(models.MessageKind, string) {
	return func(kind models.MessageKind, content string) {
		o.disp.DispatchRaw(models.Message{
			ProjectID:      o.projectID,
			ConversationID: o.convID,
			AgentID:        agentID,
			Kind:           kind,
			Tier:           models.TierRaw,
			Content:        content,
		})
	}
}

// HandleBossCommand is the main entry point for a Boss instruction.
// It runs the full Lead → task decomposition → Worker loop.
// onChunk, if non-nil, receives streamed tokens from the Lead for the live
// typing effect in the channel chat (same mechanism as DM streaming).
func (o *Orchestrator) HandleBossCommand(ctx context.Context, command string, onChunk func(string)) error {
	o.disp.DispatchSummary(models.Message{
		ProjectID:      o.projectID,
		ConversationID: o.convID,
		Kind:           models.KindBossCommand,
		Tier:           models.TierSummary,
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
	o.lead.OnRaw = o.channelRawFn("lead")
	o.vram.NotifyLeadRunning()
	decomposed, err := o.lead.TurnStream(ctx, augmented, onChunk)
	o.lead.OnRaw = nil
	if err != nil {
		var lcErr *LowConfidenceError
		if errors.As(err, &lcErr) {
			o.disp.DispatchSummary(models.Message{
				ProjectID:      o.projectID,
				ConversationID: o.convID,
				Kind:           models.KindConsultation,
				Tier:           models.TierSummary,
				AgentID:        "lead",
				Content:        consultationContent("Lead", lcErr.Score, lcErr.Reason),
			})
			return nil
		}
		var escErr *EscalationNeededError
		if errors.As(err, &escErr) {
			return o.handleEscalation(ctx, escErr, command)
		}
		return fmt.Errorf("lead decomposition: %w", err)
	}

	o.disp.DispatchRaw(models.Message{
		ProjectID:      o.projectID,
		ConversationID: o.convID,
		Kind:           models.KindDraft,
		Tier:           models.TierRaw,
		Content:        "Lead plan: " + decomposed,
	})

	tasks := parseTaskList(decomposed)

	// Speculative pre-loading: in VRAMDual mode, begin warming the worker model
	// in the background while we persist tasks and prepare execution. This
	// eliminates cold-start latency for the first specialist task.
	for _, task := range tasks {
		if task.Assignee == "specialist" {
			go o.vram.PreWarm(context.Background(), o.cfg.WorkerModel)
			break
		}
	}
	if len(tasks) == 0 {
		// Lead may have just answered directly (simple tasks).
		o.disp.DispatchSummary(models.Message{
			ProjectID:      o.projectID,
			ConversationID: o.convID,
			Kind:           models.KindAgentMessage,
			Tier:           models.TierSummary,
			AgentID:        "lead",
			Content:        decomposed,
			Metadata:       thinkingMeta(o.lead.LastThinking),
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

		// Spec E: Post a social handoff message so the Boss sees the Lead's
		// reasoning before the specialist gets to work.
		if task.Justification != "" {
			o.disp.DispatchSummary(models.Message{
				ProjectID:      o.projectID,
				ConversationID: o.convID,
				Kind:           models.KindAgentMessage,
				Tier:           models.TierSummary,
				AgentID:        "lead",
				Content:        fmt.Sprintf("🎯 **%s** → assigning to specialist. %s", task.Title, task.Justification),
			})
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
				ProjectID:      o.projectID,
				ConversationID: o.convID,
				Kind:           models.KindSystemEvent,
				Tier:           models.TierSummary,
				Content:        fmt.Sprintf("⚠️ Task %s failed: %v", task.ID, workerErr),
			})
		}
		if !result.IsError && o.optimizer != nil {
			o.optimizer.NotifyTaskDone(o.projectID)
		}
		_ = result
	}

	// Step 4: Lead produces final summary milestone.
	o.leadMu.Lock()
	o.lead.OnRaw = o.channelRawFn("lead")
	o.vram.NotifyLeadRunning()
	summary, sumErr := o.lead.TurnStream(ctx, "All sub-tasks are complete. Provide a brief summary of what was accomplished for the Boss.", onChunk)
	o.lead.OnRaw = nil
	o.leadMu.Unlock()
	if sumErr == nil {
		o.disp.DispatchSummary(models.Message{
			ProjectID:      o.projectID,
			ConversationID: o.convID,
			Kind:           models.KindMilestone,
			Tier:           models.TierSummary,
			AgentID:        "lead",
			Content:        summary,
			Metadata:       thinkingMeta(o.lead.LastThinking),
		})
	}

	return nil
}

// OllamaHealthy reports whether the configured Ollama inference backend is
// currently reachable. Used by the WarRoomService for agent status management.
func (o *Orchestrator) OllamaHealthy(ctx context.Context) bool {
	return o.inferrer.IsHealthy(ctx)
}

// HandleDirectMessage sends a message directly to a named agent and dispatches
// the response back to the supplied DM conversation ID.
//
// Unlike HandleBossCommand, this bypasses Lead task-decomposition entirely.
// Each agentID gets its own isolated RunningAgent with the agent's real system
// prompt so DM history never mixes with the war-room conversation.
//
// DM inference is serialised: only one call runs against Ollama at a time.
// Concurrent callers are queued and receive a visible "queued" status message.
// onChunk, if non-nil, is called for every streamed token so the caller can
// forward chunks to the frontend for a live typing effect.
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

	// Wrap with structured pre-flight reasoning so the agent considers
	// identity changes and tool calls before composing its reply.
	augmented = dmTurnPrompt(augmented)

	// Notify the user if they'll have to wait — check state before enqueuing
	// so the message appears immediately rather than after the current call ends.
	if qs := o.cogQueue.State(); qs.Active || qs.P1 > 0 {
		waitPos := qs.P1 + 1
		o.disp.Dispatch(models.Message{
			ProjectID:      o.projectID,
			ConversationID: convID,
			AgentID:        agentID,
			Kind:           models.KindSystemEvent,
			Tier:           models.TierRaw,
			Content:        fmt.Sprintf("⏳ queued (position %d) — waiting for Ollama to finish current call", waitPos),
		})
	}

	// Submit to the cognition queue at P1 (Boss-level priority).
	// Blocks until the fn executes and returns.
	_, err := o.cogQueue.Submit(ctx, P1Lead, func(qctx context.Context) error {
		// Route all raw activity from TurnStream (API calls, tool calls, errors)
		// to the DM conversation's EngineRoom.
		ra.OnRaw = func(kind models.MessageKind, content string) {
			o.disp.Dispatch(models.Message{
				ProjectID:      o.projectID,
				ConversationID: convID,
				AgentID:        agentID,
				Kind:           kind,
				Tier:           models.TierRaw,
				Content:        content,
			})
		}
		defer func() { ra.OnRaw = nil }()

		// Apply a per-call watchdog: if inference runs longer than 30 minutes
		// the context is cancelled and an error is returned.
		watchdogCtx, watchdogCancel := context.WithTimeout(qctx, 30*time.Minute)
		defer watchdogCancel()

		response, err := ra.TurnStream(watchdogCtx, augmented, onChunk)
		if err != nil {
			// Low confidence: agent needs clarification — surface to the Boss, not as an error.
			var lcErr *LowConfidenceError
			if errors.As(err, &lcErr) {
				o.disp.Dispatch(models.Message{
					ProjectID:      o.projectID,
					ConversationID: convID,
					AgentID:        agentID,
					Kind:           models.KindConsultation,
					Tier:           models.TierSummary,
					Content:        consultationContent(agentID, lcErr.Score, lcErr.Reason),
				})
				return nil
			}
			return fmt.Errorf("dm %s: %w", agentID, err)
		}

		// Dispatch the agent reply to the DM conversation.
		// Include thinking content in metadata so the frontend can render it
		// as a collapsed block in the persisted chat history.
		meta := thinkingMeta(ra.LastThinking)
		o.disp.Dispatch(models.Message{
			ProjectID:      o.projectID,
			ConversationID: convID,
			AgentID:        agentID,
			Kind:           models.KindAgentMessage,
			Tier:           models.TierSummary,
			Content:        response,
			Metadata:       meta,
		})
		return nil
	})
	return err
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
			HandbookPath:        o.cfg.HandbookPath,
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

// InvalidateDMAgent clears the cached RunningAgent for agentID so that the
// next DM message triggers a fresh spawn from the (possibly updated) brain files.
func (o *Orchestrator) InvalidateDMAgent(agentID string) {
	o.dmAgentsMu.Lock()
	defer o.dmAgentsMu.Unlock()
	delete(o.dmAgents, agentID)
}

// MCPFragmentForAgent returns the MCP system-prompt fragment appropriate for
// the given agent's clearance level. Used by external callers (e.g. service)
// when recomposing instruction.md after a brain file edit.
func (o *Orchestrator) MCPFragmentForAgent(agentID string) string {
	if agentID == "lead" {
		return o.mcpEng.SystemPromptFragment(models.ClearanceLead)
	}
	return o.mcpEng.SystemPromptFragment(models.ClearanceSpecialist)
}

// thinkingMeta builds a JSON metadata string embedding thinking content.
// Returns "{}" when thinking is empty so the Message always has valid JSON.
func thinkingMeta(thinking string) string {
	if thinking == "" {
		return "{}"
	}
	b, err := json.Marshal(map[string]string{"thinking": thinking})
	if err != nil {
		return "{}"
	}
	return string(b)
}

// consultationContent formats the content of a KindConsultation message
// shown to the Boss when an agent's confidence score falls below threshold.
func consultationContent(agentID string, score float64, reason string) string {
	return fmt.Sprintf("❓ **%s** needs clarification before proceeding (confidence: %d%%).\n\n%s",
		agentID, int(score*100), reason)
}

// CultureBroadcast forces a full context reset on all active agents.
// Called when COMPANY_IDENTITY.md is edited.
func (o *Orchestrator) CultureBroadcast(newIdentityPath string) error {
	o.leadMu.Lock()
	defer o.leadMu.Unlock()

	mcpFragment := o.mcpEng.SystemPromptFragment(models.ClearanceLead)
	if err := o.leadAgent.CultureUpdate(newIdentityPath, o.cfg.HandbookPath, mcpFragment); err != nil {
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

// HandbookBroadcast forces a full context reset on all active agents with the
// updated handbook. Called when handbook.md is edited.
func (o *Orchestrator) HandbookBroadcast(handbookPath string) error {
	o.leadMu.Lock()
	defer o.leadMu.Unlock()
	o.cfg.HandbookPath = handbookPath
	mcpFragment := o.mcpEng.SystemPromptFragment(models.ClearanceLead)
	if err := o.leadAgent.CultureUpdate(o.cfg.CompanyIdentityPath, handbookPath, mcpFragment); err != nil {
		return fmt.Errorf("handbook broadcast: lead update: %w", err)
	}
	o.lead.ResetContext(o.leadAgent.SystemPrompt())
	o.disp.DispatchSummary(models.Message{
		ProjectID: o.projectID,
		Kind:      models.KindSystemEvent,
		Tier:      models.TierSummary,
		Content:   "📋 Handbook updated — all active agents have received the new handbook",
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
