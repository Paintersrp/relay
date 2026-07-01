package plans

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"relay/internal/store"
)

type fakeSourceSnapshotAcquirer struct {
	snapshotID string
	status     string
	included   int
}

func (f fakeSourceSnapshotAcquirer) CreateSourceSnapshot(ctx context.Context, projectID string, repoIDs []string, includeFileMetadata bool) (string, string, int, error) {
	return f.snapshotID, f.status, f.included, nil
}

type fakeContextPacketAcquirer struct {
	results []CtxPacketResult
	inputs  []CtxPacketInput
}

func (f *fakeContextPacketAcquirer) CreateContextPacket(ctx context.Context, input CtxPacketInput) (*CtxPacketResult, error) {
	f.inputs = append(f.inputs, input)
	if len(f.results) == 0 {
		return &CtxPacketResult{ContextPacketID: "ctxpkt-empty", Status: "blocked"}, nil
	}
	result := f.results[0]
	f.results = f.results[1:]
	if result.SourceSnapshotID == "" {
		result.SourceSnapshotID = input.SourceSnapshotID
	}
	return &result, nil
}

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

// noContextRequirementsPlan returns a ContextPlan with no required seed inputs.
// Seed items are present but Required=false so they don't trigger context-packet checks
// from seeds alone. BlockedIfMissing entries satisfy the schema minItems constraint.
// Because the contextPlanRequiresPacket predicate considers non-empty blocked_if_missing
// as a context requirement, tests using this plan must seed source snapshot and context
// packet artifacts in the store before calling GetNextPassWork on a service without
// acquisition services.
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
		BlockedIfMissing:            []string{"Context delivery is advisory only."},
	}
}

func pass002ShapedContextPlan() ContextPlan {
	return ContextPlan{
		RequiredRepositories: []string{"relay"},
		SeedFilesToRead: []ContextFileRead{
			{RepoID: "relay", Path: "internal/app/plans/work_packets.go", Purpose: "work packet service", Required: boolPtr(true)},
			{RepoID: "relay", Path: "internal/contextpackets/service.go", Purpose: "context packet service", Required: boolPtr(true)},
			{RepoID: "relay", Path: "internal/mcp/orchestrator_work_tools.go", Purpose: "mcp surface", Required: boolPtr(true)},
			{RepoID: "relay", Path: "internal/api/plans/handler.go", Purpose: "api surface", Required: boolPtr(true)},
			{RepoID: "relay", Path: "relay-contracts/contracts/planner_mcp_orchestrator_work_contract.md", Purpose: "contract", Required: boolPtr(true)},
		},
		SeedSearchTerms: []ContextSearchTerm{
			{RepoID: "relay", Query: "get_next_pass_work context packet", Purpose: "work packet acquisition", Required: boolPtr(true)},
			{RepoID: "relay", Query: "context_acquisition_failed acquisition_failure_report", Purpose: "failure diagnostics", Required: boolPtr(true)},
		},
		ContextCoverageExpectations: []string{"Required evidence is available before handoff authoring."},
		BlockedIfMissing:            []string{"Required context cannot be acquired."},
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

func seedSourceSnapshot(t *testing.T, st *store.Store, projectID, sourceSnapshotID string) int64 {
	t.Helper()

	project, err := st.GetProjectByProjectID(projectID)
	if err != nil {
		t.Fatalf("GetProjectByProjectID: %v", err)
	}
	snapshot, err := st.CreateSourceSnapshot(store.CreateSourceSnapshotParams{
		SourceSnapshotID: sourceSnapshotID,
		ProjectRowID:     project.ID,
		ProjectID:        project.ProjectID,
		SnapshotKind:     "clean_commit",
		Status:           "created",
		CompletedAt:      "2026-06-28T00:00:00Z",
		SummaryJSON:      "{\"file_count\":1}",
	})
	if err != nil {
		t.Fatalf("CreateSourceSnapshot: %v", err)
	}
	return snapshot.ID
}

// seedContextPacket creates a context packet row for a pass.
func seedContextPacket(t *testing.T, st *store.Store, projectID, planID, passID, sourceSnapshotID string, snapshotRowID int64, contextPacketID string) {
	t.Helper()

	project, err := st.GetProjectByProjectID(projectID)
	if err != nil {
		t.Fatalf("GetProjectByProjectID: %v", err)
	}
	if _, err := st.CreateContextPacket(store.CreateContextPacketParams{
		ContextPacketID:     contextPacketID,
		ProjectRowID:        project.ID,
		ProjectID:           projectID,
		PlanID:              planID,
		PassID:              passID,
		TaskSlug:            "test-slug",
		SourceSnapshotRowID: snapshotRowID,
		SourceSnapshotID:    sourceSnapshotID,
		Status:              "created",
		CoveredSeedCount:    0,
		BlockedSeedCount:    0,
		MissingSeedCount:    0,
		CompletedAt:         "2026-06-28T00:00:00Z",
		PacketJSONPath:      "/artifacts/ctxpkt/" + contextPacketID + ".json",
		CoverageReportPath:  "/artifacts/ctxpkt/" + contextPacketID + "-coverage.json",
	}); err != nil {
		t.Fatalf("CreateContextPacket: %v", err)
	}
}

// seedPlanContextArtifacts creates source snapshots and context packets for all
// passes in a plan. This allows tests that expect ready-for-handoff to pass
// when contextPlanRequiresPacket returns true due to non-empty blocked_if_missing
// entries that satisfy the schema minItems constraint.
func seedPlanContextArtifacts(t *testing.T, st *store.Store, projectID, planID string, passIDs []string) {
	t.Helper()

	snapshotID := "snap-" + planID
	snapshotRowID := seedSourceSnapshot(t, st, projectID, snapshotID)
	for _, passID := range passIDs {
		packetID := "ctxpkt-" + planID + "-" + passID
		seedContextPacket(t, st, projectID, planID, passID, snapshotID, snapshotRowID, packetID)
	}
}

// seedPlan submits a two-pass plan using plans.Service.
// PASS-001 has no dependencies; PASS-002 depends on PASS-001.
// Both start with status "planned". Source snapshots and context packets
// are seeded for both passes so that contextPlanRequiresPacket requirements
// are satisfied in retrieval-only mode.
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
	result, err := svc.SubmitPlan(context.Background(), SubmitPlanRequest{UnmanagedAcknowledged: true, RawJSON: raw})
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

	seedPlanContextArtifacts(t, st, projectID, planID, []string{"PASS-001", "PASS-002"})

	return p
}

func seedPass002AcquisitionPlan(t *testing.T, st *store.Store, planID string) {
	t.Helper()
	project, err := st.GetProjectByProjectID("relay")
	if err != nil {
		t.Fatalf("GetProjectByProjectID: %v", err)
	}
	if _, err := st.UpsertProjectRepository(store.UpsertProjectRepositoryParams{
		ProjectRowID:     project.ID,
		RepoID:           "relay",
		Role:             "primary",
		LocalPath:        "D:/Code/relay",
		DefaultBranch:    "main",
		AllowedRootsJSON: `["."]`,
		MaxFileSizeBytes: 1048576,
		Enabled:          1,
	}); err != nil {
		t.Fatalf("UpsertProjectRepository: %v", err)
	}

	planSvc := NewService(st)
	plan := PlannerPassPlan{
		PlanMeta: PlanMeta{
			PlanID:        planID,
			SchemaVersion: "2.0.0",
			CreatedAt:     "2026-06-28T00:00:00Z",
			Title:         "PASS-002 acquisition test",
			Goal:          "Exercise context acquisition.",
			RepoTarget:    "Paintersrp/relay",
			BranchContext: "main",
			Status:        "active",
			ProjectID:     "relay",
			MCPCapabilityProfile: &MCPCapabilityProfile{
				ProfileID:            "test-profile",
				Mode:                 "submission_only",
				ContextBrokerEnabled: boolPtr(false),
			},
		},
		SourceIntent:       SourceIntent{Summary: "Test PASS-002-shaped acquisition."},
		GlobalContextRules: &GlobalContextRules{DefaultSourceOfTruth: "test", PlannerContextBoundary: "test", ForbiddenContextDomains: []string{"external"}},
		Passes: []PlanPassInput{
			{
				PassID: "PASS-001", Sequence: 1, Name: "First", Goal: "First complete pass.",
				IntendedExecutionScope: []string{"setup"}, NonGoals: []string{"none"},
				Dependencies: []string{}, Status: "planned", PassType: "backend_vertical_slice",
				ContextPlan:                noContextRequirementsPlan(),
				SourceSnapshotRequirements: SourceSnapshotRequirements{RequireGitStatus: boolPtr(false), RequireCommitSHA: boolPtr(false), AllowDirtyWorktree: boolPtr(true)},
				HandoffReadinessCriteria:   []string{"complete"},
			},
			{
				PassID: "PASS-002", Sequence: 2, Name: "Second", Goal: "PASS-002 shaped context acquisition.",
				IntendedExecutionScope: []string{"backend"}, NonGoals: []string{"validation tiers"},
				Dependencies: []string{"PASS-001"}, Status: "planned", PassType: "backend_vertical_slice",
				ContextPlan:                pass002ShapedContextPlan(),
				SourceSnapshotRequirements: SourceSnapshotRequirements{RequireGitStatus: boolPtr(false), RequireCommitSHA: boolPtr(false), AllowDirtyWorktree: boolPtr(true)},
				HandoffReadinessCriteria:   []string{"context acquired"},
				ContextBudget:              &ContextBudget{MaxFiles: int64Ptr(12), MaxBytes: int64Ptr(180000), MaxSearchResults: int64Ptr(25), MaxContextLines: int64Ptr(50)},
			},
		},
	}
	result, err := planSvc.SubmitPlan(context.Background(), SubmitPlanRequest{UnmanagedAcknowledged: true, RawJSON: mustMarshalPlan(t, plan)})
	if err != nil || !result.Report.Valid {
		t.Fatalf("SubmitPlan failed: err=%v issues=%+v", err, result.Report.Issues)
	}
	setPassStatus(t, st, planID, "PASS-001", StatusPassCompleted)
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

func TestCompactNextPassWorkSummaryOmitsVerboseHookText(t *testing.T) {
	t.Parallel()

	verbose := "Do not repeat pre-commit, pre-push, or ordinary commit/push flow guidance."
	resp := NextPassWorkResponse{
		OK:   false,
		Tool: NextPassWorkTool,
		Project: &WorkProjectSummary{
			ProjectID: "relay",
			Name:      "Relay",
		},
		Plan: &WorkPlanSummary{
			PlanID: "plan-compact-summary",
			Status: "active",
			Title:  "Compact summary",
		},
		SelectedPass: &WorkPassSummary{
			PassID:   "PASS-002",
			Sequence: 2,
			Name:     "Context packet pass",
			Status:   "planned",
			Goal:     verbose,
		},
		Context: &WorkContextSummary{
			SourceSnapshotID: "srcsnap-001",
			ContextReady:     false,
		},
		PlannerJumpstart: &PlannerJumpstart{
			ReadinessState: "needs_context_packet",
			SuggestedContextAcquisitionActions: []ContextAcquisitionAction{{
				Tool: "create_context_packet",
				Arguments: map[string]interface{}{
					"project_id":         "relay",
					"plan_id":            "plan-compact-summary",
					"pass_id":            "PASS-002",
					"task_slug":          "next-pass-work-plan-compact-summary-pass-002",
					"source_snapshot_id": "srcsnap-001",
				},
			}},
		},
		Blockers: []WorkBlocker{{
			Code:        BlockerRequiredContextPacketMissing,
			Message:     verbose,
			Recoverable: true,
		}},
	}

	raw, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal full response: %v", err)
	}
	if !strings.Contains(string(raw), "pre-commit") {
		t.Fatal("expected full service response to preserve verbose text")
	}

	summary := CompactNextPassWorkSummary(resp)
	if summary.SelectedPass == nil || summary.SelectedPass.PassID != "PASS-002" {
		t.Fatalf("expected selected PASS-002, got %+v", summary.SelectedPass)
	}
	if summary.ReadinessState != "needs_context_packet" {
		t.Fatalf("expected needs_context_packet, got %q", summary.ReadinessState)
	}
	if summary.SourceSnapshotID != "srcsnap-001" {
		t.Fatalf("expected source snapshot ID, got %q", summary.SourceSnapshotID)
	}
	if len(summary.Blockers) != 1 || summary.Blockers[0].Code != BlockerRequiredContextPacketMissing || !summary.Blockers[0].Recoverable {
		t.Fatalf("expected recoverable context-packet blocker, got %+v", summary.Blockers)
	}
	if len(summary.NextActions) == 0 || summary.NextActions[0].Tool != "create_context_packet" {
		t.Fatalf("expected context-packet next action, got %+v", summary.NextActions)
	}
	if summary.NextActions[0].Arguments["source_snapshot_id"] != "srcsnap-001" {
		t.Fatalf("expected actionable source_snapshot_id, got %+v", summary.NextActions[0].Arguments)
	}
	if !strings.Contains(summary.LocalPreviewHint, "pass-detail preview") {
		t.Fatalf("expected local preview hint, got %q", summary.LocalPreviewHint)
	}
}

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
	assertBlockerCode(t, resp, string(BlockerUnknownProject))
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
	result, err := planSvc.SubmitPlan(context.Background(), SubmitPlanRequest{UnmanagedAcknowledged: true, RawJSON: raw})
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
	result, err := planSvc.SubmitPlan(context.Background(), SubmitPlanRequest{UnmanagedAcknowledged: true, RawJSON: raw})
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
	assertBlockerCode(t, resp, BlockerRequiredSourceContextMissing)
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

func TestGetNextPassWork_ReadyPassReturnsHandoffAuthoringPacket(t *testing.T) {
	t.Parallel()

	svc, st := newWorkPacketService(t)
	seedPlan(t, st, "relay", "plan-handoff-authoring")

	resp, err := svc.GetNextPassWork(context.Background(), NextPassWorkRequest{
		ProjectID: "relay",
		PlanID:    "plan-handoff-authoring",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.OK {
		t.Fatalf("expected ok=true, got blockers: %+v", resp.Blockers)
	}
	if resp.SuggestedRunSubmission != nil {
		t.Fatalf("planned pass without reviewed handoff must not suggest run submission: %+v", resp.SuggestedRunSubmission)
	}
	if resp.PlannerJumpstart == nil || resp.PlannerJumpstart.ReadinessState != "ready_for_handoff_authoring" {
		t.Fatalf("expected readiness_state=ready_for_handoff_authoring, got %+v", resp.PlannerJumpstart)
	}
	if resp.HandoffWork == nil {
		t.Fatal("expected handoff_work authoring packet")
	}
	if resp.HandoffAuthoringPacket == nil {
		t.Fatal("expected handoff_authoring_packet alias")
	}
	packet := resp.HandoffWork
	if packet.ProjectID != "relay" || packet.PlanID != "plan-handoff-authoring" || packet.PassID != "PASS-001" {
		t.Fatalf("unexpected authoring IDs: %+v", packet)
	}
	if packet.PassGoal == "" || len(packet.HandoffReadinessCriteria) == 0 || len(packet.ReadinessChecks) == 0 {
		t.Fatalf("expected bounded pass/readiness facts in handoff_work: %+v", packet)
	}
	if packet.SuggestedAuthoringAction != "draft_planner_handoff" {
		t.Fatalf("expected draft_planner_handoff suggested action, got %q", packet.SuggestedAuthoringAction)
	}
	if !packet.ContextReady {
		t.Fatalf("expected context_ready=true for no-required-context ready pass: %+v", packet)
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

// -------------------------------------------------------------------
// Planner jumpstart tests
// -------------------------------------------------------------------

func TestGetNextPassWork_ReadyPassIncludesPlannerJumpstart(t *testing.T) {
	t.Parallel()

	svc, st := newWorkPacketService(t)
	seedPlan(t, st, "relay", "plan-js-ready")

	resp, err := svc.GetNextPassWork(context.Background(), NextPassWorkRequest{
		ProjectID: "relay",
		PlanID:    "plan-js-ready",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.OK {
		t.Fatalf("expected ok=true, got blockers: %+v", resp.Blockers)
	}
	if resp.PlannerJumpstart == nil {
		t.Fatal("expected planner_jumpstart in response")
	}
	if resp.PlannerJumpstart.ReadinessState != "ready_for_handoff_authoring" {
		t.Fatalf("expected readiness_state=ready_for_handoff_authoring, got %q", resp.PlannerJumpstart.ReadinessState)
	}
	if resp.PlannerJumpstart.SelectedPassSummary == nil {
		t.Fatal("expected selected_pass_summary in planner_jumpstart")
	}
	if resp.PlannerJumpstart.SelectedPassSummary.PassID != "PASS-001" {
		t.Fatalf("expected PASS-001 in planner_jumpstart, got %q", resp.PlannerJumpstart.SelectedPassSummary.PassID)
	}
	if len(resp.PlannerJumpstart.HandoffPreflightChecklist) == 0 {
		t.Fatal("expected non-empty handoff_preflight_checklist")
	}
	// A ready pass should not have suggested acquisition actions.
	if len(resp.PlannerJumpstart.SuggestedContextAcquisitionActions) != 0 {
		t.Fatalf("expected no suggested acquisition actions for ready pass, got %d",
			len(resp.PlannerJumpstart.SuggestedContextAcquisitionActions))
	}
}

func TestGetNextPassWork_MissingSourceSnapshotIncludesJumpstart(t *testing.T) {
	t.Parallel()

	svc, st := newWorkPacketService(t)
	planSvc := NewService(st)
	plan := PlannerPassPlan{
		PlanMeta: PlanMeta{
			PlanID: "plan-js-snapshotmiss", SchemaVersion: "2.0.0",
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
			ContextPlan: noContextRequirementsPlan(),
			SourceSnapshotRequirements: SourceSnapshotRequirements{
				RequireGitStatus: boolPtr(true), RequireCommitSHA: boolPtr(false), AllowDirtyWorktree: boolPtr(true),
			},
			HandoffReadinessCriteria: []string{"c"},
		}},
	}
	raw := mustMarshalPlan(t, plan)
	result, err := planSvc.SubmitPlan(context.Background(), SubmitPlanRequest{UnmanagedAcknowledged: true, RawJSON: raw})
	if err != nil || !result.Report.Valid {
		t.Fatalf("SubmitPlan failed: err=%v issues=%+v", err, result.Report.Issues)
	}

	resp, err := svc.GetNextPassWork(context.Background(), NextPassWorkRequest{
		ProjectID: "relay",
		PlanID:    "plan-js-snapshotmiss",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should still maintain the blocker.
	assertBlockerCode(t, resp, BlockerRequiredSourceContextMissing)

	// But should include the jumpstart payload with actionable guidance.
	if resp.PlannerJumpstart == nil {
		t.Fatal("expected planner_jumpstart even when source snapshot is missing")
	}
	if resp.PlannerJumpstart.ReadinessState != "needs_source_snapshot" {
		t.Fatalf("expected readiness_state=needs_source_snapshot, got %q", resp.PlannerJumpstart.ReadinessState)
	}
	if len(resp.PlannerJumpstart.SuggestedContextAcquisitionActions) == 0 {
		t.Fatal("expected at least one suggested context acquisition action")
	}
	firstAction := resp.PlannerJumpstart.SuggestedContextAcquisitionActions[0]
	if firstAction.Tool != "create_source_snapshot" {
		t.Fatalf("expected first suggested action to be create_source_snapshot, got %q", firstAction.Tool)
	}
	// Must include selected pass summary for context prep.
	if resp.PlannerJumpstart.SelectedPassSummary == nil || resp.PlannerJumpstart.SelectedPassSummary.PassID != "PASS-001" {
		t.Fatalf("expected selected_pass_summary PASS-001 in jumpstart, got %+v", resp.PlannerJumpstart.SelectedPassSummary)
	}
}

func TestGetNextPassWork_MissingContextPacketIncludesJumpstart(t *testing.T) {
	t.Parallel()

	svc, st := newWorkPacketService(t)
	planSvc := NewService(st)
	plan := PlannerPassPlan{
		PlanMeta: PlanMeta{
			PlanID: "plan-js-packetmiss", SchemaVersion: "2.0.0",
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
			ContextPlan: baseContextPlan(),
			SourceSnapshotRequirements: SourceSnapshotRequirements{
				RequireGitStatus: boolPtr(false), RequireCommitSHA: boolPtr(false), AllowDirtyWorktree: boolPtr(true),
			},
			HandoffReadinessCriteria: []string{"c"},
		}},
	}
	raw := mustMarshalPlan(t, plan)
	result, err := planSvc.SubmitPlan(context.Background(), SubmitPlanRequest{UnmanagedAcknowledged: true, RawJSON: raw})
	if err != nil || !result.Report.Valid {
		t.Fatalf("SubmitPlan failed: err=%v issues=%+v", err, result.Report.Issues)
	}

	resp, err := svc.GetNextPassWork(context.Background(), NextPassWorkRequest{
		ProjectID: "relay",
		PlanID:    "plan-js-packetmiss",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The pass requires context and no source snapshot exists; source snapshot
	// is prerequisite, so the blocker is source_context_missing.
	assertBlockerCode(t, resp, BlockerRequiredSourceContextMissing)

	// But should include the jumpstart payload with actionable guidance.
	if resp.PlannerJumpstart == nil {
		t.Fatal("expected planner_jumpstart even when context packet is missing")
	}
	if resp.PlannerJumpstart.ReadinessState != "needs_source_snapshot" {
		t.Fatalf("expected readiness_state=needs_source_snapshot, got %q", resp.PlannerJumpstart.ReadinessState)
	}
	if len(resp.PlannerJumpstart.SuggestedContextAcquisitionActions) == 0 {
		t.Fatal("expected at least one suggested context acquisition action")
	}
	firstAction := resp.PlannerJumpstart.SuggestedContextAcquisitionActions[0]
	if firstAction.Tool != "create_source_snapshot" {
		t.Fatalf("expected first suggested action to be create_source_snapshot, got %q", firstAction.Tool)
	}
	// Must include pass and context details.
	if resp.PlannerJumpstart.SelectedPassSummary == nil || resp.PlannerJumpstart.SelectedPassSummary.PassID != "PASS-001" {
		t.Fatalf("expected selected_pass_summary PASS-001 in jumpstart, got %+v", resp.PlannerJumpstart.SelectedPassSummary)
	}
}

func TestGetNextPassWork_MissingContextPacketWithSnapshotIncludesInvokableAction(t *testing.T) {
	t.Parallel()

	svc, st := newWorkPacketService(t)
	planSvc := NewService(st)
	ctxPlan := baseContextPlan()
	plan := PlannerPassPlan{
		PlanMeta: PlanMeta{
			PlanID: "plan-js-packet-action", SchemaVersion: "2.0.0",
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
			ContextPlan: ctxPlan,
			SourceSnapshotRequirements: SourceSnapshotRequirements{
				RequireGitStatus: boolPtr(false), RequireCommitSHA: boolPtr(false), AllowDirtyWorktree: boolPtr(true),
			},
			ContextBudget:            &ContextBudget{MaxFiles: int64Ptr(12), MaxBytes: int64Ptr(131072), MaxSearchResults: int64Ptr(7)},
			HandoffReadinessCriteria: []string{"c"},
		}},
	}
	raw := mustMarshalPlan(t, plan)
	result, err := planSvc.SubmitPlan(context.Background(), SubmitPlanRequest{UnmanagedAcknowledged: true, RawJSON: raw})
	if err != nil || !result.Report.Valid {
		t.Fatalf("SubmitPlan failed: err=%v issues=%+v", err, result.Report.Issues)
	}
	seedSourceSnapshot(t, st, "relay", "srcsnap-existing")

	resp, err := svc.GetNextPassWork(context.Background(), NextPassWorkRequest{
		ProjectID: "relay",
		PlanID:    "plan-js-packet-action",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertBlockerCode(t, resp, BlockerRequiredContextPacketMissing)
	actions := resp.PlannerJumpstart.SuggestedContextAcquisitionActions
	if len(actions) != 1 {
		t.Fatalf("expected one suggested action, got %d: %+v", len(actions), actions)
	}
	action := actions[0]
	if action.Tool != "create_context_packet" {
		t.Fatalf("expected create_context_packet action, got %q", action.Tool)
	}
	if action.DependsOn != "" || len(action.ArgumentBindings) != 0 {
		t.Fatalf("expected no dependency when source snapshot is known, got depends_on=%q bindings=%v", action.DependsOn, action.ArgumentBindings)
	}
	args := action.Arguments
	for _, key := range []string{"project_id", "plan_id", "pass_id", "task_slug", "source_snapshot_id", "seed_files", "seed_searches", "include_inventory", "max_sources", "max_total_bytes"} {
		if _, ok := args[key]; !ok {
			t.Fatalf("expected create_context_packet arguments to include %q: %#v", key, args)
		}
	}
	if args["project_id"] != "relay" || args["plan_id"] != "plan-js-packet-action" || args["pass_id"] != "PASS-001" {
		t.Fatalf("unexpected ID arguments: %#v", args)
	}
	if args["task_slug"] != "next-pass-work-plan-js-packet-action-pass-001" {
		t.Fatalf("unexpected task_slug: %#v", args["task_slug"])
	}
	if args["source_snapshot_id"] != "srcsnap-existing" {
		t.Fatalf("unexpected source_snapshot_id: %#v", args["source_snapshot_id"])
	}
	if args["include_inventory"] != false || args["max_sources"] != 12 || args["max_total_bytes"] != 131072 {
		t.Fatalf("unexpected inventory/budget arguments: %#v", args)
	}
	seedFiles, ok := args["seed_files"].([]map[string]interface{})
	if !ok || len(seedFiles) != 1 {
		t.Fatalf("expected one seed_file, got %#v", args["seed_files"])
	}
	if seedFiles[0]["repo_id"] != "relay" || seedFiles[0]["path"] != ctxPlan.SeedFilesToRead[0].Path || seedFiles[0]["reason"] != ctxPlan.SeedFilesToRead[0].Purpose {
		t.Fatalf("unexpected seed_file: %#v", seedFiles[0])
	}
	seedSearches, ok := args["seed_searches"].([]map[string]interface{})
	if !ok || len(seedSearches) != 1 {
		t.Fatalf("expected one seed_search, got %#v", args["seed_searches"])
	}
	if seedSearches[0]["pattern"] != ctxPlan.SeedSearchTerms[0].Query || seedSearches[0]["reason"] != ctxPlan.SeedSearchTerms[0].Purpose || seedSearches[0]["max_results"] != 7 || seedSearches[0]["context_lines"] != 0 {
		t.Fatalf("unexpected seed_search: %#v", seedSearches[0])
	}
}

func TestGetNextPassWork_ContextPacketActionNormalizesRepoAliases(t *testing.T) {
	t.Parallel()

	svc, st := newWorkPacketService(t)
	project, err := st.GetProjectByProjectID("relay")
	if err != nil {
		t.Fatalf("GetProjectByProjectID: %v", err)
	}
	if _, err := st.UpsertProjectRepository(store.UpsertProjectRepositoryParams{
		ProjectRowID:     project.ID,
		RepoID:           "Paintersrp/relay",
		Role:             "primary",
		LocalPath:        t.TempDir(),
		DefaultBranch:    "main",
		AllowedRootsJSON: "[]",
		IgnoredGlobsJSON: "[]",
		MaxFileSizeBytes: 1024,
		Enabled:          1,
	}); err != nil {
		t.Fatalf("UpsertProjectRepository: %v", err)
	}
	seedSourceSnapshot(t, st, "relay", "srcsnap-alias")
	planSvc := NewService(st)
	plan := PlannerPassPlan{
		PlanMeta: PlanMeta{
			PlanID: "plan-js-packet-alias", SchemaVersion: "2.0.0",
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
			ContextPlan: baseContextPlan(),
			SourceSnapshotRequirements: SourceSnapshotRequirements{
				RequireGitStatus: boolPtr(false), RequireCommitSHA: boolPtr(false), AllowDirtyWorktree: boolPtr(true),
			},
			HandoffReadinessCriteria: []string{"c"},
		}},
	}
	raw := mustMarshalPlan(t, plan)
	result, err := planSvc.SubmitPlan(context.Background(), SubmitPlanRequest{UnmanagedAcknowledged: true, RawJSON: raw})
	if err != nil || !result.Report.Valid {
		t.Fatalf("SubmitPlan failed: err=%v issues=%+v", err, result.Report.Issues)
	}

	resp, err := svc.GetNextPassWork(context.Background(), NextPassWorkRequest{
		ProjectID: "relay",
		PlanID:    "plan-js-packet-alias",
	})
	if err != nil {
		t.Fatalf("GetNextPassWork: %v", err)
	}
	actions := resp.PlannerJumpstart.SuggestedContextAcquisitionActions
	if len(actions) != 1 || actions[0].Tool != "create_context_packet" {
		t.Fatalf("expected create_context_packet action, got %+v", actions)
	}
	seedFiles := actions[0].Arguments["seed_files"].([]map[string]interface{})
	if seedFiles[0]["repo_id"] != "Paintersrp/relay" {
		t.Fatalf("expected normalized seed file repo_id, got %#v", seedFiles[0])
	}
	seedSearches := actions[0].Arguments["seed_searches"].([]map[string]interface{})
	repoIDs := seedSearches[0]["repo_ids"].([]string)
	if len(repoIDs) != 1 || repoIDs[0] != "Paintersrp/relay" {
		t.Fatalf("expected normalized seed search repo_ids, got %#v", seedSearches[0])
	}
}

func TestGetNextPassWork_MissingContextPacketWithoutSnapshotIncludesDependency(t *testing.T) {
	t.Parallel()

	svc, st := newWorkPacketService(t)
	planSvc := NewService(st)
	plan := PlannerPassPlan{
		PlanMeta: PlanMeta{
			PlanID: "plan-js-packet-snapshot-action", SchemaVersion: "2.0.0",
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
			ContextPlan: baseContextPlan(),
			SourceSnapshotRequirements: SourceSnapshotRequirements{
				RequireGitStatus: boolPtr(false), RequireCommitSHA: boolPtr(false), AllowDirtyWorktree: boolPtr(true),
			},
			HandoffReadinessCriteria: []string{"c"},
		}},
	}
	raw := mustMarshalPlan(t, plan)
	result, err := planSvc.SubmitPlan(context.Background(), SubmitPlanRequest{UnmanagedAcknowledged: true, RawJSON: raw})
	if err != nil || !result.Report.Valid {
		t.Fatalf("SubmitPlan failed: err=%v issues=%+v", err, result.Report.Issues)
	}

	resp, err := svc.GetNextPassWork(context.Background(), NextPassWorkRequest{
		ProjectID: "relay",
		PlanID:    "plan-js-packet-snapshot-action",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertBlockerCode(t, resp, BlockerRequiredSourceContextMissing)
	actions := resp.PlannerJumpstart.SuggestedContextAcquisitionActions
	if len(actions) != 2 {
		t.Fatalf("expected two suggested actions, got %d: %+v", len(actions), actions)
	}
	if actions[0].Tool != "create_source_snapshot" {
		t.Fatalf("expected first action create_source_snapshot, got %q", actions[0].Tool)
	}
	if actions[1].Tool != "create_context_packet" {
		t.Fatalf("expected second action create_context_packet, got %q", actions[1].Tool)
	}
	if actions[1].DependsOn != "create_source_snapshot" {
		t.Fatalf("expected create_context_packet depends_on create_source_snapshot, got %q", actions[1].DependsOn)
	}
	if got := actions[1].ArgumentBindings["source_snapshot_id"]; got != "$.result.source_snapshot_id" {
		t.Fatalf("expected source_snapshot_id binding, got %q", got)
	}
	if _, ok := actions[1].Arguments["source_snapshot_id"]; ok {
		t.Fatalf("did not expect static source_snapshot_id when no snapshot exists: %#v", actions[1].Arguments)
	}
	for _, key := range []string{"project_id", "plan_id", "pass_id", "task_slug", "seed_files", "seed_searches", "include_inventory", "max_sources", "max_total_bytes"} {
		if _, ok := actions[1].Arguments[key]; !ok {
			t.Fatalf("expected create_context_packet static arguments to include %q: %#v", key, actions[1].Arguments)
		}
	}
}

// -------------------------------------------------------------------
// Optional pass_id tests
// -------------------------------------------------------------------

func TestGetNextPassWork_RequestedPassNotFound(t *testing.T) {
	t.Parallel()

	svc, st := newWorkPacketService(t)
	seedPlan(t, st, "relay", "plan-passid-notfound")

	resp, err := svc.GetNextPassWork(context.Background(), NextPassWorkRequest{
		ProjectID: "relay",
		PlanID:    "plan-passid-notfound",
		PassID:    "PASS-NONEXISTENT",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertBlockerCode(t, resp, BlockerRequestedPassNotFound)
}

func TestGetNextPassWork_RequestedPassBlocksOnPriorAudit(t *testing.T) {
	t.Parallel()

	svc, st := newWorkPacketService(t)
	seedPlan(t, st, "relay", "plan-passid-audit")

	setPassStatus(t, st, "plan-passid-audit", "PASS-001", StatusPassAuditReady)

	resp, err := svc.GetNextPassWork(context.Background(), NextPassWorkRequest{
		ProjectID: "relay",
		PlanID:    "plan-passid-audit",
		PassID:    "PASS-002",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertBlockerCode(t, resp, BlockerPriorPassAwaitsAudit)
}

func TestGetNextPassWork_RequestedPassSuccess(t *testing.T) {
	t.Parallel()

	svc, st := newWorkPacketService(t)
	seedPlan(t, st, "relay", "plan-passid-success")

	setPassStatus(t, st, "plan-passid-success", "PASS-001", StatusPassCompleted)

	// Request PASS-002 after PASS-001 is completed.
	resp, err := svc.GetNextPassWork(context.Background(), NextPassWorkRequest{
		ProjectID: "relay",
		PlanID:    "plan-passid-success",
		PassID:    "PASS-002",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.OK {
		t.Fatalf("expected ok=true for requested PASS-002, got blockers: %+v", resp.Blockers)
	}
	if resp.SelectedPass == nil || resp.SelectedPass.PassID != "PASS-002" {
		t.Fatalf("expected selected_pass PASS-002, got %+v", resp.SelectedPass)
	}
	if resp.PlannerJumpstart == nil {
		t.Fatal("expected planner_jumpstart for selected pass")
	}
	if resp.PlannerJumpstart.ReadinessState != "ready_for_handoff_authoring" {
		t.Fatalf("expected readiness_state=ready_for_handoff_authoring, got %q", resp.PlannerJumpstart.ReadinessState)
	}
}

func TestGetNextPassWork_RequestedPassAlreadyCompleted(t *testing.T) {
	t.Parallel()

	svc, st := newWorkPacketService(t)
	seedPlan(t, st, "relay", "plan-passid-completed")

	setPassStatus(t, st, "plan-passid-completed", "PASS-001", StatusPassCompleted)

	resp, err := svc.GetNextPassWork(context.Background(), NextPassWorkRequest{
		ProjectID: "relay",
		PlanID:    "plan-passid-completed",
		PassID:    "PASS-001",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertBlockerCode(t, resp, BlockerRequestedPassNotEligible)
}

func TestGetNextPassWork_RequestedPassWithActiveRun(t *testing.T) {
	t.Parallel()

	svc, st := newWorkPacketService(t)
	seedPlan(t, st, "relay", "plan-passid-activerun")

	repo, err := st.CreateRepo("test-repo", t.TempDir())
	if err != nil {
		t.Fatalf("CreateRepo: %v", err)
	}
	plan, err := st.GetPlanByPlanID("plan-passid-activerun")
	if err != nil {
		t.Fatalf("GetPlanByPlanID: %v", err)
	}
	pass, err := st.GetPlanPassByPassID(plan.ID, "PASS-001")
	if err != nil {
		t.Fatalf("GetPlanPassByPassID: %v", err)
	}
	_, err = st.CreateRunWithAssociation(
		repo.ID,
		"active run on PASS-001",
		"executor_running",
		"", "", "opencode_go", "main",
		sql.NullInt64{Int64: plan.ID, Valid: true},
		sql.NullInt64{Int64: pass.ID, Valid: true},
	)
	if err != nil {
		t.Fatalf("CreateRunWithAssociation: %v", err)
	}
	// Update pass status to reflect active run (real flow does this via lifecycle service).
	if _, err := st.UpdatePlanPassStatus(pass.ID, StatusPassInProgress); err != nil {
		t.Fatalf("UpdatePlanPassStatus: %v", err)
	}

	// PASS-001 is active, so requesting PASS-002 should block.
	resp, err := svc.GetNextPassWork(context.Background(), NextPassWorkRequest{
		ProjectID: "relay",
		PlanID:    "plan-passid-activerun",
		PassID:    "PASS-002",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertBlockerCode(t, resp, BlockerActiveRunExists)
}

func TestGetNextPassWork_ContextPacketUsabilityGates(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name                 string
		status               string
		blockedSeedCount     int64
		missingSeedCount     int64
		snapshotID           string // for the context packet
		selectedSnapshotID   string // for the project's latest source snapshot
		expectOK             bool
		expectReadinessState string
		expectBlockerCode    string
	}{
		{
			name:                 "status blocked",
			status:               "blocked",
			blockedSeedCount:     0,
			missingSeedCount:     0,
			snapshotID:           "snap-1",
			selectedSnapshotID:   "snap-1",
			expectOK:             false,
			expectReadinessState: "needs_context_packet",
			expectBlockerCode:    BlockerRequiredContextPacketMissing,
		},
		{
			name:                 "missing seeds",
			status:               "created",
			blockedSeedCount:     0,
			missingSeedCount:     2,
			snapshotID:           "snap-1",
			selectedSnapshotID:   "snap-1",
			expectOK:             false,
			expectReadinessState: "needs_context_packet",
			expectBlockerCode:    BlockerRequiredContextPacketMissing,
		},
		{
			name:                 "blocked seeds",
			status:               "created",
			blockedSeedCount:     3,
			missingSeedCount:     0,
			snapshotID:           "snap-1",
			selectedSnapshotID:   "snap-1",
			expectOK:             false,
			expectReadinessState: "needs_context_packet",
			expectBlockerCode:    BlockerRequiredContextPacketMissing,
		},
		{
			name:                 "snapshot ID mismatch",
			status:               "created",
			blockedSeedCount:     0,
			missingSeedCount:     0,
			snapshotID:           "snap-2",
			selectedSnapshotID:   "snap-1",
			expectOK:             false,
			expectReadinessState: "needs_context_packet",
			expectBlockerCode:    BlockerRequiredContextPacketMissing,
		},
		{
			name:                 "usable packet matching snapshot",
			status:               "created",
			blockedSeedCount:     0,
			missingSeedCount:     0,
			snapshotID:           "snap-1",
			selectedSnapshotID:   "snap-1",
			expectOK:             true,
			expectReadinessState: "ready_for_handoff_authoring",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			svc, st := newWorkPacketService(t)

			// Seed snapshots in order so that tc.selectedSnapshotID is the latest.
			if tc.snapshotID != "" && tc.snapshotID != tc.selectedSnapshotID {
				seedSourceSnapshot(t, st, "relay", tc.snapshotID)
			}
			if tc.selectedSnapshotID != "" {
				seedSourceSnapshot(t, st, "relay", tc.selectedSnapshotID)
			}

			// Submit plan with context requirements
			planSvc := NewService(st)
			plan := PlannerPassPlan{
				PlanMeta: PlanMeta{
					PlanID: "plan-test", SchemaVersion: "2.0.0",
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
					ContextPlan: baseContextPlan(),
					SourceSnapshotRequirements: SourceSnapshotRequirements{
						RequireGitStatus: boolPtr(false), RequireCommitSHA: boolPtr(false), AllowDirtyWorktree: boolPtr(true),
					},
					HandoffReadinessCriteria: []string{"c"},
				}},
			}
			raw := mustMarshalPlan(t, plan)
			result, err := planSvc.SubmitPlan(context.Background(), SubmitPlanRequest{UnmanagedAcknowledged: true, RawJSON: raw})
			if err != nil || !result.Report.Valid {
				t.Fatalf("SubmitPlan failed: err=%v issues=%+v", err, result.Report.Issues)
			}

			// Create context packet in DB
			project, err := st.GetProjectByProjectID("relay")
			if err != nil {
				t.Fatalf("GetProjectByProjectID: %v", err)
			}
			var snapshotRowID int64
			if tc.snapshotID != "" {
				snap, err := st.GetSourceSnapshotByID(tc.snapshotID)
				if err != nil {
					t.Fatalf("GetSourceSnapshotByID %q: %v", tc.snapshotID, err)
				}
				snapshotRowID = snap.ID
			}

			_, err = st.CreateContextPacket(store.CreateContextPacketParams{
				ContextPacketID:     "packet-1",
				ProjectRowID:        project.ID,
				ProjectID:           project.ProjectID,
				PlanID:              "plan-test",
				PassID:              "PASS-001",
				TaskSlug:            "slug",
				SourceSnapshotRowID: snapshotRowID,
				SourceSnapshotID:    tc.snapshotID,
				Status:              tc.status,
				BlockedSeedCount:    tc.blockedSeedCount,
				MissingSeedCount:    tc.missingSeedCount,
				CompletedAt:         "2026-06-28T12:00:00Z",
				PacketJSONPath:      "/artifacts/ctxpkt/packet-1.json",
				CoverageReportPath:  "/artifacts/ctxpkt/packet-1-coverage.json",
			})
			if err != nil {
				t.Fatalf("CreateContextPacket: %v", err)
			}

			resp, err := svc.GetNextPassWork(context.Background(), NextPassWorkRequest{
				ProjectID: "relay",
				PlanID:    "plan-test",
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if resp.OK != tc.expectOK {
				t.Errorf("expected OK=%t, got %t", tc.expectOK, resp.OK)
			}

			if resp.Context == nil {
				t.Fatal("expected Context in response to be non-nil")
			}
			if resp.Context.ContextReady != tc.expectOK {
				t.Errorf("expected ContextReady=%t, got %t", tc.expectOK, resp.Context.ContextReady)
			}

			if resp.PlannerJumpstart == nil {
				t.Fatal("expected PlannerJumpstart to be non-nil")
			}
			if resp.PlannerJumpstart.ReadinessState != tc.expectReadinessState {
				t.Errorf("expected ReadinessState=%q, got %q", tc.expectReadinessState, resp.PlannerJumpstart.ReadinessState)
			}

			if tc.expectOK {
				if resp.HandoffWork == nil {
					t.Error("expected HandoffWork to be non-nil")
				} else {
					if resp.HandoffWork.SourceSnapshotID != tc.selectedSnapshotID {
						t.Errorf("expected HandoffWork.SourceSnapshotID=%q, got %q", tc.selectedSnapshotID, resp.HandoffWork.SourceSnapshotID)
					}
				}
				if len(resp.Blockers) > 0 {
					t.Errorf("expected no blockers, got %+v", resp.Blockers)
				}
			} else {
				if resp.HandoffWork != nil {
					t.Error("expected HandoffWork to be nil")
				}
				assertBlockerCode(t, resp, tc.expectBlockerCode)

				var foundCreateContextPacket bool
				for _, act := range resp.PlannerJumpstart.SuggestedContextAcquisitionActions {
					if act.Tool == "create_context_packet" {
						foundCreateContextPacket = true
						if act.Arguments["source_snapshot_id"] != tc.selectedSnapshotID {
							t.Errorf("expected suggested action source_snapshot_id=%q, got %q", tc.selectedSnapshotID, act.Arguments["source_snapshot_id"])
						}
					}
				}
				if !foundCreateContextPacket {
					t.Error("expected suggested actions to include create_context_packet")
				}
			}
		})
	}
}

func TestGetNextPassWork_PASS002FallbackCreatesUsableHandoffWork(t *testing.T) {
	t.Parallel()
	svc, st := newWorkPacketService(t)
	seedPass002AcquisitionPlan(t, st, "plan-pass002-fallback")
	svc.SetSourceService(fakeSourceSnapshotAcquirer{snapshotID: "snap-pass002", status: "created", included: 10})
	fakeCtx := &fakeContextPacketAcquirer{results: []CtxPacketResult{
		{
			ContextPacketID:    "ctxpkt-planned",
			Status:             "partial",
			CoverageReportPath: "/artifacts/ctxpkt/planned-coverage.json",
			SourceSnapshotID:   "snap-pass002",
			SourceCount:        12,
			Truncated:          true,
			Summary: CtxPacketSummary{
				SourceCount:      12,
				CoveredSeedCount: 6,
				Truncated:        true,
				MaxSources:       12,
				MaxTotalBytes:    180000,
				TotalSourceBytes: 180000,
			},
			Coverage: []CtxCoverageEntry{
				{SeedID: "file:1", SeedType: "file", Required: true, Status: "covered", Path: "internal/app/plans/work_packets.go"},
				{SeedID: "search:2", SeedType: "search", Required: true, Status: "partial", Pattern: "context_acquisition_failed acquisition_failure_report", Truncated: true},
			},
			LimitHit: "max_sources",
		},
		{
			ContextPacketID:    "ctxpkt-focused",
			Status:             "created",
			CoverageReportPath: "/artifacts/ctxpkt/focused-coverage.json",
			SourceSnapshotID:   "snap-pass002",
			SourceCount:        7,
			Summary: CtxPacketSummary{
				SourceCount:      7,
				CoveredSeedCount: 7,
				MaxSources:       12,
				MaxTotalBytes:    180000,
			},
			LimitHit: "none",
		},
	}}
	svc.SetContextPacketService(fakeCtx)

	resp, err := svc.GetNextPassWork(context.Background(), NextPassWorkRequest{ProjectID: "relay", PlanID: "plan-pass002-fallback"})
	if err != nil {
		t.Fatalf("GetNextPassWork: %v", err)
	}
	if !resp.OK || resp.HandoffWork == nil || resp.PlannerJumpstart.ReadinessState != "ready_for_handoff_authoring" {
		t.Fatalf("expected fallback handoff work, got ok=%t readiness=%q handoff=%v blockers=%+v", resp.OK, resp.PlannerJumpstart.ReadinessState, resp.HandoffWork, resp.Blockers)
	}
	if resp.Context == nil || !resp.Context.ContextReady || resp.Context.ContextPacketID != "ctxpkt-focused" {
		t.Fatalf("expected focused context packet ready, got %+v", resp.Context)
	}
	if len(fakeCtx.inputs) != 2 {
		t.Fatalf("expected two context acquisition attempts, got %d", len(fakeCtx.inputs))
	}
	if fakeCtx.inputs[0].IncludeInventory || fakeCtx.inputs[0].MaxSources != 12 || fakeCtx.inputs[0].MaxTotalBytes != 180000 || fakeCtx.inputs[0].SeedSearches[0].MaxResults != 25 || fakeCtx.inputs[0].SeedSearches[0].ContextLines != 0 {
		t.Fatalf("unexpected planned attempt input: %+v", fakeCtx.inputs[0])
	}
	if fakeCtx.inputs[1].IncludeInventory || fakeCtx.inputs[1].MaxSources != 12 || fakeCtx.inputs[1].MaxTotalBytes != 180000 || fakeCtx.inputs[1].SeedSearches[0].MaxResults != 10 || fakeCtx.inputs[1].SeedSearches[0].ContextLines != 0 {
		t.Fatalf("unexpected focused attempt input: %+v", fakeCtx.inputs[1])
	}
}

func TestContextPacketResultUsableForHandoffAllowsOptionalSearchPruning(t *testing.T) {
	result := &CtxPacketResult{
		ContextPacketID:    "ctxpkt-optional-search-pruned",
		Status:             "created",
		CoverageReportPath: "/artifacts/coverage.json",
		SourceSnapshotID:   "snap-ready",
		SourceCount:        2,
		Summary: CtxPacketSummary{
			SourceCount:             2,
			CoveredSeedCount:        1,
			OptionalSearchTruncated: true,
			MaxSources:              10,
			MaxTotalBytes:           180000,
		},
		Coverage: []CtxCoverageEntry{
			{SeedID: "file:1", SeedType: "file", Required: true, Status: "covered", Path: "internal/app/plans/work_packets.go"},
			{SeedID: "search:1", SeedType: "search", Required: false, Status: "partial", Pattern: "optional", Truncated: true, TruncationClass: "optional_search_truncated"},
		},
		LimitHit: "none",
	}

	usable, reason := contextPacketResultUsableForHandoff(result, "snap-ready")
	if !usable || reason != "" {
		t.Fatalf("expected optional search pruning to remain usable, usable=%t reason=%q", usable, reason)
	}
	summary := packetDiagnosticSummary(result)
	if summary == nil || !summary.OptionalSearchTruncated || summary.Truncated || summary.LimitHit != "none" {
		t.Fatalf("expected optional search pruning diagnostics without blocking truncation, got %+v", summary)
	}
	coverage := coverageDiagnosticSummary(result)
	if coverage == nil || !coverage.OptionalSearchTruncated || len(coverage.OptionalSearchTruncatedSeedIDs) != 1 || coverage.RequiredSeedTruncatedCount != 0 {
		t.Fatalf("expected optional search coverage diagnostics, got %+v", coverage)
	}
}

func TestContextPacketResultUsableForHandoffBlocksRequiredSearchNonExhaustive(t *testing.T) {
	result := &CtxPacketResult{
		ContextPacketID:    "ctxpkt-required-search",
		Status:             "partial",
		CoverageReportPath: "/artifacts/coverage.json",
		SourceSnapshotID:   "snap-ready",
		SourceCount:        1,
		Truncated:          true,
		Summary: CtxPacketSummary{
			SourceCount:                 1,
			Truncated:                   true,
			RequiredSearchNonExhaustive: true,
			MaxSources:                  10,
			MaxTotalBytes:               180000,
		},
		Coverage: []CtxCoverageEntry{
			{SeedID: "search:1", SeedType: "search", Required: true, Status: "partial", Pattern: "required", Truncated: true, TruncationClass: "required_search_non_exhaustive"},
		},
		LimitHit: "required_search_non_exhaustive",
	}

	usable, reason := contextPacketResultUsableForHandoff(result, "snap-ready")
	if usable || !strings.Contains(reason, "partial") {
		t.Fatalf("expected required search non-exhaustive packet to be unusable, usable=%t reason=%q", usable, reason)
	}
	blocker := blockerForContextPacketResult(result, reason)
	if blocker == nil || blocker.Code != BlockerContextPacketTruncated || !strings.Contains(blocker.Message, "required_search_non_exhaustive") {
		t.Fatalf("expected precise truncation blocker, got %+v", blocker)
	}
}

func TestGetNextPassWork_ContextAcquisitionFailureReport(t *testing.T) {
	t.Parallel()
	svc, st := newWorkPacketService(t)
	seedPass002AcquisitionPlan(t, st, "plan-pass002-failure")
	svc.SetSourceService(fakeSourceSnapshotAcquirer{snapshotID: "snap-pass002-fail", status: "created", included: 10})
	fakeCtx := &fakeContextPacketAcquirer{results: []CtxPacketResult{
		{
			ContextPacketID:    "ctxpkt-planned-fail",
			Status:             "created",
			CoverageReportPath: "/artifacts/ctxpkt/planned-fail-coverage.json",
			SourceSnapshotID:   "snap-pass002-fail",
			SourceCount:        12,
			Truncated:          true,
			Summary:            CtxPacketSummary{SourceCount: 12, Truncated: true, MaxSources: 12, MaxTotalBytes: 180000},
			Coverage: []CtxCoverageEntry{
				{SeedID: "file:1", SeedType: "file", Required: true, Status: "covered", Path: "internal/app/plans/work_packets.go"},
				{SeedID: "search:2", SeedType: "search", Required: true, Status: "partial", Pattern: "context_acquisition_failed acquisition_failure_report", Truncated: true},
			},
			LimitHit: "max_sources",
		},
		{
			ContextPacketID:    "ctxpkt-focused-fail",
			Status:             "blocked",
			CoverageReportPath: "/artifacts/ctxpkt/focused-fail-coverage.json",
			SourceSnapshotID:   "snap-pass002-fail",
			BlockedSeedCount:   1,
			SourceCount:        6,
			Summary:            CtxPacketSummary{SourceCount: 6, CoveredSeedCount: 6, BlockedSeedCount: 1, MaxSources: 12, MaxTotalBytes: 180000},
			Coverage: []CtxCoverageEntry{
				{SeedID: "file:1", SeedType: "file", Required: true, Status: "covered", Path: "internal/app/plans/work_packets.go"},
				{SeedID: "file:5", SeedType: "file", Required: true, Status: "blocked", Path: "relay-contracts/contracts/planner_mcp_orchestrator_work_contract.md", MissingCause: "blocked"},
			},
			LimitHit: "none",
		},
	}}
	svc.SetContextPacketService(fakeCtx)

	resp, err := svc.GetNextPassWork(context.Background(), NextPassWorkRequest{ProjectID: "relay", PlanID: "plan-pass002-failure"})
	if err != nil {
		t.Fatalf("GetNextPassWork: %v", err)
	}
	if resp.OK || resp.HandoffWork != nil {
		t.Fatalf("expected terminal failure without handoff work, got ok=%t handoff=%v", resp.OK, resp.HandoffWork)
	}
	if resp.PlannerJumpstart == nil || resp.PlannerJumpstart.ReadinessState != "context_acquisition_failed" {
		t.Fatalf("expected context_acquisition_failed, got %+v", resp.PlannerJumpstart)
	}
	if resp.AcquisitionFailureReport == nil {
		t.Fatal("expected acquisition_failure_report")
	}
	report := resp.AcquisitionFailureReport
	if report.FailureCode != BlockerContextCoverageIncomplete || report.ContextPacketID != "ctxpkt-focused-fail" || report.PacketSummary == nil || report.CoverageSummary == nil {
		t.Fatalf("unexpected report: %+v", report)
	}
	if len(report.AttemptedStrategies) != 2 || report.AttemptedStrategies[1].Strategy.Name != "focused_required_context" {
		t.Fatalf("expected two strategy reports, got %+v", report.AttemptedStrategies)
	}
	for _, action := range resp.PlannerJumpstart.SuggestedContextAcquisitionActions {
		if action.Tool == "create_context_packet" {
			t.Fatalf("did not expect manual create_context_packet action after backend retry failure: %+v", resp.PlannerJumpstart.SuggestedContextAcquisitionActions)
		}
	}
}

// -------------------------------------------------------------------
// Range-planning tests (Rev 12)
// -------------------------------------------------------------------

// singleRequiredFileContextPlan builds a schema-valid context plan with one
// required seed file and one required seed search.
func singleRequiredFileContextPlan(path string) ContextPlan {
	return ContextPlan{
		RequiredRepositories: []string{"relay"},
		SeedFilesToRead: []ContextFileRead{
			{RepoID: "relay", Path: path, Purpose: "required file", Required: boolPtr(true)},
		},
		SeedSearchTerms: []ContextSearchTerm{
			{RepoID: "relay", Query: "range planning", Purpose: "required search", Required: boolPtr(true)},
		},
		ContextCoverageExpectations: []string{"Required evidence is available before handoff authoring."},
		BlockedIfMissing:            []string{"Required context cannot be acquired."},
	}
}

// seedAcquisitionPlanWithContext registers the relay repo and submits a
// two-pass plan whose PASS-002 carries the provided context plan and budget.
// PASS-001 is forced to completed so PASS-002 is the eligible candidate.
func seedAcquisitionPlanWithContext(t *testing.T, st *store.Store, planID string, ctxPlan ContextPlan, budget *ContextBudget) {
	t.Helper()
	project, err := st.GetProjectByProjectID("relay")
	if err != nil {
		t.Fatalf("GetProjectByProjectID: %v", err)
	}
	if _, err := st.UpsertProjectRepository(store.UpsertProjectRepositoryParams{
		ProjectRowID:     project.ID,
		RepoID:           "relay",
		Role:             "primary",
		LocalPath:        "D:/Code/relay",
		DefaultBranch:    "main",
		AllowedRootsJSON: `["."]`,
		MaxFileSizeBytes: 1048576,
		Enabled:          1,
	}); err != nil {
		t.Fatalf("UpsertProjectRepository: %v", err)
	}

	planSvc := NewService(st)
	plan := PlannerPassPlan{
		PlanMeta: PlanMeta{
			PlanID:        planID,
			SchemaVersion: "2.0.0",
			CreatedAt:     "2026-06-28T00:00:00Z",
			Title:         "Range planning test",
			Goal:          "Exercise required seed range planning.",
			RepoTarget:    "Paintersrp/relay",
			BranchContext: "main",
			Status:        "active",
			ProjectID:     "relay",
			MCPCapabilityProfile: &MCPCapabilityProfile{
				ProfileID:            "test-profile",
				Mode:                 "submission_only",
				ContextBrokerEnabled: boolPtr(false),
			},
		},
		SourceIntent:       SourceIntent{Summary: "Range planning acquisition test."},
		GlobalContextRules: &GlobalContextRules{DefaultSourceOfTruth: "test", PlannerContextBoundary: "test", ForbiddenContextDomains: []string{"external"}},
		Passes: []PlanPassInput{
			{
				PassID: "PASS-001", Sequence: 1, Name: "First", Goal: "First complete pass.",
				IntendedExecutionScope: []string{"setup"}, NonGoals: []string{"none"},
				Dependencies: []string{}, Status: "planned", PassType: "backend_vertical_slice",
				ContextPlan:                noContextRequirementsPlan(),
				SourceSnapshotRequirements: SourceSnapshotRequirements{RequireGitStatus: boolPtr(false), RequireCommitSHA: boolPtr(false), AllowDirtyWorktree: boolPtr(true)},
				HandoffReadinessCriteria:   []string{"complete"},
			},
			{
				PassID: "PASS-002", Sequence: 2, Name: "Second", Goal: "Range planning context acquisition.",
				IntendedExecutionScope: []string{"backend"}, NonGoals: []string{"validation tiers"},
				Dependencies: []string{"PASS-001"}, Status: "planned", PassType: "workflow_backend_mcp",
				ContextPlan:                ctxPlan,
				SourceSnapshotRequirements: SourceSnapshotRequirements{RequireGitStatus: boolPtr(false), RequireCommitSHA: boolPtr(false), AllowDirtyWorktree: boolPtr(true)},
				HandoffReadinessCriteria:   []string{"context acquired"},
				ContextBudget:              budget,
			},
		},
	}
	result, err := planSvc.SubmitPlan(context.Background(), SubmitPlanRequest{UnmanagedAcknowledged: true, RawJSON: mustMarshalPlan(t, plan)})
	if err != nil || !result.Report.Valid {
		t.Fatalf("SubmitPlan failed: err=%v issues=%+v", err, result.Report.Issues)
	}
	setPassStatus(t, st, planID, "PASS-001", StatusPassCompleted)
}

// seedSnapshotFileMetadata creates a usable source snapshot with a relay
// repository row and the provided file metadata rows.
func seedSnapshotFileMetadata(t *testing.T, st *store.Store, snapshotID string, files []store.CreateSourceSnapshotFileParams) {
	t.Helper()
	seedSnapshotFileMetadataByRepo(t, st, snapshotID, map[string][]store.CreateSourceSnapshotFileParams{
		"relay": files,
	})
}

// seedSnapshotFileMetadataByRepo creates a usable source snapshot with file
// metadata rows for each provided repository ID.
func seedSnapshotFileMetadataByRepo(t *testing.T, st *store.Store, snapshotID string, filesByRepo map[string][]store.CreateSourceSnapshotFileParams) {
	t.Helper()
	project, err := st.GetProjectByProjectID("relay")
	if err != nil {
		t.Fatalf("GetProjectByProjectID: %v", err)
	}
	snapshot, err := st.CreateSourceSnapshot(store.CreateSourceSnapshotParams{
		SourceSnapshotID: snapshotID,
		ProjectRowID:     project.ID,
		ProjectID:        "relay",
		SnapshotKind:     "clean_commit",
		Status:           "created",
		CompletedAt:      "2026-06-28T00:00:00Z",
		SummaryJSON:      "{\"file_count\":1}",
	})
	if err != nil {
		t.Fatalf("CreateSourceSnapshot: %v", err)
	}
	existingRepos, err := st.ListProjectRepositories(project.ID)
	if err != nil {
		t.Fatalf("ListProjectRepositories: %v", err)
	}
	existingRepoIDs := map[string]struct{}{}
	for _, repo := range existingRepos {
		existingRepoIDs[repo.RepoID] = struct{}{}
	}
	for repoID := range filesByRepo {
		if _, ok := existingRepoIDs[repoID]; ok {
			continue
		}
		role := "reference"
		if repoID == "relay-contracts" {
			role = "contracts"
		}
		if _, err := st.UpsertProjectRepository(store.UpsertProjectRepositoryParams{
			ProjectRowID:     project.ID,
			RepoID:           repoID,
			Role:             role,
			LocalPath:        "D:/Code/" + repoID,
			DefaultBranch:    "main",
			AllowedRootsJSON: `["."]`,
			MaxFileSizeBytes: 1048576,
			Enabled:          1,
		}); err != nil {
			t.Fatalf("UpsertProjectRepository %q: %v", repoID, err)
		}
	}
	repos, err := st.ListProjectRepositories(project.ID)
	if err != nil {
		t.Fatalf("ListProjectRepositories: %v", err)
	}
	repoRowIDs := map[string]int64{}
	for _, r := range repos {
		repoRowIDs[r.RepoID] = r.ID
	}
	for repoID, files := range filesByRepo {
		repoRowID := repoRowIDs[repoID]
		if repoRowID == 0 {
			t.Fatalf("%s project repository not registered", repoID)
		}
		snapRepo, err := st.CreateSourceSnapshotRepository(store.CreateSourceSnapshotRepositoryParams{
			SourceSnapshotRowID:    snapshot.ID,
			ProjectRepositoryRowID: repoRowID,
			RepoID:                 repoID,
			Role:                   "reference",
			LocalPath:              "D:/Code/" + repoID,
			DefaultBranch:          "main",
			CurrentBranch:          "main",
			GitStatusAvailable:     1,
		})
		if err != nil {
			t.Fatalf("CreateSourceSnapshotRepository %q: %v", repoID, err)
		}
		for _, f := range files {
			f.SourceSnapshotRepositoryRowID = snapRepo.ID
			if _, err := st.CreateSourceSnapshotFile(f); err != nil {
				t.Fatalf("CreateSourceSnapshotFile %q: %v", repoID, err)
			}
		}
	}
}

// fakeContextPacketAcquirer that always returns a created, usable packet.
func usableContextPacketAcquirer(snapshotID string) *fakeContextPacketAcquirer {
	return &fakeContextPacketAcquirer{results: []CtxPacketResult{{
		ContextPacketID:    "ctxpkt-range-ok",
		Status:             "created",
		CoverageReportPath: "/artifacts/ctxpkt/range-ok-coverage.json",
		SourceSnapshotID:   snapshotID,
		SourceCount:        2,
		Summary:            CtxPacketSummary{SourceCount: 2, CoveredSeedCount: 2, MaxSources: 12, MaxTotalBytes: 180000},
		LimitHit:           "none",
	}}}
}

func TestGetNextPassWork_SuggestedSeedFilesIncludeRangeAndBudget(t *testing.T) {
	t.Parallel()
	svc, st := newWorkPacketService(t)
	seedAcquisitionPlanWithContext(t, st, "plan-range-suggested",
		singleRequiredFileContextPlan("internal/app/plans/work_packets.go"),
		&ContextBudget{MaxFiles: int64Ptr(12), MaxBytes: int64Ptr(180000), MaxSearchResults: int64Ptr(25)})
	// Snapshot present but without file metadata: metadata-unavailable fallback.
	seedSourceSnapshot(t, st, "relay", "snap-range-suggested")

	resp, err := svc.GetNextPassWork(context.Background(), NextPassWorkRequest{ProjectID: "relay", PlanID: "plan-range-suggested"})
	if err != nil {
		t.Fatalf("GetNextPassWork: %v", err)
	}
	assertBlockerCode(t, resp, BlockerRequiredContextPacketMissing)
	var action *ContextAcquisitionAction
	for i := range resp.PlannerJumpstart.SuggestedContextAcquisitionActions {
		if resp.PlannerJumpstart.SuggestedContextAcquisitionActions[i].Tool == "create_context_packet" {
			action = &resp.PlannerJumpstart.SuggestedContextAcquisitionActions[i]
		}
	}
	if action == nil {
		t.Fatalf("expected create_context_packet action, got %+v", resp.PlannerJumpstart.SuggestedContextAcquisitionActions)
	}
	seedFiles, ok := action.Arguments["seed_files"].([]map[string]interface{})
	if !ok || len(seedFiles) != 1 {
		t.Fatalf("expected one seed file, got %#v", action.Arguments["seed_files"])
	}
	if seedFiles[0]["line_start"] != 1 {
		t.Fatalf("expected line_start=1, got %#v", seedFiles[0])
	}
	if _, ok := seedFiles[0]["line_end"]; ok {
		t.Fatalf("did not expect required seed line_end cap, got %#v", seedFiles[0])
	}
	if seedFiles[0]["max_bytes"] != 180000 {
		t.Fatalf("expected max_bytes=180000, got %#v", seedFiles[0]["max_bytes"])
	}
}

func TestGetNextPassWork_InternalSeedFilesUseRangeAndPassBudgetMaxBytes(t *testing.T) {
	t.Parallel()
	svc, st := newWorkPacketService(t)
	seedPass002AcquisitionPlan(t, st, "plan-range-internal")
	svc.SetSourceService(fakeSourceSnapshotAcquirer{snapshotID: "snap-range-internal", status: "created", included: 10})
	fakeCtx := usableContextPacketAcquirer("snap-range-internal")
	svc.SetContextPacketService(fakeCtx)

	resp, err := svc.GetNextPassWork(context.Background(), NextPassWorkRequest{ProjectID: "relay", PlanID: "plan-range-internal"})
	if err != nil {
		t.Fatalf("GetNextPassWork: %v", err)
	}
	if !resp.OK {
		t.Fatalf("expected ok=true, got blockers: %+v", resp.Blockers)
	}
	if len(fakeCtx.inputs) == 0 {
		t.Fatal("expected at least one context acquisition attempt")
	}
	var required *CtxSeedFile
	for i := range fakeCtx.inputs[0].SeedFiles {
		if fakeCtx.inputs[0].SeedFiles[i].Required {
			required = &fakeCtx.inputs[0].SeedFiles[i]
			break
		}
	}
	if required == nil {
		t.Fatalf("expected a required seed file, got %+v", fakeCtx.inputs[0].SeedFiles)
	}
	if required.LineStart != 1 || required.LineEnd != 0 {
		t.Fatalf("expected open-ended required seed from line 1, got %+v", required)
	}
	// Pass budget max_bytes=180000 must be used, not defaultSeedFileMaxBytes.
	if required.MaxBytes != 180000 {
		t.Fatalf("expected internal seed MaxBytes=180000, got %d", required.MaxBytes)
	}
	if required.MaxBytes == defaultSeedFileMaxBytes {
		t.Fatalf("internal seed MaxBytes must not fall back to defaultSeedFileMaxBytes (%d)", defaultSeedFileMaxBytes)
	}
}

func TestGetNextPassWork_MetadataPresentSmallFileUsesOpenEndedChunkingRange(t *testing.T) {
	t.Parallel()
	svc, st := newWorkPacketService(t)
	seedAcquisitionPlanWithContext(t, st, "plan-range-small",
		singleRequiredFileContextPlan("internal/app/plans/work_packets.go"),
		&ContextBudget{MaxFiles: int64Ptr(12), MaxBytes: int64Ptr(180000), MaxSearchResults: int64Ptr(25)})
	seedSnapshotFileMetadata(t, st, "snap-range-small", []store.CreateSourceSnapshotFileParams{
		{Path: "internal/app/plans/work_packets.go", SizeBytes: 4096, ContentHash: "h1", HashAlgorithm: "sha256", Tracked: 1, Included: 1},
	})
	svc.SetSourceService(fakeSourceSnapshotAcquirer{snapshotID: "unused", status: "created", included: 1})
	fakeCtx := usableContextPacketAcquirer("snap-range-small")
	svc.SetContextPacketService(fakeCtx)

	resp, err := svc.GetNextPassWork(context.Background(), NextPassWorkRequest{ProjectID: "relay", PlanID: "plan-range-small"})
	if err != nil {
		t.Fatalf("GetNextPassWork: %v", err)
	}
	if !resp.OK {
		t.Fatalf("expected ok=true, got blockers: %+v", resp.Blockers)
	}
	if len(fakeCtx.inputs) == 0 {
		t.Fatal("expected a context acquisition attempt")
	}
	var required *CtxSeedFile
	for i := range fakeCtx.inputs[0].SeedFiles {
		if fakeCtx.inputs[0].SeedFiles[i].Required {
			required = &fakeCtx.inputs[0].SeedFiles[i]
			break
		}
	}
	if required == nil || required.LineStart != 1 || required.LineEnd != 0 {
		t.Fatalf("expected metadata-backed open-ended required range from line 1, got %+v", required)
	}
}

func TestGetNextPassWork_MetadataPresentLargeRequiredFileUsesChunkingRange(t *testing.T) {
	t.Parallel()
	svc, st := newWorkPacketService(t)
	seedAcquisitionPlanWithContext(t, st, "plan-range-large",
		singleRequiredFileContextPlan("internal/app/plans/huge.go"),
		&ContextBudget{MaxFiles: int64Ptr(12), MaxBytes: int64Ptr(180000), MaxSearchResults: int64Ptr(25)})
	seedSnapshotFileMetadata(t, st, "snap-range-large", []store.CreateSourceSnapshotFileParams{
		{Path: "internal/app/plans/huge.go", SizeBytes: 250000, ContentHash: "h2", HashAlgorithm: "sha256", Tracked: 1, Included: 1},
	})
	svc.SetSourceService(fakeSourceSnapshotAcquirer{snapshotID: "unused", status: "created", included: 1})
	fakeCtx := usableContextPacketAcquirer("snap-range-large")
	svc.SetContextPacketService(fakeCtx)

	resp, err := svc.GetNextPassWork(context.Background(), NextPassWorkRequest{ProjectID: "relay", PlanID: "plan-range-large"})
	if err != nil {
		t.Fatalf("GetNextPassWork: %v", err)
	}
	if !resp.OK {
		t.Fatalf("expected ok=true with chunk-capable required acquisition, got blockers: %+v", resp.Blockers)
	}
	if len(fakeCtx.inputs) == 0 {
		t.Fatal("expected context acquirer call for large required file")
	}
	var required *CtxSeedFile
	for i := range fakeCtx.inputs[0].SeedFiles {
		if fakeCtx.inputs[0].SeedFiles[i].Required {
			required = &fakeCtx.inputs[0].SeedFiles[i]
			break
		}
	}
	if required == nil || required.LineStart != 1 || required.LineEnd != 0 {
		t.Fatalf("expected large required seed to use open-ended chunking range, got %+v", required)
	}
}

func TestGetNextPassWork_MetadataPresentMissingFileFailsClosed(t *testing.T) {
	t.Parallel()
	svc, st := newWorkPacketService(t)
	seedAcquisitionPlanWithContext(t, st, "plan-range-missing",
		singleRequiredFileContextPlan("internal/app/plans/absent.go"),
		&ContextBudget{MaxFiles: int64Ptr(12), MaxBytes: int64Ptr(180000), MaxSearchResults: int64Ptr(25)})
	// Metadata index present but does not contain the required file (and one is excluded).
	seedSnapshotFileMetadata(t, st, "snap-range-missing", []store.CreateSourceSnapshotFileParams{
		{Path: "internal/app/plans/other.go", SizeBytes: 1024, ContentHash: "h3", HashAlgorithm: "sha256", Tracked: 1, Included: 1},
	})
	svc.SetSourceService(fakeSourceSnapshotAcquirer{snapshotID: "unused", status: "created", included: 1})
	fakeCtx := usableContextPacketAcquirer("snap-range-missing")
	svc.SetContextPacketService(fakeCtx)

	resp, err := svc.GetNextPassWork(context.Background(), NextPassWorkRequest{ProjectID: "relay", PlanID: "plan-range-missing"})
	if err != nil {
		t.Fatalf("GetNextPassWork: %v", err)
	}
	if resp.OK || resp.HandoffWork != nil {
		t.Fatalf("expected fail-closed without handoff work, got ok=%t handoff=%v", resp.OK, resp.HandoffWork)
	}
	if len(resp.Blockers) == 0 || resp.Blockers[0].Code != BlockerRequiredSeedFileMissingFromSnapshot {
		t.Fatalf("expected required_seed_file_missing_from_snapshot blocker, got %+v", resp.Blockers)
	}
	if len(fakeCtx.inputs) != 0 {
		t.Fatalf("expected no acquirer call before range-planning failure, got %d", len(fakeCtx.inputs))
	}
	if resp.AcquisitionFailureReport == nil || resp.AcquisitionFailureReport.SeedRangeFailure == nil {
		t.Fatalf("expected seed_range_failure report, got %+v", resp.AcquisitionFailureReport)
	}
}

func TestGetNextPassWork_ReadyPassIncludesRequiredContextBundleWithSnapshotHashes(t *testing.T) {
	t.Parallel()
	svc, st := newWorkPacketService(t)
	ctxPlan := singleRequiredFileContextPlan("internal/app/plans/work_packets.go")
	ctxPlan.SeedFilesToRead = append(ctxPlan.SeedFilesToRead,
		ContextFileRead{RepoID: "relay", Path: "docs/mcp.md", Purpose: "optional operator docs", Required: boolPtr(false)})
	ctxPlan.SeedSearchTerms = append(ctxPlan.SeedSearchTerms,
		ContextSearchTerm{RepoID: "relay", Query: "required_context_bundle", Purpose: "optional schema/docs search", Required: boolPtr(false)})
	seedAcquisitionPlanWithContext(t, st, "plan-required-context-bundle", ctxPlan,
		&ContextBudget{MaxFiles: int64Ptr(12), MaxBytes: int64Ptr(180000), MaxSearchResults: int64Ptr(25), MaxContextLines: int64Ptr(50)})
	seedSnapshotFileMetadataByRepo(t, st, "snap-required-context-bundle", map[string][]store.CreateSourceSnapshotFileParams{
		"relay": {
			{Path: "internal/app/plans/work_packets.go", SizeBytes: 4096, ContentHash: "hash-work-packets", HashAlgorithm: "sha256", Tracked: 1, Included: 1},
			{Path: "docs/mcp.md", SizeBytes: 2048, ContentHash: "hash-docs-mcp", HashAlgorithm: "sha256", Tracked: 1, Included: 1},
		},
		"relay-contracts": {
			{Path: requiredContextManifestPath, SizeBytes: 1024, ContentHash: "hash-manifest", HashAlgorithm: "sha256", Tracked: 1, Included: 1},
		},
	})
	snapshot, err := st.GetSourceSnapshotByID("snap-required-context-bundle")
	if err != nil {
		t.Fatalf("GetSourceSnapshotByID: %v", err)
	}
	seedContextPacket(t, st, "relay", "plan-required-context-bundle", "PASS-002", "snap-required-context-bundle", snapshot.ID, "ctxpkt-required-context-bundle")

	resp, err := svc.GetNextPassWork(context.Background(), NextPassWorkRequest{ProjectID: "relay", PlanID: "plan-required-context-bundle"})
	if err != nil {
		t.Fatalf("GetNextPassWork: %v", err)
	}
	if !resp.OK || resp.HandoffWork == nil || resp.PlannerJumpstart == nil {
		t.Fatalf("expected ready handoff work, got ok=%t blockers=%+v", resp.OK, resp.Blockers)
	}
	bundle := resp.RequiredContextBundle
	if bundle == nil {
		t.Fatal("expected required_context_bundle")
	}
	if bundle.ManifestRepoID != "relay-contracts" || bundle.ManifestPath != requiredContextManifestPath || bundle.ManifestHash != "hash-manifest" {
		t.Fatalf("unexpected manifest metadata: %+v", bundle)
	}
	if bundle.TaskDomain != "planner_mcp_behavior_update" {
		t.Fatalf("expected planner_mcp_behavior_update task domain, got %q", bundle.TaskDomain)
	}
	if len(bundle.RequiredFiles) != 1 || bundle.RequiredFiles[0].ContentHash != "hash-work-packets" || bundle.RequiredFiles[0].SourceSnapshotID != "snap-required-context-bundle" {
		t.Fatalf("unexpected required files: %+v", bundle.RequiredFiles)
	}
	if len(bundle.OptionalFiles) != 1 || bundle.OptionalFiles[0].ContentHash != "hash-docs-mcp" {
		t.Fatalf("unexpected optional files: %+v", bundle.OptionalFiles)
	}
	if len(bundle.RequiredSearches) != 1 || bundle.RequiredSearches[0].MaxResults != 25 || bundle.RequiredSearches[0].ContextLines != 50 {
		t.Fatalf("unexpected required searches: %+v", bundle.RequiredSearches)
	}
	if bundle.ContextBudget.MaxFiles != 12 || bundle.ContextBudget.MaxBytes != 180000 || bundle.ContextBudget.IncludeInventory {
		t.Fatalf("unexpected context budget: %+v", bundle.ContextBudget)
	}
	if len(bundle.ReadinessCriteria) == 0 || len(bundle.ContextCoverageExpectations) == 0 || len(bundle.BlockedIfMissing) == 0 {
		t.Fatalf("expected readiness/coverage/blocking guidance in bundle: %+v", bundle)
	}
	if len(bundle.Blockers) != 0 {
		t.Fatalf("expected no bundle blockers, got %+v", bundle.Blockers)
	}
	if resp.HandoffWork.RequiredContextBundle != bundle || resp.PlannerJumpstart.RequiredContextBundle != bundle {
		t.Fatalf("expected nested payloads to reuse required_context_bundle")
	}
	summary := CompactNextPassWorkSummary(resp)
	if summary.RequiredContextBundle == nil || summary.RequiredContextBundle.ManifestHash != "hash-manifest" {
		t.Fatalf("expected compact summary bundle hash, got %+v", summary.RequiredContextBundle)
	}
	assertRequiredContextBundleSafeJSON(t, bundle)
}

func TestGetNextPassWork_RequiredContextBundleReportsMissingSnapshotMetadata(t *testing.T) {
	t.Parallel()
	svc, st := newWorkPacketService(t)
	seedAcquisitionPlanWithContext(t, st, "plan-required-context-bundle-missing",
		singleRequiredFileContextPlan("internal/app/plans/work_packets.go"),
		&ContextBudget{MaxFiles: int64Ptr(12), MaxBytes: int64Ptr(180000), MaxSearchResults: int64Ptr(25)})
	seedSnapshotFileMetadataByRepo(t, st, "snap-required-context-bundle-missing", map[string][]store.CreateSourceSnapshotFileParams{
		"relay": {
			{Path: "docs/mcp.md", SizeBytes: 2048, ContentHash: "hash-docs-mcp", HashAlgorithm: "sha256", Tracked: 1, Included: 1},
		},
		"relay-contracts": {
			{Path: "agents/knowledge/planner_knowledge_manifest.json", SizeBytes: 1024, ContentHash: "hash-wrong-manifest", HashAlgorithm: "sha256", Tracked: 1, Included: 1},
		},
	})
	snapshot, err := st.GetSourceSnapshotByID("snap-required-context-bundle-missing")
	if err != nil {
		t.Fatalf("GetSourceSnapshotByID: %v", err)
	}
	seedContextPacket(t, st, "relay", "plan-required-context-bundle-missing", "PASS-002", "snap-required-context-bundle-missing", snapshot.ID, "ctxpkt-required-context-bundle-missing")

	resp, err := svc.GetNextPassWork(context.Background(), NextPassWorkRequest{ProjectID: "relay", PlanID: "plan-required-context-bundle-missing"})
	if err != nil {
		t.Fatalf("GetNextPassWork: %v", err)
	}
	if !resp.OK || resp.HandoffWork == nil {
		t.Fatalf("bundle blockers must not block otherwise ready handoff work: ok=%t blockers=%+v", resp.OK, resp.Blockers)
	}
	bundle := resp.RequiredContextBundle
	if bundle == nil {
		t.Fatal("expected required_context_bundle")
	}
	if bundle.ManifestHash != "" || len(bundle.RequiredFiles) != 1 || bundle.RequiredFiles[0].ContentHash != "" {
		t.Fatalf("expected missing manifest and file hashes, got %+v", bundle)
	}
	if len(bundle.Blockers) < 2 {
		t.Fatalf("expected manifest and required-file bundle blockers, got %+v", bundle.Blockers)
	}
	for _, blocker := range bundle.Blockers {
		if blocker.Code != BlockerRequiredSeedFileMissingFromSnapshot || !blocker.Recoverable {
			t.Fatalf("unexpected bundle blocker: %+v", blocker)
		}
	}
	if len(bundle.NextActions) == 0 {
		t.Fatalf("expected safe next action for bundle blockers")
	}
	assertRequiredContextBundleSafeJSON(t, bundle)
}

func assertRequiredContextBundleSafeJSON(t *testing.T, bundle *RequiredContextBundle) {
	t.Helper()
	data, err := json.Marshal(bundle)
	if err != nil {
		t.Fatalf("marshal required_context_bundle: %v", err)
	}
	text := string(data)
	for _, forbidden := range []string{`"content"`, `"raw"`, `"body"`, `D:/`, `D:\\`, `C:/`, `C:\\`} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("required_context_bundle contains forbidden token %q: %s", forbidden, text)
		}
	}
}
