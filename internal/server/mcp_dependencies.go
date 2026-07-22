package server

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"

	appaudits "relay/internal/app/audits"
	appoperations "relay/internal/app/operations"
	workflowprojects "relay/internal/app/projects/workflow"
	apptickets "relay/internal/app/tickets"
	appwayfinder "relay/internal/app/wayfinder"
	"relay/internal/mcp"
	"relay/internal/mcp/routecontracts"
	workflowrepos "relay/internal/repos/workflow"
	"relay/internal/sourcegateway"
	"relay/internal/sourcevault"
	workflowstore "relay/internal/store/workflow"
)

type mcpHandler struct {
	Path    string
	Handler http.Handler
}

func buildMCPHandlers(store *workflowstore.Store, vaults *sourcevault.Manager, projects *workflowprojects.Service, packets *appoperations.Service, wayfinder *appwayfinder.Service, tickets *apptickets.Service, audits *appaudits.WorkflowAuditService, log *slog.Logger) ([]mcpHandler, error) {
	if store == nil || vaults == nil || projects == nil || packets == nil || wayfinder == nil || tickets == nil || audits == nil {
		return nil, fmt.Errorf("complete MCP dependencies are required")
	}
	repositories, err := workflowrepos.NewRegistry(store)
	if err != nil {
		return nil, err
	}
	publications, err := appoperations.NewAuthorityPublicationService(store, vaults)
	if err != nil {
		return nil, err
	}
	lifecycle, err := appoperations.NewDefaultLifecycleService(store, repositories, vaults, publications, mcp.NewHTTPSFileParameterFetcher(), packets)
	if err != nil {
		return nil, err
	}
	lifecycleHandler, err := mcp.NewOperationPacketLifecycleHandler(lifecycle)
	if err != nil {
		return nil, err
	}
	key := []byte(strings.TrimSpace(os.Getenv("RELAY_SOURCE_CURSOR_HMAC_KEY")))
	if len(key) < 32 {
		return nil, fmt.Errorf("RELAY_SOURCE_CURSOR_HMAC_KEY must contain at least 32 bytes")
	}
	codec, err := sourcegateway.NewHMACCursorCodec(key)
	if err != nil {
		return nil, err
	}
	source, err := sourcegateway.NewService(packets, vaults, store, codec)
	if err != nil {
		return nil, err
	}
	set, err := routecontracts.BuildMCPRouteManifests()
	if err != nil {
		return nil, err
	}
	owners, err := mcp.NewRouteDispatchers(set, mcp.RouteDispatchServices{Projects: projects, Packets: packets, Lifecycle: lifecycleHandler, Source: source, Wayfinder: wayfinder, Tickets: tickets, Audits: audits, AuditReadback: audits})
	if err != nil {
		return nil, err
	}
	out := make([]mcpHandler, 0, len(set.Manifests))
	for _, manifest := range set.Manifests {
		handlers, err := mcp.BuildRouteHandlers(manifest, owners)
		if err != nil {
			return nil, err
		}
		server, err := mcp.NewServerForRoute(log, mcp.NewWorkflowDepsFromEnv(store, log), manifest, handlers)
		if err != nil {
			return nil, err
		}
		out = append(out, mcpHandler{manifest.RoutePath, mcp.NewHTTPHandler(server, log)})
	}
	if len(out) != 7 {
		return nil, fmt.Errorf("MCP_ROUTE_SET_INCOMPLETE")
	}
	return out, nil
}
