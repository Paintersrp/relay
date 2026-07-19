package features

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	workflowtickets "relay/internal/app/tickets"
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

func TestFeatureCompletionIsExplicitGuardedAndReopensForCurrentDefinitionChanges(t *testing.T) {
	ctx := context.Background()
	store, firstArtifact, secondArtifact := openFeatureServiceStore(t, ctx)
	if _, err := store.DB().ExecContext(ctx, `INSERT INTO repository_targets (repo_target, local_path, configured_branch_ref, configuration_version) VALUES ('relay', 'C:/relay', 'refs/heads/main', 1)`); err != nil {
		t.Fatal(err)
	}
	var vaultID, closureID int64
	if err := store.DB().QueryRowContext(ctx, `INSERT INTO source_vaults (vault_id, repo_target, relative_path) VALUES ('vault-feature-completion', 'relay', 'vaults/features') RETURNING id`).Scan(&vaultID); err != nil {
		t.Fatal(err)
	}
	if err := store.DB().QueryRowContext(ctx, `
INSERT INTO source_vault_closures (closure_id, vault_row_id, commit_oid, tree_oid, generation, ref_name, state, import_started_at, verified_at)
VALUES ('closure-feature-completion', ?, ?, ?, 1, 'refs/relay/closures/feature-completion', 'ready', '2026-07-18T00:00:00.000000000Z', '2026-07-18T00:00:01.000000000Z')
RETURNING id`, vaultID, strings.Repeat("d", 40), strings.Repeat("e", 40)).Scan(&closureID); err != nil {
		t.Fatal(err)
	}
	service, err := NewServiceWithIDs(store, &featureTestIDs{})
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := createFeatureWorkspace(ctx, store, "workspace-feature-completion", "completion")
	if err != nil {
		t.Fatal(err)
	}
	_, workspace, err = service.PublishAuthority(ctx, PublishAuthorityInput{
		WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, SourceClosureID: sql.NullInt64{Int64: closureID, Valid: true},
		Layers: []AuthorityLayerInput{{Kind: "requirements", ArtifactRowID: sql.NullInt64{Int64: firstArtifact, Valid: true}, ArtifactSHA256: strings.Repeat("b", 64), SourceClosureID: sql.NullInt64{Int64: closureID, Valid: true}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	status, err := service.EvaluateCompletion(ctx, workspace.WorkspaceID)
	if err != nil || !completionGateReady(status, "authority") || !completionGateReady(status, "tickets") || !completionGateReady(status, "integration") || !completionGateReady(status, "transitions") || !completionGateReady(status, "remediation") || !completionGateReady(status, "audit") {
		t.Fatalf("initial completion gates = %#v, err=%v", status, err)
	}
	if _, err := service.Complete(ctx, CompletionInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version}); !errors.Is(err, ErrFeatureCompletionConfirmation) {
		t.Fatalf("unconfirmed completion error = %v", err)
	}
	completed, err := service.Complete(ctx, CompletionInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, OperatorConfirmed: true})
	if err != nil || completed.Decision.Decision != "completed" {
		t.Fatalf("explicit completion = %#v, err=%v", completed, err)
	}
	status, err = service.EvaluateCompletion(ctx, workspace.WorkspaceID)
	if err != nil || status.CurrentDecision == nil || status.CurrentDecision.ID != completed.Decision.ID {
		t.Fatalf("current explicit completion = %#v, err=%v", status, err)
	}
	if _, err := service.Complete(ctx, CompletionInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, OperatorConfirmed: true}); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("stale completion error = %v", err)
	}
	_, workspace, err = service.PublishAuthority(ctx, PublishAuthorityInput{
		WorkspaceID: workspace.WorkspaceID, ExpectedVersion: status.Workspace.Version, SourceClosureID: sql.NullInt64{Int64: closureID, Valid: true},
		Layers: []AuthorityLayerInput{{Kind: "design", ArtifactRowID: sql.NullInt64{Int64: secondArtifact, Valid: true}, ArtifactSHA256: strings.Repeat("c", 64), SourceClosureID: sql.NullInt64{Int64: closureID, Valid: true}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	status, err = service.EvaluateCompletion(ctx, workspace.WorkspaceID)
	if err != nil || status.CurrentDecision != nil {
		t.Fatalf("authority reopening completion state = %#v, err=%v", status, err)
	}
	completed, err = service.Complete(ctx, CompletionInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: status.Workspace.Version, OperatorConfirmed: true})
	if err != nil {
		t.Fatal(err)
	}

	ticketService, err := workflowtickets.NewService(store)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ticketService.Publish(ctx, workflowtickets.PublishInput{
		WorkspaceID: workspace.WorkspaceID, TicketID: "P6-FEATURE", ExternalPriority: 1, ExpectedRevisionNumber: 0,
		Revision: workflowtickets.RevisionInput{
			RepoTarget: "relay", Branch: "main", BaseCommit: strings.Repeat("d", 40), SourceClosureRowID: closureID,
			SourcePath: "tickets/P6-FEATURE.delivery-ticket.json", Goal: "Reopen the completed workspace.", Context: "A new current ticket is unfinished.",
			TransitionApplicability: "required", CanonicalJSON: []byte(`{"ticket":"P6-FEATURE"}`), RenderedMarkdown: []byte("# P6-FEATURE\n"),
			Members: []workflowtickets.RevisionMemberInput{{Kind: "implementation_obligation", Path: "internal/app/features", Text: "Require explicit completion."}},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		_, err := tx.CreateDeliveryTicketSelection(ctx, workflowstore.CreateDeliveryTicketSelectionParams{
			SelectionID: "selection-feature-completion", WorkspaceRowID: status.Workspace.ID, State: "active", Rationale: "exercise the integration completion gate",
			SourceClosureRowID: sql.NullInt64{Int64: closureID, Valid: true},
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	status, err = service.EvaluateCompletion(ctx, workspace.WorkspaceID)
	if err != nil || status.CurrentDecision != nil || completionGateReady(status, "tickets") || completionGateReady(status, "integration") || completionGateReady(status, "transitions") || completionGateReady(status, "audit") {
		t.Fatalf("ticket reopening completion state = %#v, err=%v", status, err)
	}
}

type featureTestIDs struct{ next int }

func (ids *featureTestIDs) AuthorityRevisionID() string {
	ids.next++
	return "authority-feature-" + string(rune('0'+ids.next))
}

func (ids *featureTestIDs) CompletionDecisionID() string {
	ids.next++
	return "completion-feature-" + string(rune('0'+ids.next))
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

func completionGateReady(status CompletionStatus, name string) bool {
	for _, gate := range status.Gates {
		if gate.Name == name {
			return gate.Ready
		}
	}
	return false
}
