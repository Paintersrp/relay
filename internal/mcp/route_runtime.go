package mcp

import (
	"encoding/json"
	"fmt"

	"relay/internal/mcp/routecontracts"
)

type RouteDispatchers struct{ Handlers map[string]SurfaceHandler }

func BuildRouteHandlers(manifest routecontracts.RouteManifest, owners RouteDispatchers) ([]ToolHandler, error) {
	if len(owners.Handlers) != 40 {
		return nil, fmt.Errorf("MCP_DISPATCHER_MISSING: owner cardinality is %d", len(owners.Handlers))
	}
	handlers := make([]ToolHandler, 0, len(manifest.Tools))
	for _, tool := range manifest.Tools {
		handler, ok := owners.Handlers[tool.Name]
		if !ok || handler == nil {
			return nil, fmt.Errorf("MCP_DISPATCHER_MISSING: %s/%s", manifest.RoutePath, tool.Name)
		}
		handlers = append(handlers, ToolHandler{Name: tool.Name, Handle: bindToolToRoute(manifest, tool, handler)})
	}
	return handlers, nil
}

func bindToolToRoute(manifest routecontracts.RouteManifest, tool routecontracts.ToolManifest, next SurfaceHandler) SurfaceHandler {
	return func(raw json.RawMessage) ToolCallResult {
		if len(raw) == 0 {
			raw = json.RawMessage(`{}`)
		}
		return next(raw)
	}
}
