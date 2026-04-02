package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/haepapa/kotui/pkg/models"
)

// CreateTask inserts a new task node into the task tree.
func (db *DB) CreateTask(ctx context.Context, t models.Task) error {
	if t.ID == "" {
		t.ID = NewID()
	}
	now := time.Now().UTC()
	if t.CreatedAt.IsZero() {
		t.CreatedAt = now
	}
	if t.UpdatedAt.IsZero() {
		t.UpdatedAt = now
	}
	if t.Status == "" {
		t.Status = "pending"
	}
	_, err := db.ExecContext(ctx,
		`INSERT INTO tasks (id, project_id, parent_id, assignee_id, title, description, status, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.ID, t.ProjectID, nullString(t.ParentID), t.AssigneeID,
		t.Title, t.Description, t.Status, t.CreatedAt, t.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("store: create task: %w", err)
	}
	return nil
}

// GetTask retrieves a single task by ID.
func (db *DB) GetTask(ctx context.Context, id string) (*models.Task, error) {
	row := db.QueryRowContext(ctx,
		`SELECT id, project_id, COALESCE(parent_id,''), assignee_id, title, description, status, created_at, updated_at
		 FROM tasks WHERE id = ?`, id)
	return scanTask(row)
}

// UpdateTaskStatus updates a task's status and bumps updated_at.
func (db *DB) UpdateTaskStatus(ctx context.Context, id, status string) error {
	res, err := db.ExecContext(ctx,
		`UPDATE tasks SET status = ?, updated_at = ? WHERE id = ?`,
		status, time.Now().UTC(), id,
	)
	if err != nil {
		return fmt.Errorf("store: update task status: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("store: task %q not found", id)
	}
	return nil
}

// ListSubTasks returns all direct children of the given parent task.
func (db *DB) ListSubTasks(ctx context.Context, parentID string) ([]models.Task, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT id, project_id, COALESCE(parent_id,''), assignee_id, title, description, status, created_at, updated_at
		 FROM tasks WHERE parent_id = ? ORDER BY created_at`, parentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTasks(rows)
}

// ListTasksByStatus returns tasks for a project filtered by status.
func (db *DB) ListTasksByStatus(ctx context.Context, projectID, status string) ([]models.Task, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT id, project_id, COALESCE(parent_id,''), assignee_id, title, description, status, created_at, updated_at
		 FROM tasks WHERE project_id = ? AND status = ? ORDER BY created_at`,
		projectID, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTasks(rows)
}

func scanTask(row *sql.Row) (*models.Task, error) {
	var t models.Task
	err := row.Scan(&t.ID, &t.ProjectID, &t.ParentID, &t.AssigneeID, &t.Title, &t.Description, &t.Status, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func scanTasks(rows *sql.Rows) ([]models.Task, error) {
	var out []models.Task
	for rows.Next() {
		var t models.Task
		if err := rows.Scan(&t.ID, &t.ProjectID, &t.ParentID, &t.AssigneeID, &t.Title, &t.Description, &t.Status, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func nullString(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}
