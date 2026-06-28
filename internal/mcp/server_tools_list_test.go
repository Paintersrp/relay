package mcp

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestToolsList_LocalOperatorSchemasAreValidAndBounded(t *testing.T) {
	srv := NewServer(nil, &MCPDeps{ToolProfile: ToolProfileLocalOperator})

	req := Request{
		JSONRPC: JSONRPCVersion,
		ID:      json.RawMessage(`1`),
		Method:  "tools/list",
	}
	resp := srv.handleLine(mustMarshal(t, req))
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}

	raw, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal full response: %v", err)
	}

	const maxBytes = 512 * 1024
	if len(raw) > maxBytes {
		t.Errorf("response size %d exceeds cap %d", len(raw), maxBytes)
	}

	var list ToolsListResult
	b, _ := json.Marshal(resp.Result)
	if err := json.Unmarshal(b, &list); err != nil {
		t.Fatalf("unmarshal tools list: %v", err)
	}

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

func TestToolsList_AuditProfileIsSmallAndAuditScoped(t *testing.T) {
	srv := NewServer(nil, &MCPDeps{ToolProfile: ToolProfileAudit})

	req := Request{
		JSONRPC: JSONRPCVersion,
		ID:      json.RawMessage(`1`),
		Method:  "tools/list",
	}
	resp := srv.handleLine(mustMarshal(t, req))
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}

	raw, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal full response: %v", err)
	}

	const maxBytes = 64 * 1024
	if len(raw) > maxBytes {
		t.Errorf("audit response size %d exceeds cap %d", len(raw), maxBytes)
	}

	var list ToolsListResult
	b, _ := json.Marshal(resp.Result)
	if err := json.Unmarshal(b, &list); err != nil {
		t.Fatalf("unmarshal tools list: %v", err)
	}

	requiredTools := []string{
		"submit_test_audit_packet",
		"list_open_runs",
		"get_run_status",
		"submit_audit_packet",
		"get_next_audit_work",
		"create_local_audit",
		"get_local_audit",
		"list_project_local_audits",
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

	excludedTools := []string{
		"create_run_from_planner_handoff",
		"submit_planner_pass_plan",
		"create_source_snapshot",
		"read_project_file",
		"list_refactor_candidates",
		"generate_refactor_only_plan",
		toolCreatePlanAttemptWithIntent,
		toolCreatePlanSeed,
	}
	for _, excluded := range excludedTools {
		for _, name := range registered {
			if name == excluded {
				t.Errorf("tool %q should not be in audit profile", excluded)
			}
		}
	}

	localOperatorSrv := NewServer(nil, &MCPDeps{ToolProfile: ToolProfileLocalOperator})
	localResp := localOperatorSrv.handleLine(mustMarshal(t, req))
	localRaw, _ := json.Marshal(localResp)
	if len(raw) >= len(localRaw) {
		t.Errorf("audit profile response size %d should be smaller than local-operator size %d", len(raw), len(localRaw))
	}
}

func TestToolsCall_AuditProfileRejectsUnlistedTools(t *testing.T) {
	srv := NewServer(nil, &MCPDeps{ToolProfile: ToolProfileAudit})

	params, _ := json.Marshal(ToolCallParams{
		Name:      "create_run_from_planner_handoff",
		Arguments: json.RawMessage(`{}`),
	})
	req := Request{
		JSONRPC: JSONRPCVersion,
		ID:      json.RawMessage(`1`),
		Method:  "tools/call",
		Params:  params,
	}
	resp := srv.handleLine(mustMarshal(t, req))
	if resp.Error == nil {
		t.Fatal("expected JSON-RPC error for unlisted tool under audit profile")
	}
	if resp.Error.Code != CodeMethodNotFound {
		t.Errorf("expected CodeMethodNotFound, got %d", resp.Error.Code)
	}
	if !strings.Contains(resp.Error.Message, "unknown tool") {
		t.Errorf("expected 'unknown tool' in error, got %q", resp.Error.Message)
	}
}

func TestToolsCall_AuditProfileAllowsRegisteredAuditToolValidation(t *testing.T) {
	srv := NewServer(nil, &MCPDeps{ToolProfile: ToolProfileAudit})

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
