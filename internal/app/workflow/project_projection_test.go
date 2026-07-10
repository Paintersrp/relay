package workflow

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	workflowplans "relay/internal/app/plans/workflow"
	workflowruns "relay/internal/app/runs/workflow"
	workflowstore "relay/internal/store/workflow"
)

func TestWorkflowReadModelsResolveProjectWithoutGatingArchivedPlans(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, err := workflowstore.Open(filepath.Join(root, "workflow.sqlite"), filepath.Join(root, "artifacts"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
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
	plans, err := workflowplans.NewService(store)
	if err != nil {
		t.Fatal(err)
	}
	createdPlan, err := plans.CreatePlan(ctx, workflowplans.CreatePlanInput{
		ProjectID:        project.ProjectID,
		FeatureSlug:      "project-read-model",
		CanonicalJSON:    []byte("{}\n"),
		RenderedMarkdown: []byte("# Plan\n"),
		Repositories: []workflowplans.RepositoryTargetInput{
			workflowplans.RepositoryTargetInput{
				RepoTarget: "relay", Branch: "main", PlanningBaseCommit: strings.Repeat("a", 40),
			},
		},
		Passes: []workflowplans.PassInput{
			workflowplans.PassInput{Number: 1, Name: "Pass", RepoTarget: "relay"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	runs, err := workflowruns.NewService(store)
	if err != nil {
		t.Fatal(err)
	}
	createdRun, err := runs.CreateRun(ctx, workflowruns.CreateRunInput{
		FeatureSlug:      "project-read-model",
		RepoTarget:       "relay",
		Branch:           "main",
		BaseCommit:       strings.Repeat("a", 40),
		CanonicalJSON:    []byte("{}\n"),
		RenderedMarkdown: []byte("# Brief\n"),
		PlanID:           createdPlan.Plan.PlanID,
		PassNumber:       1,
	})
	if err != nil {
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
	planDetail, err := service.GetPlan(ctx, createdPlan.Plan.PlanID)
	if err != nil {
		t.Fatal(err)
	}
	if planDetail.Project.ProjectID != project.ProjectID || planDetail.Project.Status != workflowstore.ProjectStatusArchived {
		t.Fatalf("Plan Project = %+v", planDetail.Project)
	}
	runDetail, err := service.GetRun(ctx, createdRun.Run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if runDetail.Summary.Project == nil ||
		runDetail.Summary.Project.ProjectID != project.ProjectID ||
		runDetail.Summary.Project.Status != workflowstore.ProjectStatusArchived {
		t.Fatalf("Run Project = %+v", runDetail.Summary.Project)
	}
}
