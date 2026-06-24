package artifacts

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestPlanPathValidJSON(t *testing.T) {
	got, err := PlanPath("2026-06-24", "refactor-plan-relay-5e7a9c02", "planner_pass_plan_json")
	if err != nil {
		t.Fatalf("PlanPath returned unexpected error: %v", err)
	}
	want := filepath.Join(BaseDir, "handoffs", "plans", "2026-06-24_refactor-plan-relay-5e7a9c02.planner-pass-plan.json")
	if got != want {
		t.Fatalf("PlanPath = %q, want %q", got, want)
	}
}

func TestPlanPathValidMarkdown(t *testing.T) {
	got, err := PlanPath("2026-06-24", "refactor-plan-relay-5e7a9c02", "planner_pass_plan_markdown")
	if err != nil {
		t.Fatalf("PlanPath returned unexpected error: %v", err)
	}
	if !strings.HasSuffix(got, ".planner-pass-plan.md") {
		t.Fatalf("expected markdown suffix, got %q", got)
	}
}

func TestPlanPathRejectsInvalidInputs(t *testing.T) {
	cases := []struct {
		name string
		date string
		slug string
		kind string
	}{
		{"bad date", "2026/06/24", "refactor-plan-relay", "planner_pass_plan_json"},
		{"short date", "26-6-24", "refactor-plan-relay", "planner_pass_plan_json"},
		{"bad slug uppercase", "2026-06-24", "Refactor-Plan", "planner_pass_plan_json"},
		{"bad slug slash", "2026-06-24", "refactor/plan", "planner_pass_plan_json"},
		{"bad slug dotdot", "2026-06-24", "..", "planner_pass_plan_json"},
		{"unknown kind", "2026-06-24", "refactor-plan-relay", "not_a_kind"},
		{"non-plan kind", "2026-06-24", "refactor-plan-relay", "context_packet_json"},
		{"empty slug", "2026-06-24", "", "planner_pass_plan_json"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := PlanPath(tc.date, tc.slug, tc.kind); err == nil {
				t.Fatalf("expected error for %s, got nil", tc.name)
			}
		})
	}
}

func TestPlanPathStaysWithinPlanDir(t *testing.T) {
	got, err := PlanPath("2026-06-24", "refactor-plan-relay-5e7a9c02", "planner_pass_plan_json")
	if err != nil {
		t.Fatalf("PlanPath returned unexpected error: %v", err)
	}
	cleanDir := filepath.Clean(PlanDir())
	if !strings.HasPrefix(filepath.Clean(got), cleanDir+string(filepath.Separator)) {
		t.Fatalf("plan path %q escapes plan dir %q", got, cleanDir)
	}
}

func TestWritePlanRoundTrips(t *testing.T) {
	orig := BaseDir
	t.Cleanup(func() { SetBaseDir(orig) })
	SetBaseDir(t.TempDir())

	data := []byte(`{"plan_meta":{}}`)
	p, err := WritePlan("2026-06-24", "refactor-plan-relay-abcd1234", "planner_pass_plan_json", data)
	if err != nil {
		t.Fatalf("WritePlan returned unexpected error: %v", err)
	}
	if !strings.HasSuffix(p, "2026-06-24_refactor-plan-relay-abcd1234.planner-pass-plan.json") {
		t.Fatalf("unexpected written path %q", p)
	}
}
