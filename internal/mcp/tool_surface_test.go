package mcp

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestCanonicalServerToolSurfaceByProfile(t *testing.T) {
	tests := []struct {
		name    string
		profile ToolProfile
		want    []string
	}{
		{name: "planner", profile: ToolProfilePlanner, want: []string{"validate_artifact", "submit_plan", "get_plan", "create_run"}},
		{name: "auditor", profile: ToolProfileAuditor, want: []string{"validate_artifact", "create_run"}},
		{name: "local operator", profile: ToolProfileLocalOperator, want: []string{"validate_artifact", "submit_plan", "get_plan", "create_run"}},
		{name: "invalid fails closed", profile: ToolProfile("restricted"), want: []string{"validate_artifact", "submit_plan", "get_plan", "create_run"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := NewServer(nil, &MCPDeps{ToolProfile: tt.profile})
			if got := toolNames(srv.tools); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("tools = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLegacyToolsAreUnregisteredForEveryCanonicalProfile(t *testing.T) {
	legacyTools := []string{
		"submit_test_audit_packet",
		"create_run_from_planner_handoff",
		"create_run_from_planner_handoff_file",
		"validate_planner_handoff_for_compile",
		"submit_planner_pass_plan",
		"list_open_runs",
		"get_run_status",
		"submit_audit_packet",
		"get_project",
		"get_pass",
		"create_context_packet",
		"create_plan_seed",
	}
	for _, profile := range []ToolProfile{ToolProfilePlanner, ToolProfileAuditor, ToolProfileLocalOperator} {
		t.Run(string(profile), func(t *testing.T) {
			srv := NewServer(nil, &MCPDeps{ToolProfile: profile})
			for _, name := range legacyTools {
				params, _ := json.Marshal(ToolCallParams{Name: name, Arguments: json.RawMessage(`{}`)})
				resp := srv.handleLine(mustMarshal(t, Request{
					JSONRPC: JSONRPCVersion,
					ID:      json.RawMessage(`1`),
					Method:  "tools/call",
					Params:  params,
				}))
				if resp.Error == nil || resp.Error.Code != CodeMethodNotFound {
					t.Fatalf("profile %q legacy tool %q returned %+v", profile, name, resp.Error)
				}
			}
		})
	}
}

func TestCanonicalToolNamesDoNotExposeUnsafeCapabilities(t *testing.T) {
	unsafe := []string{"shell", "exec", "read_file", "write_file", "git_commit", "git_push", "checkout", "reset", "merge"}
	for _, tool := range NewServer(nil).tools {
		for _, keyword := range unsafe {
			if strings.Contains(strings.ToLower(tool.Name), keyword) {
				t.Fatalf("tool %q contains unsafe keyword %q", tool.Name, keyword)
			}
		}
	}
}
