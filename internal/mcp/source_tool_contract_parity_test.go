package mcp

import (
	"encoding/base64"
	"encoding/json"
	"sort"
	"strings"
	"testing"

	"relay/internal/app/mcpcomposition"
	"relay/internal/mcp/routecontracts"
	"relay/internal/operations/registry"
)

// sourceInvalidRequest is the bounded gateway reason returned once a request has
// been accepted by the strict decoder and the route check. Seeing it proves the
// request reached the gateway rather than failing schema decoding.
const sourceInvalidRequest = "source request is invalid"

var sourceObsoleteMembers = []string{
	"expected_packet_id", "query", "case_sensitive", "repository_keys",
	"path_prefixes", "start_line", "line_count", "limit_bytes", "project_id",
}

type sourceContractExpectation struct {
	tool     string
	minimum  map[string]any
	complete map[string]any
}

func sourceContractExpectations() []sourceContractExpectation {
	reference := sourcePathArgument("docs/notes.md")
	return []sourceContractExpectation{
		{
			tool:     "list_source_tree",
			minimum:  map[string]any{"surface_contract": sourceSnapshotSurface, "packet_id": "opkt-contract", "repository_key": "project-repository", "limit": 1},
			complete: map[string]any{"operation_id": sourceSnapshotOperation, "directory": reference, "recursive": true, "cursor": "continuation"},
		},
		{
			tool:     "search_source",
			minimum:  map[string]any{"surface_contract": sourceSnapshotSurface, "packet_id": "opkt-contract", "repository_key": "project-repository", "mode": "text_literal", "limit": 1, "examined_objects": 1, "examined_bytes": 4},
			complete: map[string]any{"operation_id": sourceSnapshotOperation, "revision": map[string]any{"anchor_name": "baseline"}, "text_literal": "alpha", "byte_literal_base64": base64.StdEncoding.EncodeToString([]byte{0}), "prefixes": []any{reference}, "cursor": "continuation"},
		},
		{
			tool:     "read_source_text",
			minimum:  map[string]any{"surface_contract": sourceSnapshotSurface, "packet_id": "opkt-contract", "repository_key": "project-repository", "path": reference, "limit": 4},
			complete: map[string]any{"operation_id": sourceSnapshotOperation, "revision": map[string]any{"anchor_name": "baseline"}, "offset": 0, "cursor": "continuation"},
		},
	}
}

func TestSourceToolPublishedContractAndStrictDecoderAgree(t *testing.T) {
	routes, err := routecontracts.BuildMCPRouteManifests()
	if err != nil {
		t.Fatal(err)
	}
	dispatchers, err := NewRouteDispatchers(routes, RouteDispatchServices{})
	if err != nil {
		t.Fatal(err)
	}
	manifest := coldStartRoute(t, routes, sourceSnapshotSurface)
	handlers, ok := dispatchers.Handlers[manifest.RoutePath]
	if !ok {
		t.Fatalf("route %s has no dispatchers", manifest.RoutePath)
	}

	for _, expectation := range sourceContractExpectations() {
		t.Run(expectation.tool, func(t *testing.T) {
			tool := sourceToolManifest(t, manifest, expectation.tool)
			if !registry.OwnedSourceToolContract(tool.Name) || tool.SchemaOwner != "contracts/source" {
				t.Fatalf("%s schema ownership = %q", tool.Name, tool.SchemaOwner)
			}
			schema := sourceSchemaObject(t, tool.InputSchema)
			if additional, ok := schema["additionalProperties"].(bool); !ok || additional {
				t.Fatalf("%s input schema does not keep additionalProperties false", tool.Name)
			}
			properties := sourceSchemaMembers(t, schema, "properties")
			required := sourceSchemaStrings(t, schema, "required")
			for _, name := range required {
				if _, declared := properties[name]; !declared {
					t.Fatalf("%s requires undeclared member %q", tool.Name, name)
				}
			}

			minimum := expectation.minimum
			complete := sourceMergedRequest(expectation.minimum, expectation.complete)
			assertSourceKeySet(t, tool.Name+" minimum", minimum, required)
			assertSourceKeySet(t, tool.Name+" complete", complete, sourceSchemaKeys(properties))

			for label, request := range map[string]map[string]any{"minimum": minimum, "complete": complete} {
				raw := sourceMarshal(t, request)
				if err := registry.ValidateSchemaInstance(tool.InputSchema, raw); err != nil {
					t.Fatalf("%s %s request violates its published input schema: %v", tool.Name, label, err)
				}
				result := handlers[tool.Name](json.RawMessage(raw))
				if !result.IsError || len(result.Content) != 1 {
					t.Fatalf("%s %s request result = %#v", tool.Name, label, result)
				}
				if reason := result.Content[0].Text; reason != sourceInvalidRequest {
					t.Fatalf("%s %s request was not accepted by the strict decoder: %s", tool.Name, label, reason)
				}
			}

			for _, obsolete := range sourceObsoleteMembers {
				if _, declared := properties[obsolete]; declared {
					t.Fatalf("%s still publishes obsolete member %q", tool.Name, obsolete)
				}
				stale := sourceMergedRequest(minimum, map[string]any{obsolete: "legacy"})
				result := handlers[tool.Name](json.RawMessage(sourceMarshal(t, stale)))
				if !result.IsError {
					t.Fatalf("%s accepted obsolete member %q", tool.Name, obsolete)
				}
				if reason := result.Content[0].Text; !strings.Contains(reason, obsolete) {
					t.Fatalf("%s obsolete member %q rejection = %s", tool.Name, obsolete, reason)
				}
			}
		})
	}
}

func TestSourceToolPublishedBoundsMatchGatewayLimits(t *testing.T) {
	routes, err := routecontracts.BuildMCPRouteManifests()
	if err != nil {
		t.Fatal(err)
	}
	manifest := coldStartRoute(t, routes, sourceSnapshotSurface)
	inlineBase64 := float64(4 * ((mcpcomposition.MaxInlinePathBytes + 2) / 3))

	for _, test := range []struct {
		tool   string
		bounds map[string]float64
	}{
		{tool: "list_source_tree", bounds: map[string]float64{
			"limit.maximum":                 float64(mcpcomposition.MaxTreePageEntries),
			"limit.minimum":                 1,
			"cursor.maxLength":              float64(mcpcomposition.MaxCursorTokenBytes),
			"directory.inline_base64_bytes": inlineBase64,
		}},
		{tool: "search_source", bounds: map[string]float64{
			"limit.maximum":                float64(mcpcomposition.MaxSearchPageMatches),
			"limit.minimum":                1,
			"text_literal.maxLength":       float64(mcpcomposition.MaxSearchLiteralBytes),
			"examined_bytes.minimum":       float64(mcpcomposition.MinTextPageBytes),
			"cursor.maxLength":             float64(mcpcomposition.MaxCursorTokenBytes),
			"prefixes.inline_base64_bytes": inlineBase64,
		}},
		{tool: "read_source_text", bounds: map[string]float64{
			"limit.minimum":            float64(mcpcomposition.MinTextPageBytes),
			"limit.maximum":            float64(mcpcomposition.MaxTextPageBytes),
			"cursor.maxLength":         float64(mcpcomposition.MaxCursorTokenBytes),
			"path.inline_base64_bytes": inlineBase64,
		}},
	} {
		t.Run(test.tool, func(t *testing.T) {
			properties := sourceSchemaMembers(t, sourceSchemaObject(t, sourceToolManifest(t, manifest, test.tool).InputSchema), "properties")
			for locator, want := range test.bounds {
				if actual := sourceSchemaBound(t, properties, locator); actual != want {
					t.Fatalf("%s %s = %v want %v", test.tool, locator, actual, want)
				}
			}
		})
	}
}

func TestSourceToolPublishedSchemasAreBoundToTheirMountedRoute(t *testing.T) {
	routes, err := routecontracts.BuildMCPRouteManifests()
	if err != nil {
		t.Fatal(err)
	}
	mounted := 0
	for _, manifest := range routes.Manifests {
		operations := make([]string, 0, len(manifest.Operations))
		for _, operation := range manifest.Operations {
			operations = append(operations, operation.OperationID)
		}
		for _, tool := range manifest.Tools {
			if !registry.OwnedSourceToolContract(tool.Name) {
				continue
			}
			mounted++
			properties := sourceSchemaMembers(t, sourceSchemaObject(t, tool.InputSchema), "properties")
			surface := sourceSchemaMembers(t, properties, "surface_contract")
			if surface["const"] != manifest.SurfaceContract {
				t.Fatalf("%s/%s surface_contract = %#v", manifest.RoutePath, tool.Name, surface)
			}
			if _, ambiguous := surface["enum"]; ambiguous {
				t.Fatalf("%s/%s surface_contract remains caller selectable", manifest.RoutePath, tool.Name)
			}
			operation := sourceSchemaMembers(t, properties, "operation_id")
			values, ok := operation["enum"].([]any)
			if !ok {
				t.Fatalf("%s/%s operation_id = %#v", manifest.RoutePath, tool.Name, operation)
			}
			published := make([]string, 0, len(values))
			for _, value := range values {
				text, ok := value.(string)
				if !ok {
					t.Fatalf("%s/%s operation_id enum = %#v", manifest.RoutePath, tool.Name, values)
				}
				published = append(published, text)
			}
			if strings.Join(published, "|") != strings.Join(operations, "|") {
				t.Fatalf("%s/%s operation_id enum = %v want %v", manifest.RoutePath, tool.Name, published, operations)
			}
		}
	}
	if mounted != 21 {
		t.Fatalf("owned source tool mounts = %d", mounted)
	}
}

func sourceToolManifest(t *testing.T, manifest routecontracts.RouteManifest, name string) routecontracts.ToolManifest {
	t.Helper()
	for _, tool := range manifest.Tools {
		if tool.Name == name {
			return tool
		}
	}
	t.Fatalf("route %s does not mount %s", manifest.RoutePath, name)
	return routecontracts.ToolManifest{}
}

func sourceSchemaObject(t *testing.T, raw json.RawMessage) map[string]any {
	t.Helper()
	var value map[string]any
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatal(err)
	}
	return value
}

func sourceSchemaMembers(t *testing.T, node map[string]any, name string) map[string]any {
	t.Helper()
	value, ok := node[name].(map[string]any)
	if !ok {
		t.Fatalf("schema member %q is not an object", name)
	}
	return value
}

func sourceSchemaStrings(t *testing.T, node map[string]any, name string) []string {
	t.Helper()
	values, ok := node[name].([]any)
	if !ok {
		t.Fatalf("schema member %q is not an array", name)
	}
	result := make([]string, 0, len(values))
	for _, value := range values {
		text, ok := value.(string)
		if !ok {
			t.Fatalf("schema member %q contains a non-string", name)
		}
		result = append(result, text)
	}
	return result
}

func sourceSchemaKeys(node map[string]any) []string {
	keys := make([]string, 0, len(node))
	for key := range node {
		keys = append(keys, key)
	}
	return keys
}

// sourceSchemaBound reads one numeric bound. The inline_base64_bytes locator
// reads the shared path selector bound, including through an array member.
func sourceSchemaBound(t *testing.T, properties map[string]any, locator string) float64 {
	t.Helper()
	parts := strings.SplitN(locator, ".", 2)
	node := sourceSchemaMembers(t, properties, parts[0])
	if parts[1] == "inline_base64_bytes" {
		if items, ok := node["items"].(map[string]any); ok {
			node = items
		}
		selector := sourceSchemaMembers(t, sourceSchemaMembers(t, node, "properties"), "inline_base64")
		return sourceSchemaNumber(t, selector, "maxLength")
	}
	return sourceSchemaNumber(t, node, parts[1])
}

func sourceSchemaNumber(t *testing.T, node map[string]any, name string) float64 {
	t.Helper()
	value, ok := node[name].(float64)
	if !ok {
		t.Fatalf("schema member %q is not numeric: %#v", name, node[name])
	}
	return value
}

func sourceMergedRequest(base map[string]any, overrides map[string]any) map[string]any {
	request := make(map[string]any, len(base)+len(overrides))
	for name, value := range base {
		request[name] = value
	}
	for name, value := range overrides {
		request[name] = value
	}
	return request
}

func sourceMarshal(t *testing.T, request map[string]any) []byte {
	t.Helper()
	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func assertSourceKeySet(t *testing.T, label string, request map[string]any, expected []string) {
	t.Helper()
	actual := sourceSchemaKeys(request)
	sort.Strings(actual)
	want := append([]string(nil), expected...)
	sort.Strings(want)
	if strings.Join(actual, ",") != strings.Join(want, ",") {
		t.Fatalf("%s members = %v want %v", label, actual, want)
	}
}
