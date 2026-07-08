package mcp

import "testing"

func TestNormalizeToolProfileCanonicalValues(t *testing.T) {
	tests := []struct {
		name   string
		raw    string
		want   ToolProfile
		wantOK bool
	}{
		{name: "empty defaults planner", raw: "", want: ToolProfilePlanner, wantOK: true},
		{name: "planner", raw: "planner", want: ToolProfilePlanner, wantOK: true},
		{name: "auditor", raw: "auditor", want: ToolProfileAuditor, wantOK: true},
		{name: "local operator", raw: "local_operator", want: ToolProfileLocalOperator, wantOK: true},
		{name: "trimmed case insensitive", raw: "  AUDITOR  ", want: ToolProfileAuditor, wantOK: true},
		{name: "legacy hyphenated local operator", raw: "local-operator", want: ToolProfilePlanner, wantOK: false},
		{name: "legacy restricted", raw: "restricted", want: ToolProfilePlanner, wantOK: false},
		{name: "legacy audit", raw: "audit", want: ToolProfilePlanner, wantOK: false},
		{name: "unknown", raw: "unknown-profile", want: ToolProfilePlanner, wantOK: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := NormalizeToolProfile(tt.raw)
			if got != tt.want || ok != tt.wantOK {
				t.Fatalf("NormalizeToolProfile(%q) = (%q, %v), want (%q, %v)", tt.raw, got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

func TestToolProfileFromEnvDefaultsAndFailsClosedToPlanner(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want ToolProfile
	}{
		{name: "unset", raw: "", want: ToolProfilePlanner},
		{name: "planner", raw: "planner", want: ToolProfilePlanner},
		{name: "auditor", raw: "auditor", want: ToolProfileAuditor},
		{name: "local operator", raw: "local_operator", want: ToolProfileLocalOperator},
		{name: "restricted", raw: "restricted", want: ToolProfilePlanner},
		{name: "audit", raw: "audit", want: ToolProfilePlanner},
		{name: "hyphenated local operator", raw: "local-operator", want: ToolProfilePlanner},
		{name: "unknown", raw: "not-real", want: ToolProfilePlanner},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(EnvMCPProfile, tt.raw)
			if got := ToolProfileFromEnv(nil); got != tt.want {
				t.Fatalf("ToolProfileFromEnv() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestWorkflowDepsCarryProfile(t *testing.T) {
	for _, profile := range []string{"planner", "auditor", "local_operator", "restricted"} {
		t.Run(profile, func(t *testing.T) {
			t.Setenv(EnvMCPProfile, profile)
			deps := NewWorkflowDepsFromEnv(nil, nil)
			want, _ := NormalizeToolProfile(profile)
			if deps.ToolProfile != want {
				t.Fatalf("ToolProfile = %q, want %q", deps.ToolProfile, want)
			}
		})
	}
}
