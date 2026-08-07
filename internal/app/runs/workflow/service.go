package workflowruns

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	workflowstore "relay/internal/store/workflow"
)

var (
	ErrInvalidRunInput          = errors.New("invalid Run input")
	ErrRepositoryTargetNotFound = errors.New("repository target not found")
)

type IDGenerator interface {
	RunID() string
	ExecutionAttemptID() string
	ArtifactID() string
	AuditDecisionID() string
}

type defaultIDGenerator struct{}

func (defaultIDGenerator) RunID() string              { return workflowstore.NewRunID() }
func (defaultIDGenerator) ExecutionAttemptID() string { return workflowstore.NewExecutionAttemptID() }
func (defaultIDGenerator) ArtifactID() string         { return workflowstore.NewArtifactID() }
func (defaultIDGenerator) AuditDecisionID() string    { return workflowstore.NewAuditDecisionID() }

type Service struct {
	store *workflowstore.Store
	ids   IDGenerator
}

func NewService(store *workflowstore.Store) (*Service, error) {
	return NewServiceWithIDs(store, defaultIDGenerator{})
}

func NewServiceWithIDs(store *workflowstore.Store, ids IDGenerator) (*Service, error) {
	if store == nil {
		return nil, fmt.Errorf("workflow store is required")
	}
	if ids == nil {
		return nil, fmt.Errorf("workflow ID generator is required")
	}
	return &Service{store: store, ids: ids}, nil
}

// CreatePackageRun creates a setup-ready Run for an approved execution package.
// It keeps Plan/pass fields unqualified and lets the package owner atomically
// establish its approval and selection-consumption facts in the same commit.
func (s *Service) CreatePackageRun(ctx context.Context, input CreatePackageRunInput) (CreatePackageRunResult, error) {
	if input.ExecutionPackageRowID < 1 || input.Preflight == nil {
		return CreatePackageRunResult{}, fmt.Errorf("%w: execution package and preflight are required", ErrInvalidRunInput)
	}
	if !validFeatureSlug(input.FeatureSlug) || strings.TrimSpace(input.RepoTarget) != input.RepoTarget || strings.TrimSpace(input.RepoTarget) == "" ||
		strings.TrimSpace(input.Branch) != input.Branch || strings.TrimSpace(input.Branch) == "" || !validCommit(input.BaseCommit) {
		return CreatePackageRunResult{}, fmt.Errorf("%w: invalid package Run identity", ErrInvalidRunInput)
	}
	runID := s.ids.RunID()
	result := CreatePackageRunResult{Artifacts: make([]workflowstore.Artifact, 0)}
	err := s.store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		registered, err := tx.GetRepositoryTarget(ctx, input.RepoTarget)
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: %s", ErrRepositoryTargetNotFound, input.RepoTarget)
		}
		if err != nil {
			return err
		}
		if registered.RepoTarget != input.RepoTarget {
			return fmt.Errorf("%w: repository target %q must use registered key casing %q", ErrRepositoryTargetNotFound, input.RepoTarget, registered.RepoTarget)
		}
		if err := input.Preflight(ctx, tx); err != nil {
			return fmt.Errorf("package Run preflight: %w", err)
		}
		run, err := tx.CreateRun(ctx, workflowstore.CreateRunParams{
			RunID: runID, FeatureSlug: input.FeatureSlug, RepoTarget: input.RepoTarget,
			Status: workflowstore.RunStatusCreated, Branch: input.Branch, BaseCommit: input.BaseCommit,
			CanonicalSHA256: "",
		})
		if err != nil {
			return fmt.Errorf("create package Run: %w", err)
		}
		run, err = tx.TransitionRun(ctx, run.RunID, workflowstore.RunStatusCreated, workflowstore.RunStatusSetupReady)
		if err != nil {
			return fmt.Errorf("mark package Run setup ready: %w", err)
		}
		run, err = tx.LinkRunToExecutionPackage(ctx, run.RunID, input.ExecutionPackageRowID)
		if err != nil {
			return fmt.Errorf("link package Run: %w", err)
		}
		if input.PackageApprovalRowIDRef != nil && *input.PackageApprovalRowIDRef != 0 {
			run, err = tx.LinkRunToExecutionPackageApproval(ctx, workflowstore.LinkRunToExecutionPackageApprovalParams{
				PackageApprovalRowID: sql.NullInt64{Int64: *input.PackageApprovalRowIDRef, Valid: true}, RunID: run.RunID,
			})
			if err != nil {
				return fmt.Errorf("link package Run to approval: %w", err)
			}
		}
		result.Run = run
		return nil
	})
	if err != nil {
		return CreatePackageRunResult{}, err
	}
	return result, nil
}
func (s *Service) BeginExecutionAttempt(ctx context.Context, input BeginExecutionAttemptInput) (BeginExecutionAttemptResult, error) {
	if strings.TrimSpace(input.RunID) == "" || strings.TrimSpace(input.Adapter) == "" || strings.TrimSpace(input.Model) == "" {
		return BeginExecutionAttemptResult{}, fmt.Errorf("run ID, adapter, and model are required")
	}
	result := BeginExecutionAttemptResult{}
	err := s.store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		run, err := tx.GetRunByRunID(ctx, input.RunID)
		if err != nil {
			return fmt.Errorf("load run: %w", err)
		}
		switch run.Status {
		case workflowstore.RunStatusSetupReady,
			workflowstore.RunStatusExecutionFailed,
			workflowstore.RunStatusCancelled:
		default:
			return fmt.Errorf("run %q cannot start execution from status %q", run.RunID, run.Status)
		}
		if run.ExecutionPackageRowID.Valid {
			if err := recheckPackageCurrentness(ctx, tx, run); err != nil {
				return fmt.Errorf("package Run currentness recheck: %w", err)
			}
		}
		// The Run transition and attempt creation commit together. Any failure
		// rolls the Run back to its prior persisted status and prevents an
		// executor adapter from receiving an attempt.
		run, err = tx.TransitionRun(ctx, run.RunID, run.Status, workflowstore.RunStatusExecuting)
		if err != nil {
			return fmt.Errorf("start run execution: %w", err)
		}
		number, err := tx.NextExecutionAttemptNumber(ctx, run.ID)
		if err != nil {
			return fmt.Errorf("select execution attempt number: %w", err)
		}
		attempt, err := tx.CreateExecutionAttempt(ctx, workflowstore.CreateExecutionAttemptParams{
			AttemptID:     s.ids.ExecutionAttemptID(),
			RunRowID:      run.ID,
			AttemptNumber: number,
			Adapter:       input.Adapter,
			Model:         input.Model,
		})
		if err != nil {
			return fmt.Errorf("create execution attempt: %w", err)
		}
		result.Run = run
		result.Attempt = attempt
		return nil
	})
	return result, err
}

func (s *Service) MarkExecutionAttemptRunning(ctx context.Context, attemptID, resultJSON string) (workflowstore.ExecutionAttempt, error) {
	if resultJSON == "" {
		resultJSON = "{}"
	}
	if !json.Valid([]byte(resultJSON)) {
		return workflowstore.ExecutionAttempt{}, fmt.Errorf("execution attempt result must be valid JSON")
	}
	var updated workflowstore.ExecutionAttempt
	err := s.store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		attempt, err := tx.TransitionExecutionAttempt(ctx, attemptID, workflowstore.AttemptStatusPending, workflowstore.AttemptStatusRunning, resultJSON)
		if err != nil {
			return fmt.Errorf("mark execution attempt running: %w", err)
		}
		updated = attempt
		return nil
	})
	return updated, err
}

func (s *Service) UpdateExecutionAttemptResult(ctx context.Context, attemptID, resultJSON string) (workflowstore.ExecutionAttempt, error) {
	if !json.Valid([]byte(resultJSON)) {
		return workflowstore.ExecutionAttempt{}, fmt.Errorf("execution attempt result must be valid JSON")
	}
	var updated workflowstore.ExecutionAttempt
	err := s.store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		attempt, err := tx.GetExecutionAttemptByAttemptID(ctx, attemptID)
		if err != nil {
			return fmt.Errorf("load execution attempt: %w", err)
		}
		if attempt.Status != workflowstore.AttemptStatusPending && attempt.Status != workflowstore.AttemptStatusRunning {
			return fmt.Errorf("execution attempt %q is already terminal", attemptID)
		}
		attempt, err = tx.UpdateExecutionAttemptResult(ctx, attemptID, attempt.Status, resultJSON)
		if err != nil {
			return fmt.Errorf("update execution attempt result: %w", err)
		}
		updated = attempt
		return nil
	})
	return updated, err
}

func (s *Service) RequestExecutionAttemptCancellation(ctx context.Context, runID, attemptID string) (workflowstore.ExecutionAttempt, error) {
	var updated workflowstore.ExecutionAttempt
	err := s.store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		run, err := tx.GetRunByRunID(ctx, runID)
		if err != nil {
			return fmt.Errorf("load cancellation Run: %w", err)
		}
		attempt, err := tx.GetExecutionAttemptByAttemptID(ctx, attemptID)
		if err != nil {
			return fmt.Errorf("load execution attempt: %w", err)
		}
		if attempt.RunRowID != run.ID {
			return fmt.Errorf("execution attempt does not belong to Run")
		}
		if attempt.Status == workflowstore.AttemptStatusSucceeded ||
			attempt.Status == workflowstore.AttemptStatusFailed ||
			attempt.Status == workflowstore.AttemptStatusCancelled ||
			attempt.Status == workflowstore.AttemptStatusTimedOut {
			updated = attempt
			return nil
		}
		attempt, err = tx.RequestExecutionAttemptCancellation(ctx, run.ID, attemptID)
		if err != nil {
			return fmt.Errorf("request execution attempt cancellation: %w", err)
		}
		updated = attempt
		return nil
	})
	return updated, err
}

func (s *Service) FinishExecutionAttempt(ctx context.Context, input FinishExecutionAttemptInput) (FinishExecutionAttemptResult, error) {
	if input.Status != workflowstore.AttemptStatusSucceeded &&
		input.Status != workflowstore.AttemptStatusFailed &&
		input.Status != workflowstore.AttemptStatusCancelled &&
		input.Status != workflowstore.AttemptStatusTimedOut {
		return FinishExecutionAttemptResult{}, fmt.Errorf("unsupported terminal execution attempt status %q", input.Status)
	}
	if input.ResultJSON == "" {
		input.ResultJSON = "{}"
	}
	if !json.Valid([]byte(input.ResultJSON)) {
		return FinishExecutionAttemptResult{}, fmt.Errorf("execution attempt result must be valid JSON")
	}
	result := FinishExecutionAttemptResult{}
	err := s.store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		attempt, err := tx.GetExecutionAttemptByAttemptID(ctx, input.AttemptID)
		if err != nil {
			return fmt.Errorf("load execution attempt: %w", err)
		}
		if attempt.Status != workflowstore.AttemptStatusPending && attempt.Status != workflowstore.AttemptStatusRunning {
			return fmt.Errorf("execution attempt %q is already terminal", input.AttemptID)
		}
		attempt, err = tx.TransitionExecutionAttempt(ctx, attempt.AttemptID, attempt.Status, input.Status, input.ResultJSON)
		if err != nil {
			return fmt.Errorf("finish execution attempt: %w", err)
		}
		run, err := tx.GetRunByRowID(ctx, attempt.RunRowID)
		if err != nil {
			return fmt.Errorf("load run for execution attempt: %w", err)
		}
		nextRunStatus := workflowstore.RunStatusExecutionFailed
		switch input.Status {
		case workflowstore.AttemptStatusSucceeded:
			nextRunStatus = workflowstore.RunStatusValidating
		case workflowstore.AttemptStatusCancelled:
			nextRunStatus = workflowstore.RunStatusCancelled
		}
		run, err = tx.TransitionRun(ctx, run.RunID, workflowstore.RunStatusExecuting, nextRunStatus)
		if err != nil {
			return fmt.Errorf("advance run after execution attempt: %w", err)
		}
		result.Run = run
		result.Attempt = attempt
		return nil
	})
	return result, err
}

func (s *Service) RecordApplierCompleted(ctx context.Context, runID string) (workflowstore.Run, error) {
	return s.transitionRunAfterApplier(ctx, runID, workflowstore.RunStatusValidating)
}

func (s *Service) RecordApplierBlocked(ctx context.Context, runID string) (workflowstore.Run, error) {
	return s.transitionRunAfterApplier(ctx, runID, workflowstore.RunStatusNeedsRevision)
}

func (s *Service) transitionRunAfterApplier(ctx context.Context, runID, nextStatus string) (workflowstore.Run, error) {
	var updated workflowstore.Run
	err := s.store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		run, err := tx.GetRunByRunID(ctx, runID)
		if err != nil {
			return fmt.Errorf("load run for applier result: %w", err)
		}
		switch run.Status {
		case workflowstore.RunStatusSetupReady,
			workflowstore.RunStatusExecutionFailed,
			workflowstore.RunStatusCancelled:
		default:
			return fmt.Errorf("record applier result requires setup_ready, execution_failed, or cancelled run, got %q", run.Status)
		}
		transition := func(next string) error {
			var transitionErr error
			run, transitionErr = tx.TransitionRun(ctx, run.RunID, run.Status, next)
			if transitionErr != nil {
				return transitionErr
			}
			return nil
		}
		switch nextStatus {
		case workflowstore.RunStatusValidating:
			if run.Status == workflowstore.RunStatusSetupReady || run.Status == workflowstore.RunStatusCancelled {
				if err := transition(workflowstore.RunStatusExecuting); err != nil {
					return fmt.Errorf("record applier result: %w", err)
				}
			}
		case workflowstore.RunStatusNeedsRevision:
			if run.Status == workflowstore.RunStatusSetupReady || run.Status == workflowstore.RunStatusCancelled {
				if err := transition(workflowstore.RunStatusExecuting); err != nil {
					return fmt.Errorf("record applier result: %w", err)
				}
			}
			if err := transition(workflowstore.RunStatusExecutionFailed); err != nil {
				return fmt.Errorf("record applier result: %w", err)
			}
			if err := transition(workflowstore.RunStatusValidating); err != nil {
				return fmt.Errorf("record applier result: %w", err)
			}
			if err := transition(workflowstore.RunStatusValidationFailed); err != nil {
				return fmt.Errorf("record applier result: %w", err)
			}
		default:
			return fmt.Errorf("unsupported applier run status %q", nextStatus)
		}
		if run.Status != nextStatus {
			if err := transition(nextStatus); err != nil {
				return fmt.Errorf("record applier result: %w", err)
			}
		}
		updated = run
		return nil
	})
	return updated, err
}

func (s *Service) RecordValidationResult(ctx context.Context, runID string, passed bool) (workflowstore.Run, error) {
	var updated workflowstore.Run
	err := s.store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		run, err := tx.GetRunByRunID(ctx, runID)
		if err != nil {
			return fmt.Errorf("load run for validation result: %w", err)
		}
		switch run.Status {
		case workflowstore.RunStatusExecutionFailed:
			run, err = tx.TransitionRun(ctx, run.RunID, workflowstore.RunStatusExecutionFailed, workflowstore.RunStatusValidating)
			if err != nil {
				return fmt.Errorf("enter validation after execution failure: %w", err)
			}
		case workflowstore.RunStatusValidating:
		default:
			return fmt.Errorf("record validation result requires validating or execution_failed run, got %q", run.Status)
		}

		next := workflowstore.RunStatusAuditReady
		if !passed {
			next = workflowstore.RunStatusValidationFailed
		}
		run, err = tx.TransitionRun(ctx, run.RunID, workflowstore.RunStatusValidating, next)
		if err != nil {
			return fmt.Errorf("record validation result: %w", err)
		}
		if !passed {
			run, err = tx.TransitionRun(ctx, run.RunID, workflowstore.RunStatusValidationFailed, workflowstore.RunStatusNeedsRevision)
			if err != nil {
				return fmt.Errorf("mark validation revision required: %w", err)
			}
		}
		updated = run
		return nil
	})
	return updated, err
}

func (s *Service) RecordAuditDecision(context.Context, RecordAuditDecisionInput) (RecordAuditDecisionResult, error) {
	return RecordAuditDecisionResult{}, fmt.Errorf("workflow audit decisions must be recorded through audits.WorkflowAuditService")
}

func validFeatureSlug(value string) bool {
	if value == "" || strings.TrimSpace(value) != value || strings.HasPrefix(value, "-") || strings.HasSuffix(value, "-") || strings.Contains(value, "--") {
		return false
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			continue
		}
		return false
	}
	return true
}

func validCommit(value string) bool {
	if len(value) != 40 {
		return false
	}
	for _, r := range value {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
			return false
		}
	}
	return true
}

func validSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, r := range value {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
			return false
		}
	}
	return true
}
