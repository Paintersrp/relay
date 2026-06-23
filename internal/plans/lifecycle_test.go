package plans

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"relay/internal/store"
)

func TestRunLifecycleService(t *testing.T) {
	svc, st := setupLifecycleTestService(t)

	t.Run("associated run creation marks planned pass in progress", func(t *testing.T) {
		run, pass := createLifecycleRunWithPass(t, st, "plan-lifecycle-start", "PASS-001", "planned")

		if err := svc.MarkAssociatedPassInProgress(run); err != nil {
			t.Fatalf("mark in progress: %v", err)
		}

		updatedPass, err := st.GetPlanPass(pass.ID)
		if err != nil {
			t.Fatalf("get pass: %v", err)
		}
		if updatedPass.Status != "in_progress" {
			t.Fatalf("expected in_progress, got %q", updatedPass.Status)
		}
	})

	t.Run("repeated start is idempotent", func(t *testing.T) {
		run, pass := createLifecycleRunWithPass(t, st, "plan-lifecycle-repeat", "PASS-001", "in_progress")

		if err := svc.MarkAssociatedPassInProgress(run); err != nil {
			t.Fatalf("mark in progress: %v", err)
		}

		updatedPass, err := st.GetPlanPass(pass.ID)
		if err != nil {
			t.Fatalf("get pass: %v", err)
		}
		if updatedPass.Status != "in_progress" {
			t.Fatalf("expected in_progress, got %q", updatedPass.Status)
		}
	})

	t.Run("terminal pass statuses are not downgraded on run creation", func(t *testing.T) {
		for _, status := range []string{"completed", "skipped"} {
			t.Run(status, func(t *testing.T) {
				run, pass := createLifecycleRunWithPass(t, st, "plan-lifecycle-terminal-"+status, "PASS-001", status)

				if err := svc.MarkAssociatedPassInProgress(run); err != nil {
					t.Fatalf("mark in progress: %v", err)
				}

				updatedPass, err := st.GetPlanPass(pass.ID)
				if err != nil {
					t.Fatalf("get pass: %v", err)
				}
				if updatedPass.Status != status {
					t.Fatalf("expected %q, got %q", status, updatedPass.Status)
				}
			})
		}
	})

	t.Run("accepted decisions mark pass completed", func(t *testing.T) {
		for _, decision := range []string{"accepted", "accepted_with_warnings"} {
			t.Run(decision, func(t *testing.T) {
				run, pass := createLifecycleRunWithPass(t, st, "plan-lifecycle-accept-"+decision, "PASS-001", "in_progress")

				if err := svc.ApplyAuditDecision(run, decision); err != nil {
					t.Fatalf("apply audit decision: %v", err)
				}

				updatedPass, err := st.GetPlanPass(pass.ID)
				if err != nil {
					t.Fatalf("get pass: %v", err)
				}
				if updatedPass.Status != "completed" {
					t.Fatalf("expected completed, got %q", updatedPass.Status)
				}
			})
		}
	})

	t.Run("revision required maps to revision_required status", func(t *testing.T) {
		run, pass := createLifecycleRunWithPass(t, st, "plan-lifecycle-revision", "PASS-001", "planned")

		if err := svc.ApplyAuditDecision(run, "revision_required"); err != nil {
			t.Fatalf("apply audit decision: %v", err)
		}

		updatedPass, err := st.GetPlanPass(pass.ID)
		if err != nil {
			t.Fatalf("get pass: %v", err)
		}
		if updatedPass.Status != "revision_required" {
			t.Fatalf("expected revision_required, got %q", updatedPass.Status)
		}
	})

	t.Run("blocked decisions map to blocked status", func(t *testing.T) {
		for _, decision := range []string{"blocked", "manual_review_required", "rejected"} {
			t.Run(decision, func(t *testing.T) {
				run, pass := createLifecycleRunWithPass(t, st, "plan-lifecycle-blocked-"+decision, "PASS-001", "in_progress")

				if err := svc.ApplyAuditDecision(run, decision); err != nil {
					t.Fatalf("apply audit decision: %v", err)
				}

				updatedPass, err := st.GetPlanPass(pass.ID)
				if err != nil {
					t.Fatalf("get pass: %v", err)
				}
				if updatedPass.Status != "blocked" {
					t.Fatalf("expected blocked, got %q", updatedPass.Status)
				}
			})
		}
	})

	t.Run("standalone and plan-only runs are no-ops", func(t *testing.T) {
		plan := submitLifecyclePlan(t, st, "plan-lifecycle-noop")
		pass, err := st.GetPlanPassByPassID(plan.ID, "PASS-001")
		if err != nil {
			t.Fatalf("get pass: %v", err)
		}

		standaloneRun, err := st.CreateRunWithExecutorAdapter(1, "Standalone", "intake_received", "gpt-4o", "gpt-4o", store.DefaultExecutorAdapter, "main")
		if err != nil {
			t.Fatalf("create standalone run: %v", err)
		}
		planOnlyRun, err := st.CreateRunWithAssociation(
			1,
			"Plan Only",
			"intake_received",
			"gpt-4o",
			"gpt-4o",
			store.DefaultExecutorAdapter,
			"main",
			sql.NullInt64{Int64: plan.ID, Valid: true},
			sql.NullInt64{},
		)
		if err != nil {
			t.Fatalf("create plan-only run: %v", err)
		}

		if err := svc.MarkAssociatedPassInProgress(standaloneRun); err != nil {
			t.Fatalf("standalone mark in progress: %v", err)
		}
		if err := svc.MarkAssociatedPassInProgress(planOnlyRun); err != nil {
			t.Fatalf("plan-only mark in progress: %v", err)
		}
		if err := svc.ApplyAuditDecision(standaloneRun, "accepted"); err != nil {
			t.Fatalf("standalone apply decision: %v", err)
		}
		if err := svc.ApplyAuditDecision(planOnlyRun, "revision_required"); err != nil {
			t.Fatalf("plan-only apply decision: %v", err)
		}

		updatedPass, err := st.GetPlanPass(pass.ID)
		if err != nil {
			t.Fatalf("get pass: %v", err)
		}
		if updatedPass.Status != "planned" {
			t.Fatalf("expected planned, got %q", updatedPass.Status)
		}
	})

	t.Run("completion ready requires all passes terminal and at least one pass", func(t *testing.T) {
		plan := submitLifecyclePlan(t, st, "plan-lifecycle-completion")

		ready, err := svc.CompletionReady(plan.ID)
		if err != nil {
			t.Fatalf("completion ready: %v", err)
		}
		if ready {
			t.Fatal("expected false while passes are planned")
		}

		firstPass, err := st.GetPlanPassByPassID(plan.ID, "PASS-001")
		if err != nil {
			t.Fatalf("get first pass: %v", err)
		}
		secondPass, err := st.GetPlanPassByPassID(plan.ID, "PASS-002")
		if err != nil {
			t.Fatalf("get second pass: %v", err)
		}

		if _, err := st.UpdatePlanPassStatus(firstPass.ID, "completed"); err != nil {
			t.Fatalf("update first pass: %v", err)
		}
		if _, err := st.UpdatePlanPassStatus(secondPass.ID, "skipped"); err != nil {
			t.Fatalf("update second pass: %v", err)
		}

		ready, err = svc.CompletionReady(plan.ID)
		if err != nil {
			t.Fatalf("completion ready: %v", err)
		}
		if !ready {
			t.Fatal("expected true when all passes are terminal")
		}

		ready, err = svc.CompletionReady(999999)
		if err != nil {
			t.Fatalf("completion ready empty: %v", err)
		}
		if ready {
			t.Fatal("expected false for plan with no passes")
		}
	})

	t.Run("unknown audit decisions return an error", func(t *testing.T) {
		run, _ := createLifecycleRunWithPass(t, st, "plan-lifecycle-invalid-decision", "PASS-001", "planned")
		if err := svc.ApplyAuditDecision(run, "unknown"); err == nil {
			t.Fatal("expected error for unknown decision")
		}
	})
}

func setupLifecycleTestService(t *testing.T) (*RunLifecycleService, *store.Store) {
	t.Helper()

	dir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	st, err := store.Open(filepath.Join(dir, "relay.db"), logger)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	if _, err := st.CreateRepo("test-repo", filepath.Join(dir, "repo")); err != nil {
		t.Fatalf("create repo: %v", err)
	}

	if _, err := st.CreateProject("test-project", "Test Project", "", "active", ""); err != nil {
		t.Fatalf("create project: %v", err)
	}

	return NewRunLifecycleService(st), st
}

func submitLifecyclePlan(t *testing.T, st *store.Store, planID string) *store.Plan {
	t.Helper()

	plan := PlannerPassPlan{
		PlanMeta: PlanMeta{
			PlanID:        planID,
			SchemaVersion: "2.0.0",
			CreatedAt:     "2026-06-21T00:00:00Z",
			Title:         "Lifecycle Test Plan",
			Goal:          "Exercise lifecycle transitions.",
			RepoTarget:    "test-repo",
			BranchContext: "main",
			Status:        "active",
			MCPCapabilityProfile: &MCPCapabilityProfile{
				ProfileID:            "relay-plan-tests",
				Mode:                 "submission_only",
				ContextBrokerEnabled: lifecycleBoolPtr(false),
			},
		},
		SourceIntent: SourceIntent{
			Summary: "Seed plan for lifecycle tests.",
		},
		GlobalContextRules: &GlobalContextRules{
			DefaultSourceOfTruth:   "Relay managed plan rows.",
			PlannerContextBoundary: "Lifecycle tests seed plans without broker tool exposure.",
			ForbiddenContextDomains: []string{
				"GitHub issues",
			},
		},
		Passes: []PlanPassInput{
			{
				PassID:                 "PASS-001",
				Sequence:               1,
				Name:                   "Pass One",
				Goal:                   "Exercise lifecycle transitions.",
				IntendedExecutionScope: []string{"internal/plans/lifecycle.go"},
				NonGoals:               []string{"No UI changes"},
				Dependencies:           []string{},
				Status:                 "planned",
				PassType:               "backend_vertical_slice",
				ContextPlan: ContextPlan{
					RequiredRepositories: []string{"relay"},
					SeedSearchTerms: []ContextSearchTerm{
						{
							RepoID:   "relay",
							Query:    "RunLifecycleService",
							Purpose:  "Locate lifecycle behavior.",
							Required: lifecycleBoolPtr(true),
						},
					},
					SeedFilesToRead: []ContextFileRead{
						{
							RepoID:   "relay",
							Path:     "internal/plans/lifecycle.go",
							Purpose:  "Exercise lifecycle transitions.",
							Required: lifecycleBoolPtr(true),
						},
					},
					ContextCoverageExpectations: []string{"Lifecycle status changes remain deterministic."},
					BlockedIfMissing:            []string{"Lifecycle code cannot be read."},
				},
				SourceSnapshotRequirements: SourceSnapshotRequirements{
					RequireGitStatus:   lifecycleBoolPtr(true),
					RequireCommitSHA:   lifecycleBoolPtr(false),
					AllowDirtyWorktree: lifecycleBoolPtr(true),
				},
				HandoffReadinessCriteria: []string{"Pass status transitions remain consistent."},
			},
			{
				PassID:                 "PASS-002",
				Sequence:               2,
				Name:                   "Pass Two",
				Goal:                   "Support completion readiness tests.",
				IntendedExecutionScope: []string{"internal/plans/lifecycle_test.go"},
				NonGoals:               []string{"No UI changes"},
				Dependencies:           []string{"PASS-001"},
				Status:                 "planned",
				PassType:               "testing_release_hardening",
				ContextPlan: ContextPlan{
					RequiredRepositories: []string{"relay"},
					SeedSearchTerms: []ContextSearchTerm{
						{
							RepoID:   "relay",
							Query:    "CompletionReady",
							Purpose:  "Verify completion logic.",
							Required: lifecycleBoolPtr(true),
						},
					},
					SeedFilesToRead: []ContextFileRead{
						{
							RepoID:   "relay",
							Path:     "internal/plans/lifecycle_test.go",
							Purpose:  "Drive completion readiness coverage.",
							Required: lifecycleBoolPtr(true),
						},
					},
					ContextCoverageExpectations: []string{"Completion readiness is true only when all passes are terminal."},
					BlockedIfMissing:            []string{"Lifecycle tests cannot inspect pass status."},
				},
				SourceSnapshotRequirements: SourceSnapshotRequirements{
					RequireGitStatus:   lifecycleBoolPtr(true),
					RequireCommitSHA:   lifecycleBoolPtr(false),
					AllowDirtyWorktree: lifecycleBoolPtr(true),
				},
				HandoffReadinessCriteria: []string{"Completion readiness logic can be verified with seeded plan rows."},
			},
		},
	}

	raw, err := json.Marshal(plan)
	if err != nil {
		t.Fatalf("marshal plan: %v", err)
	}

	result, err := NewService(st).SubmitPlan(context.Background(), SubmitPlanRequest{
		RawJSON:            raw,
		SourceArtifactPath: "handoffs/planner/lifecycle-test.json",
		ProjectID:          "test-project",
	})
	if err != nil {
		t.Fatalf("submit plan: %v", err)
	}
	if !result.Report.Valid {
		t.Fatalf("expected valid plan, got issues: %+v", result.Report.Issues)
	}

	createdPlan, err := st.GetPlanByPlanID(planID)
	if err != nil {
		t.Fatalf("get plan: %v", err)
	}

	return createdPlan
}

func createLifecycleRunWithPass(t *testing.T, st *store.Store, planID, passID, passStatus string) (*store.Run, *store.PlanPass) {
	t.Helper()

	plan := submitLifecyclePlan(t, st, planID)
	pass, err := st.GetPlanPassByPassID(plan.ID, passID)
	if err != nil {
		t.Fatalf("get pass: %v", err)
	}
	if _, err := st.UpdatePlanPassStatus(pass.ID, passStatus); err != nil {
		t.Fatalf("seed pass status: %v", err)
	}

	run, err := st.CreateRunWithAssociation(
		1,
		"Lifecycle Run",
		"intake_received",
		"gpt-4o",
		"gpt-4o",
		store.DefaultExecutorAdapter,
		"main",
		sql.NullInt64{Int64: plan.ID, Valid: true},
		sql.NullInt64{Int64: pass.ID, Valid: true},
	)
	if err != nil {
		t.Fatalf("create run: %v", err)
	}

	updatedPass, err := st.GetPlanPass(pass.ID)
	if err != nil {
		t.Fatalf("reload pass: %v", err)
	}

	return run, updatedPass
}

func lifecycleBoolPtr(value bool) *bool {
	return &value
}
