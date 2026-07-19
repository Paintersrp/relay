package workflowruns

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	workflowstore "relay/internal/store/workflow"
)

var (
	ErrMutationLeaseConflict  = errors.New("repository and branch mutation lease conflict")
	ErrMutationLeaseOwner     = errors.New("repository and branch mutation lease owner mismatch")
	ErrMutationLeaseUncertain = errors.New("repository and branch mutation lease requires reconciliation")
)

const runMutationLeaseOwnerKind = "run_execution"

// AcquireRunMutationLease establishes the one active source-mutation owner for
// a Run's exact repository and branch. It is deliberately independent of
// Plan/pass versus package linkage so eligible historical Runs use the same
// exclusion as ticket-oriented Runs.
func (s *Service) AcquireRunMutationLease(ctx context.Context, runID string) (workflowstore.RepositoryBranchMutationLease, error) {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return workflowstore.RepositoryBranchMutationLease{}, fmt.Errorf("%w: run ID is required", ErrInvalidRunInput)
	}
	run, err := s.store.GetRunByRunID(ctx, runID)
	if err != nil {
		return workflowstore.RepositoryBranchMutationLease{}, fmt.Errorf("load Run mutation lease owner: %w", err)
	}
	var lease workflowstore.RepositoryBranchMutationLease
	err = s.store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		var createErr error
		lease, createErr = tx.CreateRepositoryBranchMutationLease(ctx, workflowstore.CreateRepositoryBranchMutationLeaseParams{
			LeaseID:             workflowstore.NewRepositoryBranchMutationLeaseID(),
			RepoTarget:          run.RepoTarget,
			Branch:              run.Branch,
			OwnerKind:           runMutationLeaseOwnerKind,
			OwnerIdentity:       run.RunID,
			UncertaintyState:    workflowstore.RepositoryBranchMutationLeaseCertaintyCertain,
			ReconciliationState: workflowstore.RepositoryBranchMutationLeaseReconciliationNotRequired,
		})
		if createErr != nil {
			if isMutationLeaseUniqueConflict(createErr) {
				return fmt.Errorf("%w: %s/%s", ErrMutationLeaseConflict, run.RepoTarget, run.Branch)
			}
			return fmt.Errorf("create repository and branch mutation lease: %w", createErr)
		}
		return nil
	})
	return lease, err
}

func (s *Service) GetActiveRunMutationLease(ctx context.Context, runID string) (workflowstore.RepositoryBranchMutationLease, error) {
	run, err := s.store.GetRunByRunID(ctx, strings.TrimSpace(runID))
	if err != nil {
		return workflowstore.RepositoryBranchMutationLease{}, err
	}
	lease, err := s.store.GetActiveRepositoryBranchMutationLease(ctx, run.RepoTarget, run.Branch)
	if err != nil {
		return workflowstore.RepositoryBranchMutationLease{}, err
	}
	if err := validateRunMutationLeaseOwner(run, lease); err != nil {
		return workflowstore.RepositoryBranchMutationLease{}, err
	}
	return lease, nil
}

func (s *Service) MarkRunMutationLeaseUncertain(ctx context.Context, runID, leaseID, reason string) (workflowstore.RepositoryBranchMutationLease, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return workflowstore.RepositoryBranchMutationLease{}, fmt.Errorf("mutation lease uncertainty reason is required")
	}
	return s.updateActiveRunMutationLease(ctx, runID, leaseID, func(_ workflowstore.RepositoryBranchMutationLease) workflowstore.UpdateRepositoryBranchMutationLeaseFactsParams {
		return workflowstore.UpdateRepositoryBranchMutationLeaseFactsParams{
			UncertaintyState:    workflowstore.RepositoryBranchMutationLeaseCertaintyUncertain,
			UncertaintyReason:   sql.NullString{String: reason, Valid: true},
			ReconciliationState: workflowstore.RepositoryBranchMutationLeaseReconciliationRequired,
			ReconciliationNote: sql.NullString{
				String: "execution mutation outcome requires durable reconciliation",
				Valid:  true,
			},
		}
	})
}

func (s *Service) BeginRunMutationLeaseReconciliation(ctx context.Context, runID, leaseID, note string) (workflowstore.RepositoryBranchMutationLease, error) {
	note = strings.TrimSpace(note)
	if note == "" {
		return workflowstore.RepositoryBranchMutationLease{}, fmt.Errorf("mutation lease reconciliation note is required")
	}
	return s.updateActiveRunMutationLease(ctx, runID, leaseID, func(current workflowstore.RepositoryBranchMutationLease) workflowstore.UpdateRepositoryBranchMutationLeaseFactsParams {
		reason := current.UncertaintyReason
		if !reason.Valid || strings.TrimSpace(reason.String) == "" {
			reason = sql.NullString{String: "execution mutation outcome requires durable reconciliation", Valid: true}
		}
		return workflowstore.UpdateRepositoryBranchMutationLeaseFactsParams{
			UncertaintyState:        workflowstore.RepositoryBranchMutationLeaseCertaintyUncertain,
			UncertaintyReason:       reason,
			ReconciliationState:     workflowstore.RepositoryBranchMutationLeaseReconciliationInProgress,
			ReconciliationNote:      sql.NullString{String: note, Valid: true},
			ReconciliationStartedAt: sql.NullString{String: mutationLeaseTimestamp(), Valid: true},
		}
	})
}

func (s *Service) CompleteRunMutationLeaseReconciliation(ctx context.Context, runID, leaseID, note string) (workflowstore.RepositoryBranchMutationLease, error) {
	note = strings.TrimSpace(note)
	if note == "" {
		return workflowstore.RepositoryBranchMutationLease{}, fmt.Errorf("mutation lease reconciliation note is required")
	}
	return s.updateActiveRunMutationLease(ctx, runID, leaseID, func(current workflowstore.RepositoryBranchMutationLease) workflowstore.UpdateRepositoryBranchMutationLeaseFactsParams {
		startedAt := current.ReconciliationStartedAt
		if !startedAt.Valid {
			startedAt = sql.NullString{String: mutationLeaseTimestamp(), Valid: true}
		}
		return workflowstore.UpdateRepositoryBranchMutationLeaseFactsParams{
			UncertaintyState:        workflowstore.RepositoryBranchMutationLeaseCertaintyCertain,
			ReconciliationState:     workflowstore.RepositoryBranchMutationLeaseReconciliationReconciled,
			ReconciliationNote:      sql.NullString{String: note, Valid: true},
			ReconciliationStartedAt: startedAt,
			ReconciledAt:            sql.NullString{String: mutationLeaseTimestamp(), Valid: true},
		}
	})
}

func (s *Service) FailRunMutationLeaseReconciliation(ctx context.Context, runID, leaseID, reason, note string) (workflowstore.RepositoryBranchMutationLease, error) {
	reason = strings.TrimSpace(reason)
	note = strings.TrimSpace(note)
	if reason == "" || note == "" {
		return workflowstore.RepositoryBranchMutationLease{}, fmt.Errorf("mutation lease reconciliation reason and note are required")
	}
	return s.updateActiveRunMutationLease(ctx, runID, leaseID, func(current workflowstore.RepositoryBranchMutationLease) workflowstore.UpdateRepositoryBranchMutationLeaseFactsParams {
		startedAt := current.ReconciliationStartedAt
		if !startedAt.Valid {
			startedAt = sql.NullString{String: mutationLeaseTimestamp(), Valid: true}
		}
		return workflowstore.UpdateRepositoryBranchMutationLeaseFactsParams{
			UncertaintyState:        workflowstore.RepositoryBranchMutationLeaseCertaintyUncertain,
			UncertaintyReason:       sql.NullString{String: reason, Valid: true},
			ReconciliationState:     workflowstore.RepositoryBranchMutationLeaseReconciliationFailed,
			ReconciliationNote:      sql.NullString{String: note, Valid: true},
			ReconciliationStartedAt: startedAt,
			ReconciledAt:            sql.NullString{String: mutationLeaseTimestamp(), Valid: true},
		}
	})
}

func (s *Service) ReleaseRunMutationLease(ctx context.Context, runID, leaseID string) (workflowstore.RepositoryBranchMutationLease, error) {
	runID = strings.TrimSpace(runID)
	leaseID = strings.TrimSpace(leaseID)
	if runID == "" || leaseID == "" {
		return workflowstore.RepositoryBranchMutationLease{}, fmt.Errorf("%w: run and lease IDs are required", ErrInvalidRunInput)
	}
	var released workflowstore.RepositoryBranchMutationLease
	err := s.store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		run, err := tx.GetRunByRunID(ctx, runID)
		if err != nil {
			return err
		}
		lease, err := tx.GetRepositoryBranchMutationLeaseByLeaseID(ctx, leaseID)
		if err != nil {
			return err
		}
		if err := validateRunMutationLeaseOwner(run, lease); err != nil {
			return err
		}
		if lease.State == workflowstore.RepositoryBranchMutationLeaseStateReleased {
			released = lease
			return nil
		}
		if lease.UncertaintyState != workflowstore.RepositoryBranchMutationLeaseCertaintyCertain ||
			(lease.ReconciliationState != workflowstore.RepositoryBranchMutationLeaseReconciliationNotRequired &&
				lease.ReconciliationState != workflowstore.RepositoryBranchMutationLeaseReconciliationReconciled) {
			return fmt.Errorf("%w: state is %s", ErrMutationLeaseUncertain, lease.ReconciliationState)
		}
		released, err = tx.ReleaseRepositoryBranchMutationLease(ctx, leaseID)
		if err != nil {
			return fmt.Errorf("release repository and branch mutation lease: %w", err)
		}
		return nil
	})
	return released, err
}

func (s *Service) updateActiveRunMutationLease(
	ctx context.Context,
	runID, leaseID string,
	build func(workflowstore.RepositoryBranchMutationLease) workflowstore.UpdateRepositoryBranchMutationLeaseFactsParams,
) (workflowstore.RepositoryBranchMutationLease, error) {
	runID = strings.TrimSpace(runID)
	leaseID = strings.TrimSpace(leaseID)
	if runID == "" || leaseID == "" || build == nil {
		return workflowstore.RepositoryBranchMutationLease{}, fmt.Errorf("%w: run ID, lease ID, and update are required", ErrInvalidRunInput)
	}
	var updated workflowstore.RepositoryBranchMutationLease
	err := s.store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		run, err := tx.GetRunByRunID(ctx, runID)
		if err != nil {
			return err
		}
		lease, err := tx.GetRepositoryBranchMutationLeaseByLeaseID(ctx, leaseID)
		if err != nil {
			return err
		}
		if err := validateRunMutationLeaseOwner(run, lease); err != nil {
			return err
		}
		if lease.State != workflowstore.RepositoryBranchMutationLeaseStateActive {
			return fmt.Errorf("repository and branch mutation lease is not active")
		}
		params := build(lease)
		params.LeaseID = lease.LeaseID
		params.State = workflowstore.RepositoryBranchMutationLeaseStateActive
		updated, err = tx.UpdateRepositoryBranchMutationLeaseFacts(ctx, params)
		if err != nil {
			return fmt.Errorf("update repository and branch mutation lease facts: %w", err)
		}
		return nil
	})
	return updated, err
}

func validateRunMutationLeaseOwner(run workflowstore.Run, lease workflowstore.RepositoryBranchMutationLease) error {
	if lease.OwnerKind != runMutationLeaseOwnerKind || lease.OwnerIdentity != run.RunID ||
		!strings.EqualFold(lease.RepoTarget, run.RepoTarget) || lease.Branch != run.Branch {
		return ErrMutationLeaseOwner
	}
	return nil
}

func mutationLeaseTimestamp() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}

func isMutationLeaseUniqueConflict(err error) bool {
	if err == nil {
		return false
	}
	message := err.Error()
	return strings.Contains(message, "idx_repository_branch_mutation_leases_one_active") ||
		strings.Contains(message, "repository_branch_mutation_leases.repo_target, repository_branch_mutation_leases.branch")
}
