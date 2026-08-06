package executor

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"relay/internal/pipeline"
	"relay/internal/prototypeexecution"
	workflowstore "relay/internal/store/workflow"
)

type PrototypeCleanup struct {
	store         *workflowstore.Store
	controller    pipeline.ProcessController
	prototypeRoot string
	clock         func() time.Time
}

func NewPrototypeCleanup(store *workflowstore.Store, prototypeRoot string) (*PrototypeCleanup, error) {
	if store == nil {
		return nil, errors.New("workflow store is required")
	}
	if strings.TrimSpace(prototypeRoot) == "" {
		return nil, errors.New("prototype root is required")
	}
	root, err := filepath.Abs(prototypeRoot)
	if err != nil {
		return nil, err
	}
	if info, e := os.Lstat(root); e == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return nil, errors.New("prototype root is not a directory")
		}
	} else if os.IsNotExist(e) {
		if err := os.MkdirAll(root, 0700); err != nil {
			return nil, err
		}
	} else {
		return nil, e
	}
	return &PrototypeCleanup{store: store, prototypeRoot: root, controller: pipeline.DefaultProcessController(), clock: time.Now}, nil
}

func (c *PrototypeCleanup) ReconcileCleanup(ctx context.Context, in prototypeexecution.CleanupRequest) (prototypeexecution.CleanupResult, error) {
	if strings.TrimSpace(in.WorkspaceID) == "" || strings.TrimSpace(in.RunID) == "" || in.ExpectedRunVersion < 1 || strings.TrimSpace(in.MutationIdentity) == "" || !oneOfCleanupTrigger(in.TriggerKind) {
		return prototypeexecution.CleanupResult{}, prototypeexecution.ErrCleanupConflict
	}
	if prior, err := c.store.GetPrototypeCleanupReconciliationByMutationIdentity(ctx, in.MutationIdentity); err == nil {
		result, loadErr := c.load(ctx, in.RunID)
		if loadErr != nil {
			return prototypeexecution.CleanupResult{}, loadErr
		}
		if result.Run.WorkspaceRowID != prior.RunRowID {
			return prototypeexecution.CleanupResult{Result: result, Reconciliation: prior}, prototypeexecution.ErrCleanupOwnershipMismatch
		}
		return prototypeexecution.CleanupResult{Result: result, Reconciliation: prior}, nil
	} else if !errors.Is(err, sqlNoRows()) {
		return prototypeexecution.CleanupResult{}, err
	}

	run, err := c.store.GetPrototypeRun(ctx, in.RunID)
	if err != nil {
		return prototypeexecution.CleanupResult{}, err
	}
	if run.Version != in.ExpectedRunVersion {
		return prototypeexecution.CleanupResult{Result: prototypeexecution.Result{Run: run}}, prototypeexecution.ErrCleanupConflict
	}
	workspace, err := c.store.GetFeatureWorkspaceByRowID(ctx, run.WorkspaceRowID)
	if err != nil {
		return prototypeexecution.CleanupResult{}, err
	}
	if workspace.WorkspaceID != in.WorkspaceID {
		return prototypeexecution.CleanupResult{Result: prototypeexecution.Result{Run: run}}, prototypeexecution.ErrCleanupOwnershipMismatch
	}
	result, err := c.load(ctx, in.RunID)
	if err != nil {
		return prototypeexecution.CleanupResult{}, err
	}
	obligations, err := c.store.ListPrototypeCleanupObligationsByRunID(ctx, in.RunID)
	if err != nil {
		return prototypeexecution.CleanupResult{}, err
	}
	statuses := obligationStatuses(obligations)
	var operationErr error

	if statuses["process_ownership"] != "complete" {
		if result.Runtime == nil {
			operationErr = prototypeexecution.ErrReconciliationIncomplete
		} else if err = c.settleProcess(ctx, in.RunID, result.Runtime); err != nil {
			operationErr = err
			if !errors.Is(err, prototypeexecution.ErrReconciliationIncomplete) {
				_ = c.failObligation(ctx, in.RunID, "process_ownership", err.Error())
			}
		} else {
			_ = c.completeObligation(ctx, in.RunID, "process_ownership")
		}
	}
	result, _ = c.load(ctx, in.RunID)
	obligations, _ = c.store.ListPrototypeCleanupObligationsByRunID(ctx, in.RunID)
	statuses = obligationStatuses(obligations)
	if operationErr == nil && statuses["evidence_settlement"] != "complete" {
		if err = c.settleEvidence(ctx, in.RunID, result); err != nil {
			operationErr = err
			if !errors.Is(err, prototypeexecution.ErrReconciliationIncomplete) {
				_ = c.failObligation(ctx, in.RunID, "evidence_settlement", err.Error())
			}
		} else {
			_ = c.completeObligation(ctx, in.RunID, "evidence_settlement")
		}
	}
	result, _ = c.load(ctx, in.RunID)
	obligations, _ = c.store.ListPrototypeCleanupObligationsByRunID(ctx, in.RunID)
	statuses = obligationStatuses(obligations)
	if operationErr == nil && statuses["worktree"] != "complete" {
		if result.Runtime == nil || result.Target == nil || result.Target.WorktreePath != result.Runtime.WorktreePath {
			operationErr = prototypeexecution.ErrCleanupWorktree
			_ = c.failObligation(ctx, in.RunID, "worktree", operationErr.Error())
		} else if err = c.cleanupWorktree(ctx, result.Runtime); err != nil {
			operationErr = err
			_ = c.failObligation(ctx, in.RunID, "worktree", err.Error())
		} else {
			_ = c.completeObligation(ctx, in.RunID, "worktree")
		}
	}
	result, _ = c.load(ctx, in.RunID)
	obligations, _ = c.store.ListPrototypeCleanupObligationsByRunID(ctx, in.RunID)
	statuses = obligationStatuses(obligations)
	if operationErr == nil && statuses["ephemeral_target"] != "complete" {
		if result.Target == nil || result.Target.TargetKey == "" {
			operationErr = prototypeexecution.ErrCleanupTarget
			_ = c.failObligation(ctx, in.RunID, "ephemeral_target", operationErr.Error())
		} else if released, e := c.releaseTarget(ctx, in.RunID, result.Target.TargetKey); e != nil || released.Status != "released" {
			if e != nil {
				operationErr = e
			} else {
				operationErr = prototypeexecution.ErrCleanupTarget
			}
			_ = c.failObligation(ctx, in.RunID, "ephemeral_target", operationErr.Error())
		} else {
			_ = c.completeObligation(ctx, in.RunID, "ephemeral_target")
		}
	}
	result, _ = c.load(ctx, in.RunID)
	obligations, _ = c.store.ListPrototypeCleanupObligationsByRunID(ctx, in.RunID)
	statuses = obligationStatuses(obligations)
	if operationErr == nil && statuses["prototype_lease"] != "complete" {
		if result.Lease == nil || result.Lease.LeaseToken == "" {
			operationErr = prototypeexecution.ErrCleanupLease
			_ = c.failObligation(ctx, in.RunID, "prototype_lease", operationErr.Error())
		} else if released, e := c.releaseLease(ctx, in.RunID, result.Lease.LeaseToken); e != nil || released.Status != "released" {
			if e != nil {
				operationErr = e
			} else {
				operationErr = prototypeexecution.ErrCleanupLease
			}
			_ = c.failObligation(ctx, in.RunID, "prototype_lease", operationErr.Error())
		} else {
			_ = c.completeObligation(ctx, in.RunID, "prototype_lease")
		}
	}

	current, _ := c.store.GetPrototypeRun(ctx, in.RunID)
	if operationErr != nil {
		if current.LifecycleState != "cleanup_required" && current.LifecycleState != "closed" {
			_ = c.store.WithTx(ctx, func(tx *workflowstore.Tx) error {
				_, e := tx.MarkPrototypeCleanupRequired(ctx, in.RunID, current.Version, "cleanup-required:"+in.MutationIdentity, current.ProcessOutcome.String)
				return e
			})
			current, _ = c.store.GetPrototypeRun(ctx, in.RunID)
		}
	}
	obligations, _ = c.store.ListPrototypeCleanupObligationsByRunID(ctx, in.RunID)
	statuses = obligationStatuses(obligations)
	resulting := "cleanup_required"
	if operationErr == nil && allCleanupComplete(statuses) && current.LifecycleState != "closed" {
		_, closeErr := c.store.ClosePrototypeRun(ctx, in.RunID, current.Version, "close:"+in.MutationIdentity)
		if closeErr != nil {
			operationErr = closeErr
		} else {
			resulting = "closed"
			current, _ = c.store.GetPrototypeRun(ctx, in.RunID)
		}
	} else if current.LifecycleState == "closed" && operationErr == nil {
		resulting = "closed"
	}
	result.Run = current
	statuses = obligationStatuses(mustObligations(ctx, c.store, in.RunID))
	reconciliation := workflowstore.PrototypeCleanupReconciliation{RunRowID: current.ID, ExpectedRunVersion: in.ExpectedRunVersion, MutationIdentity: in.MutationIdentity, TriggerKind: in.TriggerKind, ObservedRunState: run.LifecycleState, ProcessOwnershipStatus: statuses["process_ownership"], EvidenceSettlementStatus: statuses["evidence_settlement"], WorktreeStatus: statuses["worktree"], EphemeralTargetStatus: statuses["ephemeral_target"], PrototypeLeaseStatus: statuses["prototype_lease"], ResultingRunState: resulting, Diagnostic: errorText(operationErr)}
	if createdErr := c.store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		var e error
		reconciliation, e = tx.CreatePrototypeCleanupReconciliation(ctx, reconciliation)
		return e
	}); createdErr != nil {
		if existing, e := c.store.GetPrototypeCleanupReconciliationByMutationIdentity(ctx, in.MutationIdentity); e == nil {
			reconciliation = existing
		} else if operationErr == nil {
			operationErr = createdErr
		}
	}
	cleanupResult := prototypeexecution.CleanupResult{Result: result, Reconciliation: reconciliation}
	return cleanupResult, operationErr
}

func (c *PrototypeCleanup) ReconcileStartup(ctx context.Context, limit int) ([]prototypeexecution.CleanupResult, error) {
	if limit < 1 || limit > 100 {
		return nil, fmt.Errorf("startup reconciliation limit must be between 1 and 100")
	}
	candidates, err := c.store.ListPrototypeCleanupCandidates(ctx, limit)
	if err != nil {
		return nil, err
	}
	out := make([]prototypeexecution.CleanupResult, 0, len(candidates))
	var firstErr error
	for _, candidate := range candidates {
		workspace, e := c.store.GetFeatureWorkspaceByRowID(ctx, candidate.WorkspaceRowID)
		if e != nil {
			if firstErr == nil {
				firstErr = e
			}
			out = append(out, prototypeexecution.CleanupResult{Result: prototypeexecution.Result{Run: candidate}})
			continue
		}
		identity := "startup-cleanup:" + candidate.PrototypeRunID + ":" + fmt.Sprint(candidate.Version)
		value, e := c.ReconcileCleanup(ctx, prototypeexecution.CleanupRequest{WorkspaceID: workspace.WorkspaceID, RunID: candidate.PrototypeRunID, ExpectedRunVersion: candidate.Version, MutationIdentity: identity, TriggerKind: "startup"})
		out = append(out, value)
		if e != nil && firstErr == nil {
			firstErr = e
		}
	}
	return out, firstErr
}

func (c *PrototypeCleanup) settleProcess(ctx context.Context, runID string, runtime *workflowstore.PrototypeRuntime) error {
	if !runtime.ProcessIdentity.Valid {
		return prototypeexecution.ErrReconciliationIncomplete
	}
	identity, err := pipeline.DecodeProcessIdentity(runtime.ProcessIdentity.String)
	if err != nil {
		return prototypeexecution.ErrCleanupOwnershipMismatch
	}
	owned, err := c.controller.OpenOwned(identity)
	if errors.Is(err, pipeline.ErrProcessNotRunning) {
		return c.markRuntimeSettled(ctx, runID)
	}
	if err != nil {
		return prototypeexecution.ErrCleanupOwnershipMismatch
	}
	defer owned.Release()
	live, err := owned.TreeRunning()
	if err != nil {
		return prototypeexecution.ErrCleanupOwnershipMismatch
	}
	if live {
		return prototypeexecution.ErrReconciliationIncomplete
	}
	return c.markRuntimeSettled(ctx, runID)
}
func (c *PrototypeCleanup) markRuntimeSettled(ctx context.Context, runID string) error {
	return c.store.WithTx(ctx, func(tx *workflowstore.Tx) error { _, err := tx.MarkPrototypeRuntimeSettled(ctx, runID); return err })
}
func (c *PrototypeCleanup) settleEvidence(_ context.Context, _ string, result prototypeexecution.Result) error {
	for _, batch := range result.EvidenceBatches {
		if batch.Completeness == "complete" && result.FinalResult != nil {
			return nil
		}
	}
	if result.Run.LifecycleState != "succeeded" && len(result.EvidenceBatches) > 0 {
		for _, batch := range result.EvidenceBatches {
			if batch.Completeness == "partial" {
				return nil
			}
		}
	}
	return prototypeexecution.ErrReconciliationIncomplete
}
func (c *PrototypeCleanup) cleanupWorktree(ctx context.Context, runtime *workflowstore.PrototypeRuntime) error {
	if err := c.verifyPaths(ctx, runtime); err != nil {
		return err
	}
	if info, err := os.Lstat(runtime.WorktreePath); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return prototypeexecution.ErrCleanupWorktree
	}
	if _, err := os.Lstat(runtime.WorktreePath); err == nil {
		parent := filepath.Dir(runtime.WorktreePath)
		cmd := exec.CommandContext(ctx, "git", "-C", parent, "worktree", "remove", "--force", runtime.WorktreePath)
		if err := cmd.Run(); err != nil {
			if _, statErr := os.Lstat(runtime.WorktreePath); statErr == nil {
				return prototypeexecution.ErrCleanupWorktree
			}
		}
		prune := exec.CommandContext(ctx, "git", "-C", parent, "worktree", "prune")
		if err := prune.Run(); err != nil {
			return prototypeexecution.ErrCleanupWorktree
		}
	} else if !os.IsNotExist(err) {
		return prototypeexecution.ErrCleanupWorktree
	}
	if _, err := os.Lstat(runtime.WorktreePath); !os.IsNotExist(err) {
		return prototypeexecution.ErrCleanupWorktree
	}
	return nil
}
func (c *PrototypeCleanup) verifyPaths(ctx context.Context, runtime *workflowstore.PrototypeRuntime) error {
	root, err := filepath.Abs(c.prototypeRoot)
	if err != nil {
		return prototypeexecution.ErrCleanupWorktree
	}
	runtimeRoot, err := filepath.Abs(runtime.RuntimeRootPath)
	if err != nil {
		return prototypeexecution.ErrCleanupWorktree
	}
	worktree, err := filepath.Abs(runtime.WorktreePath)
	if err != nil {
		return prototypeexecution.ErrCleanupWorktree
	}
	if filepath.Clean(runtimeRoot) == filepath.Clean(worktree) || !pathWithin(root, runtimeRoot) || !pathWithin(root, worktree) {
		return prototypeexecution.ErrCleanupWorktree
	}
	for _, path := range []string{runtimeRoot, worktree} {
		if info, statErr := os.Lstat(path); statErr == nil && info.Mode()&os.ModeSymlink != 0 {
			return prototypeexecution.ErrCleanupWorktree
		}
	}
	production, err := c.store.ListRepositoryTargetsWithConfiguration(ctx)
	if err != nil {
		return prototypeexecution.ErrCleanupWorktree
	}
	for _, target := range production {
		checkout, pathErr := filepath.Abs(target.LocalPath)
		if pathErr != nil || pathsOverlap(checkout, worktree) || pathsOverlap(checkout, runtimeRoot) {
			return prototypeexecution.ErrCleanupWorktree
		}
	}
	return nil
}
func pathWithin(parent, child string) bool {
	rel, err := filepath.Rel(filepath.Clean(parent), filepath.Clean(child))
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}
func pathsOverlap(left, right string) bool {
	return pathWithin(left, right) || pathWithin(right, left)
}
func (c *PrototypeCleanup) releaseTarget(ctx context.Context, runID, key string) (workflowstore.PrototypeTarget, error) {
	var v workflowstore.PrototypeTarget
	err := c.store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		var e error
		v, e = tx.ReleasePrototypeTarget(ctx, runID, key, c.clock().UTC().Format(time.RFC3339Nano))
		return e
	})
	return v, err
}
func (c *PrototypeCleanup) releaseLease(ctx context.Context, runID, token string) (workflowstore.PrototypeLease, error) {
	var v workflowstore.PrototypeLease
	err := c.store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		var e error
		v, e = tx.ReleasePrototypeLease(ctx, runID, token, c.clock().UTC().Format(time.RFC3339Nano))
		return e
	})
	return v, err
}
func (c *PrototypeCleanup) completeObligation(ctx context.Context, runID, kind string) error {
	run, err := c.store.GetPrototypeRun(ctx, runID)
	if err != nil {
		return err
	}
	return c.store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		_, err := tx.CompletePrototypeCleanupObligation(ctx, run.ID, kind)
		return err
	})
}
func (c *PrototypeCleanup) failObligation(ctx context.Context, runID, kind, detail string) error {
	run, err := c.store.GetPrototypeRun(ctx, runID)
	if err != nil {
		return err
	}
	return c.store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		_, err := tx.FailPrototypeCleanupObligation(ctx, run.ID, kind, detail)
		return err
	})
}
func (c *PrototypeCleanup) load(ctx context.Context, id string) (prototypeexecution.Result, error) {
	run, err := c.store.GetPrototypeRun(ctx, id)
	if err != nil {
		return prototypeexecution.Result{}, err
	}
	out := prototypeexecution.Result{Run: run}
	if v, e := c.store.GetPrototypeRuntimeByRunID(ctx, id); e == nil {
		out.Runtime = &v
	}
	if v, e := c.store.GetPrototypeTargetByRunID(ctx, id); e == nil {
		out.Target = &v
	}
	if v, e := c.store.GetPrototypeLeaseByRunID(ctx, id); e == nil {
		out.Lease = &v
	}
	if v, e := c.store.GetPrototypeResultByRunID(ctx, id); e == nil {
		out.FinalResult = &v
	}
	out.EvidenceBatches, _ = c.store.ListPrototypeEvidenceBatches(ctx, id)
	out.Evidence, _ = c.store.ListPrototypeEvidenceMembers(ctx, id)
	return out, nil
}
func obligationStatuses(values []workflowstore.PrototypeCleanupObligation) map[string]string {
	out := map[string]string{}
	for _, v := range values {
		out[v.ObligationKind] = v.Status
	}
	return out
}
func allCleanupComplete(v map[string]string) bool {
	for _, k := range []string{"process_ownership", "evidence_settlement", "worktree", "ephemeral_target", "prototype_lease"} {
		if v[k] != "complete" {
			return false
		}
	}
	return true
}
func mustObligations(ctx context.Context, s *workflowstore.Store, id string) []workflowstore.PrototypeCleanupObligation {
	v, _ := s.ListPrototypeCleanupObligationsByRunID(ctx, id)
	return v
}
func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
func oneOfCleanupTrigger(v string) bool { return v == "explicit" || v == "startup" }
func sqlNoRows() error                  { return sql.ErrNoRows }

var _ prototypeexecution.Cleaner = (*PrototypeCleanup)(nil)
var _ prototypeexecution.CleanupExecutor = (*PrototypeCleanup)(nil)
