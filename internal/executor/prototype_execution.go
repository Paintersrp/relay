package executor

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	workflowartifacts "relay/internal/artifacts/workflow"
	"relay/internal/pipeline"
	"relay/internal/prototypeexecution"
	workflowstore "relay/internal/store/workflow"
)

const (
	prototypeResultRelativePath           = ".relay/prototype/result.json"
	prototypeExportRelativePath           = ".relay/prototype/export"
	prototypeResultMediaType              = "application/vnd.relay.prototype-result+json"
	prototypeMaxResultBytes         int64 = 1 << 20
	prototypeMaxEvidenceMemberBytes int64 = 8 << 20
	prototypeMaxEvidenceTotalBytes  int64 = 32 << 20
	prototypeCancelGrace                  = 2 * time.Second
)

type PrototypeExecution struct {
	store           *workflowstore.Store
	ownerInstanceID string
	root            string
	controller      pipeline.ProcessController
	runner          WorkflowCommandRunner
	clock           func() time.Time
}

func NewPrototypeExecution(store *workflowstore.Store, ownerInstanceID, root string) (*PrototypeExecution, error) {
	if store == nil {
		return nil, fmt.Errorf("workflow store is required")
	}
	if strings.TrimSpace(root) == "" {
		return nil, fmt.Errorf("prototype root is required")
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if info, err := os.Lstat(absolute); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return nil, fmt.Errorf("prototype root is not a directory")
		}
	} else if os.IsNotExist(err) {
		if err := os.MkdirAll(absolute, 0700); err != nil {
			return nil, err
		}
	} else {
		return nil, err
	}
	return &PrototypeExecution{store: store, ownerInstanceID: ownerInstanceID, root: absolute, controller: pipeline.DefaultProcessController(), runner: func(ctx context.Context, workDir, binary string, args []string, stdin string, timeout time.Duration, callbacks pipeline.AgentCommandStreamCallbacks, controller pipeline.ProcessController) pipeline.AgentCommandRunResult {
		return pipeline.RunLocalAgentCommandArgsStreamingWithController(ctx, workDir, binary, args, stdin, timeout, callbacks, controller)
	}, clock: time.Now}, nil
}

var prototypeTimeoutLexical = regexp.MustCompile(`^[0-9]+$`)

func decodePrototypeTimeoutLimits(raw string) (time.Duration, error) {
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	var object map[string]json.RawMessage
	if err := decoder.Decode(&object); err != nil || object == nil {
		return 0, prototypeexecution.ErrLimitsInvalid
	}
	value, ok := object["timeout_seconds"]
	if !ok {
		return 0, prototypeexecution.ErrLimitsInvalid
	}
	lexical := strings.TrimSpace(string(value))
	if !prototypeTimeoutLexical.MatchString(lexical) {
		return 0, prototypeexecution.ErrLimitsInvalid
	}
	var number json.Number
	if err := json.Unmarshal([]byte(lexical), &number); err != nil || !prototypeTimeoutLexical.MatchString(number.String()) {
		return 0, prototypeexecution.ErrLimitsInvalid
	}
	seconds, err := strconv.ParseInt(number.String(), 10, 64)
	if err != nil || seconds < 1 || seconds > 7200 {
		return 0, prototypeexecution.ErrLimitsInvalid
	}
	return time.Duration(seconds) * time.Second, nil
}

func (p *PrototypeExecution) load(ctx context.Context, runID string) (prototypeexecution.Result, error) {
	run, err := p.store.GetPrototypeRun(ctx, runID)
	if err != nil {
		return prototypeexecution.Result{}, err
	}
	out := prototypeexecution.Result{Run: run}
	if v, e := p.store.GetPrototypeRuntimeByRunID(ctx, runID); e == nil {
		out.Runtime = &v
	}
	if v, e := p.store.GetPrototypeTargetByRunID(ctx, runID); e == nil {
		out.Target = &v
	}
	if v, e := p.store.GetPrototypeLeaseByRunID(ctx, runID); e == nil {
		out.Lease = &v
	}
	out.EvidenceBatches, _ = p.store.ListPrototypeEvidenceBatches(ctx, runID)
	if v, e := p.store.GetPrototypeResultByRunID(ctx, runID); e == nil {
		out.FinalResult = &v
	}
	out.Evidence, _ = p.store.ListPrototypeEvidenceMembers(ctx, runID)
	return out, nil
}

func (p *PrototypeExecution) authorization(ctx context.Context, run workflowstore.PrototypeRun) (workflowstore.PrototypeAuthorization, error) {
	var id string
	if err := p.store.DB().QueryRowContext(ctx, `SELECT authorization_id FROM feature_workspace_prototype_authorizations WHERE id=?`, run.AuthorizationRowID).Scan(&id); err != nil {
		return workflowstore.PrototypeAuthorization{}, err
	}
	return p.store.GetPrototypeAuthorization(ctx, id)
}
func (p *PrototypeExecution) invocation(ctx context.Context, authorization workflowstore.PrototypeAuthorization) ([]byte, error) {
	artifact, err := p.store.GetDiscoveryArtifactByRowID(ctx, authorization.InvocationArtifactRowID)
	if err != nil {
		return nil, err
	}
	file := filepath.Join(p.store.ArtifactStore().Root(), filepath.FromSlash(artifact.RelativePath))
	info, err := os.Lstat(file)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, prototypeexecution.ErrInvocation
	}
	data, err := os.ReadFile(file)
	if err != nil || int64(len(data)) != artifact.SizeBytes {
		return nil, prototypeexecution.ErrInvocation
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != artifact.SHA256 {
		return nil, prototypeexecution.ErrInvocation
	}
	return data, nil
}
func runGit(ctx context.Context, dir string, args ...string) (string, error) {
	command := exec.CommandContext(ctx, "git", args...)
	command.Dir = dir
	out, err := command.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %v: %w: %s", args, err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

func (p *PrototypeExecution) Launch(ctx context.Context, in prototypeexecution.LaunchRequest) (prototypeexecution.Result, error) {
	if strings.TrimSpace(in.RunID) == "" || in.ExpectedRunVersion < 1 || strings.TrimSpace(in.MutationIdentity) == "" {
		return prototypeexecution.Result{}, prototypeexecution.ErrInvocation
	}
	run, err := p.store.GetPrototypeRun(ctx, in.RunID)
	if err != nil {
		return prototypeexecution.Result{}, err
	}
	if run.LifecycleState != "approved" || run.Version != in.ExpectedRunVersion {
		return prototypeexecution.Result{Run: run}, prototypeexecution.ErrPreparationClaimed
	}
	authorization, err := p.authorization(ctx, run)
	if err != nil {
		return prototypeexecution.Result{Run: run}, err
	}
	timeout, err := decodePrototypeTimeoutLimits(authorization.LimitsJSON)
	if err != nil {
		return prototypeexecution.Result{Run: run}, err
	}
	if authorization.BaseCommit != authorization.SourceCommit {
		return prototypeexecution.Result{Run: run}, featureappSourceDivergence()
	}
	invocation, err := p.invocation(ctx, authorization)
	if err != nil {
		return prototypeexecution.Result{Run: run}, err
	}
	runtimeID := workflowstore.NewPrototypeRuntimeID()
	targetID := workflowstore.NewPrototypeTargetID()
	lease := workflowstore.NewPrototypeLeaseToken()
	targetKey := "prototype:" + in.RunID
	runtimeRoot := filepath.Join(p.root, in.RunID)
	worktree := filepath.Join(runtimeRoot, "worktree")
	deadline := p.clock().Add(timeout)
	runtime := workflowstore.PrototypeRuntime{RuntimeID: runtimeID, AuthorizedCommit: authorization.BaseCommit, AuthorizedTree: authorization.SourceTree, RuntimeRootPath: runtimeRoot, WorktreePath: worktree, EphemeralTargetKey: targetKey, LeaseToken: lease, BackgroundContextID: "prototype-context-" + runtimeID, DeadlineAt: deadline.UTC().Format(time.RFC3339Nano)}
	target := workflowstore.PrototypeTarget{TargetID: targetID, TargetKey: targetKey, WorktreePath: worktree, AuthorizedCommit: authorization.BaseCommit, AuthorizedTree: authorization.SourceTree}
	leaseRow := workflowstore.PrototypeLease{LeaseToken: lease, EphemeralTargetKey: targetKey, OwnerInstanceID: p.ownerInstanceID}
	_, runtime, target, leaseRow, err = p.reserve(ctx, in.RunID, in.ExpectedRunVersion, runtime, target, leaseRow)
	if err != nil {
		return prototypeexecution.Result{Run: run}, err
	}
	if err := os.MkdirAll(runtimeRoot, 0700); err != nil {
		return p.prepareFailure(ctx, in, err.Error())
	}
	production, err := p.store.GetRepositoryTarget(ctx, authorization.RepoTarget)
	if err != nil || strings.TrimSpace(production.LocalPath) == "" {
		return p.prepareFailure(ctx, in, prototypeexecution.ErrWorkingDirectory.Error())
	}
	if _, err = runGit(ctx, production.LocalPath, "worktree", "add", "--detach", worktree, authorization.BaseCommit); err != nil {
		return p.prepareFailure(ctx, in, err.Error())
	}
	commit, err := runGit(ctx, worktree, "rev-parse", "HEAD^{commit}")
	if err != nil || commit != authorization.BaseCommit {
		return p.prepareFailure(ctx, in, "prototype commit verification failed")
	}
	tree, err := runGit(ctx, worktree, "rev-parse", "HEAD^{tree}")
	if err != nil || tree != authorization.SourceTree {
		return p.prepareFailure(ctx, in, "prototype tree verification failed")
	}
	metadata := filepath.Join(worktree, ".relay", "prototype")
	if err := os.MkdirAll(filepath.Join(metadata, "export"), 0700); err != nil {
		return p.prepareFailure(ctx, in, err.Error())
	}
	if err := os.WriteFile(filepath.Join(metadata, "invocation.json"), invocation, 0600); err != nil {
		return p.prepareFailure(ctx, in, err.Error())
	}
	if err := p.store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		if _, e := tx.MarkPrototypeWorktreeReady(ctx, in.RunID); e != nil {
			return e
		}
		_, e := tx.MarkPrototypeTargetReady(ctx, in.RunID, targetKey)
		return e
	}); err != nil {
		return prototypeexecution.Result{Run: run, Runtime: &runtime, Target: &target, Lease: &leaseRow}, err
	}
	adapter, err := NewAdapterFromID(authorization.Adapter)
	if err != nil {
		return p.prepareFailure(ctx, in, err.Error())
	}
	request := ExecutorAdapterRequest{RunID: run.ID, RepoPath: worktree, BriefContent: string(invocation), BriefPath: filepath.Join(worktree, ".relay/prototype/invocation.json"), ResultPath: filepath.Join(worktree, prototypeResultRelativePath), SelectedModel: authorization.Model, Timeout: time.Until(deadline)}
	executorInvocation, err := adapter.BuildInvocation(request)
	if err != nil || filepath.Clean(executorInvocation.WorkDir) != filepath.Clean(worktree) {
		return p.prepareFailure(ctx, in, prototypeexecution.ErrInvocation.Error())
	}
	preflight := ValidateInvocationPreflight(executorInvocation)
	if !preflight.OK {
		return p.prepareFailure(ctx, in, preflight.BlockerText)
	}
	var claimed workflowstore.PrototypeRun
	if err := p.store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		var e error
		claimed, _, e = tx.MarkPrototypePreparationReady(ctx, in.RunID, run.Version)
		if e != nil {
			return e
		}
		claimed, _, e = tx.ClaimPrototypeLaunch(ctx, in.RunID, claimed.Version, "prototype-launch-claim-"+runtime.RuntimeID, "durable-claim-then-process-v1")
		return e
	}); err != nil {
		return p.prepareFailure(ctx, in, err.Error())
	}
	startup := make(chan error, 1)
	processStarted := make(chan struct{}, 1)
	background, cancel := context.WithDeadline(context.WithoutCancel(ctx), deadline)
	go func() {
		defer cancel()
		result := p.runner(background, executorInvocation.WorkDir, executorInvocation.Binary, executorInvocation.Args, executorInvocation.Stdin, time.Until(deadline), pipeline.AgentCommandStreamCallbacks{OnProcessStarted: func(identity pipeline.ProcessIdentity) error {
			encoded := identity.Encode()
			_, _, e := func() (workflowstore.PrototypeRun, workflowstore.PrototypeRuntime, error) {
				var r workflowstore.PrototypeRun
				var rt workflowstore.PrototypeRuntime
				err := p.store.WithTx(context.Background(), func(tx *workflowstore.Tx) error {
					var x error
					r, rt, x = tx.PersistPrototypeProcessIdentity(context.Background(), in.RunID, claimed.Version, encoded, p.clock().UTC().Format(time.RFC3339Nano))
					return x
				})
				return r, rt, err
			}()
			if e != nil {
				startup <- e
				return e
			}
			startup <- nil
			processStarted <- struct{}{}
			return nil
		}}, p.controller)
		select {
		case <-processStarted:
		default:
			if result.Error != "" {
				startup <- errors.New(result.Error)
			} else {
				startup <- prototypeexecution.ErrLaunchUncertain
			}
		}
		current, loadErr := p.store.GetPrototypeRun(context.Background(), in.RunID)
		if loadErr == nil && current.LifecycleState == "running" {
			rt, rtErr := p.store.GetPrototypeRuntimeByRunID(context.Background(), in.RunID)
			if rtErr == nil && rt.ProcessIdentity.Valid {
				outcome := "failed"
				cause := "runner_failure"
				if result.ExitCode == 0 && !result.TimedOut && result.Error == "" {
					outcome = "succeeded"
					cause = "runner_success"
				}
				exitCode := result.ExitCode
				_, _ = p.settleObservedOutcome(context.Background(), in.RunID, current.Version, cause, "runner:"+rt.ProcessIdentity.String, outcome, &exitCode)
			}
		}
	}()
	if err := <-startup; err != nil {
		result, loadErr := p.load(ctx, in.RunID)
		if loadErr != nil {
			return result, loadErr
		}
		if errors.Is(err, prototypeexecution.ErrLaunchUncertain) {
			return result, prototypeexecution.ErrLaunchUncertain
		}
		return result, err
	}
	return p.load(ctx, in.RunID)
}
func featureappSourceDivergence() error { return errors.New("prototype source diverged") }
func (p *PrototypeExecution) reserve(ctx context.Context, runID string, version int64, r workflowstore.PrototypeRuntime, t workflowstore.PrototypeTarget, l workflowstore.PrototypeLease) (workflowstore.PrototypeRun, workflowstore.PrototypeRuntime, workflowstore.PrototypeTarget, workflowstore.PrototypeLease, error) {
	var run workflowstore.PrototypeRun
	err := p.store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		var e error
		run, r, t, l, e = tx.ReservePrototypeRuntime(ctx, runID, version, r, t, l)
		return e
	})
	return run, r, t, l, err
}
func (p *PrototypeExecution) prepareFailure(ctx context.Context, in prototypeexecution.LaunchRequest, detail string) (prototypeexecution.Result, error) {
	run, runErr := p.store.GetPrototypeRun(ctx, in.RunID)
	runtime, _ := p.store.GetPrototypeRuntimeByRunID(ctx, in.RunID)
	target, _ := p.store.GetPrototypeTargetByRunID(ctx, in.RunID)
	lease, _ := p.store.GetPrototypeLeaseByRunID(ctx, in.RunID)
	cleanupErr := error(nil)
	if runtime.WorktreePath != "" {
		if authorization, err := p.authorization(ctx, run); err == nil {
			if production, err := p.store.GetRepositoryTarget(ctx, authorization.RepoTarget); err == nil && production.LocalPath != "" {
				if _, err := runGit(ctx, production.LocalPath, "worktree", "remove", "--force", runtime.WorktreePath); err != nil && !os.IsNotExist(err) {
					cleanupErr = err
				}
			}
		}
	}
	if runtime.RuntimeRootPath != "" {
		if err := os.RemoveAll(runtime.RuntimeRootPath); err != nil && cleanupErr == nil {
			cleanupErr = err
		}
	}
	if runErr != nil {
		return prototypeexecution.Result{}, runErr
	}
	compErr := p.store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		current, err := tx.GetPrototypeRun(ctx, in.RunID)
		if err != nil {
			return err
		}
		if !terminalPrototypeState(current.LifecycleState) {
			if _, _, err = tx.MarkPrototypePreparationFailed(ctx, in.RunID, current.Version, detail); err != nil {
				return err
			}
		}
		when := p.clock().UTC().Format(time.RFC3339Nano)
		if target.TargetKey != "" {
			if _, err = tx.ReleasePrototypeTarget(ctx, in.RunID, target.TargetKey, when); err != nil {
				return err
			}
		}
		if lease.LeaseToken != "" {
			if _, err = tx.ReleasePrototypeLease(ctx, in.RunID, lease.LeaseToken, when); err != nil {
				return err
			}
		}
		for _, kind := range []string{"process_ownership", "evidence_settlement", "prototype_lease", "ephemeral_target", "worktree"} {
			if _, err = tx.CompletePrototypeCleanupObligation(ctx, current.ID, kind); err != nil {
				return err
			}
		}
		return nil
	})
	result, err := p.load(ctx, in.RunID)
	if err != nil {
		return result, err
	}
	if cleanupErr != nil || compErr != nil {
		return result, prototypeexecution.ErrCleanupRequired
	}
	return result, prototypeexecution.ErrWorktreePreparation
}
func (p *PrototypeExecution) Reconcile(ctx context.Context, in prototypeexecution.OperationRequest) (prototypeexecution.Result, error) {
	run, err := p.store.GetPrototypeRun(ctx, in.RunID)
	if err != nil {
		return prototypeexecution.Result{}, err
	}
	if terminalPrototypeState(run.LifecycleState) {
		return p.load(ctx, in.RunID)
	}
	runtime, err := p.store.GetPrototypeRuntimeByRunID(ctx, in.RunID)
	if err != nil {
		return prototypeexecution.Result{Run: run}, err
	}
	if !runtime.ProcessIdentity.Valid || strings.TrimSpace(runtime.ProcessIdentity.String) == "" {
		return p.reconcileUnknown(ctx, in, run, runtime)
	}
	identity, err := pipeline.DecodeProcessIdentity(runtime.ProcessIdentity.String)
	if err != nil {
		return p.settleUnknown(ctx, in, run.Version)
	}
	owned, err := p.controller.OpenOwned(identity)
	if err != nil {
		if errors.Is(err, pipeline.ErrProcessNotRunning) {
			return p.settleObserved(ctx, in, run.Version, "failed", "reconcile-exit:"+runtime.ProcessIdentity.String)
		}
		return p.settleUnknown(ctx, in, run.Version)
	}
	live, err := owned.TreeRunning()
	_ = owned.Release()
	if err != nil {
		return p.settleUnknown(ctx, in, run.Version)
	}
	if live {
		return p.load(ctx, in.RunID)
	}
	return p.settleObserved(ctx, in, run.Version, "failed", "reconcile-exit:"+runtime.ProcessIdentity.String)
}
func terminalPrototypeState(state string) bool {
	switch state {
	case "succeeded", "failed", "cancelled", "timed_out", "cleanup_required", "closed":
		return true
	default:
		return false
	}
}
func (p *PrototypeExecution) reconcileUnknown(ctx context.Context, in prototypeexecution.OperationRequest, run workflowstore.PrototypeRun, runtime workflowstore.PrototypeRuntime) (prototypeexecution.Result, error) {
	if run.LifecycleState == "preparing" && runtime.LaunchPhase == "claimed" {
		var updated workflowstore.PrototypeRun
		err := p.store.WithTx(ctx, func(tx *workflowstore.Tx) error {
			var e error
			updated, e = tx.MarkPrototypeLaunchUncertain(ctx, in.RunID, run.Version, "durable identity unavailable")
			return e
		})
		if err != nil {
			return prototypeexecution.Result{Run: run}, err
		}
		return prototypeexecution.Result{Run: updated}, prototypeexecution.ErrLaunchUncertain
	}
	return p.settleUnknown(ctx, in, run.Version)
}
func (p *PrototypeExecution) settleUnknown(ctx context.Context, in prototypeexecution.OperationRequest, version int64) (prototypeexecution.Result, error) {
	return p.settleObservedOutcome(ctx, in.RunID, version, "launch_uncertain", "unknown:"+in.MutationIdentity, "unknown", nil)
}
func (p *PrototypeExecution) settleObserved(ctx context.Context, in prototypeexecution.OperationRequest, version int64, outcome, observation string) (prototypeexecution.Result, error) {
	cause := "runner_failure"
	if strings.HasPrefix(observation, "reconcile-exit:") {
		cause = "reconcile_exit"
	} else if strings.HasPrefix(observation, "cancel:") {
		cause = "cancel"
	} else if strings.HasPrefix(observation, "timeout:") {
		cause = "timeout"
	}
	return p.settleObservedOutcome(ctx, in.RunID, version, cause, observation, outcome, nil)
}
func (p *PrototypeExecution) Cancel(ctx context.Context, in prototypeexecution.OperationRequest) (prototypeexecution.Result, error) {
	run, err := p.store.GetPrototypeRun(ctx, in.RunID)
	if err != nil {
		return prototypeexecution.Result{}, err
	}
	runtime, err := p.store.GetPrototypeRuntimeByRunID(ctx, in.RunID)
	if err != nil {
		return prototypeexecution.Result{Run: run}, err
	}
	if terminalPrototypeState(run.LifecycleState) {
		return p.load(ctx, in.RunID)
	}
	if err := p.store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		_, _, e := tx.RequestPrototypeCancellation(ctx, in.RunID, run.Version, in.MutationIdentity)
		return e
	}); err != nil {
		return prototypeexecution.Result{Run: run}, err
	}
	if !runtime.ProcessIdentity.Valid {
		return p.reconcileUnknown(ctx, in, run, runtime)
	}
	identity, err := pipeline.DecodeProcessIdentity(runtime.ProcessIdentity.String)
	if err != nil {
		return p.settleUnknown(ctx, in, run.Version)
	}
	owned, err := p.controller.OpenOwned(identity)
	if errors.Is(err, pipeline.ErrProcessNotRunning) {
		return p.settleObserved(ctx, in, run.Version, "cancelled", "cancel:"+in.MutationIdentity)
	}
	if err != nil {
		return p.settleUnknown(ctx, in, run.Version)
	}
	termination, termErr := owned.Terminate(prototypeCancelGrace)
	_ = owned.Release()
	if termErr != nil || !termination.VerifiedAbsent {
		return p.settleUnknown(ctx, in, run.Version)
	}
	return p.settleObserved(ctx, in, run.Version, "cancelled", "cancel:"+in.MutationIdentity)
}
func (p *PrototypeExecution) SettleTimeout(ctx context.Context, in prototypeexecution.OperationRequest) (prototypeexecution.Result, error) {
	run, err := p.store.GetPrototypeRun(ctx, in.RunID)
	if err != nil {
		return prototypeexecution.Result{}, err
	}
	runtime, err := p.store.GetPrototypeRuntimeByRunID(ctx, in.RunID)
	if err != nil {
		return prototypeexecution.Result{Run: run}, err
	}
	deadline, err := time.Parse(time.RFC3339Nano, runtime.DeadlineAt)
	if err != nil || p.clock().Before(deadline) {
		return prototypeexecution.Result{Run: run}, prototypeexecution.ErrTimeout
	}
	if err := p.store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		_, _, e := tx.ClaimPrototypeTimeout(ctx, in.RunID, run.Version, in.MutationIdentity)
		return e
	}); err != nil {
		return prototypeexecution.Result{Run: run}, err
	}
	if terminalPrototypeState(run.LifecycleState) {
		return p.load(ctx, in.RunID)
	}
	if !runtime.ProcessIdentity.Valid {
		return p.reconcileUnknown(ctx, in, run, runtime)
	}
	identity, err := pipeline.DecodeProcessIdentity(runtime.ProcessIdentity.String)
	if err != nil {
		return p.settleUnknown(ctx, in, run.Version)
	}
	owned, err := p.controller.OpenOwned(identity)
	if errors.Is(err, pipeline.ErrProcessNotRunning) {
		return p.settleObserved(ctx, in, run.Version, "timed_out", "timeout:"+in.MutationIdentity)
	}
	if err != nil {
		return p.settleUnknown(ctx, in, run.Version)
	}
	termination, termErr := owned.Terminate(prototypeCancelGrace)
	_ = owned.Release()
	if termErr != nil || !termination.VerifiedAbsent {
		return p.settleUnknown(ctx, in, run.Version)
	}
	return p.settleObserved(ctx, in, run.Version, "timed_out", "timeout:"+in.MutationIdentity)
}

var _ prototypeexecution.Executor = (*PrototypeExecution)(nil)

type prototypeResultEnvelope struct {
	SchemaVersion    int                          `json:"schema_version"`
	RunID            string                       `json:"run_id"`
	ProposalID       string                       `json:"proposal_id"`
	AuthorizationID  string                       `json:"authorization_id"`
	InvocationSHA256 string                       `json:"invocation_sha256"`
	SourceCommit     string                       `json:"source_commit"`
	BaseCommit       string                       `json:"base_commit"`
	Adapter          string                       `json:"adapter"`
	Model            string                       `json:"model"`
	Outcome          string                       `json:"outcome"`
	VariantResults   []prototypeVariantResult     `json:"variant_results"`
	Validations      []prototypeValidationResult  `json:"validations"`
	Evidence         []prototypeEvidenceCandidate `json:"evidence"`
	TemporaryCommit  string                       `json:"temporary_commit,omitempty"`
}
type prototypeVariantResult struct {
	Variant string `json:"variant"`
	Status  string `json:"status"`
	Summary string `json:"summary"`
}
type prototypeValidationResult struct {
	Command  string `json:"command"`
	ExitCode int    `json:"exit_code"`
	Summary  string `json:"summary"`
}
type prototypeEvidenceCandidate struct {
	SemanticRole string `json:"semantic_role"`
	RelativePath string `json:"relative_path"`
	SHA256       string `json:"sha256"`
	MediaType    string `json:"media_type"`
	Required     bool   `json:"required"`
}

func validatePrototypeResultEnvelope(data []byte, run workflowstore.PrototypeRun, authorization workflowstore.PrototypeAuthorization, proposal workflowstore.PrototypeProposal, processExit *int) (prototypeResultEnvelope, error) {
	if int64(len(data)) > prototypeMaxResultBytes {
		return prototypeResultEnvelope{}, prototypeexecution.ErrResultInvalid
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	var envelope prototypeResultEnvelope
	if err := decoder.Decode(&envelope); err != nil {
		return envelope, prototypeexecution.ErrResultInvalid
	}
	var extra any
	if decoder.Decode(&extra) == nil {
		return envelope, prototypeexecution.ErrResultInvalid
	}
	if envelope.SchemaVersion != 1 || envelope.RunID != authorization.ProposedRunID || envelope.ProposalID != proposal.ProposalID || envelope.AuthorizationID != authorization.AuthorizationID || envelope.InvocationSHA256 != authorization.InvocationSHA256 || envelope.SourceCommit != authorization.SourceCommit || envelope.BaseCommit != authorization.BaseCommit || envelope.Adapter != authorization.Adapter || envelope.Model != authorization.Model {
		return envelope, prototypeexecution.ErrResultInvalid
	}
	if envelope.Outcome != "succeeded" && envelope.Outcome != "failed" {
		return envelope, prototypeexecution.ErrResultInvalid
	}
	if processExit != nil && *processExit == 0 && envelope.Outcome != "succeeded" {
		return envelope, prototypeexecution.ErrResultInvalid
	}
	if processExit != nil && *processExit != 0 && envelope.Outcome == "succeeded" {
		return envelope, prototypeexecution.ErrResultInvalid
	}
	variants := map[string]bool{}
	for _, v := range envelope.VariantResults {
		if variants[v.Variant] || v.Variant == "" || (v.Status != "succeeded" && v.Status != "failed" && v.Status != "skipped") || len(v.Summary) > 4096 {
			return envelope, prototypeexecution.ErrResultInvalid
		}
		variants[v.Variant] = true
	}
	if len(envelope.Evidence) > 32 {
		return envelope, prototypeexecution.ErrResultInvalid
	}
	roles := map[string]bool{}
	paths := map[string]bool{}
	for _, candidate := range envelope.Evidence {
		if candidate.SemanticRole == "" || roles[candidate.SemanticRole] || paths[candidate.RelativePath] || !validPrototypeSHA(candidate.SHA256) || !prototypeMediaType(candidate.MediaType) {
			return envelope, prototypeexecution.ErrResultInvalid
		}
		roles[candidate.SemanticRole] = true
		paths[candidate.RelativePath] = true
	}
	if envelope.TemporaryCommit != "" && !regexp.MustCompile(`^[0-9a-f]{40}([0-9a-f]{24})?$`).MatchString(envelope.TemporaryCommit) {
		return envelope, prototypeexecution.ErrResultInvalid
	}
	return envelope, nil
}
func validPrototypeSHA(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, c := range value {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
func prototypeMediaType(value string) bool {
	switch value {
	case "text/plain", "application/json", "text/markdown", "application/octet-stream":
		return true
	default:
		return false
	}
}
func prototypeRedacted(data []byte) bool {
	value := string(data)
	for _, marker := range []string{"OPENAI_API_KEY=", "ANTHROPIC_API_KEY=", "Authorization: Bearer ", "-----BEGIN PRIVATE KEY-----"} {
		if strings.Contains(value, marker) {
			return true
		}
	}
	return false
}
func prototypeCandidatePath(worktree string, candidate prototypeEvidenceCandidate) (string, error) {
	relative := filepath.ToSlash(candidate.RelativePath)
	if !strings.HasPrefix(relative, prototypeExportRelativePath+"/") || filepath.IsAbs(relative) || strings.Contains(relative, "../") || strings.Contains(relative, "..\\") {
		return "", prototypeexecution.ErrEvidenceUnsafe
	}
	exportRoot := filepath.Join(worktree, filepath.FromSlash(prototypeExportRelativePath))
	absolute := filepath.Join(worktree, filepath.FromSlash(relative))
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", prototypeexecution.ErrEvidenceUnsafe
	}
	rootResolved, err := filepath.EvalSymlinks(exportRoot)
	if err != nil {
		return "", prototypeexecution.ErrEvidenceUnsafe
	}
	relativeToRoot, err := filepath.Rel(rootResolved, resolved)
	if err != nil || relativeToRoot == ".." || strings.HasPrefix(relativeToRoot, ".."+string(filepath.Separator)) || filepath.IsAbs(relativeToRoot) {
		return "", prototypeexecution.ErrEvidenceUnsafe
	}
	info, err := os.Lstat(resolved)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Size() > prototypeMaxEvidenceMemberBytes {
		return "", prototypeexecution.ErrEvidenceUnsafe
	}
	if candidate.MediaType != "application/octet-stream" {
		data, err := os.ReadFile(resolved)
		if err != nil || prototypeRedacted(data) {
			return "", prototypeexecution.ErrEvidenceUnsafe
		}
	}
	return resolved, nil
}

func prototypeHashIdentity(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
func (p *PrototypeExecution) settleObservedOutcome(ctx context.Context, runID string, expectedVersion int64, settlementCause string, observationIdentity string, observedOutcome string, exitCode *int) (prototypeexecution.Result, error) {
	run, err := p.store.GetPrototypeRun(ctx, runID)
	if err != nil {
		return prototypeexecution.Result{}, err
	}
	if terminalPrototypeState(run.LifecycleState) {
		return p.load(ctx, runID)
	}
	runtime, err := p.store.GetPrototypeRuntimeByRunID(ctx, runID)
	if err != nil {
		return prototypeexecution.Result{Run: run}, err
	}
	authorization, err := p.authorization(ctx, run)
	if err != nil {
		return prototypeexecution.Result{Run: run}, err
	}
	proposalID := ""
	if err := p.store.DB().QueryRowContext(ctx, `SELECT proposal_id FROM feature_workspace_prototype_proposals WHERE id=?`, authorization.ProposalRowID).Scan(&proposalID); err != nil {
		return prototypeexecution.Result{Run: run}, err
	}
	proposal, err := p.store.GetPrototypeProposal(ctx, proposalID)
	if err != nil {
		return prototypeexecution.Result{Run: run}, err
	}
	batchIdentity := "prototype-evidence-batch:" + prototypeHashIdentity(runID+"\n"+settlementCause+"\n"+observationIdentity)
	batchID := workflowstore.NewPrototypeEvidenceBatchID()
	resultPath := filepath.Join(runtime.WorktreePath, prototypeResultRelativePath)
	data, readErr := os.ReadFile(resultPath)
	envelopeStatus := "missing"
	completeness := "partial"
	var envelope prototypeResultEnvelope
	var artifact workflowstore.DiscoveryArtifact
	var staged workflowartifacts.File
	type candidateStage struct {
		candidate prototypeEvidenceCandidate
		file      workflowartifacts.File
	}
	var candidateStages []candidateStage
	var batch *workflowartifacts.Batch
	if readErr == nil {
		envelope, err = validatePrototypeResultEnvelope(data, run, authorization, proposal, exitCode)
		if err == nil {
			envelopeStatus = "valid"
		} else {
			envelopeStatus = "invalid"
		}
	}
	if readErr == nil {
		workspaceID := ""
		_ = p.store.DB().QueryRowContext(ctx, `SELECT workspace_id FROM feature_workspaces WHERE id=?`, run.WorkspaceRowID).Scan(&workspaceID)
		batch, err = p.store.ArtifactStore().Begin("feature-discovery/" + workspaceID + "/prototype/" + run.PrototypeRunID + "/evidence")
		if err == nil {
			staged, err = batch.Stage("prototype_result", "result.json", prototypeResultMediaType, data)
		}
	}
	_ = envelope
	if batch == nil {
		workspaceID := ""
		_ = p.store.DB().QueryRowContext(ctx, `SELECT workspace_id FROM feature_workspaces WHERE id=?`, run.WorkspaceRowID).Scan(&workspaceID)
		batch, err = p.store.ArtifactStore().Begin("feature-discovery/" + workspaceID + "/prototype/" + run.PrototypeRunID + "/evidence")
		if err != nil {
			return prototypeexecution.Result{Run: run}, err
		}
	}
	if err != nil && batch != nil {
		_ = batch.Rollback()
	}
	if err != nil && readErr == nil {
		return prototypeexecution.Result{Run: run}, prototypeexecution.ErrEvidenceUnsafe
	}
	if envelopeStatus == "valid" {
		obligationSet := map[string]bool{}
		var obligations []string
		if json.Unmarshal([]byte(authorization.EvidenceObligationsJSON), &obligations) != nil {
			return prototypeexecution.Result{Run: run}, prototypeexecution.ErrEvidenceUnsafe
		}
		for _, role := range obligations {
			obligationSet[role] = true
		}
		seenRoles := map[string]bool{}
		totalEvidenceBytes := int64(0)
		for index, candidate := range envelope.Evidence {
			path, pathErr := prototypeCandidatePath(runtime.WorktreePath, candidate)
			if pathErr != nil {
				_ = batch.Rollback()
				return prototypeexecution.Result{Run: run}, prototypeexecution.ErrEvidenceUnsafe
			}
			stagedCandidate, stageErr := batch.StageFile("prototype_evidence", fmt.Sprintf("evidence-%03d-%s", index+1, filepath.Base(path)), candidate.MediaType, path)
			if stageErr != nil {
				_ = batch.Rollback()
				return prototypeexecution.Result{Run: run}, prototypeexecution.ErrEvidenceUnsafe
			}
			if stagedCandidate.SHA256 != candidate.SHA256 {
				_ = batch.Rollback()
				return prototypeexecution.Result{Run: run}, prototypeexecution.ErrEvidenceUnsafe
			}
			totalEvidenceBytes += stagedCandidate.SizeBytes
			if totalEvidenceBytes > prototypeMaxEvidenceTotalBytes {
				_ = batch.Rollback()
				return prototypeexecution.Result{Run: run}, prototypeexecution.ErrEvidenceUnsafe
			}
			seenRoles[candidate.SemanticRole] = true
			candidateStages = append(candidateStages, candidateStage{candidate: candidate, file: stagedCandidate})
		}
		completeness = "complete"
		for role := range obligationSet {
			if !seenRoles[role] {
				completeness = "partial"
				break
			}
		}
	} else {
		completeness = "complete"
	}
	var final workflowstore.PrototypeResult
	err = p.store.CommitArtifactBatch(ctx, batch, func(tx *workflowstore.Tx) error {
		var e error
		artifactCount := int64(0)
		totalBytes := int64(0)
		if staged.RelativePath != "" {
			artifact, e = tx.CreateDiscoveryArtifact(ctx, workflowstore.DiscoveryArtifact{DiscoveryArtifactID: workflowstore.NewFeatureWorkspaceDiscoveryArtifactID(), WorkspaceRowID: run.WorkspaceRowID, RelativePath: staged.RelativePath, SHA256: staged.SHA256, MediaType: staged.MediaType, SizeBytes: staged.SizeBytes})
			if e != nil {
				return e
			}
			artifactCount++
			totalBytes += staged.SizeBytes
		}
		memberArtifacts := make([]workflowstore.DiscoveryArtifact, 0, len(candidateStages))
		for _, candidate := range candidateStages {
			memberArtifact, memberErr := tx.CreateDiscoveryArtifact(ctx, workflowstore.DiscoveryArtifact{DiscoveryArtifactID: workflowstore.NewFeatureWorkspaceDiscoveryArtifactID(), WorkspaceRowID: run.WorkspaceRowID, RelativePath: candidate.file.RelativePath, SHA256: candidate.file.SHA256, MediaType: candidate.file.MediaType, SizeBytes: candidate.file.SizeBytes})
			if memberErr != nil {
				return memberErr
			}
			memberArtifacts = append(memberArtifacts, memberArtifact)
			artifactCount++
			totalBytes += candidate.file.SizeBytes
		}
		b, e := tx.CreatePrototypeEvidenceImportBatch(ctx, workflowstore.PrototypeEvidenceImportBatch{EvidenceBatchID: batchID, RunRowID: run.ID, RuntimeRowID: runtime.ID, BatchIdentity: batchIdentity, SettlementCause: settlementCause, ObservationIdentity: observationIdentity, ProcessOutcome: observedOutcome, EnvelopeStatus: envelopeStatus, Completeness: completeness, ArtifactCount: artifactCount, TotalSizeBytes: totalBytes})
		if e != nil {
			return e
		}
		for index, candidate := range candidateStages {
			_, e = tx.CreatePrototypeEvidenceMember(ctx, workflowstore.PrototypeEvidenceMember{EvidenceMemberID: workflowstore.NewPrototypeEvidenceMemberID(), RunRowID: run.ID, EvidenceBatchRowID: b.ID, Sequence: int64(index + 1), SemanticRole: candidate.candidate.SemanticRole, RelativePath: candidate.candidate.RelativePath, ArtifactRowID: memberArtifacts[index].ID, SHA256: candidate.file.SHA256, SizeBytes: candidate.file.SizeBytes, MediaType: candidate.candidate.MediaType, Completeness: "complete"})
			if e != nil {
				return e
			}
		}
		if completeness == "complete" {
			exit := sql.NullInt64{}
			if exitCode != nil {
				exit = sql.NullInt64{Int64: int64(*exitCode), Valid: true}
			}
			validationError := ""
			if envelopeStatus != "valid" {
				validationError = "result envelope " + envelopeStatus
			}
			resultArtifact := sql.NullInt64{}
			resultDigest := sql.NullString{}
			if artifact.ID != 0 {
				resultArtifact = sql.NullInt64{Int64: artifact.ID, Valid: true}
				resultDigest = sql.NullString{String: staged.SHA256, Valid: true}
			}
			final, e = tx.CreatePrototypeResult(ctx, workflowstore.PrototypeResult{ResultID: workflowstore.NewPrototypeResultID(), RunRowID: run.ID, RuntimeRowID: runtime.ID, EvidenceBatchRowID: b.ID, ArtifactRowID: resultArtifact, ValidationStatus: envelopeStatus, ProcessExitCode: exit, ProcessOutcome: observedOutcome, EnvelopeSHA256: resultDigest, ValidationError: validationError})
			if e != nil {
				return e
			}
		}
		_, e = tx.CompletePrototypeCleanupObligation(ctx, run.ID, "evidence_settlement")
		return e
	})
	if batch != nil && err != nil {
		return prototypeexecution.Result{Run: run}, err
	}
	if err != nil && batch == nil {
		err = p.store.WithTx(ctx, func(tx *workflowstore.Tx) error {
			_, e := tx.GetOrCreatePrototypeCleanupObligation(ctx, run.ID, "evidence_settlement", "")
			return e
		})
	}
	if err != nil {
		return prototypeexecution.Result{Run: run}, err
	}
	terminal := observedOutcome
	if observedOutcome == "succeeded" && (final.ID == 0 || final.ValidationStatus != "valid") {
		terminal = "failed"
	}
	if observedOutcome == "host_failed" || observedOutcome == "unknown" {
		terminal = "failed"
	}
	transition := "prototype-settlement:" + prototypeHashIdentity(runID+"\n"+settlementCause+"\n"+observationIdentity+"\n"+terminal)
	var settled workflowstore.PrototypeRun
	err = p.store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		var e error
		settled, _, e = tx.SettlePrototypeProcess(ctx, runID, expectedVersion, terminal, observedOutcome, transition)
		return e
	})
	result, _ := p.load(ctx, runID)
	if err != nil {
		return result, err
	}
	result.Run = settled
	if final.ID != 0 {
		result.FinalResult = &final
	}
	return result, nil
}
