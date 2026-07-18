package operations

import (
	"relay/internal/app/idempotency"
	"relay/internal/mcp/fileacquisition"
	"relay/internal/mcp/semanticidentity"
	"relay/internal/operations/packet"
	workflowrepos "relay/internal/repos/workflow"
	"relay/internal/sourcevault"
	workflowstore "relay/internal/store/workflow"
)

type LifecycleService struct {
	store        *workflowstore.Store
	repositories *workflowrepos.Registry
	vaults       *sourcevault.Manager
	publications *AuthorityPublicationService
	idempotency  *idempotency.Service
	fetcher      fileacquisition.FetchOne
	packets      *Service
	ids          IDGenerator
	clock        Clock
}

type LifecycleDependencies struct {
	Store        *workflowstore.Store
	Repositories *workflowrepos.Registry
	Vaults       *sourcevault.Manager
	Publications *AuthorityPublicationService
	Idempotency  *idempotency.Service
	FileFetcher  fileacquisition.FetchOne
	PacketReader *Service
	IDs          IDGenerator
	Clock        Clock
}

type CreateLifecycleInput struct {
	MutationID string
	Identity   semanticidentity.CreateOperationPacket
	Files      []fileacquisition.FileParameter
}

type RefreshLifecycleInput struct {
	MutationID    string
	PriorPacketID string
	Identity      semanticidentity.RefreshOperationPacket
	Files         []fileacquisition.FileParameter
}

type CloseLifecycleInput struct {
	MutationID string
	Identity   semanticidentity.CloseOperationPacket
}

type CreateLifecycleResult struct {
	Packet   PacketView
	Mutation idempotency.StoredResult
	Replay   bool
}

type RefreshLifecycleResult struct {
	Prior    PacketSummary
	Packet   PacketView
	Mutation idempotency.StoredResult
	Replay   bool
}

type CloseLifecycleResult struct {
	Packet   PacketSummary
	Mutation idempotency.StoredResult
	Replay   bool
}

type preparedPacketAuthority struct {
	PacketID           string
	PacketArtifactID   string
	RequestIdentity    semanticidentity.RequestIdentity
	Fingerprint        semanticidentity.Fingerprint
	Snapshot           packet.Snapshot
	RetainedArtifacts  []PublicationArtifactInput
	Bindings           []PublicationBindingInput
	VaultRelationships []PublicationVaultInput
}
