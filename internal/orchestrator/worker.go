package orchestrator

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/haepapa/kotui/internal/agent"
	"github.com/haepapa/kotui/internal/dispatcher"
	"github.com/haepapa/kotui/internal/mcp"
	"github.com/haepapa/kotui/internal/memory"
	"github.com/haepapa/kotui/internal/ollama"
	"github.com/haepapa/kotui/internal/store"
	"github.com/haepapa/kotui/pkg/models"
)

const verifyMaxRetries = 2 // Lead re-queues Worker up to this many times

// WorkerJob carries a task assignment from the Lead to a Worker.
type WorkerJob struct {
	TaskID         string
	Instruction    string
	ProjectID      string
	ConvID         string
	AgentID        string // optional: stable agent identity for memory recall
	PastExperience string // recalled journal entries from memory.FormatRecall
}

// WorkerResult is what the Worker returns after completing (or failing) a job.
type WorkerResult struct {
	TaskID  string
	Output  string
	IsError bool
}

// spawnWorker creates a new Specialist RunningAgent for a single task.
// The caller must call vram.AcquireWorkerSlot before invoking this.
func spawnWorker(
	cfg OrchestratorConfig,
	inferrer Inferrer,
	mcpEng *mcp.Engine,
	job WorkerJob,
) (*RunningAgent, *agent.Agent, error) {
	workerID := fmt.Sprintf("worker-%d", time.Now().UnixNano())

	spawnedAgent, err := agent.Spawn(agent.SpawnConfig{
		ID:                  workerID,
		Name:                "Worker",
		Role:                models.RoleSpecialist,
		Model:               cfg.WorkerModel,
		ProjectID:           job.ProjectID,
		DataDir:             cfg.DataDir,
		CompanyIdentityPath: cfg.CompanyIdentityPath,
		MCPFragment:         mcpEng.SystemPromptFragment(models.ClearanceSpecialist),
		PastExperience:      job.PastExperience,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("spawn worker: %w", err)
	}

	ra := newRunningAgent(
		workerID, "Worker", cfg.WorkerModel,
		models.ClearanceSpecialist,
		ollama.Release(), // Workers always release VRAM after each request
		spawnedAgent.SystemPrompt(),
		inferrer, mcpEng,
	)
	return ra, spawnedAgent, nil
}

// makeRawFn returns an OnRaw callback that routes raw activity events to the
// specified conversation's EngineRoom via the Dispatcher.
func makeRawFn(disp *dispatcher.Dispatcher, projectID, convID, agentID string) func(models.MessageKind, string) {
	return func(kind models.MessageKind, content string) {
		disp.DispatchRaw(models.Message{
			ProjectID:      projectID,
			ConversationID: convID,
			AgentID:        agentID,
			Kind:           kind,
			Tier:           models.TierRaw,
			Content:        content,
		})
	}
}

// runWorkerTask runs a single Worker task with Lead verification.
// It loops up to verifyMaxRetries times if the Lead rejects the output.
func runWorkerTask(
	ctx context.Context,
	cfg OrchestratorConfig,
	inferrer Inferrer,
	mcpEng *mcp.Engine,
	lead *RunningAgent,
	vram *VRAMCoordinator,
	db *store.DB,
	disp *dispatcher.Dispatcher,
	job WorkerJob,
	mem *memory.Store,
	log *slog.Logger,
) (WorkerResult, error) {
	// Acquire VRAM slot — parks Lead in swap mode, skips park for logical swap
	// (same model as the previous worker, still hot in VRAM).
	if err := vram.AcquireWorkerSlot(ctx, cfg.WorkerModel); err != nil {
		return WorkerResult{TaskID: job.TaskID, IsError: true},
			fmt.Errorf("worker slot: %w", err)
	}
	defer vram.ReleaseWorkerSlot(ctx)

	// Update task status.
	if db != nil {
		_ = db.UpdateTaskStatus(ctx, job.TaskID, "in_progress")
	}

	disp.DispatchRaw(models.Message{
		ProjectID:      job.ProjectID,
		ConversationID: job.ConvID,
		Kind:           models.KindSystemEvent,
		Tier:           models.TierRaw,
		Content:        fmt.Sprintf("[Worker] starting task %s", job.TaskID),
	})

	// Recall relevant memories for this worker.
	if mem != nil && job.ProjectID != "" {
		agentID := job.AgentID
		if agentID == "" {
			agentID = "worker"
		}
		entries, recallErr := mem.Recall(ctx, agentID, job.ProjectID, job.Instruction, 3)
		if recallErr == nil {
			job.PastExperience = memory.FormatRecall(entries)
		}
	}

	var lastOutput string
	for attempt := 0; attempt <= verifyMaxRetries; attempt++ {
		// Spawn a fresh Worker agent for each attempt.
		workerRA, workerAgent, err := spawnWorker(cfg, inferrer, mcpEng, job)
		if err != nil {
			return WorkerResult{TaskID: job.TaskID, IsError: true}, err
		}

		// Route raw activity (API calls, tool calls) to the channel EngineRoom.
		workerRA.OnRaw = makeRawFn(disp, job.ProjectID, job.ConvID, workerRA.AgentID)
		lead.OnRaw = makeRawFn(disp, job.ProjectID, job.ConvID, "lead")

		// Worker executes.
		output, err := workerRA.Turn(ctx, job.Instruction)
		lead.OnRaw = nil
		if err != nil {
			// Teardown with failure journal.
			workerAgent.Teardown(agent.JournalEntry{
				Task: job.Instruction, Outcome: "failure",
				Summary: fmt.Sprintf("Turn error on attempt %d: %v", attempt+1, err),
			})
			lastOutput = fmt.Sprintf("error: %v", err)
			continue
		}

		// Post draft (raw tier, not visible to Boss).
		disp.DispatchRaw(models.Message{
			ProjectID:      job.ProjectID,
			ConversationID: job.ConvID,
			Kind:           models.KindDraft,
			Tier:           models.TierRaw,
			AgentID:        workerRA.AgentID,
			Content:        output,
		})

		// Lead verifies (if in swap mode, Lead reloads now).
		// Verification prompt: sets an explicit "adequate = approved" bar and
		// suppresses the handbook CS protocol (irrelevant for verdict output).
		verifyInstruction := fmt.Sprintf(
			"VERIFICATION — do NOT output a confidence_score JSON line.\n\n"+
				"A Specialist completed the following task:\n%s\n\n"+
				"Specialist output:\n%s\n\n"+
				"Respond with exactly ONE of:\n"+
				"  APPROVED — the output adequately addresses the task (perfection is NOT required)\n"+
				"  CORRECTION: <specific issue> — ONLY if there is a clear factual error or an explicitly required element is completely absent\n\n"+
				"Default to APPROVED for any reasonable output. When in doubt, APPROVED.",
			job.Instruction, output)

		lead.OnRaw = makeRawFn(disp, job.ProjectID, job.ConvID, "lead")
		verdict, verifyErr := lead.Turn(ctx, verifyInstruction)
		lead.OnRaw = nil
		// Strip any CS signal lines so they cannot mask the APPROVED/CORRECTION token.
		verdict = stripSignalLines(verdict)
		journalEntry := agent.JournalEntry{
			Task:    job.Instruction,
			Outcome: "success",
			Summary: fmt.Sprintf("Completed on attempt %d. Output: %s", attempt+1, truncate(output, 200)),
		}
		workerAgent.Teardown(journalEntry)
		if mem != nil {
			agentID := job.AgentID
			if agentID == "" {
				agentID = "worker"
			}
			content := journalEntry.Task + "\n" + journalEntry.Summary
			mem.IndexAsync(ctx, agentID, job.ProjectID, content, false)
		}

		if verifyErr != nil {
			log.Warn("lead verification error", "err", verifyErr, "task", job.TaskID)
			lastOutput = output
			break // Accept the output rather than loop on verify error
		}

		if isApproved(verdict) {
			if db != nil {
				_ = db.UpdateTaskStatus(ctx, job.TaskID, "done")
			}
			disp.DispatchSummary(models.Message{
				ProjectID:      job.ProjectID,
				ConversationID: job.ConvID,
				Kind:           models.KindMilestone,
				Tier:           models.TierSummary,
				Content:        fmt.Sprintf("✓ Task completed: %s", job.TaskID),
			})
			return WorkerResult{TaskID: job.TaskID, Output: output}, nil
		}

		// Lead requested a correction.
		job.Instruction = extractCorrection(verdict, job.Instruction)
		lastOutput = output
		log.Info("lead requested correction", "task", job.TaskID, "attempt", attempt+1)
	}

	// All retries exhausted. If the worker produced any output at all, accept
	// it as a soft success: the Lead kept requesting improvements but the output
	// is usable. This prevents the confusing "failed" state when the worker did
	// produce content and the Lead was simply being overly critical.
	if lastOutput != "" {
		if db != nil {
			_ = db.UpdateTaskStatus(ctx, job.TaskID, "done")
		}
		disp.DispatchSummary(models.Message{
			ProjectID:      job.ProjectID,
			ConversationID: job.ConvID,
			Kind:           models.KindMilestone,
			Tier:           models.TierSummary,
			Content:        fmt.Sprintf("✓ Task completed (accepted after review): %s", job.TaskID),
		})
		return WorkerResult{TaskID: job.TaskID, Output: lastOutput}, nil
	}
	if db != nil {
		_ = db.UpdateTaskStatus(ctx, job.TaskID, "failed")
	}
	return WorkerResult{TaskID: job.TaskID, Output: lastOutput, IsError: true},
		fmt.Errorf("task %s: worker produced no output", job.TaskID)
}

// isApproved checks whether the Lead's verification response signals approval.
// stripSignalLines removes JSON signal lines (confidence_score, propose_handbook,
// escalation_needed) from a verdict string so they cannot mask APPROVED/CORRECTION.
func stripSignalLines(verdict string) string {
	var out []string
	for _, line := range splitLines(verdict) {
		t := trimSpace(line)
		if len(t) > 0 && t[0] == '{' {
			if indexOf(t, "confidence_score") >= 0 ||
				indexOf(t, "propose_handbook") >= 0 ||
				indexOf(t, "escalation_needed") >= 0 {
				continue
			}
		}
		out = append(out, line)
	}
	return trimSpace(joinLines(out))
}

func splitLines(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	out = append(out, s[start:])
	return out
}

func joinLines(lines []string) string {
	total := 0
	for _, l := range lines {
		total += len(l) + 1
	}
	b := make([]byte, 0, total)
	for i, l := range lines {
		if i > 0 {
			b = append(b, '\n')
		}
		b = append(b, l...)
	}
	return string(b)
}

func isApproved(verdict string) bool {
	upper := trimUpper(verdict)
	// Primary: explicit APPROVED token.
	if upper == "APPROVED" || containsWord(upper, "APPROVED") {
		return true
	}
	// Secondary: positive phrases the Lead may use instead of the token.
	positives := []string{"LOOKS GOOD", "WELL DONE", "MEETS THE REQUIREMENTS",
		"MEETS REQUIREMENTS", "SATISFACTORY", "ACCEPTABLE", "ADEQUATE",
		"OUTPUT IS GOOD", "OUTPUT IS CORRECT", "OUTPUT IS COMPLETE"}
	for _, p := range positives {
		if indexOf(upper, p) >= 0 {
			return true
		}
	}
	return false
}

// extractCorrection pulls the correction instruction from a verdict like
// "CORRECTION: rewrite the error handling".
func extractCorrection(verdict, originalInstruction string) string {
	const prefix = "CORRECTION:"
	idx := indexOf(trimUpper(verdict), prefix)
	if idx >= 0 {
		correction := verdict[idx+len(prefix):]
		return fmt.Sprintf("Previous attempt was rejected. Correction required: %s\n\nOriginal task: %s",
			trimSpace(correction), originalInstruction)
	}
	return originalInstruction
}

// trimUpper returns the string trimmed and uppercased for comparison.
func trimUpper(s string) string {
	for _, c := range s {
		_ = c
		break
	}
	result := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		b := s[i]
		if b >= 'a' && b <= 'z' {
			b -= 32
		}
		result = append(result, b)
	}
	return trimSpace(string(result))
}

func trimSpace(s string) string {
	return trimLeft(trimRight(s))
}

func trimLeft(s string) string {
	i := 0
	for i < len(s) && (s[i] == ' ' || s[i] == '\t' || s[i] == '\n' || s[i] == '\r') {
		i++
	}
	return s[i:]
}

func trimRight(s string) string {
	i := len(s)
	for i > 0 && (s[i-1] == ' ' || s[i-1] == '\t' || s[i-1] == '\n' || s[i-1] == '\r') {
		i--
	}
	return s[:i]
}

func containsWord(s, word string) bool {
	return len(s) >= len(word) && (s == word ||
		(len(s) > len(word) && (s[:len(word)] == word || s[len(s)-len(word):] == word ||
			indexOf(s, word) >= 0)))
}

func indexOf(s, sub string) int {
	if len(sub) > len(s) {
		return -1
	}
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
