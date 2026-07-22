package mcp

import (
	"encoding/json"
	"fmt"

	"relay/internal/mcp/routecontracts"
)

type RouteDispatchers struct {
	Handlers map[string]map[string]SurfaceHandler
}

func BuildRouteHandlers(manifest routecontracts.RouteManifest, owners RouteDispatchers) ([]ToolHandler, error) {
	routeHandlers, ok := owners.Handlers[manifest.RoutePath]
	if !ok {
		return nil, fmt.Errorf("MCP_DISPATCHER_MISSING: %s", manifest.RoutePath)
	}
	handlers := make([]ToolHandler, 0, len(manifest.Tools))
	for _, tool := range manifest.Tools {
		handler, ok := routeHandlers[tool.Name]
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

// AppToolRegistration binds one public catalog name to one immutable internal
// route registration. The handler remains the route-bound handler selected from
// the compiled manifest; no request field selects route authority.
type AppToolRegistration struct {
	AdvertisedName      string
	InternalToolName    string
	PublicSurface       routecontracts.AppSurface
	InternalRoutePath   string
	SurfaceContract     string
	RouteManifestSHA256 string
	StandingAuthority   routecontracts.StandingAuthorityIdentity
	Tool                routecontracts.ToolManifest
	Aliased             bool
	Handler             ToolHandler
}

func BuildAppSurfaceHandlers(surface routecontracts.AppSurfaceManifest, owners RouteDispatchers) ([]AppToolRegistration, error) {
	manifests := make(map[string]routecontracts.RouteManifest, len(surface.MemberRoutes))
	handlers := make(map[string]map[string]ToolHandler, len(surface.MemberRoutes))
	for _, manifest := range surface.MemberRoutes {
		if _, duplicate := manifests[manifest.RoutePath]; duplicate || manifest.Role != string(surface.Surface) {
			return nil, fmt.Errorf("MCP_APP_SURFACE_ROUTE_INVALID: %s", manifest.RoutePath)
		}
		manifests[manifest.RoutePath] = manifest
		bound, err := BuildRouteHandlers(manifest, owners)
		if err != nil {
			return nil, err
		}
		byName := make(map[string]ToolHandler, len(bound))
		for _, handler := range bound {
			if _, duplicate := byName[handler.Name]; duplicate {
				return nil, fmt.Errorf("MCP_APP_SURFACE_DISPATCHER_DUPLICATE: %s/%s", manifest.RoutePath, handler.Name)
			}
			byName[handler.Name] = handler
		}
		handlers[manifest.RoutePath] = byName
	}
	registrations := make([]AppToolRegistration, 0, len(surface.Tools))
	seen := make(map[string]struct{}, len(surface.Tools))
	for _, tool := range surface.Tools {
		manifest, ok := manifests[tool.InternalRoutePath]
		if !ok || manifest.Role != string(surface.Surface) || manifest.SurfaceContract != tool.SurfaceContract || manifest.ManifestSHA256 != tool.RouteManifestSHA256 {
			return nil, fmt.Errorf("MCP_APP_SURFACE_REGISTRATION_INVALID: %s", tool.AdvertisedName)
		}
		handler, ok := handlers[tool.InternalRoutePath][tool.InternalToolName]
		if !ok || handler.Handle == nil || handler.Name != tool.InternalToolName {
			return nil, fmt.Errorf("MCP_APP_SURFACE_DISPATCHER_MISSING: %s", tool.AdvertisedName)
		}
		if _, duplicate := seen[tool.AdvertisedName]; duplicate {
			return nil, fmt.Errorf("MCP_APP_SURFACE_ALIAS_COLLISION: %s", tool.AdvertisedName)
		}
		seen[tool.AdvertisedName] = struct{}{}
		registrations = append(registrations, AppToolRegistration{
			AdvertisedName: tool.AdvertisedName, InternalToolName: tool.InternalToolName,
			PublicSurface: surface.Surface, InternalRoutePath: tool.InternalRoutePath,
			SurfaceContract: tool.SurfaceContract, RouteManifestSHA256: tool.RouteManifestSHA256,
			StandingAuthority: tool.StandingAuthority, Tool: tool.Tool, Aliased: tool.Aliased, Handler: handler,
		})
	}
	if len(registrations) != len(surface.Tools) {
		return nil, fmt.Errorf("MCP_APP_SURFACE_REGISTRATION_INCOMPLETE")
	}
	return registrations, nil
}
