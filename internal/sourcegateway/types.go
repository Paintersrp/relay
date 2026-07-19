package sourcegateway

import (
	"context"
	"errors"

	"relay/internal/app/operations"
	"relay/internal/operations/registry"
	"relay/internal/sourcevault"
	workflowstore "relay/internal/store/workflow"
)

const (
	PathIdentityVersion = "relay.git-path.v1"
	CursorVersion       = "relay.source-cursor.v1"
	MaxInlinePathBytes  = 8192
	MaxTreePageEntries  = 512
	MaxBlobPageBytes    = 1 << 20
	MaxCursorTokenBytes = 32 << 10
)

const (
	CodeInvalidRequest               = "invalid_request"
	CodePacketUnavailable            = "packet_unavailable"
	CodeRouteMismatch                = "route_mismatch"
	CodeRepositoryUnavailable        = "repository_unavailable"
	CodeRetainedAuthorityUnavailable = "retained_authority_unavailable"
	CodePathAbsent                   = "path_absent"
	CodeObjectUnavailable            = "object_unavailable"
	CodeObjectMismatch               = "object_mismatch"
	CodeInvalidRange                 = "invalid_range"
	CodeInvalidSelector              = "invalid_selector"
	CodeInvalidCursor                = "invalid_cursor"
	CodeInternalFailure              = "internal_failure"
)

type Error struct{ Code string }

func (e *Error) Error() string {
	if e == nil {
		return "source gateway failure"
	}
	switch e.Code {
	case CodeInvalidRequest:
		return "source request is invalid"
	case CodePacketUnavailable:
		return "operation packet is unavailable"
	case CodeRouteMismatch:
		return "operation packet route does not match"
	case CodeRepositoryUnavailable:
		return "packet repository authority is unavailable"
	case CodeRetainedAuthorityUnavailable:
		return "retained source authority is unavailable"
	case CodePathAbsent:
		return "requested source path is absent"
	case CodeObjectUnavailable:
		return "requested Git object is unavailable"
	case CodeObjectMismatch:
		return "requested Git object does not match"
	case CodeInvalidRange:
		return "requested blob range is invalid"
	case CodeInvalidSelector:
		return "source path selector is invalid"
	case CodeInvalidCursor:
		return "source continuation is invalid"
	default:
		return "source gateway operation failed"
	}
}
func ErrorCode(err error) string {
	var value *Error
	if errors.As(err, &value) {
		return value.Code
	}
	return ""
}

type AuthorityResolver interface {
	ResolveSourceReadAuthority(context.Context, operations.ResolveSourceReadAuthorityRequest) (operations.SourceReadAuthority, error)
}
type VaultReader interface {
	ReadRetainedTree(context.Context, sourcevault.ReadRetainedTreeRequest) (sourcevault.ReadRetainedTreeResult, error)
	ReadRetainedBlobRange(context.Context, sourcevault.ReadRetainedBlobRangeRequest) (sourcevault.ReadRetainedBlobRangeResult, error)
}
type SelectorStore interface {
	CreateOrGetSourcePathSelector(context.Context, workflowstore.CreateOrGetSourcePathSelectorParams) (workflowstore.SourcePathSelector, error)
	GetSourcePathSelector(context.Context, string) (workflowstore.SourcePathSelector, error)
}
type CursorCodec interface {
	Encode(cursorPayload) (string, error)
	Decode(string) (cursorPayload, error)
}

type PathReference struct {
	PathID       string `json:"path_id"`
	InlineBase64 string `json:"inline_base64,omitempty"`
	SelectorID   string `json:"selector_id,omitempty"`
}
type PathIdentity struct {
	Version      string
	PathID       string
	ByteLength   int64
	InlineBase64 string
	SelectorID   string
	Display      string
	DisplayValid bool
}
type SourceIdentity struct {
	PacketID               string
	PacketSHA256           string
	LifecycleState         string
	SurfaceContract        registry.SurfaceContractID
	OperationID            registry.OperationID
	ProjectID              string
	RepositoryKey          string
	PublicationID          string
	VaultRelationshipRowID int64
	CommitOID              string
	TreeOID                string
}
type TreeEntry struct {
	Path       PathIdentity
	Basename   PathIdentity
	Mode       string
	ObjectType string
	ObjectOID  string
	Directory  bool
}
type ListTreeRequest struct {
	PacketID        string
	SurfaceContract registry.SurfaceContractID
	OperationID     registry.OperationID
	RepositoryKey   string
	Directory       PathReference
	Recursive       bool
	Limit           int
	Cursor          string
}
type ListTreeResult struct {
	Source    SourceIdentity
	Directory PathIdentity
	Entries   []TreeEntry
	Complete  bool
	Cursor    string
}
type ReadBlobRequest struct {
	PacketID        string
	SurfaceContract registry.SurfaceContractID
	OperationID     registry.OperationID
	RepositoryKey   string
	Path            PathReference
	Offset          int64
	Limit           int64
	Cursor          string
}
type ReadBlobResult struct {
	Source         SourceIdentity
	Path           PathIdentity
	Mode           string
	ObjectType     string
	ObjectOID      string
	Offset         int64
	ReturnedLength int64
	TotalSize      int64
	Bytes          []byte
	Complete       bool
	Cursor         string
}
