package refactors

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"relay/internal/artifacts"
	"relay/internal/plans"
	"relay/internal/store"
	"relay/internal/store/generated"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

func seedPlan(t *testing.T, st *store.Store, project *store.Project, planID, status string) store.Plan {
	t.Helper()
	q := generated.New(st.DB())
	plan, err := q.CreatePlan(context.Background(), generated.CreatePlanParams{
		PlanID:                   planID,
		SchemaVersion:            "2.0.0",
		Title:                    "Seed plan",
		Goal:                     "Seed goal",
		RepoTarget:               "Paintersrp/relay",
		BranchContext:            "main",
		Status:                   status,
		SourceIntentSummary:      "seed",
		PlanMetaJson:             "{}",
		ProjectContextJson:       "{}",
		McpCapabilityProfileJson: "{}",
		GlobalContextRulesJson:   "{}",
		RawPlanJson:              "{}",
		ProjectRowID:             project.ID,
		ProjectID:                project.ProjectID,
	})
	if err != nil {
		t.Fatalf("seedPlan(%s) failed: %v", planID, err)
	}
	return plan
}

func seedPass(t *testing.T, st *store.Store, planRowID int64, passID string, sequence int64, seedPaths, scope []string) {
	t.Helper()
	q := generated.New(st.DB())

	seedFiles := make([]plans.ContextFileRead, 0, len(seedPaths))
	for _, p := range seedPaths {
		req := true
		seedFiles = append(seedFiles, plans.ContextFileRead{RepoID: "relay", Path: p, Purpose: "seed", Required: &req})
	}
	raw := plans.PlanPassInput{
		PassID:                 passID,
		Sequence:               sequence,
		Name:                   passID,
		Goal:                   "seed",
		IntendedExecutionScope: scope,
		Status:                 "planned",
		PassType:               "implementation",
		ContextPlan:            plans.ContextPlan{SeedFilesToRead: seedFiles},
	}
	rawJSON, _ := json.Marshal(raw)
	scopeJSON, _ := json.Marshal(scope)

	if _, err := q.CreatePlanPass(context.Background(), generated.CreatePlanPassParams{
		PlanRowID:                      planRowID,
		PassID:                         passID,
		Sequence:                       sequence,
		Name:                           passID,
		Goal:                           "seed",
		IntendedExecutionScopeJson:     string(scopeJSON),
		NonGoalsJson:                   "[]",
		DependenciesJson:               "[]",
		Status:                         "planned",
		PassType:                       "implementation",
		ContextPlanJson:                "{}",
		SourceSnapshotRequirementsJson: "{}",
		HandoffReadinessCriteriaJson:   "[]",
		ContextBudgetJson:              "{}",
		RawPassJson:                    string(rawJSON),
	}); err != nil {
		t.Fatalf("seedPass(%s) failed: %v", passID, err)
	}
}

func mustCreateCandidate(t *testing.T, svc *Service, projectID, candidateID string) {
	t.Helper()
	if _, issues, err := svc.CreateCandidate(context.Background(), projectID, validCandidateInput(candidateID, candidateID)); err != nil || len(issues) > 0 {
		t.Fatalf("create candidate %s failed: err=%v issues=%+v", candidateID, err, issues)
	}
}

// ---------------------------------------------------------------------------
// Placement suggestion (S4)
// ---------------------------------------------------------------------------

func TestPlacementSuggestionDeterministic(t *testing.T) {
	svc, st := newTestService(t)
	ctx := context.Background()
	project := mustProject(t, st, "proj")
	mustCreateCandidate(t, svc, "proj", "cand-1")

	t.Run("exact_file_overlap_prefers_highest_sequence", func(t *testing.T) {
		plan := seedPlan(t, st, project, "plan-exact", "active")
		seedPass(t, st, plan.ID, "PASS-001", 1, []string{"internal/other/x.go"}, nil)
		seedPass(t, st, plan.ID, "PASS-002", 2, []string{"internal/foo/bar.go"}, nil)
		seedPass(t, st, plan.ID, "PASS-003", 3, []string{"internal/foo/bar.go"}, nil)

		sug, issues, err := svc.SuggestCandidatePlacement(ctx, "proj", "cand-1", "plan-exact")
		if err != nil || len(issues) > 0 {
			t.Fatalf("placement failed: err=%v issues=%+v", err, issues)
		}
		if sug.PlacementReason != PlacementExactFileOverlap || sug.Confidence != PlacementConfidenceHigh {
			t.Fatalf("expected exact_file_overlap/high, got %+v", sug)
		}
		if sug.AfterPassID != "PASS-003" {
			t.Fatalf("expected highest-sequence PASS-003, got %q", sug.AfterPassID)
		}
	})

	t.Run("same_directory", func(t *testing.T) {
		plan := seedPlan(t, st, project, "plan-dir", "active")
		seedPass(t, st, plan.ID, "PASS-001", 1, []string{"internal/foo/baz.go"}, nil)

		sug, _, err := svc.SuggestCandidatePlacement(ctx, "proj", "cand-1", "plan-dir")
		if err != nil {
			t.Fatalf("placement failed: %v", err)
		}
		if sug.PlacementReason != PlacementSameDirectory || sug.Confidence != PlacementConfidenceMedium {
			t.Fatalf("expected same_directory/medium, got %+v", sug)
		}
	})

	t.Run("same_subsystem", func(t *testing.T) {
		// Candidate target is internal/foo/bar.go (subsystem internal/foo, dir internal/foo).
		// Pass path internal/foo/sub/deep.go shares subsystem but not directory.
		plan := seedPlan(t, st, project, "plan-sub", "active")
		seedPass(t, st, plan.ID, "PASS-001", 1, []string{"internal/foo/sub/deep.go"}, nil)

		sug, _, err := svc.SuggestCandidatePlacement(ctx, "proj", "cand-1", "plan-sub")
		if err != nil {
			t.Fatalf("placement failed: %v", err)
		}
		if sug.PlacementReason != PlacementSameSubsystem || sug.Confidence != PlacementConfidenceLow {
			t.Fatalf("expected same_subsystem/low, got %+v", sug)
		}
	})

	t.Run("no_suggestion", func(t *testing.T) {
		plan := seedPlan(t, st, project, "plan-none", "active")
		seedPass(t, st, plan.ID, "PASS-001", 1, []string{"cmd/server/main.go"}, nil)

		sug, _, err := svc.SuggestCandidatePlacement(ctx, "proj", "cand-1", "plan-none")
		if err != nil {
			t.Fatalf("placement failed: %v", err)
		}
		if sug.PlacementReason != PlacementNoSuggestion || sug.Confidence != PlacementConfidenceNone || sug.AfterPassID != "" {
			t.Fatalf("expected no_suggestion/none/empty, got %+v", sug)
		}
	})
}

// ---------------------------------------------------------------------------
// Existing-plan promotion (S5)
// ---------------------------------------------------------------------------

func candidateRow(t *testing.T, st *store.Store, projectRowID int64, candidateID string) *store.RefactorCandidate {
	t.Helper()
	row, err := st.GetRefactorCandidateByCandidateID(projectRowID, candidateID)
	if err != nil {
		t.Fatalf("get candidate %s failed: %v", candidateID, err)
	}
	return row
}

func TestPromoteCandidateAppendSuccess(t *testing.T) {
	svc, st := newTestService(t)
	ctx := context.Background()
	project := mustProject(t, st, "proj")
	mustCreateCandidate(t, svc, "proj", "cand-1")
	plan := seedPlan(t, st, project, "plan-1", "active")
	seedPass(t, st, plan.ID, "PASS-001", 1, []string{"internal/foo/bar.go"}, nil)

	result, issues, err := svc.PromoteCandidateToPlan(ctx, PromoteCandidateInput{
		ProjectID:   "proj",
		CandidateID: "cand-1",
		PlanID:      "plan-1",
	})
	if err != nil || len(issues) > 0 {
		t.Fatalf("promote failed: err=%v issues=%+v", err, issues)
	}
	if result.PassID != "PASS-002" || result.Sequence != 2 {
		t.Fatalf("expected PASS-002 seq 2, got %+v", result)
	}
	if result.CandidateStatus != CandidateStatusScheduled {
		t.Fatalf("expected scheduled, got %q", result.CandidateStatus)
	}

	// Created pass is a normal managed refactor pass with refactor_candidate metadata.
	pass, err := st.GetPlanPassByPassID(plan.ID, "PASS-002")
	if err != nil {
		t.Fatalf("get promoted pass failed: %v", err)
	}
	if pass.PassType != "refactor" {
		t.Fatalf("expected pass_type refactor, got %q", pass.PassType)
	}
	var raw plans.PlanPassInput
	if err := json.Unmarshal([]byte(pass.RawPassJson), &raw); err != nil {
		t.Fatalf("unmarshal raw_pass_json: %v", err)
	}
	if raw.RefactorCandidate == nil || raw.RefactorCandidate.CandidateID != "cand-1" {
		t.Fatalf("expected refactor_candidate.candidate_id cand-1, got %+v", raw.RefactorCandidate)
	}
	if raw.RefactorCandidate.SchedulingMode != schedulingModeExistingPlan || raw.RefactorCandidate.Source != refactorCandidateSource {
		t.Fatalf("unexpected refactor_candidate metadata: %+v", raw.RefactorCandidate)
	}

	// Scheduling reference and status event exist.
	row := candidateRow(t, st, project.ID, "cand-1")
	active, err := st.GetActiveRefactorCandidateScheduleRef(project.ID, row.ID)
	if err != nil || active == nil {
		t.Fatalf("expected active schedule ref, err=%v ref=%+v", err, active)
	}
	if active.PlanID != "plan-1" || active.PassID != "PASS-002" || active.RunID != "" {
		t.Fatalf("unexpected schedule ref: %+v", active)
	}
	events, _ := st.ListRefactorCandidateStatusEvents(project.ID, row.ID, 0)
	sawScheduled := false
	for _, e := range events {
		if e.EventType == "scheduled" && e.FromStatus == CandidateStatusReady && e.ToStatus == CandidateStatusScheduled {
			sawScheduled = true
		}
	}
	if !sawScheduled {
		t.Fatalf("expected scheduled status event, got %+v", events)
	}
}

func TestPromoteMiddleInsertionBumpsSequences(t *testing.T) {
	svc, st := newTestService(t)
	ctx := context.Background()
	project := mustProject(t, st, "proj")
	mustCreateCandidate(t, svc, "proj", "cand-1")
	plan := seedPlan(t, st, project, "plan-1", "active")
	seedPass(t, st, plan.ID, "PASS-001", 1, nil, nil)
	seedPass(t, st, plan.ID, "PASS-002", 2, nil, nil)
	seedPass(t, st, plan.ID, "PASS-003", 3, nil, nil)

	result, issues, err := svc.PromoteCandidateToPlan(ctx, PromoteCandidateInput{
		ProjectID:   "proj",
		CandidateID: "cand-1",
		PlanID:      "plan-1",
		AfterPassID: "PASS-001",
	})
	if err != nil || len(issues) > 0 {
		t.Fatalf("promote failed: err=%v issues=%+v", err, issues)
	}
	if result.PassID != "PASS-004" || result.Sequence != 2 {
		t.Fatalf("expected PASS-004 at seq 2, got %+v", result)
	}

	passes, err := st.ListPlanPassesByPlan(plan.ID)
	if err != nil {
		t.Fatalf("list passes failed: %v", err)
	}
	seqByID := map[string]int64{}
	seenSeq := map[int64]bool{}
	for _, p := range passes {
		if seenSeq[p.Sequence] {
			t.Fatalf("duplicate sequence %d detected", p.Sequence)
		}
		seenSeq[p.Sequence] = true
		seqByID[p.PassID] = p.Sequence
	}
	if seqByID["PASS-001"] != 1 || seqByID["PASS-004"] != 2 || seqByID["PASS-002"] != 3 || seqByID["PASS-003"] != 4 {
		t.Fatalf("unexpected sequences: %+v", seqByID)
	}
}

func TestPromoteBlockedCases(t *testing.T) {
	ctx := context.Background()

	t.Run("missing_plan_id", func(t *testing.T) {
		svc, st := newTestService(t)
		mustProject(t, st, "proj")
		mustCreateCandidate(t, svc, "proj", "cand-1")
		_, issues, err := svc.PromoteCandidateToPlan(ctx, PromoteCandidateInput{ProjectID: "proj", CandidateID: "cand-1"})
		if err != nil || !hasIssueCode(issues, IssueRefactorPlanRequired) {
			t.Fatalf("expected plan_required, got err=%v issues=%+v", err, issues)
		}
	})

	t.Run("unknown_project", func(t *testing.T) {
		svc, _ := newTestService(t)
		_, issues, err := svc.PromoteCandidateToPlan(ctx, PromoteCandidateInput{ProjectID: "nope", CandidateID: "cand-1", PlanID: "plan-1"})
		if err != nil || !hasIssueCode(issues, IssueRefactorProjectUnknown) {
			t.Fatalf("expected project_unknown, got err=%v issues=%+v", err, issues)
		}
	})

	t.Run("unknown_candidate", func(t *testing.T) {
		svc, st := newTestService(t)
		project := mustProject(t, st, "proj")
		seedPlan(t, st, project, "plan-1", "active")
		_, issues, err := svc.PromoteCandidateToPlan(ctx, PromoteCandidateInput{ProjectID: "proj", CandidateID: "ghost", PlanID: "plan-1"})
		if err != nil || !hasIssueCode(issues, IssueRefactorCandidateUnknown) {
			t.Fatalf("expected candidate_unknown, got err=%v issues=%+v", err, issues)
		}
	})

	t.Run("plan_project_mismatch", func(t *testing.T) {
		svc, st := newTestService(t)
		mustProject(t, st, "proj-a")
		projectB := mustProject(t, st, "proj-b")
		mustCreateCandidate(t, svc, "proj-a", "cand-1")
		seedPlan(t, st, projectB, "plan-b", "active")

		_, issues, err := svc.PromoteCandidateToPlan(ctx, PromoteCandidateInput{ProjectID: "proj-a", CandidateID: "cand-1", PlanID: "plan-b"})
		if err != nil || !hasIssueCode(issues, IssueRefactorPlanProjectMismatch) {
			t.Fatalf("expected plan_project_mismatch, got err=%v issues=%+v", err, issues)
		}
		// Candidate unchanged.
		if candidateRow(t, st, mustGetProjectRowID(t, st, "proj-a"), "cand-1").Status != CandidateStatusReady {
			t.Fatalf("candidate status should remain ready")
		}
	})

	t.Run("non_ready_candidate", func(t *testing.T) {
		svc, st := newTestService(t)
		project := mustProject(t, st, "proj")
		mustCreateCandidate(t, svc, "proj", "cand-1")
		seedPlan(t, st, project, "plan-1", "active")
		if _, issues, err := svc.DeferCandidate(ctx, "proj", "cand-1", CandidateLifecycleInput{DeferReason: "later"}); err != nil || len(issues) > 0 {
			t.Fatalf("defer failed: err=%v issues=%+v", err, issues)
		}
		_, issues, err := svc.PromoteCandidateToPlan(ctx, PromoteCandidateInput{ProjectID: "proj", CandidateID: "cand-1", PlanID: "plan-1"})
		if err != nil || !hasIssueCode(issues, IssueRefactorCandidateNotReady) {
			t.Fatalf("expected candidate_not_ready, got err=%v issues=%+v", err, issues)
		}
	})

	t.Run("already_scheduled", func(t *testing.T) {
		svc, st := newTestService(t)
		project := mustProject(t, st, "proj")
		mustCreateCandidate(t, svc, "proj", "cand-1")
		seedPlan(t, st, project, "plan-1", "active")
		// Attach an active schedule reference while leaving the candidate ready, so
		// the already-scheduled guard (not the not-ready guard) is exercised.
		row := candidateRow(t, st, project.ID, "cand-1")
		if _, err := st.CreateRefactorCandidateScheduleRef(store.CreateRefactorCandidateScheduleRefParams{
			ScheduleRefID:  "rsched-existing",
			ProjectRowID:   project.ID,
			ProjectID:      project.ProjectID,
			CandidateRowID: row.ID,
			ScheduleKind:   "existing_plan_bonus_pass",
			PlanID:         "plan-1",
			PassID:         "PASS-001",
		}); err != nil {
			t.Fatalf("create schedule ref failed: %v", err)
		}
		_, issues, err := svc.PromoteCandidateToPlan(ctx, PromoteCandidateInput{ProjectID: "proj", CandidateID: "cand-1", PlanID: "plan-1"})
		if err != nil || !hasIssueCode(issues, IssueRefactorCandidateAlreadyScheduled) {
			t.Fatalf("expected candidate_already_scheduled, got err=%v issues=%+v", err, issues)
		}
	})

	t.Run("inactive_plan", func(t *testing.T) {
		svc, st := newTestService(t)
		project := mustProject(t, st, "proj")
		mustCreateCandidate(t, svc, "proj", "cand-1")
		seedPlan(t, st, project, "plan-1", "complete")
		_, issues, err := svc.PromoteCandidateToPlan(ctx, PromoteCandidateInput{ProjectID: "proj", CandidateID: "cand-1", PlanID: "plan-1"})
		if err != nil || !hasIssueCode(issues, IssueRefactorPlanStatusInvalid) {
			t.Fatalf("expected plan_status_invalid, got err=%v issues=%+v", err, issues)
		}
	})

	t.Run("invalid_placement_no_partial_write", func(t *testing.T) {
		svc, st := newTestService(t)
		project := mustProject(t, st, "proj")
		mustCreateCandidate(t, svc, "proj", "cand-1")
		plan := seedPlan(t, st, project, "plan-1", "active")
		seedPass(t, st, plan.ID, "PASS-001", 1, nil, nil)

		_, issues, err := svc.PromoteCandidateToPlan(ctx, PromoteCandidateInput{ProjectID: "proj", CandidateID: "cand-1", PlanID: "plan-1", AfterPassID: "PASS-999"})
		if err != nil || !hasIssueCode(issues, IssueRefactorPlacementInvalid) {
			t.Fatalf("expected placement_invalid, got err=%v issues=%+v", err, issues)
		}
		// No partial writes: candidate stays ready, no schedule ref, pass count unchanged.
		row := candidateRow(t, st, project.ID, "cand-1")
		if row.Status != CandidateStatusReady {
			t.Fatalf("candidate should remain ready, got %q", row.Status)
		}
		if active, _ := st.GetActiveRefactorCandidateScheduleRef(project.ID, row.ID); active != nil {
			t.Fatalf("expected no schedule ref after blocked promotion")
		}
		passes, _ := st.ListPlanPassesByPlan(plan.ID)
		if len(passes) != 1 {
			t.Fatalf("expected pass count unchanged (1), got %d", len(passes))
		}
	})
}

// ---------------------------------------------------------------------------
// Generated refactor-only plan (S6)
// ---------------------------------------------------------------------------

func withTempArtifacts(t *testing.T) {
	t.Helper()
	orig := artifacts.BaseDir
	t.Cleanup(func() { artifacts.SetBaseDir(orig) })
	artifacts.SetBaseDir(t.TempDir())
}

func TestGenerateRefactorOnlyPlanArtifactOnly(t *testing.T) {
	svc, st := newTestService(t)
	ctx := context.Background()
	project := mustProject(t, st, "relay")
	withTempArtifacts(t)
	mustCreateCandidate(t, svc, "relay", "cand-a")
	mustCreateCandidate(t, svc, "relay", "cand-b")

	result, issues, err := svc.GenerateRefactorOnlyPlan(ctx, GenerateRefactorPlanInput{
		ProjectID:    "relay",
		CandidateIDs: []string{"cand-a", "cand-b"},
		Title:        "Review selected refactor candidates",
	})
	if err != nil || len(issues) > 0 {
		t.Fatalf("generate failed: err=%v issues=%+v", err, issues)
	}
	if result.SubmissionPolicy != "review_required_no_auto_submit" {
		t.Fatalf("unexpected submission policy %q", result.SubmissionPolicy)
	}

	// Artifacts exist on disk.
	jsonBytes, err := os.ReadFile(result.JSONArtifactPath)
	if err != nil {
		t.Fatalf("read json artifact failed: %v", err)
	}
	mdBytes, err := os.ReadFile(result.MarkdownArtifactPath)
	if err != nil {
		t.Fatalf("read md artifact failed: %v", err)
	}

	// Generated JSON validates as Plan v2.
	_, report, err := plans.NewService(st).ValidatePlanJSON(ctx, jsonBytes)
	if err != nil {
		t.Fatalf("validate generated plan failed: %v", err)
	}
	if !report.Valid {
		t.Fatalf("generated plan should be valid, issues=%+v", report.Issues)
	}

	// Markdown clearly states review required / not submitted.
	md := string(mdBytes)
	if !contains(md, "Review required") || !contains(md, "not") {
		t.Fatalf("markdown missing review-required language: %s", md)
	}

	// No plan row was created and candidates remain ready.
	if _, err := st.GetPlanByProjectAndPlanID(project.ID, result.PlanID); err == nil {
		t.Fatalf("generated plan must not be persisted as a plan row")
	}
	for _, id := range []string{"cand-a", "cand-b"} {
		if candidateRow(t, st, project.ID, id).Status != CandidateStatusReady {
			t.Fatalf("candidate %s should remain ready after generation", id)
		}
	}
}

func TestGenerateRefactorOnlyPlanDependencyOrdering(t *testing.T) {
	svc, st := newTestService(t)
	ctx := context.Background()
	mustProject(t, st, "relay")
	withTempArtifacts(t)

	// cand-b first (dependency target), then cand-a depending on cand-b.
	mustCreateCandidate(t, svc, "relay", "cand-b")
	depInput := validCandidateInput("cand-a", "cand-a")
	depInput.CandidateDependencyIDs = []string{"cand-b"}
	if _, issues, err := svc.CreateCandidate(ctx, "relay", depInput); err != nil || len(issues) > 0 {
		t.Fatalf("create cand-a with dep failed: err=%v issues=%+v", err, issues)
	}

	// Request order [cand-a, cand-b] but dependency forces cand-b before cand-a.
	result, issues, err := svc.GenerateRefactorOnlyPlan(ctx, GenerateRefactorPlanInput{
		ProjectID:    "relay",
		CandidateIDs: []string{"cand-a", "cand-b"},
	})
	if err != nil || len(issues) > 0 {
		t.Fatalf("generate failed: err=%v issues=%+v", err, issues)
	}
	if len(result.CandidateIDs) != 2 || result.CandidateIDs[0] != "cand-b" || result.CandidateIDs[1] != "cand-a" {
		t.Fatalf("expected topo order [cand-b, cand-a], got %+v", result.CandidateIDs)
	}

	jsonBytes, _ := os.ReadFile(result.JSONArtifactPath)
	var plan plans.PlannerPassPlan
	if err := json.Unmarshal(jsonBytes, &plan); err != nil {
		t.Fatalf("unmarshal generated plan failed: %v", err)
	}
	// cand-b -> PASS-001, cand-a -> PASS-002 with dependency PASS-001.
	var aPass *plans.PlanPassInput
	for i := range plan.Passes {
		if plan.Passes[i].RefactorCandidate != nil && plan.Passes[i].RefactorCandidate.CandidateID == "cand-a" {
			aPass = &plan.Passes[i]
		}
	}
	if aPass == nil {
		t.Fatalf("cand-a pass not found")
	}
	if len(aPass.Dependencies) != 1 || aPass.Dependencies[0] != "PASS-001" {
		t.Fatalf("expected cand-a pass to depend on PASS-001, got %+v", aPass.Dependencies)
	}
}

func TestGenerateRefactorOnlyPlanBlockedCases(t *testing.T) {
	ctx := context.Background()

	t.Run("unknown_project", func(t *testing.T) {
		svc, _ := newTestService(t)
		withTempArtifacts(t)
		_, issues, err := svc.GenerateRefactorOnlyPlan(ctx, GenerateRefactorPlanInput{ProjectID: "nope", CandidateIDs: []string{"x"}})
		if err != nil || !hasIssueCode(issues, IssueRefactorProjectUnknown) {
			t.Fatalf("expected project_unknown, got err=%v issues=%+v", err, issues)
		}
	})

	t.Run("empty_candidate_ids", func(t *testing.T) {
		svc, st := newTestService(t)
		mustProject(t, st, "relay")
		_, issues, err := svc.GenerateRefactorOnlyPlan(ctx, GenerateRefactorPlanInput{ProjectID: "relay", CandidateIDs: []string{"  "}})
		if err != nil || !hasIssueCode(issues, IssueRefactorCandidateRequired) {
			t.Fatalf("expected candidate_required, got err=%v issues=%+v", err, issues)
		}
	})

	t.Run("duplicate_candidate", func(t *testing.T) {
		svc, st := newTestService(t)
		mustProject(t, st, "relay")
		mustCreateCandidate(t, svc, "relay", "cand-a")
		_, issues, err := svc.GenerateRefactorOnlyPlan(ctx, GenerateRefactorPlanInput{ProjectID: "relay", CandidateIDs: []string{"cand-a", "cand-a"}})
		if err != nil || !hasIssueCode(issues, IssueRefactorDuplicateCandidate) {
			t.Fatalf("expected duplicate_candidate, got err=%v issues=%+v", err, issues)
		}
	})

	t.Run("unknown_candidate", func(t *testing.T) {
		svc, st := newTestService(t)
		mustProject(t, st, "relay")
		_, issues, err := svc.GenerateRefactorOnlyPlan(ctx, GenerateRefactorPlanInput{ProjectID: "relay", CandidateIDs: []string{"ghost"}})
		if err != nil || !hasIssueCode(issues, IssueRefactorCandidateUnknown) {
			t.Fatalf("expected candidate_unknown, got err=%v issues=%+v", err, issues)
		}
	})

	t.Run("dependency_cycle", func(t *testing.T) {
		svc, st := newTestService(t)
		mustProject(t, st, "relay")
		withTempArtifacts(t)
		mustCreateCandidate(t, svc, "relay", "cand-b")
		aInput := validCandidateInput("cand-a", "cand-a")
		aInput.CandidateDependencyIDs = []string{"cand-b"}
		if _, issues, err := svc.CreateCandidate(ctx, "relay", aInput); err != nil || len(issues) > 0 {
			t.Fatalf("create cand-a failed: err=%v issues=%+v", err, issues)
		}
		// Update cand-b to depend on cand-a, forming a cycle.
		bUpdate := validCandidateInput("cand-b", "cand-b")
		bUpdate.CandidateDependencyIDs = []string{"cand-a"}
		if _, issues, err := svc.UpdateCandidate(ctx, "relay", "cand-b", bUpdate); err != nil || len(issues) > 0 {
			t.Fatalf("update cand-b failed: err=%v issues=%+v", err, issues)
		}
		_, issues, err := svc.GenerateRefactorOnlyPlan(ctx, GenerateRefactorPlanInput{ProjectID: "relay", CandidateIDs: []string{"cand-a", "cand-b"}})
		if err != nil || !hasIssueCode(issues, IssueRefactorDependencyCycle) {
			t.Fatalf("expected dependency_cycle, got err=%v issues=%+v", err, issues)
		}
	})

	t.Run("external_unsatisfied_dependency", func(t *testing.T) {
		svc, st := newTestService(t)
		mustProject(t, st, "relay")
		withTempArtifacts(t)
		mustCreateCandidate(t, svc, "relay", "cand-dep")
		aInput := validCandidateInput("cand-a", "cand-a")
		aInput.CandidateDependencyIDs = []string{"cand-dep"}
		if _, issues, err := svc.CreateCandidate(ctx, "relay", aInput); err != nil || len(issues) > 0 {
			t.Fatalf("create cand-a failed: err=%v issues=%+v", err, issues)
		}
		// Only cand-a selected; cand-dep is ready (not completed) and not selected.
		_, issues, err := svc.GenerateRefactorOnlyPlan(ctx, GenerateRefactorPlanInput{ProjectID: "relay", CandidateIDs: []string{"cand-a"}})
		if err != nil || !hasIssueCode(issues, IssueRefactorDependencyNotSatisfied) {
			t.Fatalf("expected dependency_not_satisfied, got err=%v issues=%+v", err, issues)
		}
	})

	t.Run("external_completed_dependency_warns", func(t *testing.T) {
		svc, st := newTestService(t)
		project := mustProject(t, st, "relay")
		withTempArtifacts(t)
		mustCreateCandidate(t, svc, "relay", "cand-done")
		aInput := validCandidateInput("cand-a", "cand-a")
		aInput.CandidateDependencyIDs = []string{"cand-done"}
		if _, issues, err := svc.CreateCandidate(ctx, "relay", aInput); err != nil || len(issues) > 0 {
			t.Fatalf("create cand-a failed: err=%v issues=%+v", err, issues)
		}
		// Force cand-done to completed directly.
		if _, err := st.UpdateRefactorCandidateStatusMetadata(store.UpdateRefactorCandidateStatusMetadataParams{
			ProjectRowID: project.ID,
			CandidateID:  "cand-done",
			Status:       CandidateStatusCompleted,
			CompletedAt:  "2026-06-24T00:00:00Z",
		}); err != nil {
			t.Fatalf("force completed failed: %v", err)
		}
		result, issues, err := svc.GenerateRefactorOnlyPlan(ctx, GenerateRefactorPlanInput{ProjectID: "relay", CandidateIDs: []string{"cand-a"}})
		if err != nil || len(issues) > 0 {
			t.Fatalf("generate failed: err=%v issues=%+v", err, issues)
		}
		if len(result.Warnings) == 0 {
			t.Fatalf("expected a dependency warning for external completed dependency")
		}
	})
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (indexOf(haystack, needle) >= 0)
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
