package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"relay/internal/mcp/routecontracts"
	"relay/internal/operations/registry"
)

func TestRouteServerUsesOnlyExactRouteHandlers(t *testing.T) {
	set, err := routecontracts.BuildMCPRouteManifests()
	if err != nil {
		t.Fatal(err)
	}
	manifest := set.Manifests[3]
	handlers := make([]ToolHandler, len(manifest.Tools))
	for i, tool := range manifest.Tools {
		name := tool.Name
		handlers[i] = ToolHandler{Name: name, Handle: func(json.RawMessage) ToolCallResult { return workflowOK(map[string]any{"tool": name}) }}
	}
	server, err := NewServerForRoute(nil, manifest, handlers)
	if err != nil {
		t.Fatal(err)
	}
	if len(server.tools) != len(manifest.Tools) {
		t.Fatalf("tools=%d", len(server.tools))
	}
	if server.toolRegistered("record_audit_decision") {
		t.Fatal("cross-route tool registered")
	}
	response := server.handleToolsCall(Request{ID: json.RawMessage("1"), Params: json.RawMessage(`{"name":"record_audit_decision","arguments":{}}`)})
	if response.Error == nil || response.Error.Code != CodeMethodNotFound {
		t.Fatalf("response=%#v", response)
	}
}

func TestAppSurfaceServersListUniqueAliasesAndDispatchToBoundRoutes(t *testing.T) {
	routes, err := routecontracts.BuildMCPRouteManifests()
	if err != nil {
		t.Fatal(err)
	}
	surfaces, err := routecontracts.BuildAppSurfaceManifests(routes)
	if err != nil {
		t.Fatal(err)
	}
	owners := fakeAppSurfaceDispatchers(routes)
	servers := make(map[routecontracts.AppSurface]*Server, len(surfaces.Surfaces))
	registrationsBySurface := make(map[routecontracts.AppSurface][]AppToolRegistration, len(surfaces.Surfaces))

	for _, surface := range surfaces.Surfaces {
		registrations, err := BuildAppSurfaceHandlers(surface, owners)
		if err != nil {
			t.Fatal(err)
		}
		server, err := NewServerForAppSurface(nil, surface, registrations)
		if err != nil {
			t.Fatal(err)
		}
		servers[surface.Surface] = server
		registrationsBySurface[surface.Surface] = registrations

		list := listTools(t, server, ToolsListParams{})
		if list.NextCursor != "" {
			t.Fatalf("%s complete catalog next cursor=%q", surface.Surface, list.NextCursor)
		}
		if len(list.Tools) != len(surface.Tools) {
			t.Fatalf("%s catalog tools=%d, want %d", surface.Surface, len(list.Tools), len(surface.Tools))
		}
		seen := make(map[string]struct{}, len(list.Tools))
		for index, definition := range list.Tools {
			registration := registrations[index]
			if definition.Name != registration.AdvertisedName {
				t.Fatalf("%s tool[%d]=%q, want %q", surface.Surface, index, definition.Name, registration.AdvertisedName)
			}
			if _, duplicate := seen[definition.Name]; duplicate {
				t.Fatalf("%s duplicate public tool %q", surface.Surface, definition.Name)
			}
			seen[definition.Name] = struct{}{}
			assertAppToolMetadata(t, definition, registration)
		}

		for index, definition := range server.tools {
			assertAppToolMetadata(t, definition, registrations[index])
		}
		filtered := listTools(t, server, ToolsListParams{Query: "list"})
		if filtered.NextCursor != "" {
			t.Fatalf("%s filtered catalog next cursor=%q", surface.Surface, filtered.NextCursor)
		}
		for _, definition := range filtered.Tools {
			if !strings.Contains(strings.ToLower(definition.Name), "list") && !strings.Contains(strings.ToLower(definition.Description), "list") {
				t.Fatalf("%s filtered tool %q does not match query", surface.Surface, definition.Name)
			}
		}
		assertToolFamilies(t, surface.Surface, list.Tools)

		if surface.Surface == routecontracts.AppSurfaceWayfinder {
			invalid := append([]AppToolRegistration(nil), registrations...)
			invalid[0].StandingAuthority.Path += ".unexpected"
			if _, err := NewServerForAppSurface(nil, surface, invalid); err == nil {
				t.Fatal("server accepted a standing-authority mismatch")
			}
		}
	}

	collisions := appToolRegistrationsByInternalName(registrationsBySurface[routecontracts.AppSurfaceWayfinder], "list_projects")
	if len(collisions) != 3 {
		t.Fatalf("wayfinder list_projects aliases=%d", len(collisions))
	}
	for surface, server := range servers {
		registrations := registrationsBySurface[surface]
		representatives := representativeAppToolsByRoute(registrations)
		if len(representatives) != appRouteCount(registrations) {
			t.Fatalf("%s representative routes=%d, want %d", surface, len(representatives), appRouteCount(registrations))
		}
		for _, registration := range representatives {
			response := callAppSurfaceTool(t, server, registration.AdvertisedName, registration.SurfaceContract)
			if response.Error != nil {
				t.Fatalf("%s response=%#v", registration.AdvertisedName, response.Error)
			}
			var result ToolCallResult
			body, err := json.Marshal(response.Result)
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(body, &result); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(toolResultText(result), registration.InternalRoutePath) {
				t.Fatalf("%s did not reach bound route %s: %s", registration.AdvertisedName, registration.InternalRoutePath, toolResultText(result))
			}
		}
	}
	for _, name := range []string{"list_projects", "planner-authoring-v1__list_projects"} {
		response := callAppSurfaceTool(t, servers[routecontracts.AppSurfaceWayfinder], name, "wayfinder-workspace.v1")
		if response.Error == nil || response.Error.Code != CodeMethodNotFound {
			t.Fatalf("unqualified or cross-role %q response=%#v", name, response)
		}
	}
}

func TestAppSurfaceRouteBoundDispatchPassesRegisteredSurfaceContractToHandler(t *testing.T) {
	routes, err := routecontracts.BuildMCPRouteManifests()
	if err != nil {
		t.Fatal(err)
	}
	surfaces, err := routecontracts.BuildAppSurfaceManifests(routes)
	if err != nil {
		t.Fatal(err)
	}
	owners := fakeAppSurfaceDispatchers(routes)
	var surface routecontracts.AppSurfaceManifest
	for _, candidate := range surfaces.Surfaces {
		if candidate.Surface == routecontracts.AppSurfaceWayfinder {
			surface = candidate
			break
		}
	}
	if surface.Surface == "" {
		t.Fatal("Wayfinder app surface is missing")
	}
	registrations, err := BuildAppSurfaceHandlers(surface, owners)
	if err != nil {
		t.Fatal(err)
	}
	var received []json.RawMessage
	var selected AppToolRegistration
	for index := range registrations {
		if registrations[index].InternalToolName != "list_projects" {
			continue
		}
		selected = registrations[index]
		registrations[index].Handler.Handle = func(raw json.RawMessage) ToolCallResult {
			received = append(received, append(json.RawMessage(nil), raw...))
			return workflowOK(map[string]string{"status": "received"})
		}
		break
	}
	if selected.AdvertisedName == "" {
		t.Fatal("Wayfinder list_projects registration is missing")
	}
	server, err := NewServerForAppSurface(nil, surface, registrations)
	if err != nil {
		t.Fatal(err)
	}

	matchingSurface, err := json.Marshal(selected.SurfaceContract)
	if err != nil {
		t.Fatal(err)
	}
	for _, arguments := range []json.RawMessage{json.RawMessage(`{}`), json.RawMessage(`{"surface_contract":` + string(matchingSurface) + `}`)} {
		if _, err := server.dispatchSurfaceTool(selected.AdvertisedName, arguments); err != nil {
			t.Fatalf("dispatch %s: %v", arguments, err)
		}
	}
	if len(received) != 2 {
		t.Fatalf("handler calls=%d, want 2", len(received))
	}
	for _, arguments := range received {
		var request map[string]json.RawMessage
		if err := json.Unmarshal(arguments, &request); err != nil {
			t.Fatal(err)
		}
		var contract registry.SurfaceContractID
		if err := json.Unmarshal(request["surface_contract"], &contract); err != nil {
			t.Fatal(err)
		}
		if contract != registry.SurfaceContractID(selected.SurfaceContract) {
			t.Fatalf("handler surface_contract=%q, want %q", contract, selected.SurfaceContract)
		}
		if err := registry.ValidateOperationRequest(contract, selected.InternalToolName, arguments); err != nil {
			t.Fatalf("handler arguments are not valid for mounted route: %v", err)
		}
	}
}

func TestAppSurfaceServerRejectsSemanticAndOperationRegistrationMismatch(t *testing.T) {
	routes, err := routecontracts.BuildMCPRouteManifests()
	if err != nil {
		t.Fatal(err)
	}
	surfaces, err := routecontracts.BuildAppSurfaceManifests(routes)
	if err != nil {
		t.Fatal(err)
	}
	owners := fakeAppSurfaceDispatchers(routes)
	var surface routecontracts.AppSurfaceManifest
	for _, candidate := range surfaces.Surfaces {
		if candidate.Surface == routecontracts.AppSurfaceWayfinder {
			surface = candidate
			break
		}
	}
	if surface.Surface == "" {
		t.Fatal("Wayfinder app surface is missing")
	}
	registrations, err := BuildAppSurfaceHandlers(surface, owners)
	if err != nil {
		t.Fatal(err)
	}
	if len(registrations) == 0 {
		t.Fatal("no app surface registrations")
	}
	for name, mutate := range map[string]func(*routecontracts.ToolManifest){
		"semantic": func(tool *routecontracts.ToolManifest) { tool.SemanticToolID = "relay.unexpected.tool.v1" },
		"operation": func(tool *routecontracts.ToolManifest) { tool.OperationID = "planner.requirements" },
	} {
		invalid := append([]AppToolRegistration(nil), registrations...)
		mutate(&invalid[0].Tool)
		if _, err := NewServerForAppSurface(nil, surface, invalid); err == nil {
			t.Fatalf("server accepted %s registration mismatch", name)
		}
	}
	server, err := NewServerForAppSurface(nil, surface, registrations)
	if err != nil {
		t.Fatal(err)
	}
	for index, definition := range server.tools {
		compiled := surface.Tools[index]
		if definition.Meta["relay/semanticToolID"] != compiled.SemanticToolID || definition.Meta["relay/operationID"] != compiled.OperationID {
			t.Fatalf("%s metadata does not equal compiled identity %q/%q", definition.Name, compiled.SemanticToolID, compiled.OperationID)
		}
	}
}

func TestAppSurfaceActionDispatchPreservesClientArguments(t *testing.T) {
	routes, err := routecontracts.BuildMCPRouteManifests()
	if err != nil {
		t.Fatal(err)
	}
	surfaces, err := routecontracts.BuildAppSurfaceManifests(routes)
	if err != nil {
		t.Fatal(err)
	}
	owners := fakeAppSurfaceDispatchers(routes)
	var surface routecontracts.AppSurfaceManifest
	for _, candidate := range surfaces.Surfaces {
		if candidate.Surface == routecontracts.AppSurfaceWayfinder {
			surface = candidate
			break
		}
	}
	registrations, err := BuildAppSurfaceHandlers(surface, owners)
	if err != nil {
		t.Fatal(err)
	}
	var received json.RawMessage
	var selected AppToolRegistration
	for index := range registrations {
		if registrations[index].InternalToolName != "create_workspace" || registrations[index].SurfaceContract != "wayfinder-workspace.v1" {
			continue
		}
		selected = registrations[index]
		registrations[index].Handler.Handle = func(raw json.RawMessage) ToolCallResult {
			received = append(json.RawMessage(nil), raw...)
			return workflowOK(map[string]any{"workspace": map[string]string{"workspace_id": "workspace-test"}})
		}
		break
	}
	if selected.AdvertisedName == "" {
		t.Fatal("Wayfinder create_workspace registration is missing")
	}
	server, err := NewServerForAppSurface(nil, surface, registrations)
	if err != nil {
		t.Fatal(err)
	}
	arguments := json.RawMessage(`{"project_id":"project-test","feature_slug":"feature-test"}`)
	result, err := server.dispatchSurfaceTool(selected.AdvertisedName, arguments)
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError || len(result.Content) == 0 || !strings.Contains(result.Content[0].Text, "workspace-test") {
		t.Fatalf("successful workspace result = %#v", result)
	}
	if string(received) != string(arguments) {
		t.Fatalf("handler arguments = %s, want %s", received, arguments)
	}
	if _, err := server.dispatchSurfaceTool(selected.AdvertisedName, json.RawMessage(`{"project_id":"project-test","feature_slug":"feature-test","surface_contract":"wayfinder-workspace.v1"}`)); err == nil {
		t.Fatal("action schema accepted surface_contract")
	}
}

func TestPlannerPublicToolsListPublishesDeliveryTicketSchema(t *testing.T) {
	routes, err := routecontracts.BuildMCPRouteManifests()
	if err != nil {
		t.Fatal(err)
	}
	surfaces, err := routecontracts.BuildAppSurfaceManifests(routes)
	if err != nil {
		t.Fatal(err)
	}
	var planner routecontracts.AppSurfaceManifest
	for _, surface := range surfaces.Surfaces {
		if surface.Surface == routecontracts.AppSurfacePlanner {
			planner = surface
			break
		}
	}
	if planner.Surface == "" {
		t.Fatal("planner app surface is missing")
	}

	registrations, err := BuildAppSurfaceHandlers(planner, fakeAppSurfaceDispatchers(routes))
	if err != nil {
		t.Fatal(err)
	}
	server, err := NewServerForAppSurface(nil, planner, registrations)
	if err != nil {
		t.Fatal(err)
	}

	const advertisedName = "planner-authoring-v1__create_operation_packet"
	var registration AppToolRegistration
	for _, candidate := range registrations {
		if candidate.AdvertisedName == advertisedName {
			registration = candidate
			break
		}
	}
	if registration.AdvertisedName == "" || registration.InternalToolName != "create_operation_packet" {
		t.Fatalf("planner delivery-ticket registration = %#v", registration)
	}

	var routeTool routecontracts.ToolManifest
	foundRoute := false
	for _, manifest := range routes.Manifests {
		if manifest.RoutePath != registration.InternalRoutePath {
			continue
		}
		for _, tool := range manifest.Tools {
			if tool.Name == registration.InternalToolName {
				routeTool = tool
				foundRoute = true
				break
			}
		}
	}
	if !foundRoute {
		t.Fatalf("route manifest tool %s/%s is missing", registration.InternalRoutePath, registration.InternalToolName)
	}

	response := server.handleLine([]byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`))
	if response.Error != nil {
		t.Fatalf("tools/list response error = %#v", response.Error)
	}
	encoded, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	var wire struct {
		JSONRPC string `json:"jsonrpc"`
		Result  struct {
			Tools []struct {
				Name        string          `json:"name"`
				InputSchema json.RawMessage `json:"inputSchema"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(encoded, &wire); err != nil {
		t.Fatalf("decode tools/list response: %v; json=%s", err, encoded)
	}
	if wire.JSONRPC != JSONRPCVersion {
		t.Fatalf("jsonrpc version = %q", wire.JSONRPC)
	}
	var advertised struct {
		Name        string          `json:"name"`
		InputSchema json.RawMessage `json:"inputSchema"`
	}
	for _, tool := range wire.Result.Tools {
		if tool.Name == advertisedName {
			advertised = tool
			break
		}
	}
	if advertised.Name == "" {
		t.Fatalf("tools/list did not advertise %q", advertisedName)
	}
	if !bytes.Equal(advertised.InputSchema, routeTool.InputSchema) {
		t.Fatalf("advertised input schema differs from mounted route schema: advertised=%s route=%s", advertised.InputSchema, routeTool.InputSchema)
	}

	var schema map[string]any
	if err := json.Unmarshal(advertised.InputSchema, &schema); err != nil {
		t.Fatal(err)
	}
	if schemaHasUnconstrainedObjectFallback(schema) {
		t.Fatal("advertised input schema contains an unconstrained object/additionalProperties-only fallback")
	}
	defs := plannerSchemaObject(t, schema, "$defs")
	admission := plannerSchemaObject(t, defs, "OperationAdmission")
	delivery := plannerSchemaBranchByOperation(t, plannerSchemaArray(t, admission, "oneOf"), "planner.delivery_ticket")
	properties := plannerSchemaObject(t, delivery, "properties")
	linkedDelivery := plannerSchemaBranchByOperation(t, plannerSchemaArray(t, schema, "oneOf"), "planner.delivery_ticket")
	linkedProperties := plannerSchemaObject(t, linkedDelivery, "properties")

	requiredInputs := plannerSchemaObject(t, properties, "required_inputs")
	requiredSlot := plannerSchemaOneOfSlot(t, plannerSchemaObject(t, requiredInputs, "items"))
	requiredProperties := plannerSchemaObject(t, requiredSlot, "properties")
	if got := plannerSchemaConst(t, requiredProperties, "input_name"); got != "confirmed_delivery_boundary" {
		t.Fatalf("required delivery-ticket input = %q", got)
	}
	if got := plannerSchemaStringValues(t, requiredProperties, "allowed_source_kinds"); !samePlannerSchemaStrings(got, []string{"inline_text"}) {
		t.Fatalf("delivery-ticket source kinds = %#v", got)
	}

	workflowReferences := plannerSchemaObject(t, properties, "workflow_reference_kinds")
	if got := plannerSchemaStringValues(t, workflowReferences, "items"); !samePlannerSchemaStrings(got, []string{"feature_workspace"}) {
		t.Fatalf("delivery-ticket workflow references = %#v", got)
	}

	callerInputs := plannerSchemaObject(t, linkedProperties, "inputs")
	callerItem := plannerSchemaObject(t, callerInputs, "items")
	for _, candidate := range plannerSchemaArray(t, callerItem, "oneOf") {
		branch, ok := candidate.(map[string]any)
		if !ok {
			t.Fatalf("caller input branch is not an object: %#v", candidate)
		}
		slot := branch
		allOf := plannerSchemaArray(t, slot, "allOf")
		if len(allOf) != 2 {
			t.Fatalf("caller input allOf = %#v", slot)
		}
		constrainedSlot, ok := allOf[1].(map[string]any)
		if !ok {
			t.Fatalf("caller input constrained slot is not an object: %#v", allOf[1])
		}
		slotProperties := plannerSchemaObject(t, constrainedSlot, "properties")
		if plannerSchemaConst(t, slotProperties, "input_name") == "current_feature_workspace_route" {
			t.Fatal("derived current_feature_workspace_route is exposed as a caller input")
		}
		if containsPlannerSchemaString(plannerSchemaStringValues(t, slotProperties, "source_kind"), "committed_source") {
			t.Fatal("committed_source is exposed for planner.delivery_ticket caller inputs")
		}
	}
}

func plannerSchemaObject(t *testing.T, value map[string]any, key string) map[string]any {
	t.Helper()
	child, ok := value[key].(map[string]any)
	if !ok {
		t.Fatalf("schema member %q is not an object", key)
	}
	return child
}

func plannerSchemaArray(t *testing.T, value map[string]any, key string) []any {
	t.Helper()
	child, ok := value[key].([]any)
	if !ok {
		t.Fatalf("schema member %q is not an array", key)
	}
	return child
}

func plannerSchemaConst(t *testing.T, properties map[string]any, key string) string {
	t.Helper()
	return plannerSchemaObject(t, properties, key)["const"].(string)
}

func plannerSchemaOneOfSlot(t *testing.T, value map[string]any) map[string]any {
	if branches, ok := value["oneOf"].([]any); ok && len(branches) == 1 {
		branch, ok := branches[0].(map[string]any)
		if !ok {
			t.Fatalf("schema oneOf slot is not an object: %#v", value)
		}
		return branch
	}
	return value
}

func plannerSchemaBranchByOperation(t *testing.T, branches []any, operation string) map[string]any {
	t.Helper()
	for _, candidate := range branches {
		branch, ok := candidate.(map[string]any)
		if !ok {
			t.Fatalf("operation branch is not an object: %#v", candidate)
		}
		properties, ok := branch["properties"].(map[string]any)
		if !ok {
			continue
		}
		if operationSchema, ok := properties["operation_id"].(map[string]any); ok && operationSchema["const"] == operation {
			return branch
		}
	}
	if operation == "planner.delivery_ticket" {
		t.Fatalf("operation branch %q is missing", operation)
	}
	return nil
}

func plannerSchemaStringValues(t *testing.T, properties map[string]any, key string) []string {
	t.Helper()
	property := plannerSchemaObject(t, properties, key)
	if items, ok := property["items"].(map[string]any); ok {
		property = items
	}
	if value, ok := property["enum"].([]any); ok {
		result := make([]string, 0, len(value))
		for _, item := range value {
			text, ok := item.(string)
			if !ok {
				t.Fatalf("schema member %q enum value is not a string: %#v", key, item)
			}
			result = append(result, text)
		}
		return result
	}
	if value, ok := property["const"].(string); ok {
		return []string{value}
	}
	t.Fatalf("schema member %q has neither string enum nor const", key)
	return nil
}

func samePlannerSchemaStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for index := range got {
		if got[index] != want[index] {
			return false
		}
	}
	return true
}

func containsPlannerSchemaString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func schemaHasUnconstrainedObjectFallback(value any) bool {
	object, ok := value.(map[string]any)
	if !ok || object["type"] != "object" || hasPlannerSchemaConstraint(object) {
		return false
	}
	additional, exists := object["additionalProperties"]
	return !exists || additional == true
}

func hasPlannerSchemaConstraint(value map[string]any) bool {
	for _, key := range []string{"properties", "required", "oneOf", "allOf", "anyOf", "not", "$ref", "patternProperties", "dependentSchemas"} {
		if _, ok := value[key]; ok {
			return true
		}
	}
	return false
}

func appRouteCount(registrations []AppToolRegistration) int {
	routes := make(map[string]struct{})
	for _, registration := range registrations {
		routes[registration.InternalRoutePath] = struct{}{}
	}
	return len(routes)
}

func assertToolFamilies(t *testing.T, surface routecontracts.AppSurface, tools []ToolDefinition) {
	t.Helper()
	want := map[routecontracts.AppSurface][]string{
		routecontracts.AppSurfaceWayfinder: {"wayfinder-workspace-v1__", "wayfinder-discovery-v1__", "wayfinder-investigation-v1__"},
		routecontracts.AppSurfacePlanner:   {"planner-authoring-v1__", "planner-ticket-frontier-v1__"},
		routecontracts.AppSurfaceAuditor:   {"auditor-review-v1__", "auditor-audit-v1__"},
	}[surface]
	for _, prefix := range want {
		found := false
		for _, tool := range tools {
			if strings.HasPrefix(tool.Name, prefix) {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("%s catalog missing %q tools", surface, prefix)
		}
	}
}

func listTools(t *testing.T, server *Server, params ToolsListParams) ToolsListResult {
	t.Helper()
	rawParams, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	response := server.handleToolsList(Request{ID: json.RawMessage(`1`), Params: rawParams})
	if response.Error != nil {
		t.Fatal(response.Error)
	}
	encoded, err := json.Marshal(response.Result)
	if err != nil {
		t.Fatal(err)
	}
	var result ToolsListResult
	if err := json.Unmarshal(encoded, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func fakeAppSurfaceDispatchers(routes routecontracts.RouteSet) RouteDispatchers {
	owners := RouteDispatchers{Handlers: make(map[string]map[string]SurfaceHandler, len(routes.Manifests))}
	for _, route := range routes.Manifests {
		byTool := make(map[string]SurfaceHandler, len(route.Tools))
		for _, tool := range route.Tools {
			routePath, toolName, surfaceContract := route.RoutePath, tool.Name, route.SurfaceContract
			byTool[toolName] = func(raw json.RawMessage) ToolCallResult {
				var args map[string]json.RawMessage
				if err := json.Unmarshal(raw, &args); err != nil {
					return toolErr(err.Error())
				}
				var received string
				if err := json.Unmarshal(args["surface_contract"], &received); err != nil || received != surfaceContract {
					return toolErr("route authority does not match mounted route")
				}
				return workflowOK(map[string]string{"route_path": routePath, "tool": toolName})
			}
		}
		owners.Handlers[route.RoutePath] = byTool
	}
	return owners
}

func assertAppToolMetadata(t *testing.T, definition ToolDefinition, registration AppToolRegistration) {
	t.Helper()
	want := map[string]string{
		"relay/publicAppSurface":            string(registration.PublicSurface),
		"relay/publicAdvertisedToolName":    registration.AdvertisedName,
		"relay/internalToolName":            registration.InternalToolName,
		"relay/routePath":                   registration.InternalRoutePath,
		"relay/surfaceContract":             registration.SurfaceContract,
		"relay/routeManifestSHA256":         registration.RouteManifestSHA256,
		"relay/standingAuthorityRepository": registration.StandingAuthority.Repository,
		"relay/standingAuthorityCommitOID":  registration.StandingAuthority.Commit,
		"relay/standingAuthorityPath":       registration.StandingAuthority.Path,
		"relay/standingAuthorityBlobOID":    registration.StandingAuthority.BlobOID,
	}
	for key, expected := range want {
		if actual, ok := definition.Meta[key].(string); !ok || actual != expected {
			t.Fatalf("%s metadata %s=%#v, want %q", definition.Name, key, definition.Meta[key], expected)
		}
	}
	if len(definition.orderedMeta) == 0 {
		return
	}
	var ordered map[string]any
	if err := json.Unmarshal(definition.orderedMeta, &ordered); err != nil {
		t.Fatal(err)
	}
	for key, expected := range want {
		if actual, ok := ordered[key].(string); !ok || actual != expected {
			t.Fatalf("%s ordered metadata %s=%#v, want %q", definition.Name, key, ordered[key], expected)
		}
	}
}

func appToolRegistrationsByInternalName(registrations []AppToolRegistration, internalName string) []AppToolRegistration {
	result := make([]AppToolRegistration, 0)
	for _, registration := range registrations {
		if registration.InternalToolName == internalName {
			result = append(result, registration)
		}
	}
	return result
}

func representativeAppToolsByRoute(registrations []AppToolRegistration) []AppToolRegistration {
	byRoute := make(map[string]AppToolRegistration)
	for _, registration := range registrations {
		if registration.InternalToolName == "list_projects" {
			byRoute[registration.InternalRoutePath] = registration
		}
	}
	result := make([]AppToolRegistration, 0, len(byRoute))
	for _, registration := range registrations {
		if representative, exists := byRoute[registration.InternalRoutePath]; exists && representative.AdvertisedName == registration.AdvertisedName {
			result = append(result, registration)
			delete(byRoute, registration.InternalRoutePath)
		}
	}
	return result
}

func callAppSurfaceTool(t *testing.T, server *Server, name, surfaceContract string) Response {
	t.Helper()
	arguments, err := json.Marshal(map[string]string{"surface_contract": surfaceContract})
	if err != nil {
		t.Fatal(err)
	}
	params, err := json.Marshal(ToolCallParams{Name: name, Arguments: arguments})
	if err != nil {
		t.Fatal(err)
	}
	return server.handleToolsCall(Request{ID: json.RawMessage(fmt.Sprintf("%q", name)), Params: params})
}
