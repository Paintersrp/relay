package executor

import (
	"context"
	"fmt"

	workflowstore "relay/internal/store/workflow"
)

// recordProcessStart supports both the legacy pending attempt and a prepared
// package attempt that was already marked running during admission.
func (s *WorkflowExecutionService) recordProcessStart(ctx context.Context, attemptID, resultJSON string) (workflowstore.ExecutionAttempt, error) {
	attempt, err := s.store.GetExecutionAttemptByAttemptID(ctx, attemptID)
	if err != nil {
		return workflowstore.ExecutionAttempt{}, fmt.Errorf("load execution attempt for process start: %w", err)
	}
	switch attempt.Status {
	case workflowstore.AttemptStatusPending:
		updated, err := s.runs.MarkExecutionAttemptRunning(ctx, attemptID, resultJSON)
		if err != nil {
			return workflowstore.ExecutionAttempt{}, err
		}
		return updated, nil
	case workflowstore.AttemptStatusRunning:
		updated, err := s.runs.UpdateExecutionAttemptResult(ctx, attemptID, resultJSON)
		if err != nil {
			return workflowstore.ExecutionAttempt{}, fmt.Errorf("update running execution attempt process identity: %w", err)
		}
		return updated, nil
	default:
		return workflowstore.ExecutionAttempt{}, fmt.Errorf("execution attempt %q cannot record process start from status %q", attemptID, attempt.Status)
	}
}
