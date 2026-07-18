package features

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	workflowstore "relay/internal/store/workflow"
)

func TestAuthorityPublicationKeepsReplacementHistoryAndAllowsNoAuthority(t *testing.T) {
	ctx := context.Background()
	store, firstArtifact, secondArtifact := openFeatureServiceStore(t, ctx)
	service, err := NewServiceWithIDs(store, &featureTestIDs{})
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := createFeatureWorkspace(ctx, store, "workspace-feature-history", "history")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.PublishAuthority(ctx, PublishAuthorityInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, Layers: []AuthorityLayerInput{
		{Kind: "plan", ArtifactRowID: sql.NullInt64{Int64: firstArtifact, Valid: true}, ArtifactSHA256: strings.Repeat("b", 64)},
	}}); err == nil {
		t.Fatal("ordinary plan authority was accepted")
	}
	empty, err := service.ReadAuthority(ctx, workspace.WorkspaceID)
	if err != nil || len(empty) != 0 {
		t.Fatalf("optional authority = %#v, %v", empty, err)
	}
	first, workspace, err := service.PublishAuthority(ctx, PublishAuthorityInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, Layers: []AuthorityLayerInput{
		{Kind: "requirements", ArtifactRowID: sql.NullInt64{Int64: firstArtifact, Valid: true}, ArtifactSHA256: strings.Repeat("b", 64)},
		{Kind: "design", ArtifactRowID: sql.NullInt64{Int64: secondArtifact, Valid: true}, ArtifactSHA256: strings.Repeat("c", 64)},
	}})
	if err != nil {
		t.Fatal(err)
	}
	second, workspace, err := service.PublishAuthority(ctx, PublishAuthorityInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, Layers: []AuthorityLayerInput{
		{Kind: "transition_plan", ArtifactRowID: sql.NullInt64{Int64: firstArtifact, Valid: true}, ArtifactSHA256: strings.Repeat("b", 64)},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if first.Revision.RevisionNumber != 1 || second.Revision.RevisionNumber != 2 || workspace.CurrentAuthorityRevisionRowID.Int64 != second.Revision.ID {
		t.Fatalf("publication results = %#v %#v %#v", first, second, workspace)
	}
	history, err := service.ReadAuthority(ctx, workspace.WorkspaceID)
	if err != nil || len(history) != 2 || len(history[0].Layers) != 2 || len(history[1].Layers) != 1 || history[1].Layers[0].LayerKind != "transition_plan" {
		t.Fatalf("authority history = %#v, %v", history, err)
	}
}

type featureTestIDs struct{ next int }

func (ids *featureTestIDs) AuthorityRevisionID() string {
	ids.next++
	return "authority-feature-" + string(rune('0'+ids.next))
}

func openFeatureServiceStore(t *testing.T, ctx context.Context) (*workflowstore.Store, int64, int64) {
	t.Helper()
	root := t.TempDir()
	store, err := workflowstore.Open(filepath.Join(root, "workflow.sqlite"), filepath.Join(root, "artifacts"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	var projectID, planID, first, second int64
	if err := store.DB().QueryRowContext(ctx, `INSERT INTO projects (project_id, name) VALUES ('project-feature-service', 'Features') RETURNING id`).Scan(&projectID); err != nil {
		t.Fatal(err)
	}
	if err := store.DB().QueryRowContext(ctx, `INSERT INTO plans (project_row_id, plan_id, feature_slug, canonical_sha256) VALUES (?, 'plan-feature-service', 'features', ?) RETURNING id`, projectID, strings.Repeat("a", 64)).Scan(&planID); err != nil {
		t.Fatal(err)
	}
	if err := store.DB().QueryRowContext(ctx, `INSERT INTO artifacts (artifact_id, owner_type, plan_row_id, kind, relative_path, media_type, sha256, size_bytes) VALUES ('artifact-feature-requirements', 'plan', ?, 'requirements', 'plans/features/requirements.json', 'application/json', ?, 2) RETURNING id`, planID, strings.Repeat("b", 64)).Scan(&first); err != nil {
		t.Fatal(err)
	}
	if err := store.DB().QueryRowContext(ctx, `INSERT INTO artifacts (artifact_id, owner_type, plan_row_id, kind, relative_path, media_type, sha256, size_bytes) VALUES ('artifact-feature-design', 'plan', ?, 'design', 'plans/features/design.json', 'application/json', ?, 2) RETURNING id`, planID, strings.Repeat("c", 64)).Scan(&second); err != nil {
		t.Fatal(err)
	}
	return store, first, second
}

func createFeatureWorkspace(ctx context.Context, store *workflowstore.Store, workspaceID, slug string) (workflowstore.FeatureWorkspace, error) {
	var workspace workflowstore.FeatureWorkspace
	err := store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		project, err := tx.GetProjectByProjectID(ctx, "project-feature-service")
		if err != nil {
			return err
		}
		workspace, err = tx.CreateFeatureWorkspace(ctx, workflowstore.CreateFeatureWorkspaceParams{WorkspaceID: workspaceID, ProjectRowID: project.ID, FeatureSlug: slug})
		return err
	})
	return workspace, err
}
