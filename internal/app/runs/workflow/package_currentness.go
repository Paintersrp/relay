package workflowruns

import (
	"context"
	"fmt"

	featureapp "relay/internal/app/features"
	workflowstore "relay/internal/store/workflow"
)

// recheckPackageCurrentness is the Run-owner boundary for package-linked work.
// It is intentionally transaction-scoped so no attempt, lease, or lifecycle
// transition can commit from a historical package basis.
func recheckPackageCurrentness(ctx context.Context, tx *workflowstore.Tx, run workflowstore.Run) error {
	if !run.ExecutionPackageRowID.Valid || !run.PackageApprovalRowID.Valid {
		return fmt.Errorf("%w: package Run approval linkage is incomplete", ErrInvalidRunInput)
	}
	pkg, err := tx.GetExecutionPackageByRowID(ctx, run.ExecutionPackageRowID.Int64)
	if err != nil || pkg.ID != run.ExecutionPackageRowID.Int64 || pkg.RepoTarget != run.RepoTarget || pkg.Branch != run.Branch || pkg.BaseCommit != run.BaseCommit {
		return fmt.Errorf("%w: package identity is stale", ErrInvalidRunInput)
	}
	workspace, err := tx.GetFeatureWorkspaceByRowID(ctx, pkg.WorkspaceRowID)
	if err != nil {
		return fmt.Errorf("%w: package workspace is unavailable", ErrInvalidRunInput)
	}
	currentness, err := featureapp.EvaluateCurrentness(ctx, tx, workspace.WorkspaceID)
	if err != nil || currentness.Readiness != featureapp.FeatureCurrent || currentness.WorkspaceVersion != workspace.Version || !currentness.AuthorityRevisionRowID.Valid || currentness.AuthorityRevisionRowID.Int64 != pkg.AuthorityRevisionRowID {
		return fmt.Errorf("%w: Feature currentness is stale", ErrInvalidRunInput)
	}
	authority, err := tx.GetFeatureWorkspaceAuthorityRevisionByRowID(ctx, pkg.AuthorityRevisionRowID)
	if err != nil || authority.WorkspaceRowID != workspace.ID || !authority.SourceClosureRowID.Valid || authority.SourceClosureRowID.Int64 != pkg.SourceClosureRowID {
		return fmt.Errorf("%w: package authority is stale", ErrInvalidRunInput)
	}
	closure, err := tx.GetSourceVaultClosureByRowID(ctx, pkg.SourceClosureRowID)
	if err != nil || closure.State != workflowstore.SourceVaultClosureStateReady || closure.CommitOID != pkg.BaseCommit {
		return fmt.Errorf("%w: package source closure is stale", ErrInvalidRunInput)
	}
	approval, err := tx.GetRunExecutionPackageApproval(ctx, run.ID)
	if err != nil || approval.ID != run.PackageApprovalRowID.Int64 || approval.PackageRowID != pkg.ID || approval.PackageSha256 != pkg.PackageSha256 {
		return fmt.Errorf("%w: package approval is stale", ErrInvalidRunInput)
	}
	selection, err := tx.GetDeliveryTicketSelectionByRowID(ctx, pkg.SelectionRowID)
	if err != nil || selection.State != "consumed" || !selection.SourceClosureRowID.Valid || selection.SourceClosureRowID.Int64 != pkg.SourceClosureRowID {
		return fmt.Errorf("%w: package selection is stale", ErrInvalidRunInput)
	}
	selectionMembers, err := tx.ListDeliveryTicketSelectionMembers(ctx, selection.ID)
	packageMembers, memberErr := tx.ListExecutionPackageMembers(ctx, pkg.ID)
	if err != nil || memberErr != nil || len(selectionMembers) != len(packageMembers) || len(selectionMembers) == 0 {
		return fmt.Errorf("%w: package member closure is stale", ErrInvalidRunInput)
	}
	for _, packageMember := range packageMembers {
		var selected workflowstore.DeliveryTicketSelectionMember
		found := false
		for _, candidate := range selectionMembers {
			if candidate.ID == packageMember.SelectionMemberRowID && candidate.RevisionRowID == packageMember.RevisionRowID {
				selected, found = candidate, true
				break
			}
		}
		if !found || selected.ApprovalRowID < 1 {
			return fmt.Errorf("%w: package member selection identity is stale", ErrInvalidRunInput)
		}
		revision, err := tx.GetDeliveryTicketRevisionByRowID(ctx, packageMember.RevisionRowID)
		if err != nil || revision.CancellationReason.Valid || revision.SourceClosureRowID != pkg.SourceClosureRowID || revision.RepoTarget != pkg.RepoTarget || revision.Branch != pkg.Branch || revision.BaseCommit != pkg.BaseCommit {
			return fmt.Errorf("%w: Delivery Ticket revision is stale", ErrInvalidRunInput)
		}
		ticket, err := tx.GetDeliveryTicketByRowID(ctx, revision.DeliveryTicketRowID)
		if err != nil || ticket.WorkspaceRowID != workspace.ID || !ticket.CurrentRevisionRowID.Valid || ticket.CurrentRevisionRowID.Int64 != revision.ID {
			return fmt.Errorf("%w: Delivery Ticket revision is stale", ErrInvalidRunInput)
		}
		ticketApproval, err := tx.GetDeliveryTicketRevisionApprovalByRowID(ctx, selected.ApprovalRowID)
		if err != nil || ticketApproval.RevisionRowID != revision.ID || ticketApproval.ApprovalKind != "delivery" || ticketApproval.ApprovalState != "approved" || ticketApproval.SourceClosureRowID != closure.ID || !ticketApproval.AuthorityRevisionRowID.Valid || ticketApproval.AuthorityRevisionRowID.Int64 != authority.ID {
			return fmt.Errorf("%w: Delivery Ticket approval is stale", ErrInvalidRunInput)
		}
	}
	return nil
}
