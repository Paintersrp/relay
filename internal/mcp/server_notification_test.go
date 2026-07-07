package mcp

import (
	"encoding/json"
	"testing"
)

func TestHandleLineWithSkipNotificationsInitializedProducesNoResponse(t *testing.T) {
	srv := NewServer(discardLogger())

	resp, skip := srv.handleLineWithSkip([]byte(`{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}`))
	if !skip {
		t.Fatal("expected notifications/initialized to be treated as a notification")
	}
	if resp.JSONRPC != "" || len(resp.ID) != 0 || resp.Result != nil || resp.Error != nil {
		t.Fatalf("expected zero-value response for notification, got %+v", resp)
	}
}

func TestHandleLineWithSkipUnknownNotificationProducesNoResponse(t *testing.T) {
	srv := NewServer(discardLogger())

	resp, skip := srv.handleLineWithSkip([]byte(`{"jsonrpc":"2.0","method":"notifications/somethingElse","params":{}}`))
	if !skip {
		t.Fatal("expected unknown no-id notification to be skipped")
	}
	if resp.JSONRPC != "" || len(resp.ID) != 0 || resp.Result != nil || resp.Error != nil {
		t.Fatalf("expected zero-value response for notification, got %+v", resp)
	}
}

func TestHandleLineWithSkipInitializeRequestStillResponds(t *testing.T) {
	srv := NewServer(discardLogger())
	params, _ := json.Marshal(InitializeParams{ProtocolVersion: MCPProtocolVersion})
	req := Request{
		JSONRPC: JSONRPCVersion,
		ID:      json.RawMessage(`1`),
		Method:  "initialize",
		Params:  params,
	}

	resp, skip := srv.handleLineWithSkip(mustMarshal(t, req))
	if skip {
		t.Fatal("expected initialize request with id to produce a response")
	}
	if resp.JSONRPC != JSONRPCVersion {
		t.Fatalf("expected jsonrpc=%q, got %q", JSONRPCVersion, resp.JSONRPC)
	}
	if resp.Error != nil {
		t.Fatalf("expected initialize success, got error %+v", resp.Error)
	}
	if resp.Result == nil {
		t.Fatal("expected initialize result")
	}
}

func TestHandleLineWithSkipUnknownRequestStillErrors(t *testing.T) {
	srv := NewServer(discardLogger())
	req := Request{
		JSONRPC: JSONRPCVersion,
		ID:      json.RawMessage(`99`),
		Method:  "unknown/request",
	}

	resp, skip := srv.handleLineWithSkip(mustMarshal(t, req))
	if skip {
		t.Fatal("expected unknown request with id to produce an error response")
	}
	if resp.Error == nil || resp.Error.Code != CodeMethodNotFound {
		t.Fatalf("expected method-not-found error, got %+v", resp.Error)
	}
}

func TestServerToolsList_ExactMatch(t *testing.T) {
	srv := NewServer(discardLogger())
	list := collectAllTools(t, srv, ToolsListParams{})

	expectedTools := toolNames(canonicalToolDefinitions(ToolProfilePlanner))

	if len(list.Tools) != len(expectedTools) {
		t.Fatalf("expected exactly %d tools, got %d", len(expectedTools), len(list.Tools))
	}

	for i, name := range expectedTools {
		if list.Tools[i].Name != name {
			t.Fatalf("expected tool at %d to be %q, got %q", i, name, list.Tools[i].Name)
		}
	}
}

func TestServerToolsList_BrokerFlagDoesNotExpandCanonicalSurface(t *testing.T) {
	harness := newCanonicalTestHarness(t, ToolProfileLocalOperator)
	deps := harness.server.deps
	deps.ToolProfile = ToolProfileLocalOperator
	deps.ContextBrokerEnabled = true
	srv := NewServer(discardLogger(), deps)
	list := collectAllTools(t, srv, ToolsListParams{})

	expectedTools := toolNames(canonicalToolDefinitions(ToolProfileLocalOperator))
	if len(list.Tools) != len(expectedTools) {
		t.Fatalf("expected exactly %d tools, got %d", len(expectedTools), len(list.Tools))
	}
	for i, name := range expectedTools {
		if list.Tools[i].Name != name {
			t.Fatalf("expected tool at %d to be %q, got %q", i, name, list.Tools[i].Name)
		}
	}
}

func TestHandleLineWithSkipPingRequestResponds(t *testing.T) {
	srv := NewServer(discardLogger())
	req := Request{
		JSONRPC: JSONRPCVersion,
		ID:      json.RawMessage(`1`),
		Method:  "ping",
	}
	resp, skip := srv.handleLineWithSkip(mustMarshal(t, req))
	if skip {
		t.Fatal("expected ping request with id not to be skipped")
	}
	if resp.Error != nil {
		t.Fatalf("expected ping success, got error %+v", resp.Error)
	}
}
