package workflowstore

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	relaydb "relay/internal/db"
	workflowgenerated "relay/internal/store/workflowgenerated"

	"github.com/pressly/goose/v3"
)

func TestFeatureWorkspaceGeneratedQueriesPreserveAuthorityAndRouteHistory(t *testing.T) {
	ctx := context.Background()
	store, _ := openWorkflowTestStore(t)
	projectID, artifactID := seedFeatureWorkspaceAuthorityArtifact(t, ctx, store)
	queries := workflowgenerated.New(store.DB())

	workspace, err := queries.CreateFeatureWorkspace(ctx, workflowgenerated.CreateFeatureWorkspaceParams{
		WorkspaceID:  "workspace-payments",
		ProjectRowID: projectID,
		FeatureSlug:  "payments",
	})
	if err != nil {
		t.Fatal(err)
	}
	sha := strings.Repeat("b", 64)
	input, err := queries.CreateFeatureWorkspaceAdmittedInput(ctx, workflowgenerated.CreateFeatureWorkspaceAdmittedInputParams{
		AdmittedInputID: "input-payments-requirements",
		WorkspaceRowID:  workspace.ID,
		Sequence:        1,
		InputName:       "requirements",
		InputRole:       "governing",
		SourceKind:      "relay_artifact",
		ArtifactRowID:   sql.NullInt64{Int64: artifactID, Valid: true},
		ArtifactSha256:  sql.NullString{String: sha, Valid: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if input.ArtifactSha256.String != sha {
		t.Fatalf("admitted input sha = %q, want %q", input.ArtifactSha256.String, sha)
	}

	if _, err := store.DB().Exec(`UPDATE feature_workspace_admitted_inputs SET input_name = 'mutated' WHERE id = ?`, input.ID); err == nil {
		t.Fatal("admitted input history was mutable")
	}
	if _, err := store.DB().Exec(`
INSERT INTO feature_workspace_admitted_inputs (
    admitted_input_id, workspace_row_id, sequence, input_name, input_role,
    source_kind, artifact_row_id, artifact_sha256
) VALUES ('input-chat', ?, 2, 'chat', 'candidate', 'chat_transcript', ?, ?)`, workspace.ID, artifactID, sha); err == nil {
		t.Fatal("chat transcript was admitted as a workspace input")
	}
	if _, err := store.DB().Exec(`
INSERT INTO feature_workspace_admitted_inputs (
    admitted_input_id, workspace_row_id, sequence, input_name, input_role,
    source_kind, artifact_row_id, artifact_sha256
) VALUES ('input-wrong-sha', ?, 2, 'wrong-sha', 'candidate', 'relay_artifact', ?, ?)`, workspace.ID, artifactID, strings.Repeat("c", 64)); err == nil {
		t.Fatal("admitted input accepted a conflicting artifact sha")
	}

	revision, err := queries.CreateFeatureWorkspaceAuthorityRevision(ctx, workflowgenerated.CreateFeatureWorkspaceAuthorityRevisionParams{
		AuthorityRevisionID: "authority-payments-1",
		WorkspaceRowID:      workspace.ID,
		RevisionNumber:      1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := queries.CreateFeatureWorkspaceAuthorityLayer(ctx, workflowgenerated.CreateFeatureWorkspaceAuthorityLayerParams{
		AuthorityRevisionRowID: revision.ID,
		LayerKind:              "requirements",
		Sequence:               1,
		ArtifactRowID:          sql.NullInt64{Int64: artifactID, Valid: true},
		ArtifactSha256:         sha,
	}); err != nil {
		t.Fatal(err)
	}
	updated, err := queries.SetFeatureWorkspaceAuthorityRevision(ctx, workflowgenerated.SetFeatureWorkspaceAuthorityRevisionParams{
		CurrentAuthorityRevisionRowID: sql.NullInt64{Int64: revision.ID, Valid: true},
		WorkspaceID:                   workspace.WorkspaceID,
		Version:                       workspace.Version,
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Version != workspace.Version+1 {
		t.Fatalf("workspace authority version = %d, want %d", updated.Version, workspace.Version+1)
	}
	if _, err := queries.SetFeatureWorkspaceAuthorityRevision(ctx, workflowgenerated.SetFeatureWorkspaceAuthorityRevisionParams{
		CurrentAuthorityRevisionRowID: sql.NullInt64{Int64: revision.ID, Valid: true},
		WorkspaceID:                   workspace.WorkspaceID,
		Version:                       workspace.Version,
	}); err != sql.ErrNoRows {
		t.Fatalf("stale authority update error = %v, want sql.ErrNoRows", err)
	}

	route, err := queries.CreateFeatureWorkspaceRouteState(ctx, workflowgenerated.CreateFeatureWorkspaceRouteStateParams{
		RouteStateID:     "route-payments-1",
		WorkspaceRowID:   workspace.ID,
		Sequence:         1,
		WorkspaceVersion: updated.Version + 1,
		State:            "discovery",
	})
	if err != nil {
		t.Fatal(err)
	}
	updated, err = queries.AdvanceFeatureWorkspaceRouteState(ctx, workflowgenerated.AdvanceFeatureWorkspaceRouteStateParams{
		CurrentRouteStateRowID: sql.NullInt64{Int64: route.ID, Valid: true},
		State:                  "open",
		WorkspaceID:            workspace.WorkspaceID,
		Version:                updated.Version,
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.CurrentRouteStateRowID.Int64 != route.ID || updated.Version != route.WorkspaceVersion {
		t.Fatalf("advanced route state = %#v, want route %#v", updated, route)
	}
	if _, err := store.DB().Exec(`DELETE FROM feature_workspace_route_states WHERE id = ?`, route.ID); err == nil {
		t.Fatal("route state history was deletable")
	}
}

func TestFeatureWorkspaceMigrationUpgradesAndRollsBack(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "workflow.sqlite")
	database, err := sql.Open("sqlite", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	goose.SetBaseFS(relaydb.WorkflowMigrationsFS)
	if err := goose.SetDialect("sqlite3"); err != nil {
		t.Fatal(err)
	}
	if err := goose.UpTo(database, "workflow_migrations", 10); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO projects (project_id, name) VALUES ('project-workspace-upgrade', 'Upgrade')`); err != nil {
		t.Fatal(err)
	}
	if err := relaydb.AutoMigrateWorkflow(database); err != nil {
		t.Fatal(err)
	}
	var projects int
	if err := database.QueryRow(`SELECT COUNT(*) FROM projects WHERE project_id = 'project-workspace-upgrade'`).Scan(&projects); err != nil {
		t.Fatal(err)
	}
	if projects != 1 {
		t.Fatalf("upgraded project rows = %d, want 1", projects)
	}
	if err := goose.DownTo(database, "workflow_migrations", 10); err != nil {
		t.Fatal(err)
	}
	var tables int
	if err := database.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'feature_workspaces'`).Scan(&tables); err != nil {
		t.Fatal(err)
	}
	if tables != 0 {
		t.Fatalf("feature workspace table survived rollback")
	}
}

func seedFeatureWorkspaceAuthorityArtifact(t *testing.T, ctx context.Context, store *Store) (int64, int64) {
	t.Helper()
	var projectID, planID, artifactID int64
	sha := strings.Repeat("b", 64)
	if err := store.DB().QueryRowContext(ctx, `
INSERT INTO projects (project_id, name)
VALUES ('project-feature-workspace', 'Feature Workspace')
RETURNING id`).Scan(&projectID); err != nil {
		t.Fatal(err)
	}
	if err := store.DB().QueryRowContext(ctx, `
INSERT INTO plans (project_row_id, plan_id, feature_slug, canonical_sha256)
VALUES (?, 'plan-feature-workspace', 'feature-workspace', ?)
RETURNING id`, projectID, strings.Repeat("a", 64)).Scan(&planID); err != nil {
		t.Fatal(err)
	}
	if err := store.DB().QueryRowContext(ctx, `
INSERT INTO artifacts (
    artifact_id, owner_type, plan_row_id, kind, relative_path, media_type, sha256, size_bytes
)
VALUES ('artifact-feature-workspace', 'plan', ?, 'requirements', 'plans/feature-workspace/requirements.json', 'application/json', ?, 2)
RETURNING id`, planID, sha).Scan(&artifactID); err != nil {
		t.Fatal(err)
	}
	return projectID, artifactID
}
