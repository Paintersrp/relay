package mcp

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

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
		dispatch[tool.Name] = surfaceDispatch{surface: registry.SurfaceContractID(manifest.SurfaceContract), toolName: tool.Name, handle: handler.Handle}
		definitions[i] = routeToolDefinition(tool.Name, tool.Description, tool, manifest, "", "")
	}
	return &Server{log: log, deps: deps, tools: definitions, surfaceHandlers: dispatch}, nil
}

// NewServerForAppSurface exposes one role-level catalog while every dispatch
// remains bound to one immutable internal route registration.
func NewServerForAppSurface(log *slog.Logger, deps *MCPDeps, surface routecontracts.AppSurfaceManifest, registrations []AppToolRegistration) (*Server, error) {
	if len(surface.MemberRoutes) == 0 || len(surface.Tools) == 0 || len(registrations) != len(surface.Tools) || surface.ManifestSHA256 == "" {
		return nil, fmt.Errorf("MCP_APP_SURFACE_SERVER_INVALID: %s", surface.Surface)
	}
	definitions := make([]ToolDefinition, 0, len(registrations))
	dispatch := make(map[string]surfaceDispatch, len(registrations))
	for index, registration := range registrations {
		compiled := surface.Tools[index]
		if registration.AdvertisedName == "" || registration.AdvertisedName != compiled.AdvertisedName ||
			registration.PublicSurface != surface.Surface || registration.InternalToolName != compiled.InternalToolName ||
			registration.InternalRoutePath != compiled.InternalRoutePath || registration.SurfaceContract != compiled.SurfaceContract ||
			registration.RouteManifestSHA256 != compiled.RouteManifestSHA256 || registration.StandingAuthority != compiled.StandingAuthority ||
			registration.Handler.Name != registration.InternalToolName || registration.Handler.Handle == nil {
			return nil, fmt.Errorf("MCP_APP_SURFACE_REGISTRATION_INVALID: %s", registration.AdvertisedName)
		}
		if _, duplicate := dispatch[registration.AdvertisedName]; duplicate {
			return nil, fmt.Errorf("MCP_APP_SURFACE_ALIAS_COLLISION: %s", registration.AdvertisedName)
		}
		descriptionPrefix := ""
		if registration.Aliased {
			descriptionPrefix = registration.SurfaceContract + " surface: "
		}
		dispatch[registration.AdvertisedName] = surfaceDispatch{
			surface: registry.SurfaceContractID(registration.SurfaceContract), toolName: registration.InternalToolName, routeBound: true, handle: registration.Handler.Handle,
		}
		definitions = append(definitions, routeToolDefinition(
			registration.AdvertisedName, descriptionPrefix+registration.Tool.Description, registration.Tool,
			routecontracts.RouteManifest{
				RoutePath: registration.InternalRoutePath, SurfaceContract: registration.SurfaceContract,
				ManifestSHA256: registration.RouteManifestSHA256, StandingAuthority: registration.StandingAuthority,
			},
			string(registration.PublicSurface), registration.InternalToolName,
		))
	}
	return &Server{log: log, deps: deps, tools: definitions, surfaceHandlers: dispatch}, nil
}

func routeToolDefinition(name, description string, tool routecontracts.ToolManifest, manifest routecontracts.RouteManifest, publicSurface, internalToolName string) ToolDefinition {
	return ToolDefinition{
		Name: name, Title: tool.Title, Description: description,
		InputSchema: append(json.RawMessage(nil), tool.InputSchema...), OutputSchema: append(json.RawMessage(nil), tool.OutputSchema...),
		Annotations:        map[string]any{"readOnlyHint": tool.Annotations.ReadOnlyHint, "destructiveHint": tool.Annotations.DestructiveHint, "idempotentHint": tool.Annotations.IdempotentHint, "openWorldHint": tool.Annotations.OpenWorldHint},
		Meta:               routeToolMetadata(manifest, tool, publicSurface, name, internalToolName),
		orderedAnnotations: orderedRouteAnnotations(tool), orderedMeta: orderedRouteMetadata(manifest, tool, publicSurface, name, internalToolName),
	}
}

func routeToolMetadata(manifest routecontracts.RouteManifest, tool routecontracts.ToolManifest, publicSurface, advertisedName, internalToolName string) map[string]any {
	metadata := map[string]any{
		"openai/widgetAccessible": false, "openai/toolInvocation/invoking": tool.Invoking, "openai/toolInvocation/invoked": tool.Invoked,
		"openai/fileParams": append([]string(nil), tool.FileParams...), "relay/routePath": manifest.RoutePath,
		"relay/surfaceContract": manifest.SurfaceContract, "relay/routeManifestSHA256": manifest.ManifestSHA256,
		"relay/standingAuthorityRepository": manifest.StandingAuthority.Repository,
		"relay/standingAuthorityCommitOID":  manifest.StandingAuthority.Commit,
		"relay/standingAuthorityPath":       manifest.StandingAuthority.Path,
		"relay/standingAuthorityBlobOID":    manifest.StandingAuthority.BlobOID,
		"relay/semanticToolID":              tool.SemanticToolID, "relay/operationID": tool.OperationID,
	}
	if publicSurface != "" {
		metadata["relay/publicAppSurface"] = publicSurface
		metadata["relay/publicAdvertisedToolName"] = advertisedName
		metadata["relay/internalToolName"] = internalToolName
	}
	return metadata
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
func orderedRouteMetadata(manifest routecontracts.RouteManifest, tool routecontracts.ToolManifest, publicSurface, advertisedName, internalToolName string) json.RawMessage {
	type metadata struct {
		Widget              bool     `json:"openai/widgetAccessible"`
		Invoking            string   `json:"openai/toolInvocation/invoking"`
		Invoked             string   `json:"openai/toolInvocation/invoked"`
		FileParams          []string `json:"openai/fileParams"`
		Route               string   `json:"relay/routePath"`
		Surface             string   `json:"relay/surfaceContract"`
		Digest              string   `json:"relay/routeManifestSHA256"`
		AuthorityRepository string   `json:"relay/standingAuthorityRepository"`
		AuthorityCommitOID  string   `json:"relay/standingAuthorityCommitOID"`
		AuthorityPath       string   `json:"relay/standingAuthorityPath"`
		AuthorityBlobOID    string   `json:"relay/standingAuthorityBlobOID"`
		Semantic            string   `json:"relay/semanticToolID"`
		Operation           string   `json:"relay/operationID"`
		PublicSurface       string   `json:"relay/publicAppSurface,omitempty"`
		AdvertisedName      string   `json:"relay/publicAdvertisedToolName,omitempty"`
		InternalTool        string   `json:"relay/internalToolName,omitempty"`
	}
	raw, _ := json.Marshal(metadata{
		false, tool.Invoking, tool.Invoked, append([]string(nil), tool.FileParams...), manifest.RoutePath, manifest.SurfaceContract, manifest.ManifestSHA256,
		manifest.StandingAuthority.Repository, manifest.StandingAuthority.Commit, manifest.StandingAuthority.Path, manifest.StandingAuthority.BlobOID,
		tool.SemanticToolID, tool.OperationID, publicSurface, advertisedName, internalToolName,
	})
	return raw
}

func appSurfaceTraceLabel(authority routecontracts.StandingAuthorityIdentity) string {
	return strings.TrimSpace(authority.Path)
}
