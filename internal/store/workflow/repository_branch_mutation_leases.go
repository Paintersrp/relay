package workflowstore

import (
	"context"

	workflowgenerated "relay/internal/store/workflowgenerated"
)

const (
	RepositoryBranchMutationLeaseStateActive   = "active"
	RepositoryBranchMutationLeaseStateReleased = "released"

	RepositoryBranchMutationLeaseCertaintyCertain   = "certain"
	RepositoryBranchMutationLeaseCertaintyUncertain = "uncertain"

	RepositoryBranchMutationLeaseReconciliationNotRequired = "not_required"
	RepositoryBranchMutationLeaseReconciliationRequired    = "required"
	RepositoryBranchMutationLeaseReconciliationInProgress  = "in_progress"
	RepositoryBranchMutationLeaseReconciliationReconciled  = "reconciled"
	RepositoryBranchMutationLeaseReconciliationFailed      = "failed"
)

// RepositoryBranchMutationLease is the durable, repository-and-branch scoped
// exclusion record. It is intentionally exposed through the store boundary so
// execution, later packet, and cutover owners share one lifecycle.
type (
	RepositoryBranchMutationLease = workflowgenerated.RepositoryBranchMutationLease

	CreateRepositoryBranchMutationLeaseParams      = workflowgenerated.CreateRepositoryBranchMutationLeaseParams
	UpdateRepositoryBranchMutationLeaseFactsParams = workflowgenerated.UpdateRepositoryBranchMutationLeaseFactsParams
)

func (s *Store) GetRepositoryBranchMutationLeaseByLeaseID(ctx context.Context, leaseID string) (RepositoryBranchMutationLease, error) {
	return workflowgenerated.New(s.db).GetRepositoryBranchMutationLeaseByLeaseID(ctx, leaseID)
}

func (s *Store) GetActiveRepositoryBranchMutationLease(ctx context.Context, repoTarget, branch string) (RepositoryBranchMutationLease, error) {
	return workflowgenerated.New(s.db).GetActiveRepositoryBranchMutationLease(ctx, workflowgenerated.GetActiveRepositoryBranchMutationLeaseParams{
		RepoTarget: repoTarget,
		Branch:     branch,
	})
}

func (s *Store) ListRepositoryBranchMutationLeases(ctx context.Context, repoTarget, branch string) ([]RepositoryBranchMutationLease, error) {
	return workflowgenerated.New(s.db).ListRepositoryBranchMutationLeases(ctx, workflowgenerated.ListRepositoryBranchMutationLeasesParams{
		RepoTarget: repoTarget,
		Branch:     branch,
	})
}

func (tx *Tx) GetRepositoryBranchMutationLeaseByLeaseID(ctx context.Context, leaseID string) (RepositoryBranchMutationLease, error) {
	return workflowgenerated.New(tx.tx).GetRepositoryBranchMutationLeaseByLeaseID(ctx, leaseID)
}

func (tx *Tx) GetActiveRepositoryBranchMutationLease(ctx context.Context, repoTarget, branch string) (RepositoryBranchMutationLease, error) {
	return workflowgenerated.New(tx.tx).GetActiveRepositoryBranchMutationLease(ctx, workflowgenerated.GetActiveRepositoryBranchMutationLeaseParams{
		RepoTarget: repoTarget,
		Branch:     branch,
	})
}

func (tx *Tx) CreateRepositoryBranchMutationLease(ctx context.Context, params CreateRepositoryBranchMutationLeaseParams) (RepositoryBranchMutationLease, error) {
	return workflowgenerated.New(tx.tx).CreateRepositoryBranchMutationLease(ctx, params)
}

func (tx *Tx) UpdateRepositoryBranchMutationLeaseFacts(ctx context.Context, params UpdateRepositoryBranchMutationLeaseFactsParams) (RepositoryBranchMutationLease, error) {
	return workflowgenerated.New(tx.tx).UpdateRepositoryBranchMutationLeaseFacts(ctx, params)
}

func (tx *Tx) ReleaseRepositoryBranchMutationLease(ctx context.Context, leaseID string) (RepositoryBranchMutationLease, error) {
	return workflowgenerated.New(tx.tx).ReleaseRepositoryBranchMutationLease(ctx, leaseID)
}
