package executor

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	executionpackages "relay/internal/app/packages"
	workflowruns "relay/internal/app/runs/workflow"
	"relay/internal/speccompiler"
	workflowstore "relay/internal/store/workflow"
)

var ErrAdaptiveDispatchAdmissionConflict = errors.New("adaptive dispatch admission conflicts with durable execution state")

type AdaptiveDispatchAdmissionInput struct {
	RunID     string
	AttemptID string
}

type AdaptiveDispatchAdmissionResult struct {
	Mode                     ExecutionMode
	AdaptiveDispatchRequired bool
	NewlyAdmitted            bool

	Run     *workflowstore.Run
	Attempt *workflowstore.ExecutionAttempt
	Lease   *workflowstore.RepositoryBranchMutationLease

	AssignmentArtifact *workflowstore.Artifact
	AssignmentBytes    []byte
	InputArtifact      *workflowstore.Artifact
	InputBytes         []byte

	ValidationCommands []speccompiler.ProjectedValidationCommand
}

// adaptiveDispatchRuntime is the canonical pre-launch runtime record. Unlike
// the later execution runtime, source_mutation_started is deliberately
// explicit so reconciliation can distinguish admission from source mutation.
// The execution_assignment_* keys and legacy mode strings are the external
// workflowruns BeginPreparedAdaptiveExecution wire contract, which still
// requires them; the run-owned artifact identity carried here is the verified
// ExecutionAssignment artifact.
type adaptiveDispatchRuntime struct {
	MutationLeaseID       string `json:"mutation_lease_id"`
	SourceMutationStarted bool   `json:"source_mutation_started"`
	AssignmentArtifactID  string `json:"execution_assignment_artifact_id"`
	AssignmentSHA256      string `json:"execution_assignment_sha256"`
	AssignmentMode        string `json:"execution_assignment_mode"`
}

// AdaptiveDispatchAdmissionService atomically turns verified preparation into
// durable dispatch permission. It does not resolve repositories or launch an
// adapter; a later dispatcher must launch only for NewlyAdmitted results.
type AdaptiveDispatchAdmissionService struct {
	store       *workflowstore.Store
	outcomes    *DeterministicOutcomeService
	attempts    *AdaptiveExecutionAttemptService
	runs        *workflowruns.Service
	assignments *ExecutionAssignmentService
}

func NewAdaptiveDispatchAdmissionService(
	store *workflowstore.Store,
	sourceVaults executionpackages.SourceVaultReader,
) (*AdaptiveDispatchAdmissionService, error) {
	if store == nil {
		return nil, fmt.Errorf("workflow store is required")
	}
	if sourceVaults == nil {
		return nil, fmt.Errorf("source-vault reader is required")
	}
	outcomes, err := NewDeterministicOutcomeService(store, sourceVaults)
	if err != nil {
		return nil, err
	}
	attempts, err := NewAdaptiveExecutionAttemptService(store, sourceVaults)
	if err != nil {
		return nil, err
	}
	runs, err := workflowruns.NewService(store)
	if err != nil {
		return nil, err
	}
	assignments, err := NewExecutionAssignmentService(store, sourceVaults)
	if err != nil {
		return nil, err
	}
	return &AdaptiveDispatchAdmissionService{store: store, outcomes: outcomes, attempts: attempts, runs: runs, assignments: assignments}, nil
}

func (s *AdaptiveDispatchAdmissionService) Begin(ctx context.Context, input AdaptiveDispatchAdmissionInput) (AdaptiveDispatchAdmissionResult, error) {
	if s == nil || s.store == nil || s.outcomes == nil || s.attempts == nil || s.runs == nil || s.assignments == nil {
		return AdaptiveDispatchAdmissionResult{}, fmt.Errorf("adaptive dispatch admission service is unavailable")
	}
	if strings.TrimSpace(input.RunID) == "" {
		return AdaptiveDispatchAdmissionResult{}, fmt.Errorf("Run ID is required")
	}
	prepared, err := s.attempts.Load(ctx, input.RunID)
	if err != nil {
		return AdaptiveDispatchAdmissionResult{}, admissionConflict(err)
	}
	if prepared.Mode == ExecutionModeCompleteApplied {
		run, err := s.store.GetRunByRunID(ctx, input.RunID)
		if err != nil {
			return AdaptiveDispatchAdmissionResult{}, err
		}
		attempts, err := s.store.ListExecutionAttemptsByRun(ctx, run.ID)
		if err != nil {
			return AdaptiveDispatchAdmissionResult{}, err
		}
		if len(attempts) != 0 {
			return AdaptiveDispatchAdmissionResult{}, ErrAdaptiveDispatchAdmissionConflict
		}
		_, leaseErr := s.runs.GetActiveRunMutationLease(ctx, run.RunID)
		if leaseErr == nil {
			return AdaptiveDispatchAdmissionResult{}, ErrAdaptiveDispatchAdmissionConflict
		}
		if leaseErr != nil && !errors.Is(leaseErr, sql.ErrNoRows) && !errors.Is(leaseErr, workflowruns.ErrMutationLeaseOwner) {
			return AdaptiveDispatchAdmissionResult{}, leaseErr
		}
		return AdaptiveDispatchAdmissionResult{Mode: prepared.Mode}, nil
	}
	if !prepared.AdaptiveDispatchRequired || prepared.Attempt == nil || prepared.InputArtifact == nil || len(prepared.InputBytes) == 0 {
		return AdaptiveDispatchAdmissionResult{}, ErrAdaptiveDispatchAdmissionConflict
	}
	if strings.TrimSpace(input.AttemptID) == "" || input.AttemptID != prepared.Attempt.AttemptID {
		return AdaptiveDispatchAdmissionResult{}, ErrAdaptiveDispatchAdmissionConflict
	}
	assignment, err := s.assignments.LoadExecutionAssignment(ctx, input.RunID)
	if err != nil {
		return AdaptiveDispatchAdmissionResult{}, admissionConflict(err)
	}
	outcome, err := s.outcomes.Load(ctx, input.RunID)
	if err != nil {
		return AdaptiveDispatchAdmissionResult{}, admissionConflict(err)
	}
	mode, err := executionModeFromOutcome(outcome.Outcome.Outcome)
	if err != nil || mode != prepared.Mode {
		return AdaptiveDispatchAdmissionResult{}, ErrAdaptiveDispatchAdmissionConflict
	}
	run, err := s.store.GetRunByRunID(ctx, input.RunID)
	if err != nil {
		return AdaptiveDispatchAdmissionResult{}, err
	}
	if assignment.Assignment.Run.RunID != run.RunID || assignment.Assignment.Run.RunRowID != run.ID {
		return AdaptiveDispatchAdmissionResult{}, ErrAdaptiveDispatchAdmissionConflict
	}
	sourceMutationStarted, modeValid := adaptiveSourceMutationStarted(mode)
	if !modeValid {
		return AdaptiveDispatchAdmissionResult{}, ErrAdaptiveDispatchAdmissionConflict
	}
	leaseID := workflowstore.NewRepositoryBranchMutationLeaseID()
	if sourceMutationStarted {
		lease, leaseErr := s.runs.GetActiveRunMutationLease(ctx, run.RunID)
		if leaseErr != nil {
			if errors.Is(leaseErr, sql.ErrNoRows) || errors.Is(leaseErr, workflowruns.ErrMutationLeaseOwner) {
				return AdaptiveDispatchAdmissionResult{}, ErrAdaptiveDispatchAdmissionConflict
			}
			return AdaptiveDispatchAdmissionResult{}, leaseErr
		}
		if lease.OwnerKind != "run_execution" || lease.OwnerIdentity != run.RunID || lease.RepoTarget != run.RepoTarget || lease.Branch != run.Branch || lease.State != workflowstore.RepositoryBranchMutationLeaseStateActive || lease.UncertaintyState != workflowstore.RepositoryBranchMutationLeaseCertaintyCertain || lease.ReconciliationState != workflowstore.RepositoryBranchMutationLeaseReconciliationNotRequired {
			return AdaptiveDispatchAdmissionResult{}, ErrAdaptiveDispatchAdmissionConflict
		}
		leaseID = lease.LeaseID
	}
	wireMode, modeValid := workflowrunsModeString(mode)
	if !modeValid {
		return AdaptiveDispatchAdmissionResult{}, ErrAdaptiveDispatchAdmissionConflict
	}
	runtimeJSON, err := marshalAdaptiveDispatchRuntime(leaseID, assignment.Artifact, mode, wireMode)
	if err != nil {
		return AdaptiveDispatchAdmissionResult{}, err
	}
	admitted, err := s.runs.BeginPreparedAdaptiveExecution(ctx, workflowruns.BeginPreparedAdaptiveExecutionInput{
		RunID: input.RunID, RunRowID: run.ID,
		AttemptID: prepared.Attempt.AttemptID, AttemptRowID: prepared.Attempt.ID, AttemptNumber: prepared.Attempt.AttemptNumber,
		Adapter: prepared.Attempt.Adapter, Model: prepared.Attempt.Model,
		InputArtifactRowID: prepared.InputArtifact.ID, InputArtifactSHA256: prepared.InputArtifact.SHA256,
		ExecutionAssignmentArtifactRowID: assignment.Artifact.ID, ExecutionAssignmentArtifactID: assignment.Artifact.ArtifactID, ExecutionAssignmentSHA256: assignment.Artifact.SHA256, ExecutionAssignmentMode: wireMode,
		ProposedLeaseID: leaseID, RunningResultJSON: string(runtimeJSON),
	})
	if err != nil {
		return AdaptiveDispatchAdmissionResult{}, admissionConflict(err)
	}

	validationCmds := make([]speccompiler.ProjectedValidationCommand, 0, len(assignment.Assignment.ValidationCommands))
	for _, cmd := range assignment.Assignment.ValidationCommands {
		validationCmds = append(validationCmds, speccompiler.ProjectedValidationCommand{
			Command:          cmd.Command,
			WorkingDirectory: cmd.WorkingDirectory,
			Expected:         cmd.Expected,
		})
	}

	return adaptiveDispatchAdmissionResult(prepared, assignment, admitted, validationCmds), nil
}

func marshalAdaptiveDispatchRuntime(leaseID string, assignment workflowstore.Artifact, mode ExecutionMode, wireMode string) ([]byte, error) {
	sourceMutationStarted, modeValid := adaptiveSourceMutationStarted(mode)
	if !modeValid || wireMode == "" {
		return nil, ErrAdaptiveDispatchAdmissionConflict
	}
	state := adaptiveDispatchRuntime{MutationLeaseID: leaseID, SourceMutationStarted: sourceMutationStarted, AssignmentArtifactID: assignment.ArtifactID, AssignmentSHA256: assignment.SHA256, AssignmentMode: wireMode}
	content, err := json.Marshal(state)
	if err != nil {
		return nil, err
	}
	content = append(content, '\n')
	var verified adaptiveDispatchRuntime
	if !json.Valid(content) || json.Unmarshal(content, &verified) != nil || verified.MutationLeaseID != leaseID || verified.SourceMutationStarted != sourceMutationStarted || verified.AssignmentArtifactID != assignment.ArtifactID || verified.AssignmentSHA256 != assignment.SHA256 || verified.AssignmentMode != wireMode {
		return nil, ErrAdaptiveDispatchAdmissionConflict
	}
	return content, nil
}

func adaptiveDispatchAdmissionResult(prepared AdaptiveExecutionAttemptResult, assignment ExecutionAssignmentResult, admitted workflowruns.BeginPreparedAdaptiveExecutionResult, validationCommands []speccompiler.ProjectedValidationCommand) AdaptiveDispatchAdmissionResult {
	run, attempt, lease := admitted.Run, admitted.Attempt, admitted.Lease
	assignmentArtifact, inputArtifact := assignment.Artifact, *prepared.InputArtifact
	return AdaptiveDispatchAdmissionResult{
		Mode: prepared.Mode, AdaptiveDispatchRequired: true, NewlyAdmitted: admitted.NewlyAdmitted,
		Run: &run, Attempt: &attempt, Lease: &lease,
		AssignmentArtifact: &assignmentArtifact, AssignmentBytes: append([]byte(nil), assignment.Bytes...),
		InputArtifact: &inputArtifact, InputBytes: append([]byte(nil), prepared.InputBytes...),
		ValidationCommands: append([]speccompiler.ProjectedValidationCommand(nil), validationCommands...),
	}
}

func admissionConflict(err error) error {
	if errors.Is(err, workflowruns.ErrMutationLeaseConflict) {
		return err
	}
	if errors.Is(err, ErrAdaptiveExecutionAttemptConflict) || errors.Is(err, ErrExecutionAssignmentConflict) || errors.Is(err, ErrDeterministicOutcomeConflict) || errors.Is(err, workflowruns.ErrPreparedAdaptiveExecutionConflict) {
		return fmt.Errorf("%w: %v", ErrAdaptiveDispatchAdmissionConflict, err)
	}
	return err
}
