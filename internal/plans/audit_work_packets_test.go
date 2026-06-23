package plans

import (
	"context"
	"database/sql"
	"strconv"
	"testing"
)

// assertAuditBlockerCode checks that the response has ok=false and the first blocker
// matches the expected code.
func assertAuditBlockerCode(t *testing.T, resp NextAuditWorkResponse, expected string) {
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

func TestGetNextAuditWork_UnknownProject(t *testing.T) {
	t.Parallel()

	svc, _ := newWorkPacketService(t)
	resp, err := svc.GetNextAuditWork(context.Background(), NextAuditWorkRequest{
		ProjectID: "no-such-project",
		PlanID:    "plan-x",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertAuditBlockerCode(t, resp, BlockerUnknownProject)
}

func TestGetNextAuditWork_UnknownPlan(t *testing.T) {
	t.Parallel()

	svc, _ := newWorkPacketService(t)
	resp, err := svc.GetNextAuditWork(context.Background(), NextAuditWorkRequest{
		ProjectID: "relay",
		PlanID:    "no-such-plan",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertAuditBlockerCode(t, resp, BlockerUnknownPlan)
}

func TestGetNextAuditWork_ProjectPlanMismatch(t *testing.T) {
	t.Parallel()

	svc, st := newWorkPacketService(t)

	if _, err := st.CreateProject("other-project", "Other", "", "active", ""); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	seedPlan(t, st, "relay", "plan-mismatch")

	resp, err := svc.GetNextAuditWork(context.Background(), NextAuditWorkRequest{
		ProjectID: "other-project",
		PlanID:    "plan-mismatch",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertAuditBlockerCode(t, resp, BlockerProjectPlanMismatch)
}

func TestGetNextAuditWork_UnknownPass(t *testing.T) {
	t.Parallel()

	svc, st := newWorkPacketService(t)
	seedPlan(t, st, "relay", "plan-passoverride")

	resp, err := svc.GetNextAuditWork(context.Background(), NextAuditWorkRequest{
		ProjectID: "relay",
		PlanID:    "plan-passoverride",
		PassID:    "NO-SUCH-PASS",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertAuditBlockerCode(t, resp, BlockerUnknownPass)
}

func TestGetNextAuditWork_UnknownRun(t *testing.T) {
	t.Parallel()

	svc, st := newWorkPacketService(t)
	seedPlan(t, st, "relay", "plan-runoverride")

	resp, err := svc.GetNextAuditWork(context.Background(), NextAuditWorkRequest{
		ProjectID: "relay",
		PlanID:    "plan-runoverride",
		RunID:     "99999",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertAuditBlockerCode(t, resp, BlockerUnknownRun)
}

func TestGetNextAuditWork_RunNotInProjectPlan(t *testing.T) {
	t.Parallel()

	svc, st := newWorkPacketService(t)
	seedPlan(t, st, "relay", "plan-a")
	seedPlan(t, st, "relay", "plan-b")

	repo, err := st.CreateRepo("test-repo", t.TempDir())
	if err != nil {
		t.Fatalf("CreateRepo: %v", err)
	}

	planB, err := st.GetPlanByPlanID("plan-b")
	if err != nil {
		t.Fatalf("GetPlanB: %v", err)
	}
	passB1, err := st.GetPlanPassByPassID(planB.ID, "PASS-001")
	if err != nil {
		t.Fatalf("GetPassB1: %v", err)
	}

	// Create run for plan B.
	runB, err := st.CreateRunWithAssociation(
		repo.ID,
		"run b",
		"audit_ready",
		"", "", "", "main",
		sql.NullInt64{Int64: planB.ID, Valid: true},
		sql.NullInt64{Int64: passB1.ID, Valid: true},
	)
	if err != nil {
		t.Fatalf("CreateRun: %v", err)
	}

	// Request next audit work for plan A with run B's ID override.
	resp, err := svc.GetNextAuditWork(context.Background(), NextAuditWorkRequest{
		ProjectID: "relay",
		PlanID:    "plan-a",
		RunID:     strconv.FormatInt(runB.ID, 10),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertAuditBlockerCode(t, resp, BlockerRunNotInProjectPlan)
}

func TestGetNextAuditWork_RunNotAuditReady(t *testing.T) {
	t.Parallel()

	svc, st := newWorkPacketService(t)
	plan := seedPlan(t, st, "relay", "plan-runready")
	pass, err := st.GetPlanPassByPassID(plan.ID, "PASS-001")
	if err != nil {
		t.Fatalf("GetPass: %v", err)
	}

	repo, err := st.CreateRepo("test-repo", t.TempDir())
	if err != nil {
		t.Fatalf("CreateRepo: %v", err)
	}

	// Create run that is NOT in audit_ready state.
	run, err := st.CreateRunWithAssociation(
		repo.ID,
		"run in progress",
		"in_progress",
		"", "", "", "main",
		sql.NullInt64{Int64: plan.ID, Valid: true},
		sql.NullInt64{Int64: pass.ID, Valid: true},
	)
	if err != nil {
		t.Fatalf("CreateRun: %v", err)
	}

	resp, err := svc.GetNextAuditWork(context.Background(), NextAuditWorkRequest{
		ProjectID: "relay",
		PlanID:    "plan-runready",
		RunID:     strconv.FormatInt(run.ID, 10),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertAuditBlockerCode(t, resp, BlockerRunNotAuditReady)
}

func TestGetNextAuditWork_AuditEvidenceMissing(t *testing.T) {
	t.Parallel()

	svc, st := newWorkPacketService(t)
	plan := seedPlan(t, st, "relay", "plan-evidence")
	pass, err := st.GetPlanPassByPassID(plan.ID, "PASS-001")
	if err != nil {
		t.Fatalf("GetPass: %v", err)
	}

	repo, err := st.CreateRepo("test-repo", t.TempDir())
	if err != nil {
		t.Fatalf("CreateRepo: %v", err)
	}

	run, err := st.CreateRunWithAssociation(
		repo.ID,
		"run audit ready",
		"audit_ready",
		"", "", "", "main",
		sql.NullInt64{Int64: plan.ID, Valid: true},
		sql.NullInt64{Int64: pass.ID, Valid: true},
	)
	if err != nil {
		t.Fatalf("CreateRun: %v", err)
	}

	// Request audit work - should block on evidence missing since no artifacts are created.
	resp, err := svc.GetNextAuditWork(context.Background(), NextAuditWorkRequest{
		ProjectID: "relay",
		PlanID:    "plan-evidence",
		RunID:     strconv.FormatInt(run.ID, 10),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertAuditBlockerCode(t, resp, BlockerAuditEvidenceMissing)
}

func TestGetNextAuditWork_AuditAlreadyFinalized(t *testing.T) {
	t.Parallel()

	svc, st := newWorkPacketService(t)
	plan := seedPlan(t, st, "relay", "plan-finalized")
	pass, err := st.GetPlanPassByPassID(plan.ID, "PASS-001")
	if err != nil {
		t.Fatalf("GetPass: %v", err)
	}

	repo, err := st.CreateRepo("test-repo", t.TempDir())
	if err != nil {
		t.Fatalf("CreateRepo: %v", err)
	}

	// 1. Terminal status check.
	run, err := st.CreateRunWithAssociation(
		repo.ID,
		"run accepted",
		"accepted",
		"", "", "", "main",
		sql.NullInt64{Int64: plan.ID, Valid: true},
		sql.NullInt64{Int64: pass.ID, Valid: true},
	)
	if err != nil {
		t.Fatalf("CreateRun: %v", err)
	}

	resp, err := svc.GetNextAuditWork(context.Background(), NextAuditWorkRequest{
		ProjectID: "relay",
		PlanID:    "plan-finalized",
		RunID:     strconv.FormatInt(run.ID, 10),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertAuditBlockerCode(t, resp, BlockerAuditAlreadyFinalized)

	// 2. Artifact kind audit_decision_json check.
	run2, err := st.CreateRunWithAssociation(
		repo.ID,
		"run audit ready with decision",
		"audit_ready",
		"", "", "", "main",
		sql.NullInt64{Int64: plan.ID, Valid: true},
		sql.NullInt64{Int64: pass.ID, Valid: true},
	)
	if err != nil {
		t.Fatalf("CreateRun2: %v", err)
	}

	if _, err := st.CreateArtifact(run2.ID, "audit_decision_json", "dec.json", "application/json"); err != nil {
		t.Fatalf("CreateArtifact: %v", err)
	}

	resp, err = svc.GetNextAuditWork(context.Background(), NextAuditWorkRequest{
		ProjectID: "relay",
		PlanID:    "plan-finalized",
		RunID:     strconv.FormatInt(run2.ID, 10),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertAuditBlockerCode(t, resp, BlockerAuditAlreadyFinalized)
}

func TestGetNextAuditWork_RevisionRequiredBlocksAdvancement(t *testing.T) {
	t.Parallel()

	svc, st := newWorkPacketService(t)
	plan := seedPlan(t, st, "relay", "plan-revblock")
	_, err := st.GetPlanPassByPassID(plan.ID, "PASS-001")
	if err != nil {
		t.Fatalf("GetPass1: %v", err)
	}
	pass2, err := st.GetPlanPassByPassID(plan.ID, "PASS-002")
	if err != nil {
		t.Fatalf("GetPass2: %v", err)
	}

	repo, err := st.CreateRepo("test-repo", t.TempDir())
	if err != nil {
		t.Fatalf("CreateRepo: %v", err)
	}

	// Make pass 2 audit-ready.
	run2, err := st.CreateRunWithAssociation(
		repo.ID,
		"run 2",
		"audit_ready",
		"", "", "", "main",
		sql.NullInt64{Int64: plan.ID, Valid: true},
		sql.NullInt64{Int64: pass2.ID, Valid: true},
	)
	if err != nil {
		t.Fatalf("CreateRun2: %v", err)
	}
	if _, err := st.CreateArtifact(run2.ID, "audit_packet", "packet.md", "text/markdown"); err != nil {
		t.Fatalf("CreateArtifact: %v", err)
	}
	if _, err := st.CreateArtifact(run2.ID, "audit_evidence_manifest_json", "manifest.json", "application/json"); err != nil {
		t.Fatalf("CreateArtifact: %v", err)
	}

	// Set pass 1 to revision_required.
	setPassStatus(t, st, "plan-revblock", "PASS-001", StatusPassRevisionRequired)

	resp, err := svc.GetNextAuditWork(context.Background(), NextAuditWorkRequest{
		ProjectID: "relay",
		PlanID:    "plan-revblock",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Selection walker should block on PASS-001 revision_required and not select PASS-002's run.
	assertAuditBlockerCode(t, resp, BlockerRevisionRequiredSamePass)
}

func TestGetNextAuditWork_ExplicitPassIDSelectsLatestRun(t *testing.T) {
	t.Parallel()

	svc, st := newWorkPacketService(t)
	plan := seedPlan(t, st, "relay", "plan-passid-latest")
	pass, err := st.GetPlanPassByPassID(plan.ID, "PASS-001")
	if err != nil {
		t.Fatalf("GetPass: %v", err)
	}

	repo, err := st.CreateRepo("test-repo", t.TempDir())
	if err != nil {
		t.Fatalf("CreateRepo: %v", err)
	}

	// Create run 1.
	_, err = st.CreateRunWithAssociation(
		repo.ID,
		"run 1",
		"audit_ready",
		"", "", "", "main",
		sql.NullInt64{Int64: plan.ID, Valid: true},
		sql.NullInt64{Int64: pass.ID, Valid: true},
	)
	if err != nil {
		t.Fatalf("CreateRun1: %v", err)
	}

	// Create run 2 (latest).
	run2, err := st.CreateRunWithAssociation(
		repo.ID,
		"run 2",
		"audit_ready",
		"", "", "", "main",
		sql.NullInt64{Int64: plan.ID, Valid: true},
		sql.NullInt64{Int64: pass.ID, Valid: true},
	)
	if err != nil {
		t.Fatalf("CreateRun2: %v", err)
	}
	if _, err := st.CreateArtifact(run2.ID, "audit_packet", "packet.md", "text/markdown"); err != nil {
		t.Fatalf("CreateArtifact: %v", err)
	}
	if _, err := st.CreateArtifact(run2.ID, "audit_evidence_manifest_json", "manifest.json", "application/json"); err != nil {
		t.Fatalf("CreateArtifact: %v", err)
	}

	resp, err := svc.GetNextAuditWork(context.Background(), NextAuditWorkRequest{
		ProjectID: "relay",
		PlanID:    "plan-passid-latest",
		PassID:    "PASS-001",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.OK {
		t.Fatalf("expected ok=true, got blockers: %+v", resp.Blockers)
	}
	if resp.SelectedRun.RunID != strconv.FormatInt(run2.ID, 10) {
		t.Fatalf("expected run2 (%d), got run ID %s", run2.ID, resp.SelectedRun.RunID)
	}
}

func TestGetNextAuditWork_AutomaticEarliestSequenceSelection_Success(t *testing.T) {
	t.Parallel()

	svc, st := newWorkPacketService(t)
	plan := seedPlan(t, st, "relay", "plan-autoselect")
	pass1, err := st.GetPlanPassByPassID(plan.ID, "PASS-001")
	if err != nil {
		t.Fatalf("GetPass1: %v", err)
	}
	pass2, err := st.GetPlanPassByPassID(plan.ID, "PASS-002")
	if err != nil {
		t.Fatalf("GetPass2: %v", err)
	}

	repo, err := st.CreateRepo("test-repo", t.TempDir())
	if err != nil {
		t.Fatalf("CreateRepo: %v", err)
	}

	// Set pass 1 status to audit_ready.
	setPassStatus(t, st, "plan-autoselect", "PASS-001", StatusPassAuditReady)
	// Set pass 2 status to audit_ready.
	setPassStatus(t, st, "plan-autoselect", "PASS-002", StatusPassAuditReady)

	// Create run 1.
	run1, err := st.CreateRunWithAssociation(
		repo.ID,
		"run 1",
		"audit_ready",
		"", "", "", "main",
		sql.NullInt64{Int64: plan.ID, Valid: true},
		sql.NullInt64{Int64: pass1.ID, Valid: true},
	)
	if err != nil {
		t.Fatalf("CreateRun1: %v", err)
	}
	if _, err := st.CreateArtifact(run1.ID, "audit_packet", "packet.md", "text/markdown"); err != nil {
		t.Fatalf("CreateArtifact: %v", err)
	}
	if _, err := st.CreateArtifact(run1.ID, "audit_evidence_manifest_json", "manifest.json", "application/json"); err != nil {
		t.Fatalf("CreateArtifact: %v", err)
	}

	// Create run 2.
	run2, err := st.CreateRunWithAssociation(
		repo.ID,
		"run 2",
		"audit_ready",
		"", "", "", "main",
		sql.NullInt64{Int64: plan.ID, Valid: true},
		sql.NullInt64{Int64: pass2.ID, Valid: true},
	)
	if err != nil {
		t.Fatalf("CreateRun2: %v", err)
	}
	if _, err := st.CreateArtifact(run2.ID, "audit_packet", "packet.md", "text/markdown"); err != nil {
		t.Fatalf("CreateArtifact: %v", err)
	}
	if _, err := st.CreateArtifact(run2.ID, "audit_evidence_manifest_json", "manifest.json", "application/json"); err != nil {
		t.Fatalf("CreateArtifact: %v", err)
	}

	resp, err := svc.GetNextAuditWork(context.Background(), NextAuditWorkRequest{
		ProjectID: "relay",
		PlanID:    "plan-autoselect",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.OK {
		t.Fatalf("expected ok=true, got blockers: %+v", resp.Blockers)
	}
	// Should select PASS-001 (earliest sequence) instead of PASS-002.
	if resp.SelectedPass.PassID != "PASS-001" {
		t.Fatalf("expected PASS-001, got %s", resp.SelectedPass.PassID)
	}
	if resp.SelectedRun.RunID != strconv.FormatInt(run1.ID, 10) {
		t.Fatalf("expected run1 (%d), got run ID %s", run1.ID, resp.SelectedRun.RunID)
	}

	// Check payload guidance
	if resp.SubmitDecisionPayloadGuidance == nil {
		t.Fatal("expected SubmitDecisionPayloadGuidance in response")
	}
	if resp.SubmitDecisionPayloadGuidance.PrimaryRoute.Method != "POST" {
		t.Errorf("expected primary route method POST, got %s", resp.SubmitDecisionPayloadGuidance.PrimaryRoute.Method)
	}
	// Verify PriorPassContext -- for PASS-001 (sequence 1), there are no prior passes.
	if len(resp.PriorPassContext.PriorPasses) != 0 {
		t.Errorf("expected 0 prior passes for PASS-001, got %d", len(resp.PriorPassContext.PriorPasses))
	}
}
