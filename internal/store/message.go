package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/haepapa/kotui/pkg/models"
)

// --- Conversations --------------------------------------------------------

// CreateConversation inserts a new conversation record.
func (db *DB) CreateConversation(ctx context.Context, projectID, title string) (string, error) {
	id := NewID()
	_, err := db.ExecContext(ctx,
		`INSERT INTO conversations (id, project_id, title) VALUES (?, ?, ?)`,
		id, projectID, title,
	)
	if err != nil {
		return "", fmt.Errorf("store: create conversation: %w", err)
	}
	return id, nil
}

// --- Messages -------------------------------------------------------------

// SaveMessage persists a message to the ledger.
// If msg.ID is empty a new ID is generated. If msg.CreatedAt is zero, now() is used.
func (db *DB) SaveMessage(ctx context.Context, msg models.Message) error {
	if msg.ID == "" {
		msg.ID = NewID()
	}
	if msg.CreatedAt.IsZero() {
		msg.CreatedAt = time.Now()
	}
	if msg.Metadata == "" {
		msg.Metadata = "{}"
	}
	_, err := db.ExecContext(ctx,
		`INSERT INTO messages
		 (id, project_id, conversation_id, agent_id, kind, tier, content, metadata, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		msg.ID, msg.ProjectID, msg.ConversationID, msg.AgentID,
		string(msg.Kind), string(msg.Tier),
		msg.Content, msg.Metadata, msg.CreatedAt.UTC(),
	)
	if err != nil {
		return fmt.Errorf("store: save message: %w", err)
	}
	return nil
}

// ListMessagesByConversation returns messages for a conversation ordered by time.
// limit <= 0 returns all messages.
func (db *DB) ListMessagesByConversation(ctx context.Context, conversationID string, limit int) ([]models.Message, error) {
	q := `SELECT id, project_id, conversation_id, agent_id, kind, tier, content, metadata, created_at
		  FROM messages WHERE conversation_id = ? ORDER BY created_at`
	var rows *sql.Rows
	var err error
	if limit > 0 {
		q += " LIMIT ?"
		rows, err = db.QueryContext(ctx, q, conversationID, limit)
	} else {
		rows, err = db.QueryContext(ctx, q, conversationID)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMessages(rows)
}

// ListSummaryMessages returns summary-tier messages for a project since the given time.
func (db *DB) ListSummaryMessages(ctx context.Context, projectID string, since time.Time) ([]models.Message, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT id, project_id, conversation_id, agent_id, kind, tier, content, metadata, created_at
		 FROM messages
		 WHERE project_id = ? AND tier = ? AND created_at > ?
		 ORDER BY created_at`,
		projectID, string(models.TierSummary), since.UTC(),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMessages(rows)
}

// CountMessages returns the number of messages in a conversation.
func (db *DB) CountMessages(ctx context.Context, conversationID string) (int, error) {
	var n int
	err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM messages WHERE conversation_id = ?`, conversationID,
	).Scan(&n)
	return n, err
}

func scanMessages(rows *sql.Rows) ([]models.Message, error) {
	var out []models.Message
	for rows.Next() {
		var m models.Message
		var kind, tier string
		if err := rows.Scan(
			&m.ID, &m.ProjectID, &m.ConversationID, &m.AgentID,
			&kind, &tier, &m.Content, &m.Metadata, &m.CreatedAt,
		); err != nil {
			return nil, err
		}
		m.Kind = models.MessageKind(kind)
		m.Tier = models.LogTier(tier)
		out = append(out, m)
	}
	return out, rows.Err()
}

// GetConversationByTitle returns the ID of the most recent conversation with the given title, or "" if none.
func (db *DB) GetConversationByTitle(ctx context.Context, projectID, title string) (string, error) {
	var id string
	err := db.QueryRowContext(ctx,
		`SELECT id FROM conversations WHERE project_id = ? AND title = ? ORDER BY created_at DESC LIMIT 1`,
		projectID, title,
	).Scan(&id)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("store: get conversation by title: %w", err)
	}
	return id, nil
}

// GetLatestConversation returns the ID of the most recent conversation for a
// project. Returns ("", nil) if none exists.
func (db *DB) GetLatestConversation(ctx context.Context, projectID string) (string, error) {
	var id string
	err := db.QueryRowContext(ctx,
		`SELECT id FROM conversations WHERE project_id = ? ORDER BY created_at DESC LIMIT 1`,
		projectID,
	).Scan(&id)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("store: get latest conversation: %w", err)
	}
	return id, nil
}
