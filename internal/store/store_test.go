package store_test

import (
	"path/filepath"
	"testing"

	"github.com/haepapa/kotui/internal/store"
)

func TestOpenAndMigrate(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("Open() error: %v", err)
	}
	defer db.Close()

	// Verify core tables were created.
	tables := []string{"projects", "agents", "conversations", "messages", "tasks", "approvals"}
	for _, table := range tables {
		var name string
		err := db.QueryRow(
			`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table,
		).Scan(&name)
		if err != nil {
			t.Errorf("table %q not found: %v", table, err)
		}
	}
}

func TestMigrationsIdempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")

	// Open twice — second open should not re-apply migrations.
	db1, err := store.Open(path)
	if err != nil {
		t.Fatalf("first Open() error: %v", err)
	}
	db1.Close()

	db2, err := store.Open(path)
	if err != nil {
		t.Fatalf("second Open() error: %v", err)
	}
	defer db2.Close()

	var count int
	db2.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&count)
	if count != 1 {
		t.Errorf("expected 1 migration record, got %d", count)
	}
}
