package server

import (
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"

	"relay/internal/transport/mcpingress"
)

func TestMCPRouteDescriptorsAndIngressSummaryUseExactClosedCatalog(t *testing.T) {
	catalog := mcpingress.Catalog()
	handlers := make([]mcpHandler, len(catalog))
	for index, mapping := range catalog {
		handlers[index] = mcpHandler{
			Path:                mapping.RoutePath,
			SurfaceContract:     fmt.Sprintf("surface-%d.v1", index+1),
			RouteManifestSHA256: strings.Repeat(fmt.Sprintf("%x", index+1), 64),
		}
	}
	routes, err := mcpRouteDescriptors(handlers)
	if err != nil {
		t.Fatal(err)
	}
	if len(routes) != 7 {
		t.Fatalf("routes=%d", len(routes))
	}
	for index, route := range routes {
		if route.MappingID != catalog[index].MappingID || route.RoutePath != catalog[index].RoutePath || route.SurfaceContract != handlers[index].SurfaceContract || route.RouteManifestSHA256 != handlers[index].RouteManifestSHA256 {
			t.Fatalf("route[%d]=%#v", index, route)
		}
	}

	t.Setenv("RELAY_MCP_TRACE_DIR", t.TempDir())
	t.Setenv("RELAY_MCP_INGRESS_UPSTREAM_BEARER_TOKEN", "protected-upstream-token")
	server := &Server{
		log:       slog.New(slog.NewTextHandler(io.Discard, nil)),
		mcpRoutes: routes,
	}
	summary, err := server.PrepareMCPIngress("http://127.0.0.1:8080")
	if err != nil {
		t.Fatal(err)
	}
	defer server.ShutdownMCPIngress(t.Context())
	if len(summary.Mappings) != 7 || !summary.UpstreamBearerConfigured {
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
		handlers[index] = mcpHandler{Path: mapping.RoutePath, SurfaceContract: "surface.v1", RouteManifestSHA256: strings.Repeat("a", 64)}
	}
	handlers[1].Path = handlers[0].Path
	if _, err := mcpRouteDescriptors(handlers); err == nil {
		t.Fatal("duplicate route was accepted")
	}
	handlers[1].Path = catalog[1].RoutePath
	handlers[1].RouteManifestSHA256 = ""
	if _, err := mcpRouteDescriptors(handlers); err == nil {
		t.Fatal("missing manifest identity was accepted")
	}
}
