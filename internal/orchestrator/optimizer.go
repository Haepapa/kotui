package orchestrator

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/haepapa/kotui/internal/ollama"
	"github.com/haepapa/kotui/internal/store"
)

// OptimizerThreshold is the number of completed specialist tasks between
// each Lead Optimizer journal-review cycle.
const OptimizerThreshold = 10

// LeadOptimizer runs a background journal-review cycle after every
// OptimizerThreshold completed specialist tasks. The Lead analyses recent
// journal entries and, when it identifies meaningful improvement opportunities,
// proposes a handbook amendment for the Boss to approve or reject.
type LeadOptimizer struct {
	cfg        OrchestratorConfig
	inferrer   Inferrer
	db         *store.DB
	log        *slog.Logger
	onProposal func(projectID, proposalText string)

	mu        sync.Mutex
	tasksDone int
	projectID string
	reviewing bool
}

// newLeadOptimizer creates a LeadOptimizer.
// onProposal is called (from a goroutine) when the Lead produces a handbook
// proposal; the caller is responsible for persisting it and notifying the Boss.
func newLeadOptimizer(
	cfg OrchestratorConfig,
	inferrer Inferrer,
	db *store.DB,
	log *slog.Logger,
	onProposal func(projectID, proposalText string),
) *LeadOptimizer {
	return &LeadOptimizer{
		cfg:        cfg,
		inferrer:   inferrer,
		db:         db,
		log:        log,
		onProposal: onProposal,
	}
}

// NotifyTaskDone increments the completed-task counter and, when the threshold
// is reached, starts a background review cycle (at most one at a time).
func (opt *LeadOptimizer) NotifyTaskDone(projectID string) {
	if projectID == "" {
		return
	}
	opt.mu.Lock()
	defer opt.mu.Unlock()
	opt.projectID = projectID
	opt.tasksDone++
	if opt.tasksDone >= OptimizerThreshold && !opt.reviewing {
		opt.tasksDone = 0
		opt.reviewing = true
		go opt.runReview(projectID)
	}
}

// runReview performs the optimizer review cycle in the background.
func (opt *LeadOptimizer) runReview(projectID string) {
	defer func() {
		opt.mu.Lock()
		opt.reviewing = false
		opt.mu.Unlock()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	opt.log.Info("optimizer: starting journal review", "project", projectID)

	// 1. Fetch recent journal entries across all agents for this project.
	entries, err := opt.db.ListRecentJournalEmbeddings(ctx, projectID, 10)
	if err != nil {
		opt.log.Warn("optimizer: failed to read journal entries", "err", err)
		return
	}
	if len(entries) == 0 {
		opt.log.Info("optimizer: no journal entries found, skipping review", "project", projectID)
		return
	}

	// 2. Build the optimizer prompt.
	prompt := buildOptimizerPrompt(entries)

	// 3. Call the Lead model directly — isolated, no conversation history.
	resp, err := opt.inferrer.Chat(ctx, ollama.ChatRequest{
		Model:     opt.cfg.LeadModel,
		KeepAlive: ollama.Forever(),
		Messages:  []ollama.ChatMessage{{Role: "user", Content: prompt}},
	})
	if err != nil {
		opt.log.Warn("optimizer: Lead review call failed", "err", err)
		return
	}

	// 4. Strip think blocks then parse the proposal signal.
	_, cleanedResp := extractThinkBlocks(resp.Content)
	proposal := parseHandbookProposal(cleanedResp)
	if proposal == nil {
		opt.log.Info("optimizer: no handbook proposal in Lead response")
		return
	}

	opt.log.Info("optimizer: handbook proposal received", "project", projectID)
	if opt.onProposal != nil {
		opt.onProposal(projectID, proposal.Diff)
	}
}

// buildOptimizerPrompt constructs the structured analysis prompt for the Lead.
func buildOptimizerPrompt(entries []store.JournalEmbedding) string {
	var sb strings.Builder
	sb.WriteString(`You are the Lead Agent performing a periodic journal review.

Below are the most recent journal entries from your team across all active specialist agents.

Analyse them to identify:
1. Recurring failures or misunderstandings
2. Gaps in the team handbook that caused confusion
3. Specific, actionable improvements to add to the handbook

If you identify at least one meaningful improvement, output a SINGLE JSON line in this exact format:
{"propose_handbook": true, "diff": "YOUR PROPOSED HANDBOOK ADDITION HERE"}

The "diff" value must be a complete, self-contained markdown section ready to append to the handbook.
It must be specific, actionable, and address a real pattern observed in the journals.

If no meaningful improvements are needed, output:
{"propose_handbook": false, "diff": ""}

Do not output anything else after the JSON line.

---

## Recent Journal Entries

`)

	for _, e := range entries {
		label := "[Journal]"
		if e.IsFeedback {
			label = "[Boss Feedback]"
		}
		sb.WriteString(fmt.Sprintf("**%s** (agent: %s)\n%s\n\n", label, e.AgentID, e.Content))
	}
	sb.WriteString("---\n\nNow provide your analysis and JSON signal:")
	return sb.String()
}
