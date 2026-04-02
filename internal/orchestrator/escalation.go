package orchestrator

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"time"

	"github.com/haepapa/kotui/internal/config"
	"github.com/haepapa/kotui/internal/dispatcher"
	"github.com/haepapa/kotui/internal/ollama"
	"github.com/haepapa/kotui/internal/store"
	"github.com/haepapa/kotui/pkg/models"
)

// EscalationRecord is persisted to SQLite when a capability escalation occurs.
type EscalationRecord struct {
	AgentID            string
	TaskID             string
	Reason             string
	CapabilityRequired string
	ResolvedBy         string // "senior_consultant" | "boss_notification"
	At                 time.Time
}

// EscalationRouter decides what to do when an agent emits escalation_needed.
type EscalationRouter struct {
	cfg      config.Config
	disp     *dispatcher.Dispatcher
	db       *store.DB
	log      *slog.Logger
}

func newEscalationRouter(cfg config.Config, disp *dispatcher.Dispatcher, db *store.DB, log *slog.Logger) *EscalationRouter {
	return &EscalationRouter{cfg: cfg, disp: disp, db: db, log: log}
}

// Route handles an EscalationNeededError from an agent's Turn():
//  1. If a Senior Consultant is configured → SSH wake (if needed) → route task → return result
//  2. If no Senior Consultant → notify Boss, pause the task
//
// Returns the Senior Consultant's response (case 1) or "" with a BossNotifiedError (case 2).
func (r *EscalationRouter) Route(
	ctx context.Context,
	err *EscalationNeededError,
	task string,
	projectID string,
) (string, error) {
	rec := EscalationRecord{
		AgentID:            err.AgentID,
		Reason:             err.Reason,
		CapabilityRequired: err.CapabilityRequired,
		At:                 time.Now(),
	}

	sc := r.cfg.SeniorConsultant
	if sc.Model == "" {
		// No Senior Consultant configured.
		rec.ResolvedBy = "boss_notification"
		r.persistRecord(ctx, rec)
		r.notifyBoss(projectID, err)
		return "", &BossNotifiedError{AgentName: err.AgentName, Reason: err.Reason, CapabilityRequired: err.CapabilityRequired}
	}

	// --- Senior Consultant is configured ---

	// SSH wake if configured.
	if sc.SSHHost != "" && sc.SSHStartCmd != "" {
		if wakeErr := sshWake(ctx, sc.SSHHost, sc.SSHStartCmd, r.log); wakeErr != nil {
			r.log.Warn("SSH wake failed; trying endpoint anyway", "host", sc.SSHHost, "err", wakeErr)
		}
	}

	// Build a temporary Ollama client for the Senior Consultant endpoint.
	endpoint := sc.Endpoint
	if endpoint == "" {
		endpoint = r.cfg.Ollama.Endpoint
	}
	scClient := ollama.New(endpoint).
		WithTimeout(r.cfg.Ollama.RequestTimeout).
		WithRetries(2)

	r.log.Info("routing escalation to senior consultant",
		"model", sc.Model, "endpoint", endpoint,
		"reason", err.Reason)

	result, chatErr := scClient.Chat(ctx, ollama.ChatRequest{
		Model: sc.Model,
		Messages: []ollama.ChatMessage{
			{Role: "system", Content: "You are a Senior Consultant. Provide expert guidance for the following task."},
			{Role: "user", Content: task},
		},
		KeepAlive: ollama.Release(), // unload immediately after
	})
	if chatErr != nil {
		rec.ResolvedBy = "senior_consultant_failed"
		r.persistRecord(ctx, rec)
		r.notifyBoss(projectID, err)
		return "", fmt.Errorf("senior consultant failed: %w; original escalation: %s", chatErr, err.Reason)
	}

	rec.ResolvedBy = "senior_consultant"
	r.persistRecord(ctx, rec)

	r.disp.DispatchSummary(models.Message{
		ProjectID: projectID,
		Kind:      models.KindMilestone,
		Tier:      models.TierSummary,
		Content:   fmt.Sprintf("🔀 Escalation resolved via Senior Consultant (%s)", sc.Model),
	})

	return result.Content, nil
}

// notifyBoss emits a summary-tier system event so the Boss sees the pause.
func (r *EscalationRouter) notifyBoss(projectID string, err *EscalationNeededError) {
	r.disp.DispatchSummary(models.Message{
		ProjectID: projectID,
		Kind:      models.KindSystemEvent,
		Tier:      models.TierSummary,
		Content: fmt.Sprintf(
			"⏸ Task paused — capability escalation\n\nAgent: %s\nReason: %s\nCapability required: %s\n\nNo Senior Consultant is configured. Please configure [senior_consultant] in config.toml or handle this task manually.",
			err.AgentName, err.Reason, err.CapabilityRequired),
	})
}

// persistRecord saves an escalation event to the approvals table for Boss review.
func (r *EscalationRouter) persistRecord(ctx context.Context, rec EscalationRecord) {
	if r.db == nil {
		return
	}
	_ = r.db.CreateApproval(ctx, models.Approval{
		ProjectID:   rec.AgentID,
		Kind:        "sudo",
		SubjectID:   rec.AgentID,
		Description: fmt.Sprintf("Capability escalation: %s (requires: %s)", rec.Reason, rec.CapabilityRequired),
	})
}

// sshWake connects to the remote host via SSH and runs the start command.
func sshWake(ctx context.Context, host, startCmd string, log *slog.Logger) error {
	wakeCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	log.Info("SSH waking remote host", "host", host, "cmd", startCmd)
	out, err := exec.CommandContext(wakeCtx, "ssh",
		"-o", "ConnectTimeout=15",
		"-o", "StrictHostKeyChecking=no",
		"-o", "BatchMode=yes",
		host, startCmd,
	).CombinedOutput()
	if err != nil {
		return fmt.Errorf("ssh wake %s: %w\n%s", host, err, string(out))
	}
	return nil
}

// BossNotifiedError is returned when no Senior Consultant is available and
// the Boss has been notified to intervene.
type BossNotifiedError struct {
	AgentName          string
	Reason             string
	CapabilityRequired string
}

func (e *BossNotifiedError) Error() string {
	return fmt.Sprintf("task paused: no senior consultant configured; boss notified (reason: %s)", e.Reason)
}
