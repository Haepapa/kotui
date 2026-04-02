package store

import (
	"context"
	"fmt"
	"time"

	"github.com/haepapa/kotui/pkg/models"
)

// CreateAgent inserts a new agent record.
func (db *DB) CreateAgent(ctx context.Context, a models.Agent) error {
	if a.ID == "" {
		a.ID = NewID()
	}
	if a.CreatedAt.IsZero() {
		a.CreatedAt = time.Now()
	}
	if a.Status == "" {
		a.Status = models.StatusIdle
	}
	_, err := db.ExecContext(ctx,
		`INSERT INTO agents (id, project_id, name, role, status, model, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		a.ID, a.ProjectID, a.Name, string(a.Role), string(a.Status), a.Model, a.CreatedAt.UTC(),
	)
	if err != nil {
		return fmt.Errorf("store: create agent: %w", err)
	}
	return nil
}

// GetAgent retrieves an agent by ID.
func (db *DB) GetAgent(ctx context.Context, id string) (*models.Agent, error) {
	var a models.Agent
	var role, status string
	err := db.QueryRowContext(ctx,
		`SELECT id, project_id, name, role, status, model, created_at FROM agents WHERE id = ?`, id,
	).Scan(&a.ID, &a.ProjectID, &a.Name, &role, &status, &a.Model, &a.CreatedAt)
	if err != nil {
		return nil, err
	}
	a.Role = models.AgentRole(role)
	a.Status = models.AgentStatus(status)
	return &a, nil
}

// UpdateAgentStatus updates an agent's status field.
func (db *DB) UpdateAgentStatus(ctx context.Context, id string, status models.AgentStatus) error {
	res, err := db.ExecContext(ctx,
		`UPDATE agents SET status = ? WHERE id = ?`, string(status), id)
	if err != nil {
		return fmt.Errorf("store: update agent status: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("store: agent %q not found", id)
	}
	return nil
}

// ListAgents returns all agents for a project.
func (db *DB) ListAgents(ctx context.Context, projectID string) ([]models.Agent, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT id, project_id, name, role, status, model, created_at
		 FROM agents WHERE project_id = ? ORDER BY created_at`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []models.Agent
	for rows.Next() {
		var a models.Agent
		var role, status string
		if err := rows.Scan(&a.ID, &a.ProjectID, &a.Name, &role, &status, &a.Model, &a.CreatedAt); err != nil {
			return nil, err
		}
		a.Role = models.AgentRole(role)
		a.Status = models.AgentStatus(status)
		out = append(out, a)
	}
	return out, rows.Err()
}
