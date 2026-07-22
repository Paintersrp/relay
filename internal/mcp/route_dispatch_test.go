package mcp

import (
	"encoding/json"
	"strings"
	"testing"

	"relay/internal/mcp/routecontracts"
)

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
