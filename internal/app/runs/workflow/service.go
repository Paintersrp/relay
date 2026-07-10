package workflowruns

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	workflowartifacts "relay/internal/artifacts/workflow"
	workflowstore "relay/internal/store/workflow"
)

var (
	ErrInvalidRunInput          = errors.New("invalid Run input")
	ErrRepositoryTargetNotFound = errors.New("repository target not found")
	ErrPlanPassAssociation      = errors.New("Plan/pass association invalid")
	ErrRemediationAssociation   = errors.New("remediation association invalid")
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

func (s *Service) CreateRun(ctx context.Context, input CreateRunInput) (CreateRunResult, error) {
	if err := validateCreateRunInput(input); err != nil {
		return CreateRunResult{}, err
	}
	runID := s.ids.RunID()
	artifactStem := input.FeatureSlug
	if input.PlanID != "" {
		artifactStem = fmt.Sprintf("%s.pass-%d", input.FeatureSlug, input.PassNumber)
	}
	batch, err := s.store.ArtifactStore().Begin("runs/" + runID)
	if err != nil {
		return CreateRunResult{}, err
	}
	canonical, err := batch.Stage(
		"execution_spec",
		artifactStem+".execution-spec.json",
		"application/json",
		input.CanonicalJSON,
	)
	if err != nil {
		_ = batch.Rollback()
		return CreateRunResult{}, err
	}
	rendered, err := batch.Stage(
		"executor_brief",
		artifactStem+".executor-brief.md",
		"text/markdown",
		input.RenderedMarkdown,
	)
	if err != nil {
		_ = batch.Rollback()
		return CreateRunResult{}, err
	}

	result := CreateRunResult{}
	err = s.store.CommitArtifactBatch(ctx, batch, func(tx *workflowstore.Tx) error {
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

		planRowID := sql.NullInt64{}
		passRowID := sql.NullInt64{}
		if input.PlanID != "" {
			plan, err := tx.GetPlanByPlanID(ctx, input.PlanID)
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("%w: managed Plan %s was not found", ErrPlanPassAssociation, input.PlanID)
			}
			if err != nil {
				return err
			}
			if plan.Status != workflowstore.PlanStatusActive {
				return fmt.Errorf("%w: managed Plan %s is %s", ErrPlanPassAssociation, input.PlanID, plan.Status)
			}
			pass, err := tx.GetPlanPassByPlanAndNumber(ctx, plan.ID, input.PassNumber)
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("%w: managed pass %d was not found", ErrPlanPassAssociation, input.PassNumber)
			}
			if err != nil {
				return err
			}
			if !strings.EqualFold(pass.RepoTarget, input.RepoTarget) {
				return fmt.Errorf("%w: managed pass repository %q does not match Run repository %q", ErrPlanPassAssociation, pass.RepoTarget, input.RepoTarget)
			}
			switch pass.Status {
			case workflowstore.PassStatusPlanned:
				pass, err = tx.TransitionPlanPass(ctx, pass.PassID, workflowstore.PassStatusPlanned, workflowstore.PassStatusInProgress)
				if err != nil {
					return fmt.Errorf("start managed pass %d: %w", input.PassNumber, err)
				}
			case workflowstore.PassStatusInProgress:
			case workflowstore.PassStatusCompleted:
				return fmt.Errorf("%w: managed pass %d is already completed", ErrPlanPassAssociation, input.PassNumber)
			default:
				return fmt.Errorf("%w: managed pass %d has unsupported status %q", ErrPlanPassAssociation, input.PassNumber, pass.Status)
			}
			planRowID = sql.NullInt64{Int64: plan.ID, Valid: true}
			passRowID = sql.NullInt64{Int64: pass.ID, Valid: true}
		}

		remediatesRowID := sql.NullInt64{}
		if input.RemediatesRunID != "" {
			original, err := tx.GetRunByRunID(ctx, input.RemediatesRunID)
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("%w: remediation source Run %s was not found", ErrRemediationAssociation, input.RemediatesRunID)
			}
			if err != nil {
				return err
			}
			if original.Status != workflowstore.RunStatusNeedsRevision ||
				!strings.EqualFold(original.RepoTarget, input.RepoTarget) ||
				original.PlanRowID != planRowID ||
				original.PlanPassRowID != passRowID {
				return fmt.Errorf("%w: remediation source Run must be needs_revision with the identical repository and Plan/pass association", ErrRemediationAssociation)
			}
			remediatesRowID = sql.NullInt64{Int64: original.ID, Valid: true}
		}

		run, err := tx.CreateRun(ctx, workflowstore.CreateRunParams{
			RunID:              runID,
			FeatureSlug:        input.FeatureSlug,
			RepoTarget:         input.RepoTarget,
			PlanRowID:          planRowID,
			PlanPassRowID:      passRowID,
			RemediatesRunRowID: remediatesRowID,
			Status:             workflowstore.RunStatusCreated,
			Branch:             input.Branch,
			BaseCommit:         input.BaseCommit,
			CanonicalSHA256:    canonical.SHA256,
		})
		if err != nil {
			return fmt.Errorf("create run: %w", err)
		}
		run, err = tx.TransitionRun(ctx, run.RunID, workflowstore.RunStatusCreated, workflowstore.RunStatusSetupReady)
		if err != nil {
			return fmt.Errorf("mark run setup ready: %w", err)
		}
		result.Run = run

		for _, staged := range []workflowartifacts.File{canonical, rendered} {
			artifact, err := tx.CreateArtifact(ctx, workflowstore.CreateArtifactParams{
				ArtifactID:   s.ids.ArtifactID(),
				OwnerType:    workflowstore.ArtifactOwnerRun,
				RunRowID:     sql.NullInt64{Int64: run.ID, Valid: true},
				Kind:         staged.Kind,
				RelativePath: staged.RelativePath,
				MediaType:    staged.MediaType,
				SHA256:       staged.SHA256,
				SizeBytes:    staged.SizeBytes,
			})
			if err != nil {
				return fmt.Errorf("create run artifact metadata: %w", err)
			}
			result.Artifacts = append(result.Artifacts, artifact)
		}
		return nil
	})
	if err != nil {
		return CreateRunResult{}, err
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

func validateCreateRunInput(input CreateRunInput) error {
	if !validFeatureSlug(input.FeatureSlug) {
		return fmt.Errorf("%w: feature slug must be lowercase kebab-case", ErrInvalidRunInput)
	}
	if strings.TrimSpace(input.RepoTarget) == "" || strings.TrimSpace(input.RepoTarget) != input.RepoTarget {
		return fmt.Errorf("%w: repository target is required without outer whitespace", ErrInvalidRunInput)
	}
	if strings.TrimSpace(input.Branch) == "" || strings.TrimSpace(input.Branch) != input.Branch {
		return fmt.Errorf("%w: branch is required without outer whitespace", ErrInvalidRunInput)
	}
	if !validCommit(input.BaseCommit) {
		return fmt.Errorf("%w: base commit must be a lowercase full 40-character SHA", ErrInvalidRunInput)
	}
	if len(input.CanonicalJSON) == 0 || len(input.RenderedMarkdown) == 0 {
		return fmt.Errorf("%w: canonical Execution Spec JSON and rendered Executor Brief are required", ErrInvalidRunInput)
	}
	if (input.PlanID == "") != (input.PassNumber == 0) {
		return fmt.Errorf("%w: Plan ID and pass number must be supplied together", ErrPlanPassAssociation)
	}
	return nil
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
