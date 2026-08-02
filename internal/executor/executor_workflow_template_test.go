package executor

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	relaydb "relay/internal/db"
	workflowstore "relay/internal/store/workflow"
)

var executorWorkflowTemplatePath string
var executorWorkflowTemplateMigrations atomic.Int32

func TestMain(m *testing.M) {
	directory, err := os.MkdirTemp("", "relay-executor-workflow-template-")
	if err != nil {
		panic(fmt.Errorf("create executor workflow template directory: %w", err))
	}
	executorWorkflowTemplatePath = filepath.Join(directory, "workflow.sqlite")
	if err := createExecutorWorkflowTemplate(executorWorkflowTemplatePath); err != nil {
		_ = os.RemoveAll(directory)
		panic(err)
	}

	code := m.Run()
	_ = os.RemoveAll(directory)
	os.Exit(code)
}

// newExecutorWorkflowStore provides an isolated copy of the closed, schema-only
// template for tests whose subject is not workflow migration behavior.
func newExecutorWorkflowStore(t *testing.T) *workflowstore.Store {
	t.Helper()
	root := t.TempDir()
	databasePath := filepath.Join(root, "workflow.sqlite")
	if err := copyExecutorWorkflowTemplate(executorWorkflowTemplatePath, databasePath); err != nil {
		t.Fatal(err)
	}
	artifactRoot := filepath.Join(root, "artifacts")
	if err := os.MkdirAll(artifactRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	store, err := workflowstore.Open(databasePath, artifactRoot)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func createExecutorWorkflowTemplate(path string) error {
	store, err := workflowstore.Open(path, filepath.Join(filepath.Dir(path), "artifacts"))
	if err != nil {
		return fmt.Errorf("open executor workflow template: %w", err)
	}
	executorWorkflowTemplateMigrations.Add(1)
	if err := assertExecutorWorkflowLatest(store); err != nil {
		_ = store.Close()
		return err
	}
	var busy, logFrames, checkpointedFrames int
	if err := store.DB().QueryRow("PRAGMA wal_checkpoint(TRUNCATE)").Scan(&busy, &logFrames, &checkpointedFrames); err != nil {
		_ = store.Close()
		return fmt.Errorf("checkpoint executor workflow template: %w", err)
	}
	// SQLite reports -1 frame counts when WAL mode is not active; that is already
	// a finalized main database. Positive log frames would make copying unsafe.
	if busy != 0 || logFrames > 0 || checkpointedFrames > 0 {
		_ = store.Close()
		return fmt.Errorf("executor workflow template WAL not empty: busy=%d log=%d checkpointed=%d", busy, logFrames, checkpointedFrames)
	}
	if err := store.Close(); err != nil {
		return fmt.Errorf("close executor workflow template: %w", err)
	}
	for _, sidecar := range []string{"-wal", "-shm", "-journal"} {
		if _, err := os.Stat(path + sidecar); err == nil {
			return fmt.Errorf("executor workflow template has active SQLite sidecar %s", path+sidecar)
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("inspect executor workflow template sidecar: %w", err)
		}
	}
	return nil
}

func copyExecutorWorkflowTemplate(source, destination string) error {
	in, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("open executor workflow template: %w", err)
	}
	defer in.Close()
	out, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create executor workflow database copy: %w", err)
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		return fmt.Errorf("copy executor workflow template: %w", copyErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close executor workflow database copy: %w", closeErr)
	}
	return nil
}

func assertExecutorWorkflowLatest(store *workflowstore.Store) error {
	var version int
	if err := store.DB().QueryRow("SELECT version_id FROM goose_db_version WHERE is_applied = 1 ORDER BY id DESC LIMIT 1").Scan(&version); err != nil {
		return fmt.Errorf("read executor workflow schema version: %w", err)
	}
	latest, err := latestExecutorWorkflowMigrationVersion()
	if err != nil {
		return err
	}
	if version != latest {
		return fmt.Errorf("executor workflow schema version = %d, want %d", version, latest)
	}
	return nil
}

func latestExecutorWorkflowMigrationVersion() (int, error) {
	entries, err := fs.ReadDir(relaydb.WorkflowMigrationsFS, "workflow_migrations")
	if err != nil {
		return 0, fmt.Errorf("read workflow migrations: %w", err)
	}
	latest := 0
	for _, entry := range entries {
		name := entry.Name()
		prefix, _, ok := strings.Cut(name, "_")
		if !ok {
			continue
		}
		version, err := strconv.Atoi(prefix)
		if err != nil {
			return 0, fmt.Errorf("parse workflow migration %q: %w", name, err)
		}
		if version > latest {
			latest = version
		}
	}
	if latest == 0 {
		return 0, fmt.Errorf("no workflow migrations found")
	}
	return latest, nil
}

func TestExecutorWorkflowTemplateIsLatestAndFinalized(t *testing.T) {
	store := newExecutorWorkflowStore(t)
	if err := assertExecutorWorkflowLatest(store); err != nil {
		t.Fatal(err)
	}
	for _, sidecar := range []string{"-wal", "-shm", "-journal"} {
		if _, err := os.Stat(executorWorkflowTemplatePath + sidecar); !os.IsNotExist(err) {
			t.Fatalf("template sidecar %s: %v", sidecar, err)
		}
	}
}

func TestExecutorWorkflowTemplateFixturesAreIsolatedAndUsable(t *testing.T) {
	first := newExecutorWorkflowStore(t)
	second := newExecutorWorkflowStore(t)
	if first.ArtifactStore().Root() == second.ArtifactStore().Root() {
		t.Fatal("fixture artifact roots are shared")
	}
	if _, err := first.DB().Exec("INSERT INTO projects (project_id, name) VALUES ('project-template-first', 'First')"); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := second.DB().QueryRow("SELECT COUNT(*) FROM projects WHERE project_id = 'project-template-first'").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("second fixture contains first fixture data: %d", count)
	}

	transaction, err := first.DB().BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.Exec("INSERT INTO projects (project_id, name) VALUES ('project-template-rollback', 'Rollback')"); err != nil {
		t.Fatal(err)
	}
	if err := transaction.Rollback(); err != nil {
		t.Fatal(err)
	}
	if err := first.DB().QueryRow("SELECT COUNT(*) FROM projects WHERE project_id = 'project-template-rollback'").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("rolled-back project count = %d", count)
	}
	if _, err := first.DB().Exec("INSERT INTO project_notes (note_id, project_row_id, title, body) VALUES ('note-template-fk', 999, 'Note', 'body')"); err == nil {
		t.Fatal("foreign key violation succeeded")
	}
	if _, err := first.DB().Exec("DELETE FROM projects WHERE project_id = 'project-template-first'"); err == nil {
		t.Fatal("project delete trigger did not run")
	}

	batch, err := first.ArtifactStore().Begin("template")
	if err != nil {
		t.Fatal(err)
	}
	file, err := batch.Stage("fixture", "artifact.txt", "text/plain", []byte("artifact"))
	if err != nil {
		t.Fatal(err)
	}
	if err := batch.Promote(); err != nil {
		t.Fatal(err)
	}
	if err := batch.PrepareCommit(); err != nil {
		t.Fatal(err)
	}
	if _, content, err := first.ArtifactStore().ReadVerifiedFile(file, 32); err != nil || string(content) != "artifact" {
		t.Fatalf("artifact-backed operation content=%q err=%v", content, err)
	}
}

func TestExecutorWorkflowTemplateAvoidsRepeatedFullMigrations(t *testing.T) {
	before := executorWorkflowTemplateMigrations.Load()
	first := newExecutorWorkflowStore(t)
	second := newExecutorWorkflowStore(t)
	if err := assertExecutorWorkflowLatest(first); err != nil {
		t.Fatal(err)
	}
	if err := assertExecutorWorkflowLatest(second); err != nil {
		t.Fatal(err)
	}
	if got := executorWorkflowTemplateMigrations.Load(); got != before || got != 1 {
		t.Fatalf("full workflow migration executions = %d, want 1", got)
	}
}
