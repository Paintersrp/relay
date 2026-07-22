package routecontracts

import (
	"strings"
	"testing"
)

func TestAppSurfaceManifestsCompileExactRoleMembershipAndStableAliases(t *testing.T) {
	routes, err := BuildMCPRouteManifests()
	if err != nil {
		t.Fatal(err)
	}
	first, err := BuildAppSurfaceManifests(routes)
	if err != nil {
		t.Fatal(err)
	}
	reversed := cloneRouteSet(routes)
	for left, right := 0, len(reversed.Manifests)-1; left < right; left, right = left+1, right-1 {
		reversed.Manifests[left], reversed.Manifests[right] = reversed.Manifests[right], reversed.Manifests[left]
	}
	second, err := BuildAppSurfaceManifests(reversed)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Surfaces) != 3 || len(second.Surfaces) != 3 {
		t.Fatalf("surfaces=%d/%d", len(first.Surfaces), len(second.Surfaces))
	}
	wantCounts := map[AppSurface]int{AppSurfaceWayfinder: 3, AppSurfacePlanner: 2, AppSurfaceAuditor: 2}
	seenRoutes := map[string]struct{}{}
	for index, surface := range first.Surfaces {
		if len(surface.MemberRoutes) != wantCounts[surface.Surface] || surface.ManifestSHA256 != second.Surfaces[index].ManifestSHA256 || surface.PublicPath != second.Surfaces[index].PublicPath {
			t.Fatalf("surface[%d]=%#v", index, surface)
		}
		seenNames := map[string]struct{}{}
		for toolIndex, tool := range surface.Tools {
			if _, duplicate := seenNames[tool.AdvertisedName]; duplicate {
				t.Fatalf("duplicate advertised tool %s", tool.AdvertisedName)
			}
			seenNames[tool.AdvertisedName] = struct{}{}
			if toolIndex > 0 && surface.Tools[toolIndex-1].AdvertisedName > tool.AdvertisedName {
				t.Fatalf("tool order is not deterministic: %q > %q", surface.Tools[toolIndex-1].AdvertisedName, tool.AdvertisedName)
			}
			if other := findAppTool(second.Surfaces[index], tool.AdvertisedName); other == nil || other.InternalRoutePath != tool.InternalRoutePath || other.InternalToolName != tool.InternalToolName || other.StandingAuthority != tool.StandingAuthority {
				t.Fatalf("unstable registration for %s", tool.AdvertisedName)
			}
		}
		for _, route := range surface.MemberRoutes {
			if _, duplicate := seenRoutes[route.RoutePath]; duplicate {
				t.Fatalf("route %s belongs to more than one app surface", route.RoutePath)
			}
			seenRoutes[route.RoutePath] = struct{}{}
			if route.Role != string(surface.Surface) {
				t.Fatalf("cross-role member %s in %s", route.RoutePath, surface.Surface)
			}
		}
	}
	if len(seenRoutes) != 7 {
		t.Fatalf("assigned routes=%d", len(seenRoutes))
	}
}

func TestAppSurfaceManifestsRetainExactToolInventoryAndCollisionIdentities(t *testing.T) {
	routes, err := BuildMCPRouteManifests()
	if err != nil {
		t.Fatal(err)
	}
	surfaces, err := BuildAppSurfaceManifests(routes)
	if err != nil {
		t.Fatal(err)
	}

	expectedRoutes := make(map[string]RouteManifest, len(routes.Manifests))
	for _, route := range routes.Manifests {
		expectedRoutes[route.RoutePath] = route
	}
	seenRoutes := make(map[string]struct{}, len(routes.Manifests))
	seenRegistrations := make(map[string]struct{})
	for _, surface := range surfaces.Surfaces {
		internalCounts := map[string]int{}
		expectedRegistrationCount := 0
		for _, route := range surface.MemberRoutes {
			original, ok := expectedRoutes[route.RoutePath]
			if !ok || original.RoutePath != route.RoutePath || original.Role != route.Role || original.SurfaceContract != route.SurfaceContract || original.ManifestSHA256 != route.ManifestSHA256 || original.StandingAuthority != route.StandingAuthority {
				t.Fatalf("unexpected app member %#v", route)
			}
			if _, duplicate := seenRoutes[route.RoutePath]; duplicate {
				t.Fatalf("route belongs to multiple surfaces: %s", route.RoutePath)
			}
			seenRoutes[route.RoutePath] = struct{}{}
			for _, tool := range route.Tools {
				internalCounts[tool.Name]++
				expectedRegistrationCount++
			}
		}
		if len(surface.Tools) != expectedRegistrationCount {
			t.Fatalf("surface %s tools=%d, want %d", surface.Surface, len(surface.Tools), expectedRegistrationCount)
		}
		for _, registration := range surface.Tools {
			route, ok := expectedRoutes[registration.InternalRoutePath]
			if !ok || route.SurfaceContract != registration.SurfaceContract || route.ManifestSHA256 != registration.RouteManifestSHA256 || route.StandingAuthority != registration.StandingAuthority {
				t.Fatalf("registration lost immutable route identity: %#v", registration)
			}
			if !routeContainsTool(route, registration.InternalToolName) {
				t.Fatalf("registration %s does not belong to %s", registration.InternalToolName, route.RoutePath)
			}
			key := registration.InternalRoutePath + "\x00" + registration.InternalToolName
			if _, duplicate := seenRegistrations[key]; duplicate {
				t.Fatalf("duplicate route registration %q", key)
			}
			seenRegistrations[key] = struct{}{}
			if internalCounts[registration.InternalToolName] == 1 {
				if registration.Aliased || registration.AdvertisedName != registration.InternalToolName {
					t.Fatalf("unique tool registration changed name: %#v", registration)
				}
			} else {
				want := strings.ReplaceAll(registration.SurfaceContract, ".", "-") + "__" + registration.InternalToolName
				if !registration.Aliased || registration.AdvertisedName != want {
					t.Fatalf("collision registration=%#v, want alias %q", registration, want)
				}
			}
		}
	}
	if len(seenRoutes) != len(routes.Manifests) || len(seenRegistrations) == 0 {
		t.Fatalf("routes=%d registrations=%d", len(seenRoutes), len(seenRegistrations))
	}
	for _, internalTool := range []string{"list_projects", "create_operation_packet", "search_source"} {
		count := 0
		for _, surface := range surfaces.Surfaces {
			for _, registration := range surface.Tools {
				if registration.InternalToolName != internalTool {
					continue
				}
				count++
				want := strings.ReplaceAll(registration.SurfaceContract, ".", "-") + "__" + internalTool
				if !registration.Aliased || registration.AdvertisedName != want {
					t.Fatalf("%s collision registration=%#v", internalTool, registration)
				}
			}
		}
		if count != len(routes.Manifests) {
			t.Fatalf("%s collision registrations=%d, want %d", internalTool, count, len(routes.Manifests))
		}
	}
}

func TestAppSurfaceManifestsRejectIncompleteDuplicateAndCrossRoleMembership(t *testing.T) {
	routes, err := BuildMCPRouteManifests()
	if err != nil {
		t.Fatal(err)
	}
	incomplete := cloneRouteSet(routes)
	incomplete.Manifests = incomplete.Manifests[:6]
	if _, err := BuildAppSurfaceManifests(incomplete); err == nil {
		t.Fatal("incomplete route set accepted")
	}
	duplicate := cloneRouteSet(routes)
	duplicate.Manifests[6] = duplicate.Manifests[5]
	if _, err := BuildAppSurfaceManifests(duplicate); err == nil {
		t.Fatal("duplicate route accepted")
	}
	crossRole := cloneRouteSet(routes)
	crossRole.Manifests[0].Role = "planner"
	if _, err := BuildAppSurfaceManifests(crossRole); err == nil {
		t.Fatal("cross-role membership accepted")
	}
}

func TestCompileAppToolsRejectsPostAliasCollision(t *testing.T) {
	routes, err := BuildMCPRouteManifests()
	if err != nil {
		t.Fatal(err)
	}
	members := []RouteManifest{routes.Manifests[0], routes.Manifests[1]}
	members[1].SurfaceContract = members[0].SurfaceContract
	if _, err := compileAppTools(members); err == nil {
		t.Fatal("post-alias collision accepted")
	}
}

func findAppTool(surface AppSurfaceManifest, advertised string) *AppToolManifest {
	for index := range surface.Tools {
		if surface.Tools[index].AdvertisedName == advertised {
			return &surface.Tools[index]
		}
	}
	return nil
}

func routeContainsTool(route RouteManifest, name string) bool {
	for _, tool := range route.Tools {
		if tool.Name == name {
			return true
		}
	}
	return false
}
