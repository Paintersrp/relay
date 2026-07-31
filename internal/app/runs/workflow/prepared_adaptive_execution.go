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

var ErrPreparedAdaptiveExecutionConflict = errors.New("prepared adaptive execution conflicts with durable execution state")

type preparedAdaptiveRuntime struct {
	MutationLeaseID          string `json:"mutation_lease_id"`
	SourceMutationStarted    bool   `json:"source_mutation_started"`
	EffectiveBriefArtifactID string `json:"effective_brief_artifact_id"`
	EffectiveBriefSHA256     string `json:"effective_brief_sha256"`
	EffectiveBriefMode       string `json:"effective_brief_mode"`
}

// BeginPreparedAdaptiveExecution atomically admits a verified existing
// attempt. It deliberately has no adapter, filesystem, or process side
// effects; only the durable execution state changes in this transaction.
func (s *Service) BeginPreparedAdaptiveExecution(ctx context.Context, input BeginPreparedAdaptiveExecutionInput) (BeginPreparedAdaptiveExecutionResult, error) {
	if s == nil || s.store == nil || !validPreparedAdaptiveExecutionInput(input) {
		return BeginPreparedAdaptiveExecutionResult{}, fmt.Errorf("%w: invalid prepared adaptive execution input", ErrPreparedAdaptiveExecutionConflict)
	}
	var result BeginPreparedAdaptiveExecutionResult
	err := s.store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		run, err := tx.GetRunByRunID(ctx, input.RunID)
		if err != nil {
			return err
		}
		if run.ID != input.RunRowID || !run.ExecutionPackageRowID.Valid {
			return ErrPreparedAdaptiveExecutionConflict
		}
		attempt, err := tx.GetExecutionAttemptByAttemptID(ctx, input.AttemptID)
		if err != nil {
			return err
		}
		if !preparedAttemptMatches(run, attempt, input) {
			return ErrPreparedAdaptiveExecutionConflict
		}
		if err := verifyPreparedArtifact(ctx, tx, input.InputArtifactRowID, input.InputArtifactSHA256, workflowstore.ArtifactOwnerExecutionAttempt, attempt.ID); err != nil {
			return err
		}
		if err := verifyPreparedArtifact(ctx, tx, input.EffectiveBriefArtifactRowID, input.EffectiveBriefSHA256, workflowstore.ArtifactOwnerRun, run.ID); err != nil {
			return err
		}

		switch {
		case run.Status == workflowstore.RunStatusSetupReady && attempt.Status == workflowstore.AttemptStatusPending:
			if attempt.CancellationRequestedAt.Valid {
				return ErrPreparedAdaptiveExecutionConflict
			}
			var lease workflowstore.RepositoryBranchMutationLease
			if input.EffectiveBriefMode == preparedAdaptiveModeAfterPartialApplication {
				lease, err = tx.GetActiveRepositoryBranchMutationLease(ctx, run.RepoTarget, run.Branch)
				if err != nil {
					if errors.Is(err, sql.ErrNoRows) {
						return ErrPreparedAdaptiveExecutionConflict
					}
					return err
				}
				if !validPreparedPartialLease(run, lease, input.ProposedLeaseID) {
					if lease.OwnerKind != runMutationLeaseOwnerKind || lease.OwnerIdentity != run.RunID {
						return fmt.Errorf("%w: %s/%s", ErrMutationLeaseConflict, run.RepoTarget, run.Branch)
					}
					return ErrPreparedAdaptiveExecutionConflict
				}
			} else {
				if active, activeErr := tx.GetActiveRepositoryBranchMutationLease(ctx, run.RepoTarget, run.Branch); activeErr == nil {
					if active.OwnerKind == runMutationLeaseOwnerKind && active.OwnerIdentity == run.RunID {
						return ErrPreparedAdaptiveExecutionConflict
					}
					return fmt.Errorf("%w: %s/%s", ErrMutationLeaseConflict, run.RepoTarget, run.Branch)
				} else if !errors.Is(activeErr, sql.ErrNoRows) {
					return activeErr
				}
				lease, err = tx.CreateRepositoryBranchMutationLease(ctx, workflowstore.CreateRepositoryBranchMutationLeaseParams{
					LeaseID: input.ProposedLeaseID, RepoTarget: run.RepoTarget, Branch: run.Branch,
					OwnerKind: runMutationLeaseOwnerKind, OwnerIdentity: run.RunID,
					UncertaintyState:    workflowstore.RepositoryBranchMutationLeaseCertaintyCertain,
					ReconciliationState: workflowstore.RepositoryBranchMutationLeaseReconciliationNotRequired,
				})
				if err != nil {
					if isMutationLeaseUniqueConflict(err) {
						return fmt.Errorf("%w: %s/%s", ErrMutationLeaseConflict, run.RepoTarget, run.Branch)
					}
					return err
				}
			}
			run, err = tx.TransitionRun(ctx, run.RunID, workflowstore.RunStatusSetupReady, workflowstore.RunStatusExecuting)
			if err != nil {
				return err
			}
			attempt, err = tx.TransitionExecutionAttempt(ctx, attempt.AttemptID, workflowstore.AttemptStatusPending, workflowstore.AttemptStatusRunning, input.RunningResultJSON)
			if err != nil {
				return err
			}
			result = BeginPreparedAdaptiveExecutionResult{Run: run, Attempt: attempt, Lease: lease, NewlyAdmitted: true}
			return nil
		case run.Status == workflowstore.RunStatusExecuting && attempt.Status == workflowstore.AttemptStatusRunning:
			lease, err := tx.GetActiveRepositoryBranchMutationLease(ctx, run.RepoTarget, run.Branch)
			if err != nil {
				return ErrPreparedAdaptiveExecutionConflict
			}
			if !validPreparedRunningAdmission(attempt.ResultJSON, input, run, lease) {
				return ErrPreparedAdaptiveExecutionConflict
			}
			result = BeginPreparedAdaptiveExecutionResult{Run: run, Attempt: attempt, Lease: lease}
			return nil
		default:
			return ErrPreparedAdaptiveExecutionConflict
		}
	})
	return result, err
}

func validPreparedAdaptiveExecutionInput(input BeginPreparedAdaptiveExecutionInput) bool {
	if strings.TrimSpace(input.RunID) == "" || input.RunRowID < 1 || strings.TrimSpace(input.AttemptID) == "" || input.AttemptRowID < 1 || input.AttemptNumber != 1 || strings.TrimSpace(input.Adapter) == "" || strings.TrimSpace(input.Model) == "" || input.InputArtifactRowID < 1 || input.EffectiveBriefArtifactRowID < 1 || strings.TrimSpace(input.EffectiveBriefArtifactID) == "" || !validSHA256(input.InputArtifactSHA256) || !validSHA256(input.EffectiveBriefSHA256) || strings.TrimSpace(input.EffectiveBriefMode) == "" || strings.TrimSpace(input.ProposedLeaseID) == "" || !json.Valid([]byte(input.RunningResultJSON)) {
		return false
	}
	state, valid := parsePreparedAdaptiveRuntime(input.RunningResultJSON)
	expectedMutationStarted, modeValid := preparedAdaptiveSourceMutationStarted(input.EffectiveBriefMode)
	return valid && modeValid && state.MutationLeaseID == input.ProposedLeaseID && state.SourceMutationStarted == expectedMutationStarted && state.EffectiveBriefSHA256 == input.EffectiveBriefSHA256 && state.EffectiveBriefMode == input.EffectiveBriefMode && state.EffectiveBriefArtifactID == input.EffectiveBriefArtifactID
}

func preparedAttemptMatches(run workflowstore.Run, attempt workflowstore.ExecutionAttempt, input BeginPreparedAdaptiveExecutionInput) bool {
	return attempt.ID == input.AttemptRowID && attempt.RunRowID == run.ID && attempt.AttemptNumber == input.AttemptNumber && attempt.Adapter == input.Adapter && attempt.Model == input.Model
}

func verifyPreparedArtifact(ctx context.Context, tx *workflowstore.Tx, rowID int64, digest, ownerType string, ownerRowID int64) error {
	artifact, err := tx.GetArtifactByRowID(ctx, rowID)
	if err != nil {
		return err
	}
	if artifact.SHA256 != digest || artifact.OwnerType != ownerType {
		return ErrPreparedAdaptiveExecutionConflict
	}
	if ownerType == workflowstore.ArtifactOwnerRun && (!artifact.RunRowID.Valid || artifact.RunRowID.Int64 != ownerRowID) {
		return ErrPreparedAdaptiveExecutionConflict
	}
	if ownerType == workflowstore.ArtifactOwnerExecutionAttempt && (!artifact.ExecutionAttemptRowID.Valid || artifact.ExecutionAttemptRowID.Int64 != ownerRowID) {
		return ErrPreparedAdaptiveExecutionConflict
	}
	return nil
}

func validPreparedRunningAdmission(resultJSON string, input BeginPreparedAdaptiveExecutionInput, run workflowstore.Run, lease workflowstore.RepositoryBranchMutationLease) bool {
	state, valid := parsePreparedAdaptiveRuntime(resultJSON)
	expectedMutationStarted, modeValid := preparedAdaptiveSourceMutationStarted(input.EffectiveBriefMode)
	return valid && modeValid && state.MutationLeaseID != "" && state.MutationLeaseID == lease.LeaseID && state.SourceMutationStarted == expectedMutationStarted && state.EffectiveBriefArtifactID == input.EffectiveBriefArtifactID && state.EffectiveBriefSHA256 == input.EffectiveBriefSHA256 && state.EffectiveBriefMode == input.EffectiveBriefMode && lease.OwnerKind == runMutationLeaseOwnerKind && lease.OwnerIdentity == run.RunID && lease.RepoTarget == run.RepoTarget && lease.Branch == run.Branch && lease.State == workflowstore.RepositoryBranchMutationLeaseStateActive && lease.UncertaintyState == workflowstore.RepositoryBranchMutationLeaseCertaintyCertain && lease.ReconciliationState == workflowstore.RepositoryBranchMutationLeaseReconciliationNotRequired
}

const preparedAdaptiveModeAfterPartialApplication = "adaptive_after_partial_application"

func preparedAdaptiveSourceMutationStarted(mode string) (bool, bool) {
	switch mode {
	case "adaptive_no_operations", "adaptive_preflight_failed":
		return false, true
	case preparedAdaptiveModeAfterPartialApplication:
		return true, true
	default:
		return false, false
	}
}

func validPreparedPartialLease(run workflowstore.Run, lease workflowstore.RepositoryBranchMutationLease, proposedLeaseID string) bool {
	return lease.LeaseID == proposedLeaseID && lease.OwnerKind == runMutationLeaseOwnerKind && lease.OwnerIdentity == run.RunID && lease.RepoTarget == run.RepoTarget && lease.Branch == run.Branch && lease.State == workflowstore.RepositoryBranchMutationLeaseStateActive && lease.UncertaintyState == workflowstore.RepositoryBranchMutationLeaseCertaintyCertain && lease.ReconciliationState == workflowstore.RepositoryBranchMutationLeaseReconciliationNotRequired
}

func parsePreparedAdaptiveRuntime(content string) (preparedAdaptiveRuntime, bool) {
	var fields map[string]json.RawMessage
	var state preparedAdaptiveRuntime
	if json.Unmarshal([]byte(content), &fields) != nil || fields == nil || json.Unmarshal([]byte(content), &state) != nil {
		return preparedAdaptiveRuntime{}, false
	}
	for _, key := range []string{"mutation_lease_id", "source_mutation_started", "effective_brief_artifact_id", "effective_brief_sha256", "effective_brief_mode"} {
		if _, ok := fields[key]; !ok {
			return preparedAdaptiveRuntime{}, false
		}
	}
	return state, true
}
