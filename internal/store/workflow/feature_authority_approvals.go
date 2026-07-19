package workflowstore

import (
	"context"
	"database/sql"
)

// ValidateGoverningArtifactApproval checks that an approval is valid for the
// given exact workspace, artifact source, family, and SHA-256. It returns the
// matching approval row or sql.ErrNoRows when no valid approval exists.
func (tx *Tx) ValidateGoverningArtifactApproval(ctx context.Context, approvalRowID, workspaceRowID int64, family, sha256 string, artifactRowID, retainedArtifactRowID sql.NullInt64) (GoverningArtifactApproval, error) {
	approval, err := tx.GetGoverningArtifactApprovalByRowID(ctx, approvalRowID)
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

// GetGoverningArtifactApprovalByRowID resolves an approval by its internal row
// identity.
func (tx *Tx) GetGoverningArtifactApprovalByRowID(ctx context.Context, rowID int64) (GoverningArtifactApproval, error) {
	var value GoverningArtifactApproval
	err := tx.tx.QueryRowContext(ctx, `
SELECT id, approval_id, workspace_row_id, artifact_row_id, retained_artifact_row_id,
       family, artifact_sha256, operator_confirmation_evidence,
       invalidated_by_approval_row_id, superseded_by_approval_row_id, created_at
FROM governing_artifact_approvals
WHERE id = ?`, rowID).Scan(
		&value.ID, &value.ApprovalID, &value.WorkspaceRowID,
		&value.ArtifactRowID, &value.RetainedArtifactRowID,
		&value.Family, &value.ArtifactSha256, &value.OperatorConfirmationEvidence,
		&value.InvalidatedByApprovalRowID, &value.SupersededByApprovalRowID,
		&value.CreatedAt,
	)
	return value, err
}

func (s *Store) GetGoverningArtifactApprovalByRowID(ctx context.Context, rowID int64) (GoverningArtifactApproval, error) {
	var value GoverningArtifactApproval
	err := s.db.QueryRowContext(ctx, `
SELECT id, approval_id, workspace_row_id, artifact_row_id, retained_artifact_row_id,
       family, artifact_sha256, operator_confirmation_evidence,
       invalidated_by_approval_row_id, superseded_by_approval_row_id, created_at
FROM governing_artifact_approvals
WHERE id = ?`, rowID).Scan(
		&value.ID, &value.ApprovalID, &value.WorkspaceRowID,
		&value.ArtifactRowID, &value.RetainedArtifactRowID,
		&value.Family, &value.ArtifactSha256, &value.OperatorConfirmationEvidence,
		&value.InvalidatedByApprovalRowID, &value.SupersededByApprovalRowID,
		&value.CreatedAt,
	)
	return value, err
}
