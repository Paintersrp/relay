package executor

import (
	"context"
	"errors"
	"fmt"
	"strings"

	workflowruns "relay/internal/app/runs/workflow"
	workflowstore "relay/internal/store/workflow"
)

var ErrPackageWorkflowDispatchConflict = errors.New("package workflow dispatch conflicts with prepared execution state")

type PackageWorkflowDispatchResult struct {
	Preparation  PackagePreparationResult
	Launch       PreparedAdaptiveLaunchResult
	FinalizedRun *workflowstore.Run
}

// These seams keep coordinator ordering and failure tests focused on this
// service while the production path remains owned by the existing services.
var (
	packageWorkflowDispatchLaunch = func(ctx context.Context, service *Execution, input PreparedAdaptiveLaunchInput) (PreparedAdaptiveLaunchResult, error) {
		return service.LaunchPreparedAdaptive(ctx, input)
	}
	packageWorkflowDispatchFinalize = func(ctx context.Context, service *workflowruns.Service, runID string) (workflowstore.Run, error) {
		return service.CompletePackageDeterministicExecution(ctx, runID)
	}
)

func (s *Execution) DispatchPreparedPackageWorkflow(ctx context.Context, prepared PackagePreparationResult) (PackageWorkflowDispatchResult, error) {
	result := PackageWorkflowDispatchResult{Preparation: prepared}
	mode, err := verifyPackageWorkflowDispatchPreparation(s, prepared)
	if err != nil {
		return result, err
	}

	if mode != EffectiveExecutorBriefDeterministicComplete {
		launch, launchErr := packageWorkflowDispatchLaunch(ctx, s, PreparedAdaptiveLaunchInput{
			RunID:     prepared.Run.RunID,
			AttemptID: prepared.Adaptive.Attempt.AttemptID,
		})
		result.Launch = launch
		if launchErr != nil {
			return result, launchErr
		}
		if err := verifyPackageWorkflowAdaptiveLaunch(prepared, launch); err != nil {
			return result, err
		}
		return result, nil
	}

	launch, launchErr := packageWorkflowDispatchLaunch(ctx, s, PreparedAdaptiveLaunchInput{RunID: prepared.Run.RunID})
	result.Launch = launch
	if launchErr != nil {
		return result, launchErr
	}
	if err := verifyPackageWorkflowCompleteLaunch(launch); err != nil {
		return result, err
	}

	finalized, finalizeErr := packageWorkflowDispatchFinalize(ctx, s.runs, prepared.Run.RunID)
	if finalizeErr != nil {
		return result, finalizeErr
	}
	if finalized.ID != prepared.Run.ID || finalized.RunID != prepared.Run.RunID || !finalized.ExecutionPackageRowID.Valid || finalized.ExecutionPackageRowID.Int64 < 1 || finalized.Status != workflowstore.RunStatusValidating {
		return result, packageWorkflowDispatchConflict("deterministic finalization returned an invalid Run")
	}
	result.FinalizedRun = &finalized
	return result, nil
}

func verifyPackageWorkflowDispatchPreparation(s *Execution, prepared PackagePreparationResult) (EffectiveExecutorBriefMode, error) {
	if s == nil || s.store == nil || s.runs == nil {
		return "", packageWorkflowDispatchConflict("execution service is unavailable")
	}
	if prepared.Run.ID < 1 || strings.TrimSpace(prepared.Run.RunID) == "" || prepared.Run.RunID != strings.TrimSpace(prepared.Run.RunID) {
		return "", packageWorkflowDispatchConflict("prepared Run identity")
	}
	if !prepared.Run.ExecutionPackageRowID.Valid || prepared.Run.ExecutionPackageRowID.Int64 < 1 {
		return "", packageWorkflowDispatchConflict("prepared Run is not package-linked")
	}

	mode, err := packageWorkflowExpectedMode(prepared.Deterministic.Outcome.Outcome.Outcome)
	if err != nil {
		return "", packageWorkflowDispatchConflict(err.Error())
	}
	input := PackagePreparationInput{RunID: prepared.Run.RunID}
	if mode != EffectiveExecutorBriefDeterministicComplete {
		if prepared.Adaptive.Attempt == nil {
			return "", packageWorkflowDispatchConflict("adaptive preparation has no attempt")
		}
		input.Adapter = prepared.Adaptive.Attempt.Adapter
		input.Model = prepared.Adaptive.Attempt.Model
		if err := validatePackagePreparationInput(input); err != nil {
			return "", packageWorkflowDispatchConflict(err.Error())
		}
	}
	if err := verifyPackagePreparation(prepared.Run, prepared.Deterministic, prepared.Adaptive, input); err != nil {
		return "", packageWorkflowDispatchConflict(err.Error())
	}
	return mode, nil
}

func verifyPackageWorkflowAdaptiveLaunch(prepared PackagePreparationResult, launch PreparedAdaptiveLaunchResult) error {
	if launch.Mode != prepared.Adaptive.Mode || !launch.AdaptiveDispatchRequired || launch.Run == nil || launch.Attempt == nil || launch.Lease == nil {
		return packageWorkflowDispatchConflict("adaptive launch result shape")
	}
	if !launch.NewlyAdmitted && launch.NewlyLaunched {
		return packageWorkflowDispatchConflict("adaptive launch result reports a launch without new admission")
	}
	if launch.Run.ID != prepared.Run.ID || launch.Run.RunID != prepared.Run.RunID {
		return packageWorkflowDispatchConflict("adaptive launch Run identity")
	}
	preparedAttempt := prepared.Adaptive.Attempt
	if launch.Attempt.ID != preparedAttempt.ID || launch.Attempt.AttemptID != preparedAttempt.AttemptID || launch.Attempt.Adapter != preparedAttempt.Adapter || launch.Attempt.Model != preparedAttempt.Model {
		return packageWorkflowDispatchConflict("adaptive launch attempt identity")
	}
	lease := launch.Lease
	if strings.TrimSpace(lease.LeaseID) == "" || lease.OwnerIdentity != prepared.Run.RunID || lease.RepoTarget != prepared.Run.RepoTarget || lease.Branch != prepared.Run.Branch || lease.State != workflowstore.RepositoryBranchMutationLeaseStateActive || lease.UncertaintyState != workflowstore.RepositoryBranchMutationLeaseCertaintyCertain || lease.ReconciliationState != workflowstore.RepositoryBranchMutationLeaseReconciliationNotRequired {
		return packageWorkflowDispatchConflict("adaptive launch lease identity")
	}
	if prepared.Adaptive.Mode == EffectiveExecutorBriefAdaptiveAfterPartialApplication {
		if prepared.Deterministic.ActiveLease == nil || lease.LeaseID != prepared.Deterministic.ActiveLease.LeaseID {
			return packageWorkflowDispatchConflict("adaptive partial launch replaced the deterministic lease")
		}
	}
	return nil
}

func verifyPackageWorkflowCompleteLaunch(launch PreparedAdaptiveLaunchResult) error {
	if launch.Mode != EffectiveExecutorBriefDeterministicComplete || launch.AdaptiveDispatchRequired || launch.NewlyAdmitted || launch.NewlyLaunched || launch.Run != nil || launch.Attempt != nil || launch.Lease != nil {
		return packageWorkflowDispatchConflict("deterministic-complete launch result shape")
	}
	return nil
}

func packageWorkflowDispatchConflict(reason string) error {
	return fmt.Errorf("%w: %s", ErrPackageWorkflowDispatchConflict, reason)
}
