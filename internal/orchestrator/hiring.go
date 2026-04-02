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

// HiringState tracks a Trial agent through the interview lifecycle.
type HiringState int

const (
	HiringProposed  HiringState = iota // Lead has proposed hiring
	HiringInterview                    // Trial agent is active, Boss is interviewing
	HiringApproved                     // Boss approved → promote to Specialist
	HiringRejected                     // Boss rejected → teardown
)

// HiringSession manages one candidate agent through the hiring workflow.
type HiringSession struct {
	ApprovalID  string
	CandidateID string
	State       HiringState
	ra          *RunningAgent
	a           *agent.Agent
	StartedAt   time.Time
}

// HiringManager orchestrates the Trial agent hiring workflow.
type HiringManager struct {
	cfg      OrchestratorConfig
	inferrer Inferrer
	mcpEng   *mcp.Engine
	disp     *dispatcher.Dispatcher
	db       *store.DB
	log      *slog.Logger
	sessions map[string]*HiringSession // keyed by approval ID
}

func newHiringManager(
	cfg OrchestratorConfig,
	inferrer Inferrer,
	mcpEng *mcp.Engine,
	disp *dispatcher.Dispatcher,
	db *store.DB,
	log *slog.Logger,
) *HiringManager {
	return &HiringManager{
		cfg:      cfg,
		inferrer: inferrer,
		mcpEng:   mcpEng,
		disp:     disp,
		db:       db,
		log:      log,
		sessions: make(map[string]*HiringSession),
	}
}

// Propose initiates the hiring workflow for a new candidate.
// The Lead calls this when it wants to hire a Specialist.
// Returns the approval ID the Boss must act on.
func (h *HiringManager) Propose(ctx context.Context, projectID, reason string) (string, error) {
	approval := models.Approval{
		ProjectID:   projectID,
		Kind:        "hiring",
		SubjectID:   fmt.Sprintf("candidate-%d", time.Now().UnixNano()),
		Description: reason,
	}
	if h.db != nil {
		if err := h.db.CreateApproval(ctx, approval); err != nil {
			return "", fmt.Errorf("hiring: create approval: %w", err)
		}
	}

	h.disp.DispatchSummary(models.Message{
		ProjectID: projectID,
		Kind:      models.KindSystemEvent,
		Tier:      models.TierSummary,
		Content:   fmt.Sprintf("👤 Hiring proposal: %s\nApproval ID: %s", reason, approval.SubjectID),
	})
	return approval.SubjectID, nil
}

// StartInterview spawns a Trial agent for the Boss to interview.
// The agent receives only Trial-clearance tools (read-only).
func (h *HiringManager) StartInterview(ctx context.Context, approvalID, projectID string) (*HiringSession, error) {
	candidateID := fmt.Sprintf("trial-%d", time.Now().UnixNano())

	// Build MCP fragment with Trial clearance only.
	trialFragment := h.mcpEng.SystemPromptFragment(models.ClearanceTrial)

	a, err := agent.Spawn(agent.SpawnConfig{
		ID:                  candidateID,
		Name:                "Candidate",
		Role:                models.RoleTrial,
		Model:               h.cfg.WorkerModel, // trial uses worker model
		ProjectID:           projectID,
		DataDir:             h.cfg.DataDir,
		CompanyIdentityPath: h.cfg.CompanyIdentityPath,
		MCPFragment:         trialFragment,
	})
	if err != nil {
		return nil, fmt.Errorf("hiring: spawn trial agent: %w", err)
	}

	ra := newRunningAgent(
		candidateID, "Candidate", h.cfg.WorkerModel,
		models.ClearanceTrial,
		ollama.Release(),
		a.SystemPrompt(),
		h.inferrer, h.mcpEng,
	)

	session := &HiringSession{
		ApprovalID:  approvalID,
		CandidateID: candidateID,
		State:       HiringInterview,
		ra:          ra,
		a:           a,
		StartedAt:   time.Now(),
	}
	h.sessions[approvalID] = session

	h.disp.DispatchSummary(models.Message{
		ProjectID: projectID,
		Kind:      models.KindSystemEvent,
		Tier:      models.TierSummary,
		Content:   fmt.Sprintf("🎤 Interview started for candidate %s", candidateID),
	})
	return session, nil
}

// CandidateTurn sends a Boss message to the Trial agent during the interview.
func (h *HiringManager) CandidateTurn(ctx context.Context, approvalID, message string) (string, error) {
	session, ok := h.sessions[approvalID]
	if !ok {
		return "", fmt.Errorf("hiring: no active session for approval %s", approvalID)
	}
	if session.State != HiringInterview {
		return "", fmt.Errorf("hiring: session %s is not in interview state", approvalID)
	}
	return session.ra.Turn(ctx, message)
}

// Decide finalises the hiring decision.
// decision must be "approved" or "rejected".
// On approval, the agent's role is promoted to Specialist in the store.
func (h *HiringManager) Decide(ctx context.Context, projectID, approvalID, decision string) error {
	session, ok := h.sessions[approvalID]
	if !ok {
		return fmt.Errorf("hiring: no session for approval %s", approvalID)
	}

	if h.db != nil {
		_ = h.db.DecideApproval(ctx, approvalID, decision)
	}

	if decision == "approved" {
		session.State = HiringApproved

		// Promote agent record to Specialist in the store.
		if h.db != nil {
			_ = h.db.UpdateAgentStatus(ctx, session.CandidateID, models.StatusOnboarded)
		}

		h.disp.DispatchSummary(models.Message{
			ProjectID: projectID,
			Kind:      models.KindMilestone,
			Tier:      models.TierSummary,
			Content:   fmt.Sprintf("✅ Candidate %s approved and onboarded as Specialist", session.CandidateID),
		})

		session.a.Teardown(agent.JournalEntry{
			Task:    "Hiring interview",
			Outcome: "success",
			Summary: "Successfully passed hiring interview and promoted to Specialist.",
		})
	} else {
		session.State = HiringRejected
		h.disp.DispatchSummary(models.Message{
			ProjectID: projectID,
			Kind:      models.KindSystemEvent,
			Tier:      models.TierSummary,
			Content:   fmt.Sprintf("❌ Candidate %s rejected", session.CandidateID),
		})
		session.a.Teardown(agent.JournalEntry{
			Task:    "Hiring interview",
			Outcome: "failure",
			Summary: "Did not pass hiring interview.",
		})
	}

	delete(h.sessions, approvalID)
	return nil
}

// ActiveSessions returns the current in-progress hiring sessions.
func (h *HiringManager) ActiveSessions() []*HiringSession {
	var out []*HiringSession
	for _, s := range h.sessions {
		out = append(out, s)
	}
	return out
}
