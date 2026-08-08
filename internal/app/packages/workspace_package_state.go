package packages

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"relay/internal/guidedapp"
)

// ReadWorkspacePackageState is the packages-owner semantic read consumed by
// the guided journey projection. It resolves the current execution package
// server-side from the workspace's current delivery selection and verifies
// source-backed currentness (workspace, authority, and ready source closure
// bindings) before reporting prepared or approved state. Consumers must not
// reconstruct package state from execution_packages rows.
func (s *Service) ReadWorkspacePackageState(ctx context.Context, workspaceID string) (guidedapp.PackageState, error) {
	if workspaceID == "" || strings.TrimSpace(workspaceID) != workspaceID {
		return guidedapp.PackageState{}, ErrInvalidPackageInput
	}
	workspace, err := s.store.GetFeatureWorkspaceByWorkspaceID(ctx, workspaceID)
	if err != nil {
		return guidedapp.PackageState{}, err
	}
	selections, err := s.store.ListDeliveryTicketSelectionsByWorkspace(ctx, workspace.ID)
	if err != nil {
		return guidedapp.PackageState{}, err
	}
	if len(selections) == 0 {
		return guidedapp.PackageState{State: "none"}, nil
	}
	latest := selections[0]
	for _, selection := range selections {
		if selection.ID > latest.ID {
			latest = selection
		}
	}
	pkg, err := s.store.GetExecutionPackageBySelectionRowID(ctx, latest.ID)
	if errors.Is(err, sql.ErrNoRows) {
		return guidedapp.PackageState{State: "none"}, nil
	}
	if err != nil {
		return guidedapp.PackageState{}, err
	}
	if pkg.WorkspaceRowID != workspace.ID {
		return guidedapp.PackageState{State: "none"}, nil
	}
	// Source-backed currentness: the package must be bound to the workspace's
	// current authority and that authority's ready source closure. After a
	// reopen or authority replacement the old package is not current.
	if !workspace.CurrentAuthorityRevisionRowID.Valid || pkg.AuthorityRevisionRowID != workspace.CurrentAuthorityRevisionRowID.Int64 {
		return guidedapp.PackageState{State: "none"}, nil
	}
	authority, err := s.store.GetFeatureWorkspaceAuthorityRevisionByRowID(ctx, workspace.CurrentAuthorityRevisionRowID.Int64)
	if err != nil {
		return guidedapp.PackageState{}, err
	}
	if authority.WorkspaceRowID != workspace.ID || !authority.SourceClosureRowID.Valid || authority.SourceClosureRowID.Int64 != pkg.SourceClosureRowID {
		return guidedapp.PackageState{State: "none"}, nil
	}
	result := guidedapp.PackageState{PackageID: pkg.PackageID, PackageSHA256: pkg.PackageSha256, State: "prepared"}
	approval, approvalErr := s.store.GetExecutionPackageApprovalByPackageRowID(ctx, pkg.ID)
	run, runErr := s.store.GetRunByExecutionPackageRowID(ctx, pkg.ID)
	switch {
	case approvalErr == nil && runErr == nil && run.PackageApprovalRowID.Valid && run.PackageApprovalRowID.Int64 == approval.ID:
		result.State = "approved"
		result.RunID = run.RunID
		result.RunStatus = run.Status
		result.RunRepoTarget = run.RepoTarget
		result.RunBranch = run.Branch
		result.RunBaseCommit = run.BaseCommit
	case errors.Is(approvalErr, sql.ErrNoRows) && errors.Is(runErr, sql.ErrNoRows):
		// prepared
	default:
		if approvalErr != nil && !errors.Is(approvalErr, sql.ErrNoRows) {
			return guidedapp.PackageState{}, approvalErr
		}
		if runErr != nil && !errors.Is(runErr, sql.ErrNoRows) {
			return guidedapp.PackageState{}, runErr
		}
		return guidedapp.PackageState{}, ErrPackageBasisChanged
	}
	return result, nil
}

// ApproveCurrentPackage resolves the current prepared execution package
// server-side and approves it with the exact package digest. It is the
// packages-owner implementation of the guided approve action; no package
// identity or digest is accepted from the guided boundary.
func (s *Service) ApproveCurrentPackage(ctx context.Context, in guidedapp.ApprovePackageInput) error {
	if in.WorkspaceID == "" || strings.TrimSpace(in.WorkspaceID) != in.WorkspaceID || strings.TrimSpace(in.Evidence) == "" {
		return ErrInvalidPackageInput
	}
	state, err := s.ReadWorkspacePackageState(ctx, in.WorkspaceID)
	if err != nil {
		return err
	}
	if state.State != "prepared" || state.PackageID == "" || state.PackageSHA256 == "" {
		return ErrPackageBasisChanged
	}
	_, err = s.Approve(ctx, ApproveInput{PackageID: state.PackageID, ExpectedPackageSha256: state.PackageSHA256, OperatorConfirmationEvidence: in.Evidence})
	return err
}
