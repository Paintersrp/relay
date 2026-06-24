package plans

import (
	"context"
	"database/sql"
	"testing"

	"relay/internal/store"
)

// setupRefactorLifecycle seeds a scheduled refactor pass plus a ready candidate
// scheduled into it, and returns a run associated with that refactor pass at the
// given pass status. It reuses the lifecycle test plan seeding helpers.
func setupRefactorLifecycle(t *testing.T, svc *RunLifecycleService, st *store.Store, planID, candidateID, passStatus string) (*store.Run, *store.Project) {
	t.Helper()

	plan := submitLifecyclePlan(t, st, planID)
	project, err := st.GetProjectByProjectID("test-project")
	if err != nil {
		t.Fatalf("GetProjectByProjectID: %v", err)
	}

	cand := seedReadyRefactorCandidate(t, st, project, candidateID)
	scheduleRefactorCandidate(t, st, project, cand, planID, "PASS-001")
	makePassRefactor(t, st, planID, "PASS-001", candidateID)

	pass, err := st.GetPlanPassByPassID(plan.ID, "PASS-001")
	if err != nil {
		t.Fatalf("GetPlanPassByPassID: %v", err)
	}
	if _, err := st.UpdatePlanPassStatus(pass.ID, passStatus); err != nil {
		t.Fatalf("seed pass status: %v", err)
	}

	run, err := st.CreateRunWithAssociation(
		1,
		"Refactor Lifecycle Run",
		"audit_ready",
		"gpt-4o", "gpt-4o", store.DefaultExecutorAdapter, "main",
		sql.NullInt64{Int64: plan.ID, Valid: true},
		sql.NullInt64{Int64: pass.ID, Valid: true},
	)
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	return run, project
}

func candidateStatus(t *testing.T, st *store.Store, project *store.Project, candidateID string) string {
	t.Helper()
	cand, err := st.GetRefactorCandidateByCandidateID(project.ID, candidateID)
	if err != nil {
		t.Fatalf("GetRefactorCandidateByCandidateID: %v", err)
	}
	return cand.Status
}

func TestApplyAuditDecision_RefactorAcceptedCompletesCandidate(t *testing.T) {
	svc, st := setupLifecycleTestService(t)
	run, project := setupRefactorLifecycle(t, svc, st, "plan-refactor-accept", "cand-accept", StatusPassAuditReady)

	if err := svc.ApplyAuditDecision(run, "accepted"); err != nil {
		t.Fatalf("ApplyAuditDecision: %v", err)
	}

	if got := candidateStatus(t, st, project, "cand-accept"); got != "completed" {
		t.Fatalf("expected candidate completed, got %q", got)
	}
}

func TestApplyAuditDecision_RefactorAcceptedWithWarningsCompletesCandidateWithWarnings(t *testing.T) {
	svc, st := setupLifecycleTestService(t)
	run, project := setupRefactorLifecycle(t, svc, st, "plan-refactor-warn", "cand-warn", StatusPassAuditReady)

	if err := svc.ApplyAuditDecision(run, "accepted_with_warnings"); err != nil {
		t.Fatalf("ApplyAuditDecision: %v", err)
	}

	if got := candidateStatus(t, st, project, "cand-warn"); got != "completed_with_warnings" {
		t.Fatalf("expected candidate completed_with_warnings, got %q", got)
	}
}

func TestApplyAuditDecision_RefactorRevisionRequiredMarksCandidateRevisionRequired(t *testing.T) {
	svc, st := setupLifecycleTestService(t)
	run, project := setupRefactorLifecycle(t, svc, st, "plan-refactor-revision", "cand-revision", StatusPassAuditReady)

	if err := svc.ApplyAuditDecision(run, "revision_required"); err != nil {
		t.Fatalf("ApplyAuditDecision: %v", err)
	}

	if got := candidateStatus(t, st, project, "cand-revision"); got != "scheduled_revision_required" {
		t.Fatalf("expected candidate scheduled_revision_required, got %q", got)
	}

	// Revision must keep the same pass selected for repair and not advance.
	work := NewOrchestratorWorkService(st)
	resp, err := work.GetNextPassWork(context.Background(), NextPassWorkRequest{
		ProjectID: "test-project",
		PlanID:    "plan-refactor-revision",
	})
	if err != nil {
		t.Fatalf("GetNextPassWork: %v", err)
	}
	if resp.OK {
		t.Fatalf("expected blocker, got ok=true")
	}
	if resp.Blockers[0].Code != BlockerRevisionRequiredSamePass {
		t.Fatalf("expected revision_required_same_pass, got %q", resp.Blockers[0].Code)
	}
}

func TestApplyAuditDecision_RefactorBlockedDoesNotRejectCandidate(t *testing.T) {
	svc, st := setupLifecycleTestService(t)

	for _, decision := range []string{"blocked", "manual_review_required", "rejected"} {
		t.Run(decision, func(t *testing.T) {
			run, project := setupRefactorLifecycle(t, svc, st, "plan-refactor-blocked-"+decision, "cand-blocked-"+decision, StatusPassAuditReady)

			if err := svc.ApplyAuditDecision(run, decision); err != nil {
				t.Fatalf("ApplyAuditDecision: %v", err)
			}

			if got := candidateStatus(t, st, project, "cand-blocked-"+decision); got != "scheduled" {
				t.Fatalf("decision %q: expected candidate to remain scheduled, got %q", decision, got)
			}
		})
	}
}

func TestApplyAuditDecision_RefactorAcceptedIsIdempotent(t *testing.T) {
	svc, st := setupLifecycleTestService(t)
	run, project := setupRefactorLifecycle(t, svc, st, "plan-refactor-idem", "cand-idem", StatusPassAuditReady)

	if err := svc.ApplyAuditDecision(run, "accepted"); err != nil {
		t.Fatalf("first ApplyAuditDecision: %v", err)
	}
	eventsAfterFirst := countRows(t, st.DB(), "refactor_candidate_status_events")

	// Re-applying the same decision must not downgrade or duplicate the terminal
	// candidate transition.
	if err := svc.ApplyAuditDecision(run, "accepted"); err != nil {
		t.Fatalf("second ApplyAuditDecision: %v", err)
	}
	if got := candidateStatus(t, st, project, "cand-idem"); got != "completed" {
		t.Fatalf("expected candidate to remain completed, got %q", got)
	}
	if eventsAfter := countRows(t, st.DB(), "refactor_candidate_status_events"); eventsAfter != eventsAfterFirst {
		t.Fatalf("re-application added %d status event(s); expected 0", eventsAfter-eventsAfterFirst)
	}
}

func TestApplyAuditDecision_NonRefactorPassNoCandidateMapping(t *testing.T) {
	svc, st := setupLifecycleTestService(t)
	run, _ := createLifecycleRunWithPass(t, st, "plan-refactor-nonrefactor", "PASS-001", "audit_ready")

	// A normal (non-refactor) pass must apply without touching any candidate.
	before := countRows(t, st.DB(), "refactor_candidate_status_events")
	if err := svc.ApplyAuditDecision(run, "accepted"); err != nil {
		t.Fatalf("ApplyAuditDecision: %v", err)
	}
	if after := countRows(t, st.DB(), "refactor_candidate_status_events"); after != before {
		t.Fatalf("non-refactor pass recorded %d candidate event(s); expected 0", after-before)
	}
}

func TestApplyRefactorCandidateForSkippedPassDefersCandidate(t *testing.T) {
	svc, st := setupLifecycleTestService(t)
	_, project := setupRefactorLifecycle(t, svc, st, "plan-refactor-skip", "cand-skip", StatusPassPlanned)

	plan, err := st.GetPlanByPlanID("plan-refactor-skip")
	if err != nil {
		t.Fatalf("GetPlanByPlanID: %v", err)
	}
	pass, err := st.GetPlanPassByPassID(plan.ID, "PASS-001")
	if err != nil {
		t.Fatalf("GetPlanPassByPassID: %v", err)
	}

	if err := svc.ApplyRefactorCandidateForSkippedPass(pass); err != nil {
		t.Fatalf("ApplyRefactorCandidateForSkippedPass: %v", err)
	}

	if got := candidateStatus(t, st, project, "cand-skip"); got != "deferred" {
		t.Fatalf("expected candidate deferred, got %q", got)
	}
}
