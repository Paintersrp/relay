package executor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	workflowruns "relay/internal/app/runs/workflow"
	"relay/internal/speccompiler"
	workflowstore "relay/internal/store/workflow"
)

type PreparedAdaptiveLaunchInput struct {
	RunID     string
	AttemptID string
}

type PreparedAdaptiveLaunchResult struct {
	Mode                     EffectiveExecutorBriefMode
	AdaptiveDispatchRequired bool
	NewlyAdmitted            bool
	NewlyLaunched            bool

	Run     *workflowstore.Run
	Attempt *workflowstore.ExecutionAttempt
	Lease   *workflowstore.RepositoryBranchMutationLease
}

// LaunchPreparedAdaptive admits exactly one prepared package attempt and
// launches only when that admission newly crossed the durable cutover.
func (s *Execution) LaunchPreparedAdaptive(ctx context.Context, input PreparedAdaptiveLaunchInput) (PreparedAdaptiveLaunchResult, error) {
	if s == nil || s.store == nil || s.runs == nil || s.adaptiveAdmission == nil {
		return PreparedAdaptiveLaunchResult{}, fmt.Errorf("prepared adaptive launch service is unavailable")
	}
	input.RunID = strings.TrimSpace(input.RunID)
	input.AttemptID = strings.TrimSpace(input.AttemptID)
	if input.RunID == "" {
		return PreparedAdaptiveLaunchResult{}, fmt.Errorf("run_id is required")
	}

	admitted, err := s.adaptiveAdmission.Begin(ctx, AdaptiveDispatchAdmissionInput{
		RunID: input.RunID, AttemptID: input.AttemptID,
	})
	if err != nil {
		return PreparedAdaptiveLaunchResult{}, err
	}
	result := preparedAdaptiveLaunchResult(admitted)
	if admitted.Mode == EffectiveExecutorBriefDeterministicComplete {
		return PreparedAdaptiveLaunchResult{Mode: admitted.Mode}, nil
	}
	if !admitted.AdaptiveDispatchRequired || admitted.Run == nil || admitted.Attempt == nil || admitted.Lease == nil || admitted.EffectiveBriefArtifact == nil || len(admitted.EffectiveBriefBytes) == 0 {
		return result, fmt.Errorf("adaptive admission returned incomplete launch identities")
	}
	if !admitted.NewlyAdmitted {
		return result, nil
	}

	run := *admitted.Run
	attempt := *admitted.Attempt
	lease := *admitted.Lease
	selected, err := preparedEffectiveBriefInput(s.store.ArtifactStore().Root(), run, *admitted.EffectiveBriefArtifact, admitted.EffectiveBriefBytes, admitted.Mode)
	if err != nil {
		return result, s.settlePreparedPrelaunchFailure(ctx, admitted, nil, err)
	}

	repository, err := s.store.GetRepositoryTarget(ctx, run.RepoTarget)
	if err != nil {
		return result, s.settlePreparedPrelaunchFailure(ctx, admitted, &selected, fmt.Errorf("resolve repository target: %w", err))
	}
	if repository.RepoTarget != run.RepoTarget || strings.TrimSpace(repository.LocalPath) == "" {
		err := fmt.Errorf("registered repository target %q does not exactly match admitted Run target", run.RepoTarget)
		return result, s.settlePreparedPrelaunchFailure(ctx, admitted, &selected, err)
	}

	if s.adapterFactory == nil {
		err := fmt.Errorf("executor adapter factory is unavailable")
		return result, s.settlePreparedPrelaunchFailure(ctx, admitted, &selected, err)
	}
	adapter, err := s.adapterFactory(attempt.Adapter)
	if err != nil {
		return result, s.settlePreparedPrelaunchFailure(ctx, admitted, &selected, fmt.Errorf("construct executor adapter: %w", err))
	}
	if adapter == nil {
		err := fmt.Errorf("construct executor adapter: factory returned nil adapter")
		return result, s.settlePreparedPrelaunchFailure(ctx, admitted, &selected, err)
	}
	runtimeResultPath := filepath.Join(s.store.ArtifactStore().Root(), ".runtime", run.RunID, attempt.AttemptID, "executor-result.tmp")
	invocation, err := adapter.BuildInvocation(ExecutorAdapterRequest{
		RunID:         run.ID,
		RepoPath:      repository.LocalPath,
		BriefContent:  string(admitted.EffectiveBriefBytes),
		BriefPath:     selected.Path,
		ResultPath:    runtimeResultPath,
		SelectedModel: attempt.Model,
		Timeout:       s.timeout,
	})
	if err != nil {
		return result, s.settlePreparedPrelaunchFailure(ctx, admitted, &selected, fmt.Errorf("build executor invocation: %w", err))
	}
	if err := verifyPreparedInvocation(invocation, adapter, attempt, repository.LocalPath, runtimeResultPath, selected); err != nil {
		return result, s.settlePreparedPrelaunchFailure(ctx, admitted, &selected, err)
	}
	if s.invocationPreflight == nil {
		err := fmt.Errorf("executor invocation preflight is unavailable")
		return result, s.settlePreparedPrelaunchFailure(ctx, admitted, &selected, err)
	}
	invocationPreflight := s.invocationPreflight(invocation)
	if !invocationPreflight.OK {
		err := fmt.Errorf("adapter preflight failed: %s", invocationPreflight.BlockerText)
		return result, s.settlePreparedPrelaunchFailure(ctx, admitted, &selected, err)
	}

	sourceMutationStarted, modeValid := adaptiveSourceMutationStarted(admitted.Mode)
	if !modeValid {
		return result, s.settlePreparedPrelaunchFailure(ctx, admitted, &selected, ErrAdaptiveDispatchAdmissionConflict)
	}
	runtimeCtx, cancel := context.WithCancel(context.Background())
	runtime := &workflowRuntime{cancel: cancel}
	s.putRuntime(attempt.AttemptID, runtime)
	s.launch(func() {
		defer s.deleteRuntime(attempt.AttemptID)
		s.execute(runtimeCtx, run, attempt, repository, selected, nil, invocation, adapter, runtime, lease, sourceMutationStarted)
	})
	result.NewlyLaunched = true
	return result, nil
}

func preparedAdaptiveLaunchResult(admitted AdaptiveDispatchAdmissionResult) PreparedAdaptiveLaunchResult {
	return PreparedAdaptiveLaunchResult{
		Mode:                     admitted.Mode,
		AdaptiveDispatchRequired: admitted.AdaptiveDispatchRequired,
		NewlyAdmitted:            admitted.NewlyAdmitted,
		Run:                      admitted.Run,
		Attempt:                  admitted.Attempt,
		Lease:                    admitted.Lease,
	}
}

func preparedEffectiveBriefInput(root string, run workflowstore.Run, artifact workflowstore.Artifact, content []byte, mode EffectiveExecutorBriefMode) (effectiveBriefInput, error) {
	if artifact.OwnerType != workflowstore.ArtifactOwnerRun || !artifact.RunRowID.Valid || artifact.RunRowID.Int64 != run.ID || artifact.Kind != effectiveExecutorBriefKind || artifact.MediaType != effectiveExecutorBriefMediaType || strings.TrimSpace(artifact.ArtifactID) == "" || strings.TrimSpace(artifact.RelativePath) == "" || !validPreparedSHA256(artifact.SHA256) || artifact.SizeBytes != int64(len(content)) || len(content) == 0 {
		return effectiveBriefInput{}, fmt.Errorf("admitted effective Brief artifact identity is invalid")
	}
	digest := sha256.Sum256(content)
	if hex.EncodeToString(digest[:]) != artifact.SHA256 {
		return effectiveBriefInput{}, fmt.Errorf("admitted effective Brief bytes do not match artifact identity")
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return effectiveBriefInput{}, fmt.Errorf("resolve artifact-store root: %w", err)
	}
	absPath, err := filepath.Abs(filepath.Join(absRoot, filepath.FromSlash(artifact.RelativePath)))
	if err != nil {
		return effectiveBriefInput{}, fmt.Errorf("resolve effective Brief path: %w", err)
	}
	relative, err := filepath.Rel(absRoot, absPath)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return effectiveBriefInput{}, fmt.Errorf("effective Brief artifact path escapes the managed root")
	}
	return effectiveBriefInput{
		Mode:         speccompiler.EffectiveBriefFull,
		RecordedMode: string(mode),
		Content:      append([]byte(nil), content...),
		Artifact:     artifact,
		Path:         absPath,
	}, nil
}

func validPreparedSHA256(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func verifyPreparedInvocation(invocation ExecutorInvocation, adapter ExecutorAdapter, attempt workflowstore.ExecutionAttempt, repositoryPath string, expectedResultPath string, selected effectiveBriefInput) error {
	if invocation.Adapter != AdapterID(attempt.Adapter) {
		return fmt.Errorf("executor invocation adapter %q does not match admitted adapter %q", invocation.Adapter, attempt.Adapter)
	}
	if adapter.ID() != AdapterID(attempt.Adapter) {
		return fmt.Errorf("constructed adapter %q does not match admitted adapter %q", adapter.ID(), attempt.Adapter)
	}
	if invocation.Model != attempt.Model {
		return fmt.Errorf("executor invocation model %q does not match admitted model %q", invocation.Model, attempt.Model)
	}
	if invocation.WorkDir != repositoryPath {
		return fmt.Errorf("executor invocation working directory %q does not match registered repository path %q", invocation.WorkDir, repositoryPath)
	}
	if invocation.ResultFile != "" && invocation.ResultFile != expectedResultPath {
		return fmt.Errorf("executor invocation result file %q does not match expected result path %q", invocation.ResultFile, expectedResultPath)
	}
	if err := verifyInvocationUsesEffectiveBrief(invocation, selected); err != nil {
		return fmt.Errorf("executor invocation effective Brief integrity check failed: %w", err)
	}
	return nil
}

func (s *Execution) settlePreparedPrelaunchFailure(ctx context.Context, admitted AdaptiveDispatchAdmissionResult, selected *effectiveBriefInput, cause error) error {
	if cause == nil {
		cause = errors.New("prepared adaptive launch failed before process start")
	}
	if admitted.Run == nil || admitted.Attempt == nil || admitted.Lease == nil {
		return cause
	}
	run := *admitted.Run
	attempt := *admitted.Attempt
	lease := *admitted.Lease
	current, err := s.store.GetExecutionAttemptByAttemptID(ctx, attempt.AttemptID)
	if err != nil {
		return errors.Join(cause, fmt.Errorf("load admitted attempt for prelaunch settlement: %w", err))
	}
	state := workflowAttemptRuntime{}
	if strings.TrimSpace(current.ResultJSON) != "" {
		if err := json.Unmarshal([]byte(current.ResultJSON), &state); err != nil {
			return errors.Join(cause, fmt.Errorf("decode admitted attempt runtime for prelaunch settlement: %w", err))
		}
	}
	state.MutationLeaseID = lease.LeaseID
	sourceMutationStarted, modeValid := adaptiveSourceMutationStarted(admitted.Mode)
	if !modeValid {
		return errors.Join(cause, ErrAdaptiveDispatchAdmissionConflict)
	}
	state.SourceMutationStarted = sourceMutationStarted
	if selected != nil {
		state.EffectiveBriefArtifactID = selected.Artifact.ArtifactID
		state.EffectiveBriefSHA256 = selected.Artifact.SHA256
		state.EffectiveBriefMode = selected.evidenceMode()
	} else if admitted.EffectiveBriefArtifact != nil {
		state.EffectiveBriefArtifactID = admitted.EffectiveBriefArtifact.ArtifactID
		state.EffectiveBriefSHA256 = admitted.EffectiveBriefArtifact.SHA256
		state.EffectiveBriefMode = string(admitted.Mode)
	}
	state.TerminationVerified = true
	state.Error = redactSensitive(cause.Error())
	resultJSON, err := json.Marshal(state)
	if err != nil {
		return errors.Join(cause, fmt.Errorf("encode prelaunch failure runtime: %w", err))
	}
	if _, err := s.runs.FinishExecutionAttempt(ctx, workflowruns.FinishExecutionAttemptInput{
		AttemptID:  attempt.AttemptID,
		Status:     workflowstore.AttemptStatusFailed,
		ResultJSON: string(resultJSON),
	}); err != nil {
		return errors.Join(cause, fmt.Errorf("terminalize admitted attempt after prelaunch failure: %w", err))
	}
	if err := s.releaseRunMutationLease(ctx, run, lease.LeaseID); err != nil {
		return errors.Join(cause, err)
	}
	return cause
}
