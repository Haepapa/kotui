package orchestrator

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/haepapa/kotui/internal/dispatcher"
	"github.com/haepapa/kotui/internal/mcp"
	"github.com/haepapa/kotui/internal/ollama"
	"github.com/haepapa/kotui/pkg/models"
)

// CuriosityThreshold is the number of completed specialist tasks between each
// Watchman critic pass.
const CuriosityThreshold = 5

// watchmanSystemPrompt is the ProjectCritic system prompt. The Watchman is a
// read-only specialist: it does not create files or run commands, it only
// scans the workspace and reports its findings.
const watchmanSystemPrompt = `You are the Watchman — a silent project critic embedded in the virtual company.

Your sole purpose is to scan the workspace for issues that the team may have overlooked and post a short, actionable report to the Boss.

Focus on:
1. Architectural consistency — naming conventions, folder structure, duplicated logic.
2. Missing tests or undocumented functions in recently changed files.
3. Potential migration rollbacks — schema changes without down-migrations, breaking API changes.
4. Incomplete work — files named TODO/DRAFT/WIP that have been sitting for more than one session.
5. Security hygiene — hardcoded secrets, overly permissive file operations.

Rules:
- Be concise: no more than 5 bullet points. Each point must name a specific file or module.
- Do NOT propose solutions, assign tasks, or modify any files. Observation only.
- If the workspace looks healthy, output exactly: ✅ Workspace looks healthy — no issues found.
- Always start your message with: 🔍 **Watchman Report**`

// CuriosityLoop fires a Watchman critic pass after every CuriosityThreshold
// completed specialist tasks. The Watchman scans the project workspace and
// posts its findings as a KindAgentMessage in the group channel.
type CuriosityLoop struct {
	cfg      OrchestratorConfig
	inferrer Inferrer
	mcpEng   *mcp.Engine
	disp     *dispatcher.Dispatcher
	log      *slog.Logger

	mu        sync.Mutex
	tasksDone int
	reviewing bool
}

// newCuriosityLoop creates a CuriosityLoop.
func newCuriosityLoop(
	cfg OrchestratorConfig,
	inferrer Inferrer,
	mcpEng *mcp.Engine,
	disp *dispatcher.Dispatcher,
	log *slog.Logger,
) *CuriosityLoop {
	return &CuriosityLoop{
		cfg:      cfg,
		inferrer: inferrer,
		mcpEng:   mcpEng,
		disp:     disp,
		log:      log,
	}
}

// NotifyTaskDone increments the counter and, when CuriosityThreshold is
// reached, starts a background Watchman pass (at most one at a time).
func (cl *CuriosityLoop) NotifyTaskDone(projectID, convID string) {
	if projectID == "" || convID == "" {
		return
	}
	cl.mu.Lock()
	cl.tasksDone++
	due := cl.tasksDone%CuriosityThreshold == 0
	if due && cl.reviewing {
		due = false // skip if a review is already in flight
	}
	if due {
		cl.reviewing = true
	}
	cl.mu.Unlock()

	if due {
		go cl.runCritique(projectID, convID)
	}
}

// runCritique spawns a throw-away Watchman RunningAgent, runs a single
// inference turn to scan the workspace, and dispatches the findings.
func (cl *CuriosityLoop) runCritique(projectID, convID string) {
	defer func() {
		cl.mu.Lock()
		cl.reviewing = false
		cl.mu.Unlock()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	watchmanID := fmt.Sprintf("watchman-%d", time.Now().UnixNano())

	ra := newRunningAgent(
		watchmanID, "Watchman",
		cl.cfg.WorkerModel,
		models.ClearanceSpecialist,
		ollama.Release(),
		watchmanSystemPrompt,
		cl.inferrer,
		cl.mcpEng,
	)

	ra.OnRaw = func(kind models.MessageKind, content string) {
		cl.disp.DispatchRaw(models.Message{
			ProjectID:      projectID,
			ConversationID: convID,
			AgentID:        watchmanID,
			Kind:           kind,
			Tier:           models.TierRaw,
			Content:        content,
		})
	}

	findings, err := ra.Turn(ctx, "Please perform your workspace critique now.")
	if err != nil {
		cl.log.Warn("watchman critique failed", "err", err)
		return
	}

	if findings == "" {
		return
	}

	cl.disp.DispatchSummary(models.Message{
		ProjectID:      projectID,
		ConversationID: convID,
		AgentID:        watchmanID,
		Kind:           models.KindAgentMessage,
		Tier:           models.TierSummary,
		Content:        findings,
	})

	cl.log.Info("watchman critique posted", "project", projectID)
}
