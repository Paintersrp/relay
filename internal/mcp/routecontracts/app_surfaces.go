package routecontracts

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// AppSurface is the role-bounded MCP app presented to ChatGPT. It is a
// transport/catalog facade over one or more immutable internal route manifests.
type AppSurface string

const (
	AppSurfaceWayfinder AppSurface = "wayfinder"
	AppSurfacePlanner   AppSurface = "planner"
	AppSurfaceAuditor   AppSurface = "auditor"
)

type AppToolManifest struct {
	AdvertisedName      string                    `json:"advertised_name"`
	InternalToolName    string                    `json:"internal_tool_name"`
	InternalRoutePath   string                    `json:"internal_route_path"`
	SurfaceContract     string                    `json:"surface_contract"`
	RouteManifestSHA256 string                    `json:"route_manifest_sha256"`
	StandingAuthority   StandingAuthorityIdentity `json:"standing_authority"`
	Tool                ToolManifest              `json:"-"`
	Aliased             bool                      `json:"-"`
}

type AppSurfaceManifest struct {
	Surface        AppSurface        `json:"surface"`
	PublicPath     string            `json:"public_path"`
	MemberRoutes   []RouteManifest   `json:"member_routes"`
	Tools          []AppToolManifest `json:"tools"`
	ManifestSHA256 string            `json:"manifest_sha256"`
}

type AppSurfaceSet struct {
	Surfaces []AppSurfaceManifest `json:"surfaces"`
}

type appSurfaceDefinition struct {
	surface    AppSurface
	role       string
	publicPath string
	routeCount int
}

var compiledAppSurfaceDefinitions = []appSurfaceDefinition{
	{surface: AppSurfaceWayfinder, role: "wayfinder", publicPath: "/mcp/wayfinder", routeCount: 3},
	{surface: AppSurfacePlanner, role: "planner", publicPath: "/mcp/planner", routeCount: 2},
	{surface: AppSurfaceAuditor, role: "auditor", publicPath: "/mcp/auditor", routeCount: 2},
}

// BuildAppSurfaceManifests compiles the fixed role-to-route assignment without
// deriving public membership from route-path text or request input.
func BuildAppSurfaceManifests(routeSet RouteSet) (AppSurfaceSet, error) {
	if len(routeSet.Manifests) != 7 {
		return AppSurfaceSet{}, fmt.Errorf("MCP_APP_SURFACE_SET_INCOMPLETE: routes=%d", len(routeSet.Manifests))
	}
	byRole := make(map[string][]RouteManifest, len(compiledAppSurfaceDefinitions))
	seenRoutes := make(map[string]struct{}, len(routeSet.Manifests))
	for _, manifest := range routeSet.Manifests {
		if manifest.RoutePath == "" || manifest.Role == "" || manifest.SurfaceContract == "" || !validAppManifestSHA256(manifest.ManifestSHA256) {
			return AppSurfaceSet{}, fmt.Errorf("MCP_APP_SURFACE_SET_INVALID: route identity")
		}
		if _, duplicate := seenRoutes[manifest.RoutePath]; duplicate {
			return AppSurfaceSet{}, fmt.Errorf("MCP_APP_SURFACE_SET_INVALID: duplicate route %s", manifest.RoutePath)
		}
		seenRoutes[manifest.RoutePath] = struct{}{}
		byRole[manifest.Role] = append(byRole[manifest.Role], manifest)
	}

	result := AppSurfaceSet{Surfaces: make([]AppSurfaceManifest, 0, len(compiledAppSurfaceDefinitions))}
	assigned := make(map[string]AppSurface, len(routeSet.Manifests))
	for _, definition := range compiledAppSurfaceDefinitions {
		members := append([]RouteManifest(nil), byRole[definition.role]...)
		if len(members) != definition.routeCount {
			return AppSurfaceSet{}, fmt.Errorf("MCP_APP_SURFACE_SET_INCOMPLETE: %s routes=%d", definition.surface, len(members))
		}
		sort.Slice(members, func(i, j int) bool { return members[i].RoutePath < members[j].RoutePath })
		for _, member := range members {
			if member.Role != definition.role {
				return AppSurfaceSet{}, fmt.Errorf("MCP_APP_SURFACE_ROLE_MISMATCH: %s", member.RoutePath)
			}
			if existing, duplicate := assigned[member.RoutePath]; duplicate {
				return AppSurfaceSet{}, fmt.Errorf("MCP_APP_SURFACE_MEMBERSHIP_DUPLICATE: %s/%s", existing, member.RoutePath)
			}
			assigned[member.RoutePath] = definition.surface
		}
		tools, err := compileAppTools(members)
		if err != nil {
			return AppSurfaceSet{}, err
		}
		manifest := AppSurfaceManifest{Surface: definition.surface, PublicPath: definition.publicPath, MemberRoutes: members, Tools: tools}
		basis, err := appSurfaceManifestBasis(manifest)
		if err != nil {
			return AppSurfaceSet{}, err
		}
		sum := sha256.Sum256(basis)
		manifest.ManifestSHA256 = hex.EncodeToString(sum[:])
		result.Surfaces = append(result.Surfaces, manifest)
	}
	if len(assigned) != 7 || len(result.Surfaces) != 3 {
		return AppSurfaceSet{}, fmt.Errorf("MCP_APP_SURFACE_SET_INCOMPLETE")
	}
	return cloneAppSurfaceSet(result), nil
}

func BuildMCPAppSurfaceManifests() (AppSurfaceSet, error) {
	routes, err := BuildMCPRouteManifests()
	if err != nil {
		return AppSurfaceSet{}, err
	}
	return BuildAppSurfaceManifests(routes)
}

func compileAppTools(members []RouteManifest) ([]AppToolManifest, error) {
	byName := make(map[string]int)
	for _, member := range members {
		for _, tool := range member.Tools {
			byName[tool.Name]++
		}
	}
	tools := make([]AppToolManifest, 0)
	seenAdvertised := make(map[string]struct{})
	for _, member := range members {
		for _, tool := range member.Tools {
			advertised := tool.Name
			aliased := byName[tool.Name] > 1
			if aliased {
				key, err := appRouteKey(member)
				if err != nil {
					return nil, err
				}
				advertised = key + "__" + tool.Name
			}
			if !validMCPToolName(advertised) {
				return nil, fmt.Errorf("MCP_APP_TOOL_ALIAS_INVALID: %s", advertised)
			}
			if _, duplicate := seenAdvertised[advertised]; duplicate {
				return nil, fmt.Errorf("MCP_APP_TOOL_ALIAS_COLLISION: %s", advertised)
			}
			seenAdvertised[advertised] = struct{}{}
			tools = append(tools, AppToolManifest{
				AdvertisedName: advertised, InternalToolName: tool.Name,
				InternalRoutePath: member.RoutePath, SurfaceContract: member.SurfaceContract,
				RouteManifestSHA256: member.ManifestSHA256, StandingAuthority: member.StandingAuthority,
				Tool: tool, Aliased: aliased,
			})
		}
	}
	sort.Slice(tools, func(i, j int) bool { return tools[i].AdvertisedName < tools[j].AdvertisedName })
	return tools, nil
}

func appRouteKey(manifest RouteManifest) (string, error) {
	key := strings.ReplaceAll(manifest.SurfaceContract, ".", "-")
	if !validMCPToolName(key) {
		return "", fmt.Errorf("MCP_APP_ROUTE_KEY_INVALID: %s", manifest.SurfaceContract)
	}
	return key, nil
}

func validAppManifestSHA256(value string) bool {
	if len(value) != 64 || strings.ToLower(value) != value {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func validMCPToolName(value string) bool {
	if len(value) == 0 || len(value) > 128 {
		return false
	}
	for _, character := range value {
		if !(character >= 'a' && character <= 'z') && !(character >= 'A' && character <= 'Z') && !(character >= '0' && character <= '9') && character != '_' && character != '-' {
			return false
		}
	}
	return true
}

func appSurfaceManifestBasis(value AppSurfaceManifest) ([]byte, error) {
	type memberIdentity struct {
		RoutePath       string `json:"route_path"`
		Role            string `json:"role"`
		SurfaceContract string `json:"surface_contract"`
		ManifestSHA256  string `json:"manifest_sha256"`
	}
	type toolIdentity struct {
		AdvertisedName      string                    `json:"advertised_name"`
		InternalToolName    string                    `json:"internal_tool_name"`
		InternalRoutePath   string                    `json:"internal_route_path"`
		SurfaceContract     string                    `json:"surface_contract"`
		RouteManifestSHA256 string                    `json:"route_manifest_sha256"`
		StandingAuthority   StandingAuthorityIdentity `json:"standing_authority"`
	}
	type basis struct {
		SchemaVersion string           `json:"schema_version"`
		Surface       AppSurface       `json:"surface"`
		PublicPath    string           `json:"public_path"`
		Members       []memberIdentity `json:"members"`
		Tools         []toolIdentity   `json:"tools"`
	}
	members := make([]memberIdentity, 0, len(value.MemberRoutes))
	for _, route := range value.MemberRoutes {
		members = append(members, memberIdentity{route.RoutePath, route.Role, route.SurfaceContract, route.ManifestSHA256})
	}
	tools := make([]toolIdentity, 0, len(value.Tools))
	for _, tool := range value.Tools {
		tools = append(tools, toolIdentity{tool.AdvertisedName, tool.InternalToolName, tool.InternalRoutePath, tool.SurfaceContract, tool.RouteManifestSHA256, tool.StandingAuthority})
	}
	return json.Marshal(basis{"relay.mcp.app-surface-manifest.v1", value.Surface, value.PublicPath, members, tools})
}

func cloneAppSurfaceSet(value AppSurfaceSet) AppSurfaceSet {
	result := AppSurfaceSet{Surfaces: make([]AppSurfaceManifest, len(value.Surfaces))}
	for index, surface := range value.Surfaces {
		result.Surfaces[index] = surface
		result.Surfaces[index].MemberRoutes = cloneRouteSet(RouteSet{Manifests: surface.MemberRoutes}).Manifests
		result.Surfaces[index].Tools = append([]AppToolManifest(nil), surface.Tools...)
	}
	return result
}
