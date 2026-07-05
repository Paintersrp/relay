package workflowplans

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	workflowstore "relay/internal/store/workflow"
)

type sequenceIDs struct {
	planID        string
	passIDs       []string
	artifactBase  string
	passIndex     int
	artifactIndex int
}

func (ids *sequenceIDs) PlanID() string {
	return ids.planID
}

func (ids *sequenceIDs) PassID() string {
	value := ids.passIDs[ids.passIndex]
	ids.passIndex++
	return value
}

func (ids *sequenceIDs) ArtifactID() string {
	ids.artifactIndex++
	return fmt.Sprintf("%s-%d", ids.artifactBase, ids.artifactIndex)
}

func TestCreatePlanPersistsCanonicalArtifactsAndDependencies(t *testing.T) {
	ctx := context.Background()
	store, root := openPlanTestStore(t)
	registerPlanTestRepo(t, ctx, store, "relay")
	service, err := NewServiceWithIDs(store, &sequenceIDs{
		planID:       "plan-test",
		passIDs:      []string{"pass-one", "pass-two"},
		artifactBase: "artifact-plan",
	})
	if err != nil {
		t.Fatal(err)
	}

	canonical := []byte("{\"feature_slug\":\"feature\"}\n")
	rendered := []byte("# Plan of Passes\n")
	result, err := service.CreatePlan(ctx, CreatePlanInput{
		FeatureSlug:      "feature",
		CanonicalJSON:    canonical,
		RenderedMarkdown: rendered,
		Repositories: []RepositoryTargetInput{
			{
				RepoTarget:         "relay",
				Branch:             "feat/simplification",
				PlanningBaseCommit: strings.Repeat("a", 40),
			}},
		Passes: []PassInput{
			{Number: 1, Name: "Foundation", RepoTarget: "relay"},
			{Number: 2, Name: "Integration", RepoTarget: "relay", DependsOn: []int64{1}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Plan.PlanID != "plan-test" || result.Plan.Status != workflowstore.PlanStatusActive {
		t.Fatalf("unexpected plan: %+v", result.Plan)
	}
	if len(result.Passes) != 2 || result.Passes[0].Status != workflowstore.PassStatusPlanned || result.Passes[1].Status != workflowstore.PassStatusPlanned {
		t.Fatalf("unexpected passes: %+v", result.Passes)
	}
	if len(result.Artifacts) != 2 {
		t.Fatalf("unexpected artifacts: %+v", result.Artifacts)
	}
	for _, expected := range []struct {
		path string
		data []byte
	}{
		{path: filepath.Join(root, "artifacts", "plans", "plan-test", "feature.plan.json"), data: canonical},
		{path: filepath.Join(root, "artifacts", "plans", "plan-test", "feature.plan.md"), data: rendered},
	} {
		data, err := os.ReadFile(expected.path)
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != string(expected.data) {
			t.Fatalf("artifact %s changed: got %q want %q", expected.path, data, expected.data)
		}
	}

	var dependencyCount int64
	if err := store.DB().QueryRow(`
SELECT COUNT(*)
FROM plan_pass_dependencies
WHERE pass_row_id = ? AND depends_on_pass_row_id = ?`,
		result.Passes[1].ID,
		result.Passes[0].ID,
	).Scan(&dependencyCount); err != nil {
		t.Fatal(err)
	}
	if dependencyCount != 1 {
		t.Fatalf("dependency count = %d, want 1", dependencyCount)
	}
}

func TestCreatePlanDatabaseFailureLeavesNoRecordsOrArtifacts(t *testing.T) {
	ctx := context.Background()
	store, root := openPlanTestStore(t)
	registerPlanTestRepo(t, ctx, store, "relay")
	service, err := NewServiceWithIDs(store, &sequenceIDs{
		planID:       "plan-rollback",
		passIDs:      []string{"duplicate-pass", "duplicate-pass"},
		artifactBase: "artifact-rollback",
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = service.CreatePlan(ctx, CreatePlanInput{
		FeatureSlug:      "feature",
		CanonicalJSON:    []byte("{}\n"),
		RenderedMarkdown: []byte("# Plan\n"),
		Repositories: []RepositoryTargetInput{
			{
				RepoTarget:         "relay",
				Branch:             "main",
				PlanningBaseCommit: strings.Repeat("a", 40),
			}},
		Passes: []PassInput{
			{Number: 1, Name: "One", RepoTarget: "relay"},
			{Number: 2, Name: "Two", RepoTarget: "relay"},
		},
	})
	if err == nil {
		t.Fatal("expected duplicate generated pass ID to fail")
	}
	assertTableCount(t, store.DB(), "plans", 0)
	assertNoRegularFiles(t, filepath.Join(root, "artifacts"))
}

func TestCreatePlanPromotionFailureRollsBackDatabase(t *testing.T) {
	ctx := context.Background()
	store, root := openPlanTestStore(t)
	registerPlanTestRepo(t, ctx, store, "relay")
	service, err := NewServiceWithIDs(store, &sequenceIDs{
		planID:       "plan-promotion-failure",
		passIDs:      []string{"pass-one"},
		artifactBase: "artifact-promotion",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "artifacts", "plans"), []byte("block directory"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err = service.CreatePlan(ctx, CreatePlanInput{
		FeatureSlug:      "feature",
		CanonicalJSON:    []byte("{}\n"),
		RenderedMarkdown: []byte("# Plan\n"),
		Repositories: []RepositoryTargetInput{
			{
				RepoTarget:         "relay",
				Branch:             "main",
				PlanningBaseCommit: strings.Repeat("a", 40),
			}},
		Passes: []PassInput{
			{Number: 1, Name: "One", RepoTarget: "relay"},
		},
	})
	if err == nil {
		t.Fatal("expected artifact promotion failure")
	}
	assertTableCount(t, store.DB(), "plans", 0)
}

func openPlanTestStore(t *testing.T) (*workflowstore.Store, string) {
	t.Helper()
	root := t.TempDir()
	store, err := workflowstore.Open(filepath.Join(root, "workflow.sqlite"), filepath.Join(root, "artifacts"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store, root
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

func assertTableCount(t *testing.T, db *sql.DB, table string, want int64) {
	t.Helper()
	var got int64
	if err := db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("table %s count = %d, want %d", table, got, want)
	}
}

func assertNoRegularFiles(t *testing.T, root string) {
	t.Helper()
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return err
		}
		if entry.Type().IsRegular() {
			return fmt.Errorf("unexpected durable file %s", path)
		}
		return nil
	})
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
}
