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
	workflowstore "relay/internal/store/workflow"
)

var ErrAdaptiveDispatchAdmissionConflict = errors.New("adaptive dispatch admission conflicts with durable execution state")

type AdaptiveDispatchAdmissionInput struct {
	RunID     string
	AttemptID string
}

type AdaptiveDispatchAdmissionResult struct {
	Mode                     EffectiveExecutorBriefMode
	AdaptiveDispatchRequired bool
	NewlyAdmitted            bool

	Run     *workflowstore.Run
	Attempt *workflowstore.ExecutionAttempt
	Lease   *workflowstore.RepositoryBranchMutationLease

	EffectiveBriefArtifact *workflowstore.Artifact
	EffectiveBriefBytes    []byte
	InputArtifact          *workflowstore.Artifact
	InputBytes             []byte
}

// adaptiveDispatchRuntime is the canonical pre-launch runtime record. Unlike
// the later execution runtime, source_mutation_started is deliberately
// explicit so reconciliation can distinguish admission from source mutation.
type adaptiveDispatchRuntime struct {
	MutationLeaseID          string                     `json:"mutation_lease_id"`
	SourceMutationStarted    bool                       `json:"source_mutation_started"`
	EffectiveBriefArtifactID string                     `json:"effective_brief_artifact_id"`
	EffectiveBriefSHA256     string                     `json:"effective_brief_sha256"`
	EffectiveBriefMode       EffectiveExecutorBriefMode `json:"effective_brief_mode"`
}

// AdaptiveDispatchAdmissionService atomically turns verified preparation into
// durable dispatch permission. It does not resolve repositories or launch an
// adapter; a later dispatcher must launch only for NewlyAdmitted results.
type AdaptiveDispatchAdmissionService struct {
	store    *workflowstore.Store
	briefs   *EffectiveExecutorBriefService
	attempts *AdaptiveExecutionAttemptService
	runs     *workflowruns.Service
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
	briefs, err := NewEffectiveExecutorBriefService(store, sourceVaults)
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
	return &AdaptiveDispatchAdmissionService{store: store, briefs: briefs, attempts: attempts, runs: runs}, nil
}

func (s *AdaptiveDispatchAdmissionService) Begin(ctx context.Context, input AdaptiveDispatchAdmissionInput) (AdaptiveDispatchAdmissionResult, error) {
	if s == nil || s.store == nil || s.briefs == nil || s.attempts == nil || s.runs == nil {
		return AdaptiveDispatchAdmissionResult{}, fmt.Errorf("adaptive dispatch admission service is unavailable")
	}
	if strings.TrimSpace(input.RunID) == "" {
		return AdaptiveDispatchAdmissionResult{}, fmt.Errorf("Run ID is required")
	}
	prepared, err := s.attempts.Load(ctx, input.RunID)
	if err != nil {
		return AdaptiveDispatchAdmissionResult{}, admissionConflict(err)
	}
	if prepared.Mode == EffectiveExecutorBriefDeterministicComplete {
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
	brief, err := s.briefs.Load(ctx, input.RunID)
	if err != nil {
		return AdaptiveDispatchAdmissionResult{}, admissionConflict(err)
	}
	if brief.Mode != prepared.Mode || brief.Artifact == nil || !brief.AdaptiveDispatchRequired || len(brief.Bytes) == 0 {
		return AdaptiveDispatchAdmissionResult{}, ErrAdaptiveDispatchAdmissionConflict
	}
	run, err := s.store.GetRunByRunID(ctx, input.RunID)
	if err != nil {
		return AdaptiveDispatchAdmissionResult{}, err
	}
	sourceMutationStarted, modeValid := adaptiveSourceMutationStarted(brief.Mode)
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
	runtimeJSON, err := marshalAdaptiveDispatchRuntime(leaseID, *brief.Artifact, brief.Mode)
	if err != nil {
		return AdaptiveDispatchAdmissionResult{}, err
	}
	admitted, err := s.runs.BeginPreparedAdaptiveExecution(ctx, workflowruns.BeginPreparedAdaptiveExecutionInput{
		RunID: input.RunID, RunRowID: run.ID,
		AttemptID: prepared.Attempt.AttemptID, AttemptRowID: prepared.Attempt.ID, AttemptNumber: prepared.Attempt.AttemptNumber,
		Adapter: prepared.Attempt.Adapter, Model: prepared.Attempt.Model,
		InputArtifactRowID: prepared.InputArtifact.ID, InputArtifactSHA256: prepared.InputArtifact.SHA256,
		EffectiveBriefArtifactRowID: brief.Artifact.ID, EffectiveBriefArtifactID: brief.Artifact.ArtifactID, EffectiveBriefSHA256: brief.Artifact.SHA256, EffectiveBriefMode: string(brief.Mode),
		ProposedLeaseID: leaseID, RunningResultJSON: string(runtimeJSON),
	})
	if err != nil {
		return AdaptiveDispatchAdmissionResult{}, admissionConflict(err)
	}
	return adaptiveDispatchAdmissionResult(prepared, brief, admitted), nil
}

func marshalAdaptiveDispatchRuntime(leaseID string, brief workflowstore.Artifact, mode EffectiveExecutorBriefMode) ([]byte, error) {
	sourceMutationStarted, modeValid := adaptiveSourceMutationStarted(mode)
	if !modeValid {
		return nil, ErrAdaptiveDispatchAdmissionConflict
	}
	state := adaptiveDispatchRuntime{MutationLeaseID: leaseID, SourceMutationStarted: sourceMutationStarted, EffectiveBriefArtifactID: brief.ArtifactID, EffectiveBriefSHA256: brief.SHA256, EffectiveBriefMode: mode}
	content, err := json.Marshal(state)
	if err != nil {
		return nil, err
	}
	content = append(content, '\n')
	var verified adaptiveDispatchRuntime
	if !json.Valid(content) || json.Unmarshal(content, &verified) != nil || verified.MutationLeaseID != leaseID || verified.SourceMutationStarted != sourceMutationStarted || verified.EffectiveBriefArtifactID != brief.ArtifactID || verified.EffectiveBriefSHA256 != brief.SHA256 || verified.EffectiveBriefMode != mode {
		return nil, ErrAdaptiveDispatchAdmissionConflict
	}
	return content, nil
}

func adaptiveSourceMutationStarted(mode EffectiveExecutorBriefMode) (bool, bool) {
	switch mode {
	case EffectiveExecutorBriefAdaptiveNoOperations, EffectiveExecutorBriefAdaptivePreflightFailed:
		return false, true
	case EffectiveExecutorBriefAdaptiveAfterPartialApplication:
		return true, true
	default:
		return false, false
	}
}

func adaptiveDispatchAdmissionResult(prepared AdaptiveExecutionAttemptResult, brief EffectiveExecutorBriefResult, admitted workflowruns.BeginPreparedAdaptiveExecutionResult) AdaptiveDispatchAdmissionResult {
	run, attempt, lease := admitted.Run, admitted.Attempt, admitted.Lease
	briefArtifact, inputArtifact := *brief.Artifact, *prepared.InputArtifact
	return AdaptiveDispatchAdmissionResult{
		Mode: prepared.Mode, AdaptiveDispatchRequired: true, NewlyAdmitted: admitted.NewlyAdmitted,
		Run: &run, Attempt: &attempt, Lease: &lease,
		EffectiveBriefArtifact: &briefArtifact, EffectiveBriefBytes: append([]byte(nil), brief.Bytes...),
		InputArtifact: &inputArtifact, InputBytes: append([]byte(nil), prepared.InputBytes...),
	}
}

func admissionConflict(err error) error {
	if errors.Is(err, workflowruns.ErrMutationLeaseConflict) {
		return err
	}
	if errors.Is(err, ErrAdaptiveExecutionAttemptConflict) || errors.Is(err, ErrEffectiveExecutorBriefConflict) || errors.Is(err, workflowruns.ErrPreparedAdaptiveExecutionConflict) {
		return fmt.Errorf("%w: %v", ErrAdaptiveDispatchAdmissionConflict, err)
	}
	return err
}
