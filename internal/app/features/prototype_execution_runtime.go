package features

import (
	"context"
	"database/sql"
	"relay/internal/prototypeexecution"
	"strings"
)

func (s *Service) prototypeOperation(ctx context.Context, workspaceID, runID string, enabled bool, fn func() (prototypeexecution.Result, error)) (prototypeexecution.Result, error) {
	if s.prototypeExecutor == nil {
		return prototypeexecution.Result{}, prototypeexecution.ErrInvocation
	}
	w, e := s.store.GetFeatureWorkspaceByWorkspaceID(ctx, strings.TrimSpace(workspaceID))
	if e != nil {
		return prototypeexecution.Result{}, e
	}
	if enabled && w.DiscoveryCapabilityEnabled != 1 {
		return prototypeexecution.Result{}, ErrPrototypeCapabilityDisabled
	}
	r, e := s.store.GetPrototypeRun(ctx, strings.TrimSpace(runID))
	if e != nil {
		return prototypeexecution.Result{}, e
	}
	if r.WorkspaceRowID != w.ID {
		return prototypeexecution.Result{}, ErrPrototypeOwnership
	}
	return fn()
}
func (s *Service) LaunchApprovedPrototype(ctx context.Context, in prototypeexecution.LaunchRequest) (prototypeexecution.Result, error) {
	if strings.TrimSpace(in.MutationIdentity) == "" {
		return prototypeexecution.Result{}, ErrPrototypeApprovalMissing
	}
	return s.prototypeOperation(ctx, in.WorkspaceID, in.RunID, true, func() (prototypeexecution.Result, error) { return s.prototypeExecutor.Launch(ctx, in) })
}
func (s *Service) ReconcilePrototypeLaunch(ctx context.Context, in prototypeexecution.OperationRequest) (prototypeexecution.Result, error) {
	return s.prototypeOperation(ctx, in.WorkspaceID, in.RunID, false, func() (prototypeexecution.Result, error) { return s.prototypeExecutor.Reconcile(ctx, in) })
}
func (s *Service) CancelPrototypeExecution(ctx context.Context, in prototypeexecution.OperationRequest) (prototypeexecution.Result, error) {
	if strings.TrimSpace(in.MutationIdentity) == "" {
		return prototypeexecution.Result{}, ErrPrototypeApprovalMissing
	}
	return s.prototypeOperation(ctx, in.WorkspaceID, in.RunID, false, func() (prototypeexecution.Result, error) { return s.prototypeExecutor.Cancel(ctx, in) })
}
func (s *Service) SettlePrototypeTimeout(ctx context.Context, in prototypeexecution.OperationRequest) (prototypeexecution.Result, error) {
	if strings.TrimSpace(in.MutationIdentity) == "" {
		return prototypeexecution.Result{}, ErrPrototypeApprovalMissing
	}
	return s.prototypeOperation(ctx, in.WorkspaceID, in.RunID, false, func() (prototypeexecution.Result, error) { return s.prototypeExecutor.SettleTimeout(ctx, in) })
}

var _ = sql.ErrNoRows
