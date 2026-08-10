package executor

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	workflowrepos "relay/internal/repos/workflow"
	workflowstore "relay/internal/store/workflow"
)

type WorkflowMutationLeaseReconcileResult struct {
	Lease     *workflowstore.RepositoryBranchMutationLease
	Released  bool
	Preflight workflowrepos.ExecutionPreflightResult
}

func (s *Execution) acquireRunMutationLease(ctx context.Context, run workflowstore.Run) (workflowstore.RepositoryBranchMutationLease, error) {
	lease, err := s.runs.AcquireRunMutationLease(ctx, run.RunID)
	if err != nil {
		return workflowstore.RepositoryBranchMutationLease{}, fmt.Errorf("acquire repository and branch mutation lease: %w", err)
	}
	return lease, nil
}

func (s *Execution) releaseRunMutationLease(ctx context.Context, run workflowstore.Run, leaseID string) error {
	if strings.TrimSpace(leaseID) == "" {
		return nil
	}
	if _, err := s.runs.ReleaseRunMutationLease(ctx, run.RunID, leaseID); err != nil {
		return fmt.Errorf("release repository and branch mutation lease: %w", err)
	}
	return nil
}

func (s *Execution) retainRunMutationLease(ctx context.Context, run workflowstore.Run, leaseID, reason string) error {
	if strings.TrimSpace(leaseID) == "" {
		return nil
	}
	if _, err := s.runs.MarkRunMutationLeaseUncertain(ctx, run.RunID, leaseID, reason); err != nil {
		return fmt.Errorf("mark repository and branch mutation lease uncertain: %w", err)
	}
	return nil
}

func (s *Execution) reconcileRunMutationLease(
	ctx context.Context,
	run workflowstore.Run,
	repository workflowstore.RepositoryTarget,
	leaseID, reason string,
) (bool, workflowrepos.ExecutionPreflightResult, error) {
	if strings.TrimSpace(leaseID) == "" {
		return true, workflowrepos.ExecutionPreflightResult{}, nil
	}
	if err := s.retainRunMutationLease(ctx, run, leaseID, reason); err != nil {
		return false, workflowrepos.ExecutionPreflightResult{}, err
	}
	if _, err := s.runs.BeginRunMutationLeaseReconciliation(ctx, run.RunID, leaseID, "inspect durable repository state after execution mutation"); err != nil {
		return false, workflowrepos.ExecutionPreflightResult{}, fmt.Errorf("begin repository and branch mutation lease reconciliation: %w", err)
	}
	preflight := s.preflight(ctx, repository.LocalPath, run.Branch, run.BaseCommit)
	if !preflight.OK {
		reason := "repository evidence did not prove a clean mutation outcome"
		note := "repository preflight failed: " + strings.TrimSpace(preflight.BlockerCode)
		if strings.TrimSpace(preflight.BlockerCode) == "" {
			note = "repository preflight did not prove a clean mutation outcome"
		}
		if _, err := s.runs.FailRunMutationLeaseReconciliation(ctx, run.RunID, leaseID, reason, note); err != nil {
			return false, preflight, fmt.Errorf("record failed repository and branch mutation lease reconciliation: %w", err)
		}
		return false, preflight, nil
	}
	if _, err := s.runs.CompleteRunMutationLeaseReconciliation(ctx, run.RunID, leaseID, "repository preflight proved the Run base branch, commit, and worktree are clean"); err != nil {
		return false, preflight, fmt.Errorf("record reconciled repository and branch mutation lease: %w", err)
	}
	if err := s.releaseRunMutationLease(ctx, run, leaseID); err != nil {
		return false, preflight, err
	}
	return true, preflight, nil
}

func (s *Execution) settleRunMutationLeaseAfterTerminalAttempt(
	ctx context.Context,
	run workflowstore.Run,
	repository workflowstore.RepositoryTarget,
	attempt workflowstore.ExecutionAttempt,
	state workflowAttemptRuntime,
	status string,
) error {
	if strings.TrimSpace(state.MutationLeaseID) == "" {
		return nil
	}
	if status == workflowstore.AttemptStatusSucceeded {
		active, err := s.runs.GetActiveRunMutationLease(ctx, run.RunID)
		if err != nil {
			return fmt.Errorf("load successful Run mutation lease: %w", err)
		}
		if active.UncertaintyState != workflowstore.RepositoryBranchMutationLeaseCertaintyCertain ||
			(active.ReconciliationState != workflowstore.RepositoryBranchMutationLeaseReconciliationNotRequired &&
				active.ReconciliationState != workflowstore.RepositoryBranchMutationLeaseReconciliationReconciled) {
			if _, err := s.runs.CompleteRunMutationLeaseReconciliation(
				ctx,
				run.RunID,
				state.MutationLeaseID,
				"verified successful execution reached a terminal state and its process is absent",
			); err != nil {
				return fmt.Errorf("record successful repository and branch mutation lease reconciliation: %w", err)
			}
		}
		return s.releaseRunMutationLease(ctx, run, state.MutationLeaseID)
	}
	if !state.SourceMutationStarted {
		return s.releaseRunMutationLease(ctx, run, state.MutationLeaseID)
	}
	_, _, err := s.reconcileRunMutationLease(
		ctx,
		run,
		repository,
		state.MutationLeaseID,
		"execution attempt ended after source mutation and requires reconciliation",
	)
	if err != nil {
		return err
	}
	_ = attempt
	return nil
}

// ReconcileMutationLease is the durable recovery hook for a Run whose source
// mutation lease survived a process failure or service restart. It preserves an
// active lease until process and repository evidence together prove release is
// safe.
func (s *Execution) ReconcileMutationLease(ctx context.Context, runID string) (WorkflowMutationLeaseReconcileResult, error) {
	run, err := s.store.GetRunByRunID(ctx, strings.TrimSpace(runID))
	if err != nil {
		return WorkflowMutationLeaseReconcileResult{}, err
	}
	lease, err := s.runs.GetActiveRunMutationLease(ctx, run.RunID)
	if err != nil {
		return WorkflowMutationLeaseReconcileResult{}, err
	}
	attempts, err := s.store.ListExecutionAttemptsByRun(ctx, run.ID)
	if err != nil {
		return WorkflowMutationLeaseReconcileResult{}, err
	}
	for _, attempt := range attempts {
		if terminalAttemptStatus(attempt.Status) {
			continue
		}
		if _, err := s.Reconcile(ctx, run.RunID, attempt.AttemptID); err != nil {
			return WorkflowMutationLeaseReconcileResult{Lease: &lease}, err
		}
		current, getErr := s.runs.GetActiveRunMutationLease(ctx, run.RunID)
		if errors.Is(getErr, sql.ErrNoRows) {
			return WorkflowMutationLeaseReconcileResult{Released: true}, nil
		}
		if getErr != nil {
			return WorkflowMutationLeaseReconcileResult{}, getErr
		}
		lease = current
		refreshed, getErr := s.store.GetExecutionAttemptByAttemptID(ctx, attempt.AttemptID)
		if getErr != nil {
			return WorkflowMutationLeaseReconcileResult{}, getErr
		}
		if !terminalAttemptStatus(refreshed.Status) {
			return WorkflowMutationLeaseReconcileResult{Lease: &lease}, nil
		}
	}
	repository, err := s.store.GetRepositoryTarget(ctx, run.RepoTarget)
	if err != nil {
		return WorkflowMutationLeaseReconcileResult{Lease: &lease}, err
	}
	released, preflight, err := s.reconcileRunMutationLease(
		ctx,
		run,
		repository,
		lease.LeaseID,
		"restart reconciliation requires durable repository evidence",
	)
	if err != nil {
		return WorkflowMutationLeaseReconcileResult{Lease: &lease, Preflight: preflight}, err
	}
	if released {
		return WorkflowMutationLeaseReconcileResult{Released: true, Preflight: preflight}, nil
	}
	updated, err := s.runs.GetActiveRunMutationLease(ctx, run.RunID)
	if err != nil {
		return WorkflowMutationLeaseReconcileResult{Preflight: preflight}, err
	}
	return WorkflowMutationLeaseReconcileResult{Lease: &updated, Preflight: preflight}, nil
}
