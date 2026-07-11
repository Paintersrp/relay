package mcp

import (
	"encoding/json"
	"testing"
)

func finalToolCallResponse(t *testing.T, server *Server, name string) Response {
	t.Helper()
	params, err := json.Marshal(ToolCallParams{Name: name, Arguments: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	return server.handleToolsCall(Request{ID: json.RawMessage(`1`), Params: params})
}

func TestFinalAdvertisedToolsReachDispatcher(t *testing.T) {
	for _, profile := range []ToolProfile{
		ToolProfilePlanner,
		ToolProfileAuditor,
		ToolProfileLocalOperator,
	} {
		t.Run(string(profile), func(t *testing.T) {
			server := NewServer(nil, &MCPDeps{ToolProfile: profile})
			for _, tool := range workflowToolDefinitions(profile) {
				t.Run(tool.Name, func(t *testing.T) {
					response := finalToolCallResponse(t, server, tool.Name)
					if response.Error != nil {
						t.Fatalf("advertised tool %q rejected before handler dispatch: %+v", tool.Name, response.Error)
					}
				})
			}
		})
	}
}

func TestFinalCanonicalProfileIsolation(t *testing.T) {
	tests := []struct {
		profile     ToolProfile
		unavailable []string
	}{
		{
			profile:     ToolProfilePlanner,
			unavailable: []string{"get_audit_packet", "get_run_artifact", "record_audit_decision"},
		},
		{
			profile:     ToolProfileAuditor,
			unavailable: []string{"list_projects", "submit_plan", "get_plan"},
		},
	}
	for _, tt := range tests {
		t.Run(string(tt.profile), func(t *testing.T) {
			server := NewServer(nil, &MCPDeps{ToolProfile: tt.profile})
			for _, name := range tt.unavailable {
				response := finalToolCallResponse(t, server, name)
				if response.Error == nil || response.Error.Code != CodeMethodNotFound {
					t.Fatalf("profile %q unavailable tool %q response = %+v", tt.profile, name, response)
				}
			}
		})
	}
}

func TestFinalServerRejectsUnknownTool(t *testing.T) {
	server := NewServer(nil, &MCPDeps{ToolProfile: ToolProfileLocalOperator})
	response := finalToolCallResponse(t, server, "not_a_relay_tool")
	if response.Error == nil || response.Error.Code != CodeMethodNotFound {
		t.Fatalf("unknown tool response = %+v", response)
	}
}
