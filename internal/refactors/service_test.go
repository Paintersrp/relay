package refactors

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"relay/internal/store"
)

func newTestService(t *testing.T) (*Service, *store.Store) {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	st, err := store.Open(filepath.Join(t.TempDir(), "relay.db"), logger)
	if err != nil {
		t.Fatalf("store.Open failed: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return NewService(st), st
}

func mustProject(t *testing.T, st *store.Store, projectID string) *store.Project {
	t.Helper()
	project, err := st.CreateProject(projectID, projectID+" name", "", "active", "")
	if err != nil {
		t.Fatalf("CreateProject(%s) failed: %v", projectID, err)
	}
	return project
}

func validCandidateInput(candidateID, title string) CandidateInput {
	return CandidateInput{
		CandidateID:        candidateID,
		Title:              title,
		ProblemSummary:     "Duplicate parsing branch causes drift.",
		DesiredBehavior:    "Single parsing path shared across callers.",
		Rationale:          "Reduces maintenance burden.",
		ProposedPassName:   "Consolidate parsing",
		ProposedPassGoal:   "Remove the duplicate parsing branch.",
		ProposedPassScope:  []string{"Replace duplicate parsing branch in internal/foo/bar.go"},
		NonGoals:           []string{"Do not change public API behavior"},
		TargetFiles:        []string{"internal/foo/bar.go"},
		ValidationCommands: []string{"go test ./internal/foo/..."},
		AuditFocus:         []string{"Verify behavior remains unchanged"},
		RiskLevel:          RiskMedium,
	}
}

func validDiscoveryInput(taskID, title string) DiscoveryTaskInput {
	return DiscoveryTaskInput{
		DiscoveryTaskID: taskID,
		Title:           title,
		AnalysisPrompt:  "Analyze whether the parsing branch is duplicated.",
		TargetScope:     TargetScope{Kind: "directory", Values: []string{"internal/foo"}},
	}
}

func TestDiscoveryTasksAreProjectScoped(t *testing.T) {
	svc, st := newTestService(t)
	ctx := context.Background()
	projectA := mustProject(t, st, "project-a")
	mustProject(t, st, "project-b")

	if _, issues, err := svc.CreateDiscoveryTask(ctx, projectA.ProjectID, validDiscoveryInput("task-a-1", "Investigate parsing")); err != nil || len(issues) > 0 {
		t.Fatalf("CreateDiscoveryTask failed: err=%v issues=%+v", err, issues)
	}

	tasksA, err := svc.ListDiscoveryTasks(ctx, "project-a", "", 0)
	if err != nil {
		t.Fatalf("ListDiscoveryTasks(A) failed: %v", err)
	}
	if len(tasksA) != 1 {
		t.Fatalf("expected 1 task in project A, got %d", len(tasksA))
	}

	tasksB, err := svc.ListDiscoveryTasks(ctx, "project-b", "", 0)
	if err != nil {
		t.Fatalf("ListDiscoveryTasks(B) failed: %v", err)
	}
	if len(tasksB) != 0 {
		t.Fatalf("expected 0 tasks in project B, got %d", len(tasksB))
	}

	// Cross-project get must not resolve.
	if _, err := svc.GetDiscoveryTask(ctx, "project-b", "task-a-1"); err == nil {
		t.Errorf("expected error getting project A task scoped to project B")
	}
}

func TestCandidateCreateRequiresPassReadyFields(t *testing.T) {
	svc, st := newTestService(t)
	ctx := context.Background()
	mustProject(t, st, "project-passready")

	input := CandidateInput{
		CandidateID: "candidate-bad",
		Title:       "Bad candidate",
		// Missing goal/problem/desired/rationale/pass-name/pass-goal/arrays, invalid risk.
		ProposedPassScope:  []string{},
		NonGoals:           []string{"   "},
		TargetFiles:        nil,
		ValidationCommands: []string{},
		AuditFocus:         []string{},
		RiskLevel:          "extreme",
	}

	candidate, issues, err := svc.CreateCandidate(ctx, "project-passready", input)
	if err != nil {
		t.Fatalf("CreateCandidate returned unexpected error: %v", err)
	}
	if candidate != nil {
		t.Fatalf("expected no candidate on validation failure, got %+v", candidate)
	}
	if len(issues) == 0 {
		t.Fatal("expected validation issues for non-pass-ready candidate")
	}

	wantCodes := map[string]bool{CodeNotPassReady: false, CodeInvalidRiskLevel: false}
	for _, issue := range issues {
		if _, ok := wantCodes[issue.Code]; ok {
			wantCodes[issue.Code] = true
		}
	}
	for code, seen := range wantCodes {
		if !seen {
			t.Errorf("expected a validation issue with code %q, got %+v", code, issues)
		}
	}

	// Verify nothing was persisted.
	rows, err := st.ListRefactorCandidatesByProject(mustGetProjectRowID(t, st, "project-passready"), 0)
	if err != nil {
		t.Fatalf("ListRefactorCandidatesByProject failed: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("expected 0 persisted candidates, got %d", len(rows))
	}
}

func TestCandidateRejectsCrossProjectDependencies(t *testing.T) {
	svc, st := newTestService(t)
	ctx := context.Background()
	mustProject(t, st, "project-a")
	mustProject(t, st, "project-b")

	// Candidate in project A and candidate in project B.
	if _, issues, err := svc.CreateCandidate(ctx, "project-a", validCandidateInput("cand-a", "Cand A")); err != nil || len(issues) > 0 {
		t.Fatalf("create cand-a failed: err=%v issues=%+v", err, issues)
	}
	if _, issues, err := svc.CreateCandidate(ctx, "project-b", validCandidateInput("cand-b", "Cand B")); err != nil || len(issues) > 0 {
		t.Fatalf("create cand-b failed: err=%v issues=%+v", err, issues)
	}

	// Attempt to create a candidate in project A that depends on project B's candidate.
	input := validCandidateInput("cand-a2", "Cand A2")
	input.CandidateDependencyIDs = []string{"cand-b"}
	candidate, issues, err := svc.CreateCandidate(ctx, "project-a", input)
	if err != nil {
		t.Fatalf("CreateCandidate returned unexpected error: %v", err)
	}
	if candidate != nil {
		t.Fatalf("expected no candidate on cross-project dependency, got %+v", candidate)
	}
	if !hasIssueCode(issues, CodeNotFound) {
		t.Fatalf("expected not_found validation for cross-project dependency, got %+v", issues)
	}

	// The candidate must not have been persisted.
	if _, err := svc.GetCandidate(ctx, "project-a", "cand-a2"); err == nil {
		t.Errorf("expected cand-a2 to not exist after failed create")
	}
}

func TestCandidateCreatePersistsSameProjectDependency(t *testing.T) {
	svc, st := newTestService(t)
	ctx := context.Background()
	project := mustProject(t, st, "project-deps")

	if _, issues, err := svc.CreateCandidate(ctx, "project-deps", validCandidateInput("dep-target", "Dependency target")); err != nil || len(issues) > 0 {
		t.Fatalf("create dependency target failed: err=%v issues=%+v", err, issues)
	}

	input := validCandidateInput("dep-source", "Dependency source")
	input.CandidateDependencyIDs = []string{"dep-target"}
	if _, issues, err := svc.CreateCandidate(ctx, "project-deps", input); err != nil || len(issues) > 0 {
		t.Fatalf("create dependency source failed: err=%v issues=%+v", err, issues)
	}

	// Resolve the source candidate row to confirm a dependency link exists.
	sourceRow, err := st.GetRefactorCandidateByCandidateID(project.ID, "dep-source")
	if err != nil {
		t.Fatalf("GetRefactorCandidateByCandidateID failed: %v", err)
	}
	deps, err := st.ListRefactorCandidateDependencies(project.ID, sourceRow.ID)
	if err != nil {
		t.Fatalf("ListRefactorCandidateDependencies failed: %v", err)
	}
	if len(deps) != 1 {
		t.Fatalf("expected 1 persisted dependency, got %d", len(deps))
	}
}

func TestCandidateRejectsSelfDependency(t *testing.T) {
	svc, st := newTestService(t)
	ctx := context.Background()
	mustProject(t, st, "project-self")

	input := validCandidateInput("cand-self", "Self dep")
	input.CandidateDependencyIDs = []string{"cand-self"}
	_, issues, err := svc.CreateCandidate(ctx, "project-self", input)
	if err != nil {
		t.Fatalf("CreateCandidate returned unexpected error: %v", err)
	}
	if !hasIssueCode(issues, CodeSelfReference) {
		t.Fatalf("expected self_reference validation, got %+v", issues)
	}
}

func TestCandidateLifecycleDeferRejectSupersede(t *testing.T) {
	svc, st := newTestService(t)
	ctx := context.Background()
	mustProject(t, st, "project-lifecycle")

	// Defer then reject.
	if _, issues, err := svc.CreateCandidate(ctx, "project-lifecycle", validCandidateInput("cand-1", "Cand 1")); err != nil || len(issues) > 0 {
		t.Fatalf("create cand-1 failed: err=%v issues=%+v", err, issues)
	}
	deferred, issues, err := svc.DeferCandidate(ctx, "project-lifecycle", "cand-1", CandidateLifecycleInput{DeferReason: "Not now"})
	if err != nil || len(issues) > 0 {
		t.Fatalf("defer cand-1 failed: err=%v issues=%+v", err, issues)
	}
	if deferred.Status != CandidateStatusDeferred {
		t.Fatalf("expected deferred status, got %q", deferred.Status)
	}
	rejected, issues, err := svc.RejectCandidate(ctx, "project-lifecycle", "cand-1", CandidateLifecycleInput{RejectReason: "Out of scope"})
	if err != nil || len(issues) > 0 {
		t.Fatalf("reject cand-1 failed: err=%v issues=%+v", err, issues)
	}
	if rejected.Status != CandidateStatusRejected {
		t.Fatalf("expected rejected status, got %q", rejected.Status)
	}

	// Defer requires a reason.
	if _, issues, _ := svc.CreateCandidate(ctx, "project-lifecycle", validCandidateInput("cand-2", "Cand 2")); len(issues) > 0 {
		t.Fatalf("create cand-2 unexpected issues: %+v", issues)
	}
	if _, issues, err := svc.DeferCandidate(ctx, "project-lifecycle", "cand-2", CandidateLifecycleInput{}); err != nil || !hasIssueCode(issues, CodeRequired) {
		t.Fatalf("expected required defer_reason validation, got err=%v issues=%+v", err, issues)
	}

	// Supersede cand-3 by cand-4, and reject self-supersede.
	if _, issues, _ := svc.CreateCandidate(ctx, "project-lifecycle", validCandidateInput("cand-3", "Cand 3")); len(issues) > 0 {
		t.Fatalf("create cand-3 unexpected issues: %+v", issues)
	}
	if _, issues, _ := svc.CreateCandidate(ctx, "project-lifecycle", validCandidateInput("cand-4", "Cand 4")); len(issues) > 0 {
		t.Fatalf("create cand-4 unexpected issues: %+v", issues)
	}
	superseded, issues, err := svc.SupersedeCandidate(ctx, "project-lifecycle", "cand-3", CandidateLifecycleInput{SupersededByCandidateID: "cand-4", SupersedeReason: "Replaced"})
	if err != nil || len(issues) > 0 {
		t.Fatalf("supersede cand-3 failed: err=%v issues=%+v", err, issues)
	}
	if superseded.Status != CandidateStatusSuperseded || superseded.SupersededByCandidateID != "cand-4" {
		t.Fatalf("unexpected superseded result: %+v", superseded)
	}
	if _, issues, _ := svc.SupersedeCandidate(ctx, "project-lifecycle", "cand-4", CandidateLifecycleInput{SupersededByCandidateID: "cand-4"}); !hasIssueCode(issues, CodeSelfReference) {
		t.Fatalf("expected self_reference for self-supersede, got %+v", issues)
	}

	// Confirm status events were recorded for the rejected candidate.
	rejRow, err := st.GetRefactorCandidateByCandidateID(mustGetProjectRowID(t, st, "project-lifecycle"), "cand-1")
	if err != nil {
		t.Fatalf("get cand-1 row failed: %v", err)
	}
	events, err := st.ListRefactorCandidateStatusEvents(rejRow.ProjectRowID, rejRow.ID, 0)
	if err != nil {
		t.Fatalf("ListRefactorCandidateStatusEvents failed: %v", err)
	}
	if len(events) < 2 {
		t.Fatalf("expected at least 2 status events (defer, reject) for cand-1, got %d", len(events))
	}
}

func TestCandidateTerminalStatusBlocksUpdate(t *testing.T) {
	svc, st := newTestService(t)
	ctx := context.Background()
	project := mustProject(t, st, "project-terminal")

	if _, issues, err := svc.CreateCandidate(ctx, "project-terminal", validCandidateInput("cand-term", "Terminal")); err != nil || len(issues) > 0 {
		t.Fatalf("create cand-term failed: err=%v issues=%+v", err, issues)
	}

	// Force a terminal status directly through the store.
	if _, err := st.UpdateRefactorCandidateStatusMetadata(store.UpdateRefactorCandidateStatusMetadataParams{
		ProjectRowID:   project.ID,
		CandidateID:    "cand-term",
		Status:         CandidateStatusRejected,
		RejectedReason: "terminal for test",
	}); err != nil {
		t.Fatalf("force terminal status failed: %v", err)
	}

	update := validCandidateInput("cand-term", "Terminal updated")
	candidate, issues, err := svc.UpdateCandidate(ctx, "project-terminal", "cand-term", update)
	if err != nil {
		t.Fatalf("UpdateCandidate returned unexpected error: %v", err)
	}
	if candidate != nil {
		t.Fatalf("expected no candidate update on terminal status, got %+v", candidate)
	}
	if !hasIssueCode(issues, CodeTerminalStatus) {
		t.Fatalf("expected terminal_status validation, got %+v", issues)
	}

	// Title must remain unchanged.
	row, err := st.GetRefactorCandidateByCandidateID(project.ID, "cand-term")
	if err != nil {
		t.Fatalf("get cand-term failed: %v", err)
	}
	if row.Title != "Terminal" {
		t.Fatalf("expected title unchanged, got %q", row.Title)
	}
}

func TestCandidateSecretLikeValueRejected(t *testing.T) {
	svc, st := newTestService(t)
	ctx := context.Background()
	mustProject(t, st, "project-secret")

	input := validCandidateInput("cand-secret", "Secret")
	input.Rationale = "token is ghp_exampletokenvalue1234567890"
	_, issues, err := svc.CreateCandidate(ctx, "project-secret", input)
	if err != nil {
		t.Fatalf("CreateCandidate returned unexpected error: %v", err)
	}
	if !hasIssueCode(issues, CodeSecretLikeValue) {
		t.Fatalf("expected secret_like_value validation, got %+v", issues)
	}
}

func TestCandidateListAndSearch(t *testing.T) {
	svc, st := newTestService(t)
	ctx := context.Background()
	mustProject(t, st, "project-search")

	in1 := validCandidateInput("cand-parser", "Parser cleanup")
	in1.Title = "Parser cleanup"
	if _, issues, err := svc.CreateCandidate(ctx, "project-search", in1); err != nil || len(issues) > 0 {
		t.Fatalf("create in1 failed: err=%v issues=%+v", err, issues)
	}
	in2 := validCandidateInput("cand-logger", "Logger refactor")
	in2.Title = "Logger refactor"
	if _, issues, err := svc.CreateCandidate(ctx, "project-search", in2); err != nil || len(issues) > 0 {
		t.Fatalf("create in2 failed: err=%v issues=%+v", err, issues)
	}

	all, err := svc.ListCandidates(ctx, "project-search", "", "", 0)
	if err != nil {
		t.Fatalf("ListCandidates failed: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2 candidates, got %d", len(all))
	}

	found, err := svc.ListCandidates(ctx, "project-search", "", "Logger", 0)
	if err != nil {
		t.Fatalf("ListCandidates(search) failed: %v", err)
	}
	if len(found) != 1 || found[0].CandidateID != "cand-logger" {
		t.Fatalf("expected only cand-logger from search, got %+v", found)
	}
}

func hasIssueCode(issues []ValidationIssue, code string) bool {
	for _, issue := range issues {
		if issue.Code == code {
			return true
		}
	}
	return false
}

func mustGetProjectRowID(t *testing.T, st *store.Store, projectID string) int64 {
	t.Helper()
	project, err := st.GetProjectByProjectID(projectID)
	if err != nil {
		t.Fatalf("GetProjectByProjectID(%s) failed: %v", projectID, err)
	}
	return project.ID
}

func validScheduleInput() CandidateScheduleInput {
	return CandidateScheduleInput{
		ScheduleKind: "existing_plan_bonus_pass",
		PlanID:       "plan-123",
		PassID:       "PASS-009",
		Note:         "Slotted as a bonus pass",
	}
}

func TestMarkCandidateScheduledPersistsScheduleRefAndEvent(t *testing.T) {
	svc, st := newTestService(t)
	ctx := context.Background()
	project := mustProject(t, st, "project-sched")

	if _, issues, err := svc.CreateCandidate(ctx, "project-sched", validCandidateInput("cand-sched", "Schedule me")); err != nil || len(issues) > 0 {
		t.Fatalf("create cand-sched failed: err=%v issues=%+v", err, issues)
	}

	input := validScheduleInput()
	input.RunID = "run-7"
	scheduled, issues, err := svc.MarkCandidateScheduled(ctx, "project-sched", "cand-sched", input)
	if err != nil || len(issues) > 0 {
		t.Fatalf("MarkCandidateScheduled failed: err=%v issues=%+v", err, issues)
	}
	if scheduled == nil || scheduled.Status != CandidateStatusScheduled {
		t.Fatalf("expected scheduled status, got %+v", scheduled)
	}

	row, err := st.GetRefactorCandidateByCandidateID(project.ID, "cand-sched")
	if err != nil {
		t.Fatalf("get cand-sched failed: %v", err)
	}

	active, err := st.GetActiveRefactorCandidateScheduleRef(project.ID, row.ID)
	if err != nil {
		t.Fatalf("GetActiveRefactorCandidateScheduleRef failed: %v", err)
	}
	if active == nil {
		t.Fatalf("expected an active schedule ref after mark-scheduled")
	}
	if active.ScheduleKind != "existing_plan_bonus_pass" || active.PlanID != "plan-123" || active.PassID != "PASS-009" || active.RunID != "run-7" {
		t.Fatalf("unexpected schedule ref contents: %+v", active)
	}

	events, err := st.ListRefactorCandidateStatusEvents(project.ID, row.ID, 0)
	if err != nil {
		t.Fatalf("ListRefactorCandidateStatusEvents failed: %v", err)
	}
	var sawScheduled bool
	for _, e := range events {
		if e.EventType == "scheduled" && e.FromStatus == CandidateStatusReady && e.ToStatus == CandidateStatusScheduled {
			sawScheduled = true
		}
	}
	if !sawScheduled {
		t.Fatalf("expected a scheduled status event from ready to scheduled, got %+v", events)
	}
}

func TestMarkCandidateScheduledRejectsDuplicate(t *testing.T) {
	svc, st := newTestService(t)
	ctx := context.Background()
	project := mustProject(t, st, "project-dupsched")

	if _, issues, err := svc.CreateCandidate(ctx, "project-dupsched", validCandidateInput("cand-dup", "Dup")); err != nil || len(issues) > 0 {
		t.Fatalf("create cand-dup failed: err=%v issues=%+v", err, issues)
	}

	if _, issues, err := svc.MarkCandidateScheduled(ctx, "project-dupsched", "cand-dup", validScheduleInput()); err != nil || len(issues) > 0 {
		t.Fatalf("first mark-scheduled failed: err=%v issues=%+v", err, issues)
	}

	// A second mark-scheduled must be rejected: the candidate is no longer ready
	// and an active schedule ref already exists.
	candidate, issues, err := svc.MarkCandidateScheduled(ctx, "project-dupsched", "cand-dup", validScheduleInput())
	if err != nil {
		t.Fatalf("second mark-scheduled returned unexpected error: %v", err)
	}
	if candidate != nil {
		t.Fatalf("expected no candidate on duplicate scheduling, got %+v", candidate)
	}
	if len(issues) == 0 {
		t.Fatalf("expected validation issue for duplicate scheduling")
	}

	// Only a single active schedule ref should exist.
	row, err := st.GetRefactorCandidateByCandidateID(project.ID, "cand-dup")
	if err != nil {
		t.Fatalf("get cand-dup failed: %v", err)
	}
	refs, err := st.ListRefactorCandidateScheduleRefs(project.ID, row.ID)
	if err != nil {
		t.Fatalf("ListRefactorCandidateScheduleRefs failed: %v", err)
	}
	if len(refs) != 1 {
		t.Fatalf("expected exactly 1 schedule ref, got %d", len(refs))
	}
}

func TestMarkCandidateScheduledRejectsNonReady(t *testing.T) {
	svc, st := newTestService(t)
	ctx := context.Background()
	mustProject(t, st, "project-nonready")

	if _, issues, err := svc.CreateCandidate(ctx, "project-nonready", validCandidateInput("cand-nr", "Non ready")); err != nil || len(issues) > 0 {
		t.Fatalf("create cand-nr failed: err=%v issues=%+v", err, issues)
	}
	if _, issues, err := svc.DeferCandidate(ctx, "project-nonready", "cand-nr", CandidateLifecycleInput{DeferReason: "later"}); err != nil || len(issues) > 0 {
		t.Fatalf("defer cand-nr failed: err=%v issues=%+v", err, issues)
	}

	candidate, issues, err := svc.MarkCandidateScheduled(ctx, "project-nonready", "cand-nr", validScheduleInput())
	if err != nil {
		t.Fatalf("MarkCandidateScheduled returned unexpected error: %v", err)
	}
	if candidate != nil {
		t.Fatalf("expected no candidate when scheduling a non-ready candidate, got %+v", candidate)
	}
	if !hasIssueCode(issues, CodeInvalidTransition) {
		t.Fatalf("expected invalid_transition for non-ready candidate, got %+v", issues)
	}
}

func TestMarkCandidateScheduledValidatesInput(t *testing.T) {
	svc, st := newTestService(t)
	ctx := context.Background()
	mustProject(t, st, "project-schedval")

	if _, issues, err := svc.CreateCandidate(ctx, "project-schedval", validCandidateInput("cand-sv", "Schedule val")); err != nil || len(issues) > 0 {
		t.Fatalf("create cand-sv failed: err=%v issues=%+v", err, issues)
	}

	// Missing plan_id/pass_id and invalid schedule_kind.
	_, issues, err := svc.MarkCandidateScheduled(ctx, "project-schedval", "cand-sv", CandidateScheduleInput{ScheduleKind: "bogus_kind"})
	if err != nil {
		t.Fatalf("MarkCandidateScheduled returned unexpected error: %v", err)
	}
	if !hasIssueCode(issues, CodeInvalidScheduleKind) {
		t.Fatalf("expected invalid_schedule_kind, got %+v", issues)
	}
	if !hasIssueCode(issues, CodeRequired) {
		t.Fatalf("expected required plan_id/pass_id, got %+v", issues)
	}
}

func markScheduledForTest(t *testing.T, svc *Service, projectID, candidateID string) {
	t.Helper()
	if _, issues, err := svc.MarkCandidateScheduled(context.Background(), projectID, candidateID, validScheduleInput()); err != nil || len(issues) > 0 {
		t.Fatalf("mark-scheduled %s failed: err=%v issues=%+v", candidateID, err, issues)
	}
}

func TestApplyCandidateCompletionHookAcceptsAllOutcomes(t *testing.T) {
	ctx := context.Background()
	outcomes := []string{
		CandidateStatusCompleted,
		CandidateStatusCompletedWithWarnings,
		CandidateStatusScheduledRevisionRequired,
		CandidateStatusDeferred,
	}
	for _, outcome := range outcomes {
		outcome := outcome
		t.Run(outcome, func(t *testing.T) {
			svc, st := newTestService(t)
			mustProject(t, st, "project-hook")
			candidateID := "cand-" + outcome
			if _, issues, err := svc.CreateCandidate(ctx, "project-hook", validCandidateInput(candidateID, "Hook "+outcome)); err != nil || len(issues) > 0 {
				t.Fatalf("create %s failed: err=%v issues=%+v", candidateID, err, issues)
			}
			markScheduledForTest(t, svc, "project-hook", candidateID)

			result, issues, err := svc.ApplyCandidateCompletionHook(ctx, "project-hook", candidateID, CandidateCompletionHookInput{Status: outcome, Reason: "audit outcome"})
			if err != nil || len(issues) > 0 {
				t.Fatalf("ApplyCandidateCompletionHook(%s) failed: err=%v issues=%+v", outcome, err, issues)
			}
			if result == nil || result.Status != outcome {
				t.Fatalf("expected status %q, got %+v", outcome, result)
			}
		})
	}
}

func TestApplyCandidateCompletionHookRejectsUnscheduledSource(t *testing.T) {
	svc, st := newTestService(t)
	ctx := context.Background()
	mustProject(t, st, "project-hooksrc")

	if _, issues, err := svc.CreateCandidate(ctx, "project-hooksrc", validCandidateInput("cand-ready", "Ready")); err != nil || len(issues) > 0 {
		t.Fatalf("create cand-ready failed: err=%v issues=%+v", err, issues)
	}

	// Candidate is still ready, not scheduled.
	candidate, issues, err := svc.ApplyCandidateCompletionHook(ctx, "project-hooksrc", "cand-ready", CandidateCompletionHookInput{Status: CandidateStatusCompleted})
	if err != nil {
		t.Fatalf("ApplyCandidateCompletionHook returned unexpected error: %v", err)
	}
	if candidate != nil {
		t.Fatalf("expected no candidate for unscheduled source, got %+v", candidate)
	}
	if !hasIssueCode(issues, CodeInvalidTransition) {
		t.Fatalf("expected invalid_transition for unscheduled source, got %+v", issues)
	}
}

func TestApplyCandidateCompletionHookRejectsInvalidTargetStatus(t *testing.T) {
	svc, st := newTestService(t)
	ctx := context.Background()
	mustProject(t, st, "project-hooktarget")

	if _, issues, err := svc.CreateCandidate(ctx, "project-hooktarget", validCandidateInput("cand-ht", "Hook target")); err != nil || len(issues) > 0 {
		t.Fatalf("create cand-ht failed: err=%v issues=%+v", err, issues)
	}
	markScheduledForTest(t, svc, "project-hooktarget", "cand-ht")

	// "rejected" is intentionally not an allowed completion-hook outcome.
	_, issues, err := svc.ApplyCandidateCompletionHook(ctx, "project-hooktarget", "cand-ht", CandidateCompletionHookInput{Status: CandidateStatusRejected})
	if err != nil {
		t.Fatalf("ApplyCandidateCompletionHook returned unexpected error: %v", err)
	}
	if !hasIssueCode(issues, CodeInvalidStatus) {
		t.Fatalf("expected invalid_status for disallowed target status, got %+v", issues)
	}
}
