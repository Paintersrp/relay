package sourcegateway

import (
	"context"

	"relay/internal/operations/registry"
	"relay/internal/sourcevault"
)

const (
	MinTextPageBytes         int64 = 4
	MaxTextPageBytes         int64 = 1 << 20
	MaxCommitPageBytes       int64 = 1 << 20
	MaxCommitHeaderEntries         = 256
	MaxCommitParentEntries         = 256
	MaxHistoryPageEntries          = 256
	MaxComparisonPageEntries       = 512
	MaxDiffPageBytes         int64 = 1 << 20
	textValidationChunkBytes int64 = 64 << 10
	binaryProbeBytes         int64 = 8000
)

type RevisionReference struct{ AnchorName string }

type FidelityVaultReader interface {
	ReadRetainedCommitRange(context.Context, sourcevault.ReadRetainedCommitRangeRequest) (sourcevault.ReadRetainedCommitRangeResult, error)
	ReadRetainedCommitNode(context.Context, sourcevault.ReadRetainedCommitNodeRequest) (sourcevault.RetainedCommitNode, error)
	ReadRetainedComparison(context.Context, sourcevault.ReadRetainedComparisonRequest) (sourcevault.ReadRetainedComparisonResult, error)
	ReadRetainedDiffRange(context.Context, sourcevault.ReadRetainedDiffRangeRequest) (sourcevault.ReadRetainedDiffRangeResult, error)
}

type TextSegment struct {
	StartOffset, EndOffset                 int64
	Bytes, Terminator                      []byte
	ContinuesLine, LineComplete, FinalLine bool
}
type ReadTextRequest struct {
	PacketID        string
	SurfaceContract registry.SurfaceContractID
	OperationID     registry.OperationID
	RepositoryKey   string
	Revision        RevisionReference
	Path            PathReference
	Offset, Limit   int64
	Cursor          string
}
type ReadTextResult struct {
	Source                        SourceIdentity
	Path                          PathIdentity
	Mode, ObjectOID               string
	Segments                      []TextSegment
	Offset, NextOffset, TotalSize int64
	Complete                      bool
	Cursor                        string
}

type CommitSummary struct {
	CommitOID, TreeOID                   string
	ParentCount, RawSize, MessageSize    int64
	AuthorRaw, CommitterRaw, EncodingRaw []byte
	SignatureRaw                         [][]byte
}
type ReadCommitBytesRequest struct {
	PacketID        string
	SurfaceContract registry.SurfaceContractID
	OperationID     registry.OperationID
	RepositoryKey   string
	Revision        RevisionReference
	CommitOID       string
	Offset, Limit   int64
	Cursor          string
}
type ReadCommitBytesResult struct {
	Source                            SourceIdentity
	Summary                           CommitSummary
	Offset, ReturnedLength, TotalSize int64
	Bytes                             []byte
	Complete                          bool
	Cursor                            string
}
type CommitHeader struct{ Name, Value, Raw []byte }
type ReadCommitHeadersRequest struct {
	PacketID        string
	SurfaceContract registry.SurfaceContractID
	OperationID     registry.OperationID
	RepositoryKey   string
	Revision        RevisionReference
	CommitOID       string
	Limit           int
	Cursor          string
}
type ReadCommitHeadersResult struct {
	Source   SourceIdentity
	Summary  CommitSummary
	Headers  []CommitHeader
	Complete bool
	Cursor   string
}
type ReadCommitParentsRequest struct {
	PacketID        string
	SurfaceContract registry.SurfaceContractID
	OperationID     registry.OperationID
	RepositoryKey   string
	Revision        RevisionReference
	CommitOID       string
	Limit           int
	Cursor          string
}
type ReadCommitParentsResult struct {
	Source     SourceIdentity
	Summary    CommitSummary
	ParentOIDs []string
	Complete   bool
	Cursor     string
}
type ReadCommitMessageRequest struct {
	PacketID        string
	SurfaceContract registry.SurfaceContractID
	OperationID     registry.OperationID
	RepositoryKey   string
	Revision        RevisionReference
	CommitOID       string
	Offset, Limit   int64
	Cursor          string
}
type ReadCommitMessageResult struct {
	Source                            SourceIdentity
	Summary                           CommitSummary
	Offset, ReturnedLength, TotalSize int64
	Bytes                             []byte
	Complete                          bool
	Cursor                            string
}

type CommitHistoryEntry struct {
	CommitOID, TreeOID string
	ParentOIDs         []string
	RawSize, Distance  int64
}
type CommitHistoryRequest struct {
	PacketID        string
	SurfaceContract registry.SurfaceContractID
	OperationID     registry.OperationID
	RepositoryKey   string
	Revision        RevisionReference
	Limit           int
	Cursor          string
}
type CommitHistoryResult struct {
	Source   SourceIdentity
	Entries  []CommitHistoryEntry
	Complete bool
	Cursor   string
}
type PathState struct {
	Present                     bool
	Mode, ObjectType, ObjectOID string
}
type PathHistoryEntry struct {
	CommitOID, TreeOID string
	ParentOIDs         []string
	Distance           int64
	State              PathState
}
type PathHistoryRequest struct {
	PacketID        string
	SurfaceContract registry.SurfaceContractID
	OperationID     registry.OperationID
	RepositoryKey   string
	Revision        RevisionReference
	Path            PathReference
	PathSeed        []byte
	Limit           int
	Cursor          string
}
type PathHistoryResult struct {
	Source   SourceIdentity
	Path     PathIdentity
	Entries  []PathHistoryEntry
	Complete bool
	Cursor   string
}

type ChangeKind string

const (
	ChangeAddition     ChangeKind = "addition"
	ChangeDeletion     ChangeKind = "deletion"
	ChangeModification ChangeKind = "modification"
	ChangeRename       ChangeKind = "rename"
	ChangeCopy         ChangeKind = "copy"
	ChangeBinary       ChangeKind = "binary_change"
	ChangeType         ChangeKind = "type_change"
	ChangeMode         ChangeKind = "mode_change"
)

type ComparisonEntry struct {
	EntryID                                       string
	Kind                                          ChangeKind
	BeforePath                                    *PathIdentity
	BeforeMode, BeforeObjectType, BeforeObjectOID string
	AfterPath                                     *PathIdentity
	AfterMode, AfterObjectType, AfterObjectOID    string
}
type CompareRequest struct {
	PacketID        string
	SurfaceContract registry.SurfaceContractID
	OperationID     registry.OperationID
	RepositoryKey   string
	Before, After   RevisionReference
	Limit           int
	Cursor          string
}
type CompareResult struct {
	Before, After SourceIdentity
	Entries       []ComparisonEntry
	Complete      bool
	Cursor        string
}
type ReadDiffRequest struct {
	PacketID        string
	SurfaceContract registry.SurfaceContractID
	OperationID     registry.OperationID
	RepositoryKey   string
	Before, After   RevisionReference
	Offset, Limit   int64
	Cursor          string
}
type ReadDiffResult struct {
	Before, After                     SourceIdentity
	Offset, ReturnedLength, TotalSize int64
	Bytes                             []byte
	Complete                          bool
	Cursor                            string
}
