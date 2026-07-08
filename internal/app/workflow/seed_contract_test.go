package workflow

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	workflowstore "relay/internal/store/workflow"
)

func TestWorkflowSeedProducesLoadableCanonicalPlanAndPassDetails(t *testing.T) {
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

	seedPath := filepath.Join("..", "..", "..", "scripts", "seed-workflow-db.sql")
	seed, err := os.ReadFile(seedPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().ExecContext(ctx, string(seed)); err != nil {
		t.Fatalf("apply workflow seed: %v", err)
	}

	service, err := NewService(store)
	if err != nil {
		t.Fatal(err)
	}
	const planID = "plan-00000000-0000-0000-0000-000000000001"
	const passID = "pass-00000000-0000-0000-0001-000000000001"

	detail, err := service.GetPlan(ctx, planID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Plan.FeatureSlug != "relay-specification-workflow-pivot" {
		t.Fatalf("feature slug = %q", detail.Plan.FeatureSlug)
	}
	if strings.Contains(detail.Plan.FeatureSlug, "/") {
		t.Fatalf("feature slug is not canonical: %q", detail.Plan.FeatureSlug)
	}
	if len(detail.Passes) == 0 {
		t.Fatal("seeded Plan has no passes")
	}
	if detail.Passes[0].DependsOn == nil {
		t.Fatal("seeded Plan detail exposed nil dependsOn instead of an empty collection")
	}
	if detail.Passes[0].Runs == nil {
		t.Fatal("seeded Plan detail exposed nil runs instead of an empty collection")
	}

	pass, err := service.GetPlanPass(ctx, planID, passID)
	if err != nil {
		t.Fatal(err)
	}
	if pass.DependsOn == nil {
		t.Fatal("seeded pass detail exposed nil dependsOn instead of an empty collection")
	}
	if pass.Runs == nil {
		t.Fatal("seeded pass detail exposed nil runs instead of an empty collection")
	}
}