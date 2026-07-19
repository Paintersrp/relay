package workflowstore

import (
	"context"
	"database/sql"

	workflowgenerated "relay/internal/store/workflowgenerated"
)

// Execution package rows are the durable package-to-Run association used by
// ticket-oriented execution. Generated query types remain behind the store
// boundary just like the delivery-ticket surface.
type (
	ExecutionPackage                = workflowgenerated.ExecutionPackage
	ExecutionPackageMember          = workflowgenerated.ExecutionPackageMember
	ExecutionPackageApprovalBinding = workflowgenerated.ExecutionPackageApprovalBinding
	ExecutionPackageApproval        = workflowgenerated.ExecutionPackageApproval

	CreateExecutionPackageParams                = workflowgenerated.CreateExecutionPackageParams
	CreateExecutionPackageMemberParams          = workflowgenerated.CreateExecutionPackageMemberParams
	CreateExecutionPackageApprovalBindingParams = workflowgenerated.CreateExecutionPackageApprovalBindingParams
	CreateExecutionPackageApprovalParams        = workflowgenerated.CreateExecutionPackageApprovalParams
	LinkRunToExecutionPackageApprovalParams     = workflowgenerated.LinkRunToExecutionPackageApprovalParams
)

func (s *Store) GetExecutionPackageByPackageID(ctx context.Context, packageID string) (ExecutionPackage, error) {
	return workflowgenerated.New(s.db).GetExecutionPackageByPackageID(ctx, packageID)
}

func (s *Store) GetExecutionPackageBySelectionRowID(ctx context.Context, selectionRowID int64) (ExecutionPackage, error) {
	return workflowgenerated.New(s.db).GetExecutionPackageBySelectionRowID(ctx, selectionRowID)
}

func (s *Store) GetExecutionPackageByRowID(ctx context.Context, packageRowID int64) (ExecutionPackage, error) {
	return getExecutionPackageByRowID(ctx, s.db, packageRowID)
}

func (s *Store) ListExecutionPackagesByWorkspace(ctx context.Context, workspaceRowID int64) ([]ExecutionPackage, error) {
	return workflowgenerated.New(s.db).ListExecutionPackagesByWorkspace(ctx, workspaceRowID)
}

func (s *Store) ListExecutionPackageMembers(ctx context.Context, packageRowID int64) ([]ExecutionPackageMember, error) {
	return workflowgenerated.New(s.db).ListExecutionPackageMembers(ctx, packageRowID)
}

func (s *Store) ListExecutionPackageApprovalBindings(ctx context.Context, packageRowID int64) ([]ExecutionPackageApprovalBinding, error) {
	return workflowgenerated.New(s.db).ListExecutionPackageApprovalBindings(ctx, packageRowID)
}

func (s *Store) GetRunByExecutionPackageRowID(ctx context.Context, packageRowID int64) (Run, error) {
	return getRunByExecutionPackageRowID(ctx, s.db, packageRowID)
}

func (s *Store) GetExecutionPackageApprovalByPackageRowID(ctx context.Context, packageRowID int64) (ExecutionPackageApproval, error) {
	return workflowgenerated.New(s.db).GetExecutionPackageApprovalByPackageRowID(ctx, packageRowID)
}

func (s *Store) GetExecutionPackageApprovalByApprovalID(ctx context.Context, approvalID string) (ExecutionPackageApproval, error) {
	return workflowgenerated.New(s.db).GetExecutionPackageApprovalByApprovalID(ctx, approvalID)
}

func (tx *Tx) GetExecutionPackageByPackageID(ctx context.Context, packageID string) (ExecutionPackage, error) {
	return workflowgenerated.New(tx.tx).GetExecutionPackageByPackageID(ctx, packageID)
}

func (tx *Tx) GetExecutionPackageBySelectionRowID(ctx context.Context, selectionRowID int64) (ExecutionPackage, error) {
	return workflowgenerated.New(tx.tx).GetExecutionPackageBySelectionRowID(ctx, selectionRowID)
}

func (tx *Tx) GetExecutionPackageByRowID(ctx context.Context, packageRowID int64) (ExecutionPackage, error) {
	return getExecutionPackageByRowID(ctx, tx.tx, packageRowID)
}

func (tx *Tx) CreateExecutionPackage(ctx context.Context, params CreateExecutionPackageParams) (ExecutionPackage, error) {
	return workflowgenerated.New(tx.tx).CreateExecutionPackage(ctx, params)
}

func (tx *Tx) ListExecutionPackageMembers(ctx context.Context, packageRowID int64) ([]ExecutionPackageMember, error) {
	return workflowgenerated.New(tx.tx).ListExecutionPackageMembers(ctx, packageRowID)
}

func (tx *Tx) CreateExecutionPackageMember(ctx context.Context, params CreateExecutionPackageMemberParams) (ExecutionPackageMember, error) {
	return workflowgenerated.New(tx.tx).CreateExecutionPackageMember(ctx, params)
}

func (tx *Tx) ListExecutionPackageApprovalBindings(ctx context.Context, packageRowID int64) ([]ExecutionPackageApprovalBinding, error) {
	return workflowgenerated.New(tx.tx).ListExecutionPackageApprovalBindings(ctx, packageRowID)
}

func (tx *Tx) CreateExecutionPackageApprovalBinding(ctx context.Context, params CreateExecutionPackageApprovalBindingParams) (ExecutionPackageApprovalBinding, error) {
	return workflowgenerated.New(tx.tx).CreateExecutionPackageApprovalBinding(ctx, params)
}

func (tx *Tx) ConsumeDeliveryTicketSelection(ctx context.Context, selectionID string) (DeliveryTicketSelection, error) {
	return workflowgenerated.New(tx.tx).ConsumeDeliveryTicketSelection(ctx, selectionID)
}

func (tx *Tx) CreateExecutionPackageApproval(ctx context.Context, params CreateExecutionPackageApprovalParams) (ExecutionPackageApproval, error) {
	return workflowgenerated.New(tx.tx).CreateExecutionPackageApproval(ctx, params)
}

func (tx *Tx) GetExecutionPackageApprovalByPackageRowID(ctx context.Context, packageRowID int64) (ExecutionPackageApproval, error) {
	return workflowgenerated.New(tx.tx).GetExecutionPackageApprovalByPackageRowID(ctx, packageRowID)
}

func (tx *Tx) LinkRunToExecutionPackageApproval(ctx context.Context, params LinkRunToExecutionPackageApprovalParams) (Run, error) {
	value, err := workflowgenerated.New(tx.tx).LinkRunToExecutionPackageApproval(ctx, params)
	return Run{
		ID:                    value.ID,
		RunID:                 value.RunID,
		FeatureSlug:           value.FeatureSlug,
		RepoTarget:            value.RepoTarget,
		PlanRowID:             value.PlanRowID,
		PlanPassRowID:         value.PlanPassRowID,
		RemediatesRunRowID:    value.RemediatesRunRowID,
		Status:                value.Status,
		Branch:                value.Branch,
		BaseCommit:            value.BaseCommit,
		CanonicalSHA256:       value.CanonicalSha256,
		CreatedAt:             value.CreatedAt,
		UpdatedAt:             value.UpdatedAt,
		CompletedAt:           value.CompletedAt,
		ExecutionPackageRowID: value.ExecutionPackageRowID,
		PackageApprovalRowID:  value.PackageApprovalRowID,
	}, err
}

func (tx *Tx) LinkRunToExecutionPackage(ctx context.Context, runID string, packageRowID int64) (Run, error) {
	value, err := workflowgenerated.New(tx.tx).LinkRunToExecutionPackage(ctx, workflowgenerated.LinkRunToExecutionPackageParams{
		ExecutionPackageRowID: sql.NullInt64{Int64: packageRowID, Valid: true},
		RunID:                 runID,
	})
	return Run{
		ID:                    value.ID,
		RunID:                 value.RunID,
		FeatureSlug:           value.FeatureSlug,
		RepoTarget:            value.RepoTarget,
		PlanRowID:             value.PlanRowID,
		PlanPassRowID:         value.PlanPassRowID,
		RemediatesRunRowID:    value.RemediatesRunRowID,
		Status:                value.Status,
		Branch:                value.Branch,
		BaseCommit:            value.BaseCommit,
		CanonicalSHA256:       value.CanonicalSha256,
		CreatedAt:             value.CreatedAt,
		UpdatedAt:             value.UpdatedAt,
		CompletedAt:           value.CompletedAt,
		ExecutionPackageRowID: value.ExecutionPackageRowID,
		PackageApprovalRowID:  value.PackageApprovalRowID,
	}, err
}

func (tx *Tx) GetRunExecutionPackageApproval(ctx context.Context, runRowID int64) (ExecutionPackageApproval, error) {
	return workflowgenerated.New(tx.tx).GetRunExecutionPackageApproval(ctx, runRowID)
}

func (s *Store) GetRunExecutionPackageApproval(ctx context.Context, runRowID int64) (ExecutionPackageApproval, error) {
	return workflowgenerated.New(s.db).GetRunExecutionPackageApproval(ctx, runRowID)
}

type runExecutionPackageQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func getExecutionPackageByRowID(ctx context.Context, queryer runExecutionPackageQueryer, packageRowID int64) (ExecutionPackage, error) {
	var value ExecutionPackage
	err := queryer.QueryRowContext(ctx, `
SELECT id, package_id, selection_row_id, workspace_row_id, repo_target, branch, base_commit,
       source_closure_row_id, authority_revision_row_id, package_sha256, authority_sha256,
       source_sha256, design_brief_sha256, execution_spec_sha256, created_at
FROM execution_packages
WHERE id = ?`, packageRowID).Scan(
		&value.ID, &value.PackageID, &value.SelectionRowID, &value.WorkspaceRowID,
		&value.RepoTarget, &value.Branch, &value.BaseCommit, &value.SourceClosureRowID,
		&value.AuthorityRevisionRowID, &value.PackageSha256, &value.AuthoritySha256,
		&value.SourceSha256, &value.DesignBriefSha256, &value.ExecutionSpecSha256, &value.CreatedAt,
	)
	return value, err
}

func getRunByExecutionPackageRowID(ctx context.Context, queryer runExecutionPackageQueryer, packageRowID int64) (Run, error) {
	var value Run
	err := queryer.QueryRowContext(ctx, `
SELECT id, run_id, feature_slug, repo_target, plan_row_id, plan_pass_row_id, remediates_run_row_id,
       status, branch, base_commit, canonical_sha256, created_at, updated_at, completed_at,
       execution_package_row_id, package_approval_row_id
FROM runs
WHERE execution_package_row_id = ?`, packageRowID).Scan(
		&value.ID,
		&value.RunID,
		&value.FeatureSlug,
		&value.RepoTarget,
		&value.PlanRowID,
		&value.PlanPassRowID,
		&value.RemediatesRunRowID,
		&value.Status,
		&value.Branch,
		&value.BaseCommit,
		&value.CanonicalSHA256,
		&value.CreatedAt,
		&value.UpdatedAt,
		&value.CompletedAt,
		&value.ExecutionPackageRowID,
		&value.PackageApprovalRowID,
	)
	return value, err
}
