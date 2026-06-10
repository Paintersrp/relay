package handlers

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"relay/internal/artifacts"
	"relay/internal/pipeline"
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
			branch_name TEXT NOT NULL DEFAULT '',
			base_commit TEXT NOT NULL DEFAULT '',
			head_commit TEXT NOT NULL DEFAULT '',
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

func newTestHandoff(t *testing.T, s *store.Store, handoffText string) int64 {
	t.Helper()
	repoDir := t.TempDir()
	// Create files referenced in handoff so Intake Review doesn't flag them
	os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("# repo"), 0644)
	os.WriteFile(filepath.Join(repoDir, "foo.go"), []byte("package foo"), 0644)
	// Minimal fake go.mod for scope validity
	os.WriteFile(filepath.Join(repoDir, "go.mod"), []byte("module test"), 0644)
	repo, err := s.CreateRepo("test-repo", repoDir)
	if err != nil {
		t.Fatalf("create repo: %v", err)
	}
	run, err := s.CreateRun(repo.ID, "Test Run", "draft", "test-model", "test-model", "main")
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	artifactPath, err := artifacts.Write(run.ID, "original_handoff", pipeline.ArtifactFilename("original_handoff"), []byte(handoffText))
	if err != nil {
		t.Fatalf("write artifact: %v", err)
	}
	_, err = s.CreateArtifact(run.ID, "original_handoff", artifactPath, "text/plain")
	if err != nil {
		t.Fatalf("create artifact record: %v", err)
	}
	return run.ID
}

func newTestHandoffWithRepoFiles(t *testing.T, s *store.Store, handoffText string, files map[string]string) int64 {
	t.Helper()
	repoDir := t.TempDir()
	// Create specified files in the repo
	for name, content := range files {
		dir := filepath.Dir(name)
		if dir != "." {
			os.MkdirAll(filepath.Join(repoDir, dir), 0755)
		}
		os.WriteFile(filepath.Join(repoDir, name), []byte(content), 0644)
	}
	repo, err := s.CreateRepo("test-repo", repoDir)
	if err != nil {
		t.Fatalf("create repo: %v", err)
	}
	run, err := s.CreateRun(repo.ID, "Test Run", "draft", "test-model", "test-model", "main")
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	artifactPath, err := artifacts.Write(run.ID, "original_handoff", pipeline.ArtifactFilename("original_handoff"), []byte(handoffText))
	if err != nil {
		t.Fatalf("write artifact: %v", err)
	}
	_, err = s.CreateArtifact(run.ID, "original_handoff", artifactPath, "text/plain")
	if err != nil {
		t.Fatalf("create artifact record: %v", err)
	}
	return run.ID
}

func validHandoff() string {
	return `# Test Handoff

## Goal

Do a thing.

## Scope

- README.md

## Do not change

- Nothing.

## Task checklist

- [ ] Do it

## Tests / validation

` + "```bash" + `
go test ./...
` + "```" + `

## Agent final output requirement

Return DONE or BLOCKED.
`
}

func blockedHandoff() string {
	// Has a missing section to trigger validation failures
	return `# Blocked Handoff

## Goal

Do a thing.

## Scope

- foo.go

## Do not change

- Nothing.

`
}

func scopeMismatchHandoff() string {
	return `# Scope Mismatch Handoff

## Goal

Do a thing.

## Scope

- src/definitely-missing.ts

## Do not change

- Nothing.

## Task checklist

- [ ] Do it

## Tests / validation

` + "```bash" + `
go test ./...
` + "```" + `

## Agent final output requirement

Return DONE or BLOCKED.
`
}

func TestPrepareRunForReviewWithValidHandoff(t *testing.T) {
	s := setupTestStore(t)
	h := NewRunsHandler(s, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	runID := newTestHandoff(t, s, validHandoff())

	result := h.prepareRunForReview(runID)

	if result.Blocked {
		t.Errorf("expected no blockers, got %v", result.Blockers)
	}
	if !result.PromptGenerated {
		t.Error("expected prompt to be generated")
	}
	if !result.PacketGenerated {
		t.Error("expected packet to be generated")
	}

	// Verify artifacts stored
	artifactsList, err := s.ListArtifactsByRun(runID)
	if err != nil {
		t.Fatalf("list artifacts: %v", err)
	}

	hasHandoffValidation := false
	hasAgentPrompt := false
	hasPacket := false
	for _, a := range artifactsList {
		switch a.Kind {
		case "handoff_validation_json":
			hasHandoffValidation = true
		case "agent_prompt":
			hasAgentPrompt = true
		case "opencode_handoff_packet":
			hasPacket = true
		}
	}

	if !hasHandoffValidation {
		t.Error("expected handoff_validation_json artifact")
	}
	if !hasAgentPrompt {
		t.Error("expected agent_prompt artifact")
	}
	if !hasPacket {
		t.Error("expected opencode_handoff_packet artifact")
	}
}

func TestPrepareRunForReviewWithBlocker(t *testing.T) {
	s := setupTestStore(t)
	h := NewRunsHandler(s, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	runID := newTestHandoff(t, s, blockedHandoff())

	result := h.prepareRunForReview(runID)

	if !result.Blocked {
		t.Error("expected blockers to be detected")
	}
	if len(result.Blockers) == 0 {
		t.Error("expected at least one blocker")
	}
	if result.PromptGenerated {
		t.Error("expected prompt NOT to be generated when blocked")
	}
	if result.PacketGenerated {
		t.Error("expected packet NOT to be generated when blocked")
	}

	// Verify only validation artifact was stored
	artifactsList, err := s.ListArtifactsByRun(runID)
	if err != nil {
		t.Fatalf("list artifacts: %v", err)
	}

	hasHandoffValidation := false
	hasAgentPrompt := false
	hasPacket := false
	for _, a := range artifactsList {
		switch a.Kind {
		case "handoff_validation_json":
			hasHandoffValidation = true
		case "agent_prompt":
			hasAgentPrompt = true
		case "opencode_handoff_packet":
			hasPacket = true
		}
	}

	if !hasHandoffValidation {
		t.Error("expected handoff_validation_json artifact")
	}
	if hasAgentPrompt {
		t.Error("expected no agent_prompt artifact when blocked")
	}
	if hasPacket {
		t.Error("expected no opencode_handoff_packet artifact when blocked")
	}
}

func TestPrepareRunForReviewWithIntakeReviewScopeBlocker(t *testing.T) {
	s := setupTestStore(t)
	h := NewRunsHandler(s, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	// Use a repo with only basic files that does NOT include src/definitely-missing.ts
	runID := newTestHandoffWithRepoFiles(t, s, scopeMismatchHandoff(), map[string]string{
		"README.md": "# repo",
	})

	result := h.prepareRunForReview(runID)

	if !result.Blocked {
		t.Fatal("expected blockers to be detected from Intake Review")
	}
	found := false
	for _, b := range result.Blockers {
		if b == "Selected repo does not appear to match handoff scope." {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected blocker 'Selected repo does not appear to match handoff scope.', got %v", result.Blockers)
	}
	if result.PromptGenerated {
		t.Error("expected prompt NOT to be generated when blocked by Intake Review")
	}
	if result.PacketGenerated {
		t.Error("expected packet NOT to be generated when blocked by Intake Review")
	}

	// Verify artifacts
	artifactsList, err := s.ListArtifactsByRun(runID)
	if err != nil {
		t.Fatalf("list artifacts: %v", err)
	}

	hasHandoffValidation := false
	hasAgentPrompt := false
	hasPacket := false
	for _, a := range artifactsList {
		switch a.Kind {
		case "handoff_validation_json":
			hasHandoffValidation = true
		case "agent_prompt":
			hasAgentPrompt = true
		case "opencode_handoff_packet":
			hasPacket = true
		}
	}

	if !hasHandoffValidation {
		t.Error("expected handoff_validation_json artifact")
	}
	if hasAgentPrompt {
		t.Error("expected no agent_prompt artifact when blocked by Intake Review")
	}
	if hasPacket {
		t.Error("expected no opencode_handoff_packet artifact when blocked by Intake Review")
	}

	// Verify run status is needs_review
	run, err := s.GetRun(runID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if run.Status != "needs_review" {
		t.Errorf("expected run status 'needs_review', got %q", run.Status)
	}

	// Verify at least one warn event mentions automatic setup stopped or Intake Review blockers
	events, err := s.ListEventsByRun(runID)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	hasWarn := false
	for _, e := range events {
		if e.Level == "warn" && (contains(e.Message, "Automatic setup stopped") || contains(e.Message, "Intake Review")) {
			hasWarn = true
			break
		}
	}
	if !hasWarn {
		t.Error("expected a warn event about automatic setup stopping or Intake Review blockers")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && containsFunc(s, substr)
}

func containsFunc(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
