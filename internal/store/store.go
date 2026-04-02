// Package store manages the SQLite state database — the "Company Ledger."
//
// It uses modernc.org/sqlite (pure Go, no cgo) and runs versioned migrations
// from the embedded migrations/ directory on every startup.
package store

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"log/slog"
	"path/filepath"
	"sort"
	"strings"

	_ "modernc.org/sqlite" // register the sqlite driver
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// DB wraps a *sql.DB with migration support.
type DB struct {
	*sql.DB
}

// Open opens (or creates) the SQLite database at the given file path and runs
// any pending migrations.
func Open(path string) (*DB, error) {
	dsn := fmt.Sprintf("file:%s?_foreign_keys=on&_journal_mode=WAL", path)
	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("store: open %s: %w", path, err)
	}
	if err := sqlDB.Ping(); err != nil {
		return nil, fmt.Errorf("store: ping %s: %w", path, err)
	}
	// Limit to a single writer connection to avoid WAL conflicts.
	sqlDB.SetMaxOpenConns(1)

	db := &DB{sqlDB}
	if err := db.migrate(); err != nil {
		return nil, fmt.Errorf("store: migrate: %w", err)
	}
	return db, nil
}

// migrate applies all pending versioned migrations in order.
func (db *DB) migrate() error {
	// Ensure the migrations tracking table exists (bootstraps itself).
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version    INTEGER PRIMARY KEY,
		applied_at DATETIME NOT NULL DEFAULT (datetime('now'))
	)`)
	if err != nil {
		return err
	}

	applied, err := db.appliedVersions()
	if err != nil {
		return err
	}

	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		return err
	}

	// Sort deterministically.
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		version, err := versionFromFilename(entry.Name())
		if err != nil {
			return err
		}
		if applied[version] {
			continue
		}

		data, err := migrationsFS.ReadFile(filepath.Join("migrations", entry.Name()))
		if err != nil {
			return err
		}

		slog.Info("store: applying migration", "version", version, "file", entry.Name())
		if _, err := db.Exec(string(data)); err != nil {
			return fmt.Errorf("store: migration %d (%s): %w", version, entry.Name(), err)
		}
		if _, err := db.Exec(`INSERT INTO schema_migrations (version) VALUES (?)`, version); err != nil {
			return err
		}
	}
	return nil
}

func (db *DB) appliedVersions() (map[int]bool, error) {
	rows, err := db.QueryContext(context.Background(), `SELECT version FROM schema_migrations`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := make(map[int]bool)
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		m[v] = true
	}
	return m, rows.Err()
}

// versionFromFilename parses the leading integer from filenames like "001_initial.sql".
func versionFromFilename(name string) (int, error) {
	var v int
	_, err := fmt.Sscanf(name, "%d", &v)
	if err != nil {
		return 0, fmt.Errorf("store: cannot parse version from filename %q", name)
	}
	return v, nil
}

// Close closes the underlying database connection.
func (db *DB) Close() error {
	return db.DB.Close()
}
