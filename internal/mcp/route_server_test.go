package mcp

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"relay/internal/mcp/routecontracts"
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

func TestAppSurfaceServersListUniqueAliasesAndDispatchToBoundRoutes(t *testing.T) {
	routes, err := routecontracts.BuildMCPRouteManifests()
	if err != nil {
		t.Fatal(err)
	}
	surfaces, err := routecontracts.BuildAppSurfaceManifests(routes)
	if err != nil {
		t.Fatal(err)
	}
	owners := fakeAppSurfaceDispatchers(routes)
	servers := make(map[routecontracts.AppSurface]*Server, len(surfaces.Surfaces))
	registrationsBySurface := make(map[routecontracts.AppSurface][]AppToolRegistration, len(surfaces.Surfaces))

	for _, surface := range surfaces.Surfaces {
		registrations, err := BuildAppSurfaceHandlers(surface, owners)
		if err != nil {
			t.Fatal(err)
		}
		server, err := NewServerForAppSurface(nil, nil, surface, registrations)
		if err != nil {
			t.Fatal(err)
		}
		servers[surface.Surface] = server
		registrationsBySurface[surface.Surface] = registrations

		list := collectAllTools(t, server, ToolsListParams{})
		if len(list.Tools) != len(surface.Tools) {
			t.Fatalf("%s catalog tools=%d, want %d", surface.Surface, len(list.Tools), len(surface.Tools))
		}
		seen := make(map[string]struct{}, len(list.Tools))
		for index, definition := range list.Tools {
			registration := registrations[index]
			if definition.Name != registration.AdvertisedName {
				t.Fatalf("%s tool[%d]=%q, want %q", surface.Surface, index, definition.Name, registration.AdvertisedName)
			}
			if _, duplicate := seen[definition.Name]; duplicate {
				t.Fatalf("%s duplicate public tool %q", surface.Surface, definition.Name)
			}
			seen[definition.Name] = struct{}{}
			assertAppToolMetadata(t, definition, registration)
		}

		for index, definition := range server.tools {
			assertAppToolMetadata(t, definition, registrations[index])
		}

		if surface.Surface == routecontracts.AppSurfaceWayfinder {
			invalid := append([]AppToolRegistration(nil), registrations...)
			invalid[0].StandingAuthority.Path += ".unexpected"
			if _, err := NewServerForAppSurface(nil, nil, surface, invalid); err == nil {
				t.Fatal("server accepted a standing-authority mismatch")
			}
		}
	}

	wayfinder := servers[routecontracts.AppSurfaceWayfinder]
	collisions := appToolRegistrationsByInternalName(registrationsBySurface[routecontracts.AppSurfaceWayfinder], "list_projects")
	if len(collisions) != 3 {
		t.Fatalf("wayfinder list_projects aliases=%d", len(collisions))
	}
	for _, registration := range collisions[:2] {
		response := callAppSurfaceTool(t, wayfinder, registration.AdvertisedName, registration.SurfaceContract)
		if response.Error != nil {
			t.Fatalf("%s response=%#v", registration.AdvertisedName, response.Error)
		}
		var result ToolCallResult
		body, err := json.Marshal(response.Result)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(body, &result); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(toolResultText(result), registration.InternalRoutePath) {
			t.Fatalf("%s did not reach bound route %s: %s", registration.AdvertisedName, registration.InternalRoutePath, toolResultText(result))
		}
	}
	for _, name := range []string{"list_projects", "planner-authoring-v1__list_projects"} {
		response := callAppSurfaceTool(t, wayfinder, name, "wayfinder-workspace.v1")
		if response.Error == nil || response.Error.Code != CodeMethodNotFound {
			t.Fatalf("unqualified or cross-role %q response=%#v", name, response)
		}
	}
}

func fakeAppSurfaceDispatchers(routes routecontracts.RouteSet) RouteDispatchers {
	owners := RouteDispatchers{Handlers: make(map[string]map[string]SurfaceHandler, len(routes.Manifests))}
	for _, route := range routes.Manifests {
		byTool := make(map[string]SurfaceHandler, len(route.Tools))
		for _, tool := range route.Tools {
			routePath, toolName := route.RoutePath, tool.Name
			byTool[toolName] = func(raw json.RawMessage) ToolCallResult {
				var args map[string]json.RawMessage
				if err := json.Unmarshal(raw, &args); err != nil {
					return toolErr(err.Error())
				}
				if _, supplied := args["surface_contract"]; supplied {
					return toolErr("route authority must not be caller supplied")
				}
				return workflowOK(map[string]string{"route_path": routePath, "tool": toolName})
			}
		}
		owners.Handlers[route.RoutePath] = byTool
	}
	return owners
}

func assertAppToolMetadata(t *testing.T, definition ToolDefinition, registration AppToolRegistration) {
	t.Helper()
	want := map[string]string{
		"relay/publicAppSurface":            string(registration.PublicSurface),
		"relay/publicAdvertisedToolName":    registration.AdvertisedName,
		"relay/internalToolName":            registration.InternalToolName,
		"relay/routePath":                   registration.InternalRoutePath,
		"relay/surfaceContract":             registration.SurfaceContract,
		"relay/routeManifestSHA256":         registration.RouteManifestSHA256,
		"relay/standingAuthorityRepository": registration.StandingAuthority.Repository,
		"relay/standingAuthorityCommitOID":  registration.StandingAuthority.Commit,
		"relay/standingAuthorityPath":       registration.StandingAuthority.Path,
		"relay/standingAuthorityBlobOID":    registration.StandingAuthority.BlobOID,
	}
	for key, expected := range want {
		if actual, ok := definition.Meta[key].(string); !ok || actual != expected {
			t.Fatalf("%s metadata %s=%#v, want %q", definition.Name, key, definition.Meta[key], expected)
		}
	}
	if len(definition.orderedMeta) == 0 {
		return
	}
	var ordered map[string]any
	if err := json.Unmarshal(definition.orderedMeta, &ordered); err != nil {
		t.Fatal(err)
	}
	for key, expected := range want {
		if actual, ok := ordered[key].(string); !ok || actual != expected {
			t.Fatalf("%s ordered metadata %s=%#v, want %q", definition.Name, key, ordered[key], expected)
		}
	}
}

func appToolRegistrationsByInternalName(registrations []AppToolRegistration, internalName string) []AppToolRegistration {
	result := make([]AppToolRegistration, 0)
	for _, registration := range registrations {
		if registration.InternalToolName == internalName {
			result = append(result, registration)
		}
	}
	return result
}

func callAppSurfaceTool(t *testing.T, server *Server, name, surfaceContract string) Response {
	t.Helper()
	arguments, err := json.Marshal(map[string]string{"surface_contract": surfaceContract})
	if err != nil {
		t.Fatal(err)
	}
	params, err := json.Marshal(ToolCallParams{Name: name, Arguments: arguments})
	if err != nil {
		t.Fatal(err)
	}
	return server.handleToolsCall(Request{ID: json.RawMessage(fmt.Sprintf("%q", name)), Params: params})
}
