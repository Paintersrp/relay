package handlers

import (
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"relay/internal/artifacts"
	"relay/internal/store"
)

func setupTestStore(t *testing.T) *store.Store {
	t.Helper()
	dir := t.TempDir()
	// Isolate artifacts in the test's temp directory
	artifacts.SetBaseDir(filepath.Join(dir, "data", "artifacts"))
	dbPath := filepath.Join(dir, "test.db")
	s, err := store.Open(dbPath, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	// Run migrations inline since the store doesn't auto-migrate
	_, err = s.DB().Exec(`
		CREATE TABLE IF NOT EXISTS repos (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			path TEXT NOT NULL DEFAULT '',
			source TEXT NOT NULL DEFAULT 'manual',
			discovered_at TEXT NOT NULL DEFAULT '',
			last_seen_at TEXT NOT NULL DEFAULT '',
			default_validation_commands TEXT NOT NULL DEFAULT '[]',
			created_at TEXT NOT NULL DEFAULT (datetime('now')),
			updated_at TEXT NOT NULL DEFAULT (datetime('now'))
		);
		CREATE UNIQUE INDEX IF NOT EXISTS idx_repos_path_unique_nonempty ON repos(path) WHERE path <> '';
		CREATE TABLE IF NOT EXISTS repo_roots (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			path TEXT NOT NULL,
			enabled INTEGER NOT NULL DEFAULT 1,
			last_scanned_at TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL DEFAULT (datetime('now')),
			updated_at TEXT NOT NULL DEFAULT (datetime('now'))
		);
		CREATE TABLE IF NOT EXISTS runs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			repo_id INTEGER NOT NULL REFERENCES repos(id),
			title TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'draft',
			recommended_model TEXT NOT NULL DEFAULT '',
			selected_model TEXT NOT NULL DEFAULT '',
			executor_adapter TEXT NOT NULL DEFAULT 'opencode_go',
			branch_name TEXT NOT NULL DEFAULT '',
			base_commit TEXT NOT NULL DEFAULT '',
			head_commit TEXT NOT NULL DEFAULT '',
			plan_row_id INTEGER REFERENCES plans(id) ON DELETE SET NULL,
			plan_pass_row_id INTEGER REFERENCES plan_passes(id) ON DELETE SET NULL,
			created_at TEXT NOT NULL DEFAULT (datetime('now')),
			updated_at TEXT NOT NULL DEFAULT (datetime('now'))
		);
		CREATE TABLE IF NOT EXISTS artifacts (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			run_id INTEGER NOT NULL REFERENCES runs(id),
			kind TEXT NOT NULL,
			path TEXT NOT NULL DEFAULT '',
			mime_type TEXT NOT NULL DEFAULT 'text/plain',
			created_at TEXT NOT NULL DEFAULT (datetime('now'))
		);
		CREATE TABLE IF NOT EXISTS events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			run_id INTEGER NOT NULL REFERENCES runs(id),
			level TEXT NOT NULL DEFAULT 'info',
			message TEXT NOT NULL DEFAULT '',
			metadata_json TEXT NOT NULL DEFAULT '{}',
			created_at TEXT NOT NULL DEFAULT (datetime('now'))
		);
		CREATE TABLE IF NOT EXISTS checks (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			run_id INTEGER NOT NULL REFERENCES runs(id),
			kind TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'pending',
			summary TEXT NOT NULL DEFAULT '',
			details_json TEXT NOT NULL DEFAULT '{}',
			created_at TEXT NOT NULL DEFAULT (datetime('now'))
		);
		CREATE TABLE IF NOT EXISTS agent_executions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			run_id INTEGER NOT NULL REFERENCES runs(id),
			provider TEXT NOT NULL DEFAULT 'opencode_go',
			status TEXT NOT NULL DEFAULT 'configured',
			command_preview TEXT NOT NULL DEFAULT '',
			exit_code INTEGER,
			started_at TEXT,
			finished_at TEXT,
			stdout_artifact_path TEXT,
			stderr_artifact_path TEXT,
			combined_artifact_path TEXT,
			result_artifact_path TEXT,
			error TEXT,
			created_at TEXT NOT NULL DEFAULT (datetime('now')),
			updated_at TEXT NOT NULL DEFAULT (datetime('now'))
		);
		CREATE TABLE IF NOT EXISTS validation_executions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			run_id INTEGER NOT NULL REFERENCES runs(id),
			status TEXT NOT NULL DEFAULT 'starting',
			started_at TEXT NOT NULL DEFAULT (datetime('now')),
			updated_at TEXT NOT NULL DEFAULT (datetime('now')),
			finished_at TEXT,
			error TEXT
		);
		CREATE UNIQUE INDEX IF NOT EXISTS idx_validation_executions_one_active_per_run
			ON validation_executions(run_id) WHERE status IN ('starting', 'running');
	`)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func testItoa(v int64) string { return strconv.FormatInt(v, 10) }
