package executor

import (
	"context"
	"errors"
	"fmt"
	"strings"

	executionpackages "relay/internal/app/packages"
	workflowruns "relay/internal/app/runs/workflow"
	workflowstore "relay/internal/store/workflow"
)

var ErrPackagePreparationConflict = errors.New("package workflow preparation conflicts with deterministic outcome")

type PackagePreparationInput struct {
	RunID   string
	Adapter string
	Model   string
}

type PackagePreparationResult struct {
	Run           workflowstore.Run
	Deterministic PackageDeterministicExecutionResult
	Adaptive      AdaptiveExecutionAttemptResult
}

// These seams keep coordinator ordering and failure tests focused on this
// service while production continues to use the owned services directly.
var (
	packageWorkflowAdmit = func(ctx context.Context, service *workflowruns.Service, runID string) (workflowstore.Run, error) {
		return service.AdmitPackageExecution(ctx, runID)
	}
	packageWorkflowExecuteDeterministic = func(ctx context.Context, service *PackageDeterministicExecutionService, runID string) (PackageDeterministicExecutionResult, error) {
		return service.Execute(ctx, runID)
	}
	packageWorkflowPrepareAdaptive = func(ctx context.Context, service *AdaptiveExecutionAttemptService, input AdaptiveExecutionAttemptInput) (AdaptiveExecutionAttemptResult, error) {
		return service.Prepare(ctx, input)
	}
)

type PackagePreparation struct {
	runs          *workflowruns.Service
	deterministic *PackageDeterministicExecutionService
	adaptive      *AdaptiveExecutionAttemptService
}

func NewPackagePreparation(
	store *workflowstore.Store,
	sourceVaults executionpackages.SourceVaultReader,
) (*PackagePreparation, error) {
	if store == nil {
		return nil, fmt.Errorf("workflow store is required")
	}
	if sourceVaults == nil {
		return nil, fmt.Errorf("source-vault reader is required")
	}
	runs, err := workflowruns.NewService(store)
	if err != nil {
		return nil, err
	}
	deterministic, err := NewPackageDeterministicExecutionService(store, sourceVaults)
	if err != nil {
		return nil, err
	}
	adaptive, err := NewAdaptiveExecutionAttemptService(store, sourceVaults)
	if err != nil {
		return nil, err
	}
	return &PackagePreparation{runs: runs, deterministic: deterministic, adaptive: adaptive}, nil
}

func (s *PackagePreparation) Prepare(ctx context.Context, input PackagePreparationInput) (PackagePreparationResult, error) {
	if s == nil || s.runs == nil || s.deterministic == nil || s.adaptive == nil {
		return PackagePreparationResult{}, fmt.Errorf("package workflow preparation service is unavailable")
	}
	if err := validatePackagePreparationInput(input); err != nil {
		return PackagePreparationResult{}, err
	}

	run, err := packageWorkflowAdmit(ctx, s.runs, input.RunID)
	if err != nil {
		return PackagePreparationResult{}, err
	}
	result := PackagePreparationResult{Run: run}

	deterministic, err := packageWorkflowExecuteDeterministic(ctx, s.deterministic, input.RunID)
	result.Deterministic = deterministic
	if err != nil {
		return result, err
	}

	adaptive, err := packageWorkflowPrepareAdaptive(ctx, s.adaptive, AdaptiveExecutionAttemptInput{
		RunID: input.RunID, Adapter: input.Adapter, Model: input.Model,
	})
	result.Adaptive = adaptive
	if err != nil {
		return result, err
	}
	if err := verifyPackagePreparation(result.Run, result.Deterministic, result.Adaptive, input); err != nil {
		return result, err
	}
	return result, nil
}

func validatePackagePreparationInput(input PackagePreparationInput) error {
	if input.RunID == "" || strings.TrimSpace(input.RunID) != input.RunID {
		return fmt.Errorf("Run ID must be nonblank without outer whitespace")
	}
	normalized, err := NormalizeKnownAdapterID(input.Adapter)
	if err != nil {
		return err
	}
	if input.Adapter != normalized {
		return fmt.Errorf("executor adapter must be a known normalized adapter ID")
	}
	if input.Model == "" || strings.TrimSpace(input.Model) != input.Model {
		return fmt.Errorf("executor model must be nonblank without outer whitespace")
	}
	return nil
}

func verifyPackagePreparation(run workflowstore.Run, deterministic PackageDeterministicExecutionResult, adaptive AdaptiveExecutionAttemptResult, input PackagePreparationInput) error {
	mode, err := packageWorkflowExpectedMode(deterministic.Outcome.Outcome.Outcome)
	if err != nil {
		return err
	}
	switch mode {
	case EffectiveExecutorBriefAdaptiveNoOperations:
		if deterministic.Application != nil || deterministic.Outcome.Outcome.Application != nil || deterministic.ActiveLease != nil || deterministic.Outcome.Outcome.PreflightFailure != nil {
			return packagePreparationConflict("not_present result shape")
		}
		return verifyPackageWorkflowAdaptiveResult(run, adaptive, input, mode)
	case EffectiveExecutorBriefAdaptivePreflightFailed:
		if deterministic.Application != nil || deterministic.Outcome.Outcome.Application != nil || deterministic.ActiveLease != nil || deterministic.Outcome.Outcome.PreflightFailure == nil {
			return packagePreparationConflict("preflight_failed result shape")
		}
		return verifyPackageWorkflowAdaptiveResult(run, adaptive, input, mode)
	case EffectiveExecutorBriefAdaptiveAfterPartialApplication:
		lease := deterministic.ActiveLease
		if lease == nil || lease.State != workflowstore.RepositoryBranchMutationLeaseStateActive || lease.OwnerKind != "run_execution" || lease.OwnerIdentity != run.RunID || lease.RepoTarget != run.RepoTarget || lease.Branch != run.Branch || lease.UncertaintyState != workflowstore.RepositoryBranchMutationLeaseCertaintyCertain || lease.ReconciliationState != workflowstore.RepositoryBranchMutationLeaseReconciliationNotRequired {
			return packagePreparationConflict("partial application lease shape")
		}
		return verifyPackageWorkflowAdaptiveResult(run, adaptive, input, mode)
	case EffectiveExecutorBriefDeterministicComplete:
		if deterministic.ActiveLease != nil {
			return packagePreparationConflict("complete application has an active lease")
		}
		if adaptive.Mode != mode || adaptive.AdaptiveDispatchRequired || adaptive.Attempt != nil || adaptive.InputArtifact != nil || len(adaptive.InputBytes) != 0 {
			return packagePreparationConflict("deterministic-complete adaptive result shape")
		}
		return nil
	default:
		return packagePreparationConflict("unsupported effective mode")
	}
}

func packageWorkflowExpectedMode(outcome DeterministicOutcomeSummary) (EffectiveExecutorBriefMode, error) {
	switch outcome.Status {
	case string(DeterministicPreflightNotPresent):
		if outcome.Coverage != "" {
			return "", packagePreparationConflict("not_present outcome coverage")
		}
		return EffectiveExecutorBriefAdaptiveNoOperations, nil
	case string(DeterministicPreflightFailed):
		return EffectiveExecutorBriefAdaptivePreflightFailed, nil
	case "applied":
		switch outcome.Coverage {
		case "partial":
			return EffectiveExecutorBriefAdaptiveAfterPartialApplication, nil
		case "complete":
			return EffectiveExecutorBriefDeterministicComplete, nil
		default:
			return "", packagePreparationConflict("applied outcome coverage")
		}
	default:
		return "", packagePreparationConflict("deterministic outcome status")
	}
}

func verifyPackageWorkflowAdaptiveResult(run workflowstore.Run, adaptive AdaptiveExecutionAttemptResult, input PackagePreparationInput, expected EffectiveExecutorBriefMode) error {
	if adaptive.Mode != expected || !adaptive.AdaptiveDispatchRequired || adaptive.Attempt == nil || adaptive.InputArtifact == nil || len(adaptive.InputBytes) == 0 {
		return packagePreparationConflict("adaptive result shape")
	}
	attempt := adaptive.Attempt
	artifact := adaptive.InputArtifact
	if attempt.RunRowID != run.ID || attempt.Adapter != input.Adapter || attempt.Model != input.Model || attempt.AttemptNumber != 1 || (attempt.Status != workflowstore.AttemptStatusPending && attempt.Status != workflowstore.AttemptStatusRunning) {
		return packagePreparationConflict("adaptive attempt identity")
	}
	if artifact.OwnerType != workflowstore.ArtifactOwnerExecutionAttempt || !artifact.ExecutionAttemptRowID.Valid || artifact.ExecutionAttemptRowID.Int64 != attempt.ID {
		return packagePreparationConflict("adaptive input artifact ownership")
	}
	return nil
}

func packagePreparationConflict(reason string) error {
	return fmt.Errorf("%w: %s", ErrPackagePreparationConflict, reason)
}
