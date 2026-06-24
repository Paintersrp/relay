package mcp

import (
	"encoding/json"
	"testing"
)

func TestHandleLineWithSkipNotificationsInitializedProducesNoResponse(t *testing.T) {
	srv := NewServer(discardLogger())

	resp, skip := srv.handleLineWithSkip([]byte(`{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}`))
	if !skip {
		t.Fatal("expected notifications/initialized to be treated as a notification")
	}
	if resp.JSONRPC != "" || len(resp.ID) != 0 || resp.Result != nil || resp.Error != nil {
		t.Fatalf("expected zero-value response for notification, got %+v", resp)
	}
}

func TestHandleLineWithSkipUnknownNotificationProducesNoResponse(t *testing.T) {
	srv := NewServer(discardLogger())

	resp, skip := srv.handleLineWithSkip([]byte(`{"jsonrpc":"2.0","method":"notifications/somethingElse","params":{}}`))
	if !skip {
		t.Fatal("expected unknown no-id notification to be skipped")
	}
	if resp.JSONRPC != "" || len(resp.ID) != 0 || resp.Result != nil || resp.Error != nil {
		t.Fatalf("expected zero-value response for notification, got %+v", resp)
	}
}

func TestHandleLineWithSkipInitializeRequestStillResponds(t *testing.T) {
	srv := NewServer(discardLogger())
	params, _ := json.Marshal(InitializeParams{ProtocolVersion: MCPProtocolVersion})
	req := Request{
		JSONRPC: JSONRPCVersion,
		ID:      json.RawMessage(`1`),
		Method:  "initialize",
		Params:  params,
	}

	resp, skip := srv.handleLineWithSkip(mustMarshal(t, req))
	if skip {
		t.Fatal("expected initialize request with id to produce a response")
	}
	if resp.JSONRPC != JSONRPCVersion {
		t.Fatalf("expected jsonrpc=%q, got %q", JSONRPCVersion, resp.JSONRPC)
	}
	if resp.Error != nil {
		t.Fatalf("expected initialize success, got error %+v", resp.Error)
	}
	if resp.Result == nil {
		t.Fatal("expected initialize result")
	}
}

func TestHandleLineWithSkipUnknownRequestStillErrors(t *testing.T) {
	srv := NewServer(discardLogger())
	req := Request{
		JSONRPC: JSONRPCVersion,
		ID:      json.RawMessage(`99`),
		Method:  "unknown/request",
	}

	resp, skip := srv.handleLineWithSkip(mustMarshal(t, req))
	if skip {
		t.Fatal("expected unknown request with id to produce an error response")
	}
	if resp.Error == nil || resp.Error.Code != CodeMethodNotFound {
		t.Fatalf("expected method-not-found error, got %+v", resp.Error)
	}
}

func TestServerToolsList_ExactMatch(t *testing.T) {
	srv := NewServer(discardLogger())
	req := Request{
		JSONRPC: JSONRPCVersion,
		ID:      json.RawMessage(`1`),
		Method:  "tools/list",
	}
	resp := srv.handleLine(mustMarshal(t, req))
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}

	var list ToolsListResult
	b, _ := json.Marshal(resp.Result)
	if err := json.Unmarshal(b, &list); err != nil {
		t.Fatalf("unmarshal tools list: %v", err)
	}

	expectedTools := []string{
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
		"get_next_pass_work",
		"get_next_audit_work",
		"create_source_snapshot",
		"list_project_files",
		"search_project_files",
		"read_project_file",
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

	if len(list.Tools) != len(expectedTools) {
		t.Fatalf("expected exactly %d tools, got %d", len(expectedTools), len(list.Tools))
	}

	for i, name := range expectedTools {
		if list.Tools[i].Name != name {
			t.Fatalf("expected tool at %d to be %q, got %q", i, name, list.Tools[i].Name)
		}
	}
}

func TestServerToolsList_BrokerEnabled_ExactMatch(t *testing.T) {
	deps := setupTestDeps(t)
	deps.ToolProfile = ToolProfileLocalOperator
	deps.ContextBrokerEnabled = true
	srv := NewServer(discardLogger(), deps)
	req := Request{
		JSONRPC: JSONRPCVersion,
		ID:      json.RawMessage(`1`),
		Method:  "tools/list",
	}
	resp := srv.handleLine(mustMarshal(t, req))
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}

	var list ToolsListResult
	b, _ := json.Marshal(resp.Result)
	if err := json.Unmarshal(b, &list); err != nil {
		t.Fatalf("unmarshal tools list: %v", err)
	}

	expectedTools := []string{
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
		"get_next_pass_work",
		"get_next_audit_work",
		"create_source_snapshot",
		"list_project_files",
		"search_project_files",
		"read_project_file",
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

	if len(list.Tools) != len(expectedTools) {
		t.Fatalf("expected exactly %d tools, got %d", len(expectedTools), len(list.Tools))
	}

	for i, name := range expectedTools {
		if list.Tools[i].Name != name {
			t.Fatalf("expected tool at %d to be %q, got %q", i, name, list.Tools[i].Name)
		}
	}
}

func TestHandleLineWithSkipPingRequestResponds(t *testing.T) {
	srv := NewServer(discardLogger())
	req := Request{
		JSONRPC: JSONRPCVersion,
		ID:      json.RawMessage(`1`),
		Method:  "ping",
	}
	resp, skip := srv.handleLineWithSkip(mustMarshal(t, req))
	if skip {
		t.Fatal("expected ping request with id not to be skipped")
	}
	if resp.Error != nil {
		t.Fatalf("expected ping success, got error %+v", resp.Error)
	}
}
