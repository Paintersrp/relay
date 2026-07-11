package workflow

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	workflowplans "relay/internal/app/plans/workflow"
	workflowstore "relay/internal/store/workflow"
)

func TestWorkflowReadModelsExposeCanonicalPlanAndNonNilCollections(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, err := workflowstore.Open(
		filepath.Join(root, "workflow.sqlite"),
		filepath.Join(root, "artifacts"),
	)
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
			ProjectID: "project-read-model-contract",
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
	created, err := plans.CreatePlan(ctx, workflowplans.CreatePlanInput{
		ProjectID:        project.ProjectID,
		FeatureSlug:      "relay-specification-workflow-pivot",
		CanonicalJSON:    []byte("{}\n"),
		RenderedMarkdown: []byte("# Plan\n"),
		Repositories: []workflowplans.RepositoryTargetInput{
			{
				RepoTarget:         "relay",
				Branch:             "main",
				PlanningBaseCommit: strings.Repeat("a", 40),
			},
		},
		Passes: []workflowplans.PassInput{
			{Number: 1, Name: "Pass", RepoTarget: "relay"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(created.Passes) != 1 {
		t.Fatalf("created passes = %d, want 1", len(created.Passes))
	}

	service, err := NewService(store)
	if err != nil {
		t.Fatal(err)
	}
	detail, err := service.GetPlan(ctx, created.Plan.PlanID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Plan.FeatureSlug != "relay-specification-workflow-pivot" {
		t.Fatalf("feature slug = %q", detail.Plan.FeatureSlug)
	}
	if strings.Contains(detail.Plan.FeatureSlug, "/") {
		t.Fatalf("feature slug is not canonical: %q", detail.Plan.FeatureSlug)
	}
	if len(detail.Passes) != 1 {
		t.Fatalf("Plan passes = %d, want 1", len(detail.Passes))
	}
	if detail.Passes[0].DependsOn == nil || len(detail.Passes[0].DependsOn) != 0 {
		t.Fatalf("Plan detail dependsOn = %#v, want non-nil empty collection", detail.Passes[0].DependsOn)
	}
	if detail.Passes[0].Runs == nil || len(detail.Passes[0].Runs) != 0 {
		t.Fatalf("Plan detail runs = %#v, want non-nil empty collection", detail.Passes[0].Runs)
	}

	pass, err := service.GetPlanPass(ctx, created.Plan.PlanID, created.Passes[0].PassID)
	if err != nil {
		t.Fatal(err)
	}
	if pass.DependsOn == nil || len(pass.DependsOn) != 0 {
		t.Fatalf("pass detail dependsOn = %#v, want non-nil empty collection", pass.DependsOn)
	}
	if pass.Runs == nil || len(pass.Runs) != 0 {
		t.Fatalf("pass detail runs = %#v, want non-nil empty collection", pass.Runs)
	}
}
