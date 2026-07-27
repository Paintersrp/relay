package executor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	workflowstore "relay/internal/store/workflow"
)

func TestPrepareAdaptiveExecutionAttemptModes(t *testing.T) {
	for _, test := range []struct {
		name       string
		operations bool
		coverage   string
		outcome    func() DeterministicOutcomeInput
		mode       EffectiveExecutorBriefMode
		adaptive   bool
	}{
		{name: "operations absent", outcome: func() DeterministicOutcomeInput {
			return DeterministicOutcomeInput{Preflight: DeterministicPreflightResult{Status: DeterministicPreflightNotPresent}}
		}, mode: EffectiveExecutorBriefAdaptiveNoOperations, adaptive: true},
		{name: "partial preflight failure", operations: true, coverage: "partial", outcome: func() DeterministicOutcomeInput { return failedOutcomeInput("partial") }, mode: EffectiveExecutorBriefAdaptivePreflightFailed, adaptive: true},
		{name: "complete preflight failure", operations: true, coverage: "complete", outcome: func() DeterministicOutcomeInput { return failedOutcomeInput("complete") }, mode: EffectiveExecutorBriefAdaptivePreflightFailed, adaptive: true},
		{name: "partial application", operations: true, coverage: "partial", outcome: func() DeterministicOutcomeInput { return appliedOutcomeInput("partial") }, mode: EffectiveExecutorBriefAdaptiveAfterPartialApplication, adaptive: true},
		{name: "complete application", operations: true, coverage: "complete", outcome: func() DeterministicOutcomeInput { return appliedOutcomeInput("complete") }, mode: EffectiveExecutorBriefDeterministicComplete, adaptive: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newExecutionAssignmentFixture(t, test.operations, test.coverage)
			prepareExecutionAssignment(t, fixture)
			input := test.outcome()
			input.RunID = fixture.run.RunID
			if _, err := NewDeterministicOutcomeService(fixture.store, fixture.sourceVaultReader); err != nil {
				t.Fatal(err)
			}
			persistOutcome(t, fixture, input)
			service := newAdaptiveAttemptService(t, fixture)
			result, err := service.Prepare(context.Background(), AdaptiveExecutionAttemptInput{RunID: fixture.run.RunID, Adapter: "CODEX_CLI", Model: "  gpt-test  "})
			if err != nil {
				t.Fatal(err)
			}
			if result.Mode != test.mode || result.AdaptiveDispatchRequired != test.adaptive {
				t.Fatalf("result = %#v", result)
			}
			attempts, err := fixture.store.ListExecutionAttemptsByRun(context.Background(), fixture.run.ID)
			if err != nil {
				t.Fatal(err)
			}
			if !test.adaptive {
				if result.Attempt != nil || result.InputArtifact != nil || len(result.InputBytes) != 0 || len(attempts) != 0 {
					t.Fatalf("complete result = %#v attempts=%#v", result, attempts)
				}
				return
			}
			if result.Attempt == nil || result.InputArtifact == nil || result.Attempt.AttemptNumber != 1 || result.Attempt.Status != workflowstore.AttemptStatusPending || result.Attempt.Adapter != "codex" || result.Attempt.Model != "gpt-test" || len(attempts) != 1 {
				t.Fatalf("adaptive result = %#v attempts=%#v", result, attempts)
			}
			if result.InputArtifact.Kind != adaptiveExecutionInputKind || result.InputArtifact.OwnerType != workflowstore.ArtifactOwnerExecutionAttempt || result.InputArtifact.MediaType != adaptiveExecutionInputMediaType {
				t.Fatalf("input artifact = %#v", result.InputArtifact)
			}
			if !strings.HasSuffix(string(result.InputBytes), "\n") || strings.HasSuffix(string(result.InputBytes), "\n\n") || bytes.Contains(result.InputBytes, []byte(testfixturesBrief(t))) {
				t.Fatalf("unexpected input bytes: %q", result.InputBytes)
			}
			var decoded adaptiveExecutionInputDocument
			if err := json.Unmarshal(result.InputBytes, &decoded); err != nil {
				t.Fatal(err)
			}
			if decoded.EffectiveExecutorBrief.ArtifactID == "" || decoded.ExecutionAttempt.AttemptID != result.Attempt.AttemptID || decoded.Executor.Adapter != "codex" {
				t.Fatalf("input document = %#v", decoded)
			}
		})
	}
}

func TestAdaptiveExecutionAttemptCompletePreparationAndReadbackOwnsNoAttemptOrInput(t *testing.T) {
	fixture := adaptiveAttemptFixture(t, true, "complete", appliedOutcomeInput("complete"))
	service := newAdaptiveAttemptService(t, fixture)
	prepared, err := service.Prepare(context.Background(), AdaptiveExecutionAttemptInput{RunID: fixture.run.RunID, Adapter: "codex", Model: "model"})
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Mode != EffectiveExecutorBriefDeterministicComplete || prepared.AdaptiveDispatchRequired || prepared.Attempt != nil || prepared.InputArtifact != nil || len(prepared.InputBytes) != 0 {
		t.Fatalf("complete prepared result = %#v", prepared)
	}
	artifacts, err := fixture.store.ListArtifactsByRun(context.Background(), fixture.run.ID)
	if err != nil {
		t.Fatal(err)
	}
	var effectiveBriefs int
	for _, artifact := range artifacts {
		if artifact.Kind == effectiveExecutorBriefKind && artifact.OwnerType == workflowstore.ArtifactOwnerRun {
			effectiveBriefs++
		}
		if artifact.Kind == adaptiveExecutionInputKind {
			t.Fatalf("complete run owns adaptive input artifact: %#v", artifact)
		}
	}
	if effectiveBriefs != 1 {
		t.Fatalf("effective brief artifacts = %d", effectiveBriefs)
	}
	attempts, err := fixture.store.ListExecutionAttemptsByRun(context.Background(), fixture.run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(attempts) != 0 {
		t.Fatalf("complete attempts = %#v", attempts)
	}

	loaded, err := service.Load(context.Background(), fixture.run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Mode != EffectiveExecutorBriefDeterministicComplete || loaded.AdaptiveDispatchRequired || loaded.Attempt != nil || loaded.InputArtifact != nil || len(loaded.InputBytes) != 0 {
		t.Fatalf("complete loaded result = %#v", loaded)
	}
}

func TestPrepareAdaptiveExecutionAttemptIdempotencyAndConflicts(t *testing.T) {
	fixture := adaptiveAttemptFixture(t, false, "", DeterministicOutcomeInput{Preflight: DeterministicPreflightResult{Status: DeterministicPreflightNotPresent}})
	service := newAdaptiveAttemptService(t, fixture)
	first, err := service.Prepare(context.Background(), AdaptiveExecutionAttemptInput{RunID: fixture.run.RunID, Adapter: "codex_cli", Model: "model-a"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.Prepare(context.Background(), AdaptiveExecutionAttemptInput{RunID: fixture.run.RunID, Adapter: "codex", Model: "model-a"})
	if err != nil {
		t.Fatal(err)
	}
	if first.Attempt.ID != second.Attempt.ID || first.InputArtifact.ID != second.InputArtifact.ID || !bytes.Equal(first.InputBytes, second.InputBytes) {
		t.Fatalf("repeated preparation differs: %#v %#v", first, second)
	}
	first.InputBytes[0] = '!'
	if second.InputBytes[0] == '!' {
		t.Fatal("result bytes are not defensive copies")
	}
	for _, changed := range []AdaptiveExecutionAttemptInput{{RunID: fixture.run.RunID, Adapter: "kiro", Model: "model-a"}, {RunID: fixture.run.RunID, Adapter: "codex", Model: "model-b"}} {
		if _, err := service.Prepare(context.Background(), changed); !errors.Is(err, ErrAdaptiveExecutionAttemptConflict) {
			t.Fatalf("conflicting selection error = %v", err)
		}
	}
	if _, err := service.Prepare(context.Background(), AdaptiveExecutionAttemptInput{RunID: fixture.run.RunID, Adapter: "unknown", Model: "model-a"}); err == nil {
		t.Fatal("unknown adapter succeeded")
	}
	if _, err := service.Prepare(context.Background(), AdaptiveExecutionAttemptInput{RunID: fixture.run.RunID, Adapter: "codex", Model: " \t "}); err == nil {
		t.Fatal("blank model succeeded")
	}
	path := filepath.Join(fixture.store.ArtifactStore().Root(), filepath.FromSlash(first.InputArtifact.RelativePath))
	if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Prepare(context.Background(), AdaptiveExecutionAttemptInput{RunID: fixture.run.RunID, Adapter: "codex", Model: "model-a"}); !errors.Is(err, ErrAdaptiveExecutionAttemptConflict) {
		t.Fatalf("tampered input error = %v", err)
	}
}

func TestPrepareAdaptiveExecutionAttemptCompleteConflictAndConcurrentPreparation(t *testing.T) {
	complete := adaptiveAttemptFixture(t, true, "complete", appliedOutcomeInput("complete"))
	if err := complete.store.WithTx(context.Background(), func(tx *workflowstore.Tx) error {
		_, err := tx.CreateExecutionAttempt(context.Background(), workflowstore.CreateExecutionAttemptParams{AttemptID: workflowstore.NewExecutionAttemptID(), RunRowID: complete.run.ID, AttemptNumber: 1, Adapter: "codex", Model: "model"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	service := newAdaptiveAttemptService(t, complete)
	if _, err := service.Prepare(context.Background(), AdaptiveExecutionAttemptInput{RunID: complete.run.RunID}); !errors.Is(err, ErrAdaptiveExecutionAttemptConflict) {
		t.Fatalf("complete conflict error = %v", err)
	}

	fixture := adaptiveAttemptFixture(t, false, "", DeterministicOutcomeInput{Preflight: DeterministicPreflightResult{Status: DeterministicPreflightNotPresent}})
	parallel := newAdaptiveAttemptService(t, fixture)
	start := make(chan struct{})
	results := make(chan AdaptiveExecutionAttemptResult, 2)
	errs := make(chan error, 2)
	var group sync.WaitGroup
	for range 2 {
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			result, err := parallel.Prepare(context.Background(), AdaptiveExecutionAttemptInput{RunID: fixture.run.RunID, Adapter: "codex", Model: "model"})
			results <- result
			errs <- err
		}()
	}
	close(start)
	group.Wait()
	close(results)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var attemptID string
	for result := range results {
		if result.Attempt == nil {
			t.Fatalf("missing attempt: %#v", result)
		}
		if attemptID != "" && result.Attempt.AttemptID != attemptID {
			t.Fatalf("concurrent attempt identities differ: %q %q", attemptID, result.Attempt.AttemptID)
		}
		attemptID = result.Attempt.AttemptID
	}
	attempts, err := fixture.store.ListExecutionAttemptsByRun(context.Background(), fixture.run.ID)
	if err != nil || len(attempts) != 1 {
		t.Fatalf("attempts = %#v err=%v", attempts, err)
	}
}

func TestPrepareAdaptiveExecutionAttemptDatabaseGuardsPackageRunsOnly(t *testing.T) {
	fixture := adaptiveAttemptFixture(t, false, "", DeterministicOutcomeInput{Preflight: DeterministicPreflightResult{Status: DeterministicPreflightNotPresent}})
	service := newAdaptiveAttemptService(t, fixture)
	if _, err := service.Prepare(context.Background(), AdaptiveExecutionAttemptInput{RunID: fixture.run.RunID, Adapter: "codex", Model: "model"}); err != nil {
		t.Fatal(err)
	}
	if err := fixture.store.WithTx(context.Background(), func(tx *workflowstore.Tx) error {
		_, err := tx.CreateExecutionAttempt(context.Background(), workflowstore.CreateExecutionAttemptParams{AttemptID: workflowstore.NewExecutionAttemptID(), RunRowID: fixture.run.ID, AttemptNumber: 2, Adapter: "codex", Model: "model"})
		return err
	}); err == nil {
		t.Fatal("second package attempt succeeded")
	}
	var legacyID int64
	if err := fixture.store.DB().QueryRowContext(context.Background(), `INSERT INTO runs (run_id, feature_slug, repo_target, status, branch, base_commit) VALUES ('run-legacy-attempts', 'checkout', 'relay', 'created', 'legacy', ?) RETURNING id`, strings.Repeat("a", 40)).Scan(&legacyID); err != nil {
		t.Fatal(err)
	}
	if err := fixture.store.WithTx(context.Background(), func(tx *workflowstore.Tx) error {
		legacy, err := tx.GetRunByRowID(context.Background(), legacyID)
		if err != nil {
			return err
		}
		if _, err := tx.TransitionRun(context.Background(), legacy.RunID, workflowstore.RunStatusCreated, workflowstore.RunStatusSetupReady); err != nil {
			return err
		}
		if _, err := tx.TransitionRun(context.Background(), legacy.RunID, workflowstore.RunStatusSetupReady, workflowstore.RunStatusExecuting); err != nil {
			return err
		}
		if _, err := tx.CreateExecutionAttempt(context.Background(), workflowstore.CreateExecutionAttemptParams{AttemptID: workflowstore.NewExecutionAttemptID(), RunRowID: legacyID, AttemptNumber: 1, Adapter: "codex", Model: "model"}); err != nil {
			return err
		}
		_, err = tx.CreateExecutionAttempt(context.Background(), workflowstore.CreateExecutionAttemptParams{AttemptID: workflowstore.NewExecutionAttemptID(), RunRowID: legacyID, AttemptNumber: 2, Adapter: "codex", Model: "model"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

func adaptiveAttemptFixture(t *testing.T, operations bool, coverage string, outcome DeterministicOutcomeInput) *executionAssignmentFixture {
	t.Helper()
	fixture := newExecutionAssignmentFixture(t, operations, coverage)
	prepareExecutionAssignment(t, fixture)
	outcome.RunID = fixture.run.RunID
	persistOutcome(t, fixture, outcome)
	return fixture
}

func newAdaptiveAttemptService(t *testing.T, fixture *executionAssignmentFixture) *AdaptiveExecutionAttemptService {
	t.Helper()
	service, err := NewAdaptiveExecutionAttemptService(fixture.store, fixture.sourceVaultReader)
	if err != nil {
		t.Fatal(err)
	}
	return service
}
