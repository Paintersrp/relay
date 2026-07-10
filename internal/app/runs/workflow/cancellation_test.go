package workflowruns

import (
	"context"
	"strings"
	"testing"

	workflowstore "relay/internal/store/workflow"
)

func TestRequestExecutionAttemptCancellationRequiresMatchingRunBeforeMutation(t *testing.T) {
	ctx := context.Background()
	store, _ := openRunTestStore(t)
	registerRunTestRepo(t, ctx, store, "relay")
	service, err := NewService(store)
	if err != nil {
		t.Fatal(err)
	}
	create := func(feature string) workflowstore.Run {
		result, err := service.CreateRun(ctx, CreateRunInput{
			FeatureSlug:      feature,
			RepoTarget:       "relay",
			Branch:           "main",
			BaseCommit:       strings.Repeat("a", 40),
			CanonicalJSON:    []byte("{}\n"),
			RenderedMarkdown: []byte("# Brief\n"),
		})
		if err != nil {
			t.Fatal(err)
		}
		return result.Run
	}
	runA := create("run-a")
	runB := create("run-b")
	begun, err := service.BeginExecutionAttempt(ctx, BeginExecutionAttemptInput{
		RunID:   runB.RunID,
		Adapter: "codex",
		Model:   "test-model",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.MarkExecutionAttemptRunning(ctx, begun.Attempt.AttemptID, `{}`); err != nil {
		t.Fatal(err)
	}

	if _, err := service.RequestExecutionAttemptCancellation(ctx, runA.RunID, begun.Attempt.AttemptID); err == nil {
		t.Fatal("cross-Run cancellation was accepted")
	}
	after, err := store.GetExecutionAttemptByAttemptID(ctx, begun.Attempt.AttemptID)
	if err != nil {
		t.Fatal(err)
	}
	if after.CancellationRequestedAt.Valid {
		t.Fatal("cross-Run cancellation mutated the execution attempt")
	}
	finished, err := service.FinishExecutionAttempt(ctx, FinishExecutionAttemptInput{
		AttemptID:  begun.Attempt.AttemptID,
		Status:     workflowstore.AttemptStatusSucceeded,
		ResultJSON: `{"ok":true}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if finished.Attempt.Status != workflowstore.AttemptStatusSucceeded || finished.Run.Status != workflowstore.RunStatusValidating {
		t.Fatalf("unexpected terminal state: %+v", finished)
	}
}
