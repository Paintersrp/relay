package mcp

import (
	"reflect"
	"strings"
	"testing"
)

func TestDefaultLocalOperatorToolSurfaceIncludesAllSafeTools(t *testing.T) {
	srv := NewServer(nil, &MCPDeps{})
	gotNames := toolNames(srv.tools)
	expected := []string{
		"submit_test_audit_packet",
		"create_run_from_planner_handoff",
		"submit_planner_pass_plan",
		"list_open_runs",
		"get_run_status",
		"submit_audit_packet",
		"get_project",
		"get_plan",
		"get_pass",
		"get_pass_context",
		"create_source_snapshot",
		"list_project_files",
		"search_project_files",
		"read_project_file",
		"create_context_packet",
		"get_context_packet",
	}

	if !reflect.DeepEqual(gotNames, expected) {
		t.Errorf("expected tools:\n%v\ngot:\n%v", expected, gotNames)
	}
}

func TestRestrictedToolSurfaceHidesBrokerTools(t *testing.T) {
	srv := NewServer(nil, &MCPDeps{ToolProfile: ToolProfileRestricted})
	gotNames := toolNames(srv.tools)
	expected := []string{
		"submit_test_audit_packet",
		"create_run_from_planner_handoff",
		"submit_planner_pass_plan",
		"list_open_runs",
		"get_run_status",
		"submit_audit_packet",
	}

	if !reflect.DeepEqual(gotNames, expected) {
		t.Errorf("expected tools:\n%v\ngot:\n%v", expected, gotNames)
	}

	// Double check that no broker tool names exist in the list.
	brokerTools := []string{
		"get_project",
		"get_plan",
		"get_pass",
		"get_pass_context",
		"create_source_snapshot",
		"list_project_files",
		"search_project_files",
		"read_project_file",
		"create_context_packet",
		"get_context_packet",
	}
	for _, name := range gotNames {
		for _, broker := range brokerTools {
			if name == broker {
				t.Errorf("broker tool %q should be hidden in restricted mode", name)
			}
		}
	}
}

func TestRestrictedBrokerToolCallIsUnknown(t *testing.T) {
	srv := NewServer(nil, &MCPDeps{ToolProfile: ToolProfileRestricted})
	
	reqJSON := `{
		"jsonrpc": "2.0",
		"id": 1,
		"method": "tools/call",
		"params": {
			"name": "get_project",
			"arguments": {}
		}
	}`
	
	resp := srv.handleLine([]byte(reqJSON))
	
	if resp.Error == nil {
		t.Fatal("expected error calling broker tool in restricted mode, got success")
	}
	
	if resp.Error.Code != CodeMethodNotFound {
		t.Errorf("expected error code %d (CodeMethodNotFound), got %d", CodeMethodNotFound, resp.Error.Code)
	}
	
	if !strings.Contains(resp.Error.Message, "unknown tool") {
		t.Errorf("expected error message to contain 'unknown tool', got %q", resp.Error.Message)
	}
}

func toolNames(tools []ToolDefinition) []string {
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Name)
	}
	return names
}
