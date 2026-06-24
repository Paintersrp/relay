package plans

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"

	"relay/internal/store"
)

// seedReadyRefactorCandidate creates a pass-ready refactor candidate (status
// "ready") in the given project using the PASS-002/PASS-003 store wrappers.
func seedReadyRefactorCandidate(t *testing.T, st *store.Store, project *store.Project, candidateID string) *store.RefactorCandidate {
	t.Helper()

	cand, err := st.CreateRefactorCandidate(store.CreateRefactorCandidateParams{
		CandidateID:            candidateID,
		ProjectRowID:           project.ID,
		ProjectID:              project.ProjectID,
		Title:                  "Consolidate parsing",
		ProblemSummary:         "Duplicate parsing branch causes drift.",
		CurrentBehavior:        "Two parsing paths exist.",
		DesiredBehavior:        "Single parsing path shared across callers.",
		Rationale:              "Reduce maintenance burden.",
		ProposedPassName:       "Consolidate parsing",
		ProposedPassGoal:       "Remove the duplicate parsing branch.",
		ProposedPassScopeJSON:  `["Replace duplicate parsing branch in internal/foo/bar.go"]`,
		ProposedNonGoalsJSON:   `["Do not change public API behavior"]`,
		TargetFilesJSON:        `["internal/foo/bar.go"]`,
		ValidationCommandsJSON: `["go test ./internal/foo/..."]`,
		AuditFocusJSON:         `["Verify behavior remains unchanged"]`,
		ConstraintsJSON:        `[]`,
		RiskLevel:              "medium",
		Status:                 "ready",
		MetadataJSON:           `{}`,
	})
	if err != nil {
		t.Fatalf("CreateRefactorCandidate: %v", err)
	}
	return cand
}

// scheduleRefactorCandidate flips a ready candidate to scheduled and persists an
// active schedule reference pointing at the given plan/pass.
func scheduleRefactorCandidate(t *testing.T, st *store.Store, project *store.Project, candidate *store.RefactorCandidate, planID, passID string) {
	t.Helper()

	if _, err := st.UpdateRefactorCandidateStatusMetadata(store.UpdateRefactorCandidateStatusMetadataParams{
		ProjectRowID: project.ID,
		CandidateID:  candidate.CandidateID,
		Status:       "scheduled",
		ScheduledAt:  "2026-06-24T00:00:00Z",
	}); err != nil {
		t.Fatalf("UpdateRefactorCandidateStatusMetadata(scheduled): %v", err)
	}

	if _, err := st.CreateRefactorCandidateScheduleRef(store.CreateRefactorCandidateScheduleRefParams{
		ScheduleRefID:  "rsched-" + candidate.CandidateID,
		ProjectRowID:   project.ID,
		ProjectID:      project.ProjectID,
		CandidateRowID: candidate.ID,
		ScheduleKind:   "existing_plan_bonus_pass",
		PlanID:         planID,
		PassID:         passID,
	}); err != nil {
		t.Fatalf("CreateRefactorCandidateScheduleRef: %v", err)
	}
}

// makePassRefactor rewrites a persisted pass so it is a scheduled refactor pass:
// pass_type "refactor" plus refactor_candidate metadata in raw_pass_json.
func makePassRefactor(t *testing.T, st *store.Store, planID, passID, candidateID string) {
	t.Helper()

	plan, err := st.GetPlanByPlanID(planID)
	if err != nil {
		t.Fatalf("GetPlanByPlanID %q: %v", planID, err)
	}
	pass, err := st.GetPlanPassByPassID(plan.ID, passID)
	if err != nil {
		t.Fatalf("GetPlanPassByPassID %q: %v", passID, err)
	}

	raw := PlanPassInput{
		PassID:   passID,
		PassType: "refactor",
		RefactorCandidate: &RefactorCandidateMetadata{
			CandidateID:    candidateID,
			Source:         "refactor_backlog_candidate",
			SchedulingMode: "existing_plan_bonus_pass",
		},
	}
	b, err := json.Marshal(raw)
	if err != nil {
		t.Fatalf("marshal refactor raw pass: %v", err)
	}
	if _, err := st.DB().Exec(`UPDATE plan_passes SET pass_type = 'refactor', raw_pass_json = ? WHERE id = ?`, string(b), pass.ID); err != nil {
		t.Fatalf("update pass to refactor: %v", err)
	}
}

func mustProjectRow(t *testing.T, st *store.Store, projectID string) *store.Project {
	t.Helper()
	p, err := st.GetProjectByProjectID(projectID)
	if err != nil {
		t.Fatalf("GetProjectByProjectID %q: %v", projectID, err)
	}
	return p
}

// -------------------------------------------------------------------
// Next-pass work: scheduled refactor passes
// -------------------------------------------------------------------

func TestGetNextPassWork_RefactorPassWithValidScheduleRef(t *testing.T) {
	t.Parallel()

	svc, st := newWorkPacketService(t)
	seedPlan(t, st, "relay", "plan-refactor-valid")
	project := mustProjectRow(t, st, "relay")

	cand := seedReadyRefactorCandidate(t, st, project, "cand-valid")
	scheduleRefactorCandidate(t, st, project, cand, "plan-refactor-valid", "PASS-001")
	makePassRefactor(t, st, "plan-refactor-valid", "PASS-001", "cand-valid")

	resp, err := svc.GetNextPassWork(context.Background(), NextPassWorkRequest{
		ProjectID: "relay",
		PlanID:    "plan-refactor-valid",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.OK {
		t.Fatalf("expected ok=true, got blockers: %+v", resp.Blockers)
	}
	if resp.SelectedPass == nil || resp.SelectedPass.PassID != "PASS-001" {
		t.Fatalf("expected PASS-001 selected, got %+v", resp.SelectedPass)
	}
	rc := resp.SelectedPass.RefactorCandidate
	if rc == nil {
		t.Fatal("expected refactor_candidate metadata on selected pass")
	}
	if rc.CandidateID != "cand-valid" {
		t.Fatalf("expected candidate_id cand-valid, got %q", rc.CandidateID)
	}
	if rc.Source != "refactor_backlog_candidate" {
		t.Fatalf("expected source refactor_backlog_candidate, got %q", rc.Source)
	}
	if rc.SchedulingMode != "existing_plan_bonus_pass" {
		t.Fatalf("expected scheduling_mode existing_plan_bonus_pass, got %q", rc.SchedulingMode)
	}
	if rc.CandidateStatus != "scheduled" {
		t.Fatalf("expected candidate_status scheduled, got %q", rc.CandidateStatus)
	}
	if rc.ScheduleRefStatus != "scheduled" {
		t.Fatalf("expected schedule_ref_status scheduled, got %q", rc.ScheduleRefStatus)
	}
}

func TestGetNextPassWork_NonRefactorPassOmitsMetadata(t *testing.T) {
	t.Parallel()

	svc, st := newWorkPacketService(t)
	seedPlan(t, st, "relay", "plan-refactor-omit")

	resp, err := svc.GetNextPassWork(context.Background(), NextPassWorkRequest{
		ProjectID: "relay",
		PlanID:    "plan-refactor-omit",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.OK {
		t.Fatalf("expected ok=true, got blockers: %+v", resp.Blockers)
	}
	if resp.SelectedPass == nil {
		t.Fatal("expected selected pass")
	}
	if resp.SelectedPass.RefactorCandidate != nil {
		t.Fatalf("expected no refactor_candidate metadata for normal pass, got %+v", resp.SelectedPass.RefactorCandidate)
	}
}

func TestGetNextPassWork_RefactorPassMissingScheduleRefBlocks(t *testing.T) {
	t.Parallel()

	svc, st := newWorkPacketService(t)
	seedPlan(t, st, "relay", "plan-refactor-missing")
	project := mustProjectRow(t, st, "relay")

	cand := seedReadyRefactorCandidate(t, st, project, "cand-missing")
	// Flip to scheduled but do NOT create an active schedule reference.
	if _, err := st.UpdateRefactorCandidateStatusMetadata(store.UpdateRefactorCandidateStatusMetadataParams{
		ProjectRowID: project.ID,
		CandidateID:  cand.CandidateID,
		Status:       "scheduled",
		ScheduledAt:  "2026-06-24T00:00:00Z",
	}); err != nil {
		t.Fatalf("UpdateRefactorCandidateStatusMetadata: %v", err)
	}
	makePassRefactor(t, st, "plan-refactor-missing", "PASS-001", "cand-missing")

	resp, err := svc.GetNextPassWork(context.Background(), NextPassWorkRequest{
		ProjectID: "relay",
		PlanID:    "plan-refactor-missing",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertBlockerCode(t, resp, BlockerUnsafeRequest)
	assertStaleRefactorMessage(t, resp.Blockers[0].Message)
}

func TestGetNextPassWork_RefactorPassMismatchedScheduleRefBlocks(t *testing.T) {
	t.Parallel()

	svc, st := newWorkPacketService(t)
	seedPlan(t, st, "relay", "plan-refactor-mismatch")
	project := mustProjectRow(t, st, "relay")

	cand := seedReadyRefactorCandidate(t, st, project, "cand-mismatch")
	// Schedule ref points at a different pass than the selected one.
	scheduleRefactorCandidate(t, st, project, cand, "plan-refactor-mismatch", "PASS-999")
	makePassRefactor(t, st, "plan-refactor-mismatch", "PASS-001", "cand-mismatch")

	resp, err := svc.GetNextPassWork(context.Background(), NextPassWorkRequest{
		ProjectID: "relay",
		PlanID:    "plan-refactor-mismatch",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertBlockerCode(t, resp, BlockerUnsafeRequest)
	assertStaleRefactorMessage(t, resp.Blockers[0].Message)
}

func TestGetNextPassWork_RefactorPassMalformedMetadataBlocks(t *testing.T) {
	t.Parallel()

	svc, st := newWorkPacketService(t)
	seedPlan(t, st, "relay", "plan-refactor-malformed")

	plan, err := st.GetPlanByPlanID("plan-refactor-malformed")
	if err != nil {
		t.Fatalf("GetPlanByPlanID: %v", err)
	}
	pass, err := st.GetPlanPassByPassID(plan.ID, "PASS-001")
	if err != nil {
		t.Fatalf("GetPlanPassByPassID: %v", err)
	}
	// A refactor pass with malformed raw_pass_json must fail closed.
	if _, err := st.DB().Exec(`UPDATE plan_passes SET pass_type = 'refactor', raw_pass_json = '{not-json' WHERE id = ?`, pass.ID); err != nil {
		t.Fatalf("corrupt raw pass: %v", err)
	}

	resp, err := svc.GetNextPassWork(context.Background(), NextPassWorkRequest{
		ProjectID: "relay",
		PlanID:    "plan-refactor-malformed",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertBlockerCode(t, resp, BlockerUnsafeRequest)
}

func TestGetNextPassWork_RefactorRetrievalDoesNotMutate(t *testing.T) {
	t.Parallel()

	svc, st := newWorkPacketService(t)
	seedPlan(t, st, "relay", "plan-refactor-readonly")
	project := mustProjectRow(t, st, "relay")

	cand := seedReadyRefactorCandidate(t, st, project, "cand-readonly")
	scheduleRefactorCandidate(t, st, project, cand, "plan-refactor-readonly", "PASS-001")
	makePassRefactor(t, st, "plan-refactor-readonly", "PASS-001", "cand-readonly")

	eventsBefore := countRows(t, st.DB(), "refactor_candidate_status_events")

	if _, err := svc.GetNextPassWork(context.Background(), NextPassWorkRequest{
		ProjectID: "relay",
		PlanID:    "plan-refactor-readonly",
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	after, err := st.GetRefactorCandidateByCandidateID(project.ID, "cand-readonly")
	if err != nil {
		t.Fatalf("reload candidate: %v", err)
	}
	if after.Status != "scheduled" {
		t.Fatalf("retrieval mutated candidate status to %q", after.Status)
	}
	eventsAfter := countRows(t, st.DB(), "refactor_candidate_status_events")
	if eventsAfter != eventsBefore {
		t.Fatalf("retrieval recorded %d status event(s); expected 0", eventsAfter-eventsBefore)
	}
}

// -------------------------------------------------------------------
// Next-audit work: scheduled refactor passes
// -------------------------------------------------------------------

// seedAuditReadyRefactorRun creates an audit-ready run with the evidence
// artifacts required by GetNextAuditWork, associated with a refactor pass.
func seedAuditReadyRefactorRun(t *testing.T, st *store.Store, planID, passID string) *store.Run {
	t.Helper()

	repo, err := st.CreateRepo("audit-repo-"+planID, t.TempDir())
	if err != nil {
		t.Fatalf("CreateRepo: %v", err)
	}
	plan, err := st.GetPlanByPlanID(planID)
	if err != nil {
		t.Fatalf("GetPlanByPlanID: %v", err)
	}
	pass, err := st.GetPlanPassByPassID(plan.ID, passID)
	if err != nil {
		t.Fatalf("GetPlanPassByPassID: %v", err)
	}

	run, err := st.CreateRunWithAssociation(
		repo.ID,
		"refactor audit run",
		"audit_ready",
		"", "", "opencode_go", "main",
		sql.NullInt64{Int64: plan.ID, Valid: true},
		sql.NullInt64{Int64: pass.ID, Valid: true},
	)
	if err != nil {
		t.Fatalf("CreateRunWithAssociation: %v", err)
	}

	if _, err := st.CreateArtifact(run.ID, "audit_packet", "data/artifacts/audit_packet.md", "text/markdown"); err != nil {
		t.Fatalf("CreateArtifact audit_packet: %v", err)
	}
	if _, err := st.CreateArtifact(run.ID, "audit_evidence_manifest_json", "data/artifacts/audit_evidence_manifest.json", "application/json"); err != nil {
		t.Fatalf("CreateArtifact audit_evidence_manifest_json: %v", err)
	}

	return run
}

func TestGetNextAuditWork_RefactorPassWithValidScheduleRef(t *testing.T) {
	t.Parallel()

	svc, st := newWorkPacketService(t)
	seedPlan(t, st, "relay", "plan-audit-refactor")
	project := mustProjectRow(t, st, "relay")

	cand := seedReadyRefactorCandidate(t, st, project, "cand-audit")
	scheduleRefactorCandidate(t, st, project, cand, "plan-audit-refactor", "PASS-001")
	makePassRefactor(t, st, "plan-audit-refactor", "PASS-001", "cand-audit")
	setPassStatus(t, st, "plan-audit-refactor", "PASS-001", StatusPassAuditReady)
	seedAuditReadyRefactorRun(t, st, "plan-audit-refactor", "PASS-001")

	resp, err := svc.GetNextAuditWork(context.Background(), NextAuditWorkRequest{
		ProjectID: "relay",
		PlanID:    "plan-audit-refactor",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.OK {
		t.Fatalf("expected ok=true, got blockers: %+v", resp.Blockers)
	}
	if resp.SelectedPass == nil || resp.SelectedPass.RefactorCandidate == nil {
		t.Fatalf("expected refactor_candidate metadata on audit selected pass, got %+v", resp.SelectedPass)
	}
	if resp.SelectedPass.RefactorCandidate.CandidateID != "cand-audit" {
		t.Fatalf("expected candidate_id cand-audit, got %q", resp.SelectedPass.RefactorCandidate.CandidateID)
	}
}

func TestGetNextAuditWork_RefactorPassStaleScheduleRefBlocks(t *testing.T) {
	t.Parallel()

	svc, st := newWorkPacketService(t)
	seedPlan(t, st, "relay", "plan-audit-stale")
	project := mustProjectRow(t, st, "relay")

	cand := seedReadyRefactorCandidate(t, st, project, "cand-audit-stale")
	// Mismatched pass id => stale scheduling reference.
	scheduleRefactorCandidate(t, st, project, cand, "plan-audit-stale", "PASS-404")
	makePassRefactor(t, st, "plan-audit-stale", "PASS-001", "cand-audit-stale")
	setPassStatus(t, st, "plan-audit-stale", "PASS-001", StatusPassAuditReady)
	seedAuditReadyRefactorRun(t, st, "plan-audit-stale", "PASS-001")

	resp, err := svc.GetNextAuditWork(context.Background(), NextAuditWorkRequest{
		ProjectID: "relay",
		PlanID:    "plan-audit-stale",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.OK {
		t.Fatalf("expected ok=false for stale schedule ref, got ok=true")
	}
	if len(resp.Blockers) == 0 || resp.Blockers[0].Code != BlockerUnsafeRequest {
		t.Fatalf("expected unsafe_request blocker, got %+v", resp.Blockers)
	}
	assertStaleRefactorMessage(t, resp.Blockers[0].Message)
}

func assertStaleRefactorMessage(t *testing.T, msg string) {
	t.Helper()
	const prefix = "stale refactor scheduling reference:"
	if len(msg) < len(prefix) || msg[:len(prefix)] != prefix {
		t.Fatalf("expected message to start with %q, got %q", prefix, msg)
	}
}
