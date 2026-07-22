package mcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"

	appaudits "relay/internal/app/audits"
	appoperations "relay/internal/app/operations"
	workflowprojects "relay/internal/app/projects/workflow"
	apptickets "relay/internal/app/tickets"
	appwayfinder "relay/internal/app/wayfinder"
	"relay/internal/mcp/fileacquisition"
	"relay/internal/mcp/routecontracts"
	"relay/internal/mcp/semanticidentity"
	"relay/internal/operations/registry"
	"relay/internal/sourcegateway"
)

type RouteDispatchServices struct {
	Projects      *workflowprojects.Service
	Packets       *appoperations.Service
	Lifecycle     *OperationPacketLifecycleHandler
	Source        *sourcegateway.Service
	Wayfinder     *appwayfinder.Service
	Tickets       *apptickets.Service
	Audits        WorkflowAuditToolService
	AuditReadback AuditReadbackService
}

type AuditReadbackService interface {
	GetAuditEffects(context.Context, string) (any, error)
	GetRemediationSeed(context.Context, string) (any, error)
}

func NewRouteDispatchers(set routecontracts.RouteSet, services RouteDispatchServices) (RouteDispatchers, error) {
	handlers := make(map[string]map[string]SurfaceHandler, len(set.Manifests))
	toolNames := make(map[string]struct{}, 40)
	for _, manifest := range set.Manifests {
		if _, exists := handlers[manifest.RoutePath]; exists {
			return RouteDispatchers{}, fmt.Errorf("MCP_DISPATCHER_MISSING: duplicate route %s", manifest.RoutePath)
		}
		routeHandlers := make(map[string]SurfaceHandler, len(manifest.Tools))
		for _, tool := range manifest.Tools {
			if _, exists := routeHandlers[tool.Name]; exists {
				return RouteDispatchers{}, fmt.Errorf("MCP_DISPATCHER_MISSING: duplicate %s/%s", manifest.RoutePath, tool.Name)
			}
			handler, err := newRouteToolDispatcher(manifest, tool, services)
			if err != nil {
				return RouteDispatchers{}, err
			}
			routeHandlers[tool.Name] = handler
			toolNames[tool.Name] = struct{}{}
		}
		handlers[manifest.RoutePath] = routeHandlers
	}
	if len(toolNames) != 40 {
		return RouteDispatchers{}, fmt.Errorf("MCP_DISPATCHER_MISSING: got %d handlers", len(toolNames))
	}
	return RouteDispatchers{Handlers: handlers}, nil
}

func newRouteToolDispatcher(manifest routecontracts.RouteManifest, tool routecontracts.ToolManifest, services RouteDispatchServices) (SurfaceHandler, error) {
	switch tool.Category {
	case "operation_family":
		return newFamilyContextHandler(manifest, tool, services.Packets), nil
	case "packet":
		return newPacketLifecycleHandler(tool.Name, services), nil
	case "source":
		return newSourceGatewayHandler(manifest, tool.Name, services.Source), nil
	case "wayfinder_action":
		return newWayfinderHandler(tool.Name, services.Wayfinder), nil
	case "frontier":
		return newTicketFrontierHandler(manifest, services.Packets, services.Tickets), nil
	case "audit":
		return newAuditReadbackHandler(manifest, tool.Name, services), nil
	default:
		return nil, fmt.Errorf("MCP_TOOL_CONTRACT_INVALID: category %q", tool.Category)
	}
}

func newFamilyContextHandler(manifest routecontracts.RouteManifest, tool routecontracts.ToolManifest, packets *appoperations.Service) SurfaceHandler {
	return func(raw json.RawMessage) ToolCallResult {
		var input struct {
			PacketID             string `json:"packet_id"`
			ExpectedPacketSHA256 string `json:"expected_packet_sha256"`
		}
		if err := brokerDecodeStrict(raw, &input); err != nil {
			return toolErr("MCP_FAMILY_PACKET_MISMATCH: " + err.Error())
		}
		view, err := packets.Get(context.Background(), input.PacketID)
		if err != nil {
			return toolErr(err.Error())
		}
		if view.Summary.PacketSHA256 != input.ExpectedPacketSHA256 || string(view.Summary.Role) != manifest.Role || string(view.Summary.SurfaceContract) != manifest.SurfaceContract || string(view.Summary.OperationID) != tool.OperationID {
			return toolErr("MCP_FAMILY_PACKET_MISMATCH")
		}
		var document map[string]any
		if err := json.Unmarshal(view.DocumentBytes, &document); err != nil {
			return toolErr(err.Error())
		}
		domainName := lookupDocumentStringField(document, "manifest_domain", "domain")
		domains := []routecontracts.DomainAuthorityIdentity{}
		for _, domain := range manifest.DomainAuthority {
			if domain.Domain == domainName {
				domains = append(domains, domain)
			}
		}
		return workflowOK(map[string]any{"route_path": manifest.RoutePath, "role": manifest.Role, "surface_contract": manifest.SurfaceContract, "operation_id": tool.OperationID, "manifest_domain": domainName, "standing_authority": manifest.StandingAuthority, "domain_authority": domains, "packet": map[string]any{"summary": view.Summary, "document_media_type": view.DocumentMediaType, "document_size_bytes": view.DocumentSizeBytes, "document": document}, "inputs": lookupDocumentArrayField(document["inputs"]), "repositories": lookupDocumentArrayField(document["repositories"]), "readiness": view.Summary.ReadinessState, "blockers": []any{}})
	}
}

func newPacketLifecycleHandler(name string, services RouteDispatchServices) SurfaceHandler {
	switch name {
	case "list_projects":
		return func(raw json.RawMessage) ToolCallResult {
			var in struct {
				Status string `json:"status"`
				Limit  int    `json:"limit"`
			}
			if err := brokerDecodeStrict(raw, &in); err != nil {
				return toolErr(err.Error())
			}
			values, err := services.Projects.ListProjects(context.Background(), workflowprojects.ListProjectsInput{Status: in.Status, Limit: in.Limit})
			if err != nil {
				return toolErr(err.Error())
			}
			return workflowOK(map[string]any{"projects": values})
		}
	case "get_active_operation_packet":
		return func(raw json.RawMessage) ToolCallResult {
			var in struct {
				PacketID string `json:"packet_id"`
			}
			if err := brokerDecodeStrict(raw, &in); err != nil {
				return toolErr(err.Error())
			}
			value, err := services.Packets.Get(context.Background(), in.PacketID)
			if err != nil {
				return toolErr(err.Error())
			}
			return workflowOK(value)
		}
	case "create_operation_packet":
		return func(raw json.RawMessage) ToolCallResult {
			var in struct {
				MutationID string                                 `json:"mutation_id"`
				Identity   semanticidentity.CreateOperationPacket `json:"identity"`
				Files      []fileacquisition.FileParameter        `json:"files"`
			}
			if err := brokerDecodeStrict(raw, &in); err != nil {
				return toolErr(err.Error())
			}
			value, err := services.Lifecycle.Create(context.Background(), CreateOperationPacketRequest{MutationID: in.MutationID, Identity: in.Identity, Files: in.Files})
			if err != nil {
				return toolErr(err.Error())
			}
			return workflowOK(value)
		}
	case "refresh_operation_packet":
		return func(raw json.RawMessage) ToolCallResult {
			var in struct {
				MutationID    string                                  `json:"mutation_id"`
				PriorPacketID string                                  `json:"prior_packet_id"`
				Identity      semanticidentity.RefreshOperationPacket `json:"identity"`
				Files         []fileacquisition.FileParameter         `json:"files"`
			}
			if err := brokerDecodeStrict(raw, &in); err != nil {
				return toolErr(err.Error())
			}
			value, err := services.Lifecycle.Refresh(context.Background(), RefreshOperationPacketRequest{MutationID: in.MutationID, PriorPacketID: in.PriorPacketID, Identity: in.Identity, Files: in.Files})
			if err != nil {
				return toolErr(err.Error())
			}
			return workflowOK(value)
		}
	case "close_operation_packet":
		return func(raw json.RawMessage) ToolCallResult {
			var in struct {
				MutationID string                                `json:"mutation_id"`
				Identity   semanticidentity.CloseOperationPacket `json:"identity"`
			}
			if err := brokerDecodeStrict(raw, &in); err != nil {
				return toolErr(err.Error())
			}
			value, err := services.Lifecycle.Close(context.Background(), CloseOperationPacketRequest{MutationID: in.MutationID, Identity: in.Identity})
			if err != nil {
				return toolErr(err.Error())
			}
			return workflowOK(value)
		}
	case "read_operation_input":
		return newPacketSectionProjectionHandler(services.Packets, "inputs")
	case "list_operation_repositories":
		return newPacketSectionProjectionHandler(services.Packets, "repositories")
	default:
		return func(json.RawMessage) ToolCallResult { return toolErr("MCP_DISPATCHER_MISSING: " + name) }
	}
}

func newPacketSectionProjectionHandler(packets *appoperations.Service, section string) SurfaceHandler {
	return func(raw json.RawMessage) ToolCallResult {
		var in struct {
			PacketID string `json:"packet_id"`
		}
		if err := brokerDecodeStrict(raw, &in); err != nil {
			return toolErr(err.Error())
		}
		view, err := packets.Get(context.Background(), in.PacketID)
		if err != nil {
			return toolErr(err.Error())
		}
		var doc map[string]any
		if err := json.Unmarshal(view.DocumentBytes, &doc); err != nil {
			return toolErr(err.Error())
		}
		return workflowOK(map[string]any{"packet": view.Summary, section: lookupDocumentArrayField(doc[section])})
	}
}

type packetSourceContext struct {
	PacketID      string                          `json:"packet_id"`
	OperationID   registry.OperationID            `json:"operation_id"`
	RepositoryKey string                          `json:"repository_key"`
	Revision      sourcegateway.RevisionReference `json:"revision"`
}

func requireManifestOperation(manifest routecontracts.RouteManifest, requested registry.OperationID) (registry.SurfaceContractID, registry.OperationID, error) {
	if requested == "" {
		if len(manifest.Operations) != 1 {
			return "", "", fmt.Errorf("operation_id is required")
		}
		requested = registry.OperationID(manifest.Operations[0].OperationID)
	}
	for _, op := range manifest.Operations {
		if op.OperationID == string(requested) {
			return registry.SurfaceContractID(manifest.SurfaceContract), requested, nil
		}
	}
	return "", "", fmt.Errorf("operation_id is not a route member")
}

func newSourceGatewayHandler(manifest routecontracts.RouteManifest, name string, service *sourcegateway.Service) SurfaceHandler {
	return func(raw json.RawMessage) ToolCallResult {
		switch name {
		case "list_source_tree":
			var in struct {
				packetSourceContext
				Directory sourcegateway.PathReference `json:"directory"`
				Recursive bool                        `json:"recursive"`
				Limit     int                         `json:"limit"`
				Cursor    string                      `json:"cursor"`
			}
			if err := brokerDecodeStrict(raw, &in); err != nil {
				return toolErr(err.Error())
			}
			surface, op, err := requireManifestOperation(manifest, in.OperationID)
			if err != nil {
				return toolErr(err.Error())
			}
			value, err := service.ListTree(context.Background(), sourcegateway.ListTreeRequest{PacketID: in.PacketID, SurfaceContract: surface, OperationID: op, RepositoryKey: in.RepositoryKey, Directory: in.Directory, Recursive: in.Recursive, Limit: in.Limit, Cursor: in.Cursor})
			if err != nil {
				return toolErr(err.Error())
			}
			return workflowOK(value)
		case "search_source":
			var in struct {
				packetSourceContext
				Mode              sourcegateway.SearchMode      `json:"mode"`
				TextLiteral       string                        `json:"text_literal"`
				ByteLiteralBase64 string                        `json:"byte_literal_base64"`
				Prefixes          []sourcegateway.PathReference `json:"prefixes"`
				Limit             int                           `json:"limit"`
				ExaminedObjects   int64                         `json:"examined_objects"`
				ExaminedBytes     int64                         `json:"examined_bytes"`
				Cursor            string                        `json:"cursor"`
			}
			if err := brokerDecodeStrict(raw, &in); err != nil {
				return toolErr(err.Error())
			}
			surface, op, err := requireManifestOperation(manifest, in.OperationID)
			if err != nil {
				return toolErr(err.Error())
			}
			literal, err := base64.StdEncoding.DecodeString(in.ByteLiteralBase64)
			if in.ByteLiteralBase64 == "" {
				literal = nil
				err = nil
			}
			if err != nil {
				return toolErr(err.Error())
			}
			value, err := service.Search(context.Background(), sourcegateway.SearchRequest{PacketID: in.PacketID, SurfaceContract: surface, OperationID: op, RepositoryKey: in.RepositoryKey, Revision: in.Revision, Mode: in.Mode, TextLiteral: in.TextLiteral, ByteLiteral: literal, Prefixes: in.Prefixes, Limit: in.Limit, Budget: sourcegateway.SearchBudget{ExaminedObjects: in.ExaminedObjects, ExaminedBytes: in.ExaminedBytes}, Cursor: in.Cursor})
			if err != nil {
				return toolErr(err.Error())
			}
			return workflowOK(value)
		case "read_source_text":
			var in struct {
				packetSourceContext
				Path   sourcegateway.PathReference `json:"path"`
				Offset int64                       `json:"offset"`
				Limit  int64                       `json:"limit"`
				Cursor string                      `json:"cursor"`
			}
			if err := brokerDecodeStrict(raw, &in); err != nil {
				return toolErr(err.Error())
			}
			surface, op, err := requireManifestOperation(manifest, in.OperationID)
			if err != nil {
				return toolErr(err.Error())
			}
			value, err := service.ReadText(context.Background(), sourcegateway.ReadTextRequest{PacketID: in.PacketID, SurfaceContract: surface, OperationID: op, RepositoryKey: in.RepositoryKey, Revision: in.Revision, Path: in.Path, Offset: in.Offset, Limit: in.Limit, Cursor: in.Cursor})
			if err != nil {
				return toolErr(err.Error())
			}
			return workflowOK(value)
		case "read_source_blob":
			var in struct {
				packetSourceContext
				Path   sourcegateway.PathReference `json:"path"`
				Offset int64                       `json:"offset"`
				Limit  int64                       `json:"limit"`
				Cursor string                      `json:"cursor"`
			}
			if err := brokerDecodeStrict(raw, &in); err != nil {
				return toolErr(err.Error())
			}
			surface, op, err := requireManifestOperation(manifest, in.OperationID)
			if err != nil {
				return toolErr(err.Error())
			}
			value, err := service.ReadBlob(context.Background(), sourcegateway.ReadBlobRequest{PacketID: in.PacketID, SurfaceContract: surface, OperationID: op, RepositoryKey: in.RepositoryKey, Path: in.Path, Offset: in.Offset, Limit: in.Limit, Cursor: in.Cursor})
			if err != nil {
				return toolErr(err.Error())
			}
			return workflowOK(value)
		case "get_source_commit":
			var in struct {
				packetSourceContext
				CommitOID string `json:"commit_oid"`
				Offset    int64  `json:"offset"`
				Limit     int64  `json:"limit"`
				Cursor    string `json:"cursor"`
			}
			if err := brokerDecodeStrict(raw, &in); err != nil {
				return toolErr(err.Error())
			}
			surface, op, err := requireManifestOperation(manifest, in.OperationID)
			if err != nil {
				return toolErr(err.Error())
			}
			value, err := service.ReadCommitBytes(context.Background(), sourcegateway.ReadCommitBytesRequest{PacketID: in.PacketID, SurfaceContract: surface, OperationID: op, RepositoryKey: in.RepositoryKey, Revision: in.Revision, CommitOID: in.CommitOID, Offset: in.Offset, Limit: in.Limit, Cursor: in.Cursor})
			if err != nil {
				return toolErr(err.Error())
			}
			return workflowOK(value)
		case "list_source_history":
			var in struct {
				packetSourceContext
				Path   *sourcegateway.PathReference `json:"path"`
				Limit  int                          `json:"limit"`
				Cursor string                       `json:"cursor"`
			}
			if err := brokerDecodeStrict(raw, &in); err != nil {
				return toolErr(err.Error())
			}
			surface, op, err := requireManifestOperation(manifest, in.OperationID)
			if err != nil {
				return toolErr(err.Error())
			}
			if in.Path == nil {
				value, err := service.CommitHistory(context.Background(), sourcegateway.CommitHistoryRequest{PacketID: in.PacketID, SurfaceContract: surface, OperationID: op, RepositoryKey: in.RepositoryKey, Revision: in.Revision, Limit: in.Limit, Cursor: in.Cursor})
				if err != nil {
					return toolErr(err.Error())
				}
				return workflowOK(value)
			}
			value, err := service.PathHistory(context.Background(), sourcegateway.PathHistoryRequest{PacketID: in.PacketID, SurfaceContract: surface, OperationID: op, RepositoryKey: in.RepositoryKey, Revision: in.Revision, Path: *in.Path, Limit: in.Limit, Cursor: in.Cursor})
			if err != nil {
				return toolErr(err.Error())
			}
			return workflowOK(value)
		case "compare_source":
			var in struct {
				packetSourceContext
				Before sourcegateway.RevisionReference `json:"before"`
				After  sourcegateway.RevisionReference `json:"after"`
				Limit  int                             `json:"limit"`
				Cursor string                          `json:"cursor"`
			}
			if err := brokerDecodeStrict(raw, &in); err != nil {
				return toolErr(err.Error())
			}
			surface, op, err := requireManifestOperation(manifest, in.OperationID)
			if err != nil {
				return toolErr(err.Error())
			}
			value, err := service.Compare(context.Background(), sourcegateway.CompareRequest{PacketID: in.PacketID, SurfaceContract: surface, OperationID: op, RepositoryKey: in.RepositoryKey, Before: in.Before, After: in.After, Limit: in.Limit, Cursor: in.Cursor})
			if err != nil {
				return toolErr(err.Error())
			}
			return workflowOK(value)
		case "read_source_diff":
			var in struct {
				packetSourceContext
				Before sourcegateway.RevisionReference `json:"before"`
				After  sourcegateway.RevisionReference `json:"after"`
				Offset int64                           `json:"offset"`
				Limit  int64                           `json:"limit"`
				Cursor string                          `json:"cursor"`
			}
			if err := brokerDecodeStrict(raw, &in); err != nil {
				return toolErr(err.Error())
			}
			surface, op, err := requireManifestOperation(manifest, in.OperationID)
			if err != nil {
				return toolErr(err.Error())
			}
			value, err := service.ReadDiff(context.Background(), sourcegateway.ReadDiffRequest{PacketID: in.PacketID, SurfaceContract: surface, OperationID: op, RepositoryKey: in.RepositoryKey, Before: in.Before, After: in.After, Offset: in.Offset, Limit: in.Limit, Cursor: in.Cursor})
			if err != nil {
				return toolErr(err.Error())
			}
			return workflowOK(value)
		default:
			return toolErr("MCP_DISPATCHER_MISSING: " + name)
		}
	}
}

func newWayfinderHandler(name string, service *appwayfinder.Service) SurfaceHandler {
	return func(raw json.RawMessage) ToolCallResult {
		switch name {
		case "create_workspace":
			var in appwayfinder.CreateWorkspaceInput
			if err := brokerDecodeStrict(raw, &in); err != nil {
				return toolErr(err.Error())
			}
			value, err := service.CreateWorkspace(context.Background(), in)
			if err != nil {
				return toolErr(err.Error())
			}
			return workflowOK(map[string]any{"workspace": value})
		case "admit_workspace_input":
			var in appwayfinder.AdmitInputInput
			if err := brokerDecodeStrict(raw, &in); err != nil {
				return toolErr(err.Error())
			}
			value, workspace, err := service.AdmitInput(context.Background(), in)
			if err != nil {
				return toolErr(err.Error())
			}
			return workflowOK(map[string]any{"input": value, "workspace": workspace})
		case "add_workspace_destination":
			var in appwayfinder.AddDestinationInput
			if err := brokerDecodeStrict(raw, &in); err != nil {
				return toolErr(err.Error())
			}
			value, workspace, err := service.AddDestination(context.Background(), in)
			if err != nil {
				return toolErr(err.Error())
			}
			return workflowOK(map[string]any{"destination": value, "workspace": workspace})
		case "route_workspace":
			var in appwayfinder.RouteWorkspaceInput
			if err := brokerDecodeStrict(raw, &in); err != nil {
				return toolErr(err.Error())
			}
			value, workspace, err := service.RouteWorkspace(context.Background(), in)
			if err != nil {
				return toolErr(err.Error())
			}
			return workflowOK(map[string]any{"route": value, "workspace": workspace})
		case "create_discovery_ticket":
			var in appwayfinder.CreateDiscoveryTicketInput
			if err := brokerDecodeStrict(raw, &in); err != nil {
				return toolErr(err.Error())
			}
			value, workspace, err := service.CreateDiscoveryTicket(context.Background(), in)
			if err != nil {
				return toolErr(err.Error())
			}
			return workflowOK(map[string]any{"ticket": value, "workspace": workspace})
		case "resolve_discovery_ticket":
			var in appwayfinder.ResolveDiscoveryTicketInput
			if err := brokerDecodeStrict(raw, &in); err != nil {
				return toolErr(err.Error())
			}
			resolution, ticket, workspace, err := service.ResolveDiscoveryTicket(context.Background(), in)
			if err != nil {
				return toolErr(err.Error())
			}
			return workflowOK(map[string]any{"resolution": resolution, "ticket": ticket, "workspace": workspace})
		case "attach_investigation":
			var in appwayfinder.AttachInvestigationInput
			if err := brokerDecodeStrict(raw, &in); err != nil {
				return toolErr(err.Error())
			}
			value, workspace, err := service.AttachInvestigation(context.Background(), in)
			if err != nil {
				return toolErr(err.Error())
			}
			return workflowOK(map[string]any{"investigation": value, "workspace": workspace})
		default:
			return toolErr("MCP_DISPATCHER_MISSING: " + name)
		}
	}
}

func newTicketFrontierHandler(manifest routecontracts.RouteManifest, packets *appoperations.Service, tickets *apptickets.Service) SurfaceHandler {
	return func(raw json.RawMessage) ToolCallResult {
		var in struct {
			PacketID string `json:"packet_id"`
			TicketID string `json:"ticket_id"`
		}
		if err := brokerDecodeStrict(raw, &in); err != nil {
			return toolErr(err.Error())
		}
		view, err := packets.Get(context.Background(), in.PacketID)
		if err != nil {
			return toolErr(err.Error())
		}
		if string(view.Summary.SurfaceContract) != manifest.SurfaceContract || string(view.Summary.OperationID) != "planner.ticket_frontier" {
			return toolErr("packet route mismatch")
		}
		value, err := tickets.Read(context.Background(), in.TicketID)
		if err != nil {
			return toolErr(err.Error())
		}
		return workflowOK(value)
	}
}

func newAuditReadbackHandler(manifest routecontracts.RouteManifest, name string, services RouteDispatchServices) SurfaceHandler {
	return func(raw json.RawMessage) ToolCallResult {
		switch name {
		case "get_audit_packet":
			var in struct {
				RunID string `json:"run_id"`
			}
			if err := brokerDecodeStrict(raw, &in); err != nil {
				return toolErr(err.Error())
			}
			result, err := services.Audits.GetCurrentPacket(context.Background(), in.RunID)
			if err != nil {
				return toolErr(err.Error())
			}
			var packet any
			if err := json.Unmarshal(result.PacketBytes, &packet); err != nil {
				return toolErr(err.Error())
			}
			return workflowOK(map[string]any{"run": result.Run, "audit_packet": result.Packet, "packet": packet})
		case "get_run_artifact":
			var in appaudits.GetWorkflowAuditArtifactInput
			if err := brokerDecodeStrict(raw, &in); err != nil {
				return toolErr(err.Error())
			}
			result, err := services.Audits.GetCurrentArtifact(context.Background(), in)
			if err != nil {
				return toolErr(err.Error())
			}
			return workflowOK(result)
		case "record_audit_decision":
			var in appaudits.RecordWorkflowAuditDecisionInput
			if err := brokerDecodeStrict(raw, &in); err != nil {
				return toolErr(err.Error())
			}
			result, err := services.Audits.RecordDecision(context.Background(), in)
			if err != nil {
				return toolErr(err.Error())
			}
			return workflowOK(result)
		case "get_audit_effects":
			var in struct {
				PacketID        string `json:"packet_id"`
				AuditDecisionID string `json:"audit_decision_id"`
			}
			if err := brokerDecodeStrict(raw, &in); err != nil {
				return toolErr(err.Error())
			}
			if err := validateAuditRoute(manifest, services.Packets, in.PacketID); err != nil {
				return toolErr(err.Error())
			}
			result, err := services.AuditReadback.GetAuditEffects(context.Background(), in.AuditDecisionID)
			if err != nil {
				return toolErr(err.Error())
			}
			return workflowOK(result)
		case "get_remediation_seed":
			var in struct {
				PacketID          string `json:"packet_id"`
				RemediationSeedID string `json:"remediation_seed_id"`
			}
			if err := brokerDecodeStrict(raw, &in); err != nil {
				return toolErr(err.Error())
			}
			if err := validateAuditRoute(manifest, services.Packets, in.PacketID); err != nil {
				return toolErr(err.Error())
			}
			result, err := services.AuditReadback.GetRemediationSeed(context.Background(), in.RemediationSeedID)
			if err != nil {
				return toolErr(err.Error())
			}
			return workflowOK(result)
		default:
			return toolErr("MCP_DISPATCHER_MISSING: " + name)
		}
	}
}
func validateAuditRoute(manifest routecontracts.RouteManifest, packets *appoperations.Service, packetID string) error {
	view, err := packets.Get(context.Background(), packetID)
	if err != nil {
		return err
	}
	if string(view.Summary.Role) != manifest.Role || string(view.Summary.SurfaceContract) != manifest.SurfaceContract || string(view.Summary.OperationID) != "auditor.audit" {
		return fmt.Errorf("packet route mismatch")
	}
	return nil
}
func lookupDocumentStringField(document map[string]any, objectName, property string) string {
	object, ok := document[objectName].(map[string]any)
	if !ok {
		return ""
	}
	value, _ := object[property].(string)
	return value
}
func lookupDocumentArrayField(value any) []any {
	items, ok := value.([]any)
	if !ok {
		return []any{}
	}
	return items
}
