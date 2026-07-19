package workflowstore

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
)

func TestApprovalPersistenceKeepsImmutableRecordsAndRejectsMismatchedPublish(t *testing.T) {
	ctx := context.Background()
	store, artifactID := openApprovalTestStore(t, ctx)

	approvalID := NewGoverningArtifactApprovalID()
	var workspace FeatureWorkspace
	var approval GoverningArtifactApproval
	sha := strings.Repeat("b", 64)

	if err := store.WithTx(ctx, func(tx *Tx) error {
		project, err := tx.GetProjectByProjectID(ctx, "project-approval-test")
		if err != nil {
			return err
		}
		workspace, err = tx.CreateFeatureWorkspace(ctx, CreateFeatureWorkspaceParams{
			WorkspaceID:  "workspace-approval-test",
			ProjectRowID: project.ID,
			FeatureSlug:  "approval-test",
		})
		if err != nil {
			return err
		}
		approval, err = tx.CreateGoverningArtifactApproval(ctx, CreateGoverningArtifactApprovalParams{
			ApprovalID:                   approvalID,
			WorkspaceRowID:               workspace.ID,
			ArtifactRowID:                sql.NullInt64{Int64: artifactID, Valid: true},
			Family:                       "requirements",
			ArtifactSha256:               sha,
			OperatorConfirmationEvidence: "operator confirmed",
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}

	if approval.ApprovalID != approvalID || approval.WorkspaceRowID != workspace.ID || approval.Family != "requirements" || approval.ArtifactSha256 != sha {
		t.Fatalf("created approval = %#v", approval)
	}

	read, err := store.GetGoverningArtifactApprovalByApprovalID(ctx, approvalID)
	if err != nil || read.ID != approval.ID || read.OperatorConfirmationEvidence != "operator confirmed" {
		t.Fatalf("read approval = %#v, %v", read, err)
	}

	if _, err := store.DB().Exec(`UPDATE governing_artifact_approvals SET family = 'design' WHERE id = ?`, approval.ID); err == nil {
		t.Fatal("approval record was mutable")
	}
	if _, err := store.DB().Exec(`DELETE FROM governing_artifact_approvals WHERE id = ?`, approval.ID); err == nil {
		t.Fatal("approval record was deletable")
	}

	revisionID := NewFeatureWorkspaceAuthorityRevisionID()
	if err := store.WithTx(ctx, func(tx *Tx) error {
		revision, txErr := tx.CreateFeatureWorkspaceAuthorityRevision(ctx, CreateFeatureWorkspaceAuthorityRevisionParams{
			AuthorityRevisionID: revisionID,
			WorkspaceRowID:      workspace.ID,
			RevisionNumber:      1,
		})
		if txErr != nil {
			return txErr
		}
		_, txErr = tx.CreateFeatureWorkspaceAuthorityLayer(ctx, CreateFeatureWorkspaceAuthorityLayerParams{
			AuthorityRevisionRowID: revision.ID,
			LayerKind:              "requirements",
			Sequence:               1,
			ArtifactRowID:          sql.NullInt64{Int64: artifactID, Valid: true},
			ArtifactSha256:         sha,
			ApprovalRowID:          sql.NullInt64{Int64: approval.ID, Valid: true},
		})
		return txErr
	}); err != nil {
		t.Fatal(err)
	}

	wrongSHA := strings.Repeat("c", 64)
	if err := store.WithTx(ctx, func(tx *Tx) error {
		revision, txErr := tx.CreateFeatureWorkspaceAuthorityRevision(ctx, CreateFeatureWorkspaceAuthorityRevisionParams{
			AuthorityRevisionID: NewFeatureWorkspaceAuthorityRevisionID(),
			WorkspaceRowID:      workspace.ID,
			RevisionNumber:      2,
		})
		if txErr != nil {
			return txErr
		}
		_, txErr = tx.CreateFeatureWorkspaceAuthorityLayer(ctx, CreateFeatureWorkspaceAuthorityLayerParams{
			AuthorityRevisionRowID: revision.ID,
			LayerKind:              "requirements",
			Sequence:               1,
			ArtifactRowID:          sql.NullInt64{Int64: artifactID, Valid: true},
			ArtifactSha256:         wrongSHA,
			ApprovalRowID:          sql.NullInt64{Int64: approval.ID, Valid: true},
		})
		return txErr
	}); err == nil {
		t.Fatal("mismatched SHA-256 layer was accepted with approval trigger guard")
	}

	wrongWorkspaceID := "workspace-approval-wrong"
	var wrongWorkspace FeatureWorkspace
	if err := store.WithTx(ctx, func(tx *Tx) error {
		project, err := tx.GetProjectByProjectID(ctx, "project-approval-test")
		if err != nil {
			return err
		}
		wrongWorkspace, err = tx.CreateFeatureWorkspace(ctx, CreateFeatureWorkspaceParams{
			WorkspaceID:  wrongWorkspaceID,
			ProjectRowID: project.ID,
			FeatureSlug:  "wrong-approval",
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.WithTx(ctx, func(tx *Tx) error {
		revision, txErr := tx.CreateFeatureWorkspaceAuthorityRevision(ctx, CreateFeatureWorkspaceAuthorityRevisionParams{
			AuthorityRevisionID: NewFeatureWorkspaceAuthorityRevisionID(),
			WorkspaceRowID:      wrongWorkspace.ID,
			RevisionNumber:      1,
		})
		if txErr != nil {
			return txErr
		}
		_, txErr = tx.CreateFeatureWorkspaceAuthorityLayer(ctx, CreateFeatureWorkspaceAuthorityLayerParams{
			AuthorityRevisionRowID: revision.ID,
			LayerKind:              "requirements",
			Sequence:               1,
			ArtifactRowID:          sql.NullInt64{Int64: artifactID, Valid: true},
			ArtifactSha256:         sha,
			ApprovalRowID:          sql.NullInt64{Int64: approval.ID, Valid: true},
		})
		return txErr
	}); err == nil {
		t.Fatal("cross-workspace approval was accepted")
	}

	invalidated, err := store.ValidateGoverningArtifactApproval(ctx, approval.ID, workspace.ID, "requirements", sha, sql.NullInt64{Int64: artifactID, Valid: true}, sql.NullInt64{})
	if err != nil || invalidated.ID != approval.ID {
		t.Fatalf("valid approval validation = %#v, %v", invalidated, err)
	}

	mismatch, err := store.ValidateGoverningArtifactApproval(ctx, approval.ID, workspace.ID, "design", sha, sql.NullInt64{Int64: artifactID, Valid: true}, sql.NullInt64{})
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("wrong-family validation = %#v, %v", mismatch, err)
	}

	history, err := store.ListGoverningArtifactApprovalsByWorkspace(ctx, workspace.ID)
	if err != nil || len(history) != 1 || history[0].ID != approval.ID {
		t.Fatalf("approval history = %#v, %v", history, err)
	}

	if err := store.WithTx(ctx, func(tx *Tx) error {
		rollbackApproval := NewGoverningArtifactApprovalID()
		_, txErr := tx.CreateGoverningArtifactApproval(ctx, CreateGoverningArtifactApprovalParams{
			ApprovalID:                   rollbackApproval,
			WorkspaceRowID:               workspace.ID,
			ArtifactRowID:                sql.NullInt64{Int64: artifactID, Valid: true},
			Family:                       "requirements",
			ArtifactSha256:               sha,
			OperatorConfirmationEvidence: "rollback test",
		})
		if txErr != nil {
			return txErr
		}
		return errors.New("intentional rollback")
	}); err == nil {
		t.Fatal("expected rollback error")
	}
	if _, err := store.GetGoverningArtifactApprovalByApprovalID(ctx, "should-not-exist"); err == nil {
		t.Fatal("rolled-back approval persisted")
	}
	history, err = store.ListGoverningArtifactApprovalsByWorkspace(ctx, workspace.ID)
	if err != nil || len(history) != 1 {
		t.Fatalf("approval history after rollback = %#v, %v", history, err)
	}
}

func (s *Store) ValidateGoverningArtifactApproval(ctx context.Context, approvalRowID, workspaceRowID int64, family, sha256 string, artifactRowID, retainedArtifactRowID sql.NullInt64) (GoverningArtifactApproval, error) {
	approval, err := s.GetGoverningArtifactApprovalByRowID(ctx, approvalRowID)
	if err != nil {
		return GoverningArtifactApproval{}, err
	}
	if approval.WorkspaceRowID != workspaceRowID || approval.Family != family || approval.ArtifactSha256 != sha256 {
		return GoverningArtifactApproval{}, sql.ErrNoRows
	}
	if artifactRowID.Valid && (approval.ArtifactRowID.Int64 != artifactRowID.Int64 || !approval.ArtifactRowID.Valid) {
		return GoverningArtifactApproval{}, sql.ErrNoRows
	}
	if retainedArtifactRowID.Valid && (approval.RetainedArtifactRowID.Int64 != retainedArtifactRowID.Int64 || !approval.RetainedArtifactRowID.Valid) {
		return GoverningArtifactApproval{}, sql.ErrNoRows
	}
	if approval.InvalidatedByApprovalRowID.Valid || approval.SupersededByApprovalRowID.Valid {
		return GoverningArtifactApproval{}, sql.ErrNoRows
	}
	return approval, nil
}

func openApprovalTestStore(t *testing.T, ctx context.Context) (*Store, int64) {
	t.Helper()
	store, _ := openWorkflowTestStore(t)
	var projectID, planID, artifactID int64
	sha := strings.Repeat("b", 64)
	if err := store.DB().QueryRowContext(ctx, `
INSERT INTO projects (project_id, name) VALUES ('project-approval-test', 'Approvals') RETURNING id`).Scan(&projectID); err != nil {
		t.Fatal(err)
	}
	if err := store.DB().QueryRowContext(ctx, `
INSERT INTO plans (project_row_id, plan_id, feature_slug, canonical_sha256) VALUES (?, 'plan-approval-test', 'approval-test', ?) RETURNING id`, projectID, strings.Repeat("a", 64)).Scan(&planID); err != nil {
		t.Fatal(err)
	}
	if err := store.DB().QueryRowContext(ctx, `
INSERT INTO artifacts (artifact_id, owner_type, plan_row_id, kind, relative_path, media_type, sha256, size_bytes) VALUES ('artifact-approval-test', 'plan', ?, 'requirements', 'plans/approval-test/requirements.json', 'application/json', ?, 2) RETURNING id`, planID, sha).Scan(&artifactID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().ExecContext(ctx, `INSERT INTO repository_targets (repo_target, local_path, configured_branch_ref, configuration_version) VALUES ('relay', 'C:/relay', 'refs/heads/main', 1)`); err != nil {
		t.Fatal(err)
	}
	return store, artifactID
}

func TestApprovalEvidenceEnforcement(t *testing.T) {
	ctx := context.Background()
	store, artifactID := openApprovalTestStore(t, ctx)

	sha := strings.Repeat("b", 64)
	var workspace FeatureWorkspace
	if err := store.WithTx(ctx, func(tx *Tx) error {
		project, err := tx.GetProjectByProjectID(ctx, "project-approval-test")
		if err != nil {
			return err
		}
		workspace, err = tx.CreateFeatureWorkspace(ctx, CreateFeatureWorkspaceParams{
			WorkspaceID:  "workspace-evidence-test",
			ProjectRowID: project.ID,
			FeatureSlug:  "evidence-test",
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}

	invalidEvidences := []string{"", "   "}
	for _, evidence := range invalidEvidences {
		if err := store.WithTx(ctx, func(tx *Tx) error {
			_, err := tx.CreateGoverningArtifactApproval(ctx, CreateGoverningArtifactApprovalParams{
				ApprovalID:                   NewGoverningArtifactApprovalID(),
				WorkspaceRowID:               workspace.ID,
				ArtifactRowID:                sql.NullInt64{Int64: artifactID, Valid: true},
				Family:                       "requirements",
				ArtifactSha256:               sha,
				OperatorConfirmationEvidence: evidence,
			})
			return err
		}); err == nil {
			t.Fatalf("empty/whitespace evidence %q was accepted", evidence)
		}
	}

	validLengths := []int{1, 4096}
	for _, length := range validLengths {
		evidence := strings.Repeat("x", length)
		var approval GoverningArtifactApproval
		if err := store.WithTx(ctx, func(tx *Tx) error {
			var txErr error
			approval, txErr = tx.CreateGoverningArtifactApproval(ctx, CreateGoverningArtifactApprovalParams{
				ApprovalID:                   NewGoverningArtifactApprovalID(),
				WorkspaceRowID:               workspace.ID,
				ArtifactRowID:                sql.NullInt64{Int64: artifactID, Valid: true},
				Family:                       "requirements",
				ArtifactSha256:               sha,
				OperatorConfirmationEvidence: evidence,
			})
			return txErr
		}); err != nil {
			t.Fatalf("valid evidence length %d was rejected: %v", length, err)
		}
		if approval.OperatorConfirmationEvidence != evidence {
			t.Fatalf("evidence mismatch length %d: %q", length, approval.OperatorConfirmationEvidence)
		}
		valid, err := store.GetValidGoverningArtifactApproval(ctx, GetValidGoverningArtifactApprovalParams{
			WorkspaceRowID:        workspace.ID,
			Family:                "requirements",
			ArtifactSha256:        sha,
			ArtifactRowID:         sql.NullInt64{Int64: artifactID, Valid: true},
			RetainedArtifactRowID: sql.NullInt64{},
		})
		if err != nil || valid.ID != approval.ID {
			t.Fatalf("valid evidence length %d not resolved: %v", length, err)
		}
	}

	tooLong := strings.Repeat("x", 4097)
	if err := store.WithTx(ctx, func(tx *Tx) error {
		_, err := tx.CreateGoverningArtifactApproval(ctx, CreateGoverningArtifactApprovalParams{
			ApprovalID:                   NewGoverningArtifactApprovalID(),
			WorkspaceRowID:               workspace.ID,
			ArtifactRowID:                sql.NullInt64{Int64: artifactID, Valid: true},
			Family:                       "requirements",
			ArtifactSha256:               sha,
			OperatorConfirmationEvidence: tooLong,
		})
		return err
	}); err == nil {
		t.Fatal("4097-length evidence was accepted")
	}

	approved, err := store.GetValidGoverningArtifactApproval(ctx, GetValidGoverningArtifactApprovalParams{
		WorkspaceRowID:        workspace.ID,
		Family:                "requirements",
		ArtifactSha256:        sha,
		ArtifactRowID:         sql.NullInt64{Int64: artifactID, Valid: true},
		RetainedArtifactRowID: sql.NullInt64{},
	})
	if err != nil || approved.OperatorConfirmationEvidence != strings.Repeat("x", 4096) {
		t.Fatalf("most-recent valid evidence resolution = %#v, %v", approved, err)
	}

	rollbackID := NewGoverningArtifactApprovalID()
	if err := store.WithTx(ctx, func(tx *Tx) error {
		_, txErr := tx.CreateGoverningArtifactApproval(ctx, CreateGoverningArtifactApprovalParams{
			ApprovalID:                   rollbackID,
			WorkspaceRowID:               workspace.ID,
			ArtifactRowID:                sql.NullInt64{Int64: artifactID, Valid: true},
			Family:                       "requirements",
			ArtifactSha256:               sha,
			OperatorConfirmationEvidence: "rollback evidence",
		})
		if txErr != nil {
			return txErr
		}
		return errors.New("intentional rollback")
	}); err == nil {
		t.Fatal("expected rollback error")
	}
	if _, err := store.GetGoverningArtifactApprovalByApprovalID(ctx, rollbackID); err == nil {
		t.Fatal("rolled-back evidence approval persisted")
	}
}
