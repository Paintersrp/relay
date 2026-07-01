package mcp

import (
	"reflect"
	"strings"
	"testing"
)

func TestDefaultLocalOperatorToolSurfaceIncludesAllSafeTools(t *testing.T) {
	srv := NewServer(nil, &MCPDeps{})
	gotNames := toolNames(srv.tools)
	expected := append(baseToolNamesForTest(),
		"get_project",
		"get_plan",
		"get_pass",
		"get_pass_context",
		"get_next_pass_work",
		"get_next_audit_work",
		"create_source_snapshot",
		"list_project_files",
		"search_project_files",
		"read_project_file",
		"resolve_project_repository",
		"get_repository_git_status",
		"get_repository_recent_commit",
		"list_repository_changed_files",
		"get_repository_diff",
		"create_context_packet",
		"get_context_packet",
		"create_local_audit",
		"get_local_audit",
		"list_project_local_audits",
		"search_project_context_memory",
		"list_project_context_records",
		"get_project_context_record",
		"create_project_context_record",
		"supersede_project_context_record",
		"list_refactor_discovery_tasks",
		"get_refactor_discovery_task",
		"create_refactor_discovery_task",
		"update_refactor_discovery_task",
		"complete_refactor_discovery_task",
		"close_refactor_discovery_task",
		"supersede_refactor_discovery_task",
		"list_refactor_candidates",
		"get_refactor_candidate",
		"search_refactor_candidates",
		"create_refactor_candidate",
		"update_refactor_candidate",
		"defer_refactor_candidate",
		"reject_refactor_candidate",
		"supersede_refactor_candidate",
		"suggest_refactor_candidate_placement",
		"promote_refactor_candidate_to_plan",
		"generate_refactor_only_plan",
	)

	if !reflect.DeepEqual(gotNames, expected) {
		t.Errorf("expected tools:\n%v\ngot:\n%v", expected, gotNames)
	}
}

func TestRestrictedToolSurfaceHidesBrokerTools(t *testing.T) {
	srv := NewServer(nil, &MCPDeps{ToolProfile: ToolProfileRestricted})
	gotNames := toolNames(srv.tools)
	expected := baseToolNamesForTest()

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
		"resolve_project_repository",
		"get_repository_git_status",
		"get_repository_recent_commit",
		"list_repository_changed_files",
		"get_repository_diff",
		"create_context_packet",
		"get_context_packet",
		"create_local_audit",
		"get_local_audit",
		"list_project_local_audits",
		"search_project_context_memory",
		"list_project_context_records",
		"get_project_context_record",
		"create_project_context_record",
		"supersede_project_context_record",
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

func TestRestrictedGitBrokerToolCallIsUnknown(t *testing.T) {
	srv := NewServer(nil, &MCPDeps{ToolProfile: ToolProfileRestricted})

	reqJSON := `{
		"jsonrpc": "2.0",
		"id": 1,
		"method": "tools/call",
		"params": {
			"name": "get_repository_diff",
			"arguments": {
				"project_id": "relay",
				"repo_id": "relay",
				"mode": "worktree"
			}
		}
	}`

	resp := srv.handleLine([]byte(reqJSON))

	if resp.Error == nil {
		t.Fatal("expected error calling git broker tool in restricted mode, got success")
	}
	if resp.Error.Code != CodeMethodNotFound {
		t.Errorf("expected error code %d (CodeMethodNotFound), got %d", CodeMethodNotFound, resp.Error.Code)
	}
	if !strings.Contains(resp.Error.Message, "unknown tool") {
		t.Errorf("expected error message to contain 'unknown tool', got %q", resp.Error.Message)
	}
}

func TestRestrictedMemoryBrokerToolCallIsUnknown(t *testing.T) {
	srv := NewServer(nil, &MCPDeps{ToolProfile: ToolProfileRestricted})

	reqJSON := `{
		"jsonrpc": "2.0",
		"id": 1,
		"method": "tools/call",
		"params": {
			"name": "create_project_context_record",
			"arguments": {
				"project_id": "relay",
				"kind": "decision",
				"title": "Durable context",
				"body": "Important long-form project context.",
				"dedupe_reason": "Checked existing context first."
			}
		}
	}`

	resp := srv.handleLine([]byte(reqJSON))

	if resp.Error == nil {
		t.Fatal("expected error calling memory broker tool in restricted mode, got success")
	}
	if resp.Error.Code != CodeMethodNotFound {
		t.Errorf("expected error code %d (CodeMethodNotFound), got %d", CodeMethodNotFound, resp.Error.Code)
	}
	if !strings.Contains(resp.Error.Message, "unknown tool") {
		t.Errorf("expected error message to contain 'unknown tool', got %q", resp.Error.Message)
	}
}

func TestRestrictedLocalAuditBrokerToolCallIsUnknown(t *testing.T) {
	srv := NewServer(nil, &MCPDeps{ToolProfile: ToolProfileRestricted})

	reqJSON := `{
		"jsonrpc": "2.0",
		"id": 1,
		"method": "tools/call",
		"params": {
			"name": "create_local_audit",
			"arguments": {
				"mode": "full_repository",
				"project_id": "relay"
			}
		}
	}`

	resp := srv.handleLine([]byte(reqJSON))

	if resp.Error == nil {
		t.Fatal("expected error calling local audit broker tool in restricted mode, got success")
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
