package server

import (
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"relay/internal/transport/mcpingress"
)

func TestNewWorkflowReturnsErrorForIncompleteMCPHandlerSet(t *testing.T) {
	server, err := NewWorkflow(nil, nil, "owner-test", nil, nil)
	if err == nil {
		t.Fatal("expected incomplete MCP handler error")
	}
	if server != nil {
		t.Fatal("server was returned with an incomplete MCP handler set")
	}
	if !strings.Contains(err.Error(), "construct MCP handler set") {
		t.Fatalf("error = %q", err)
	}
}

func TestBuildWorkflowRuntimeReturnsErrorForNilWorkflowStore(t *testing.T) {
	handler, routes, err := buildWorkflowRuntime(nil, nil, "owner-test", nil, nil)
	if err == nil {
		t.Fatal("expected nil workflow store error")
	}
	if handler != nil || routes != nil {
		t.Fatalf("partial runtime = handler %v, routes %#v", handler, routes)
	}
	if !strings.Contains(err.Error(), "workflow store is required") {
		t.Fatalf("error = %q", err)
	}
}

func TestBuildWorkflowRuntimePropagatesAuditConstructionError(t *testing.T) {
	store, _, _ := openWorkflowRouteTestStore(t)
	handler, routes, err := buildWorkflowRuntime(store, nil, "owner-test", nil, nil)
	if err == nil {
		t.Fatal("expected audit construction error")
	}
	if handler != nil || routes != nil {
		t.Fatalf("partial runtime = handler %v, routes %#v", handler, routes)
	}
	if !strings.Contains(err.Error(), "construct audit service") {
		t.Fatalf("error = %q", err)
	}
}

func TestBuildWorkflowRuntimeReturnsNoPartialResultsForRouteDescriptorError(t *testing.T) {
	store, _, vaults := openWorkflowRouteTestStore(t)
	handlers := workflowConstructionMCPHandlers()
	handlers[0].ToolRegistrations = nil
	handler, routes, err := buildWorkflowRuntime(store, nil, "owner-test", vaults, handlers)
	if err == nil {
		t.Fatal("expected MCP route descriptor construction error")
	}
	if handler != nil || routes != nil {
		t.Fatalf("partial runtime = handler %v, routes %#v", handler, routes)
	}
	if !strings.Contains(err.Error(), "construct MCP route descriptors") {
		t.Fatalf("error = %q", err)
	}
}

func TestNewWorkflowReturnsHandlerAndAllMCPRoutes(t *testing.T) {
	store, _, vaults := openWorkflowRouteTestStore(t)
	workflowServer, err := NewWorkflow(store, slog.New(slog.NewTextHandler(io.Discard, nil)), "owner-test", vaults, workflowConstructionMCPHandlers())
	if err != nil {
		t.Fatal(err)
	}
	if workflowServer == nil || workflowServer.Handler() == nil {
		t.Fatal("successful construction did not return a server handler")
	}
	if len(workflowServer.mcpRoutes) != len(mcpingress.Catalog()) {
		t.Fatalf("MCP routes = %d, want %d", len(workflowServer.mcpRoutes), len(mcpingress.Catalog()))
	}
}

func workflowConstructionMCPHandlers() []MCPHandler {
	catalog := mcpingress.Catalog()
	handlers := make([]MCPHandler, len(catalog))
	for index, mapping := range catalog {
		handlers[index] = testMCPHandler(mapping, index)
		handlers[index].Handler = http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	}
	return handlers
}
