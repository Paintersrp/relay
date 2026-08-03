package workflowplans

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	workflowstore "relay/internal/store/workflow"
	"relay/internal/testsupport/workflowfixture"
)

// Plan and Pass writes are retired. The service keeps exactly one read-only
// presentation operation for historical records.
func TestGetPlanProjectsHistoricalPlanWithoutWriteAdmission(t *testing.T) {
	ctx := context.Background()
	store, _ := openPlanTestStore(t)
	registerPlanTestRepo(t, ctx, store, "relay")
	projectID := createPlanTestProject(t, ctx, store)

	var plan workflowstore.Plan
	var pass workflowstore.PlanPass
	if err := store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		project, err := tx.GetProjectByProjectID(ctx, projectID)
		if err != nil {
			return err
		}
		plan, err = tx.CreatePlan(ctx, workflowstore.CreatePlanParams{
			ProjectRowID:    project.ID,
			PlanID:          "plan-historical",
			FeatureSlug:     "feature",
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
			PlanningBaseCommit: strings.Repeat("b", 40),
		}); err != nil {
			return err
		}
		pass, err = tx.CreatePlanPass(ctx, workflowstore.CreatePlanPassParams{
			PassID:     "pass-historical",
			PlanRowID:  plan.ID,
			PassNumber: 1,
			Name:       "Foundation",
			RepoTarget: "relay",
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}

	service, err := NewService(store)
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.GetPlan(ctx, plan.PlanID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Plan.PlanID != plan.PlanID || result.Project.ProjectID != projectID {
		t.Fatalf("plan projection = %+v / %+v", result.Plan, result.Project)
	}
	if len(result.Passes) != 1 || result.Passes[0].PassID != pass.PassID {
		t.Fatalf("pass projection = %+v", result.Passes)
	}
}

func TestGetPlanRejectsUnknownAndBlankPlanID(t *testing.T) {
	ctx := context.Background()
	store, _ := openPlanTestStore(t)
	service, err := NewService(store)
	if err != nil {
		t.Fatal(err)
	}
	for _, planID := range []string{"", "plan-missing"} {
		if _, err := service.GetPlan(ctx, planID); !errors.Is(err, ErrPlanNotFound) {
			t.Fatalf("GetPlan(%q) error = %v", planID, err)
		}
	}
}

func openPlanTestStore(t *testing.T) (*workflowstore.Store, string) {
	t.Helper()
	store := workflowfixture.Open(t, workflowstore.Open)
	return store, filepath.Dir(store.ArtifactStore().Root())
}

func createPlanTestProject(t *testing.T, ctx context.Context, store *workflowstore.Store) string {
	t.Helper()
	var project workflowstore.Project
	if err := store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		var err error
		project, err = tx.CreateProject(ctx, workflowstore.CreateProjectParams{
			ProjectID: "project-plan-tests",
			Name:      "Plan tests",
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return project.ProjectID
}

func registerPlanTestRepo(t *testing.T, ctx context.Context, store *workflowstore.Store, key string) {
	t.Helper()
	path := t.TempDir()
	if err := store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		_, err := tx.CreateRepositoryTarget(ctx, key, path)
		return err
	}); err != nil {
		t.Fatal(err)
	}
}
