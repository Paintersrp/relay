package workflow

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	workflowartifacts "relay/internal/artifacts/workflow"
	workflowstore "relay/internal/store/workflow"
)

func TestResolveRunStageUsesThreeStageWorkflow(t *testing.T) {
	tests := map[string]string{
		workflowstore.RunStatusCreated:          RunStageSpecification,
		workflowstore.RunStatusSetupReady:       RunStageSpecification,
		workflowstore.RunStatusExecuting:        RunStageExecute,
		workflowstore.RunStatusExecutionFailed:  RunStageExecute,
		workflowstore.RunStatusCancelled:        RunStageExecute,
		workflowstore.RunStatusValidating:       RunStageAudit,
		workflowstore.RunStatusValidationFailed: RunStageAudit,
		workflowstore.RunStatusAuditReady:       RunStageAudit,
		workflowstore.RunStatusNeedsRevision:    RunStageAudit,
		workflowstore.RunStatusCompleted:        RunStageAudit,
	}
	for status, expected := range tests {
		stage, err := ResolveRunStage(status)
		if err != nil || stage != expected {
			t.Fatalf("status %q => stage %q, error %v; want %q", status, stage, err, expected)
		}
	}
	if _, err := ResolveRunStage("legacy_status"); err == nil {
		t.Fatal("legacy status was routed")
	}
}

func TestWorkflowServiceReturnsBoundedSpecificationAndArtifactContent(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, err := workflowstore.Open(filepath.Join(root, "workflow.sqlite"), filepath.Join(root, "artifacts"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service, err := NewService(store)
	if err != nil {
		t.Fatal(err)
	}
	repoPath := filepath.Join(root, "repo")
	if err := os.MkdirAll(repoPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := service.RegisterRepository(ctx, "relay", repoPath); err != nil {
		t.Fatal(err)
	}

	batch, err := store.ArtifactStore().Begin("runs/run-read")
	if err != nil {
		t.Fatal(err)
	}
	specFile, err := batch.Stage("execution_spec", "read-test.execution-spec.json", "application/json", []byte(`{"feature_slug":"read-test"}`))
	if err != nil {
		t.Fatal(err)
	}
	briefFile, err := batch.Stage("executor_brief", "read-test.executor-brief.md", "text/markdown", []byte("# Brief\n"))
	if err != nil {
		t.Fatal(err)
	}
	var run workflowstore.Run
	if err := store.CommitArtifactBatch(ctx, batch, func(tx *workflowstore.Tx) error {
		var err error
		run, err = tx.CreateRun(ctx, workflowstore.CreateRunParams{
			RunID: "run-read", FeatureSlug: "read-test", RepoTarget: "relay",
			Status: workflowstore.RunStatusCreated, Branch: "main",
			BaseCommit: strings.Repeat("a", 40), CanonicalSHA256: specFile.SHA256,
		})
		if err != nil {
			return err
		}
		run, err = tx.TransitionRun(ctx, run.RunID, workflowstore.RunStatusCreated, workflowstore.RunStatusSetupReady)
		if err != nil {
			return err
		}
		for _, staged := range []workflowartifacts.File{specFile, briefFile} {
			if _, err := tx.CreateArtifact(ctx, workflowstore.CreateArtifactParams{
				ArtifactID: "artifact-" + staged.Kind,
				OwnerType:  workflowstore.ArtifactOwnerRun,
				RunRowID:   sql.NullInt64{Int64: run.ID, Valid: true},
				Kind:       staged.Kind, RelativePath: staged.RelativePath, MediaType: staged.MediaType,
				SHA256: staged.SHA256, SizeBytes: staged.SizeBytes,
			}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	review, err := service.GetSpecification(ctx, run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if review.Run.Stage != RunStageSpecification ||
		review.ExecutionSpec.ArtifactID != "artifact-execution_spec" ||
		review.ExecutorBrief.ArtifactID != "artifact-executor_brief" {
		t.Fatalf("review = %+v", review)
	}
	content, err := service.GetArtifactContent(ctx, ArtifactContentInput{
		ArtifactID: review.ExecutorBrief.ArtifactID,
		Limit:      4,
	})
	if err != nil {
		t.Fatal(err)
	}
	if string(content.Bytes) != "# Br" || !content.Truncated || !content.HasNext || content.NextOffset != 4 {
		t.Fatalf("content = %+v", content)
	}
	if content.Artifact.OwnerType != workflowstore.ArtifactOwnerRun {
		t.Fatalf("artifact metadata = %+v", content.Artifact)
	}
}
