package architecture_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMCPRuntimeAndLegacyCatalogRemainIsolated(t *testing.T) {
	root := repoRoot(t)
	for _, relative := range []string{"internal/mcp/routecontracts/catalog.go", "internal/mcp/route_server.go", "internal/mcp/route_runtime.go", "internal/mcp/route_dispatch.go", "internal/server/mcp_dependencies.go"} {
		data, err := os.ReadFile(filepath.Join(root, relative))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "internal/mcp/surfacecontracts") || strings.Contains(string(data), "NewServer(") {
			t.Fatalf("%s references aggregate authority", relative)
		}
	}
	legacy, err := os.ReadFile(filepath.Join(root, "internal/mcp/surfacecontracts/catalog.go"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(legacy), "internal/mcp/routecontracts") {
		t.Fatal("legacy catalog references MCP route contracts")
	}
}
