package workflowstore

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkflowReadQueriesAreBoundedAndOrdered(t *testing.T) {
	ctx := context.Background()
	store, _ := openWorkflowTestStore(t)

	var plan Plan
	var firstPass PlanPass
	var secondPass PlanPass
	if err := store.WithTx(ctx, func(tx *Tx) error {
		if _, err := tx.CreateRepositoryTarget(ctx, "relay", filepath.Join(t.TempDir(), "relay")); err != nil {
			return err
		}
		project, err := tx.CreateProject(ctx, CreateProjectParams{
			ProjectID: "project-read",
			Name:      "Read tests",
		})
		if err != nil {
			return err
		}
		plan, err = tx.CreatePlan(ctx, CreatePlanParams{
			ProjectRowID:    project.ID,
			PlanID:          "plan-read",
			FeatureSlug:     "read-test",
			CanonicalSHA256: strings.Repeat("a", 64),
		})
		if err != nil {
			return err
		}
		if _, err := tx.CreatePlanRepositoryTarget(ctx, CreatePlanRepositoryTargetParams{
			PlanRowID:          plan.ID,
			Sequence:           1,
			RepoTarget:         "relay",
			Branch:             "main",
			PlanningBaseCommit: strings.Repeat("b", 40),
		}); err != nil {
			return err
		}
		firstPass, err = tx.CreatePlanPass(ctx, CreatePlanPassParams{
			PassID: "pass-one", PlanRowID: plan.ID, PassNumber: 1, Name: "One", RepoTarget: "relay",
		})
		if err != nil {
			return err
		}
		secondPass, err = tx.CreatePlanPass(ctx, CreatePlanPassParams{
			PassID: "pass-two", PlanRowID: plan.ID, PassNumber: 2, Name: "Two", RepoTarget: "relay",
		})
		if err != nil {
			return err
		}
		if err := tx.CreatePlanPassDependency(ctx, secondPass.ID, firstPass.ID); err != nil {
			return err
		}
		firstPass, err = tx.TransitionPlanPass(ctx, firstPass.PassID, PassStatusPlanned, PassStatusInProgress)
		if err != nil {
			return err
		}
		for index, status := range []string{RunStatusSetupReady, RunStatusExecuting} {
			run, err := tx.CreateRun(ctx, CreateRunParams{
				RunID:           "run-" + string(rune('a'+index)),
				FeatureSlug:     "read-test",
				RepoTarget:      "relay",
				PlanRowID:       sql.NullInt64{Int64: plan.ID, Valid: true},
				PlanPassRowID:   sql.NullInt64{Int64: firstPass.ID, Valid: true},
				Status:          RunStatusCreated,
				Branch:          "main",
				BaseCommit:      strings.Repeat("c", 40),
				CanonicalSHA256: strings.Repeat("d", 64),
			})
			if err != nil {
				return err
			}
			run, err = tx.TransitionRun(ctx, run.RunID, RunStatusCreated, RunStatusSetupReady)
			if err != nil {
				return err
			}
			if status == RunStatusExecuting {
				if _, err := tx.TransitionRun(ctx, run.RunID, RunStatusSetupReady, RunStatusExecuting); err != nil {
					return err
				}
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	repositories, err := store.ListRepositoryTargets(ctx)
	if err != nil || len(repositories) != 1 || repositories[0].RepoTarget != "relay" {
		t.Fatalf("repositories = %+v, error = %v", repositories, err)
	}
	plans, err := store.ListPlans(ctx, PlanListQuery{Status: PlanStatusActive, Limit: 1})
	if err != nil || len(plans) != 1 || plans[0].PlanID != plan.PlanID {
		t.Fatalf("plans = %+v, error = %v", plans, err)
	}
	projectPlans, err := store.ListPlans(ctx, PlanListQuery{
		ProjectRowID: sql.NullInt64{Int64: plan.ProjectRowID, Valid: true},
		Limit:        1,
	})
	if err != nil || len(projectPlans) != 1 || projectPlans[0].ProjectRowID != plan.ProjectRowID {
		t.Fatalf("project plans = %+v, error = %v", projectPlans, err)
	}
	targets, err := store.ListPlanRepositoryTargets(ctx, plan.ID)
	if err != nil || len(targets) != 1 || targets[0].Sequence != 1 {
		t.Fatalf("targets = %+v, error = %v", targets, err)
	}
	dependencies, err := store.ListPlanPassDependencies(ctx, plan.ID)
	if err != nil || len(dependencies) != 1 ||
		dependencies[0].PassRowID != secondPass.ID ||
		dependencies[0].DependsOnPassRowID != firstPass.ID {
		t.Fatalf("dependencies = %+v, error = %v", dependencies, err)
	}
	runs, err := store.ListRuns(ctx, RunListQuery{
		Status:        RunStatusExecuting,
		PlanRowID:     sql.NullInt64{Int64: plan.ID, Valid: true},
		PlanPassRowID: sql.NullInt64{Int64: firstPass.ID, Valid: true},
		Limit:         1,
	})
	if err != nil || len(runs) != 1 || runs[0].Status != RunStatusExecuting {
		t.Fatalf("runs = %+v, error = %v", runs, err)
	}
}

func TestListRecentExecutionAttemptsByRunUsesLatestBound(t *testing.T) {
	ctx := context.Background()
	store, _ := openWorkflowTestStore(t)
	var run Run
	if err := store.WithTx(ctx, func(tx *Tx) error {
		if _, err := tx.CreateRepositoryTarget(ctx, "relay", filepath.Join(t.TempDir(), "relay")); err != nil {
			return err
		}
		var err error
		run, err = tx.CreateRun(ctx, CreateRunParams{
			RunID: "run-attempts", FeatureSlug: "attempt-test", RepoTarget: "relay",
			Status: RunStatusCreated, Branch: "main",
			BaseCommit: strings.Repeat("a", 40), CanonicalSHA256: strings.Repeat("b", 64),
		})
		if err != nil {
			return err
		}
		run, err = tx.TransitionRun(ctx, run.RunID, RunStatusCreated, RunStatusSetupReady)
		if err != nil {
			return err
		}
		run, err = tx.TransitionRun(ctx, run.RunID, RunStatusSetupReady, RunStatusExecuting)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.WithTx(ctx, func(tx *Tx) error {
		for number := int64(1); number <= 3; number++ {
			attempt, err := tx.CreateExecutionAttempt(ctx, CreateExecutionAttemptParams{
				AttemptID: "attempt-" + string(rune('0'+number)),
				RunRowID:  run.ID, AttemptNumber: number, Adapter: "codex", Model: "model",
			})
			if err != nil {
				return err
			}
			attempt, err = tx.TransitionExecutionAttempt(ctx, attempt.AttemptID, AttemptStatusPending, AttemptStatusRunning, `{}`)
			if err != nil {
				return err
			}
			if _, err := tx.TransitionExecutionAttempt(ctx, attempt.AttemptID, AttemptStatusRunning, AttemptStatusSucceeded, `{}`); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	attempts, err := store.ListRecentExecutionAttemptsByRun(ctx, run.ID, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(attempts) != 2 || attempts[0].AttemptNumber != 2 || attempts[1].AttemptNumber != 3 {
		t.Fatalf("attempts = %+v", attempts)
	}
}
