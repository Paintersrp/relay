package cutover

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"

	"relay/internal/mcp/routecontracts"
)

const appSurfaceTopologyVersion = "relay.mcp.role-app-surfaces.v1"

func normalizeGatewayConfiguration(input GatewayConfigurationIdentity) (GatewayConfigurationIdentity, error) {
	if len(input.AppSurfaces) == 0 && len(input.RouteMemberships) == 0 && len(input.AppSurfaceMappings) == 0 {
		return normalizeLegacyGatewayConfiguration(input)
	}
	return normalizeAppSurfaceGatewayConfiguration(input)
}

func normalizeAppSurfaceGatewayConfiguration(input GatewayConfigurationIdentity) (GatewayConfigurationIdentity, error) {
	input.RelayRepository = strings.TrimSpace(input.RelayRepository)
	input.StandingRepository = strings.TrimSpace(input.StandingRepository)
	input.TopologyVersion = strings.TrimSpace(input.TopologyVersion)
	if input.RelayRepository == "" || input.StandingRepository == "" || input.TopologyVersion != appSurfaceTopologyVersion ||
		!validLowerHex(input.RelayCommitOID, 40) || !validLowerHex(input.StandingCommitOID, 40) ||
		len(input.Routes) != 7 || len(input.Mappings) != 0 || len(input.AppSurfaces) != 3 || len(input.RouteMemberships) != 7 || len(input.AppSurfaceMappings) != 3 ||
		len(input.StandingAuthorities) != 3 || len(input.DependencyOutcomes) != 3 {
		return GatewayConfigurationIdentity{}, ErrCutoverConfigurationInvalid
	}
	routes, err := routecontracts.BuildMCPRouteManifests()
	if err != nil {
		return GatewayConfigurationIdentity{}, ErrCutoverConfigurationInvalid
	}
	surfaces, err := routecontracts.BuildAppSurfaceManifests(routes)
	if err != nil {
		return GatewayConfigurationIdentity{}, ErrCutoverConfigurationInvalid
	}
	sort.Slice(input.Routes, func(i, j int) bool { return input.Routes[i].Sequence < input.Routes[j].Sequence })
	sort.Slice(input.AppSurfaces, func(i, j int) bool { return input.AppSurfaces[i].Sequence < input.AppSurfaces[j].Sequence })
	sort.Slice(input.RouteMemberships, func(i, j int) bool { return input.RouteMemberships[i].RoutePath < input.RouteMemberships[j].RoutePath })
	sort.Slice(input.AppSurfaceMappings, func(i, j int) bool {
		return input.AppSurfaceMappings[i].Sequence < input.AppSurfaceMappings[j].Sequence
	})
	sort.Slice(input.StandingAuthorities, func(i, j int) bool { return input.StandingAuthorities[i].Role < input.StandingAuthorities[j].Role })
	sort.Slice(input.DependencyOutcomes, func(i, j int) bool {
		return input.DependencyOutcomes[i].Sequence < input.DependencyOutcomes[j].Sequence
	})

	expectedRoutes := make(map[string]routecontracts.RouteManifest, len(routes.Manifests))
	for _, route := range routes.Manifests {
		expectedRoutes[route.RoutePath] = route
	}
	seenRoutes := make(map[string]struct{}, len(input.Routes))
	for index, route := range input.Routes {
		expected, ok := expectedRoutes[route.RoutePath]
		if !ok || route.Sequence != int64(index+1) || route.Role != expected.Role || route.SurfaceContractID != expected.SurfaceContract ||
			route.ManifestSHA256 != expected.ManifestSHA256 || route.AuthorityCommitOID != expected.StandingAuthority.Commit || route.AuthorityBlobOID != expected.StandingAuthority.BlobOID {
			return GatewayConfigurationIdentity{}, ErrCutoverConfigurationInvalid
		}
		if _, duplicate := seenRoutes[route.RoutePath]; duplicate {
			return GatewayConfigurationIdentity{}, ErrCutoverConfigurationInvalid
		}
		seenRoutes[route.RoutePath] = struct{}{}
	}

	expectedSurfaceByName := make(map[string]routecontracts.AppSurfaceManifest, len(surfaces.Surfaces))
	expectedMembership := make(map[string]string, len(routes.Manifests))
	for _, surface := range surfaces.Surfaces {
		expectedSurfaceByName[string(surface.Surface)] = surface
		for _, route := range surface.MemberRoutes {
			expectedMembership[route.RoutePath] = string(surface.Surface)
		}
	}
	seenSurfaces := make(map[string]struct{}, len(input.AppSurfaces))
	for index, surface := range input.AppSurfaces {
		expected, ok := expectedSurfaceByName[surface.Surface]
		if !ok || surface.Sequence != int64(index+1) || surface.PublicPath != expected.PublicPath || surface.ManifestSHA256 != expected.ManifestSHA256 {
			return GatewayConfigurationIdentity{}, ErrCutoverConfigurationInvalid
		}
		if _, duplicate := seenSurfaces[surface.Surface]; duplicate {
			return GatewayConfigurationIdentity{}, ErrCutoverConfigurationInvalid
		}
		seenSurfaces[surface.Surface] = struct{}{}
	}
	seenMemberships := make(map[string]struct{}, len(input.RouteMemberships))
	for _, membership := range input.RouteMemberships {
		if expectedMembership[membership.RoutePath] != membership.PublicSurface {
			return GatewayConfigurationIdentity{}, ErrCutoverConfigurationInvalid
		}
		if _, duplicate := seenMemberships[membership.RoutePath]; duplicate {
			return GatewayConfigurationIdentity{}, ErrCutoverConfigurationInvalid
		}
		seenMemberships[membership.RoutePath] = struct{}{}
	}
	if len(seenMemberships) != 7 {
		return GatewayConfigurationIdentity{}, ErrCutoverConfigurationInvalid
	}
	seenMappings := make(map[string]struct{}, len(input.AppSurfaceMappings))
	for index, mapping := range input.AppSurfaceMappings {
		expected, ok := expectedSurfaceByName[mapping.PublicSurface]
		if !ok || mapping.Sequence != int64(index+1) || mapping.MappingID != mapping.PublicSurface || mapping.PublicPath != expected.PublicPath ||
			strings.TrimSpace(mapping.ListenerIdentity) == "" || strings.TrimSpace(mapping.UpstreamIdentity) == "" ||
			!validLowerHex(mapping.HealthEvidenceSHA256, 64) || !validLowerHex(mapping.TraceEvidenceSHA256, 64) {
			return GatewayConfigurationIdentity{}, ErrCutoverConfigurationInvalid
		}
		if _, duplicate := seenMappings[mapping.MappingID]; duplicate {
			return GatewayConfigurationIdentity{}, ErrCutoverConfigurationInvalid
		}
		seenMappings[mapping.MappingID] = struct{}{}
	}
	if err := validateAppSurfaceStandingAuthorities(input, expectedRoutes); err != nil {
		return GatewayConfigurationIdentity{}, err
	}
	if err := validateAppSurfaceDependencies(input); err != nil {
		return GatewayConfigurationIdentity{}, err
	}
	input.ConfigurationSHA256 = ""
	raw, err := json.Marshal(input)
	if err != nil {
		return GatewayConfigurationIdentity{}, err
	}
	sum := sha256.Sum256(raw)
	input.ConfigurationSHA256 = hex.EncodeToString(sum[:])
	return input, nil
}

func validateAppSurfaceStandingAuthorities(input GatewayConfigurationIdentity, expectedRoutes map[string]routecontracts.RouteManifest) error {
	expected := make(map[string]routecontracts.StandingAuthorityIdentity, 3)
	for _, route := range expectedRoutes {
		expected[route.Role] = route.StandingAuthority
	}
	seen := map[string]struct{}{}
	for _, authority := range input.StandingAuthorities {
		standing, ok := expected[authority.Role]
		if !ok || authority.Repository != input.StandingRepository || authority.CommitOID != input.StandingCommitOID || authority.Path != standing.Path || authority.BlobOID != standing.BlobOID || !validLowerHex(authority.ContentSHA256, 64) {
			return ErrCutoverConfigurationInvalid
		}
		if _, duplicate := seen[authority.Role]; duplicate {
			return ErrCutoverConfigurationInvalid
		}
		seen[authority.Role] = struct{}{}
	}
	if len(seen) != 3 {
		return ErrCutoverConfigurationInvalid
	}
	return nil
}

func validateAppSurfaceDependencies(input GatewayConfigurationIdentity) error {
	expected := map[string]int64{"CURRENT-MCP-SURFACES": 2, "PRIVATE-TRANSPORT-TRACE": 2, "STANDING-AUTHORITY": 2}
	seen := map[string]struct{}{}
	for index, dependency := range input.DependencyOutcomes {
		revision, ok := expected[dependency.TicketID]
		if !ok || dependency.Sequence != int64(index+1) || dependency.TicketRevision != revision || dependency.Outcome != "completed_accepted" || !validLowerHex(dependency.EvidenceSHA256, 64) {
			return ErrCutoverConfigurationInvalid
		}
		if _, duplicate := seen[dependency.TicketID]; duplicate {
			return ErrCutoverConfigurationInvalid
		}
		seen[dependency.TicketID] = struct{}{}
	}
	if len(seen) != 3 {
		return ErrCutoverConfigurationInvalid
	}
	return nil
}
