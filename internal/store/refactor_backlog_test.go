package store

import (
	"io"
	"log/slog"
	"path/filepath"
	"testing"
)

func newRefactorBacklogTestStore(t *testing.T) *Store {
	t.Helper()
	tempDB := filepath.Join(t.TempDir(), "refactor_backlog.db")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	st, err := Open(tempDB, logger)
	if err != nil {
		t.Fatalf("Open store failed: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func mustCreateProject(t *testing.T, st *Store, projectID string) *Project {
	t.Helper()
	project, err := st.CreateProject(projectID, projectID+" name", "", "active", "")
	if err != nil {
		t.Fatalf("CreateProject(%s) failed: %v", projectID, err)
	}
	return project
}

// validCandidateParams returns a fully pass-ready candidate parameter set.
func validCandidateParams(project *Project, candidateID, title string) CreateRefactorCandidateParams {
	return CreateRefactorCandidateParams{
		CandidateID:            candidateID,
		ProjectRowID:           project.ID,
		ProjectID:              project.ProjectID,
		Title:                  title,
		ProblemSummary:         "Duplicate parsing branch causes drift.",
		DesiredBehavior:        "Single parsing path shared across callers.",
		Rationale:              "Reduces maintenance burden and divergence risk.",
		ProposedPassName:       "Consolidate parsing",
		ProposedPassGoal:       "Remove the duplicate parsing branch.",
		ProposedPassScopeJSON:  `["Replace duplicate parsing branch in internal/foo/bar.go"]`,
		ProposedNonGoalsJSON:   `["Do not change public API behavior"]`,
		TargetFilesJSON:        `["internal/foo/bar.go"]`,
		ValidationCommandsJSON: `["go test ./internal/foo/..."]`,
		AuditFocusJSON:         `["Verify behavior remains unchanged and duplicate branch is removed"]`,
		ConstraintsJSON:        `[]`,
		RiskLevel:              "medium",
		Status:                 "ready",
		MetadataJSON:           `{}`,
	}
}

func TestRefactorBacklogPersistenceDiscoveryTaskProjectScope(t *testing.T) {
	st := newRefactorBacklogTestStore(t)

	projectA := mustCreateProject(t, st, "project-a")
	projectB := mustCreateProject(t, st, "project-b")

	_, err := st.CreateRefactorDiscoveryTask(CreateRefactorDiscoveryTaskParams{
		TaskID:       "task-a-1",
		ProjectRowID: projectA.ID,
		ProjectID:    projectA.ProjectID,
		Title:        "Investigate parsing duplication",
		Prompt:       "Analyze whether the parsing branch is duplicated.",
	})
	if err != nil {
		t.Fatalf("CreateRefactorDiscoveryTask failed: %v", err)
	}

	tasksA, err := st.ListRefactorDiscoveryTasksByProject(projectA.ID, 0)
	if err != nil {
		t.Fatalf("ListRefactorDiscoveryTasksByProject(A) failed: %v", err)
	}
	if len(tasksA) != 1 {
		t.Fatalf("expected 1 task in project A, got %d", len(tasksA))
	}
	if tasksA[0].TaskID != "task-a-1" {
		t.Errorf("expected task-a-1, got %s", tasksA[0].TaskID)
	}

	tasksB, err := st.ListRefactorDiscoveryTasksByProject(projectB.ID, 0)
	if err != nil {
		t.Fatalf("ListRefactorDiscoveryTasksByProject(B) failed: %v", err)
	}
	if len(tasksB) != 0 {
		t.Fatalf("expected 0 tasks in project B, got %d", len(tasksB))
	}

	// Cross-project get must not resolve.
	if _, err := st.GetRefactorDiscoveryTaskByTaskID(projectB.ID, "task-a-1"); err == nil {
		t.Errorf("expected error getting project A task scoped to project B")
	}
}

func TestRefactorCandidateRequiresPassReadyFields(t *testing.T) {
	st := newRefactorBacklogTestStore(t)
	project := mustCreateProject(t, st, "project-passready")

	cases := []struct {
		name   string
		mutate func(p *CreateRefactorCandidateParams)
	}{
		{
			name: "blank proposed_pass_goal",
			mutate: func(p *CreateRefactorCandidateParams) {
				p.ProposedPassGoal = "   "
			},
		},
		{
			name: "empty proposed_pass_scope_json",
			mutate: func(p *CreateRefactorCandidateParams) {
				p.ProposedPassScopeJSON = `[]`
			},
		},
		{
			name: "empty target_files_json",
			mutate: func(p *CreateRefactorCandidateParams) {
				p.TargetFilesJSON = `[]`
			},
		},
		{
			name: "malformed validation_commands_json",
			mutate: func(p *CreateRefactorCandidateParams) {
				p.ValidationCommandsJSON = `["go test`
			},
		},
		{
			name: "object instead of array for audit_focus_json",
			mutate: func(p *CreateRefactorCandidateParams) {
				p.AuditFocusJSON = `{}`
			},
		},
		{
			name: "invalid risk level",
			mutate: func(p *CreateRefactorCandidateParams) {
				p.RiskLevel = "extreme"
			},
		},
		{
			name: "non-ready initial status",
			mutate: func(p *CreateRefactorCandidateParams) {
				p.Status = "scheduled"
			},
		},
	}

	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			params := validCandidateParams(project, "candidate-bad", "Bad candidate")
			// Use distinct candidate IDs so a leaked insert would be detectable.
			params.CandidateID = "candidate-bad"
			tc.mutate(&params)

			if _, err := st.CreateRefactorCandidate(params); err == nil {
				t.Fatalf("case %d (%s): expected error, got nil", i, tc.name)
			}

			// Verify no row was inserted.
			if _, err := st.GetRefactorCandidateByCandidateID(project.ID, "candidate-bad"); err == nil {
				t.Fatalf("case %d (%s): candidate row was inserted despite validation failure", i, tc.name)
			}
		})
	}

	// Confirm no candidates exist in the project at all.
	all, err := st.ListRefactorCandidatesByProject(project.ID, 0)
	if err != nil {
		t.Fatalf("ListRefactorCandidatesByProject failed: %v", err)
	}
	if len(all) != 0 {
		t.Fatalf("expected 0 candidates after failed creates, got %d", len(all))
	}
}

func TestRefactorCandidatePersistenceLinksAndScheduleRefs(t *testing.T) {
	st := newRefactorBacklogTestStore(t)
	project := mustCreateProject(t, st, "project-links")

	task, err := st.CreateRefactorDiscoveryTask(CreateRefactorDiscoveryTaskParams{
		TaskID:       "task-1",
		ProjectRowID: project.ID,
		ProjectID:    project.ProjectID,
		Title:        "Discovery task",
		Prompt:       "Investigate.",
	})
	if err != nil {
		t.Fatalf("CreateRefactorDiscoveryTask failed: %v", err)
	}

	cand1, err := st.CreateRefactorCandidate(validCandidateParams(project, "candidate-1", "Candidate one"))
	if err != nil {
		t.Fatalf("CreateRefactorCandidate(1) failed: %v", err)
	}
	cand2, err := st.CreateRefactorCandidate(validCandidateParams(project, "candidate-2", "Candidate two"))
	if err != nil {
		t.Fatalf("CreateRefactorCandidate(2) failed: %v", err)
	}

	// Link candidate 1 -> discovery task.
	link, err := st.CreateRefactorCandidateDiscoveryLink(CreateRefactorCandidateDiscoveryLinkParams{
		LinkID:             "link-1",
		ProjectRowID:       project.ID,
		ProjectID:          project.ProjectID,
		CandidateRowID:     cand1.ID,
		DiscoveryTaskRowID: task.ID,
	})
	if err != nil {
		t.Fatalf("CreateRefactorCandidateDiscoveryLink failed: %v", err)
	}
	if link.LinkID != "link-1" {
		t.Errorf("expected link-1, got %s", link.LinkID)
	}

	links, err := st.ListRefactorCandidateDiscoveryLinks(project.ID, cand1.ID)
	if err != nil {
		t.Fatalf("ListRefactorCandidateDiscoveryLinks failed: %v", err)
	}
	if len(links) != 1 {
		t.Fatalf("expected 1 link for candidate 1, got %d", len(links))
	}

	taskLinks, err := st.ListRefactorDiscoveryTaskCandidateLinks(project.ID, task.ID)
	if err != nil {
		t.Fatalf("ListRefactorDiscoveryTaskCandidateLinks failed: %v", err)
	}
	if len(taskLinks) != 1 {
		t.Fatalf("expected 1 candidate link for task, got %d", len(taskLinks))
	}

	// Candidate 1 depends on candidate 2.
	dep, err := st.CreateRefactorCandidateDependency(CreateRefactorCandidateDependencyParams{
		DependencyID:            "dep-1",
		ProjectRowID:            project.ID,
		ProjectID:               project.ProjectID,
		CandidateRowID:          cand1.ID,
		DependsOnCandidateRowID: cand2.ID,
	})
	if err != nil {
		t.Fatalf("CreateRefactorCandidateDependency failed: %v", err)
	}
	if dep.DependencyType != "blocks" {
		t.Errorf("expected default dependency_type 'blocks', got %s", dep.DependencyType)
	}

	deps, err := st.ListRefactorCandidateDependencies(project.ID, cand1.ID)
	if err != nil {
		t.Fatalf("ListRefactorCandidateDependencies failed: %v", err)
	}
	if len(deps) != 1 || deps[0].DependsOnCandidateRowID != cand2.ID {
		t.Fatalf("expected 1 dependency on candidate 2, got %+v", deps)
	}

	// Self-dependency must be rejected.
	if _, err := st.CreateRefactorCandidateDependency(CreateRefactorCandidateDependencyParams{
		DependencyID:            "dep-self",
		ProjectRowID:            project.ID,
		ProjectID:               project.ProjectID,
		CandidateRowID:          cand1.ID,
		DependsOnCandidateRowID: cand1.ID,
	}); err == nil {
		t.Errorf("expected self-dependency to be rejected")
	}

	// Schedule ref for candidate 1.
	scheduleRef, err := st.CreateRefactorCandidateScheduleRef(CreateRefactorCandidateScheduleRefParams{
		ScheduleRefID:  "sched-1",
		ProjectRowID:   project.ID,
		ProjectID:      project.ProjectID,
		CandidateRowID: cand1.ID,
		ScheduleKind:   "existing_plan_bonus_pass",
		PlanID:         "plan-test",
		PassID:         "PASS-999",
	})
	if err != nil {
		t.Fatalf("CreateRefactorCandidateScheduleRef failed: %v", err)
	}
	if scheduleRef.Status != "scheduled" {
		t.Errorf("expected default schedule status 'scheduled', got %s", scheduleRef.Status)
	}
	if scheduleRef.PlanID != "plan-test" || scheduleRef.PassID != "PASS-999" {
		t.Errorf("unexpected schedule ref plan/pass: %s / %s", scheduleRef.PlanID, scheduleRef.PassID)
	}
	if scheduleRef.PlanRowID.Valid {
		t.Errorf("expected nullable plan_row_id to be NULL when not provided")
	}

	scheduleRefs, err := st.ListRefactorCandidateScheduleRefs(project.ID, cand1.ID)
	if err != nil {
		t.Fatalf("ListRefactorCandidateScheduleRefs failed: %v", err)
	}
	if len(scheduleRefs) != 1 {
		t.Fatalf("expected 1 schedule ref, got %d", len(scheduleRefs))
	}

	active, err := st.GetActiveRefactorCandidateScheduleRef(project.ID, cand1.ID)
	if err != nil {
		t.Fatalf("GetActiveRefactorCandidateScheduleRef failed: %v", err)
	}
	if active == nil || active.ScheduleRefID != "sched-1" {
		t.Fatalf("expected active schedule ref sched-1, got %+v", active)
	}

	// Status event for candidate 1.
	event, err := st.CreateRefactorCandidateStatusEvent(CreateRefactorCandidateStatusEventParams{
		EventID:        "event-1",
		ProjectRowID:   project.ID,
		ProjectID:      project.ProjectID,
		CandidateRowID: cand1.ID,
		EventType:      "scheduled",
		FromStatus:     "ready",
		ToStatus:       "scheduled",
		Reason:         "Scheduled into bonus pass.",
	})
	if err != nil {
		t.Fatalf("CreateRefactorCandidateStatusEvent failed: %v", err)
	}
	if event.EventType != "scheduled" {
		t.Errorf("expected event type 'scheduled', got %s", event.EventType)
	}

	events, err := st.ListRefactorCandidateStatusEvents(project.ID, cand1.ID, 0)
	if err != nil {
		t.Fatalf("ListRefactorCandidateStatusEvents failed: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 status event, got %d", len(events))
	}
}

func TestRefactorCandidateRejectsCrossProjectLinking(t *testing.T) {
	st := newRefactorBacklogTestStore(t)
	projectA := mustCreateProject(t, st, "project-x")
	projectB := mustCreateProject(t, st, "project-y")

	candA, err := st.CreateRefactorCandidate(validCandidateParams(projectA, "candidate-a", "Candidate A"))
	if err != nil {
		t.Fatalf("CreateRefactorCandidate(A) failed: %v", err)
	}
	candB, err := st.CreateRefactorCandidate(validCandidateParams(projectB, "candidate-b", "Candidate B"))
	if err != nil {
		t.Fatalf("CreateRefactorCandidate(B) failed: %v", err)
	}

	taskB, err := st.CreateRefactorDiscoveryTask(CreateRefactorDiscoveryTaskParams{
		TaskID:       "task-b",
		ProjectRowID: projectB.ID,
		ProjectID:    projectB.ProjectID,
		Title:        "Task B",
		Prompt:       "Investigate B.",
	})
	if err != nil {
		t.Fatalf("CreateRefactorDiscoveryTask(B) failed: %v", err)
	}

	// Link declared under project A, but discovery task belongs to project B.
	if _, err := st.CreateRefactorCandidateDiscoveryLink(CreateRefactorCandidateDiscoveryLinkParams{
		LinkID:             "link-cross",
		ProjectRowID:       projectA.ID,
		ProjectID:          projectA.ProjectID,
		CandidateRowID:     candA.ID,
		DiscoveryTaskRowID: taskB.ID,
	}); err == nil {
		t.Errorf("expected cross-project link to be rejected")
	}

	// Dependency declared under project A, but depends-on candidate belongs to project B.
	if _, err := st.CreateRefactorCandidateDependency(CreateRefactorCandidateDependencyParams{
		DependencyID:            "dep-cross",
		ProjectRowID:            projectA.ID,
		ProjectID:               projectA.ProjectID,
		CandidateRowID:          candA.ID,
		DependsOnCandidateRowID: candB.ID,
	}); err == nil {
		t.Errorf("expected cross-project dependency to be rejected")
	}

	// Ensure no durable cross-project artifacts were created.
	links, err := st.ListRefactorCandidateDiscoveryLinks(projectA.ID, candA.ID)
	if err != nil {
		t.Fatalf("ListRefactorCandidateDiscoveryLinks failed: %v", err)
	}
	if len(links) != 0 {
		t.Errorf("expected no links for candidate A, got %d", len(links))
	}
	deps, err := st.ListRefactorCandidateDependencies(projectA.ID, candA.ID)
	if err != nil {
		t.Fatalf("ListRefactorCandidateDependencies failed: %v", err)
	}
	if len(deps) != 0 {
		t.Errorf("expected no dependencies for candidate A, got %d", len(deps))
	}
}
