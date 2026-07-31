package workflowruns

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	workflowstore "relay/internal/store/workflow"
)

// AdmitPackageExecution verifies that a package-linked Run is eligible for
// execution without advancing its Run lifecycle or creating any
// execution-owned rows.
func (s *Service) AdmitPackageExecution(ctx context.Context, runID string) (workflowstore.Run, error) {
	if s == nil || s.store == nil {
		return workflowstore.Run{}, fmt.Errorf("%w: workflow service and store are required", ErrInvalidRunInput)
	}
	if runID == "" || strings.TrimSpace(runID) != runID {
		return workflowstore.Run{}, fmt.Errorf("%w: Run ID must be nonblank and have no outer whitespace", ErrInvalidRunInput)
	}

	var admitted workflowstore.Run
	err := s.store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		run, err := tx.GetRunByRunID(ctx, runID)
		if err != nil {
			return fmt.Errorf("load package Run: %w", err)
		}
		if !run.ExecutionPackageRowID.Valid {
			return fmt.Errorf("package Run admission requires an execution package")
		}
		switch run.Status {
		case workflowstore.RunStatusSetupReady, workflowstore.RunStatusExecuting:
		default:
			return fmt.Errorf("package Run admission requires setup_ready or executing Run, got %q", run.Status)
		}

		admitted = run
		return nil
	})
	if err != nil {
		return workflowstore.Run{}, err
	}
	return admitted, nil
}

// CompletePackageDeterministicExecution finalizes a deterministically complete
// package Run without creating any execution-owned rows or touching the
// repository. The executor is responsible for proving deterministic
// completion before calling this lifecycle operation.
func (s *Service) CompletePackageDeterministicExecution(ctx context.Context, runID string) (workflowstore.Run, error) {
	if s == nil || s.store == nil {
		return workflowstore.Run{}, fmt.Errorf("%w: workflow service and store are required", ErrInvalidRunInput)
	}
	if runID == "" || strings.TrimSpace(runID) != runID {
		return workflowstore.Run{}, fmt.Errorf("%w: Run ID must be nonblank and have no outer whitespace", ErrInvalidRunInput)
	}

	var finalized workflowstore.Run
	err := s.store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		run, err := tx.GetRunByRunID(ctx, runID)
		if err != nil {
			return fmt.Errorf("load package Run: %w", err)
		}
		if !run.ExecutionPackageRowID.Valid {
			return fmt.Errorf("deterministic package finalization requires an execution package")
		}

		attempts, err := tx.ListExecutionAttemptsByRun(ctx, run.ID)
		if err != nil {
			return fmt.Errorf("list package Run execution attempts: %w", err)
		}
		if len(attempts) != 0 {
			return fmt.Errorf("deterministic package finalization requires zero execution attempts")
		}

		if _, err := tx.GetActiveRepositoryBranchMutationLease(ctx, run.RepoTarget, run.Branch); err == nil {
			return fmt.Errorf("deterministic package finalization requires no active repository/branch mutation lease")
		} else if !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("check active repository/branch mutation lease: %w", err)
		}

		switch run.Status {
		case workflowstore.RunStatusSetupReady:
			run, err = tx.TransitionRun(ctx, run.RunID, workflowstore.RunStatusSetupReady, workflowstore.RunStatusExecuting)
			if err != nil {
				return fmt.Errorf("transition package Run to executing: %w", err)
			}
			run, err = tx.TransitionRun(ctx, run.RunID, workflowstore.RunStatusExecuting, workflowstore.RunStatusValidating)
			if err != nil {
				return fmt.Errorf("transition package Run to validating: %w", err)
			}
			finalized = run
			return nil
		case workflowstore.RunStatusValidating:
			finalized = run
			return nil
		default:
			return fmt.Errorf("deterministic package finalization requires setup_ready or validating Run, got %q", run.Status)
		}
	})
	if err != nil {
		return workflowstore.Run{}, err
	}
	return finalized, nil
}
