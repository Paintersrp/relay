package mcp

import "testing"

func TestCanonicalToolDefinitionsByProfile(t *testing.T) {
	cases := []struct {
		name    string
		profile ToolProfile
		want    []string
	}{
		{name: "planner", profile: ToolProfilePlanner, want: []string{"validate_artifact", "submit_plan", "get_plan", "create_run"}},
		{name: "auditor", profile: ToolProfileAuditor, want: []string{"validate_artifact", "create_run"}},
		{name: "local operator", profile: ToolProfileLocalOperator, want: []string{"validate_artifact", "submit_plan", "get_plan", "create_run"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := canonicalToolDefinitions(tc.profile)
			if len(got) != len(tc.want) {
				t.Fatalf("tool count = %d, want %d", len(got), len(tc.want))
			}
			for i, tool := range got {
				if tool.Name != tc.want[i] {
					t.Fatalf("tool[%d] = %q, want %q", i, tool.Name, tc.want[i])
				}
			}
		})
	}
}
