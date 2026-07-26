package executor

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"strings"

	executionpackages "relay/internal/app/packages"
	workflowruns "relay/internal/app/runs/workflow"
	workflowstore "relay/internal/store/workflow"
)

var ErrPackageDeterministicExecutionConflict = errors.New("package deterministic execution conflicts with recorded execution state")

// PackageDeterministicExecutionResult is the verified result of the package
// deterministic phase. ActiveLease is returned only when this phase must keep
// ownership for partial or uncertain source state.
type PackageDeterministicExecutionResult struct {
	Assignment  ExecutionAssignmentResult
	Preflight   DeterministicPreflightResult
	Application *DeterministicApplicationResult
	Outcome     DeterministicOutcomeResult
	ActiveLease *workflowstore.RepositoryBranchMutationLease
}

// These seams keep coordinator tests deterministic without introducing a
// second execution abstraction. Production uses the existing implementations.
var (
	packageDeterministicPreflight                                                                                                                    = PreflightDeterministicOperations
	packageDeterministicApply                                                                                                                        = ApplyDeterministicMutationPlan
	packageDeterministicPersist   func(context.Context, *DeterministicOutcomeService, DeterministicOutcomeInput) (DeterministicOutcomeResult, error) = func(ctx context.Context, service *DeterministicOutcomeService, input DeterministicOutcomeInput) (DeterministicOutcomeResult, error) {
		return service.Persist(ctx, input)
	}
	packageDeterministicLoad func(context.Context, *DeterministicOutcomeService, string) (DeterministicOutcomeResult, error) = func(ctx context.Context, service *DeterministicOutcomeService, runID string) (DeterministicOutcomeResult, error) {
		return service.Load(ctx, runID)
	}
	packageDeterministicAcquire = func(ctx context.Context, service *workflowruns.Service, runID string) (workflowstore.RepositoryBranchMutationLease, error) {
		return service.AcquireRunMutationLease(ctx, runID)
	}
	packageDeterministicRelease = func(ctx context.Context, service *workflowruns.Service, runID, leaseID string) (workflowstore.RepositoryBranchMutationLease, error) {
		return service.ReleaseRunMutationLease(ctx, runID, leaseID)
	}
	packageDeterministicMarkUncertain = func(ctx context.Context, service *workflowruns.Service, runID, leaseID, reason string) (workflowstore.RepositoryBranchMutationLease, error) {
		return service.MarkRunMutationLeaseUncertain(ctx, runID, leaseID, reason)
	}
	packageDeterministicHasOutcome = func(ctx context.Context, service *PackageDeterministicExecutionService, runRowID int64) (bool, error) {
		return service.runHasDeterministicOutcome(ctx, runRowID)
	}
)

type PackageDeterministicExecutionService struct {
	store       *workflowstore.Store
	packages    *executionpackages.Service
	assignments *ExecutionAssignmentService
	outcomes    *DeterministicOutcomeService
	runs        *workflowruns.Service
}

func NewPackageDeterministicExecutionService(store *workflowstore.Store) (*PackageDeterministicExecutionService, error) {
	if store == nil {
		return nil, fmt.Errorf("workflow store is required")
	}
	packages, err := executionpackages.NewService(store)
	if err != nil {
		return nil, err
	}
	assignments, err := NewExecutionAssignmentService(store)
	if err != nil {
		return nil, err
	}
	outcomes, err := NewDeterministicOutcomeService(store)
	if err != nil {
		return nil, err
	}
	runs, err := workflowruns.NewService(store)
	if err != nil {
		return nil, err
	}
	return &PackageDeterministicExecutionService{
		store: store, packages: packages, assignments: assignments, outcomes: outcomes, runs: runs,
	}, nil
}

// Execute runs the package-linked deterministic phase for exactly one Run.
// Adaptive execution is intentionally outside this service.
func (s *PackageDeterministicExecutionService) Execute(ctx context.Context, runID string) (PackageDeterministicExecutionResult, error) {
	if s == nil || s.store == nil || s.packages == nil || s.assignments == nil || s.outcomes == nil || s.runs == nil {
		return PackageDeterministicExecutionResult{}, fmt.Errorf("package deterministic execution service is unavailable")
	}
	if strings.TrimSpace(runID) == "" || strings.TrimSpace(runID) != runID {
		return PackageDeterministicExecutionResult{}, fmt.Errorf("Run ID must be nonblank without outer whitespace")
	}

	authority, err := s.packages.LoadApprovedAuthorityForRun(ctx, runID)
	if err != nil {
		return PackageDeterministicExecutionResult{}, err
	}
	assignment, err := s.assignments.PrepareExecutionAssignment(ctx, runID)
	if err != nil {
		return PackageDeterministicExecutionResult{}, err
	}
	if err := validatePackageDeterministicAssignment(authority, assignment); err != nil {
		return PackageDeterministicExecutionResult{}, err
	}

	if hasOutcome, err := packageDeterministicHasOutcome(ctx, s, authority.Run.ID); err != nil {
		return PackageDeterministicExecutionResult{}, err
	} else if hasOutcome {
		outcome, loadErr := packageDeterministicLoad(ctx, s.outcomes, runID)
		if loadErr != nil {
			return PackageDeterministicExecutionResult{Assignment: assignment}, loadErr
		}
		return s.settleExistingOutcome(ctx, assignment, outcome)
	}

	if authority.Run.Status != workflowstore.RunStatusSetupReady {
		return PackageDeterministicExecutionResult{Assignment: assignment}, fmt.Errorf("new deterministic work requires a setup_ready Run")
	}

	if authority.DeterministicOperations == nil {
		preflight, preflightErr := packageDeterministicPreflight(DeterministicPreflightInput{})
		if preflightErr != nil {
			return PackageDeterministicExecutionResult{Assignment: assignment}, preflightErr
		}
		if preflight.Status != DeterministicPreflightNotPresent || preflight.Coverage != "" || preflight.Plan != nil || preflight.Failure != nil {
			return PackageDeterministicExecutionResult{Assignment: assignment, Preflight: preflight}, fmt.Errorf("invalid absent deterministic preflight result")
		}
		return s.persistWithoutLease(ctx, assignment, preflight)
	}

	repository, err := s.store.GetRepositoryTarget(ctx, assignment.Assignment.Repository.Target)
	if err != nil {
		return PackageDeterministicExecutionResult{Assignment: assignment}, err
	}
	if repository.RepoTarget != assignment.Assignment.Repository.Target || repository.RepoTarget != authority.Run.RepoTarget {
		return PackageDeterministicExecutionResult{Assignment: assignment}, fmt.Errorf("registered repository target does not match execution assignment")
	}
	if strings.TrimSpace(repository.LocalPath) == "" {
		return PackageDeterministicExecutionResult{Assignment: assignment}, fmt.Errorf("registered repository target has no local repository path")
	}

	preflight, err := packageDeterministicPreflight(DeterministicPreflightInput{
		RepositoryRoot: repository.LocalPath,
		ExpectedBranch: assignment.Assignment.Repository.Branch,
		ExpectedCommit: assignment.Assignment.Repository.BaseCommit,
		Document:       authority.DeterministicOperations.Document,
	})
	if err != nil {
		return PackageDeterministicExecutionResult{Assignment: assignment}, err
	}
	if err := validatePackageDeterministicPreflight(preflight, assignment.Assignment.DeterministicOperations); err != nil {
		return PackageDeterministicExecutionResult{Assignment: assignment, Preflight: preflight}, err
	}
	if preflight.Status == DeterministicPreflightFailed {
		return s.persistWithoutLease(ctx, assignment, preflight)
	}
	if preflight.Plan == nil {
		return PackageDeterministicExecutionResult{Assignment: assignment, Preflight: preflight}, fmt.Errorf("ready deterministic preflight has no prepared mutation plan")
	}

	lease, err := packageDeterministicAcquire(ctx, s.runs, runID)
	if err != nil {
		return PackageDeterministicExecutionResult{Assignment: assignment, Preflight: preflight}, err
	}
	base := PackageDeterministicExecutionResult{Assignment: assignment, Preflight: preflight}
	if hasOutcome, outcomeErr := packageDeterministicHasOutcome(ctx, s, authority.Run.ID); outcomeErr != nil {
		return s.releasePostAcquisitionLease(ctx, base, runID, lease, outcomeErr)
	} else if hasOutcome {
		outcome, loadErr := packageDeterministicLoad(ctx, s.outcomes, runID)
		if loadErr != nil {
			return s.releasePostAcquisitionLease(ctx, PackageDeterministicExecutionResult{Assignment: assignment}, runID, lease, loadErr)
		}
		return s.settleOutcomeAfterAcquisition(ctx, assignment, outcome, runID, lease)
	}
	application, applicationErr := packageDeterministicApply(DeterministicApplyInput{
		RepositoryRoot: repository.LocalPath,
		ExpectedBranch: assignment.Assignment.Repository.Branch,
		ExpectedCommit: assignment.Assignment.Repository.BaseCommit,
		Plan:           preflight.Plan,
	})
	if applicationErr != nil {
		return s.settleApplicationError(ctx, base, runID, lease, applicationErr)
	}

	outcome, persistErr := packageDeterministicPersist(ctx, s.outcomes, DeterministicOutcomeInput{RunID: runID, Preflight: preflight, Application: &application})
	if persistErr != nil {
		return s.retainUncertainAfterError(ctx, base, runID, lease, persistErr, "deterministic outcome persistence failed after source mutation")
	}
	base.Application = &application
	base.Outcome = outcome
	if err := validatePersistedPackageDeterministicOutcome(outcome, preflight, application); err != nil {
		return s.retainUncertainAfterError(ctx, base, runID, lease, err, "persisted deterministic outcome evidence does not match source mutation")
	}

	if application.Coverage == "partial" {
		base.ActiveLease = &lease
		return base, nil
	}
	if application.Coverage != "complete" {
		return s.retainUncertainAfterError(ctx, base, runID, lease, fmt.Errorf("unsupported deterministic application coverage %q", application.Coverage), "deterministic application returned unsupported coverage")
	}
	if _, releaseErr := packageDeterministicRelease(ctx, s.runs, runID, lease.LeaseID); releaseErr != nil {
		base.ActiveLease = &lease
		return base, releaseErr
	}
	return base, nil
}

// settleOutcomeAfterAcquisition settles only the lease obtained by this
// invocation. A durable partial outcome cannot be handed off through a new
// lease because its required ownership continuity has already been broken.
func (s *PackageDeterministicExecutionService) settleOutcomeAfterAcquisition(ctx context.Context, assignment ExecutionAssignmentResult, outcome DeterministicOutcomeResult, runID string, lease workflowstore.RepositoryBranchMutationLease) (PackageDeterministicExecutionResult, error) {
	result := PackageDeterministicExecutionResult{Assignment: assignment, Outcome: outcome}
	switch outcome.Outcome.Outcome.Status {
	case string(DeterministicPreflightNotPresent), string(DeterministicPreflightFailed):
		return s.releasePostAcquisitionLease(ctx, result, runID, lease, nil)
	case "applied":
		switch outcome.Outcome.Outcome.Coverage {
		case "complete":
			return s.releasePostAcquisitionLease(ctx, result, runID, lease, nil)
		case "partial":
			return s.releasePostAcquisitionLease(ctx, result, runID, lease, ErrPackageDeterministicExecutionConflict)
		default:
			return s.releasePostAcquisitionLease(ctx, result, runID, lease, fmt.Errorf("unsupported durable deterministic outcome coverage %q", outcome.Outcome.Outcome.Coverage))
		}
	default:
		return s.releasePostAcquisitionLease(ctx, result, runID, lease, fmt.Errorf("unsupported durable deterministic outcome status %q", outcome.Outcome.Outcome.Status))
	}
}

func (s *PackageDeterministicExecutionService) releasePostAcquisitionLease(ctx context.Context, result PackageDeterministicExecutionResult, runID string, lease workflowstore.RepositoryBranchMutationLease, operationErr error) (PackageDeterministicExecutionResult, error) {
	if _, releaseErr := packageDeterministicRelease(ctx, s.runs, runID, lease.LeaseID); releaseErr != nil {
		result.ActiveLease = &lease
		return result, errors.Join(operationErr, releaseErr)
	}
	return result, operationErr
}

func (s *PackageDeterministicExecutionService) persistWithoutLease(ctx context.Context, assignment ExecutionAssignmentResult, preflight DeterministicPreflightResult) (PackageDeterministicExecutionResult, error) {
	outcome, err := packageDeterministicPersist(ctx, s.outcomes, DeterministicOutcomeInput{RunID: assignment.Assignment.Run.RunID, Preflight: preflight})
	result := PackageDeterministicExecutionResult{Assignment: assignment, Preflight: preflight, Outcome: outcome}
	if err != nil {
		return result, err
	}
	if err := validatePersistedPackageDeterministicOutcome(outcome, preflight, DeterministicApplicationResult{}); err != nil {
		return result, err
	}
	return result, nil
}

func (s *PackageDeterministicExecutionService) settleExistingOutcome(ctx context.Context, assignment ExecutionAssignmentResult, outcome DeterministicOutcomeResult) (PackageDeterministicExecutionResult, error) {
	result := PackageDeterministicExecutionResult{Assignment: assignment, Outcome: outcome}
	switch outcome.Outcome.Outcome.Status {
	case string(DeterministicPreflightNotPresent), string(DeterministicPreflightFailed):
		return result, nil
	case "applied":
		if outcome.Outcome.Outcome.Coverage == "partial" {
			lease, err := s.exactActiveRunLease(ctx, assignment.Assignment.Run.RunID, assignment.Assignment.Repository)
			if err != nil {
				return result, err
			}
			if lease.UncertaintyState != workflowstore.RepositoryBranchMutationLeaseCertaintyCertain || lease.ReconciliationState != workflowstore.RepositoryBranchMutationLeaseReconciliationNotRequired {
				return result, fmt.Errorf("existing partial deterministic outcome requires a certain unreconciled lease")
			}
			result.ActiveLease = &lease
			return result, nil
		}
		if outcome.Outcome.Outcome.Coverage != "complete" {
			return result, fmt.Errorf("unsupported durable deterministic outcome coverage %q", outcome.Outcome.Outcome.Coverage)
		}
		lease, err := s.sameRunActiveLease(ctx, assignment.Assignment.Run.RunID, assignment.Assignment.Repository)
		if err != nil {
			return result, err
		}
		if lease == nil {
			return result, nil
		}
		if _, err := packageDeterministicRelease(ctx, s.runs, assignment.Assignment.Run.RunID, lease.LeaseID); err != nil {
			result.ActiveLease = lease
			return result, err
		}
		return result, nil
	default:
		return result, fmt.Errorf("unsupported durable deterministic outcome status %q", outcome.Outcome.Outcome.Status)
	}
}

func (s *PackageDeterministicExecutionService) settleApplicationError(ctx context.Context, result PackageDeterministicExecutionResult, runID string, lease workflowstore.RepositoryBranchMutationLease, applicationErr error) (PackageDeterministicExecutionResult, error) {
	if errors.Is(applicationErr, ErrDeterministicMutationReconciliation) {
		updated, stateErr := packageDeterministicMarkUncertain(ctx, s.runs, runID, lease.LeaseID, "deterministic application rollback is incomplete or mutation state is uncertain")
		if stateErr == nil {
			result.ActiveLease = &updated
		}
		return result, errors.Join(applicationErr, stateErr)
	}
	_, releaseErr := packageDeterministicRelease(ctx, s.runs, runID, lease.LeaseID)
	if releaseErr != nil {
		result.ActiveLease = &lease
	}
	return result, errors.Join(applicationErr, releaseErr)
}

func (s *PackageDeterministicExecutionService) retainUncertainAfterError(ctx context.Context, result PackageDeterministicExecutionResult, runID string, lease workflowstore.RepositoryBranchMutationLease, operationErr error, reason string) (PackageDeterministicExecutionResult, error) {
	updated, stateErr := packageDeterministicMarkUncertain(ctx, s.runs, runID, lease.LeaseID, reason)
	if stateErr == nil {
		result.ActiveLease = &updated
	}
	return result, errors.Join(operationErr, stateErr)
}

func (s *PackageDeterministicExecutionService) runHasDeterministicOutcome(ctx context.Context, runRowID int64) (bool, error) {
	artifacts, err := s.store.ListArtifactsByRun(ctx, runRowID)
	if err != nil {
		return false, err
	}
	for _, artifact := range artifacts {
		if artifact.Kind == deterministicOutcomeKind {
			return true, nil
		}
	}
	return false, nil
}

func (s *PackageDeterministicExecutionService) exactActiveRunLease(ctx context.Context, runID string, repository ExecutionAssignmentRepository) (workflowstore.RepositoryBranchMutationLease, error) {
	lease, err := s.store.GetActiveRepositoryBranchMutationLease(ctx, repository.Target, repository.Branch)
	if err != nil {
		return workflowstore.RepositoryBranchMutationLease{}, fmt.Errorf("existing partial deterministic outcome lease is unavailable: %w", err)
	}
	if !sameRunLease(lease, runID, repository) {
		return workflowstore.RepositoryBranchMutationLease{}, fmt.Errorf("existing partial deterministic outcome lease conflicts with Run")
	}
	return lease, nil
}

func (s *PackageDeterministicExecutionService) sameRunActiveLease(ctx context.Context, runID string, repository ExecutionAssignmentRepository) (*workflowstore.RepositoryBranchMutationLease, error) {
	lease, err := s.store.GetActiveRepositoryBranchMutationLease(ctx, repository.Target, repository.Branch)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !sameRunLease(lease, runID, repository) {
		return nil, nil
	}
	return &lease, nil
}

func sameRunLease(lease workflowstore.RepositoryBranchMutationLease, runID string, repository ExecutionAssignmentRepository) bool {
	return lease.OwnerKind == "run_execution" && lease.OwnerIdentity == runID && lease.RepoTarget == repository.Target && lease.Branch == repository.Branch && lease.State == workflowstore.RepositoryBranchMutationLeaseStateActive
}

func validatePackageDeterministicAssignment(authority executionpackages.ApprovedAuthority, assignment ExecutionAssignmentResult) error {
	expected, _, _, err := buildExecutionAssignment(authority)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(assignment.Assignment, expected) {
		return ErrPackageDeterministicExecutionConflict
	}
	return nil
}

func validatePackageDeterministicPreflight(result DeterministicPreflightResult, operations ExecutionAssignmentOperations) error {
	if operations.Presence != "present" || (operations.Coverage != "partial" && operations.Coverage != "complete") || result.Coverage != operations.Coverage {
		return fmt.Errorf("deterministic preflight coverage conflicts with approved operations")
	}
	switch result.Status {
	case DeterministicPreflightFailed:
		if result.Plan != nil || result.Failure == nil {
			return fmt.Errorf("preflight_failed result must contain structured failure evidence and no plan")
		}
	case DeterministicPreflightReady:
		if result.Failure != nil {
			return fmt.Errorf("ready deterministic preflight contains failure evidence")
		}
	default:
		return fmt.Errorf("unsupported deterministic preflight status %q", result.Status)
	}
	return nil
}

func validatePersistedPackageDeterministicOutcome(outcome DeterministicOutcomeResult, preflight DeterministicPreflightResult, application DeterministicApplicationResult) error {
	if preflight.Status == DeterministicPreflightReady {
		if outcome.Outcome.Outcome.Status != "applied" || outcome.Outcome.Outcome.Coverage != application.Coverage || outcome.Outcome.Application == nil {
			return ErrPackageDeterministicExecutionConflict
		}
		model, err := validateDeterministicPlan(preflight.Plan)
		if err != nil || !reflect.DeepEqual(*outcome.Outcome.Application, applicationEvidence(model, application.ChangedPaths)) {
			return ErrPackageDeterministicExecutionConflict
		}
		return nil
	}
	if outcome.Outcome.Outcome.Status != string(preflight.Status) || outcome.Outcome.Application != nil {
		return ErrPackageDeterministicExecutionConflict
	}
	if preflight.Status == DeterministicPreflightNotPresent && (outcome.Outcome.Outcome.Coverage != "" || outcome.Outcome.PreflightFailure != nil) {
		return ErrPackageDeterministicExecutionConflict
	}
	if preflight.Status == DeterministicPreflightFailed && (outcome.Outcome.Outcome.Coverage != preflight.Coverage || outcome.Outcome.PreflightFailure == nil) {
		return ErrPackageDeterministicExecutionConflict
	}
	return nil
}
