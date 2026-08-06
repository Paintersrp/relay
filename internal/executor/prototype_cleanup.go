package executor

import (
	"context"
	"errors"
	"fmt"
	"os"
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

func (p *PrototypeCleanup) ReconcileCleanup(ctx context.Context, in prototypeexecution.CleanupRequest) (prototypeexecution.CleanupResult, error) {
	if strings.TrimSpace(in.WorkspaceID) == "" || strings.TrimSpace(in.RunID) == "" || in.ExpectedRunVersion < 1 || strings.TrimSpace(in.MutationIdentity) == "" || strings.TrimSpace(in.TriggerKind) == "" {
		return prototypeexecution.CleanupResult{}, prototypeexecution.ErrCleanupConflict
	}
	run, err := p.store.GetPrototypeRun(ctx, in.RunID)
	if err != nil {
		return prototypeexecution.CleanupResult{}, err
	}
	if run.Version != in.ExpectedRunVersion {
		return prototypeexecution.CleanupResult{Result: prototypeexecution.Result{Run: run}}, prototypeexecution.ErrCleanupConflict
	}
	var workspace string
	if err := p.store.DB().QueryRowContext(ctx, `SELECT workspace_id FROM feature_workspaces WHERE id=?`, run.WorkspaceRowID).Scan(&workspace); err != nil {
		return prototypeexecution.CleanupResult{Result: prototypeexecution.Result{Run: run}}, err
	}
	if workspace != in.WorkspaceID {
		return prototypeexecution.CleanupResult{Result: prototypeexecution.Result{Run: run}}, prototypeexecution.ErrCleanupOwnership
	}
	result, err := p.load(ctx, in.RunID)
	if err != nil {
		return prototypeexecution.CleanupResult{}, err
	}
	var reconciliation workflowstore.PrototypeCleanupReconciliation
	err = p.store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		reconciliation, err = tx.CreatePrototypeCleanupReconciliation(ctx, workflowstore.PrototypeCleanupReconciliation{RunRowID: run.ID, ExpectedRunVersion: in.ExpectedRunVersion, MutationIdentity: in.MutationIdentity, TriggerKind: in.TriggerKind})
		return err
	})
	if err != nil {
		return prototypeexecution.CleanupResult{Result: result}, err
	}
	if result.Runtime != nil {
		if err := p.verifyProcess(result.Runtime); err != nil {
			return prototypeexecution.CleanupResult{Result: result, Reconciliation: reconciliation}, err
		}
	}
	if result.FinalResult == nil || result.FinalResult.ValidationStatus != "valid" {
		return prototypeexecution.CleanupResult{Result: result, Reconciliation: reconciliation}, prototypeexecution.ErrCleanupEvidence
	}
	if result.Runtime != nil {
		if err := p.verifyPaths(result.Runtime); err != nil {
			return prototypeexecution.CleanupResult{Result: result, Reconciliation: reconciliation}, err
		}
	}
	if err := p.store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		if result.Target != nil && result.Target.TargetKey != "" {
			if _, e := tx.ReleasePrototypeTarget(ctx, in.RunID, result.Target.TargetKey, p.clock().UTC().Format(time.RFC3339Nano)); e != nil {
				return e
			}
		}
		if result.Lease != nil && result.Lease.LeaseToken != "" {
			if _, e := tx.ReleasePrototypeLease(ctx, in.RunID, result.Lease.LeaseToken, p.clock().UTC().Format(time.RFC3339Nano)); e != nil {
				return e
			}
		}
		if result.Runtime != nil {
			if err := p.removePaths(result.Runtime); err != nil {
				return err
			}
		}
		_, err = tx.ClosePrototypeRun(ctx, in.RunID, in.ExpectedRunVersion, in.MutationIdentity)
		return err
	}); err != nil {
		return prototypeexecution.CleanupResult{Result: result, Reconciliation: reconciliation}, err
	}
	result, err = p.load(ctx, in.RunID)
	if err != nil {
		return prototypeexecution.CleanupResult{}, err
	}
	reconciliation.Status = "closed"
	return prototypeexecution.CleanupResult{Result: result, Reconciliation: reconciliation}, nil
}

func (p *PrototypeCleanup) ReconcileStartup(ctx context.Context) ([]prototypeexecution.CleanupResult, error) {
	candidates, err := p.store.ListPrototypeCleanupCandidates(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]prototypeexecution.CleanupResult, 0, len(candidates))
	for _, c := range candidates {
		var ws string
		if e := p.store.DB().QueryRowContext(ctx, `SELECT workspace_id FROM feature_workspaces w JOIN feature_workspace_prototype_runs r ON r.workspace_row_id=w.id WHERE r.id=?`, c.RunRowID).Scan(&ws); e != nil {
			return out, e
		}
		var runID string
		if e := p.store.DB().QueryRowContext(ctx, `SELECT prototype_run_id FROM feature_workspace_prototype_runs WHERE id=?`, c.RunRowID).Scan(&runID); e != nil {
			return out, e
		}
		r, e := p.ReconcileCleanup(ctx, prototypeexecution.CleanupRequest{WorkspaceID: ws, RunID: runID, ExpectedRunVersion: c.ExpectedRunVersion, MutationIdentity: c.MutationIdentity, TriggerKind: "startup"})
		out = append(out, r)
		if e != nil {
			return out, e
		}
	}
	return out, nil
}

func (p *PrototypeCleanup) verifyProcess(runtime *workflowstore.PrototypeRuntime) error {
	if runtime.LaunchPhase != "ownership_unresolved" && !runtime.ProcessIdentity.Valid {
		return nil
	}
	if !runtime.ProcessIdentity.Valid {
		return prototypeexecution.ErrCleanupOwnership
	}
	id, err := pipeline.DecodeProcessIdentity(runtime.ProcessIdentity.String)
	if err != nil {
		return prototypeexecution.ErrCleanupOwnership
	}
	owned, err := p.controller.OpenOwned(id)
	if errors.Is(err, pipeline.ErrProcessNotRunning) {
		return nil
	}
	if err != nil {
		return prototypeexecution.ErrCleanupOwnership
	}
	defer owned.Release()
	live, err := owned.TreeRunning()
	if err != nil {
		return prototypeexecution.ErrCleanupOwnership
	}
	if live {
		return fmt.Errorf("%w: live process is not terminated by ordinary cleanup", prototypeexecution.ErrCleanupOwnership)
	}
	return nil
}
func (p *PrototypeCleanup) verifyPaths(r *workflowstore.PrototypeRuntime) error {
	for _, path := range []string{r.RuntimeRootPath, r.WorktreePath} {
		abs, err := filepath.Abs(path)
		if err != nil {
			return prototypeexecution.ErrCleanupWorktree
		}
		rel, err := filepath.Rel(p.prototypeRoot, abs)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
			return prototypeexecution.ErrCleanupWorktree
		}
	}
	return nil
}
func (p *PrototypeCleanup) removePaths(r *workflowstore.PrototypeRuntime) error {
	if err := p.verifyPaths(r); err != nil {
		return err
	}
	if err := os.RemoveAll(r.RuntimeRootPath); err != nil {
		return prototypeexecution.ErrCleanupWorktree
	}
	return nil
}
func (p *PrototypeCleanup) load(ctx context.Context, id string) (prototypeexecution.Result, error) {
	run, err := p.store.GetPrototypeRun(ctx, id)
	if err != nil {
		return prototypeexecution.Result{}, err
	}
	out := prototypeexecution.Result{Run: run}
	if v, e := p.store.GetPrototypeRuntimeByRunID(ctx, id); e == nil {
		out.Runtime = &v
	}
	if v, e := p.store.GetPrototypeTargetByRunID(ctx, id); e == nil {
		out.Target = &v
	}
	if v, e := p.store.GetPrototypeLeaseByRunID(ctx, id); e == nil {
		out.Lease = &v
	}
	if v, e := p.store.GetPrototypeResultByRunID(ctx, id); e == nil {
		out.FinalResult = &v
	}
	out.EvidenceBatches, _ = p.store.ListPrototypeEvidenceBatches(ctx, id)
	out.Evidence, _ = p.store.ListPrototypeEvidenceMembers(ctx, id)
	return out, nil
}

var _ prototypeexecution.Cleaner = (*PrototypeCleanup)(nil)
var _ prototypeexecution.CleanupExecutor = (*PrototypeCleanup)(nil)
