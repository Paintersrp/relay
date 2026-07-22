package transporttrace

import (
	"bytes"
	"strings"
	"testing"
)

func TestMarshalLineUsesExactFieldOrderAndNoProtectedContent(t *testing.T) {
	record := completeTestRecord("planner", strings.Repeat("e", 64))
	record.RequestID = "0123456789abcdef0123456789abcdef"
	record.DurationMS = 17
	record.OperationID = "planner.requirements"
	record.PacketID = "packet-1"
	record.ProjectID = "project-1"
	record.SourceIdentity = SourceIdentity{RepositoryKey: "relay", CommitOID: strings.Repeat("b", 40), PathID: strings.Repeat("c", 64), CursorSHA256: strings.Repeat("d", 64)}
	record.RequestSizeBytes = 31
	record.ResponseSizeBytes = 47
	record.DownstreamWrite = DownstreamWrite{AttemptedBytes: 47, WrittenBytes: 47, Complete: true, ErrorClass: ErrorNone}
	line, err := MarshalLine(record)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasSuffix(line, []byte{'\n'}) {
		t.Fatal("trace record is not LF terminated")
	}
	ordered := []string{"schema_version", "request_id", "started_at", "duration_ms", "mapping_id", "public_surface", "public_surface_manifest_sha256", "route_path", "jsonrpc_method", "tool_name", "public_advertised_tool_name", "internal_tool_name", "internal_route_path", "surface_contract", "route_manifest_sha256", "standing_authority_repository", "standing_authority_commit_oid", "standing_authority_path", "standing_authority_blob_oid", "operation_id", "packet_id", "project_id", "source_identity", "request_size_bytes", "response_size_bytes", "response_sha256", "completion_state", "outcome_class", "error_class", "downstream_write"}
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
	record := testRecord("auditor", strings.Repeat("c", 64))
	record.RequestID = strings.Repeat("a", 32)
	record.OutcomeClass = OutcomeResponseWrite
	record.ErrorClass = ErrorDownstreamShortWrite
	record.DownstreamWrite = DownstreamWrite{AttemptedBytes: 10, WrittenBytes: 9, Complete: true, ErrorClass: ErrorDownstreamShortWrite}
	if _, err := MarshalLine(record); err == nil {
		t.Fatal("inconsistent downstream evidence was accepted")
	}
}

func completeTestRecord(surface, digest string) Record {
	return Record{
		SchemaVersion: SchemaVersion, RequestID: strings.Repeat("a", 32), StartedAt: "2026-07-22T12:00:00Z",
		MappingID: surface, PublicSurface: surface, PublicSurfaceManifestSHA256: strings.Repeat("a", 64), RoutePath: "/mcp/" + surface,
		JSONRPCMethod: "tools/call", ToolName: "planner-authoring-v1__search_source", PublicAdvertisedToolName: "planner-authoring-v1__search_source",
		InternalToolName: "search_source", InternalRoutePath: "/mcp/v1/planner/authoring", SurfaceContract: "planner-authoring.v1", RouteManifestSHA256: strings.Repeat("b", 64),
		StandingAuthorityRepository: "Paintersrp/relay-specs", StandingAuthorityCommitOID: strings.Repeat("c", 40), StandingAuthorityPath: "agents/planner.md", StandingAuthorityBlobOID: strings.Repeat("d", 40),
		ResponseSHA256: digest, CompletionState: CompletionNotApplicable, OutcomeClass: OutcomeSuccess, DownstreamWrite: DownstreamWrite{Complete: true},
	}
}
