package mcp

import (
	"context"
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"

	appwayfinder "relay/internal/app/wayfinder"
	"relay/internal/mcp/routecontracts"
	"relay/internal/operations/registry"
	workflowstore "relay/internal/store/workflow"
)

func TestWayfinderAppSurfacePublishedActionsReachRealService(t *testing.T) {
	ctx := context.Background()
	server, store, artifactID := openWayfinderAppSurface(t, ctx)
	artifactSHA := strings.Repeat("b", 64)

	workspaceID := ""
	workspaceVersion := int64(0)
	ticketID := ""
	actions := []struct {
		name      string
		tool      string
		request   func() map[string]any
		assertion func(t *testing.T, result map[string]json.RawMessage)
	}{
		{
			name: "create workspace", tool: "wayfinder-workspace-v1__create_workspace",
			request: func() map[string]any {
				return map[string]any{"project_id": "project-test", "feature_slug": "feature-test"}
			},
			assertion: func(t *testing.T, result map[string]json.RawMessage) {
				var workspace workflowstore.FeatureWorkspace
				decodeWayfinderResult(t, result, "workspace", &workspace)
				if workspace.WorkspaceID == "" || workspace.Version != 1 {
					t.Fatalf("created workspace = %#v", workspace)
				}
				if workspace.ProjectRowID == 0 || workspace.FeatureSlug != "feature-test" {
					t.Fatalf("created workspace contract = %#v", workspace)
				}
				workspaceID, workspaceVersion = workspace.WorkspaceID, workspace.Version
			},
		},
		{
			name: "admit input", tool: "wayfinder-workspace-v1__admit_workspace_input",
			request: func() map[string]any {
				return map[string]any{"workspace_id": workspaceID, "expected_version": workspaceVersion, "sequence": 1, "name": "requirements", "role": "governing", "source_kind": "relay_artifact", "source_reference": "confirmed requirements", "artifact_row_id": artifactID, "artifact_sha256": artifactSHA}
			},
			assertion: func(t *testing.T, result map[string]json.RawMessage) {
				var workspace workflowstore.FeatureWorkspace
				decodeWayfinderResult(t, result, "workspace", &workspace)
				workspaceVersion = workspace.Version
			},
		},
		{
			name: "add destination", tool: "wayfinder-workspace-v1__add_workspace_destination",
			request: func() map[string]any {
				return map[string]any{"workspace_id": workspaceID, "expected_version": workspaceVersion, "sequence": 1, "kind": "destination", "key": "internal/app/wayfinder"}
			},
			assertion: func(t *testing.T, result map[string]json.RawMessage) {
				var workspace workflowstore.FeatureWorkspace
				decodeWayfinderResult(t, result, "workspace", &workspace)
				workspaceVersion = workspace.Version
			},
		},
		{
			name: "create ticket", tool: "wayfinder-discovery-v1__create_discovery_ticket",
			request: func() map[string]any {
				return map[string]any{"workspace_id": workspaceID, "expected_version": workspaceVersion, "ticket_key": "contract-drift", "subject": "Repair published contract", "depends_on_ticket_ids": []string{}, "dependency_kind": "informs"}
			},
			assertion: func(t *testing.T, result map[string]json.RawMessage) {
				var ticket workflowstore.FeatureWorkspaceDiscoveryTicket
				decodeWayfinderResult(t, result, "ticket", &ticket)
				var workspace workflowstore.FeatureWorkspace
				decodeWayfinderResult(t, result, "workspace", &workspace)
				if ticket.DiscoveryTicketID == "" || ticket.Version != 1 {
					t.Fatalf("created ticket = %#v", ticket)
				}
				ticketID, workspaceVersion = ticket.DiscoveryTicketID, workspace.Version
			},
		},
		{
			name: "read workspace", tool: "wayfinder-workspace-v1__read_workspace",
			request: func() map[string]any {
				return map[string]any{"workspace_id": workspaceID}
			},
			assertion: func(t *testing.T, result map[string]json.RawMessage) {
				var readback struct {
					Workspace struct {
						WorkspaceID string `json:"workspace_id"`
						ProjectID   string `json:"project_id"`
						Version     int64  `json:"version"`
					} `json:"workspace"`
					Tickets []struct {
						TicketID string `json:"ticket_id"`
						Version  int64  `json:"version"`
					} `json:"tickets"`
					Routes []struct {
						RouteStateID string `json:"route_state_id"`
					} `json:"routes"`
				}
				raw, err := json.Marshal(result)
				if err != nil {
					t.Fatal(err)
				}
				if err := json.Unmarshal(raw, &readback); err != nil {
					t.Fatal(err)
				}
				if readback.Workspace.WorkspaceID != workspaceID || readback.Workspace.ProjectID != "project-test" || readback.Workspace.Version != workspaceVersion || len(readback.Tickets) != 1 || readback.Tickets[0].TicketID != ticketID || len(readback.Routes) != 0 {
					t.Fatalf("workspace readback = %#v", readback)
				}
				encoded, err := json.Marshal(result)
				if err != nil {
					t.Fatal(err)
				}
				if strings.Contains(string(encoded), "row_id") || strings.Contains(string(encoded), "retained_artifact") || strings.Contains(string(encoded), "valid") {
					t.Fatalf("internal persistence identity leaked: %s", encoded)
				}
			},
		},
		{
			name: "attach investigation", tool: "wayfinder-investigation-v1__attach_investigation",
			request: func() map[string]any {
				return map[string]any{"workspace_id": workspaceID, "expected_version": workspaceVersion, "ticket_id": ticketID, "sequence": 1, "kind": "artifact", "artifact_row_id": artifactID, "artifact_sha256": artifactSHA}
			},
			assertion: func(t *testing.T, result map[string]json.RawMessage) {
				var workspace workflowstore.FeatureWorkspace
				decodeWayfinderResult(t, result, "workspace", &workspace)
				workspaceVersion = workspace.Version
			},
		},
		{
			name: "route workspace", tool: "wayfinder-workspace-v1__route_workspace",
			request: func() map[string]any {
				return map[string]any{"workspace_id": workspaceID, "expected_version": workspaceVersion, "sequence": 1, "state": "resolved", "ticket_id": ticketID}
			},
			assertion: func(t *testing.T, result map[string]json.RawMessage) {
				var workspace workflowstore.FeatureWorkspace
				decodeWayfinderResult(t, result, "workspace", &workspace)
				workspaceVersion = workspace.Version
			},
		},
	}

	for _, action := range actions {
		t.Run(action.name, func(t *testing.T) {
			response := coldStartCall(t, server, action.tool, action.request())
			if response.Error != nil {
				t.Fatalf("published request rejected: %v", response.Error)
			}
			action.assertion(t, decodeWayfinderToolResult(t, response))
			assertWayfinderRejectedSpellings(t, server, action.tool, action.request())
		})
	}

	workspace, err := store.GetFeatureWorkspaceByWorkspaceID(ctx, workspaceID)
	if err != nil || workspace.FeatureSlug != "feature-test" || workspace.Version != workspaceVersion {
		t.Fatalf("persisted workspace = %#v, %v", workspace, err)
	}
	if workspace.ProjectRowID == 0 {
		t.Fatalf("persisted workspace lacks project ownership: %#v", workspace)
	}
	detail, err := appwayfinder.NewService(store)
	if err != nil {
		t.Fatal(err)
	}
	persisted, err := detail.ReadWorkspace(ctx, workspaceID)
	if err != nil || len(persisted.Inputs) != 1 || len(persisted.Destinations) != 1 || len(persisted.Tickets) != 1 || len(persisted.Tickets[0].Resolutions) != 0 || len(persisted.Investigations) != 1 || len(persisted.Routes) != 1 {
		t.Fatalf("persisted action sequence = %#v, %v", persisted, err)
	}
}

func TestWayfinderCreateWorkspaceSchemaRejectsMissingRequiredProperties(t *testing.T) {
	server, _, _ := openWayfinderAppSurface(t, context.Background())
	for _, request := range []map[string]any{{"feature_slug": "feature-test"}, {"project_id": "project-test"}} {
		if response := coldStartCall(t, server, "wayfinder-workspace-v1__create_workspace", request); response.Error == nil {
			t.Fatalf("missing property request accepted: %#v", request)
		}
	}
}

func TestWayfinderContractDriftGuardPublishedSchemasMatchExplicitRuntimeWireTypes(t *testing.T) {
	contracts := []struct {
		tool string
		wire any
	}{
		{"read_workspace", readWorkspaceWireInput{}},
		{"create_workspace", createWorkspaceWireInput{}},
		{"admit_workspace_input", admitWorkspaceInputWireInput{}},
		{"add_workspace_destination", addWorkspaceDestinationWireInput{}},
		{"route_workspace", routeWorkspaceWireInput{}},
		{"create_discovery_ticket", createDiscoveryTicketWireInput{}},
		{"resolve_discovery_ticket", resolveDiscoveryTicketWireInput{}},
		{"attach_investigation", attachInvestigationWireInput{}},
	}
	for _, contract := range contracts {
		t.Run(contract.tool, func(t *testing.T) {
			published, ok := registry.LookupPublishedToolContract(contract.tool)
			if !ok {
				t.Fatal("published schema is missing")
			}
			var schema struct {
				Properties map[string]json.RawMessage `json:"properties"`
				Required   []string                   `json:"required"`
			}
			if err := json.Unmarshal(published.InputSchema, &schema); err != nil {
				t.Fatal(err)
			}
			wireFields, required := explicitWireFields(t, reflect.TypeOf(contract.wire))
			if !reflect.DeepEqual(sortedStringKeys(schema.Properties), sortedWireKeys(wireFields)) {
				t.Fatalf("published properties=%v runtime fields=%v", sortedStringKeys(schema.Properties), sortedWireKeys(wireFields))
			}
			sort.Strings(schema.Required)
			sort.Strings(required)
			if !reflect.DeepEqual(schema.Required, required) {
				t.Fatalf("published required=%v runtime required=%v", schema.Required, required)
			}
		})
	}
}

func TestWayfinderResolvePublishedOutputSchemaMatchesPublicDTO(t *testing.T) {
	published, ok := registry.LookupPublishedToolContract("resolve_discovery_ticket")
	if !ok {
		t.Fatal("published resolve schema is missing")
	}
	var schema struct {
		AdditionalProperties bool                       `json:"additionalProperties"`
		Properties           map[string]json.RawMessage `json:"properties"`
		Required             []string                   `json:"required"`
	}
	if err := json.Unmarshal(published.OutputSchema, &schema); err != nil {
		t.Fatal(err)
	}
	assertObject := func(raw json.RawMessage, name string, fields []string) {
		t.Helper()
		var object struct {
			AdditionalProperties bool                       `json:"additionalProperties"`
			Properties           map[string]json.RawMessage `json:"properties"`
			Required             []string                   `json:"required"`
		}
		if err := json.Unmarshal(raw, &object); err != nil {
			t.Fatalf("decode %s schema: %v", name, err)
		}
		if object.AdditionalProperties || !reflect.DeepEqual(sortedStringKeys(object.Properties), sortedWireKeys(func() map[string]struct{} {
			result := make(map[string]struct{}, len(fields))
			for _, field := range fields {
				result[field] = struct{}{}
			}
			return result
		}())) || !reflect.DeepEqual(sortedStringKeys(object.Properties), func() []string {
			values := append([]string(nil), object.Required...)
			sort.Strings(values)
			return values
		}()) {
			t.Fatalf("%s schema fields are not closed and exact: properties=%v required=%v", name, sortedStringKeys(object.Properties), object.Required)
		}
	}
	if schema.AdditionalProperties || !reflect.DeepEqual(sortedStringKeys(schema.Properties), []string{"resolution", "ticket", "workspace"}) || !reflect.DeepEqual(func() []string {
		values := append([]string(nil), schema.Required...)
		sort.Strings(values)
		return values
	}(), []string{"resolution", "ticket", "workspace"}) {
		t.Fatalf("resolve output schema is not closed and exact: properties=%v", sortedStringKeys(schema.Properties))
	}
	assertObject(schema.Properties["resolution"], "resolution", []string{"resolution_id", "sequence", "resolution_kind", "packet_id", "input_name", "artifact_sha256", "created_at"})
	assertObject(schema.Properties["ticket"], "ticket", []string{"discovery_ticket_id", "ticket_key", "subject", "state", "version", "created_at", "updated_at"})
	assertObject(schema.Properties["workspace"], "workspace", []string{"workspace_id", "feature_slug", "state", "version", "created_at", "updated_at"})
}

func explicitWireFields(t *testing.T, wire reflect.Type) (map[string]struct{}, []string) {
	t.Helper()
	fields := make(map[string]struct{}, wire.NumField())
	required := make([]string, 0, wire.NumField())
	for index := 0; index < wire.NumField(); index++ {
		field := wire.Field(index)
		tag, ok := field.Tag.Lookup("json")
		if !ok || tag == "" || tag == "-" {
			t.Fatalf("runtime wire field %s has no explicit JSON tag", field.Name)
		}
		parts := strings.Split(tag, ",")
		if parts[0] == "" {
			t.Fatalf("runtime wire field %s has an empty JSON name", field.Name)
		}
		fields[parts[0]] = struct{}{}
		if len(parts) == 1 || parts[1] != "omitempty" {
			required = append(required, parts[0])
		}
	}
	return fields, required
}

func sortedStringKeys(values map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sortedWireKeys(values map[string]struct{}) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func assertWayfinderRejectedSpellings(t *testing.T, server *Server, tool string, request map[string]any) {
	t.Helper()
	camelCase, goField := "workspaceId", "WorkspaceID"
	if tool == "wayfinder-workspace-v1__create_workspace" {
		camelCase, goField = "projectId", "ProjectID"
	}
	spellings := []string{"unknown_property", camelCase, goField}
	if tool == "wayfinder-workspace-v1__create_workspace" {
		spellings = append(spellings, "featureSlug", "FeatureSlug")
	}
	for _, spelling := range spellings {
		invalid := cloneWayfinderRequest(request)
		invalid[spelling] = true
		if response := coldStartCall(t, server, tool, invalid); response.Error == nil {
			t.Fatalf("%s spelling accepted: %#v", spelling, invalid)
		}
	}
}

func cloneWayfinderRequest(request map[string]any) map[string]any {
	copy := make(map[string]any, len(request)+1)
	for key, value := range request {
		copy[key] = value
	}
	return copy
}

func decodeWayfinderToolResult(t *testing.T, response Response) map[string]json.RawMessage {
	t.Helper()
	var result ToolCallResult
	raw, err := json.Marshal(response.Result)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &result); err != nil || result.IsError || len(result.Content) != 1 {
		t.Fatalf("tool result = %#v, %v", result, err)
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal([]byte(result.Content[0].Text), &payload); err != nil {
		t.Fatal(err)
	}
	return payload
}

func decodeWayfinderResult(t *testing.T, result map[string]json.RawMessage, key string, target any) {
	t.Helper()
	if err := json.Unmarshal(result[key], target); err != nil {
		t.Fatalf("decode %s: %v", key, err)
	}
}

func openWayfinderAppSurface(t *testing.T, ctx context.Context) (*Server, *workflowstore.Store, int64) {
	t.Helper()
	store := coldStartStore(t, t.TempDir(), false)
	t.Cleanup(func() { _ = store.Close() })
	var projectRowID, planRowID, artifactRowID int64
	if err := store.DB().QueryRowContext(ctx, `INSERT INTO projects (project_id, name) VALUES ('project-test', 'Wayfinder test') RETURNING id`).Scan(&projectRowID); err != nil {
		t.Fatal(err)
	}
	if err := store.DB().QueryRowContext(ctx, `INSERT INTO plans (project_row_id, plan_id, feature_slug, canonical_sha256) VALUES (?, 'plan-test', 'feature-test', ?) RETURNING id`, projectRowID, strings.Repeat("a", 64)).Scan(&planRowID); err != nil {
		t.Fatal(err)
	}
	if err := store.DB().QueryRowContext(ctx, `INSERT INTO artifacts (artifact_id, owner_type, plan_row_id, kind, relative_path, media_type, sha256, size_bytes) VALUES ('artifact-test', 'plan', ?, 'requirements', 'plans/feature/requirements.json', 'application/json', ?, 2) RETURNING id`, planRowID, strings.Repeat("b", 64)).Scan(&artifactRowID); err != nil {
		t.Fatal(err)
	}
	wayfinder, err := appwayfinder.NewService(store)
	if err != nil {
		t.Fatal(err)
	}
	routes, err := routecontracts.BuildMCPRouteManifests()
	if err != nil {
		t.Fatal(err)
	}
	dispatchers, err := NewRouteDispatchers(routes, RouteDispatchServices{Wayfinder: wayfinder})
	if err != nil {
		t.Fatal(err)
	}
	surfaces, err := routecontracts.BuildAppSurfaceManifests(routes)
	if err != nil {
		t.Fatal(err)
	}
	for _, surface := range surfaces.Surfaces {
		if surface.Surface == routecontracts.AppSurfaceWayfinder {
			registrations, err := BuildAppSurfaceHandlers(surface, dispatchers)
			if err != nil {
				t.Fatal(err)
			}
			server, err := NewServerForAppSurface(nil, surface, registrations)
			if err != nil {
				t.Fatal(err)
			}
			return server, store, artifactRowID
		}
	}
	t.Fatal("Wayfinder app surface is missing")
	return nil, nil, 0
}
