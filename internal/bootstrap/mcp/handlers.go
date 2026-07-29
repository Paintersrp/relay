// Package mcpbootstrap assembles published MCP applications before they are
// handed to the HTTP transport adapter.
package mcpbootstrap

import (
	"fmt"
	"log/slog"
	"os"
	"strings"

	appaudits "relay/internal/app/audits"
	"relay/internal/app/mcpcomposition"
	workflowprojects "relay/internal/app/projects/workflow"
	apptickets "relay/internal/app/tickets"
	appwayfinder "relay/internal/app/wayfinder"
	"relay/internal/mcp"
	"relay/internal/mcp/routecontracts"
	"relay/internal/server"
	workflowstore "relay/internal/store/workflow"
)

func BuildHandlers(store *workflowstore.Store, policy mcpcomposition.Services, log *slog.Logger) ([]server.MCPHandler, error) {
	if store == nil || policy.Vaults == nil || policy.Packets == nil || policy.Lifecycle == nil || policy.Source == nil {
		return nil, fmt.Errorf("complete MCP application dependencies are required")
	}
	projects, err := workflowprojects.NewService(store)
	if err != nil {
		return nil, err
	}
	wayfinder, err := appwayfinder.NewService(store)
	if err != nil {
		return nil, err
	}
	tickets, err := apptickets.NewService(store)
	if err != nil {
		return nil, err
	}
	audits, err := appaudits.NewWorkflowAuditServiceWithSourceVaults(store, policy.Vaults)
	if err != nil {
		return nil, err
	}
	lifecycle, err := mcp.NewOperationPacketLifecycleHandler(policy.Lifecycle)
	if err != nil {
		return nil, err
	}
	routes, err := routecontracts.BuildMCPRouteManifests()
	if err != nil {
		return nil, err
	}
	surfaces, err := routecontracts.BuildAppSurfaceManifests(routes)
	if err != nil {
		return nil, err
	}
	owners, err := mcp.NewRouteDispatchers(routes, mcp.RouteDispatchServices{Projects: projects, Packets: policy.Packets, Lifecycle: lifecycle, Source: policy.Source, Wayfinder: wayfinder, Tickets: tickets, Audits: audits, AuditReadback: audits})
	if err != nil {
		return nil, err
	}
	output := make([]server.MCPHandler, 0, len(surfaces.Surfaces))
	for _, surface := range surfaces.Surfaces {
		registrations, err := mcp.BuildAppSurfaceHandlers(surface, owners)
		if err != nil {
			return nil, err
		}
		application, err := mcp.NewServerForAppSurface(log, mcp.NewWorkflowDepsFromEnv(store, log, policy.Vaults), surface, registrations)
		if err != nil {
			return nil, err
		}
		output = append(output, server.MCPHandler{Path: surface.PublicPath, PublicSurface: string(surface.Surface), PublicSurfaceManifestSHA256: surface.ManifestSHA256, ToolRegistrations: registrations, Handler: mcp.NewHTTPHandler(application, log)})
	}
	if len(output) != 3 {
		return nil, fmt.Errorf("MCP_APP_SURFACE_SET_INCOMPLETE")
	}
	return output, nil
}

func SourceCursorKeyFromEnv() ([]byte, error) {
	key := []byte(strings.TrimSpace(os.Getenv("RELAY_SOURCE_CURSOR_HMAC_KEY")))
	if len(key) < 32 {
		return nil, fmt.Errorf("RELAY_SOURCE_CURSOR_HMAC_KEY must contain at least 32 bytes")
	}
	return key, nil
}
