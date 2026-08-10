package operations

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"relay/internal/app/packages"
	"relay/internal/executor"
	workflowstore "relay/internal/store/workflow"
)

var (
	ErrNoActiveMutationLease = errors.New("no active repository branch mutation lease")
	ErrMutationLeaseConflict = errors.New("repository branch mutation lease does not belong to the supplied Run")
)

func IsPackageWorkflowNotFound(err error) bool {
	return errors.Is(err, sql.ErrNoRows) || errors.Is(err, packages.ErrPackageNotFound)
}

type PackageWorkflowOwner interface {
	Prepare(context.Context, packages.PrepareInput) (packages.PrepareResult, error)
	Approve(context.Context, packages.ApproveInput) (packages.ApproveResult, error)
	Get(context.Context, string) (packages.Detail, error)
}

type MutationLeaseReconciler interface {
	ReconcileMutationLease(context.Context, string) (executor.WorkflowMutationLeaseReconcileResult, error)
}

// MutationLeaseStatus is the bounded read projection shared by API and UI.
type MutationLeaseStatus struct {
	Run   workflowstore.Run
	Lease *workflowstore.RepositoryBranchMutationLease
}

// PackageWorkflowService is one projection layer over the existing package
// owner. Package preparation, approval, and lease reconciliation are direct
// domain operations with no packet admission or parallel lifecycle.
type PackageWorkflowService struct {
	packages   PackageWorkflowOwner
	reconciler MutationLeaseReconciler
	store      *workflowstore.Store
}

func NewPackageWorkflowService(owner PackageWorkflowOwner, reconciler MutationLeaseReconciler, store *workflowstore.Store) (*PackageWorkflowService, error) {
	if owner == nil || reconciler == nil || store == nil {
		return nil, ErrPackageAdmission
	}
	return &PackageWorkflowService{packages: owner, reconciler: reconciler, store: store}, nil
}

func (s *PackageWorkflowService) Prepare(ctx context.Context, input packages.PrepareInput) (PackageDetailView, error) {
	if s == nil || s.packages == nil {
		return PackageDetailView{}, ErrPackageAdmission
	}
	result, err := s.packages.Prepare(ctx, input)
	if err != nil {
		return PackageDetailView{}, err
	}
	view := packageDetailView(packages.Detail{
		Package:                 result.Package,
		Members:                 result.Members,
		Ticket:                  result.Ticket,
		TicketRevision:          result.TicketRevision,
		TicketDocument:          result.TicketDocument,
		DeterministicOperations: result.Operations,
	})
	currentness, err := s.packageCurrentness(ctx, result.Package.WorkspaceRowID)
	if err != nil {
		return PackageDetailView{}, err
	}
	view.Currentness = currentness
	return view, nil
}

func (s *PackageWorkflowService) Approve(ctx context.Context, input packages.ApproveInput) (PackageApprovalView, error) {
	if s == nil || s.packages == nil {
		return PackageApprovalView{}, ErrPackageAdmission
	}
	result, err := s.packages.Approve(ctx, input)
	if err != nil {
		return PackageApprovalView{}, err
	}
	currentness, err := s.packageCurrentness(ctx, result.Package.WorkspaceRowID)
	if err != nil {
		return PackageApprovalView{}, err
	}
	return PackageApprovalView{
		Package:           packageIdentityView(result.Package),
		Run:               runView(result.Run),
		PackageApprovalID: result.PackageApproval.ApprovalID,
		Currentness:       currentness,
	}, nil
}

func (s *PackageWorkflowService) Get(ctx context.Context, packageID string) (PackageDetailView, error) {
	if s == nil || s.packages == nil {
		return PackageDetailView{}, ErrPackageAdmission
	}
	detail, err := s.packages.Get(ctx, packageID)
	if err != nil {
		return PackageDetailView{}, err
	}
	view := packageDetailView(detail)
	currentness, err := s.packageCurrentness(ctx, detail.Package.WorkspaceRowID)
	if err != nil {
		return PackageDetailView{}, err
	}
	view.Currentness = currentness
	return view, nil
}

func (s *PackageWorkflowService) GetMutationLease(ctx context.Context, runID string) (*MutationLeaseView, error) {
	status, err := s.getMutationLease(ctx, runID)
	if err != nil {
		return nil, err
	}
	return mutationLeaseView(status), nil
}

func (s *PackageWorkflowService) getMutationLease(ctx context.Context, runID string) (MutationLeaseStatus, error) {
	if s == nil || s.store == nil || !exactNonBlank(runID) {
		return MutationLeaseStatus{}, ErrPackageAdmission
	}
	run, err := s.store.GetRunByRunID(ctx, runID)
	if err != nil {
		return MutationLeaseStatus{}, err
	}
	status := MutationLeaseStatus{Run: run}
	lease, err := s.store.GetActiveRepositoryBranchMutationLease(ctx, run.RepoTarget, run.Branch)
	if errors.Is(err, sql.ErrNoRows) {
		return status, nil
	}
	if err != nil {
		return MutationLeaseStatus{}, err
	}
	status.Lease = &lease
	return status, nil
}

// ReconcileMutationLease binds reconciliation to the caller's exact active
// lease. Loading by the Run's repository/branch prevents one Run from
// reconciling a foreign or replaced branch lease before the executor runs.
func (s *PackageWorkflowService) ReconcileMutationLease(ctx context.Context, runID, leaseID string) (MutationLeaseReconcileView, error) {
	if s == nil || s.reconciler == nil || !exactNonBlank(runID) || !exactNonBlank(leaseID) {
		return MutationLeaseReconcileView{}, ErrMutationLeaseConflict
	}
	status, err := s.getMutationLease(ctx, runID)
	if err != nil {
		return MutationLeaseReconcileView{}, err
	}
	if status.Lease == nil {
		return MutationLeaseReconcileView{}, ErrNoActiveMutationLease
	}
	lease := status.Lease
	if lease.LeaseID != leaseID || lease.OwnerIdentity != status.Run.RunID ||
		lease.RepoTarget != status.Run.RepoTarget || lease.Branch != status.Run.Branch ||
		lease.State != workflowstore.RepositoryBranchMutationLeaseStateActive {
		return MutationLeaseReconcileView{}, ErrMutationLeaseConflict
	}
	result, err := s.reconciler.ReconcileMutationLease(ctx, status.Run.RunID)
	if err != nil {
		return MutationLeaseReconcileView{}, err
	}
	if result.Released {
		return MutationLeaseReconcileView{Released: true}, nil
	}
	refreshed, err := s.getMutationLease(ctx, status.Run.RunID)
	if err != nil {
		return MutationLeaseReconcileView{}, err
	}
	if refreshed.Lease == nil {
		return MutationLeaseReconcileView{Released: true}, nil
	}
	return MutationLeaseReconcileView{Lease: mutationLeaseView(refreshed)}, nil
}

var _ = strings.EqualFold
