package routecontracts

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"relay/internal/operations/registry"
)

func TestSharedPacketRouteContractsAreBoundToTheirMountedRoute(t *testing.T) {
	set, err := BuildMCPRouteManifests()
	if err != nil {
		t.Fatal(err)
	}
	for _, manifest := range set.Manifests {
		for _, tool := range manifest.Tools {
			if !isSharedPacketToolForTest(tool.Name) {
				continue
			}
			if contract, ok := registry.LookupPublishedToolContract(tool.Name); !ok || contract.MetadataSource == "legacy_exact_copy" {
				t.Fatalf("%s still uses legacy packet contract metadata", tool.Name)
			}
			if !strings.Contains(string(tool.InputSchema), `"surface_contract":{"const":"`+manifest.SurfaceContract+`","type"`) {
				t.Fatalf("%s/%s input schema is not route-bound", manifest.RoutePath, tool.Name)
			}
		}
	}
}

func isSharedPacketToolForTest(name string) bool {
	switch name {
	case "list_projects", "get_active_operation_packet", "create_operation_packet", "refresh_operation_packet", "close_operation_packet", "read_operation_input", "list_operation_repositories":
		return true
	default:
		return false
	}
}

func TestPlannerDeliveryTicketAdmissionIsDiscoverableOnPublicCreatePacketContract(t *testing.T) {
	set, err := BuildMCPRouteManifests()
	if err != nil {
		t.Fatal(err)
	}
	var planner RouteManifest
	for _, manifest := range set.Manifests {
		if manifest.SurfaceContract == "planner-authoring.v1" {
			planner = manifest
			break
		}
	}
	if planner.SurfaceContract == "" {
		t.Fatal("planner authoring route is missing")
	}
	var tool *ToolManifest
	for index := range planner.Tools {
		if planner.Tools[index].Name == "create_operation_packet" {
			tool = &planner.Tools[index]
			break
		}
	}
	if tool == nil {
		t.Fatal("create_operation_packet is missing from planner authoring route")
	}

	var schema map[string]any
	if err := json.Unmarshal(tool.InputSchema, &schema); err != nil {
		t.Fatal(err)
	}
	if id, _ := schema["$id"].(string); !strings.HasSuffix(id, ":input:v1") {
		t.Fatalf("route-bound create schema identity = %q", id)
	}
	defs := schemaObject(t, schema, "$defs")
	admission := schemaObject(t, defs, "OperationAdmission")
	branches := schemaArray(t, admission, "oneOf")
	var delivery map[string]any
	for _, candidate := range branches {
		branch, ok := candidate.(map[string]any)
		if !ok {
			t.Fatal("operation admission branch is not an object")
		}
		properties := schemaObject(t, branch, "properties")
		operationID := schemaObject(t, properties, "operation_id")
		if operationID["const"] == "planner.delivery_ticket" {
			delivery = branch
			break
		}
	}
	if delivery == nil {
		t.Fatal("planner.delivery_ticket admission is not published")
	}
	linkedBranches := schemaArray(t, schema, "oneOf")
	var linkedDelivery map[string]any
	for _, candidate := range linkedBranches {
		branch, ok := candidate.(map[string]any)
		if !ok {
			t.Fatal("linked operation branch is not an object")
		}
		properties := schemaObject(t, branch, "properties")
		if schemaConst(t, properties, "operation_id") == "planner.delivery_ticket" {
			linkedDelivery = branch
			break
		}
	}
	if linkedDelivery == nil {
		t.Fatal("create request is not linked to planner.delivery_ticket admission")
	}
	linkedInputs := schemaObject(t, schemaObject(t, linkedDelivery, "properties"), "inputs")
	if linkedInputs["minItems"] != float64(1) || linkedInputs["maxItems"] != float64(4) {
		t.Fatalf("linked caller input cardinality = %#v", linkedInputs)
	}
	properties := schemaObject(t, delivery, "properties")
	cardinality := schemaObject(t, properties, "caller_input_cardinality")
	cardinalityProperties := schemaObject(t, cardinality, "properties")
	minimum, ok := cardinalityProperties["minimum"].(map[string]any)
	if !ok || fmt.Sprint(minimum["const"]) != "1" {
		t.Fatalf("caller minimum cardinality = %#v", cardinality)
	}
	maximum, ok := cardinalityProperties["maximum"].(map[string]any)
	if !ok || fmt.Sprint(maximum["const"]) != "4" {
		t.Fatalf("caller maximum cardinality = %#v", cardinality)
	}
	callerInputs := schemaObject(t, properties, "caller_inputs")
	if callerInputs["minItems"] != float64(1) || callerInputs["maxItems"] != float64(4) {
		t.Fatalf("caller input representation cardinality = %#v", callerInputs)
	}

	requiredSchema := schemaObject(t, properties, "required_inputs")
	if requiredSchema["minItems"] != float64(1) || requiredSchema["maxItems"] != float64(1) {
		t.Fatalf("required input cardinality = %#v", requiredSchema)
	}
	requiredSlot := schemaOneOfSlot(t, schemaObject(t, requiredSchema, "items"))
	requiredSlotProperties := schemaObject(t, requiredSlot, "properties")
	if schemaConst(t, requiredSlotProperties, "input_name") != "confirmed_delivery_boundary" || schemaConst(t, requiredSlotProperties, "attestation_kind") != "confirmed_intent" {
		t.Fatalf("delivery-ticket caller slot = %#v", requiredSlot)
	}
	if got := schemaEnum(t, schemaObject(t, requiredSlotProperties, "allowed_source_kinds"), "items"); !reflect.DeepEqual(got, []any{"inline_text"}) {
		t.Fatalf("caller source kinds = %#v", got)
	}
	if got := schemaEnum(t, schemaObject(t, requiredSlotProperties, "required_attestation_kinds"), "items"); !reflect.DeepEqual(got, []any{"confirmed_intent", "sensitive_data_clearance"}) {
		t.Fatalf("caller required attestation kinds = %#v", got)
	}

	callerItems := schemaObject(t, linkedInputs, "items")
	callerBranches := schemaArray(t, callerItems, "oneOf")
	for _, candidate := range callerBranches {
		branch, ok := candidate.(map[string]any)
		if !ok {
			t.Fatal("caller slot branch is not an object")
		}
		allOf := schemaArray(t, branch, "allOf")
		if len(allOf) != 2 {
			t.Fatalf("caller slot allOf = %#v", branch)
		}
		generic := allOf[0].(map[string]any)
		if generic["$ref"] != "#/$defs/InputBinding" {
			t.Fatalf("caller slot generic base = %#v", generic)
		}
		slot := allOf[1].(map[string]any)
		slotProperties := schemaObject(t, slot, "properties")
		inputName := schemaConst(t, slotProperties, "input_name")
		if inputName == "current_feature_workspace_route" {
			t.Fatal("derived current_feature_workspace_route is exposed as a caller input")
		}
		if got := schemaArray(t, schemaObject(t, slotProperties, "source_kind"), "enum"); containsSchemaValue(got, "committed_source") {
			t.Fatalf("caller slot admits committed_source: %#v", slot)
		}
	}

	itemDocument, err := json.Marshal(map[string]any{
		"$schema":  "https://json-schema.org/draft/2020-12/schema",
		"$defs":    defs,
		"type":     "object",
		"required": []string{"inputs"},
		"properties": map[string]any{
			"inputs": map[string]any{"type": "array", "items": callerItems},
		},
		"additionalProperties": false,
	})
	if err != nil {
		t.Fatal(err)
	}
	validCallerInput := map[string]any{
		"input_name":      "confirmed_delivery_boundary",
		"source_kind":     "inline_text",
		"display_name":    "boundary",
		"media_type":      "text/plain",
		"expected_sha256": strings.Repeat("a", 64),
		"source":          map[string]any{"text": "the confirmed boundary"},
	}
	validInstance, err := json.Marshal(map[string]any{"inputs": []any{validCallerInput}})
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.ValidateSchemaInstance(itemDocument, validInstance); err != nil {
		t.Fatalf("valid linked caller input rejected: %v", err)
	}
	for name, invalid := range map[string]map[string]any{
		"derived name": func() map[string]any {
			copy := cloneSchemaMap(validCallerInput)
			copy["input_name"] = "current_feature_workspace_route"
			return copy
		}(),
		"committed source": func() map[string]any {
			copy := cloneSchemaMap(validCallerInput)
			copy["source_kind"] = "committed_source"
			copy["source"] = map[string]any{
				"repository_key":    "repo",
				"revision":          "revision",
				"path":              map[string]any{"path_id": strings.Repeat("b", 64)},
				"expected_blob_oid": strings.Repeat("c", 40),
			}
			return copy
		}(),
	} {
		invalidInstance, err := json.Marshal(map[string]any{"inputs": []any{invalid}})
		if err != nil {
			t.Fatal(err)
		}
		if err := registry.ValidateSchemaInstance(itemDocument, invalidInstance); err == nil {
			t.Fatalf("linked caller input accepted forbidden %s", name)
		}
	}

	derivedSchema := schemaObject(t, properties, "derived_inputs")
	if derivedSchema["minItems"] != float64(1) || derivedSchema["maxItems"] != float64(1) {
		t.Fatalf("derived input cardinality = %#v", derivedSchema)
	}
	derivedSlot := schemaOneOfSlot(t, schemaObject(t, derivedSchema, "items"))
	derivedSlotProperties := schemaObject(t, derivedSlot, "properties")
	if schemaConst(t, derivedSlotProperties, "input_name") != "current_feature_workspace_route" || schemaConst(t, derivedSlotProperties, "workflow_record_policy") != "derived" {
		t.Fatalf("delivery-ticket derived slot = %#v", derivedSlot)
	}
	if schemaObject(t, derivedSlotProperties, "allowed_source_kinds")["maxItems"] != float64(0) {
		t.Fatal("Relay-derived input exposes caller source kinds")
	}

	attestations := schemaObject(t, properties, "required_attestation_kinds")
	if got := schemaEnum(t, attestations, "items"); !reflect.DeepEqual(got, []any{"confirmed_intent", "sensitive_data_clearance"}) {
		t.Fatalf("required attestation kinds = %#v", got)
	}
	workflowReferences := schemaObject(t, properties, "workflow_reference_kinds")
	if got := schemaEnum(t, workflowReferences, "items"); !reflect.DeepEqual(got, []any{"feature_workspace"}) {
		t.Fatalf("workflow reference kinds = %#v", got)
	}

	inputBinding := schemaObject(t, defs, "InputBinding")
	inputKinds := map[string]bool{}
	for _, candidate := range schemaArray(t, inputBinding, "oneOf") {
		branch, ok := candidate.(map[string]any)
		if !ok {
			t.Fatal("InputBinding branch is not an object")
		}
		inputKinds[schemaConst(t, schemaObject(t, branch, "properties"), "source_kind")] = true
	}
	for _, kind := range []string{"uploaded_file", "relay_artifact", "inline_text", "workflow_record", "committed_source"} {
		if !inputKinds[kind] {
			t.Fatalf("generic InputBinding lost source kind %q", kind)
		}
	}

	operation, ok := registry.LookupPublishedOperation("planner.delivery_ticket")
	if !ok || len(operation.RequiredInputs) != 1 || operation.RequiredInputs[0].InputName != "confirmed_delivery_boundary" || len(operation.DerivedInputs) != 1 || operation.DerivedInputs[0].InputName != "current_feature_workspace_route" {
		t.Fatalf("runtime operation authority no longer matches expected admission facts: %#v", operation)
	}
}

func TestPlannerDeliveryTicketLinkedRequestSchemaInstances(t *testing.T) {
	set, err := BuildMCPRouteManifests()
	if err != nil {
		t.Fatal(err)
	}
	var schema json.RawMessage
	for _, manifest := range set.Manifests {
		if manifest.SurfaceContract != "planner-authoring.v1" {
			continue
		}
		for _, tool := range manifest.Tools {
			if tool.Name == "create_operation_packet" {
				schema = tool.InputSchema
				break
			}
		}
	}
	if len(schema) == 0 {
		t.Fatal("planner create_operation_packet schema is missing")
	}
	sha := strings.Repeat("a", 64)
	input := map[string]any{
		"input_name":      "confirmed_delivery_boundary",
		"source_kind":     "inline_text",
		"display_name":    "boundary",
		"media_type":      "text/plain",
		"expected_sha256": sha,
		"source":          map[string]any{"text": "the confirmed boundary"},
	}
	clearance := map[string]any{
		"policy_version": "relay.canonical-artifact-sensitive-data.v1",
		"subject_sha256": sha,
		"declaration": map[string]any{
			"password": false, "api_key_or_access_token": false,
			"refresh_token_or_session_material": false, "cookie_or_authorization_header": false,
			"private_or_ssh_key": false, "credential": false,
			"complete_secret_bearing_environment_file": false, "avoidable_signed_secret_bearing_url": false,
		},
		"confirmed": true,
	}
	valid := map[string]any{
		"surface_contract": "planner-authoring.v1",
		"mutation_id":      "mutation-1",
		"operation_id":     "planner.delivery_ticket",
		"project_id":       "project-1",
		"inputs":           []any{input},
		"workflow_references": []any{
			map[string]any{"kind": "feature_workspace", "workspace_id": "workspace-1"},
		},
		"attestations": []any{
			map[string]any{"kind": "confirmed_intent", "input_name": "confirmed_delivery_boundary", "subject_sha256": sha},
			map[string]any{"kind": "sensitive_data_clearance", "input_name": "confirmed_delivery_boundary", "clearance": clearance},
		},
	}
	validRaw, err := json.Marshal(valid)
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.ValidateSchemaInstance(schema, validRaw); err != nil {
		t.Fatalf("valid planner.delivery_ticket request rejected: %v", err)
	}

	tests := map[string]func(map[string]any){
		"invalid workflow kind": func(value map[string]any) {
			value["workflow_references"] = []any{map[string]any{"kind": "run", "run_id": "run-1"}}
		},
		"missing confirmed intent": func(value map[string]any) {
			value["attestations"] = []any{value["attestations"].([]any)[1]}
		},
		"wrong confirmed intent": func(value map[string]any) {
			attestations := value["attestations"].([]any)
			wrong := cloneSchemaMap(attestations[0].(map[string]any))
			wrong["kind"] = "approved_artifact"
			value["attestations"] = []any{wrong, attestations[1]}
		},
	}
	for name, mutate := range tests {
		invalid := cloneSchemaMap(valid)
		mutate(invalid)
		raw, err := json.Marshal(invalid)
		if err != nil {
			t.Fatal(err)
		}
		if err := registry.ValidateSchemaInstance(schema, raw); err == nil {
			t.Fatalf("invalid %s request was accepted", name)
		}
	}
}

func cloneSchemaMap(value map[string]any) map[string]any {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	var clone map[string]any
	if err := json.Unmarshal(encoded, &clone); err != nil {
		panic(err)
	}
	return clone
}
func schemaObject(t *testing.T, value map[string]any, key string) map[string]any {
	t.Helper()
	child, ok := value[key].(map[string]any)
	if !ok {
		t.Fatalf("schema member %q is not an object", key)
	}
	return child
}

func schemaArray(t *testing.T, value map[string]any, key string) []any {
	t.Helper()
	child, ok := value[key].([]any)
	if !ok {
		t.Fatalf("schema member %q is not an array", key)
	}
	return child
}

func schemaConst(t *testing.T, value map[string]any, key string) string {
	t.Helper()
	property, ok := value[key].(map[string]any)
	if !ok {
		t.Fatalf("schema member %q is not a property schema", key)
	}
	child, ok := property["const"].(string)
	if !ok {
		t.Fatalf("schema member %q is not a string const", key)
	}
	return child
}

func schemaEnum(t *testing.T, value map[string]any, key string) []any {
	t.Helper()
	items := schemaObject(t, value, key)
	return schemaArray(t, items, "enum")
}

func containsSchemaValue(values []any, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func schemaOneOfSlot(t *testing.T, value any) map[string]any {
	object, ok := value.(map[string]any)
	if !ok {
		panic("schema slot is not an object")
	}
	branches, ok := object["oneOf"].([]any)
	if !ok || len(branches) != 1 {
		return object
	}
	branch, ok := branches[0].(map[string]any)
	if !ok {
		panic("schema slot branch is not an object")
	}
	return branch
}
