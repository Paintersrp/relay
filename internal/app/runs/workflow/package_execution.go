package workflowruns

import (
	"context"
	"fmt"
	"strings"

	workflowstore "relay/internal/store/workflow"
)

// AdmitPackageExecution crosses the execution boundary for an eligible
// package-linked Run without advancing its Run lifecycle or creating any
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

		if err := tx.AttemptCrossCutoverBoundaryForRun(ctx, run.ID, run.ExecutionPackageRowID); err != nil {
			return fmt.Errorf("cross package execution cutover boundary: %w", err)
		}
		admitted, err = tx.GetRunByRunID(ctx, runID)
		if err != nil {
			return fmt.Errorf("reload admitted package Run: %w", err)
		}
		return nil
	})
	if err != nil {
		return workflowstore.Run{}, err
	}
	return admitted, nil
}
