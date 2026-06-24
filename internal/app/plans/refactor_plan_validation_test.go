package plans

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// validRefactorPlan builds on the shared valid Plan v2 fixture and layers on
// schema-approved refactor metadata: optional generated refactor-only plan
// metadata plus a single scheduled refactor pass carrying a refactor_candidate.
func validRefactorPlan() PlannerPassPlan {
	plan := validPlannerPassPlan()

	plan.PlanMeta.RefactorPlanMetadata = &RefactorPlanMetadata{
		Source:             "selected_refactor_candidates",
		SourceCandidateIDs: []string{"refactor-candidate-a", "refactor-candidate-b"},
		SubmissionPolicy:   "review_required_no_auto_submit",
		Notes:              "Generated refactor-only plan; review required before submission.",
	}

	// Turn the first pass into a scheduled refactor pass. The second pass stays
	// a non-refactor pass with no refactor_candidate.
	plan.Passes[0].PassType = "refactor"
	plan.Passes[0].RefactorCandidate = &RefactorCandidateMetadata{
		CandidateID:            "refactor-candidate-a",
		Source:                 "refactor_backlog_candidate",
		SchedulingMode:         "generated_refactor_only_plan",
		SourceDiscoveryTaskIDs: []string{"refactor-discovery-scan-1"},
	}

	return plan
}

func newValidationOnlyService() *Service {
	return NewService(nil)
}

func TestValidatePlanAcceptsRefactorMetadata(t *testing.T) {
	t.Parallel()

	svc := newValidationOnlyService()
	raw := mustMarshalPlan(t, validRefactorPlan())

	plan, report, err := svc.ValidatePlanJSON(context.Background(), raw)
	if err != nil {
		t.Fatalf("ValidatePlanJSON returned error: %v", err)
	}
	if !report.Valid {
		t.Fatalf("expected valid report, got issues: %+v", report.Issues)
	}
	if plan == nil {
		t.Fatal("expected non-nil plan for valid refactor metadata")
	}

	// Plan-level refactor metadata is preserved through typed unmarshal.
	if plan.PlanMeta.RefactorPlanMetadata == nil {
		t.Fatal("expected plan_meta.refactor_plan_metadata to be preserved")
	}
	if plan.PlanMeta.RefactorPlanMetadata.Source != "selected_refactor_candidates" {
		t.Fatalf("unexpected refactor_plan_metadata.source: %q", plan.PlanMeta.RefactorPlanMetadata.Source)
	}
	if plan.PlanMeta.RefactorPlanMetadata.SubmissionPolicy != "review_required_no_auto_submit" {
		t.Fatalf("unexpected refactor_plan_metadata.submission_policy: %q", plan.PlanMeta.RefactorPlanMetadata.SubmissionPolicy)
	}
	if len(plan.PlanMeta.RefactorPlanMetadata.SourceCandidateIDs) != 2 {
		t.Fatalf("expected 2 source_candidate_ids, got %v", plan.PlanMeta.RefactorPlanMetadata.SourceCandidateIDs)
	}

	// Pass-level refactor metadata is preserved through typed unmarshal.
	if plan.Passes[0].RefactorCandidate == nil {
		t.Fatal("expected passes[0].refactor_candidate to be preserved")
	}
	if plan.Passes[0].RefactorCandidate.CandidateID != "refactor-candidate-a" {
		t.Fatalf("unexpected refactor_candidate.candidate_id: %q", plan.Passes[0].RefactorCandidate.CandidateID)
	}
	if plan.Passes[0].RefactorCandidate.Source != "refactor_backlog_candidate" {
		t.Fatalf("unexpected refactor_candidate.source: %q", plan.Passes[0].RefactorCandidate.Source)
	}
	if plan.Passes[0].RefactorCandidate.SchedulingMode != "generated_refactor_only_plan" {
		t.Fatalf("unexpected refactor_candidate.scheduling_mode: %q", plan.Passes[0].RefactorCandidate.SchedulingMode)
	}

	// The non-refactor pass must not carry refactor_candidate metadata.
	if plan.Passes[1].RefactorCandidate != nil {
		t.Fatal("expected non-refactor pass to have nil refactor_candidate")
	}
}

func TestRefactorPassMarshalPreservesRefactorCandidate(t *testing.T) {
	t.Parallel()

	plan := validRefactorPlan()

	encoded, err := json.Marshal(plan.Passes[0])
	if err != nil {
		t.Fatalf("json.Marshal pass: %v", err)
	}
	if !strings.Contains(string(encoded), `"refactor_candidate"`) {
		t.Fatalf("expected marshaled refactor pass to include refactor_candidate, got: %s", string(encoded))
	}

	// A non-refactor pass with nil refactor_candidate must omit the field.
	encodedNonRefactor, err := json.Marshal(plan.Passes[1])
	if err != nil {
		t.Fatalf("json.Marshal non-refactor pass: %v", err)
	}
	if strings.Contains(string(encodedNonRefactor), `"refactor_candidate"`) {
		t.Fatalf("expected non-refactor pass to omit refactor_candidate, got: %s", string(encodedNonRefactor))
	}
}

func TestValidatePlanRejectsRefactorMetadataDrift(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(*PlannerPassPlan)
	}{
		{
			name: "refactor pass missing refactor_candidate",
			mutate: func(plan *PlannerPassPlan) {
				plan.Passes[0].RefactorCandidate = nil
			},
		},
		{
			name: "non-refactor pass carries refactor_candidate",
			mutate: func(plan *PlannerPassPlan) {
				plan.Passes[1].PassType = "backend_vertical_slice"
				plan.Passes[1].RefactorCandidate = &RefactorCandidateMetadata{
					CandidateID:    "refactor-candidate-z",
					Source:         "refactor_backlog_candidate",
					SchedulingMode: "existing_plan_bonus_pass",
				}
			},
		},
		{
			name: "wrong refactor_candidate source",
			mutate: func(plan *PlannerPassPlan) {
				plan.Passes[0].RefactorCandidate.Source = "refactor_candidate"
			},
		},
		{
			name: "wrong refactor_candidate scheduling_mode",
			mutate: func(plan *PlannerPassPlan) {
				plan.Passes[0].RefactorCandidate.SchedulingMode = "bonus_managed_pass"
			},
		},
		{
			name: "duplicate source_discovery_task_ids",
			mutate: func(plan *PlannerPassPlan) {
				plan.Passes[0].RefactorCandidate.SourceDiscoveryTaskIDs = []string{"dup", "dup"}
			},
		},
		{
			name: "empty source_discovery_task_id value",
			mutate: func(plan *PlannerPassPlan) {
				plan.Passes[0].RefactorCandidate.SourceDiscoveryTaskIDs = []string{""}
			},
		},
		{
			name: "wrong refactor_plan_metadata submission_policy",
			mutate: func(plan *PlannerPassPlan) {
				plan.PlanMeta.RefactorPlanMetadata.SubmissionPolicy = "auto_submit"
			},
		},
		{
			name: "wrong refactor_plan_metadata source",
			mutate: func(plan *PlannerPassPlan) {
				plan.PlanMeta.RefactorPlanMetadata.Source = "refactor_candidate"
			},
		},
		{
			name: "empty refactor_plan_metadata source_candidate_ids",
			mutate: func(plan *PlannerPassPlan) {
				plan.PlanMeta.RefactorPlanMetadata.SourceCandidateIDs = []string{}
			},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			svc := newValidationOnlyService()
			plan := validRefactorPlan()
			tc.mutate(&plan)

			_, report, err := svc.ValidatePlanJSON(context.Background(), mustMarshalPlan(t, plan))
			if err != nil {
				t.Fatalf("ValidatePlanJSON returned error: %v", err)
			}
			assertIssueCode(t, report, IssuePlanRefactorMetadataInvalid)
		})
	}
}
