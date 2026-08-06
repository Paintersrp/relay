package prototypeexecution

import (
	"context"
	"errors"
	workflowstore "relay/internal/store/workflow"
)

type LaunchRequest struct {
	WorkspaceID        string
	RunID              string
	ExpectedRunVersion int64
	MutationIdentity   string
}
type OperationRequest struct {
	WorkspaceID        string
	RunID              string
	ExpectedRunVersion int64
	MutationIdentity   string
}
type Result struct {
	Run             workflowstore.PrototypeRun
	Runtime         *workflowstore.PrototypeRuntime
	Target          *workflowstore.PrototypeTarget
	Lease           *workflowstore.PrototypeLease
	EvidenceBatches []workflowstore.PrototypeEvidenceImportBatch
	FinalResult     *workflowstore.PrototypeResult
	Evidence        []workflowstore.PrototypeEvidenceMember
}
type Executor interface {
	Launch(context.Context, LaunchRequest) (Result, error)
	Reconcile(context.Context, OperationRequest) (Result, error)
	Cancel(context.Context, OperationRequest) (Result, error)
	SettleTimeout(context.Context, OperationRequest) (Result, error)
}

type CleanupRequest struct {
	WorkspaceID        string
	RunID              string
	ExpectedRunVersion int64
	MutationIdentity   string
	TriggerKind        string
}

type CleanupResult struct {
	Result
	Reconciliation workflowstore.PrototypeCleanupReconciliation
}

type Cleaner interface {
	ReconcileCleanup(context.Context, CleanupRequest) (CleanupResult, error)
	ReconcileStartup(context.Context) ([]CleanupResult, error)
}

// CleanupExecutor is retained as the narrow compatibility name for callers
// that only need explicit cleanup reconciliation.
type CleanupExecutor interface {
	ReconcileCleanup(context.Context, CleanupRequest) (CleanupResult, error)
}

var (
	ErrPreparationClaimed    = errors.New("prototype preparation already claimed")
	ErrLaunchAlreadyClaimed  = errors.New("prototype launch already claimed")
	ErrLaunchUncertain       = errors.New("prototype launch ownership is uncertain")
	ErrProcessOwnership      = errors.New("prototype process ownership cannot be verified")
	ErrWorktreePreparation   = errors.New("prototype worktree preparation failed")
	ErrEphemeralTarget       = errors.New("prototype ephemeral target failed")
	ErrLease                 = errors.New("prototype lease operation failed")
	ErrWorkingDirectory      = errors.New("prototype working directory is invalid")
	ErrInvocation            = errors.New("prototype invocation failed")
	ErrCancellation          = errors.New("prototype cancellation failed")
	ErrTimeout               = errors.New("prototype timeout settlement failed")
	ErrResultInvalid         = errors.New("prototype result is invalid")
	ErrEvidenceUnsafe        = errors.New("prototype evidence is unsafe")
	ErrEvidenceMissing       = errors.New("prototype evidence is missing")
	ErrCleanupRequired       = errors.New("prototype cleanup is required")
	ErrCleanupConflict       = errors.New("prototype cleanup transition conflicts")
	ErrCleanupNotFound       = errors.New("prototype cleanup obligation not found")
	ErrQAAssociationConflict = errors.New("prototype QA association conflicts")
	ErrLimitsInvalid         = errors.New("prototype execution limits are invalid")
	ErrCleanupOwnership      = errors.New("prototype cleanup ownership is invalid")
	ErrCleanupEvidence       = errors.New("prototype cleanup evidence is incomplete")
	ErrCleanupWorktree       = errors.New("prototype cleanup worktree is invalid")
	ErrCleanupTarget         = errors.New("prototype cleanup target is invalid")
	ErrCleanupLease          = errors.New("prototype cleanup lease is invalid")
)
