package mcp

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestToolsListCanonicalProfilesAreExactAndSchemasAreBounded(t *testing.T) {
	tests := []struct {
		name    string
		profile ToolProfile
		want    []string
	}{
		{name: "planner", profile: ToolProfilePlanner, want: []string{"validate_artifact", "submit_plan", "get_plan", "create_run"}},
		{name: "auditor", profile: ToolProfileAuditor, want: []string{"validate_artifact", "create_run", "get_audit_packet", "record_audit_decision"}},
		{name: "local operator", profile: ToolProfileLocalOperator, want: toolNames(legacyLocalOperatorToolDefinitions())},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := NewServer(nil, &MCPDeps{ToolProfile: tt.profile})
			list := collectAllTools(t, srv, ToolsListParams{})
			if got := toolNames(list.Tools); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("tools = %v, want %v", got, tt.want)
			}
			for _, tool := range list.Tools {
				if !json.Valid(tool.InputSchema) {
					t.Fatalf("tool %q has invalid schema: %s", tool.Name, tool.InputSchema)
				}
				var schema map[string]any
				if err := json.Unmarshal(tool.InputSchema, &schema); err != nil {
					t.Fatal(err)
				}
				if schema["type"] != "object" {
					t.Fatalf("tool %q top-level schema type = %v", tool.Name, schema["type"])
				}
				if tool.Name == "validate_artifact" || tool.Name == "submit_plan" || tool.Name == "create_run" {
					params, ok := tool.Meta["openai/fileParams"].([]any)
					if !ok || !reflect.DeepEqual(params, []any{"artifact_file"}) {
						t.Fatalf("tool %q file params = %#v", tool.Name, tool.Meta["openai/fileParams"])
					}
					props := schema["properties"].(map[string]any)
					fileSchema := props["artifact_file"].(map[string]any)
					if fileSchema["type"] != "object" || fileSchema["additionalProperties"] != false {
						t.Fatalf("tool %q artifact_file schema is not bounded: %#v", tool.Name, fileSchema)
					}
				}
			}
		})
	}
}

func TestToolsListQueryFilteringDoesNotMutateCanonicalRegistry(t *testing.T) {
	srv := NewServer(nil, &MCPDeps{ToolProfile: ToolProfilePlanner})
	allBefore := collectAllTools(t, srv, ToolsListParams{})
	planOnly := collectAllTools(t, srv, ToolsListParams{Query: "plan"})
	if len(planOnly.Tools) == 0 {
		t.Fatal("expected plan query to return tools")
	}
	for _, tool := range planOnly.Tools {
		text := strings.ToLower(tool.Name + " " + tool.Description)
		if !strings.Contains(text, "plan") {
			t.Fatalf("query returned non-plan tool %q", tool.Name)
		}
	}
	allAfter := collectAllTools(t, srv, ToolsListParams{})
	if !reflect.DeepEqual(toolNames(allBefore.Tools), toolNames(allAfter.Tools)) {
		t.Fatal("filtered tools/list mutated canonical registry")
	}
}

func TestToolsListInvalidCursor(t *testing.T) {
	srv := NewServer(nil)
	params, _ := json.Marshal(ToolsListParams{Cursor: "not-a-cursor"})
	resp := srv.handleLine(mustMarshal(t, Request{
		JSONRPC: JSONRPCVersion,
		ID:      json.RawMessage(`1`),
		Method:  "tools/list",
		Params:  params,
	}))
	if resp.Error == nil || resp.Error.Code != CodeInvalidParams {
		t.Fatalf("expected invalid params, got %+v", resp.Error)
	}
}

func listToolsPage(t *testing.T, srv *Server, params ToolsListParams) ToolsListResult {
	t.Helper()
	var rawParams json.RawMessage
	if params.Cursor != "" || params.Query != "" || len(params.IncludeTags) > 0 {
		rawParams = mustMarshal(t, params)
	}
	resp := srv.handleLine(mustMarshal(t, Request{
		JSONRPC: JSONRPCVersion,
		ID:      json.RawMessage(`1`),
		Method:  "tools/list",
		Params:  rawParams,
	}))
	if resp.Error != nil {
		t.Fatalf("unexpected tools/list error: %v", resp.Error)
	}
	raw, err := json.Marshal(resp)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) > 256*1024 {
		t.Fatalf("tools/list response size = %d", len(raw))
	}
	var list ToolsListResult
	body, _ := json.Marshal(resp.Result)
	if err := json.Unmarshal(body, &list); err != nil {
		t.Fatal(err)
	}
	return list
}

func collectAllTools(t *testing.T, srv *Server, params ToolsListParams) ToolsListResult {
	t.Helper()
	var out ToolsListResult
	for {
		page := listToolsPage(t, srv, params)
		out.Tools = append(out.Tools, page.Tools...)
		if page.NextCursor == "" {
			return out
		}
		params.Cursor = page.NextCursor
	}
}

func toolNames(tools []ToolDefinition) []string {
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Name)
	}
	return names
}
