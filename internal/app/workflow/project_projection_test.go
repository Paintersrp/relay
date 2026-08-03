package workflow

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	workflowstore "relay/internal/store/workflow"
)

func TestWorkflowReadModelsResolveProjectWithoutGatingArchivedPlans(t *testing.T) {
	ctx := context.Background()
	store, root := openApplicationWorkflowStore(t)
	repositoryPath := filepath.Join(root, "repo")
	if err := os.MkdirAll(repositoryPath, 0o755); err != nil {
		t.Fatal(err)
	}
	var project workflowstore.Project
	if err := store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		if _, err := tx.CreateRepositoryTarget(ctx, "relay", repositoryPath); err != nil {
			return err
		}
		var err error
		project, err = tx.CreateProject(ctx, workflowstore.CreateProjectParams{
			ProjectID: "project-read-model",
			Name:      "Relay",
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	createdPlan, createdPass := seedHistoricalPlan(t, ctx, store, project.ID, "plan-read-model", "project-read-model")
	var createdRun workflowstore.Run
	if err := store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		var err error
		createdRun, err = tx.CreateRun(ctx, workflowstore.CreateRunParams{
			RunID: "run-project-read-model", FeatureSlug: "project-read-model", RepoTarget: "relay",
			Status: workflowstore.RunStatusCreated, Branch: "main", BaseCommit: strings.Repeat("a", 40),
			PlanRowID: sql.NullInt64{Int64: createdPlan.ID, Valid: true}, PlanPassRowID: sql.NullInt64{Int64: createdPass.ID, Valid: true},
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		_, err := tx.TransitionProjectStatus(ctx, project.ProjectID, workflowstore.ProjectStatusActive, workflowstore.ProjectStatusArchived)
		return err
	}); err != nil {
		t.Fatal(err)
	}

	service, err := NewService(store)
	if err != nil {
		t.Fatal(err)
	}
	planDetail, err := service.GetPlan(ctx, createdPlan.PlanID)
	if err != nil {
		t.Fatal(err)
	}
	if planDetail.Project.ProjectID != project.ProjectID || planDetail.Project.Status != workflowstore.ProjectStatusArchived {
		t.Fatalf("Plan Project = %+v", planDetail.Project)
	}
	runDetail, err := service.GetRun(ctx, createdRun.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if runDetail.Summary.Project == nil ||
		runDetail.Summary.Project.ProjectID != project.ProjectID ||
		runDetail.Summary.Project.Status != workflowstore.ProjectStatusArchived {
		t.Fatalf("Run Project = %+v", runDetail.Summary.Project)
	}
}

// seedHistoricalPlan writes one historical Plan, repository target, and Pass
// directly through the store. Legacy Plan write admission is retired, so no
// application service creates Plans.
func seedHistoricalPlan(t *testing.T, ctx context.Context, store *workflowstore.Store, projectRowID int64, planID, featureSlug string) (workflowstore.Plan, workflowstore.PlanPass) {
	t.Helper()
	var plan workflowstore.Plan
	var pass workflowstore.PlanPass
	if err := store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		var err error
		plan, err = tx.CreatePlan(ctx, workflowstore.CreatePlanParams{
			ProjectRowID:    projectRowID,
			PlanID:          planID,
			FeatureSlug:     featureSlug,
			CanonicalSHA256: strings.Repeat("a", 64),
		})
		if err != nil {
			return err
		}
		if _, err := tx.CreatePlanRepositoryTarget(ctx, workflowstore.CreatePlanRepositoryTargetParams{
			PlanRowID:          plan.ID,
			Sequence:           1,
			RepoTarget:         "relay",
			Branch:             "main",
			PlanningBaseCommit: strings.Repeat("a", 40),
		}); err != nil {
			return err
		}
		pass, err = tx.CreatePlanPass(ctx, workflowstore.CreatePlanPassParams{
			PassID:     "pass-" + planID,
			PlanRowID:  plan.ID,
			PassNumber: 1,
			Name:       "Pass",
			RepoTarget: "relay",
		})
		if err != nil {
			return err
		}
		pass, err = tx.TransitionPlanPass(ctx, pass.PassID, workflowstore.PassStatusPlanned, workflowstore.PassStatusInProgress)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return plan, pass
}
