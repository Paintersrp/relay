package db

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"
)

func openSQLite(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", path+"?_journal_mode=WAL&_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestAutoMigrateCreatesAllTables(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	db := openSQLite(t, dbPath)

	if err := AutoMigrate(db); err != nil {
		t.Fatalf("auto-migrate: %v", err)
	}

	expectedTables := []string{
		"repos",
		"runs",
		"artifacts",
		"events",
		"checks",
		"repo_roots",
		"agent_executions",
		"projects",
		"project_repositories",
		"source_snapshots",
		"source_snapshot_repositories",
		"source_snapshot_files",
	}
	for _, table := range expectedTables {
		var name string
		err := db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&name)
		if err != nil {
			t.Errorf("expected table %q to exist after auto-migrate, got error: %v", table, err)
		}
	}
}

func TestAutoMigrateIdempotent(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	db := openSQLite(t, dbPath)

	if err := AutoMigrate(db); err != nil {
		t.Fatalf("first auto-migrate: %v", err)
	}

	if err := AutoMigrate(db); err != nil {
		t.Fatalf("second auto-migrate (idempotent): %v", err)
	}

	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='agent_executions'").Scan(&count)
	if err != nil {
		t.Fatalf("query agent_executions: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected agent_executions table to exist after second migrate, got count %d", count)
	}
}

func TestAutoMigrateOnFreshDB(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "fresh.db")
	db := openSQLite(t, dbPath)

	if err := AutoMigrate(db); err != nil {
		t.Fatalf("auto-migrate on fresh db: %v", err)
	}

	var tableCount int
	err := db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table'").Scan(&tableCount)
	if err != nil {
		t.Fatalf("count tables: %v", err)
	}
	if tableCount < 5 {
		t.Fatalf("expected at least 5 tables after auto-migrate, got %d", tableCount)
	}
}

func TestMissingAgentExecutionsTable(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "partial.db")
	db := openSQLite(t, dbPath)

	// Apply only first two migrations via goose, then verify auto-migrate adds the third
	goose.SetBaseFS(MigrationsFS)
	if err := goose.SetDialect("sqlite3"); err != nil {
		t.Fatalf("goose set dialect: %v", err)
	}
	if err := goose.UpTo(db, "migrations", 2); err != nil {
		t.Fatalf("goose up to 2: %v", err)
	}

	// Confirm agent_executions does NOT exist yet
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='agent_executions'").Scan(&count)
	if err != nil {
		t.Fatalf("query agent_executions: %v", err)
	}
	if count != 0 {
		t.Fatal("expected agent_executions to not exist before auto-migrate")
	}

	// Now run full auto-migrate (should apply migration 3 only)
	if err := AutoMigrate(db); err != nil {
		t.Fatalf("auto-migrate should add missing agent_executions: %v", err)
	}

	var name string
	err = db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='agent_executions'").Scan(&name)
	if err != nil {
		t.Fatalf("expected agent_executions table to exist after auto-migrate: %v", err)
	}
}
