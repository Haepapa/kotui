package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/haepapa/kotui/pkg/models"
)

// CreateApproval inserts a new pending approval request.
func (db *DB) CreateApproval(ctx context.Context, a models.Approval) error {
	if a.ID == "" {
		a.ID = NewID()
	}
	if a.CreatedAt.IsZero() {
		a.CreatedAt = time.Now()
	}
	a.Status = "pending"
	_, err := db.ExecContext(ctx,
		`INSERT INTO approvals (id, project_id, kind, subject_id, description, status, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		a.ID, a.ProjectID, a.Kind, a.SubjectID, a.Description, a.Status, a.CreatedAt.UTC(),
	)
	if err != nil {
		return fmt.Errorf("store: create approval: %w", err)
	}
	return nil
}

// DecideApproval marks an approval as approved or rejected.
func (db *DB) DecideApproval(ctx context.Context, id, decision string) error {
	if decision != "approved" && decision != "rejected" {
		return fmt.Errorf("store: invalid decision %q (must be approved or rejected)", decision)
	}
	now := time.Now().UTC()
	res, err := db.ExecContext(ctx,
		`UPDATE approvals SET status = ?, decided_at = ? WHERE id = ? AND status = 'pending'`,
		decision, now, id,
	)
	if err != nil {
		return fmt.Errorf("store: decide approval: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("store: approval %q not found or already decided", id)
	}
	return nil
}

// ListPendingApprovals returns all pending approvals for a project.
func (db *DB) ListPendingApprovals(ctx context.Context, projectID string) ([]models.Approval, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT id, project_id, kind, subject_id, description, status, created_at, decided_at
		 FROM approvals WHERE project_id = ? AND status = 'pending' ORDER BY created_at`,
		projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanApprovals(rows)
}

// GetApproval retrieves an approval by ID.
func (db *DB) GetApproval(ctx context.Context, id string) (*models.Approval, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT id, project_id, kind, subject_id, description, status, created_at, decided_at
		 FROM approvals WHERE id = ?`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items, err := scanApprovals(rows)
	if err != nil || len(items) == 0 {
		return nil, err
	}
	return &items[0], nil
}

func scanApprovals(rows *sql.Rows) ([]models.Approval, error) {
	var out []models.Approval
	for rows.Next() {
		var a models.Approval
		var decidedAt sql.NullTime
		if err := rows.Scan(
			&a.ID, &a.ProjectID, &a.Kind, &a.SubjectID,
			&a.Description, &a.Status, &a.CreatedAt, &decidedAt,
		); err != nil {
			return nil, err
		}
		if decidedAt.Valid {
			t := decidedAt.Time
			a.DecidedAt = &t
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
