package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	appoperations "relay/internal/app/operations"
	workflowprojects "relay/internal/app/projects/workflow"
	apptickets "relay/internal/app/tickets"
	"relay/internal/mcp/routecontracts"
	"relay/internal/mcp/semanticidentity"
	"relay/internal/operations/registry"
)

func TestListProjectsRouteContractsAcceptOnlyMountedSurface(t *testing.T) {
	store := coldStartStore(t, t.TempDir(), false)
	t.Cleanup(func() { _ = store.Close() })
	projects, err := workflowprojects.NewService(store)
	if err != nil {
		t.Fatal(err)
	}
	routes, err := routecontracts.BuildMCPRouteManifests()
	if err != nil {
		t.Fatal(err)
	}
	dispatchers, err := NewRouteDispatchers(routes, RouteDispatchServices{Projects: projects})
	if err != nil {
		t.Fatal(err)
	}

	for _, manifest := range routes.Manifests {
		t.Run(manifest.SurfaceContract, func(t *testing.T) {
			handlers, err := BuildRouteHandlers(manifest, dispatchers)
			if err != nil {
				t.Fatal(err)
			}
			server, err := NewServerForRoute(nil, manifest, handlers)
			if err != nil {
				t.Fatal(err)
			}
			matching := coldStartCall(t, server, "list_projects", map[string]any{
				"surface_contract": manifest.SurfaceContract,
				"status":           "active",
				"limit":            100,
			})
			if matching.Error != nil {
				t.Fatalf("matching surface rejected: %v", matching.Error)
			}
			var listed struct {
				Projects []any `json:"projects"`
			}
			coldStartDecode(t, matching, &listed)

			foreign := "wayfinder-workspace.v1"
			if foreign == manifest.SurfaceContract {
				foreign = "auditor-audit.v1"
			}
			if response := coldStartCall(t, server, "list_projects", map[string]any{"surface_contract": foreign}); response.Error == nil {
				t.Fatal("foreign surface was accepted")
			}
			if response := coldStartCall(t, server, "list_projects", map[string]any{"surface_contract": manifest.SurfaceContract, "unknown": true}); response.Error == nil {
				t.Fatal("unknown field was accepted")
			}
		})
	}
}

func TestWayfinderListProjectsTransportEnrichmentPreservesRouteAuthority(t *testing.T) {
	store := coldStartStore(t, t.TempDir(), false)
	t.Cleanup(func() { _ = store.Close() })
	projects, err := workflowprojects.NewService(store)
	if err != nil {
		t.Fatal(err)
	}
	routes, err := routecontracts.BuildMCPRouteManifests()
	if err != nil {
		t.Fatal(err)
	}
	dispatchers, err := NewRouteDispatchers(routes, RouteDispatchServices{Projects: projects})
	if err != nil {
		t.Fatal(err)
	}
	surfaces, err := routecontracts.BuildAppSurfaceManifests(routes)
	if err != nil {
		t.Fatal(err)
	}
	var wayfinder routecontracts.AppSurfaceManifest
	for _, surface := range surfaces.Surfaces {
		if surface.Surface == routecontracts.AppSurfaceWayfinder {
			wayfinder = surface
			break
		}
	}
	registrations, err := BuildAppSurfaceHandlers(wayfinder, dispatchers)
	if err != nil {
		t.Fatal(err)
	}
	server, err := NewServerForAppSurface(nil, wayfinder, registrations)
	if err != nil {
		t.Fatal(err)
	}
	const tool = "wayfinder-workspace-v1__list_projects"
	clientVisible := map[string]any{"limit": 100, "status": "active"}
	response := coldStartCall(t, server, tool, clientVisible)
	if response.Error != nil {
		t.Fatalf("transport-enriched call failed: %v", response.Error)
	}
	var listed struct {
		Projects []any `json:"projects"`
	}
	coldStartDecode(t, response, &listed)

	matching := map[string]any{"limit": 100, "status": "active", "surface_contract": "wayfinder-workspace.v1"}
	if response := coldStartCall(t, server, tool, matching); response.Error != nil {
		t.Fatalf("matching client surface rejected: %v", response.Error)
	}
	conflicting := map[string]any{"limit": 100, "status": "active", "surface_contract": "auditor-review.v1"}
	if response := coldStartCall(t, server, tool, conflicting); response.Error == nil || !strings.Contains(response.Error.Message, "conflicts with mounted route") {
		t.Fatalf("conflicting client surface was silently overwritten: %#v", response.Error)
	}
}

func TestRouteSourceDispatchersUseEachRouteManifest(t *testing.T) {
	set, err := routecontracts.BuildMCPRouteManifests()
	if err != nil {
		t.Fatal(err)
	}
	owners, err := NewRouteDispatchers(set, RouteDispatchServices{})
	if err != nil {
		t.Fatal(err)
	}

	for _, manifest := range set.Manifests {
		t.Run(manifest.RoutePath, func(t *testing.T) {
			var ownOperation string
			for _, operation := range manifest.Operations {
				ownOperation = operation.OperationID
				break
			}
			if ownOperation == "" {
				t.Fatal("route has no operation")
			}
			foreignOperation := "wayfinder.workspace"
			for _, operation := range manifest.Operations {
				if operation.OperationID == foreignOperation {
					foreignOperation = "auditor.audit"
					break
				}
			}

			handlers, err := BuildRouteHandlers(manifest, owners)
			if err != nil {
				t.Fatal(err)
			}
			var search SurfaceHandler
			for _, handler := range handlers {
				if handler.Name == "search_source" {
					search = handler.Handle
					break
				}
			}
			if search == nil {
				t.Fatal("search_source handler missing")
			}

			own := search(sourceSearchDispatchTestInput(t, ownOperation))
			if strings.Contains(toolResultText(own), "operation_id is not a route member") {
				t.Fatalf("own operation rejected by route dispatcher: %s", toolResultText(own))
			}

			foreign := search(sourceSearchDispatchTestInput(t, foreignOperation))
			if !strings.Contains(toolResultText(foreign), "operation_id is not a route member") {
				t.Fatalf("foreign operation was not rejected: %#v", foreign)
			}
		})
	}
}

func sourceSearchDispatchTestInput(t *testing.T, operation string) json.RawMessage {
	t.Helper()
	value, err := json.Marshal(map[string]any{
		"operation_id":        operation,
		"byte_literal_base64": "!",
	})
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func toolResultText(result ToolCallResult) string {
	if len(result.Content) == 0 {
		return ""
	}
	return result.Content[0].Text
}

type routeFrontierPacketAuthorizer struct {
	events  *[]string
	request appoperations.MutationRequest
}

func (a *routeFrontierPacketAuthorizer) AuthorizeMutation(_ context.Context, request appoperations.MutationRequest) (appoperations.MutationAuthorization, error) {
	a.request = request
	*a.events = append(*a.events, "admit")
	return appoperations.MutationAuthorization{Allowed: true, Summary: appoperations.PacketSummary{ProjectID: "project-packet"}}, nil
}

type routeFrontierReader struct {
	events    *[]string
	feature   string
	unitID    string
	projectID string
}

func (r *routeFrontierReader) ReadFrontier(_ context.Context, input apptickets.FrontierV2Input) (apptickets.FrontierV2, error) {
	r.feature = input.FeatureSlug
	r.unitID = input.RequestedUnitID
	r.projectID = input.ProjectID
	*r.events = append(*r.events, "read")
	return apptickets.FrontierV2{FeatureSlug: input.FeatureSlug, Entries: []apptickets.FrontierV2Entry{}, ProgramCandidates: []string{}}, nil
}

func TestTicketFrontierDispatchAdmitsPublishedPlannerIdentityBeforeRead(t *testing.T) {
	set, err := routecontracts.BuildMCPRouteManifests()
	if err != nil {
		t.Fatal(err)
	}
	var frontierRoute string
	for _, manifest := range set.Manifests {
		if manifest.SurfaceContract == string(registry.PlannerTicketFrontierSurface) {
			frontierRoute = manifest.RoutePath
			break
		}
	}
	if frontierRoute == "" {
		t.Fatal("Planner frontier route is missing")
	}

	events := []string{}
	packets := &routeFrontierPacketAuthorizer{events: &events}
	admissionService, err := appoperations.NewTicketFrontierAdmissionService(packets)
	if err != nil {
		t.Fatal(err)
	}
	admitter, err := NewTicketFrontierAdmitter(admissionService)
	if err != nil {
		t.Fatal(err)
	}
	reader := &routeFrontierReader{events: &events}
	dispatchers, err := NewRouteDispatchers(set, RouteDispatchServices{
		TicketFrontierAdmitter: admitter,
		Tickets:                reader,
	})
	if err != nil {
		t.Fatal(err)
	}
	handler := dispatchers.Handlers[frontierRoute]["read_ticket_frontier"]
	if handler == nil {
		t.Fatal("Planner frontier handler is missing")
	}

	input, err := json.Marshal(map[string]string{"packet_id": "planner-packet", "feature_slug": "checkout", "requested_unit_id": "P4-T1"})
	if err != nil {
		t.Fatal(err)
	}
	if result := handler(input); result.IsError {
		t.Fatalf("frontier result = %s", toolResultText(result))
	}
	if strings.Join(events, ",") != "admit,read" {
		t.Fatalf("call order = %v", events)
	}
	if reader.feature != "checkout" || reader.unitID != "P4-T1" || reader.projectID != "project-packet" {
		t.Fatalf("frontier read input = %#v", reader)
	}
	if packets.request.PacketID != "planner-packet" ||
		packets.request.SurfaceContract != registry.PlannerTicketFrontierSurface ||
		packets.request.OperationID != registry.PlannerTicketFrontierOperationID ||
		packets.request.Action != registry.TicketActionReadFrontier {
		t.Fatalf("packet admission request = %#v", packets.request)
	}
}

func TestTicketFrontierDispatchRejectsLegacyTicketIDRequest(t *testing.T) {
	set, err := routecontracts.BuildMCPRouteManifests()
	if err != nil {
		t.Fatal(err)
	}
	events := []string{}
	packets := &routeFrontierPacketAuthorizer{events: &events}
	admissionService, err := appoperations.NewTicketFrontierAdmissionService(packets)
	if err != nil {
		t.Fatal(err)
	}
	admitter, err := NewTicketFrontierAdmitter(admissionService)
	if err != nil {
		t.Fatal(err)
	}
	dispatchers, err := NewRouteDispatchers(set, RouteDispatchServices{TicketFrontierAdmitter: admitter, Tickets: &routeFrontierReader{events: &events}})
	if err != nil {
		t.Fatal(err)
	}
	var frontierRoute string
	for _, manifest := range set.Manifests {
		if manifest.SurfaceContract == string(registry.PlannerTicketFrontierSurface) {
			frontierRoute = manifest.RoutePath
			break
		}
	}
	handler := dispatchers.Handlers[frontierRoute]["read_ticket_frontier"]
	input, err := json.Marshal(map[string]string{"packet_id": "planner-packet", "ticket_id": "ticket-1"})
	if err != nil {
		t.Fatal(err)
	}
	if result := handler(input); !result.IsError {
		t.Fatal("legacy ticket_id request was accepted")
	}
	if strings.Join(events, "") != "" {
		t.Fatalf("legacy request reached admission = %v", events)
	}
}

func TestPacketInputRequestValidationReportsMissingSemanticMetadata(t *testing.T) {
	fileIndex := int64(0)
	for _, test := range []struct {
		name, displayName, mediaType, field string
		input                               semanticidentity.InputBinding
	}{
		{name: "display name", mediaType: "text/plain", field: "$.inputs[0].display_name"},
		{name: "media type", displayName: "resolution.txt", field: "$.inputs[0].media_type"},
		{name: "inline source extra field", displayName: "resolution.txt", mediaType: "text/plain", field: "$.inputs[0].source", input: semanticidentity.InputBinding{SourceKind: "inline_text", Source: semanticidentity.InputBindingSource{Text: "resolution", FileIndex: &fileIndex}}},
		{name: "uploaded source remains accepted", displayName: "candidate.md", mediaType: "text/markdown", input: semanticidentity.InputBinding{SourceKind: "uploaded_file", Source: semanticidentity.InputBindingSource{FileIndex: &fileIndex}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := test.input
			input.DisplayName, input.MediaType = test.displayName, test.mediaType
			result := validatePacketInputRequest([]semanticidentity.InputBinding{input})
			if test.field == "" {
				if result != nil {
					t.Fatalf("unexpected validation result = %#v", result)
				}
				return
			}
			if result == nil || !result.IsError || result.StructuredContent == nil || !strings.Contains(toolResultText(*result), test.field) {
				t.Fatalf("request validation result = %#v", result)
			}
		})
	}
}
