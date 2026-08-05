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
	ResultMembers   []workflowstore.PrototypeResultMember
}
type Executor interface {
	Launch(context.Context, LaunchRequest) (Result, error)
	Reconcile(context.Context, OperationRequest) (Result, error)
	Cancel(context.Context, OperationRequest) (Result, error)
	SettleTimeout(context.Context, OperationRequest) (Result, error)
}

var (
	ErrPreparationClaimed   = errors.New("prototype preparation already claimed")
	ErrLaunchAlreadyClaimed = errors.New("prototype launch already claimed")
	ErrLaunchUncertain      = errors.New("prototype launch ownership is uncertain")
	ErrProcessOwnership     = errors.New("prototype process ownership cannot be verified")
	ErrWorktreePreparation  = errors.New("prototype worktree preparation failed")
	ErrEphemeralTarget      = errors.New("prototype ephemeral target failed")
	ErrLease                = errors.New("prototype lease operation failed")
	ErrWorkingDirectory     = errors.New("prototype working directory is invalid")
	ErrInvocation           = errors.New("prototype invocation failed")
	ErrCancellation         = errors.New("prototype cancellation failed")
	ErrTimeout              = errors.New("prototype timeout settlement failed")
	ErrResultInvalid        = errors.New("prototype result is invalid")
	ErrEvidenceUnsafe       = errors.New("prototype evidence is unsafe")
	ErrEvidenceMissing      = errors.New("prototype evidence is missing")
	ErrCleanupRequired      = errors.New("prototype cleanup is required")
	ErrLimitsInvalid        = errors.New("prototype execution limits are invalid")
)
