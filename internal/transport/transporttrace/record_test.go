package transporttrace

import (
	"bytes"
	"strings"
	"testing"
)

func TestMarshalLineUsesExactFieldOrderAndNoProtectedContent(t *testing.T) {
	record := Record{
		SchemaVersion:       SchemaVersion,
		RequestID:           "0123456789abcdef0123456789abcdef",
		StartedAt:           "2026-07-22T12:00:00Z",
		DurationMS:          17,
		MappingID:           "planner-authoring",
		RoutePath:           "/mcp/v1/planner/authoring",
		SurfaceContract:     "planner-authoring.v1",
		RouteManifestSHA256: strings.Repeat("a", 64),
		JSONRPCMethod:       "tools/call",
		ToolName:            "search_source",
		OperationID:         "planner.requirements",
		PacketID:            "packet-1",
		ProjectID:           "project-1",
		SourceIdentity: SourceIdentity{
			RepositoryKey: "relay",
			CommitOID:     strings.Repeat("b", 40),
			PathID:        strings.Repeat("c", 64),
			CursorSHA256:  strings.Repeat("d", 64),
		},
		RequestSizeBytes:  31,
		ResponseSizeBytes: 47,
		ResponseSHA256:    strings.Repeat("e", 64),
		CompletionState:   CompletionBounded,
		OutcomeClass:      OutcomeSuccess,
		ErrorClass:        ErrorNone,
		DownstreamWrite: DownstreamWrite{
			AttemptedBytes: 47,
			WrittenBytes:   47,
			Complete:       true,
			ErrorClass:     ErrorNone,
		},
	}
	line, err := MarshalLine(record)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasSuffix(line, []byte{'\n'}) {
		t.Fatal("trace record is not LF terminated")
	}
	ordered := []string{"schema_version", "request_id", "started_at", "duration_ms", "mapping_id", "route_path", "surface_contract", "route_manifest_sha256", "jsonrpc_method", "tool_name", "operation_id", "packet_id", "project_id", "source_identity", "request_size_bytes", "response_size_bytes", "response_sha256", "completion_state", "outcome_class", "error_class", "downstream_write"}
	position := -1
	for _, name := range ordered {
		next := bytes.Index(line, []byte(`"`+name+`"`))
		if next <= position {
			t.Fatalf("field %s is out of order", name)
		}
		position = next
	}
	for _, prohibited := range []string{"authorization", "signed_url", "source body", "conversation", "mutation_json", "inline_base64", "cursor-secret"} {
		if bytes.Contains(bytes.ToLower(line), []byte(prohibited)) {
			t.Fatalf("prohibited content present: %s", prohibited)
		}
	}
}

func TestRecordRejectsInconsistentDownstreamEvidence(t *testing.T) {
	record := Record{
		SchemaVersion:       SchemaVersion,
		RequestID:           strings.Repeat("a", 32),
		StartedAt:           "2026-07-22T12:00:00Z",
		MappingID:           "auditor-audit",
		RoutePath:           "/mcp/v1/auditor/audit",
		SurfaceContract:     "auditor-audit.v1",
		RouteManifestSHA256: strings.Repeat("b", 64),
		ResponseSHA256:      strings.Repeat("c", 64),
		CompletionState:     CompletionNotApplicable,
		OutcomeClass:        OutcomeResponseWrite,
		ErrorClass:          ErrorDownstreamShortWrite,
		DownstreamWrite:     DownstreamWrite{AttemptedBytes: 10, WrittenBytes: 9, Complete: true, ErrorClass: ErrorDownstreamShortWrite},
	}
	if _, err := MarshalLine(record); err == nil {
		t.Fatal("inconsistent downstream evidence was accepted")
	}
}
