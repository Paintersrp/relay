package workflowstore

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	_ "modernc.org/sqlite"
)

func TestRepositoryTargetBranchMigrationPreservesRowsAndRelationships(t *testing.T) {
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "legacy.sqlite")+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`
CREATE TABLE repository_targets (
    repo_target TEXT PRIMARY KEY COLLATE NOCASE,
    local_path TEXT NOT NULL UNIQUE,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
CREATE TABLE projects (
    id INTEGER PRIMARY KEY,
    project_id TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'active',
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
CREATE TABLE project_repository_targets (
    project_row_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    repo_target TEXT NOT NULL REFERENCES repository_targets(repo_target) ON DELETE RESTRICT,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    PRIMARY KEY (project_row_id, repo_target)
);
INSERT INTO repository_targets (repo_target, local_path) VALUES ('relay', '/tmp/relay');
INSERT INTO projects (id, project_id, name) VALUES (1, 'project-migration', 'Migration');
INSERT INTO project_repository_targets (project_row_id, repo_target) VALUES (1, 'relay');
`); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join("..", "..", "db", "workflow_migrations", "00007_repository_target_branches.sql"))
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(string(data), "-- +goose Down")
	if len(parts) != 2 {
		t.Fatal("migration does not contain one goose Down boundary")
	}
	up := strings.TrimPrefix(parts[0], "-- +goose Up")
	if _, err := db.Exec(up); err != nil {
		t.Fatal(err)
	}
	var ref sql.NullString
	var version int64
	if err := db.QueryRow(`
SELECT configured_branch_ref, configuration_version
FROM repository_targets
WHERE repo_target = 'relay'`).Scan(&ref, &version); err != nil {
		t.Fatal(err)
	}
	if ref.Valid {
		t.Fatalf("migrated configured ref = %q, want null", ref.String)
	}
	if version != 1 {
		t.Fatalf("migrated configuration version = %d, want 1", version)
	}
	var relationshipCount int
	if err := db.QueryRow(`
SELECT COUNT(*)
FROM project_repository_targets
WHERE project_row_id = 1 AND repo_target = 'relay'`).Scan(&relationshipCount); err != nil {
		t.Fatal(err)
	}
	if relationshipCount != 1 {
		t.Fatalf("Project relationship count = %d, want 1", relationshipCount)
	}
	if _, err := db.Exec(`INSERT INTO repository_targets (repo_target, local_path) VALUES ('other', '/tmp/relay')`); err == nil {
		t.Fatal("local-path uniqueness was lost")
	}
	if _, err := db.Exec(`DELETE FROM repository_targets WHERE repo_target = 'relay'`); err == nil {
		t.Fatal("Project foreign key no longer protects repository target")
	}
}

func TestRepositoryTargetBranchPersistenceMatrix(t *testing.T) {
	ctx := context.Background()
	store, _ := openWorkflowTestStore(t)

	var unconfigured RepositoryTarget
	var configured RepositoryTarget
	if err := store.WithTx(ctx, func(tx *Tx) error {
		var err error
		unconfigured, err = tx.CreateRepositoryTarget(ctx, "unconfigured", filepath.Join(t.TempDir(), "unconfigured"))
		if err != nil {
			return err
		}
		configured, err = tx.CreateRepositoryTargetWithConfiguration(ctx, CreateRepositoryTargetParams{
			RepoTarget:          "configured",
			LocalPath:           filepath.Join(t.TempDir(), "configured"),
			ConfiguredBranchRef: sql.NullString{String: "refs/heads/main", Valid: true},
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if unconfigured.ConfiguredBranchRef.Valid || unconfigured.ConfigurationVersion != 1 {
		t.Fatalf("new unconfigured target = %#v", unconfigured)
	}
	if !configured.ConfiguredBranchRef.Valid ||
		configured.ConfiguredBranchRef.String != "refs/heads/main" ||
		configured.ConfigurationVersion != 1 {
		t.Fatalf("new configured target = %#v", configured)
	}

	byTarget, err := store.GetRepositoryTarget(ctx, "configured")
	if err != nil {
		t.Fatal(err)
	}
	byPath, err := store.GetRepositoryTargetByLocalPath(ctx, configured.LocalPath)
	if err != nil {
		t.Fatal(err)
	}
	listed, err := store.ListRepositoryTargetsWithConfiguration(ctx)
	if err != nil {
		t.Fatal(err)
	}
	assertRepositoryTargetConfiguration(t, byTarget, "refs/heads/main", 1)
	assertRepositoryTargetConfiguration(t, byPath, "refs/heads/main", 1)
	found := false
	for _, target := range listed {
		if target.RepoTarget == "configured" {
			assertRepositoryTargetConfiguration(t, target, "refs/heads/main", 1)
			found = true
		}
	}
	if !found {
		t.Fatal("production list read omitted configured target")
	}

	var changed RepositoryTarget
	if err := store.WithTx(ctx, func(tx *Tx) error {
		var err error
		changed, err = tx.ConfigureRepositoryTarget(ctx, ConfigureRepositoryTargetParams{
			RepoTarget:                   "configured",
			ExpectedConfigurationVersion: 1,
			ConfiguredBranchRef:          "refs/heads/release",
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	assertRepositoryTargetConfiguration(t, changed, "refs/heads/release", 2)

	err = store.WithTx(ctx, func(tx *Tx) error {
		_, err := tx.ConfigureRepositoryTarget(ctx, ConfigureRepositoryTargetParams{
			RepoTarget:                   "configured",
			ExpectedConfigurationVersion: 1,
			ConfiguredBranchRef:          "refs/heads/stale",
		})
		return err
	})
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("stale CAS error = %v, want sql.ErrNoRows", err)
	}
	afterStale, err := store.GetRepositoryTarget(ctx, "configured")
	if err != nil {
		t.Fatal(err)
	}
	assertRepositoryTargetConfiguration(t, afterStale, "refs/heads/release", 2)
}

func TestRepositoryTargetBranchConstraints(t *testing.T) {
	store, _ := openWorkflowTestStore(t)
	path := filepath.Join(t.TempDir(), "target")
	cases := []struct {
		name    string
		ref     any
		version int64
	}{
		{name: "zero version", ref: nil, version: 0},
		{name: "negative version", ref: nil, version: -1},
		{name: "empty ref", ref: "", version: 1},
		{name: "short ref", ref: "main", version: 1},
		{name: "outer whitespace", ref: " refs/heads/main", version: 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := store.DB().Exec(`
INSERT INTO repository_targets (
    repo_target, local_path, configured_branch_ref, configuration_version
)
VALUES (?, ?, ?, ?)`, "case-"+strings.ReplaceAll(tc.name, " ", "-"), path+"-"+tc.name, tc.ref, tc.version)
			if err == nil {
				t.Fatal("invalid repository target configuration was accepted")
			}
		})
	}
}

func TestConfigureRepositoryTargetConcurrentCASHasOneWinner(t *testing.T) {
	ctx := context.Background()
	store, _ := openWorkflowTestStore(t)
	if err := store.WithTx(ctx, func(tx *Tx) error {
		_, err := tx.CreateRepositoryTargetWithConfiguration(ctx, CreateRepositoryTargetParams{
			RepoTarget:          "relay",
			LocalPath:           filepath.Join(t.TempDir(), "relay"),
			ConfiguredBranchRef: sql.NullString{String: "refs/heads/main", Valid: true},
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}

	start := make(chan struct{})
	results := make(chan error, 2)
	var wait sync.WaitGroup
	for _, ref := range []string{"refs/heads/one", "refs/heads/two"} {
		ref := ref
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			results <- store.WithTx(ctx, func(tx *Tx) error {
				_, err := tx.ConfigureRepositoryTarget(ctx, ConfigureRepositoryTargetParams{
					RepoTarget:                   "relay",
					ExpectedConfigurationVersion: 1,
					ConfiguredBranchRef:          ref,
				})
				return err
			})
		}()
	}
	close(start)
	wait.Wait()
	close(results)
	successes := 0
	stale := 0
	for err := range results {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, sql.ErrNoRows):
			stale++
		default:
			t.Fatalf("concurrent CAS error = %v", err)
		}
	}
	if successes != 1 || stale != 1 {
		t.Fatalf("CAS outcomes: successes=%d stale=%d", successes, stale)
	}
	target, err := store.GetRepositoryTarget(ctx, "relay")
	if err != nil {
		t.Fatal(err)
	}
	if target.ConfigurationVersion != 2 {
		t.Fatalf("winner configuration version = %d, want 2", target.ConfigurationVersion)
	}
}

func assertRepositoryTargetConfiguration(
	t *testing.T,
	target RepositoryTarget,
	ref string,
	version int64,
) {
	t.Helper()
	if target.ConfigurationVersion != version {
		t.Fatalf("configuration version = %d, want %d", target.ConfigurationVersion, version)
	}
	if ref == "" {
		if target.ConfiguredBranchRef.Valid {
			t.Fatalf("configured ref = %q, want null", target.ConfiguredBranchRef.String)
		}
		return
	}
	if !target.ConfiguredBranchRef.Valid || target.ConfiguredBranchRef.String != ref {
		t.Fatalf("configured ref = %#v, want %q", target.ConfiguredBranchRef, ref)
	}
}
