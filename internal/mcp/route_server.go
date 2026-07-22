package mcp

import (
	"encoding/json"
	"fmt"
	"log/slog"

	"relay/internal/mcp/routecontracts"
	"relay/internal/operations/registry"
)

type ToolHandler struct {
	Name   string
	Handle SurfaceHandler
}

func NewServerForRoute(log *slog.Logger, deps *MCPDeps, manifest routecontracts.RouteManifest, handlers []ToolHandler) (*Server, error) {
	if len(manifest.Tools) == 0 || len(handlers) != len(manifest.Tools) {
		return nil, fmt.Errorf("MCP_DISPATCHER_MISSING: %s", manifest.RoutePath)
	}
	definitions := make([]ToolDefinition, len(manifest.Tools))
	dispatch := make(map[string]surfaceDispatch, len(manifest.Tools))
	for i, tool := range manifest.Tools {
		handler := handlers[i]
		if handler.Name != tool.Name || handler.Handle == nil {
			return nil, fmt.Errorf("MCP_DISPATCHER_MISSING: %s/%s", manifest.RoutePath, tool.Name)
		}
		if _, dup := dispatch[tool.Name]; dup {
			return nil, fmt.Errorf("MCP_DISPATCHER_MISSING: duplicate %s/%s", manifest.RoutePath, tool.Name)
		}
		dispatch[tool.Name] = surfaceDispatch{surface: registry.SurfaceContractID(manifest.SurfaceContract), handle: handler.Handle}
		definitions[i] = ToolDefinition{Name: tool.Name, Title: tool.Title, Description: tool.Description, InputSchema: append(json.RawMessage(nil), tool.InputSchema...), OutputSchema: append(json.RawMessage(nil), tool.OutputSchema...), Annotations: map[string]any{"readOnlyHint": tool.Annotations.ReadOnlyHint, "destructiveHint": tool.Annotations.DestructiveHint, "idempotentHint": tool.Annotations.IdempotentHint, "openWorldHint": tool.Annotations.OpenWorldHint}, Meta: map[string]any{"openai/widgetAccessible": false, "openai/toolInvocation/invoking": tool.Invoking, "openai/toolInvocation/invoked": tool.Invoked, "openai/fileParams": append([]string(nil), tool.FileParams...), "relay/routePath": manifest.RoutePath, "relay/surfaceContract": manifest.SurfaceContract, "relay/routeManifestSHA256": manifest.ManifestSHA256, "relay/semanticToolID": tool.SemanticToolID, "relay/operationID": tool.OperationID}, orderedAnnotations: orderedRouteAnnotations(tool), orderedMeta: orderedRouteMetadata(manifest, tool)}
	}
	return &Server{log: log, deps: deps, tools: definitions, surfaceHandlers: dispatch}, nil
}

func orderedRouteAnnotations(tool routecontracts.ToolManifest) json.RawMessage {
	raw, _ := json.Marshal(struct {
		ReadOnlyHint    bool `json:"readOnlyHint"`
		DestructiveHint bool `json:"destructiveHint"`
		IdempotentHint  bool `json:"idempotentHint"`
		OpenWorldHint   bool `json:"openWorldHint"`
	}{tool.Annotations.ReadOnlyHint, tool.Annotations.DestructiveHint, tool.Annotations.IdempotentHint, tool.Annotations.OpenWorldHint})
	return raw
}
func orderedRouteMetadata(manifest routecontracts.RouteManifest, tool routecontracts.ToolManifest) json.RawMessage {
	raw, _ := json.Marshal(struct {
		Widget     bool     `json:"openai/widgetAccessible"`
		Invoking   string   `json:"openai/toolInvocation/invoking"`
		Invoked    string   `json:"openai/toolInvocation/invoked"`
		FileParams []string `json:"openai/fileParams"`
		Route      string   `json:"relay/routePath"`
		Surface    string   `json:"relay/surfaceContract"`
		Digest     string   `json:"relay/routeManifestSHA256"`
		Semantic   string   `json:"relay/semanticToolID"`
		Operation  string   `json:"relay/operationID"`
	}{false, tool.Invoking, tool.Invoked, append([]string(nil), tool.FileParams...), manifest.RoutePath, manifest.SurfaceContract, manifest.ManifestSHA256, tool.SemanticToolID, tool.OperationID})
	return raw
}
