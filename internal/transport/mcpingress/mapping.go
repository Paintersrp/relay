package mcpingress

import (
	"net/url"
)

type MappingID string

const (
	MappingWayfinderWorkspace     MappingID = "wayfinder-workspace"
	MappingWayfinderDiscovery     MappingID = "wayfinder-discovery"
	MappingWayfinderInvestigation MappingID = "wayfinder-investigation"
	MappingAuthoring              MappingID = "planner-authoring"
	MappingTicketFrontier         MappingID = "planner-frontier"
	MappingArtifactReview         MappingID = "auditor-review"
	MappingRunAudit               MappingID = "auditor-audit"
)

type RouteDescriptor struct {
	MappingID           MappingID
	RoutePath           string
	SurfaceContract     string
	RouteManifestSHA256 string
}

type PrivateAddress struct{ value string }

func (address PrivateAddress) String() string { return address.value }

type UpstreamTarget struct{ value url.URL }

func (target UpstreamTarget) URL() url.URL   { return target.value }
func (target UpstreamTarget) String() string { return target.value.String() }

type MappingSpec struct {
	ID                  MappingID
	RoutePath           string
	SurfaceContract     string
	RouteManifestSHA256 string
	Listener            PrivateAddress
	Upstream            UpstreamTarget
}

type catalogEntry struct {
	ID             MappingID
	RoutePath      string
	ListenerEnv    string
	DefaultAddress string
}

var mappingCatalog = []catalogEntry{
	{MappingWayfinderWorkspace, "/mcp/v1/wayfinder/workspace", "RELAY_MCP_INGRESS_WAYFINDER_WORKSPACE_ADDR", "127.0.0.1:18101"},
	{MappingWayfinderDiscovery, "/mcp/v1/wayfinder/discovery", "RELAY_MCP_INGRESS_WAYFINDER_DISCOVERY_ADDR", "127.0.0.1:18102"},
	{MappingWayfinderInvestigation, "/mcp/v1/wayfinder/investigation", "RELAY_MCP_INGRESS_WAYFINDER_INVESTIGATION_ADDR", "127.0.0.1:18103"},
	{MappingAuthoring, "/mcp/v1/planner/authoring", "RELAY_MCP_INGRESS_PLANNER_AUTHORING_ADDR", "127.0.0.1:18104"},
	{MappingTicketFrontier, "/mcp/v1/planner/frontier", "RELAY_MCP_INGRESS_PLANNER_FRONTIER_ADDR", "127.0.0.1:18105"},
	{MappingArtifactReview, "/mcp/v1/auditor/review", "RELAY_MCP_INGRESS_AUDITOR_REVIEW_ADDR", "127.0.0.1:18106"},
	{MappingRunAudit, "/mcp/v1/auditor/audit", "RELAY_MCP_INGRESS_AUDITOR_AUDIT_ADDR", "127.0.0.1:18107"},
}

func Catalog() []RouteDescriptor {
	result := make([]RouteDescriptor, len(mappingCatalog))
	for index, entry := range mappingCatalog {
		result[index] = RouteDescriptor{MappingID: entry.ID, RoutePath: entry.RoutePath}
	}
	return result
}
