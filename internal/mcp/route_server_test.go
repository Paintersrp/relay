package mcp

import (
	"encoding/json"
	"relay/internal/mcp/routecontracts"
	"testing"
)

func TestRouteServerUsesOnlyExactRouteHandlers(t *testing.T) {
	set, err := routecontracts.BuildMCPRouteManifests()
	if err != nil {
		t.Fatal(err)
	}
	manifest := set.Manifests[3]
	handlers := make([]ToolHandler, len(manifest.Tools))
	for i, tool := range manifest.Tools {
		name := tool.Name
		handlers[i] = ToolHandler{Name: name, Handle: func(json.RawMessage) ToolCallResult { return workflowOK(map[string]any{"tool": name}) }}
	}
	server, err := NewServerForRoute(nil, nil, manifest, handlers)
	if err != nil {
		t.Fatal(err)
	}
	if len(server.tools) != len(manifest.Tools) {
		t.Fatalf("tools=%d", len(server.tools))
	}
	if server.toolRegistered("record_audit_decision") {
		t.Fatal("cross-route tool registered")
	}
	response := server.handleToolsCall(Request{ID: json.RawMessage("1"), Params: json.RawMessage(`{"name":"record_audit_decision","arguments":{}}`)})
	if response.Error == nil || response.Error.Code != CodeMethodNotFound {
		t.Fatalf("response=%#v", response)
	}
}
