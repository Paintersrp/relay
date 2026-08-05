package routecontracts

import (
	"strings"
	"testing"

	"relay/internal/operations/registry"
)

func TestSharedPacketRouteContractsAreBoundToTheirMountedRoute(t *testing.T) {
	set, err := BuildMCPRouteManifests()
	if err != nil {
		t.Fatal(err)
	}
	for _, manifest := range set.Manifests {
		for _, tool := range manifest.Tools {
			if !isSharedPacketToolForTest(tool.Name) {
				continue
			}
			if contract, ok := registry.LookupPublishedToolContract(tool.Name); !ok || contract.MetadataSource == "legacy_exact_copy" {
				t.Fatalf("%s still uses legacy packet contract metadata", tool.Name)
			}
			if !strings.Contains(string(tool.InputSchema), `"surface_contract":{"const":"`+manifest.SurfaceContract+`","type"`) {
				t.Fatalf("%s/%s input schema is not route-bound", manifest.RoutePath, tool.Name)
			}
		}
	}
}

func isSharedPacketToolForTest(name string) bool {
	switch name {
	case "list_projects", "get_active_operation_packet", "create_operation_packet", "refresh_operation_packet", "close_operation_packet", "read_operation_input", "list_operation_repositories":
		return true
	default:
		return false
	}
}
