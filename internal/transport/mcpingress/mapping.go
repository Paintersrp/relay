package mcpingress

import "net/url"

type MappingID string

const (
	MappingWayfinder AppMappingID = "wayfinder"
	MappingPlanner   AppMappingID = "planner"
	MappingAuditor   AppMappingID = "auditor"
)

// AppMappingID names one independently supervised public app ingress.
type AppMappingID = MappingID

type ToolIdentity struct {
	AdvertisedName              string
	InternalToolName            string
	InternalRoutePath           string
	SurfaceContract             string
	RouteManifestSHA256         string
	StandingAuthorityRepository string
	StandingAuthorityCommitOID  string
	StandingAuthorityPath       string
	StandingAuthorityBlobOID    string
}

type RouteDescriptor struct {
	MappingID                   MappingID
	RoutePath                   string
	PublicSurface               string
	PublicSurfaceManifestSHA256 string
	ToolIdentities              []ToolIdentity
}

type PrivateAddress struct{ value string }

func (address PrivateAddress) String() string { return address.value }

type UpstreamTarget struct{ value url.URL }

func (target UpstreamTarget) URL() url.URL   { return target.value }
func (target UpstreamTarget) String() string { return target.value.String() }

type MappingSpec struct {
	ID                          MappingID
	RoutePath                   string
	PublicSurface               string
	PublicSurfaceManifestSHA256 string
	ToolIdentities              []ToolIdentity
	Listener                    PrivateAddress
	Upstream                    UpstreamTarget
}

type catalogEntry struct {
	ID             MappingID
	RoutePath      string
	ListenerEnv    string
	DefaultAddress string
}

var mappingCatalog = []catalogEntry{
	{MappingWayfinder, "/mcp/wayfinder", "RELAY_MCP_INGRESS_WAYFINDER_ADDR", "127.0.0.1:18101"},
	{MappingPlanner, "/mcp/planner", "RELAY_MCP_INGRESS_PLANNER_ADDR", "127.0.0.1:18102"},
	{MappingAuditor, "/mcp/auditor", "RELAY_MCP_INGRESS_AUDITOR_ADDR", "127.0.0.1:18103"},
}

func Catalog() []RouteDescriptor {
	result := make([]RouteDescriptor, len(mappingCatalog))
	for index, entry := range mappingCatalog {
		result[index] = RouteDescriptor{MappingID: entry.ID, RoutePath: entry.RoutePath}
	}
	return result
}
