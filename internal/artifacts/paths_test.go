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

func TestCloseoutPathValidJSON(t *testing.T) {
	got, err := CloseoutPath("2026-06-30", "closeout-evidence-model", "closeout_evidence_json")
	if err != nil {
		t.Fatalf("CloseoutPath returned unexpected error: %v", err)
	}
	want := filepath.Join(BaseDir, "handoffs", "closeout", "2026-06-30_closeout-evidence-model.closeout-evidence.json")
	if got != want {
		t.Fatalf("CloseoutPath = %q, want %q", got, want)
	}
}

func TestCloseoutPathValidMarkdown(t *testing.T) {
	got, err := CloseoutPath("2026-06-30", "closeout-evidence-model", "closeout_evidence_markdown")
	if err != nil {
		t.Fatalf("CloseoutPath returned unexpected error: %v", err)
	}
	if !strings.HasSuffix(got, ".closeout-evidence.md") {
		t.Fatalf("expected markdown suffix, got %q", got)
	}
}

func TestCloseoutPathRejectsUnknownKind(t *testing.T) {
	if _, err := CloseoutPath("2026-06-30", "closeout-evidence-model", "unknown_closeout_kind"); err == nil {
		t.Fatal("expected unknown closeout artifact kind to be rejected")
	}
}

func TestIsAllowedKindKnown(t *testing.T) {
	for _, kind := range []string{"canonical_packet", "validation_run_json", "planner_handoff", "executor_result"} {
		if !IsAllowedKind(kind) {
			t.Fatalf("IsAllowedKind(%q) = false, want true", kind)
		}
	}
}

func TestIsAllowedKindUnknown(t *testing.T) {
	if IsAllowedKind("nonexistent_kind_xyz") {
		t.Fatal("IsAllowedKind for unknown kind should return false")
	}
	if IsAllowedKind("") {
		t.Fatal("IsAllowedKind for empty string should return false")
	}
}

func TestRunDirContainsValid(t *testing.T) {
	orig := BaseDir
	t.Cleanup(func() { SetBaseDir(orig) })
	SetBaseDir(t.TempDir())

	dir := Dir(42)
	subPath := filepath.Join(dir, "sub", "file.txt")
	if !RunDirContains(42, dir) {
		t.Fatal("RunDirContains should accept the run dir itself")
	}
	if !RunDirContains(42, subPath) {
		t.Fatalf("RunDirContains should accept a subpath under the run dir: %q", subPath)
	}
}

func TestRunDirContainsRejectsEscape(t *testing.T) {
	orig := BaseDir
	t.Cleanup(func() { SetBaseDir(orig) })
	SetBaseDir(t.TempDir())

	dir := Dir(42)
	escaped := filepath.Join(dir, "..", "other")
	if RunDirContains(42, escaped) {
		t.Fatal("RunDirContains should reject path escaping run dir")
	}
}

func TestRunDirContainsDifferentRun(t *testing.T) {
	orig := BaseDir
	t.Cleanup(func() { SetBaseDir(orig) })
	SetBaseDir(t.TempDir())

	dir2 := Dir(2)
	if RunDirContains(1, dir2) {
		t.Fatal("RunDirContains should not accept a different run's directory")
	}
}
