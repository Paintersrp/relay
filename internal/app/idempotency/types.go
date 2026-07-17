package idempotency

import (
	"context"

	"relay/internal/mcp/semanticidentity"
	"relay/internal/operations/registry"
	workflowstore "relay/internal/store/workflow"
)

type MutationKey struct {
	SurfaceContractID registry.SurfaceContractID
	Tool              registry.MutationTool
	MutationID        string
}

type StoredResult struct {
	ResultKind         semanticidentity.ResultKind
	ResultIdentity     semanticidentity.ResultIdentity
	ResultIdentityJSON []byte
	ResultSHA256       string
	CommittedAt        string
}

type ResolutionKind string

const (
	ResolutionMiss     ResolutionKind = "miss"
	ResolutionReplay   ResolutionKind = "replay"
	ResolutionConflict ResolutionKind = "conflict"
)

type Resolution struct {
	Kind   ResolutionKind
	Result StoredResult
}

type RecordSuccessInput struct {
	Key                   MutationKey
	SurfaceManifestSHA256 string
	Fingerprint           semanticidentity.Fingerprint
}

type DomainMutation func(context.Context, *workflowstore.Tx) (semanticidentity.ResultIdentity, error)

type Store interface {
	GetMCPMutationResultOptional(context.Context, workflowstore.MCPMutationKey) (workflowstore.MCPMutationResult, bool, error)
	WithTx(context.Context, func(*workflowstore.Tx) error) error
}
