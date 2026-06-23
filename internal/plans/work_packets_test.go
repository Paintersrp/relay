package plans

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"relay/internal/store"
)

// baseContextPlan returns a schema-valid ContextPlan for test purposes.
// It has one required seed search term and one required file, satisfying
// all minItems constraints from the plan schema.
func baseContextPlan() ContextPlan {
	return ContextPlan{
		RequiredRepositories: []string{"relay"},
		SeedSearchTerms: []ContextSearchTerm{
			{RepoID: "relay", Query: "plans validate", Purpose: "Locate validation flow.", Required: boolPtr(true)},
		},
		SeedFilesToRead: []ContextFileRead{
			{RepoID: "relay", Path: "internal/plans/validator.go", Purpose: "Validate plans.", Required: boolPtr(true)},
		},
		ContextCoverageExpectations: []string{"Validation remains fail-closed for Plan v2."},
		BlockedIfMissing:            []string{"Validation code cannot be located."},
	}
}

// noContextRequirementsPlan returns a ContextPlan with no required inputs.
// Seed items are present but Required=false so they don't trigger context-packet checks.
func noContextRequirementsPlan() ContextPlan {
	return ContextPlan{
		RequiredRepositories: []string{"relay"},
		SeedSearchTerms: []ContextSearchTerm{
			{RepoID: "relay", Query: "plans validate", Purpose: "Optional context.", Required: boolPtr(false)},
		},
		SeedFilesToRead: []ContextFileRead{
			{RepoID: "relay", Path: "internal/plans/validator.go", Purpose: "Optional file.", Required: boolPtr(false)},
		},
		ContextCoverageExpectations: []string{"Coverage is best-effort."},
		BlockedIfMissing:            []string{"Not blocked if missing."},
	}
}

// newWorkPacketService creates a test service and store with a default "relay" project.
func newWorkPacketService(t *testing.T) (*OrchestratorWorkService, *store.Store) {
	t.Helper()

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "relay.sqlite")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	st, err := store.Open(dbPath, logger)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() {
		if err := st.Close(); err != nil {
			t.Fatalf("store.Close: %v", err)
		}
	})

	if _, err := st.CreateProject("relay", "Relay", "Default test project", "active", ""); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	return NewOrchestratorWorkService(st), st
}

// seedPlan submits a two-pass plan using plans.Service.
// PASS-001 has no dependencies; PASS-002 depends on PASS-001.
// Both start with status "planned" and no required context inputs so
// the service will select them without needing source snapshots or packets.
func seedPlan(t *testing.T, st *store.Store, projectID, planID string) *store.Plan {
	t.Helper()

	svc := NewService(st)
	plan := PlannerPassPlan{
		PlanMeta: PlanMeta{
			PlanID:        planID,
			SchemaVersion: "2.0.0",
			CreatedAt:     "2026-06-23T00:00:00Z",
			Title:         "Test plan",
			Goal:          "Work packet test plan",
			RepoTarget:    "Paintersrp/relay",
			BranchContext: "main",
			Status:        "active",
			ProjectID:     projectID,
			MCPCapabilityProfile: &MCPCapabilityProfile{
				ProfileID:            "test-profile",
				Mode:                 "submission_only",
				ContextBrokerEnabled: boolPtr(false),
			},
		},
		SourceIntent: SourceIntent{Summary: "Test plan for work packet service."},
		GlobalContextRules: &GlobalContextRules{
			DefaultSourceOfTruth:    "Relay managed plan.",
			PlannerContextBoundary:  "Test only.",
			ForbiddenContextDomains: []string{"GitHub issues"},
		},
		Passes: []PlanPassInput{
			{
				PassID:                 "PASS-001",
				Sequence:               1,
				Name:                   "First pass",
				Goal:                   "First pass goal",
				IntendedExecutionScope: []string{"internal/plans"},
				NonGoals:               []string{"No UI"},
				Dependencies:           []string{},
				Status:                 "planned",
				PassType:               "backend_vertical_slice",
				// No required context inputs -- service can select without snapshot/packet.
				ContextPlan: noContextRequirementsPlan(),
				SourceSnapshotRequirements: SourceSnapshotRequirements{
					RequireGitStatus:   boolPtr(false),
					RequireCommitSHA:   boolPtr(false),
					AllowDirtyWorktree: boolPtr(true),
				},
				HandoffReadinessCriteria: []string{"Pass 1 complete"},
			},
			{
				PassID:                 "PASS-002",
				Sequence:               2,
				Name:                   "Second pass",
				Goal:                   "Second pass goal",
				IntendedExecutionScope: []string{"internal/plans"},
				NonGoals:               []string{"No UI"},
				Dependencies:           []string{"PASS-001"},
				Status:                 "planned",
				PassType:               "backend_vertical_slice",
				ContextPlan:            noContextRequirementsPlan(),
				SourceSnapshotRequirements: SourceSnapshotRequirements{
					RequireGitStatus:   boolPtr(false),
					RequireCommitSHA:   boolPtr(false),
					AllowDirtyWorktree: boolPtr(true),
				},
				HandoffReadinessCriteria: []string{"Pass 2 complete"},
			},
		},
	}

	raw := mustMarshalPlan(t, plan)
	result, err := svc.SubmitPlan(context.Background(), SubmitPlanRequest{RawJSON: raw})
	if err != nil {
		t.Fatalf("SubmitPlan: %v", err)
	}
	if !result.Report.Valid {
		t.Fatalf("SubmitPlan invalid: %+v", result.Report.Issues)
	}

	p, err := st.GetPlanByPlanID(planID)
	if err != nil {
		t.Fatalf("GetPlanByPlanID: %v", err)
	}
	return p
}

// setPassStatus updates a pass status directly via the store.
func setPassStatus(t *testing.T, st *store.Store, planID, passID, status string) {
	t.Helper()

	plan, err := st.GetPlanByPlanID(planID)
	if err != nil {
		t.Fatalf("GetPlanByPlanID %q: %v", planID, err)
	}
	pass, err := st.GetPlanPassByPassID(plan.ID, passID)
	if err != nil {
		t.Fatalf("GetPlanPassByPassID %q: %v", passID, err)
	}
	if _, err := st.UpdatePlanPassStatus(pass.ID, status); err != nil {
		t.Fatalf("UpdatePlanPassStatus %q => %q: %v", passID, status, err)
	}
}

// assertBlockerCode checks that the response has ok=false and the first blocker
// matches the expected code.
func assertBlockerCode(t *testing.T, resp NextPassWorkResponse, expected string) {
	t.Helper()

	if resp.OK {
		t.Fatalf("expected ok=false, got ok=true")
	}
	if len(resp.Blockers) == 0 {
		t.Fatalf("expected at least one blocker, got none")
	}
	if resp.Blockers[0].Code != expected {
		t.Fatalf("expected blocker code %q, got %q (message: %q)", expected, resp.Blockers[0].Code, resp.Blockers[0].Message)
	}
}

// -------------------------------------------------------------------
// Tests
// -------------------------------------------------------------------

func TestGetNextPassWork_UnknownProject(t *testing.T) {
	t.Parallel()

	svc, _ := newWorkPacketService(t)
	resp, err := svc.GetNextPassWork(context.Background(), NextPassWorkRequest{
		ProjectID: "no-such-project",
		PlanID:    "plan-x",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertBlockerCode(t, resp, BlockerUnknownProject)
}

func TestGetNextPassWork_UnknownPlan(t *testing.T) {
	t.Parallel()

	svc, _ := newWorkPacketService(t)
	resp, err := svc.GetNextPassWork(context.Background(), NextPassWorkRequest{
		ProjectID: "relay",
		PlanID:    "no-such-plan",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertBlockerCode(t, resp, BlockerUnknownPlan)
}

func TestGetNextPassWork_ProjectPlanMismatch(t *testing.T) {
	t.Parallel()

	svc, st := newWorkPacketService(t)

	if _, err := st.CreateProject("other-project", "Other", "", "active", ""); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	seedPlan(t, st, "relay", "plan-mismatch")

	// Request with "other-project" -- plan belongs to "relay".
	resp, err := svc.GetNextPassWork(context.Background(), NextPassWorkRequest{
		ProjectID: "other-project",
		PlanID:    "plan-mismatch",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertBlockerCode(t, resp, BlockerProjectPlanMismatch)
}

func TestGetNextPassWork_PlanNotActive(t *testing.T) {
	t.Parallel()

	svc, st := newWorkPacketService(t)
	seedPlan(t, st, "relay", "plan-notactive")

	plan, err := st.GetPlanByPlanID("plan-notactive")
	if err != nil {
		t.Fatalf("GetPlanByPlanID: %v", err)
	}
	if _, err := st.DB().Exec(`UPDATE plans SET status = 'complete' WHERE id = ?`, plan.ID); err != nil {
		t.Fatalf("update plan status: %v", err)
	}

	resp, err := svc.GetNextPassWork(context.Background(), NextPassWorkRequest{
		ProjectID: "relay",
		PlanID:    "plan-notactive",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertBlockerCode(t, resp, BlockerPlanNotActive)
}

func TestGetNextPassWork_SelectsLowestSequence(t *testing.T) {
	t.Parallel()

	svc, st := newWorkPacketService(t)
	seedPlan(t, st, "relay", "plan-selectseq")

	resp, err := svc.GetNextPassWork(context.Background(), NextPassWorkRequest{
		ProjectID: "relay",
		PlanID:    "plan-selectseq",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.OK {
		t.Fatalf("expected ok=true, got blockers: %+v", resp.Blockers)
	}
	if resp.SelectedPass == nil {
		t.Fatal("expected selected_pass in response")
	}
	if resp.SelectedPass.PassID != "PASS-001" {
		t.Fatalf("expected PASS-001, got %q", resp.SelectedPass.PassID)
	}
	if resp.SelectedPass.Sequence != 1 {
		t.Fatalf("expected sequence 1, got %d", resp.SelectedPass.Sequence)
	}
	if resp.Tool != NextPassWorkTool {
		t.Fatalf("expected tool %q, got %q", NextPassWorkTool, resp.Tool)
	}
}

func TestGetNextPassWork_HandoffReadyBlocksAdvancement(t *testing.T) {
	t.Parallel()

	svc, st := newWorkPacketService(t)
	seedPlan(t, st, "relay", "plan-handoff")

	// PASS-001 is handoff_ready (Planner submitted a handoff but it wasn't acted on yet).
	setPassStatus(t, st, "plan-handoff", "PASS-001", StatusPassHandoffReady)

	resp, err := svc.GetNextPassWork(context.Background(), NextPassWorkRequest{
		ProjectID: "relay",
		PlanID:    "plan-handoff",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertBlockerCode(t, resp, BlockerNoEligiblePass)
}

func TestGetNextPassWork_DependenciesIncomplete_PlannedDep(t *testing.T) {
	t.Parallel()

	svc, st := newWorkPacketService(t)
	seedPlan(t, st, "relay", "plan-planned-dep")

	// Set PASS-001 to in_progress so it's active (not completed/skipped).
	// The walker should block on active_run_exists for PASS-001
	// (its pass-level status blocks PASS-002's dep check via the walker seeing run_created/in_progress first).
	setPassStatus(t, st, "plan-planned-dep", "PASS-001", StatusPassInProgress)

	resp, err := svc.GetNextPassWork(context.Background(), NextPassWorkRequest{
		ProjectID: "relay",
		PlanID:    "plan-planned-dep",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Walker sees PASS-001 as in_progress -- returns active_run_exists.
	assertBlockerCode(t, resp, BlockerActiveRunExists)
}

func TestGetNextPassWork_PriorPassAwaitsAudit(t *testing.T) {
	t.Parallel()

	svc, st := newWorkPacketService(t)
	seedPlan(t, st, "relay", "plan-audit")

	setPassStatus(t, st, "plan-audit", "PASS-001", StatusPassAuditReady)

	resp, err := svc.GetNextPassWork(context.Background(), NextPassWorkRequest{
		ProjectID: "relay",
		PlanID:    "plan-audit",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertBlockerCode(t, resp, BlockerPriorPassAwaitsAudit)
}

func TestGetNextPassWork_RevisionRequiredSamePass(t *testing.T) {
	t.Parallel()

	svc, st := newWorkPacketService(t)
	seedPlan(t, st, "relay", "plan-revision")

	setPassStatus(t, st, "plan-revision", "PASS-001", StatusPassRevisionRequired)

	resp, err := svc.GetNextPassWork(context.Background(), NextPassWorkRequest{
		ProjectID: "relay",
		PlanID:    "plan-revision",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertBlockerCode(t, resp, BlockerRevisionRequiredSamePass)
}

func TestGetNextPassWork_ActiveRunExists(t *testing.T) {
	t.Parallel()

	svc, st := newWorkPacketService(t)
	seedPlan(t, st, "relay", "plan-activerun")

	repo, err := st.CreateRepo("test-repo", t.TempDir())
	if err != nil {
		t.Fatalf("CreateRepo: %v", err)
	}

	plan, err := st.GetPlanByPlanID("plan-activerun")
	if err != nil {
		t.Fatalf("GetPlanByPlanID: %v", err)
	}
	pass, err := st.GetPlanPassByPassID(plan.ID, "PASS-001")
	if err != nil {
		t.Fatalf("GetPlanPassByPassID: %v", err)
	}

	// Create a non-terminal run associated with PASS-001.
	_, err = st.CreateRunWithAssociation(
		repo.ID,
		"active run",
		"executor_running",
		"", "", "opencode_go", "main",
		sql.NullInt64{Int64: plan.ID, Valid: true},
		sql.NullInt64{Int64: pass.ID, Valid: true},
	)
	if err != nil {
		t.Fatalf("CreateRunWithAssociation: %v", err)
	}

	resp, err := svc.GetNextPassWork(context.Background(), NextPassWorkRequest{
		ProjectID: "relay",
		PlanID:    "plan-activerun",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertBlockerCode(t, resp, BlockerActiveRunExists)
}

func TestGetNextPassWork_RequiredSourceContextMissing(t *testing.T) {
	t.Parallel()

	svc, st := newWorkPacketService(t)

	planSvc := NewService(st)
	plan := PlannerPassPlan{
		PlanMeta: PlanMeta{
			PlanID: "plan-snapshotmiss", SchemaVersion: "2.0.0",
			CreatedAt: "2026-06-23T00:00:00Z", Title: "T", Goal: "G",
			RepoTarget: "Paintersrp/relay", BranchContext: "main", Status: "active",
			ProjectID: "relay",
			MCPCapabilityProfile: &MCPCapabilityProfile{
				ProfileID: "p", Mode: "submission_only", ContextBrokerEnabled: boolPtr(false),
			},
		},
		SourceIntent:       SourceIntent{Summary: "S"},
		GlobalContextRules: &GlobalContextRules{DefaultSourceOfTruth: "D", PlannerContextBoundary: "B", ForbiddenContextDomains: []string{"X"}},
		Passes: []PlanPassInput{{
			PassID: "PASS-001", Sequence: 1, Name: "N", Goal: "G",
			IntendedExecutionScope: []string{"a"}, NonGoals: []string{"b"},
			Dependencies: []string{}, Status: "planned", PassType: "backend_vertical_slice",
			// Use noContextRequirementsPlan so we don't need a context packet, only snapshot.
			ContextPlan: noContextRequirementsPlan(),
			// Require git status -- no snapshot will exist.
			SourceSnapshotRequirements: SourceSnapshotRequirements{
				RequireGitStatus: boolPtr(true), RequireCommitSHA: boolPtr(false), AllowDirtyWorktree: boolPtr(true),
			},
			HandoffReadinessCriteria: []string{"c"},
		}},
	}
	raw := mustMarshalPlan(t, plan)
	result, err := planSvc.SubmitPlan(context.Background(), SubmitPlanRequest{RawJSON: raw})
	if err != nil || !result.Report.Valid {
		t.Fatalf("SubmitPlan failed: err=%v issues=%+v", err, result.Report.Issues)
	}

	resp, err := svc.GetNextPassWork(context.Background(), NextPassWorkRequest{
		ProjectID: "relay",
		PlanID:    "plan-snapshotmiss",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertBlockerCode(t, resp, BlockerRequiredSourceContextMissing)
}

func TestGetNextPassWork_RequiredContextPacketMissing(t *testing.T) {
	t.Parallel()

	svc, st := newWorkPacketService(t)

	planSvc := NewService(st)
	plan := PlannerPassPlan{
		PlanMeta: PlanMeta{
			PlanID: "plan-packetmiss", SchemaVersion: "2.0.0",
			CreatedAt: "2026-06-23T00:00:00Z", Title: "T", Goal: "G",
			RepoTarget: "Paintersrp/relay", BranchContext: "main", Status: "active",
			ProjectID: "relay",
			MCPCapabilityProfile: &MCPCapabilityProfile{
				ProfileID: "p", Mode: "submission_only", ContextBrokerEnabled: boolPtr(false),
			},
		},
		SourceIntent:       SourceIntent{Summary: "S"},
		GlobalContextRules: &GlobalContextRules{DefaultSourceOfTruth: "D", PlannerContextBoundary: "B", ForbiddenContextDomains: []string{"X"}},
		Passes: []PlanPassInput{{
			PassID: "PASS-001", Sequence: 1, Name: "N", Goal: "G",
			IntendedExecutionScope: []string{"a"}, NonGoals: []string{"b"},
			Dependencies: []string{}, Status: "planned", PassType: "backend_vertical_slice",
			// Required seed file -- triggers context packet check.
			ContextPlan: baseContextPlan(),
			// No snapshot required -- isolates the packet blocker.
			SourceSnapshotRequirements: SourceSnapshotRequirements{
				RequireGitStatus: boolPtr(false), RequireCommitSHA: boolPtr(false), AllowDirtyWorktree: boolPtr(true),
			},
			HandoffReadinessCriteria: []string{"c"},
		}},
	}
	raw := mustMarshalPlan(t, plan)
	result, err := planSvc.SubmitPlan(context.Background(), SubmitPlanRequest{RawJSON: raw})
	if err != nil || !result.Report.Valid {
		t.Fatalf("SubmitPlan failed: err=%v issues=%+v", err, result.Report.Issues)
	}

	resp, err := svc.GetNextPassWork(context.Background(), NextPassWorkRequest{
		ProjectID: "relay",
		PlanID:    "plan-packetmiss",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertBlockerCode(t, resp, BlockerRequiredContextPacketMissing)
}

func TestGetNextPassWork_NoEligiblePass_AllTerminal(t *testing.T) {
	t.Parallel()

	svc, st := newWorkPacketService(t)
	seedPlan(t, st, "relay", "plan-terminal")

	setPassStatus(t, st, "plan-terminal", "PASS-001", StatusPassCompleted)
	setPassStatus(t, st, "plan-terminal", "PASS-002", StatusPassSkipped)

	resp, err := svc.GetNextPassWork(context.Background(), NextPassWorkRequest{
		ProjectID: "relay",
		PlanID:    "plan-terminal",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertBlockerCode(t, resp, BlockerNoEligiblePass)
}

func TestGetNextPassWork_UnsafeRequestEmptyIDs(t *testing.T) {
	t.Parallel()

	svc, _ := newWorkPacketService(t)

	cases := []struct {
		name      string
		projectID string
		planID    string
	}{
		{"empty project", "", "plan-x"},
		{"empty plan", "relay", ""},
		{"both empty", "", ""},
		{"path project", "../etc/passwd", "plan-x"},
		{"path plan", "relay", "../../secret"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			resp, err := svc.GetNextPassWork(context.Background(), NextPassWorkRequest{
				ProjectID: tc.projectID,
				PlanID:    tc.planID,
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			assertBlockerCode(t, resp, BlockerUnsafeRequest)
		})
	}
}

func TestGetNextPassWork_SuggestedSubmissionContainsOnlyPlanAndPassID(t *testing.T) {
	t.Parallel()

	svc, st := newWorkPacketService(t)
	seedPlan(t, st, "relay", "plan-suggestcheck")

	resp, err := svc.GetNextPassWork(context.Background(), NextPassWorkRequest{
		ProjectID: "relay",
		PlanID:    "plan-suggestcheck",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.OK {
		t.Fatalf("expected ok=true, got blockers: %+v", resp.Blockers)
	}
	if resp.SuggestedRunSubmission == nil {
		t.Fatal("expected suggested_run_submission in response")
	}
	if resp.SuggestedRunSubmission.Tool != "create_run_from_planner_handoff" {
		t.Fatalf("expected tool create_run_from_planner_handoff, got %q", resp.SuggestedRunSubmission.Tool)
	}
	if resp.SuggestedRunSubmission.Arguments.PlanID == "" {
		t.Fatal("expected plan_id in suggested arguments")
	}
	if resp.SuggestedRunSubmission.Arguments.PassID == "" {
		t.Fatal("expected pass_id in suggested arguments")
	}
}

func TestGetNextPassWork_RetrievalOnlyNoRunCreated(t *testing.T) {
	t.Parallel()

	svc, st := newWorkPacketService(t)
	seedPlan(t, st, "relay", "plan-retrieval")

	runCountBefore := countRows(t, st.DB(), "runs")

	resp, err := svc.GetNextPassWork(context.Background(), NextPassWorkRequest{
		ProjectID: "relay",
		PlanID:    "plan-retrieval",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.OK {
		t.Fatalf("expected ok=true, got blockers: %+v", resp.Blockers)
	}

	runCountAfter := countRows(t, st.DB(), "runs")
	if runCountAfter != runCountBefore {
		t.Fatalf("GetNextPassWork created %d run row(s); expected 0 (retrieval-only)", runCountAfter-runCountBefore)
	}
}

func TestGetNextPassWork_SelectsSecondPassWhenFirstCompleted(t *testing.T) {
	t.Parallel()

	svc, st := newWorkPacketService(t)
	seedPlan(t, st, "relay", "plan-secondpass")

	setPassStatus(t, st, "plan-secondpass", "PASS-001", StatusPassCompleted)

	resp, err := svc.GetNextPassWork(context.Background(), NextPassWorkRequest{
		ProjectID: "relay",
		PlanID:    "plan-secondpass",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.OK {
		t.Fatalf("expected ok=true, got blockers: %+v", resp.Blockers)
	}
	if resp.SelectedPass.PassID != "PASS-002" {
		t.Fatalf("expected PASS-002, got %q", resp.SelectedPass.PassID)
	}

	// Dependency status must show PASS-001 as satisfied.
	if len(resp.DependencyStatus) == 0 {
		t.Fatal("expected dependency_status in response")
	}
	var foundDep bool
	for _, ds := range resp.DependencyStatus {
		if ds.PassID == "PASS-001" {
			if !ds.Satisfied {
				t.Fatalf("expected PASS-001 satisfied=true, got false")
			}
			foundDep = true
		}
	}
	if !foundDep {
		t.Fatal("expected PASS-001 in dependency_status")
	}
}

func TestGetNextPassWork_SelectsSecondPassWhenFirstSkipped(t *testing.T) {
	t.Parallel()

	svc, st := newWorkPacketService(t)
	seedPlan(t, st, "relay", "plan-skipfirst")

	setPassStatus(t, st, "plan-skipfirst", "PASS-001", StatusPassSkipped)

	resp, err := svc.GetNextPassWork(context.Background(), NextPassWorkRequest{
		ProjectID: "relay",
		PlanID:    "plan-skipfirst",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.OK {
		t.Fatalf("expected ok=true, got blockers: %+v", resp.Blockers)
	}
	if resp.SelectedPass.PassID != "PASS-002" {
		t.Fatalf("expected PASS-002, got %q", resp.SelectedPass.PassID)
	}
}

func TestGetNextPassWork_ReadyForPlannerIsEligible(t *testing.T) {
	t.Parallel()

	svc, st := newWorkPacketService(t)
	seedPlan(t, st, "relay", "plan-rfp")

	setPassStatus(t, st, "plan-rfp", "PASS-001", StatusPassReadyForPlanner)

	resp, err := svc.GetNextPassWork(context.Background(), NextPassWorkRequest{
		ProjectID: "relay",
		PlanID:    "plan-rfp",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.OK {
		t.Fatalf("expected ok=true for ready_for_planner pass, got blockers: %+v", resp.Blockers)
	}
	if resp.SelectedPass.PassID != "PASS-001" {
		t.Fatalf("expected PASS-001, got %q", resp.SelectedPass.PassID)
	}
}

func TestGetNextPassWork_BlockedPassPreventsAdvancement(t *testing.T) {
	t.Parallel()

	svc, st := newWorkPacketService(t)
	seedPlan(t, st, "relay", "plan-blocked")

	setPassStatus(t, st, "plan-blocked", "PASS-001", StatusPassBlocked)

	resp, err := svc.GetNextPassWork(context.Background(), NextPassWorkRequest{
		ProjectID: "relay",
		PlanID:    "plan-blocked",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertBlockerCode(t, resp, BlockerNoEligiblePass)
	if resp.SelectedPass != nil {
		t.Fatalf("expected no selected pass when PASS-001 is blocked, got %+v", resp.SelectedPass)
	}
	if resp.SuggestedRunSubmission != nil {
		t.Fatalf("expected no suggested run submission when PASS-001 is blocked, got %+v", resp.SuggestedRunSubmission)
	}
}
