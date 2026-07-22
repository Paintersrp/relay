package server

import (
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"

	"relay/internal/mcp"
	"relay/internal/mcp/routecontracts"
	"relay/internal/transport/mcpingress"
)

func TestMCPRouteDescriptorsAndIngressSummaryUseExactClosedCatalog(t *testing.T) {
	catalog := mcpingress.Catalog()
	handlers := make([]mcpHandler, len(catalog))
	for index, mapping := range catalog {
		handlers[index] = testMCPHandler(mapping, index)
	}
	routes, err := mcpRouteDescriptors(handlers)
	if err != nil {
		t.Fatal(err)
	}
	if len(routes) != 3 {
		t.Fatalf("routes=%d", len(routes))
	}
	for index, route := range routes {
		if route.MappingID != catalog[index].MappingID || route.RoutePath != catalog[index].RoutePath || route.PublicSurface != handlers[index].PublicSurface || route.PublicSurfaceManifestSHA256 != handlers[index].PublicSurfaceManifestSHA256 || len(route.ToolIdentities) != 1 || route.ToolIdentities[0].InternalRoutePath != "/mcp/v1/test" {
			t.Fatalf("route[%d]=%#v", index, route)
		}
	}

	t.Setenv("RELAY_MCP_TRACE_DIR", t.TempDir())
	t.Setenv("RELAY_MCP_INGRESS_UPSTREAM_BEARER_TOKEN", "protected-upstream-token")
	server := &Server{log: slog.New(slog.NewTextHandler(io.Discard, nil)), mcpRoutes: routes}
	summary, err := server.PrepareMCPIngress("http://127.0.0.1:8080")
	if err != nil {
		t.Fatal(err)
	}
	defer server.ShutdownMCPIngress(t.Context())
	if len(summary.Mappings) != 3 || !summary.UpstreamBearerConfigured {
		t.Fatalf("summary=%#v", summary)
	}
	if strings.Contains(fmt.Sprintf("%#v", summary), "protected-upstream-token") {
		t.Fatal("summary exposed upstream bearer")
	}
	for index, mapping := range summary.Mappings {
		if mapping.MappingID != catalog[index].MappingID || mapping.RoutePath != catalog[index].RoutePath {
			t.Fatalf("summary mapping[%d]=%#v", index, mapping)
		}
	}
}

func TestMCPRouteDescriptorsRejectMissingOrDuplicateRoute(t *testing.T) {
	catalog := mcpingress.Catalog()
	handlers := make([]mcpHandler, len(catalog))
	for index, mapping := range catalog {
		handlers[index] = testMCPHandler(mapping, index)
	}
	handlers[1].Path = handlers[0].Path
	if _, err := mcpRouteDescriptors(handlers); err == nil {
		t.Fatal("duplicate route was accepted")
	}
	handlers[1].Path = catalog[1].RoutePath
	handlers[1].ToolRegistrations = nil
	if _, err := mcpRouteDescriptors(handlers); err == nil {
		t.Fatal("missing tool identities were accepted")
	}
	handlers[1] = testMCPHandler(catalog[1], 1)
	handlers[1].PublicSurface = "unexpected"
	if _, err := mcpRouteDescriptors(handlers); err == nil {
		t.Fatal("mismatched public surface was accepted")
	}
}

func testMCPHandler(mapping mcpingress.RouteDescriptor, index int) mcpHandler {
	return mcpHandler{
		Path: mapping.RoutePath, PublicSurface: string(mapping.MappingID), PublicSurfaceManifestSHA256: strings.Repeat(fmt.Sprintf("%x", index+1), 64),
		ToolRegistrations: []mcp.AppToolRegistration{{
			AdvertisedName: "tool_" + string(mapping.MappingID), InternalToolName: "search_source", InternalRoutePath: "/mcp/v1/test",
			SurfaceContract: "test-surface.v1", RouteManifestSHA256: strings.Repeat("a", 64),
			StandingAuthority: routecontracts.StandingAuthorityIdentity{Repository: "Paintersrp/relay-specs", Commit: strings.Repeat("b", 40), Path: "agents/test.md", BlobOID: strings.Repeat("c", 40)},
		}},
	}
}
