package operations

import (
	"time"

	"relay/internal/operations/packet"
	"relay/internal/operations/registry"
)

const (
	CodePacketNotFound               = "packet_not_found"
	CodePacketNotReady               = "packet_not_ready"
	CodePacketSuperseded             = "packet_superseded"
	CodePacketClosed                 = "packet_closed"
	CodePacketRouteMismatch          = "packet_route_mismatch"
	CodePacketActionNotAllowed       = "packet_action_not_allowed"
	CodePacketRefreshConflict        = "packet_refresh_conflict"
	CodeRetainedAuthorityUnavailable = "retained_authority_unavailable"
	CodeInvalidPacketDocument        = "invalid_packet_document"
	CodePacketArtifactMismatch       = "packet_artifact_mismatch"
	CodeAuthorityPublicationConflict = "authority_publication_conflict"
	CodeAuthorityPublicationFailure  = "authority_publication_failure"
	CodeInternalFailure              = "internal_failure"
)

type IDGenerator interface {
	PacketID() string
	ArtifactID() string
}
type Clock interface{ Now() time.Time }
type CreateInput struct{ Document packet.Document }
type RefreshInput struct {
	PriorPacketID string
	Document      packet.Document
}
type CloseInput struct{ PacketID string }
type DependencyRequirement struct {
	Class string
	Key   string
}
type MutationRequest struct {
	PacketID             string
	SurfaceContract      registry.SurfaceContractID
	OperationID          registry.OperationID
	Action               registry.AllowedAction
	RequiredDependencies []DependencyRequirement
}
type ReadRequest struct {
	PacketID        string
	DependencyClass string
	DependencyKey   string
}
type PreparedPacket struct {
	PacketID   string
	ArtifactID string
	Snapshot   packet.Snapshot
}
type ReplacementPacketIdentity struct {
	PacketID        string
	PacketSHA256    string
	Role            registry.Role
	OperationID     registry.OperationID
	SurfaceContract registry.SurfaceContractID
}
type PacketSummary struct {
	PacketID          string
	PacketSHA256      string
	SchemaVersion     string
	Role              registry.Role
	OperationID       registry.OperationID
	SurfaceContract   registry.SurfaceContractID
	ProjectID         string
	ReadinessState    string
	LifecycleState    string
	ReplacementPacket *ReplacementPacketIdentity
	SupersededAt      *string
	ClosedAt          *string
}
type PacketView struct {
	Summary           PacketSummary
	DocumentMediaType string
	DocumentSizeBytes int64
	DocumentBytes     []byte
}
type MutationAuthorization struct {
	Summary PacketSummary
	Allowed bool
}
type ReadAuthorization struct {
	Summary         PacketSummary
	DependencyClass string
	DependencyKey   string
	OwnerIdentity   string
}
