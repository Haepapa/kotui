package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// JournalEmbedding is a stored embedding record.
type JournalEmbedding struct {
	ID         string
	AgentID    string
	ProjectID  string
	Content    string
	Embedding  []float32
	IsFeedback bool
	CreatedAt  time.Time
}

// SaveJournalEmbedding inserts an embedding record.
func (db *DB) SaveJournalEmbedding(ctx context.Context, e JournalEmbedding) error {
	embJSON, err := json.Marshal(e.Embedding)
	if err != nil {
		return fmt.Errorf("store: marshal embedding: %w", err)
	}
	isFeedback := 0
	if e.IsFeedback {
		isFeedback = 1
	}
	_, err = db.ExecContext(ctx,
		`INSERT INTO journal_embeddings (id, agent_id, project_id, content, embedding, is_feedback)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		e.ID, e.AgentID, e.ProjectID, e.Content, string(embJSON), isFeedback,
	)
	if err != nil {
		return fmt.Errorf("store: save journal embedding: %w", err)
	}
	return nil
}

// ListJournalEmbeddings returns all embeddings for an agent in a project.
func (db *DB) ListJournalEmbeddings(ctx context.Context, agentID, projectID string) ([]JournalEmbedding, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT id, agent_id, project_id, content, embedding, is_feedback, created_at
		 FROM journal_embeddings
		 WHERE agent_id = ? AND project_id = ?
		 ORDER BY created_at DESC`,
		agentID, projectID,
	)
	if err != nil {
		return nil, fmt.Errorf("store: list journal embeddings: %w", err)
	}
	defer rows.Close()

	var results []JournalEmbedding
	for rows.Next() {
		var e JournalEmbedding
		var embJSON string
		var isFeedback int
		if err := rows.Scan(&e.ID, &e.AgentID, &e.ProjectID, &e.Content, &embJSON, &isFeedback, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("store: scan journal embedding: %w", err)
		}
		if err := json.Unmarshal([]byte(embJSON), &e.Embedding); err != nil {
			return nil, fmt.Errorf("store: unmarshal embedding: %w", err)
		}
		e.IsFeedback = isFeedback != 0
		results = append(results, e)
	}
	return results, rows.Err()
}

// ListRecentJournalEmbeddings returns the most recent journal entries across
// all agents for a project, ordered newest-first. Used by the LeadOptimizer
// to gather context for handbook proposal reviews.
func (db *DB) ListRecentJournalEmbeddings(ctx context.Context, projectID string, limit int) ([]JournalEmbedding, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT id, agent_id, project_id, content, embedding, is_feedback, created_at
		 FROM journal_embeddings
		 WHERE project_id = ?
		 ORDER BY created_at DESC
		 LIMIT ?`,
		projectID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("store: list recent journal embeddings: %w", err)
	}
	defer rows.Close()

	var results []JournalEmbedding
	for rows.Next() {
		var e JournalEmbedding
		var embJSON string
		var isFeedback int
		if err := rows.Scan(&e.ID, &e.AgentID, &e.ProjectID, &e.Content, &embJSON, &isFeedback, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("store: scan journal embedding: %w", err)
		}
		if err := json.Unmarshal([]byte(embJSON), &e.Embedding); err != nil {
			return nil, fmt.Errorf("store: unmarshal embedding: %w", err)
		}
		e.IsFeedback = isFeedback != 0
		results = append(results, e)
	}
	return results, rows.Err()
}
