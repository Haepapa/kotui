// Package relay — inbound command parser and handler for remote messaging relays.
//
// All relay adapters (Telegram, Slack, WhatsApp) call ParseCommand on inbound
// messages and dispatch the result to a CommandFunc.
package relay

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/haepapa/kotui/internal/orchestrator"
	"github.com/haepapa/kotui/internal/store"
)

// CommandFunc is the callback that relay adapters call when they receive a
// command from an external channel.
// It returns a plain-text response to be sent back to the caller.
type CommandFunc func(ctx context.Context, text string) string

// ParseCommand extracts a slash command and optional arguments from a message.
// Returns (cmd, args, true) for recognised commands, or ("", "", false) for
// plain messages that are not commands.
//
// Recognised patterns: /status, /summary, /approve <id>
func ParseCommand(text string) (cmd, args string, ok bool) {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "/") {
		return "", "", false
	}
	parts := strings.SplitN(text, " ", 2)
	cmd = strings.ToLower(parts[0])
	if len(parts) > 1 {
		args = strings.TrimSpace(parts[1])
	}
	switch cmd {
	case "/status", "/summary", "/approve":
		return cmd, args, true
	default:
		return cmd, args, false
	}
}

// NewCommandHandler wires a CommandFunc against the live DB and orchestrator.
// If orch is nil the handler degrades gracefully (reports "no AI backend").
func NewCommandHandler(db *store.DB, orch *orchestrator.Orchestrator, log *slog.Logger) CommandFunc {
	return func(ctx context.Context, text string) string {
		cmd, args, ok := ParseCommand(text)
		if !ok {
			return fmt.Sprintf("Unknown command %q. Available: /status, /summary, /approve <id>", strings.TrimSpace(text))
		}

		switch cmd {
		case "/status":
			return handleStatus(ctx, db, orch)
		case "/summary":
			return handleSummary(ctx, db)
		case "/approve":
			return handleApprove(ctx, db, args, log)
		default:
			return "Unrecognised command."
		}
	}
}

func handleStatus(ctx context.Context, db *store.DB, orch *orchestrator.Orchestrator) string {
	var sb strings.Builder
	sb.WriteString("*Kōtui Status*\n")
	sb.WriteString(fmt.Sprintf("Time: %s\n", time.Now().UTC().Format(time.RFC3339)))

	if orch == nil {
		sb.WriteString("AI backend: not available\n")
	} else {
		sb.WriteString("AI backend: running\n")
	}

	proj, err := db.GetActiveProject(ctx)
	if err != nil || proj == nil {
		sb.WriteString("Active project: none\n")
	} else {
		sb.WriteString(fmt.Sprintf("Active project: %s (%s)\n", proj.Name, proj.ID))
	}

	pending, err := db.ListPendingApprovals(ctx, "")
	if err == nil {
		sb.WriteString(fmt.Sprintf("Pending approvals: %d\n", len(pending)))
	}

	return sb.String()
}

func handleSummary(ctx context.Context, db *store.DB) string {
	since := time.Now().Add(-24 * time.Hour)
	msgs, err := db.ListSummaryMessages(ctx, "", since)
	if err != nil {
		return fmt.Sprintf("Error fetching summary: %v", err)
	}
	if len(msgs) == 0 {
		return "No summary messages in the last 24 hours."
	}

	limit := 10
	if len(msgs) < limit {
		limit = len(msgs)
	}
	// Most recent last
	recent := msgs[len(msgs)-limit:]

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("*Last %d summary events:*\n", len(recent)))
	for _, m := range recent {
		ts := m.CreatedAt.UTC().Format("15:04")
		content := truncate(m.Content, 120)
		sb.WriteString(fmt.Sprintf("[%s] %s\n", ts, content))
	}
	return sb.String()
}

func handleApprove(ctx context.Context, db *store.DB, id string, log *slog.Logger) string {
	if id == "" {
		// List pending approvals
		pending, err := db.ListPendingApprovals(ctx, "")
		if err != nil {
			return fmt.Sprintf("Error listing approvals: %v", err)
		}
		if len(pending) == 0 {
			return "No pending approvals."
		}
		var sb strings.Builder
		sb.WriteString("*Pending approvals:*\n")
		for _, a := range pending {
			sb.WriteString(fmt.Sprintf("ID: %s | %s | %s\n", a.ID[:8], a.Kind, truncate(a.Description, 60)))
		}
		sb.WriteString("\nUse /approve <id> to approve.")
		return sb.String()
	}

	// Find the approval — support short (8-char) ID prefix
	pending, err := db.ListPendingApprovals(ctx, "")
	if err != nil {
		return fmt.Sprintf("Error fetching approvals: %v", err)
	}

	var matchID string
	for _, a := range pending {
		if a.ID == id || strings.HasPrefix(a.ID, id) {
			matchID = a.ID
			break
		}
	}
	if matchID == "" {
		return fmt.Sprintf("No pending approval found matching %q.", id)
	}

	if err := db.DecideApproval(ctx, matchID, "approved"); err != nil {
		log.Error("relay approve failed", "id", matchID, "err", err)
		return fmt.Sprintf("Error approving %s: %v", matchID[:8], err)
	}
	return fmt.Sprintf("✓ Approval %s approved.", matchID[:8])
}
