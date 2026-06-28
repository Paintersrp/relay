package mcp

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestToolsList_LocalOperatorSchemasAreValidAndBounded(t *testing.T) {
	srv := NewServer(nil, &MCPDeps{ToolProfile: ToolProfileLocalOperator})

	list := collectAllTools(t, srv, ToolsListParams{})

	seen := map[string]bool{}
	for _, tool := range list.Tools {
		if tool.Name == "" {
			t.Error("tool has empty name")
		}
		if seen[tool.Name] {
			t.Errorf("duplicate tool name: %q", tool.Name)
		}
		seen[tool.Name] = true

		if len(tool.InputSchema) == 0 {
			t.Errorf("tool %q has empty InputSchema", tool.Name)
		}
		if !json.Valid(tool.InputSchema) {
			t.Errorf("tool %q InputSchema is not valid JSON: %s", tool.Name, string(tool.InputSchema))
		}

		var schema map[string]interface{}
		if err := json.Unmarshal(tool.InputSchema, &schema); err != nil {
			t.Errorf("tool %q InputSchema decode error: %v", tool.Name, err)
			continue
		}
		if typ, ok := schema["type"]; !ok || typ != "object" {
			t.Errorf("tool %q InputSchema top-level type must be object, got %v", tool.Name, typ)
		}
	}
}

func TestToolsList_LocalOperatorPagedDiscoveryIncludesPlannerAndAuditor(t *testing.T) {
	srv := NewServer(nil, &MCPDeps{ToolProfile: ToolProfileLocalOperator})
	list := collectAllTools(t, srv, ToolsListParams{})

	requiredTools := []string{
		"submit_test_audit_packet",
		"list_open_runs",
		"get_run_status",
		"submit_audit_packet",
		"get_next_audit_work",
		"create_local_audit",
		"get_local_audit",
		"list_project_local_audits",
		"create_run_from_planner_handoff",
		"submit_planner_pass_plan",
		toolCreatePlanAttemptWithIntent,
		toolCreatePlanSeed,
	}
	registered := toolNames(list.Tools)
	for _, required := range requiredTools {
		found := false
		for _, name := range registered {
			if name == required {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("required audit tool %q not found in tools/list", required)
		}
	}

	firstPage := listToolsPage(t, srv, ToolsListParams{})
	if len(firstPage.Tools) > toolsListPageSize {
		t.Fatalf("first page returned %d tools, exceeds page size %d", len(firstPage.Tools), toolsListPageSize)
	}
	if firstPage.NextCursor == "" && len(list.Tools) > toolsListPageSize {
		t.Fatal("expected next cursor for paged local-operator discovery")
	}
}

func TestToolsList_QueryFilteringDoesNotMutateRegistry(t *testing.T) {
	srv := NewServer(nil, &MCPDeps{ToolProfile: ToolProfileLocalOperator})
	allBefore := collectAllTools(t, srv, ToolsListParams{})
	auditOnly := collectAllTools(t, srv, ToolsListParams{Query: "audit"})
	if len(auditOnly.Tools) == 0 {
		t.Fatal("expected audit query to return tools")
	}
	for _, tool := range auditOnly.Tools {
		text := strings.ToLower(tool.Name + " " + tool.Description + " " + strings.Join(toolTagsByName(tool.Name), " "))
		if !strings.Contains(text, "audit") {
			t.Fatalf("query returned non-audit tool %q", tool.Name)
		}
	}
	allAfter := collectAllTools(t, srv, ToolsListParams{})
	if strings.Join(toolNames(allBefore.Tools), "\n") != strings.Join(toolNames(allAfter.Tools), "\n") {
		t.Fatal("filtered tools/list mutated registry order or contents")
	}
}

func TestToolsList_InvalidCursor(t *testing.T) {
	srv := NewServer(nil, &MCPDeps{ToolProfile: ToolProfileLocalOperator})
	params, _ := json.Marshal(ToolsListParams{Cursor: "not-a-cursor"})
	req := Request{
		JSONRPC: JSONRPCVersion,
		ID:      json.RawMessage(`1`),
		Method:  "tools/list",
		Params:  params,
	}
	resp := srv.handleLine(mustMarshal(t, req))
	if resp.Error == nil {
		t.Fatal("expected JSON-RPC error for invalid cursor")
	}
	if resp.Error.Code != CodeInvalidParams {
		t.Errorf("expected CodeInvalidParams, got %d", resp.Error.Code)
	}
}

func TestToolsCall_AuditProfileAliasUsesFullLocalOperatorRegistry(t *testing.T) {
	srv := NewServer(nil, &MCPDeps{ToolProfile: ToolProfileLocalOperator})

	tests := []struct {
		name string
		args string
	}{
		{"get_run_status", `{}`},
		{"get_local_audit", `{}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params, _ := json.Marshal(ToolCallParams{
				Name:      tt.name,
				Arguments: json.RawMessage(tt.args),
			})
			req := Request{
				JSONRPC: JSONRPCVersion,
				ID:      json.RawMessage(`1`),
				Method:  "tools/call",
				Params:  params,
			}
			resp := srv.handleLine(mustMarshal(t, req))

			if resp.Error != nil {
				t.Fatalf("unexpected JSON-RPC error for registered tool %q: %v", tt.name, resp.Error)
			}

			var result ToolCallResult
			b, _ := json.Marshal(resp.Result)
			if err := json.Unmarshal(b, &result); err != nil {
				t.Fatalf("unmarshal result for %q: %v", tt.name, err)
			}

			if !result.IsError {
				t.Fatalf("expected tool-level error for %q with empty args (no store wired), got success", tt.name)
			}

			text := result.Content[0].Text
			if strings.Contains(text, "unknown tool") {
				t.Errorf("tool %q should reach handler (DEPENDENCY_ERROR or VALIDATION_ERROR), not unknown tool: %s", tt.name, text)
			}
		})
	}
}

func listToolsPage(t *testing.T, srv *Server, params ToolsListParams) ToolsListResult {
	t.Helper()
	var rawParams json.RawMessage
	if params.Cursor != "" || params.Query != "" || len(params.IncludeTags) > 0 {
		rawParams = mustMarshal(t, params)
	}
	req := Request{
		JSONRPC: JSONRPCVersion,
		ID:      json.RawMessage(`1`),
		Method:  "tools/list",
		Params:  rawParams,
	}
	resp := srv.handleLine(mustMarshal(t, req))
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}
	raw, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal page response: %v", err)
	}
	const maxBytes = 256 * 1024
	if len(raw) > maxBytes {
		t.Errorf("response size %d exceeds cap %d", len(raw), maxBytes)
	}
	var list ToolsListResult
	b, _ := json.Marshal(resp.Result)
	if err := json.Unmarshal(b, &list); err != nil {
		t.Fatalf("unmarshal tools list: %v", err)
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
