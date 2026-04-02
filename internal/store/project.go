package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/haepapa/kotui/pkg/models"
)

// NewID generates a random UUID-like identifier.
func NewID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return fmt.Sprintf("%s-%s-%s-%s-%s",
		hex.EncodeToString(b[0:4]),
		hex.EncodeToString(b[4:6]),
		hex.EncodeToString(b[6:8]),
		hex.EncodeToString(b[8:10]),
		hex.EncodeToString(b[10:16]),
	)
}

// CreateProject inserts a new project. If Active is true, all other projects
// are set to inactive first.
func (db *DB) CreateProject(ctx context.Context, p models.Project) error {
	if p.ID == "" {
		p.ID = NewID()
	}
	if p.CreatedAt.IsZero() {
		p.CreatedAt = time.Now()
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if p.Active {
		if _, err := tx.ExecContext(ctx, `UPDATE projects SET active = 0`); err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx,
		`INSERT INTO projects (id, name, description, data_path, active, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		p.ID, p.Name, p.Description, p.DataPath, boolInt(p.Active), p.CreatedAt.UTC(),
	)
	if err != nil {
		return fmt.Errorf("store: create project: %w", err)
	}
	return tx.Commit()
}

// GetProject retrieves a project by ID.
func (db *DB) GetProject(ctx context.Context, id string) (*models.Project, error) {
	row := db.QueryRowContext(ctx,
		`SELECT id, name, description, data_path, active, created_at FROM projects WHERE id = ?`, id)
	return scanProject(row)
}

// ListProjects returns all non-archived projects ordered by creation time.
func (db *DB) ListProjects(ctx context.Context) ([]models.Project, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT id, name, description, data_path, active, created_at FROM projects WHERE archived = 0 ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanProjects(rows)
}

// RenameProject updates the name and description of a project.
func (db *DB) RenameProject(ctx context.Context, id, name, description string) error {
	if name == "" {
		return fmt.Errorf("store: project name must not be empty")
	}
	res, err := db.ExecContext(ctx,
		`UPDATE projects SET name = ?, description = ? WHERE id = ? AND archived = 0`,
		name, description, id)
	if err != nil {
		return fmt.Errorf("store: rename project: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("store: project %q not found", id)
	}
	return nil
}

// ArchiveProject marks a project as archived so it no longer appears in the sidebar.
// If the project is currently active, active is cleared first.
func (db *DB) ArchiveProject(ctx context.Context, id string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Clear active flag for this project.
	if _, err := tx.ExecContext(ctx, `UPDATE projects SET active = 0, archived = 1 WHERE id = ?`, id); err != nil {
		return fmt.Errorf("store: archive project: %w", err)
	}
	return tx.Commit()
}

// SetActiveProject marks the given project as active and all others as inactive.
func (db *DB) SetActiveProject(ctx context.Context, id string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE projects SET active = 0`); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `UPDATE projects SET active = 1 WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("store: project %q not found", id)
	}
	return tx.Commit()
}

// GetActiveProject returns the currently active project, or nil if none.
func (db *DB) GetActiveProject(ctx context.Context) (*models.Project, error) {
	row := db.QueryRowContext(ctx,
		`SELECT id, name, description, data_path, active, created_at FROM projects WHERE active = 1 LIMIT 1`)
	p, err := scanProject(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return p, err
}

func scanProject(row *sql.Row) (*models.Project, error) {
	var p models.Project
	var active int
	err := row.Scan(&p.ID, &p.Name, &p.Description, &p.DataPath, &active, &p.CreatedAt)
	if err != nil {
		return nil, err
	}
	p.Active = active != 0
	return &p, nil
}

func scanProjects(rows *sql.Rows) ([]models.Project, error) {
	var out []models.Project
	for rows.Next() {
		var p models.Project
		var active int
		if err := rows.Scan(&p.ID, &p.Name, &p.Description, &p.DataPath, &active, &p.CreatedAt); err != nil {
			return nil, err
		}
		p.Active = active != 0
		out = append(out, p)
	}
	return out, rows.Err()
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
