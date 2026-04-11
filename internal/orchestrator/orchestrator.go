// Package orchestrator is the top-level coordinator of the Kōtui Virtual Company.
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
	"strings"
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

	// SudoGate, if non-nil, enables the Boss-approval workflow for sudo commands.
	// When nil, sudo commands are hard-blocked.
	SudoGate *tools.SudoGate
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

	// curiosity runs a Watchman critic pass after every CuriosityThreshold tasks.
	curiosity *CuriosityLoop

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
	// Same pattern for memRef: the memory store is built after registration.
	var dispatchFileWrite func(path string)
	var memRef *memory.Store // set after o.memory is built
	if err := tools.RegisterAllWithHooks(mcpEng, cfg.AppConfig, cfg.OnBrainUpdate, func(path string) {
		if dispatchFileWrite != nil {
			dispatchFileWrite(path)
		}
	}, &memRef, cfg.SudoGate); err != nil {
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
			memRef = o.memory // wire the KB tool's lazy getter
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

	// CuriosityLoop: always active — fires a Watchman critic pass every
	// CuriosityThreshold completed specialist tasks.
	o.curiosity = newCuriosityLoop(cfg, inferrer, o.mcpEng, o.disp, log)

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

		// Seed the Lead agent with the last 20 turns from this channel's
		// persisted history so it can recall prior exchanges after a restart.
		if msgs, hErr := o.db.ListConversationHistory(ctx, convID, 20); hErr == nil && len(msgs) > 0 {
			o.lead.SeedHistory(messagesToChatHistory(msgs))
			o.log.Info("orchestrator: seeded lead history", "turns", len(msgs), "conv", convID)
		}
	}
	return nil
}

// messagesToChatHistory converts persisted summary messages to Ollama chat
// history entries. boss_command → user role; agent_message/consultation →
// assistant role. Other kinds are skipped.
func messagesToChatHistory(msgs []models.Message) []ollama.ChatMessage {
	out := make([]ollama.ChatMessage, 0, len(msgs))
	for _, m := range msgs {
		switch m.Kind {
		case models.KindBossCommand:
			out = append(out, ollama.ChatMessage{Role: "user", Content: m.Content})
		case models.KindAgentMessage, models.KindConsultation:
			out = append(out, ollama.ChatMessage{Role: "assistant", Content: m.Content})
		}
	}
	return out
}

// ProjectID returns the currently active project ID.
func (o *Orchestrator) ProjectID() string { return o.projectID }

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

// classifyIntent runs a fast, lightweight classification call to determine the
// intent of a Boss message before the main inference. It creates a minimal
// ephemeral agent (no system prompt, no history) so the classification is pure
// and unaffected by accumulated context.
//
// Returns one of: "TASK", "BRIEF", "CHAT", "FOLLOWUP".
// Falls back to "TASK" on any error so the main pipeline always proceeds.
func (o *Orchestrator) classifyIntent(ctx context.Context, command string) string {
	lastReply := o.lead.LastAssistantMessage()
	prompt := classifyPrompt(command, lastReply)

	// Ephemeral classifier agent: no system prompt, no MCP tools, minimal options.
	classifier := newRunningAgent(
		"classifier", "Classifier",
		o.cfg.LeadModel,
		models.ClearanceLead,
		ollama.ForDuration(30*time.Second), // briefly warm, then unload
		"",                                  // no system prompt
		o.inferrer, o.mcpEng,
	)
	// Use a short timeout — classification should be near-instant.
	classCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	result, err := classifier.TurnOnce(classCtx, prompt, &ollama.ModelOptions{
		NumPredict:  10,
		ThinkBudget: 0,
	})
	if err != nil {
		o.log.Warn("classifyIntent: inference failed, defaulting to TASK", "err", err)
		return "TASK"
	}

	// Parse — accept the first recognised word anywhere in the response.
	upper := strings.ToUpper(strings.TrimSpace(result))
	for _, candidate := range []string{"FOLLOWUP", "BRIEF", "CHAT", "TASK"} {
		if strings.Contains(upper, candidate) {
			return candidate
		}
	}
	o.log.Warn("classifyIntent: unrecognised response, defaulting to TASK", "response", result)
	return "TASK"
}

// HandleBossCommand is the main entry point for a Boss instruction.
// It runs the full Lead → task decomposition → Worker loop.
// onChunk, if non-nil, receives streamed tokens from the Lead for the live
// typing effect in the channel chat (same mechanism as DM streaming).
func (o *Orchestrator) HandleBossCommand(ctx context.Context, command string, onChunk func(string)) error {
	// Apply a per-call watchdog so a stuck or over-thinking model can't block
	// the channel indefinitely. 30 minutes matches the DM path watchdog.
	watchdogCtx, watchdogCancel := context.WithTimeout(ctx, 30*time.Minute)
	defer watchdogCancel()
	ctx = watchdogCtx

	o.disp.DispatchSummary(models.Message{
		ProjectID:      o.projectID,
		ConversationID: o.convID,
		Kind:           models.KindBossCommand,
		Tier:           models.TierSummary,
		Content:   command,
	})
	// Also route to the Engine Room (TierRaw) so the activity log shows user messages.
	o.disp.DispatchRaw(models.Message{
		ProjectID:      o.projectID,
		ConversationID: o.convID,
		AgentID:        "boss",
		Kind:           models.KindBossCommand,
		Tier:           models.TierRaw,
		Content:        command,
	})

	o.log.Info("boss command received", "command", truncate(command, 80))

	// Step 1: Classify intent then route to the appropriate prompt template.
	//
	// Option 6 (two-step): a fast classification call (no thinking, ≤10 tokens)
	// determines the message category before the main inference. This prevents
	// "context drowning" where decomposePrompt() acts as a full re-onboarding on
	// every turn, causing small models to forget prior context.
	//
	// Option 4 (aligned prompts): each category gets a purpose-built prompt that
	// mirrors the DM prompt's "Understand → Act" structure:
	//   TASK     → decomposePrompt (first turn) or taskFollowUpPrompt (with history)
	//   BRIEF    → briefAckPrompt (warm acknowledgement, no task list)
	//   CHAT     → chatReplyPrompt (conversational, no JSON)
	//   FOLLOWUP → followUpPrompt (direct action on prior output, skip decompose)
	//
	// IMPORTANT: always call InjectHistoryEntry(command) before TurnStream so
	// the raw user message (not the augmented wrapper) is stored in history.
	lastReply := o.lead.LastAssistantMessage()
	hasHistory := len(o.lead.History()) > 0

	// Run classification (skipped when there is no prior history — first message
	// is always routed through the full decomposePrompt to establish context).
	intent := "TASK"
	if hasHistory {
		intent = o.classifyIntent(ctx, command)
		o.log.Info("intent classified", "intent", intent, "command", truncate(command, 60))
	}

	var augmented string
	switch intent {
	case "FOLLOWUP":
		augmented = followUpPrompt(command, lastReply)
	case "BRIEF":
		augmented = briefAckPrompt(command)
	case "CHAT":
		augmented = chatReplyPrompt(command)
	default: // "TASK" (and fallback)
		augmented = decomposePrompt(command)
	}

	// Store the raw command in history (not the augmented wrapper) so that
	// follow-up turns see a natural conversation, not prompt-engineering boilerplate.
	o.lead.InjectHistoryEntry(command)
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
		var toErr *InferenceTimeoutError
		if errors.As(err, &toErr) {
			o.disp.DispatchSummary(models.Message{
				ProjectID:      o.projectID,
				ConversationID: o.convID,
				Kind:           models.KindSystemEvent,
				Tier:           models.TierSummary,
				AgentID:        "lead",
				Content:        fmt.Sprintf("⏱ **Inference timed out** after %.0f seconds — the model was still thinking when the limit was reached.\n\nTo fix this: go to **Settings → Local Ollama → Inference Timeout** and increase the value (e.g. 600 for 10 minutes). The current timeout may be too short for this model.", toErr.Elapsed),
			})
			return nil
		}
		if errors.Is(err, context.Canceled) {
			o.disp.DispatchSummary(models.Message{
				ProjectID:      o.projectID,
				ConversationID: o.convID,
				Kind:           models.KindSystemEvent,
				Tier:           models.TierSummary,
				AgentID:        "lead",
				Content:        "⛔ Stopped by user.",
			})
			return nil
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

	// Format repair: if no JSON task list was found but the response looks like
	// a plain-text decomposition, ask the model to re-emit as JSON. This handles
	// models that describe tasks in prose instead of following the format.
	if len(tasks) == 0 && looksLikeDecomposition(decomposed) {
		o.log.Info("task list not found, attempting format repair", "content_preview", decomposed[:min(80, len(decomposed))])
		if repaired, repairErr := o.repairTaskList(ctx, decomposed); repairErr == nil {
			tasks = parseTaskList(repaired)
			if len(tasks) > 0 {
				o.log.Info("format repair succeeded", "tasks", len(tasks))
			}
		}
	}

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
		// Lead answered directly (conversational, project brief, or simple task).
		reply := humanReadableDecomposed(decomposed)
		o.disp.DispatchSummary(models.Message{
			ProjectID:      o.projectID,
			ConversationID: o.convID,
			Kind:           models.KindAgentMessage,
			Tier:           models.TierSummary,
			AgentID:        "lead",
			Content:        reply,
			Metadata:       thinkingMeta(o.lead.LastThinking),
		})
		// Automatic fallback journal — ensures a record is written even if the
		// agent did not call write_journal itself. The agent's own entry (if any)
		// will be richer; this guarantees the diary is never empty after a turn.
		go func() {
			paths := agent.AgentPaths(o.cfg.DataDir, "lead")
			_ = agent.WriteJournal(paths, agent.JournalEntry{
				Task:    truncate(command, 120),
				Outcome: "success",
				Summary: truncate(reply, 300),
			})
		}()
		return nil
	}

	// Option 2: dispatch any prose the Lead wrote before the task list as a
	// social acknowledgement. This ensures the Boss always sees a human-readable
	// response before the 🎯 assignment messages, even when work begins immediately.
	if preamble := humanReadableDecomposed(decomposed); preamble != "" {
		o.disp.DispatchSummary(models.Message{
			ProjectID:      o.projectID,
			ConversationID: o.convID,
			Kind:           models.KindAgentMessage,
			Tier:           models.TierSummary,
			AgentID:        "lead",
			Content:        preamble,
			Metadata:       thinkingMeta(o.lead.LastThinking),
		})
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
		if !result.IsError {
			o.curiosity.NotifyTaskDone(o.projectID, o.convID)
		}
		_ = result
	}

	// Step 4: Lead produces final summary milestone.
	// The summary prompt explicitly forbids tool calls so the model does not
	// attempt to write_journal mid-stream — that would send confusing content
	// to the frontend and may produce a stale streaming bubble if the second
	// loop response is poor.
	o.leadMu.Lock()
	o.lead.OnRaw = o.channelRawFn("lead")
	o.vram.NotifyLeadRunning()
	summary, sumErr := o.lead.TurnStream(ctx,
		"All sub-tasks are complete. Write a brief, warm summary for the Boss — "+
			"mention what was accomplished, where any key outputs ended up (e.g. files created), "+
			"and offer a natural next-step or ask if they'd like to adjust anything. "+
			"Sound like a colleague wrapping up a task, not a system log. "+
			"Respond with plain conversational text only — do NOT call any tools.",
		onChunk)
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

// looksLikeDecomposition returns true if text looks like a plain-text task
// decomposition that the model forgot to format as JSON. Used to decide
// whether to attempt a format repair pass.
//
// We are intentionally strict here: code explanations, prose answers, and
// numbered tutorial steps all contain words like "step 1" or "implement",
// so we require task-specific structural signals and explicitly bail out
// when the response contains fenced code blocks (which indicate a code
// answer, not a task plan).
func looksLikeDecomposition(text string) bool {
	// Code blocks are a strong negative signal — a p5.js tutorial or shell
	// script with "step 1" and "implement" must not trigger repair.
	if strings.Count(text, "```") >= 2 {
		return false
	}
	lower := strings.ToLower(text)
	signals := []string{
		"task decomposition", "decompose", "sub-task", "subtask",
		"t1:", "t2:", "task 1:", "task 2:",
		"assign to specialist", "assign to lead",
		"knowledge_base", "file_manager",
		"task list", "here are the tasks",
	}
	count := 0
	for _, s := range signals {
		if strings.Contains(lower, s) {
			count++
		}
	}
	return count >= 2 && len(strings.Fields(text)) > 15
}

// repairTaskList sends a short follow-up asking the model to re-emit its plan
// as a JSON array. Used when the model produced a valid-looking decomposition
// but in the wrong format. Uses Turn (non-streaming) since this is invisible,
// but does enable OnRaw so the repair API call appears in the activity log.
func (o *Orchestrator) repairTaskList(ctx context.Context, decomposed string) (string, error) {
	o.lead.OnRaw = o.channelRawFn("lead")
	defer func() { o.lead.OnRaw = nil }()
	repair := fmt.Sprintf(
		"Your previous response described tasks in plain text, but the system requires a JSON array to execute them. "+
			"Re-emit your task list NOW as a JSON array on ONE line in exactly this format:\n"+
			`[{"id":"t1","title":"short","description":"detail","assignee":"specialist","justification":"why"}]`+"\n\n"+
			"Previous plan:\n%s\n\nOutput ONLY the JSON array line, nothing else.",
		decomposed,
	)
	return o.lead.Turn(ctx, repair)
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
		// Seed the new DM agent with its prior conversation history so it can
		// recall previous exchanges after an app restart.
		if o.db != nil {
			if dmConvID, err := o.db.GetDMConversation(ctx, agentID); err == nil && dmConvID != "" {
				if msgs, hErr := o.db.ListConversationHistory(ctx, dmConvID, 20); hErr == nil && len(msgs) > 0 {
					ra.SeedHistory(messagesToChatHistory(msgs))
					o.log.Info("orchestrator: seeded dm agent history", "agent", agentID, "turns", len(msgs))
				}
			}
		}
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
	// For follow-up turns, prepend a reminder of the last response so small
	// models can't ignore their own prior output.
	if lastReply := ra.LastAssistantMessage(); lastReply != "" {
		trimmed := lastReply
		if len(trimmed) > 1500 {
			trimmed = trimmed[:1500] + "\n…[truncated — full response is in your conversation history]"
		}
		augmented = fmt.Sprintf("Your previous response was:\n---\n%s\n---\n\n%s", trimmed, augmented)
	}
	augmented = dmTurnPrompt(augmented)

	// Store the raw user message in history (not the dmTurnPrompt wrapper) so
	// subsequent turns see a natural conversation rather than repeated boilerplate.
	ra.InjectHistoryEntry(message)

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
			// Inference timeout: show a helpful message rather than silently failing.
			var toErr *InferenceTimeoutError
			if errors.As(err, &toErr) {
				o.disp.Dispatch(models.Message{
					ProjectID:      o.projectID,
					ConversationID: convID,
					AgentID:        agentID,
					Kind:           models.KindSystemEvent,
					Tier:           models.TierSummary,
					Content:        fmt.Sprintf("⏱ **Inference timed out** after %.0f seconds — the model was still thinking when the limit was reached.\n\nTo fix this: go to **Settings → Local Ollama → Inference Timeout** and increase the value (e.g. 600 for 10 minutes).", toErr.Elapsed),
				})
				return nil
			}
			if errors.Is(err, context.Canceled) {
				o.disp.DispatchSummary(models.Message{
					ProjectID:      o.projectID,
					ConversationID: convID,
					Kind:           models.KindSystemEvent,
					Tier:           models.TierSummary,
					AgentID:        agentID,
					Content:        "⛔ Stopped by user.",
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

		// Automatic fallback journal for DM agents — guarantees a diary entry
		// is written after every successful turn even when the agent did not call
		// write_journal itself. The agent-written entry (if any) is richer.
		go func() {
			paths := agent.AgentPaths(o.cfg.DataDir, agentID)
			_ = agent.WriteJournal(paths, agent.JournalEntry{
				Task:    truncate(message, 120),
				Outcome: "success",
				Summary: truncate(response, 300),
			})
		}()

		// Spec C: After >= 3 Boss messages, enqueue a background self-reflection.
		// This runs at P3 so it never blocks the Boss's next interaction.
		if ra.countBossMessages() >= reflectMinBossMessages {
			historySnap := ra.buildHistoryText()
			dataDir := o.cfg.DataDir
			go func() {
				_, _ = o.cogQueue.Submit(context.Background(), P3Background, func(qctx context.Context) error {
					return ra.Reflect(qctx, dataDir, historySnap)
				})
			}()
		}

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

// MCPEngine returns the MCP engine for tool introspection.
func (o *Orchestrator) MCPEngine() *mcp.Engine { return o.mcpEng }

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
