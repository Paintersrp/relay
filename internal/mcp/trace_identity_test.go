package mcp

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

func TestObserveTraceRequestUsesOnlyExactCanonicalPaths(t *testing.T) {
	cursor := "cursor-secret-material"
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"search_source","arguments":{"operation_id":"planner.requirements","packet_id":"packet-1","project_id":"project-1","repository_key":"relay","revision":{"kind":"commit","commit_oid":"0123456789012345678901234567890123456789"},"path":{"path_id":"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef","inline_base64":"protected-path"},"cursor":"` + cursor + `","text_literal":"` + strings.Repeat("protected-source-body", 4096) + `","mutation_json":{"secret":"do-not-retain"}}}}`
	identity, err := ObserveTraceRequest(strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if identity.JSONRPCMethod != "tools/call" || identity.ToolName != "search_source" || identity.OperationID != "planner.requirements" || identity.PacketID != "packet-1" || identity.ProjectID != "project-1" {
		t.Fatalf("identity=%#v", identity)
	}
	if identity.Source.RepositoryKey != "relay" || identity.Source.RevisionKind != "commit" || identity.Source.CommitOID != "0123456789012345678901234567890123456789" || identity.Source.PathID == "" {
		t.Fatalf("source=%#v", identity.Source)
	}
	digest := sha256.Sum256([]byte(cursor))
	if identity.Source.CursorSHA256 != hex.EncodeToString(digest[:]) {
		t.Fatalf("cursor digest=%s", identity.Source.CursorSHA256)
	}
}

func TestObserveTraceRequestRejectsLookalikesAndUnrelatedIdentityKeys(t *testing.T) {
	body := `{"jsonrpc":"2.0","method":"tools/call","params":{"name":"search_source","arguments":{"packet-id":"protected-a","Packet_ID":"protected-b","protected_body":{"packet_id":"protected-c","repository_key":"protected-d","operation_id":"protected-e"},"mutation_json":{"project_id":"protected-f"}},"protected":{"name":"protected-tool"}},"operation_id":"protected-root"}`
	identity, err := ObserveTraceRequest(strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if identity.ToolName != "search_source" || identity.PacketID != "" || identity.ProjectID != "" || identity.OperationID != "" || identity.Source.RepositoryKey != "" {
		t.Fatalf("identity=%#v", identity)
	}
}

func TestObserveTraceRequestSuppressesEveryDuplicateCanonicalOccurrence(t *testing.T) {
	validOID := "0123456789012345678901234567890123456789"
	validPathID := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	tests := []struct {
		name             string
		arguments        string
		assertSuppressed func(TraceRequestIdentity) bool
	}{
		{name: "valid then null", arguments: `"packet_id":"first","packet_id":null`, assertSuppressed: func(value TraceRequestIdentity) bool { return value.PacketID == "" }},
		{name: "null then valid", arguments: `"packet_id":null,"packet_id":"second"`, assertSuppressed: func(value TraceRequestIdentity) bool { return value.PacketID == "" }},
		{name: "valid then empty", arguments: `"packet_id":"first","packet_id":""`, assertSuppressed: func(value TraceRequestIdentity) bool { return value.PacketID == "" }},
		{name: "empty then valid", arguments: `"packet_id":"","packet_id":"second"`, assertSuppressed: func(value TraceRequestIdentity) bool { return value.PacketID == "" }},
		{name: "valid then wrong type", arguments: `"repository_key":"relay","repository_key":{"value":"other"}`, assertSuppressed: func(value TraceRequestIdentity) bool { return value.Source.RepositoryKey == "" }},
		{name: "wrong type then valid", arguments: `"repository_key":false,"repository_key":"relay"`, assertSuppressed: func(value TraceRequestIdentity) bool { return value.Source.RepositoryKey == "" }},
		{name: "valid hex then invalid hex", arguments: `"revision":{"commit_oid":"` + validOID + `","commit_oid":"not-a-valid-oid"}`, assertSuppressed: func(value TraceRequestIdentity) bool { return value.Source.CommitOID == "" }},
		{name: "invalid hex then valid hex", arguments: `"revision":{"commit_oid":"not-a-valid-oid","commit_oid":"` + validOID + `"}`, assertSuppressed: func(value TraceRequestIdentity) bool { return value.Source.CommitOID == "" }},
		{name: "valid path then null", arguments: `"path":{"path_id":"` + validPathID + `","path_id":null}`, assertSuppressed: func(value TraceRequestIdentity) bool { return value.Source.PathID == "" }},
		{name: "valid cursor then null", arguments: `"cursor":"first","cursor":null`, assertSuppressed: func(value TraceRequestIdentity) bool { return value.Source.CursorSHA256 == "" }},
		{name: "null cursor then valid", arguments: `"cursor":null,"cursor":"second"`, assertSuppressed: func(value TraceRequestIdentity) bool { return value.Source.CursorSHA256 == "" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			body := `{"jsonrpc":"2.0","method":"tools/call","params":{"name":"search_source","arguments":{` + test.arguments + `}}}`
			identity, err := ObserveTraceRequest(strings.NewReader(body))
			if err != nil {
				t.Fatal(err)
			}
			if !test.assertSuppressed(identity) {
				t.Fatalf("duplicate canonical occurrence retained: %#v", identity)
			}
		})
	}
}

func TestObserveTraceResponseUsesOnlyExactAuthoritativeResultPaths(t *testing.T) {
	body := `{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"` + strings.Repeat("source body", 4096) + `"}],"structuredContent":{"Source":{"RepositoryKey":"relay","CommitOID":"0123456789012345678901234567890123456789"},"Complete":false,"Cursor":"next-secret","ObjectOID":"abcdefabcdefabcdefabcdefabcdefabcdefabcd"},"isError":false}}`
	outcome, err := ObserveTraceResponse(strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if outcome.ToolIsError || !outcome.CompleteSet || outcome.Complete || !outcome.HasCursor {
		t.Fatalf("outcome=%#v", outcome)
	}
	if outcome.Source.RepositoryKey != "relay" || outcome.Source.CommitOID == "" || outcome.Source.BlobOID == "" || outcome.Source.CursorSHA256 == "" {
		t.Fatalf("source=%#v", outcome.Source)
	}
}

func TestObserveTraceResponseRejectsProtectedIdentityShapedContent(t *testing.T) {
	body := `{"jsonrpc":"2.0","result":{"content":[{"type":"text","text":"protected"}],"structuredContent":{"Protected":{"Source":{"RepositoryKey":"protected-a","CommitOID":"0123456789012345678901234567890123456789"},"Complete":true,"Cursor":"protected-cursor"},"source":{"repositoryKey":"protected-b"},"complete":true,"cursor":"protected-cursor-2"},"isError":false}}`
	outcome, err := ObserveTraceResponse(strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Source != (TraceSourceIdentity{}) || outcome.CompleteSet || outcome.HasCursor {
		t.Fatalf("protected response content retained: %#v", outcome)
	}
}

func TestObserveTraceResponseSuppressesEveryDuplicateCanonicalOccurrence(t *testing.T) {
	validOID := "0123456789012345678901234567890123456789"
	tests := []struct {
		name             string
		body             string
		assertSuppressed func(TraceResponseOutcome) bool
	}{
		{name: "flag valid then null", body: `{"jsonrpc":"2.0","result":{"structuredContent":{"Complete":true,"Complete":null}}}`, assertSuppressed: func(value TraceResponseOutcome) bool { return !value.CompleteSet }},
		{name: "flag null then valid", body: `{"jsonrpc":"2.0","result":{"structuredContent":{"Complete":null,"Complete":true}}}`, assertSuppressed: func(value TraceResponseOutcome) bool { return !value.CompleteSet }},
		{name: "flag valid then wrong type", body: `{"jsonrpc":"2.0","result":{"structuredContent":{"Bounded":false,"Bounded":"false"}}}`, assertSuppressed: func(value TraceResponseOutcome) bool { return !value.BoundedSet }},
		{name: "tool flag wrong type then valid", body: `{"jsonrpc":"2.0","result":{"isError":0,"isError":true}}`, assertSuppressed: func(value TraceResponseOutcome) bool { return !value.ToolIsError }},
		{name: "error code valid then malformed", body: `{"jsonrpc":"2.0","error":{"code":-32602,"code":1.5}}`, assertSuppressed: func(value TraceResponseOutcome) bool { return value.RPCErrorCode == 0 }},
		{name: "error code malformed then valid", body: `{"jsonrpc":"2.0","error":{"code":"-32602","code":-32602}}`, assertSuppressed: func(value TraceResponseOutcome) bool { return value.RPCErrorCode == 0 }},
		{name: "source valid then null", body: `{"jsonrpc":"2.0","result":{"structuredContent":{"Source":{"CommitOID":"` + validOID + `","CommitOID":null}}}}`, assertSuppressed: func(value TraceResponseOutcome) bool { return value.Source.CommitOID == "" }},
		{name: "source invalid then valid", body: `{"jsonrpc":"2.0","result":{"structuredContent":{"Source":{"CommitOID":"bad","CommitOID":"` + validOID + `"}}}}`, assertSuppressed: func(value TraceResponseOutcome) bool { return value.Source.CommitOID == "" }},
		{name: "cursor valid then empty", body: `{"jsonrpc":"2.0","result":{"structuredContent":{"Cursor":"first","Cursor":""}}}`, assertSuppressed: func(value TraceResponseOutcome) bool { return !value.HasCursor && value.Source.CursorSHA256 == "" }},
		{name: "cursor null then valid", body: `{"jsonrpc":"2.0","result":{"structuredContent":{"Cursor":null,"Cursor":"second"}}}`, assertSuppressed: func(value TraceResponseOutcome) bool { return !value.HasCursor && value.Source.CursorSHA256 == "" }},
		{name: "cursor aliases count as same slot", body: `{"jsonrpc":"2.0","result":{"nextCursor":"first","structuredContent":{"Cursor":null}}}`, assertSuppressed: func(value TraceResponseOutcome) bool { return !value.HasCursor && value.Source.CursorSHA256 == "" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			outcome, err := ObserveTraceResponse(strings.NewReader(test.body))
			if err != nil {
				t.Fatal(err)
			}
			if !test.assertSuppressed(outcome) {
				t.Fatalf("duplicate canonical occurrence retained: %#v", outcome)
			}
		})
	}
}
