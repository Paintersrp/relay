package transporttrace

import (
	"testing"

	"relay/internal/mcp"
)

func TestClassifyDistinguishesSourceAndWriteOutcomes(t *testing.T) {
	request := mcp.TraceRequestIdentity{ToolName: "search_source", Source: mcp.TraceSourceIdentity{RepositoryKey: "relay"}}
	bounded := Classify(request, mcp.TraceResponseOutcome{CompleteSet: true, Complete: false, HasCursor: true}, 200, DownstreamWrite{AttemptedBytes: 10, WrittenBytes: 10, Complete: true})
	if bounded.Outcome != OutcomeSuccess || bounded.Completion != CompletionBounded || bounded.Error != ErrorNone || bounded.Source.RepositoryKey != "relay" {
		t.Fatalf("bounded=%#v", bounded)
	}
	failed := Classify(request, mcp.TraceResponseOutcome{ToolIsError: true}, 200, DownstreamWrite{AttemptedBytes: 10, WrittenBytes: 10, Complete: true})
	if failed.Outcome != OutcomeSourceFailure || failed.Error != ErrorSourceBlocked {
		t.Fatalf("failed=%#v", failed)
	}
	write := Classify(request, mcp.TraceResponseOutcome{}, 200, DownstreamWrite{AttemptedBytes: 10, WrittenBytes: 4, ErrorClass: ErrorDownstreamShortWrite})
	if write.Outcome != OutcomeResponseWrite || write.Error != ErrorDownstreamShortWrite {
		t.Fatalf("write=%#v", write)
	}
}

func TestClassifyDistinguishesAdmissionAndApplicationFailure(t *testing.T) {
	request := mcp.TraceRequestIdentity{JSONRPCMethod: "tools/call", ToolName: "list_projects"}
	admission := Classify(request, mcp.TraceResponseOutcome{RPCErrorCode: mcp.CodeMethodNotFound}, 200, DownstreamWrite{Complete: true})
	if admission.Outcome != OutcomeAdmissionRejection || admission.Error != ErrorToolRejected {
		t.Fatalf("admission=%#v", admission)
	}
	application := Classify(request, mcp.TraceResponseOutcome{ToolIsError: true}, 200, DownstreamWrite{Complete: true})
	if application.Outcome != OutcomeApplicationFailure || application.Error != ErrorApplicationBlocked {
		t.Fatalf("application=%#v", application)
	}
}
