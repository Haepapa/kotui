package orchestrator

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/haepapa/kotui/internal/agent"
	"github.com/haepapa/kotui/internal/dispatcher"
	"github.com/haepapa/kotui/internal/mcp"
	"github.com/haepapa/kotui/internal/ollama"
	"github.com/haepapa/kotui/internal/store"
	"github.com/haepapa/kotui/pkg/models"
)

const verifyMaxRetries = 2 // Lead re-queues Worker up to this many times

// WorkerJob carries a task assignment from the Lead to a Worker.
type WorkerJob struct {
	TaskID      string
	Instruction string
	ProjectID   string
	ConvID      string
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
	log *slog.Logger,
) (WorkerResult, error) {
	// Acquire VRAM slot — parks Lead in swap mode.
	if err := vram.AcquireWorkerSlot(ctx); err != nil {
		return WorkerResult{TaskID: job.TaskID, IsError: true},
			fmt.Errorf("worker slot: %w", err)
	}
	defer vram.ReleaseWorkerSlot(ctx)

	// Update task status.
	if db != nil {
		_ = db.UpdateTaskStatus(ctx, job.TaskID, "in_progress")
	}

	disp.DispatchRaw(models.Message{
		ProjectID: job.ProjectID,
		Kind:      models.KindSystemEvent,
		Tier:      models.TierRaw,
		Content:   fmt.Sprintf("[Worker] starting task %s", job.TaskID),
	})

	var lastOutput string
	for attempt := 0; attempt <= verifyMaxRetries; attempt++ {
		// Spawn a fresh Worker agent for each attempt.
		workerRA, workerAgent, err := spawnWorker(cfg, inferrer, mcpEng, job)
		if err != nil {
			return WorkerResult{TaskID: job.TaskID, IsError: true}, err
		}

		// Worker executes.
		output, err := workerRA.Turn(ctx, job.Instruction)
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
			ProjectID: job.ProjectID,
			Kind:      models.KindDraft,
			Tier:      models.TierRaw,
			AgentID:   workerRA.AgentID,
			Content:   output,
		})

		// Lead verifies (if in swap mode, Lead reloads now).
		verifyInstruction := fmt.Sprintf(
			"A Specialist has completed task %q. Review their output and respond with one of:\n"+
				"- APPROVED — if the output is correct and complete\n"+
				"- CORRECTION: <instruction> — if the output needs revision\n\n"+
				"Worker output:\n%s", job.Instruction, output)

		verdict, verifyErr := lead.Turn(ctx, verifyInstruction)
		workerAgent.Teardown(agent.JournalEntry{
			Task:    job.Instruction,
			Outcome: "success",
			Summary: fmt.Sprintf("Completed on attempt %d. Output: %s", attempt+1, truncate(output, 200)),
		})

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
				ProjectID: job.ProjectID,
				Kind:      models.KindMilestone,
				Tier:      models.TierSummary,
				Content:   fmt.Sprintf("✓ Task completed: %s", job.TaskID),
			})
			return WorkerResult{TaskID: job.TaskID, Output: output}, nil
		}

		// Lead requested a correction.
		job.Instruction = extractCorrection(verdict, job.Instruction)
		lastOutput = output
		log.Info("lead requested correction", "task", job.TaskID, "attempt", attempt+1)
	}

	// All retries exhausted.
	if db != nil {
		_ = db.UpdateTaskStatus(ctx, job.TaskID, "failed")
	}
	return WorkerResult{TaskID: job.TaskID, Output: lastOutput, IsError: true},
		fmt.Errorf("task %s: all %d verification attempts failed", job.TaskID, verifyMaxRetries+1)
}

// isApproved checks whether the Lead's verification response signals approval.
func isApproved(verdict string) bool {
	upper := trimUpper(verdict)
	return upper == "APPROVED" || containsWord(upper, "APPROVED")
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
