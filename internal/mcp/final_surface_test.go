package mcp

import (
	"encoding/json"
	"testing"
)

func finalToolNames(tools []ToolDefinition) []string {
	out := make([]string, len(tools))
	for i, tool := range tools {
		out[i] = tool.Name
	}
	return out
}

func TestFinalCanonicalProfileInventories(t *testing.T) {
	tests := []struct {
		profile ToolProfile
		want    []string
	}{
		{ToolProfilePlanner, []string{"validate_artifact", "list_projects", "submit_plan", "get_plan", "create_run"}},
		{ToolProfileAuditor, []string{"validate_artifact", "create_run", "get_audit_packet", "get_run_artifact", "record_audit_decision"}},
		{ToolProfileLocalOperator, []string{"validate_artifact", "list_projects", "submit_plan", "get_plan", "create_run", "get_audit_packet", "get_run_artifact", "record_audit_decision"}},
	}
	for _, tt := range tests {
		got := finalToolNames(canonicalToolDefinitions(tt.profile))
		if len(got) != len(tt.want) {
			t.Fatalf("%s tools = %v, want %v", tt.profile, got, tt.want)
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Fatalf("%s tools = %v, want %v", tt.profile, got, tt.want)
			}
		}
	}
}

func TestFinalServerRejectsRetiredToolNames(t *testing.T) {
	server := NewServer(nil, &MCPDeps{ToolProfile: ToolProfileLocalOperator})
	for _, name := range []string{
		"create_run_from_planner_handoff", "validate_planner_handoff_for_compile",
		"create_plan_attempt_with_intent", "create_plan_seed", "get_pass_context",
		"create_context_packet", "create_local_audit", "list_refactor_candidates",
	} {
		params, _ := json.Marshal(ToolCallParams{Name: name, Arguments: json.RawMessage(`{}`)})
		response := server.handleToolsCall(Request{ID: json.RawMessage(`1`), Params: params})
		if response.Error == nil || response.Error.Code != CodeMethodNotFound {
			t.Fatalf("retired tool %q response = %+v", name, response)
		}
	}
}
