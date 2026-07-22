package transporttrace

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

const SchemaVersion = "relay.transport.mcp-trace.v1"

type CompletionState string

const (
	CompletionComplete      CompletionState = "complete"
	CompletionBounded       CompletionState = "bounded"
	CompletionNotApplicable CompletionState = "not_applicable"
	CompletionUnknown       CompletionState = "unknown"
)

type OutcomeClass string

const (
	OutcomeSuccess            OutcomeClass = "success"
	OutcomeAdmissionRejection OutcomeClass = "admission_rejection"
	OutcomeApplicationFailure OutcomeClass = "application_failure"
	OutcomeSourceFailure      OutcomeClass = "source_failure"
	OutcomeResponseWrite      OutcomeClass = "response_write_failure"
)

type ErrorClass string

const (
	ErrorNone                     ErrorClass = ""
	ErrorMethodNotAllowed         ErrorClass = "method_not_allowed"
	ErrorPathNotFound             ErrorClass = "path_not_found"
	ErrorAuthorizationRejected    ErrorClass = "authorization_rejected"
	ErrorRequestReadFailed        ErrorClass = "request_read_failed"
	ErrorUpstreamUnavailable      ErrorClass = "upstream_unavailable"
	ErrorUpstreamTimeout          ErrorClass = "upstream_timeout"
	ErrorProtocolRejected         ErrorClass = "protocol_rejected"
	ErrorToolRejected             ErrorClass = "tool_rejected"
	ErrorApplicationBlocked       ErrorClass = "application_blocked"
	ErrorSourceBlocked            ErrorClass = "source_blocked"
	ErrorSourceBudgetBoundary     ErrorClass = "source_budget_boundary"
	ErrorDownstreamShortWrite     ErrorClass = "downstream_short_write"
	ErrorDownstreamDisconnected   ErrorClass = "downstream_disconnected"
	ErrorInternalTransportFailure ErrorClass = "internal_transport_failure"
)

type SourceIdentity struct {
	RepositoryKey   string `json:"repository_key,omitempty"`
	RevisionKind    string `json:"revision_kind,omitempty"`
	CommitOID       string `json:"commit_oid,omitempty"`
	BeforeCommitOID string `json:"before_commit_oid,omitempty"`
	AfterCommitOID  string `json:"after_commit_oid,omitempty"`
	AnchorName      string `json:"anchor_name,omitempty"`
	PathID          string `json:"path_id,omitempty"`
	BlobOID         string `json:"blob_oid,omitempty"`
	CursorSHA256    string `json:"cursor_sha256,omitempty"`
}

type DownstreamWrite struct {
	AttemptedBytes int64      `json:"attempted_bytes"`
	WrittenBytes   int64      `json:"written_bytes"`
	Complete       bool       `json:"complete"`
	ErrorClass     ErrorClass `json:"error_class"`
}

type Record struct {
	SchemaVersion       string          `json:"schema_version"`
	RequestID           string          `json:"request_id"`
	StartedAt           string          `json:"started_at"`
	DurationMS          int64           `json:"duration_ms"`
	MappingID           string          `json:"mapping_id"`
	RoutePath           string          `json:"route_path"`
	SurfaceContract     string          `json:"surface_contract"`
	RouteManifestSHA256 string          `json:"route_manifest_sha256"`
	JSONRPCMethod       string          `json:"jsonrpc_method"`
	ToolName            string          `json:"tool_name"`
	OperationID         string          `json:"operation_id"`
	PacketID            string          `json:"packet_id"`
	ProjectID           string          `json:"project_id"`
	SourceIdentity      SourceIdentity  `json:"source_identity"`
	RequestSizeBytes    int64           `json:"request_size_bytes"`
	ResponseSizeBytes   int64           `json:"response_size_bytes"`
	ResponseSHA256      string          `json:"response_sha256"`
	CompletionState     CompletionState `json:"completion_state"`
	OutcomeClass        OutcomeClass    `json:"outcome_class"`
	ErrorClass          ErrorClass      `json:"error_class"`
	DownstreamWrite     DownstreamWrite `json:"downstream_write"`
}

func MarshalLine(record Record) ([]byte, error) {
	if err := record.Validate(); err != nil {
		return nil, err
	}
	value, err := json.Marshal(record)
	if err != nil {
		return nil, fmt.Errorf("marshal transport trace: %w", err)
	}
	return append(value, '\n'), nil
}

func (record Record) Validate() error {
	if record.SchemaVersion != SchemaVersion {
		return fmt.Errorf("transport trace schema version is invalid")
	}
	if !validLowerHex(record.RequestID, 32) {
		return fmt.Errorf("transport trace request ID is invalid")
	}
	if strings.TrimSpace(record.StartedAt) == "" || record.DurationMS < 0 ||
		strings.TrimSpace(record.MappingID) == "" || strings.TrimSpace(record.RoutePath) == "" ||
		strings.TrimSpace(record.SurfaceContract) == "" || !validLowerHex(record.RouteManifestSHA256, 64) {
		return fmt.Errorf("transport trace route identity is invalid")
	}
	if record.RequestSizeBytes < 0 || record.ResponseSizeBytes < 0 || !validLowerHex(record.ResponseSHA256, 64) {
		return fmt.Errorf("transport trace byte evidence is invalid")
	}
	if !validCompletion(record.CompletionState) || !validOutcome(record.OutcomeClass) || !validError(record.ErrorClass) {
		return fmt.Errorf("transport trace outcome is invalid")
	}
	if record.DownstreamWrite.AttemptedBytes < 0 || record.DownstreamWrite.WrittenBytes < 0 ||
		record.DownstreamWrite.WrittenBytes > record.DownstreamWrite.AttemptedBytes ||
		!validError(record.DownstreamWrite.ErrorClass) {
		return fmt.Errorf("transport trace downstream write is invalid")
	}
	writeComplete := record.DownstreamWrite.WrittenBytes == record.DownstreamWrite.AttemptedBytes && record.DownstreamWrite.ErrorClass == ErrorNone
	if record.DownstreamWrite.Complete != writeComplete {
		return fmt.Errorf("transport trace downstream completion is inconsistent")
	}
	if record.OutcomeClass == OutcomeSuccess && record.ErrorClass != ErrorNone {
		return fmt.Errorf("successful transport trace cannot carry an error class")
	}
	if record.OutcomeClass == OutcomeResponseWrite && record.DownstreamWrite.ErrorClass == ErrorNone {
		return fmt.Errorf("response-write failure requires downstream error evidence")
	}
	return nil
}

func validCompletion(value CompletionState) bool {
	switch value {
	case CompletionComplete, CompletionBounded, CompletionNotApplicable, CompletionUnknown:
		return true
	default:
		return false
	}
}

func validOutcome(value OutcomeClass) bool {
	switch value {
	case OutcomeSuccess, OutcomeAdmissionRejection, OutcomeApplicationFailure, OutcomeSourceFailure, OutcomeResponseWrite:
		return true
	default:
		return false
	}
}

func validError(value ErrorClass) bool {
	switch value {
	case ErrorNone, ErrorMethodNotAllowed, ErrorPathNotFound, ErrorAuthorizationRejected,
		ErrorRequestReadFailed, ErrorUpstreamUnavailable, ErrorUpstreamTimeout,
		ErrorProtocolRejected, ErrorToolRejected, ErrorApplicationBlocked,
		ErrorSourceBlocked, ErrorSourceBudgetBoundary, ErrorDownstreamShortWrite,
		ErrorDownstreamDisconnected, ErrorInternalTransportFailure:
		return true
	default:
		return false
	}
}

func validLowerHex(value string, size int) bool {
	if len(value) != size || strings.ToLower(value) != value {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
