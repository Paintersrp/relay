package workflowfixture

import (
	"database/sql"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	relaydb "relay/internal/db"
)

type migratedStore interface {
	DB() *sql.DB
	Close() error
}

var template struct {
	sync.Once
	bytes []byte
	err   error
}

// Open copies the process-local migrated template into a private fixture and
// opens it through the supplied production store constructor.
func Open[T migratedStore](t testing.TB, opener func(string, string) (T, error)) T {
	t.Helper()
	root := t.TempDir()
	return OpenAt(t, filepath.Join(root, "workflow.sqlite"), filepath.Join(root, "artifacts"), opener)
}

// OpenAt prepares dbPath from the template when it does not yet exist. It is
// intended for tests that deliberately reopen or concurrently open one copy.
func OpenAt[T migratedStore](t testing.TB, dbPath, artifactRoot string, opener func(string, string) (T, error)) T {
	t.Helper()
	template.Do(func() { template.bytes, template.err = build(opener) })
	if template.err != nil {
		t.Fatalf("build workflow fixture template: %v", template.err)
	}
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
			t.Fatalf("create workflow fixture directory: %v", err)
		}
		if err := os.WriteFile(dbPath, template.bytes, 0o600); err != nil {
			t.Fatalf("write workflow fixture database: %v", err)
		}
	} else if err != nil {
		t.Fatalf("inspect workflow fixture database: %v", err)
	}
	if err := os.MkdirAll(artifactRoot, 0o755); err != nil {
		t.Fatalf("create workflow fixture artifacts: %v", err)
	}
	store, err := opener(dbPath, artifactRoot)
	if err != nil {
		t.Fatalf("open workflow fixture: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func build[T migratedStore](opener func(string, string) (T, error)) ([]byte, error) {
	directory, err := os.MkdirTemp("", "relay-workflow-template-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(directory)
	path := filepath.Join(directory, "workflow.sqlite")
	store, err := opener(path, filepath.Join(directory, "artifacts"))
	if err != nil {
		return nil, err
	}
	closed := false
	defer func() {
		if !closed {
			_ = store.Close()
		}
	}()
	latest, err := latestMigrationVersion()
	if err != nil {
		return nil, err
	}
	var version int
	if err := store.DB().QueryRow("SELECT version_id FROM goose_db_version WHERE is_applied = 1 ORDER BY id DESC LIMIT 1").Scan(&version); err != nil {
		return nil, fmt.Errorf("read schema version: %w", err)
	}
	if version != latest {
		return nil, fmt.Errorf("schema version = %d, want %d", version, latest)
	}
	var busy, logFrames, checkpointedFrames int
	if err := store.DB().QueryRow("PRAGMA wal_checkpoint(TRUNCATE)").Scan(&busy, &logFrames, &checkpointedFrames); err != nil {
		return nil, fmt.Errorf("checkpoint template: %w", err)
	}
	if busy != 0 || logFrames > 0 || checkpointedFrames > 0 {
		return nil, fmt.Errorf("template WAL not empty: busy=%d log=%d checkpointed=%d", busy, logFrames, checkpointedFrames)
	}
	if err := store.Close(); err != nil {
		return nil, fmt.Errorf("close template: %w", err)
	}
	closed = true
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		if _, err := os.Stat(path + suffix); err == nil {
			return nil, fmt.Errorf("template has SQLite sidecar %s", suffix)
		} else if !os.IsNotExist(err) {
			return nil, err
		}
	}
	return os.ReadFile(path)
}

func latestMigrationVersion() (int, error) {
	entries, err := fs.ReadDir(relaydb.WorkflowMigrationsFS, "workflow_migrations")
	if err != nil {
		return 0, err
	}
	latest := 0
	for _, entry := range entries {
		prefix, _, ok := strings.Cut(entry.Name(), "_")
		if !ok {
			continue
		}
		version, err := strconv.Atoi(prefix)
		if err != nil {
			return 0, err
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
