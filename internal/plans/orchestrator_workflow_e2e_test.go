package plans

import (
	"context"
	"database/sql"
	"fmt"
	"testing"

	"relay/internal/store"
)

// assertPassStatusEquals reloads a pass by ID and asserts its current status.
func assertPassStatusEquals(t *testing.T, st *store.Store, planID, passID, want string) {
	t.Helper()

	plan, err := st.GetPlanByPlanID(planID)
	if err != nil {
		t.Fatalf("GetPlanByPlanID %q: %v", planID, err)
	}
	pass, err := st.GetPlanPassByPassID(plan.ID, passID)
	if err != nil {
		t.Fatalf("GetPlanPassByPassID %q: %v", passID, err)
	}
	if pass.Status != want {
		t.Fatalf("pass %q: expected status %q, got %q", passID, want, pass.Status)
	}
}

// createAuditReadyRunForPass creates a pass-associated run and the audit
// evidence artifacts required for audit-work selection.
func createAuditReadyRunForPass(t *testing.T, st *store.Store, plan *store.Plan, passID, status string) *store.Run {
	t.Helper()

	repo, err := st.CreateRepo("test-repo-"+passID, t.TempDir())
	if err != nil {
		t.Fatalf("CreateRepo: %v", err)
	}
	pass, err := st.GetPlanPassByPassID(plan.ID, passID)
	if err != nil {
		t.Fatalf("GetPlanPassByPassID %q: %v", passID, err)
	}

	run, err := st.CreateRunWithAssociation(
		repo.ID,
		"run-"+passID,
		status,
		"", "", "opencode_go", "main",
		sql.NullInt64{Int64: plan.ID, Valid: true},
		sql.NullInt64{Int64: pass.ID, Valid: true},
	)
	if err != nil {
		t.Fatalf("CreateRunWithAssociation: %v", err)
	}

	// Audit evidence rows (presence-only; no secrets, tokens, or signed URLs).
	if _, err := st.CreateArtifact(run.ID, "audit_packet", "packet.md", "text/markdown"); err != nil {
		t.Fatalf("CreateArtifact audit_packet: %v", err)
	}
	if _, err := st.CreateArtifact(run.ID, "audit_evidence_manifest_json", "manifest.json", "application/json"); err != nil {
		t.Fatalf("CreateArtifact audit_evidence_manifest_json: %v", err)
	}

	return run
}

// TestProjectOrchestratorWorkflow_E2EAcceptedPassAllowsNextPass walks the full
// project-scoped orchestrator loop: select PASS-001, create and progress an
// associated run through intake/prepare/audit-ready, confirm advancement is
// blocked while audit is pending, retrieve audit work, apply an accepted
// decision, and confirm PASS-002 becomes selectable only after PASS-001 is
// completed.
func TestProjectOrchestratorWorkflow_E2EAcceptedPassAllowsNextPass(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	svc, st := newWorkPacketService(t)
	lifecycle := NewRunLifecycleService(st)
	plan := seedPlan(t, st, "relay", "plan-e2e-accepted")

	// Step 1: next-pass work selects PASS-001.
	resp, err := svc.GetNextPassWork(ctx, NextPassWorkRequest{ProjectID: "relay", PlanID: "plan-e2e-accepted"})
	if err != nil {
		t.Fatalf("GetNextPassWork: %v", err)
	}
	if !resp.OK {
		t.Fatalf("expected ok=true, got blockers: %+v", resp.Blockers)
	}
	if resp.SelectedPass == nil || resp.SelectedPass.PassID != "PASS-001" {
		t.Fatalf("expected PASS-001 selected, got %+v", resp.SelectedPass)
	}

	// Step 2: create a pass-associated run for PASS-001 and progress its status.
	repo, err := st.CreateRepo("test-repo", t.TempDir())
	if err != nil {
		t.Fatalf("CreateRepo: %v", err)
	}
	pass1, err := st.GetPlanPassByPassID(plan.ID, "PASS-001")
	if err != nil {
		t.Fatalf("GetPlanPassByPassID PASS-001: %v", err)
	}
	run, err := st.CreateRunWithAssociation(
		repo.ID,
		"run-PASS-001",
		"intake_received",
		"", "", "opencode_go", "main",
		sql.NullInt64{Int64: plan.ID, Valid: true},
		sql.NullInt64{Int64: pass1.ID, Valid: true},
	)
	if err != nil {
		t.Fatalf("CreateRunWithAssociation: %v", err)
	}

	// intake_received -> pass run_created.
	if err := lifecycle.SyncAssociatedPassForRunStatus(run); err != nil {
		t.Fatalf("sync intake_received: %v", err)
	}
	assertPassStatusEquals(t, st, "plan-e2e-accepted", "PASS-001", StatusPassRunCreated)

	// approved_for_prepare -> pass in_progress.
	run, err = st.UpdateRunStatus(run.ID, "approved_for_prepare")
	if err != nil {
		t.Fatalf("UpdateRunStatus approved_for_prepare: %v", err)
	}
	if err := lifecycle.SyncAssociatedPassForRunStatus(run); err != nil {
		t.Fatalf("sync approved_for_prepare: %v", err)
	}
	assertPassStatusEquals(t, st, "plan-e2e-accepted", "PASS-001", StatusPassInProgress)

	// audit_ready -> pass audit_ready.
	run, err = st.UpdateRunStatus(run.ID, "audit_ready")
	if err != nil {
		t.Fatalf("UpdateRunStatus audit_ready: %v", err)
	}
	if err := lifecycle.SyncAssociatedPassForRunStatus(run); err != nil {
		t.Fatalf("sync audit_ready: %v", err)
	}
	assertPassStatusEquals(t, st, "plan-e2e-accepted", "PASS-001", StatusPassAuditReady)

	// Step 3: before the audit decision, next-pass work must block (prior pass
	// awaits audit) and must NOT select PASS-002.
	blockedResp, err := svc.GetNextPassWork(ctx, NextPassWorkRequest{ProjectID: "relay", PlanID: "plan-e2e-accepted"})
	if err != nil {
		t.Fatalf("GetNextPassWork (pre-audit): %v", err)
	}
	assertBlockerCode(t, blockedResp, BlockerPriorPassAwaitsAudit)
	if blockedResp.SelectedPass != nil {
		t.Fatalf("expected no selected pass while PASS-001 awaits audit, got %+v", blockedResp.SelectedPass)
	}

	// Step 4: create required audit artifacts and retrieve audit work.
	if _, err := st.CreateArtifact(run.ID, "audit_packet", "packet.md", "text/markdown"); err != nil {
		t.Fatalf("CreateArtifact audit_packet: %v", err)
	}
	if _, err := st.CreateArtifact(run.ID, "audit_evidence_manifest_json", "manifest.json", "application/json"); err != nil {
		t.Fatalf("CreateArtifact audit_evidence_manifest_json: %v", err)
	}

	auditResp, err := svc.GetNextAuditWork(ctx, NextAuditWorkRequest{ProjectID: "relay", PlanID: "plan-e2e-accepted"})
	if err != nil {
		t.Fatalf("GetNextAuditWork: %v", err)
	}
	if !auditResp.OK {
		t.Fatalf("expected audit ok=true, got blockers: %+v", auditResp.Blockers)
	}
	if auditResp.SelectedPass == nil || auditResp.SelectedPass.PassID != "PASS-001" {
		t.Fatalf("expected audit work for PASS-001, got %+v", auditResp.SelectedPass)
	}
	if auditResp.SelectedRun == nil || auditResp.SelectedRun.RunID != fmt.Sprintf("%d", run.ID) {
		t.Fatalf("expected audit work selected run %d, got %+v", run.ID, auditResp.SelectedRun)
	}
	if len(auditResp.AllowedDecisions) == 0 {
		t.Fatal("expected allowed audit decisions in response")
	}

	// Step 5: apply an accepted decision; PASS-001 becomes completed.
	run, err = st.UpdateRunStatus(run.ID, "accepted")
	if err != nil {
		t.Fatalf("UpdateRunStatus accepted: %v", err)
	}
	if err := lifecycle.ApplyAuditDecision(run, "accepted"); err != nil {
		t.Fatalf("ApplyAuditDecision accepted: %v", err)
	}
	assertPassStatusEquals(t, st, "plan-e2e-accepted", "PASS-001", StatusPassCompleted)

	// Step 6: next-pass work now selects PASS-002 with PASS-001 satisfied.
	nextResp, err := svc.GetNextPassWork(ctx, NextPassWorkRequest{ProjectID: "relay", PlanID: "plan-e2e-accepted"})
	if err != nil {
		t.Fatalf("GetNextPassWork (post-audit): %v", err)
	}
	if !nextResp.OK {
		t.Fatalf("expected ok=true selecting PASS-002, got blockers: %+v", nextResp.Blockers)
	}
	if nextResp.SelectedPass == nil || nextResp.SelectedPass.PassID != "PASS-002" {
		t.Fatalf("expected PASS-002 selected, got %+v", nextResp.SelectedPass)
	}
	var sawSatisfiedDep bool
	for _, ds := range nextResp.DependencyStatus {
		if ds.PassID == "PASS-001" {
			if !ds.Satisfied {
				t.Fatalf("expected PASS-001 dependency satisfied=true, got false")
			}
			sawSatisfiedDep = true
		}
	}
	if !sawSatisfiedDep {
		t.Fatal("expected PASS-001 to appear satisfied in dependency_status")
	}
}

// TestProjectOrchestratorWorkflow_RevisionRequiredDoesNotAdvance proves that a
// revision_required pass blocks advancement and never selects the next pass.
func TestProjectOrchestratorWorkflow_RevisionRequiredDoesNotAdvance(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	svc, st := newWorkPacketService(t)
	lifecycle := NewRunLifecycleService(st)
	plan := seedPlan(t, st, "relay", "plan-e2e-revision")

	// Create an audit-ready run for PASS-001 with required audit evidence.
	run := createAuditReadyRunForPass(t, st, plan, "PASS-001", "audit_ready")

	// Sync the pass to audit_ready, then apply a revision_required decision.
	if err := lifecycle.SyncAssociatedPassForRunStatus(run); err != nil {
		t.Fatalf("sync audit_ready: %v", err)
	}
	assertPassStatusEquals(t, st, "plan-e2e-revision", "PASS-001", StatusPassAuditReady)

	if err := lifecycle.ApplyAuditDecision(run, "revision_required"); err != nil {
		t.Fatalf("ApplyAuditDecision revision_required: %v", err)
	}
	assertPassStatusEquals(t, st, "plan-e2e-revision", "PASS-001", StatusPassRevisionRequired)

	// Next-pass work must block on the same pass and must not select PASS-002.
	resp, err := svc.GetNextPassWork(ctx, NextPassWorkRequest{ProjectID: "relay", PlanID: "plan-e2e-revision"})
	if err != nil {
		t.Fatalf("GetNextPassWork: %v", err)
	}
	assertBlockerCode(t, resp, BlockerRevisionRequiredSamePass)
	if resp.SelectedPass != nil {
		t.Fatalf("expected no selected pass while PASS-001 requires revision, got %+v", resp.SelectedPass)
	}
}

// TestProjectOrchestratorWorkflow_BlockedPassDoesNotAdvance proves that a
// blocked pass prevents advancement and never selects the next pass.
func TestProjectOrchestratorWorkflow_BlockedPassDoesNotAdvance(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	svc, st := newWorkPacketService(t)
	seedPlan(t, st, "relay", "plan-e2e-blocked")

	setPassStatus(t, st, "plan-e2e-blocked", "PASS-001", StatusPassBlocked)

	resp, err := svc.GetNextPassWork(ctx, NextPassWorkRequest{ProjectID: "relay", PlanID: "plan-e2e-blocked"})
	if err != nil {
		t.Fatalf("GetNextPassWork: %v", err)
	}
	if resp.OK {
		t.Fatalf("expected ok=false when PASS-001 is blocked, got ok=true")
	}
	assertBlockerCode(t, resp, BlockerNoEligiblePass)
	if resp.SelectedPass != nil {
		t.Fatalf("expected no selected pass when PASS-001 is blocked, got %+v", resp.SelectedPass)
	}
}
