package server

import (
	"context"
	"fmt"
	"os"

	"relay/internal/transport/mcpingress"
)

type MCPRouteDescriptor struct {
	MappingID                   mcpingress.MappingID
	RoutePath                   string
	PublicSurface               string
	PublicSurfaceManifestSHA256 string
	ToolIdentities              []mcpingress.ToolIdentity
}

type MCPIngressMappingSummary struct {
	MappingID       mcpingress.MappingID
	RoutePath       string
	ListenerAddress string
}

type MCPIngressSummary struct {
	Mappings                 []MCPIngressMappingSummary
	UpstreamBearerConfigured bool
}

func mcpRouteDescriptors(handlers []mcpHandler) ([]MCPRouteDescriptor, error) {
	if len(handlers) == 0 {
		return nil, nil
	}
	catalog := mcpingress.Catalog()
	if len(handlers) != len(catalog) {
		return nil, fmt.Errorf("MCP_INGRESS_MAPPING_SET_INVALID: handlers=%d", len(handlers))
	}
	byPath := make(map[string]mcpHandler, len(handlers))
	for _, handler := range handlers {
		if _, duplicate := byPath[handler.Path]; duplicate {
			return nil, fmt.Errorf("MCP_INGRESS_MAPPING_SET_INVALID: duplicate route %s", handler.Path)
		}
		byPath[handler.Path] = handler
	}
	result := make([]MCPRouteDescriptor, 0, len(catalog))
	for _, mapping := range catalog {
		handler, ok := byPath[mapping.RoutePath]
		if !ok || handler.PublicSurface != string(mapping.MappingID) || handler.PublicSurfaceManifestSHA256 == "" || len(handler.ToolRegistrations) == 0 {
			return nil, fmt.Errorf("MCP_INGRESS_ROUTE_MISMATCH: %s", mapping.RoutePath)
		}
		identities := make([]mcpingress.ToolIdentity, 0, len(handler.ToolRegistrations))
		seen := make(map[string]struct{}, len(handler.ToolRegistrations))
		for _, registration := range handler.ToolRegistrations {
			if registration.AdvertisedName == "" || registration.InternalToolName == "" || registration.InternalRoutePath == "" || registration.SurfaceContract == "" || registration.RouteManifestSHA256 == "" || registration.StandingAuthority.Path == "" {
				return nil, fmt.Errorf("MCP_INGRESS_REGISTRATION_INVALID: %s", handler.Path)
			}
			if _, duplicate := seen[registration.AdvertisedName]; duplicate {
				return nil, fmt.Errorf("MCP_INGRESS_REGISTRATION_DUPLICATE: %s", registration.AdvertisedName)
			}
			seen[registration.AdvertisedName] = struct{}{}
			identities = append(identities, mcpingress.ToolIdentity{
				AdvertisedName: registration.AdvertisedName, InternalToolName: registration.InternalToolName,
				InternalRoutePath: registration.InternalRoutePath, SurfaceContract: registration.SurfaceContract,
				RouteManifestSHA256:         registration.RouteManifestSHA256,
				StandingAuthorityRepository: registration.StandingAuthority.Repository, StandingAuthorityCommitOID: registration.StandingAuthority.Commit,
				StandingAuthorityPath: registration.StandingAuthority.Path, StandingAuthorityBlobOID: registration.StandingAuthority.BlobOID,
			})
		}
		result = append(result, MCPRouteDescriptor{
			MappingID: mapping.MappingID, RoutePath: handler.Path, PublicSurface: handler.PublicSurface,
			PublicSurfaceManifestSHA256: handler.PublicSurfaceManifestSHA256, ToolIdentities: identities,
		})
	}
	return result, nil
}

func (server *Server) PrepareMCPIngress(defaultUpstreamBase string) (MCPIngressSummary, error) {
	server.ingressMu.Lock()
	defer server.ingressMu.Unlock()
	if server.ingress != nil {
		return MCPIngressSummary{}, fmt.Errorf("MCP ingress is already prepared")
	}
	if len(server.mcpRoutes) != 3 {
		return MCPIngressSummary{}, fmt.Errorf("MCP_INGRESS_MAPPING_SET_INVALID: route descriptors=%d", len(server.mcpRoutes))
	}
	descriptors := make([]mcpingress.RouteDescriptor, len(server.mcpRoutes))
	for index, route := range server.mcpRoutes {
		descriptors[index] = mcpingress.RouteDescriptor{
			MappingID: route.MappingID, RoutePath: route.RoutePath, PublicSurface: route.PublicSurface,
			PublicSurfaceManifestSHA256: route.PublicSurfaceManifestSHA256, ToolIdentities: append([]mcpingress.ToolIdentity(nil), route.ToolIdentities...),
		}
	}
	config, err := mcpingress.LoadConfig(os.Getenv, defaultUpstreamBase, descriptors)
	if err != nil {
		return MCPIngressSummary{}, err
	}
	supervisor, err := mcpingress.NewSupervisor(config, server.log)
	if err != nil {
		return MCPIngressSummary{}, err
	}
	server.ingress = supervisor
	summary := MCPIngressSummary{UpstreamBearerConfigured: config.Bearer.Configured()}
	for _, mapping := range config.Mappings {
		summary.Mappings = append(summary.Mappings, MCPIngressMappingSummary{MappingID: mapping.ID, RoutePath: mapping.RoutePath, ListenerAddress: mapping.Listener.String()})
	}
	return summary, nil
}

func (server *Server) StartMCPIngress(ctx context.Context) error {
	server.ingressMu.Lock()
	supervisor := server.ingress
	server.ingressMu.Unlock()
	if supervisor == nil {
		return fmt.Errorf("MCP ingress is not prepared")
	}
	return supervisor.Start(ctx)
}

func (server *Server) ShutdownMCPIngress(ctx context.Context) error {
	server.ingressMu.Lock()
	supervisor := server.ingress
	server.ingress = nil
	server.ingressMu.Unlock()
	if supervisor == nil {
		return nil
	}
	return supervisor.Shutdown(ctx)
}

func (server *Server) MCPIngressHealth() []mcpingress.HealthSnapshot {
	server.ingressMu.Lock()
	supervisor := server.ingress
	server.ingressMu.Unlock()
	if supervisor == nil {
		return nil
	}
	return supervisor.Snapshots()
}
