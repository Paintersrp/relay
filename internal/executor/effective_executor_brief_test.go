package executor

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	workflowstore "relay/internal/store/workflow"
	"relay/internal/testfixtures"
)

func TestPrepareEffectiveExecutorBriefDecidesEveryOutcome(t *testing.T) {
	for _, test := range []struct {
		name       string
		operations bool
		coverage   string
		input      func() DeterministicOutcomeInput
		mode       EffectiveExecutorBriefMode
		adaptive   bool
	}{
		{name: "operations absent", input: func() DeterministicOutcomeInput {
			return DeterministicOutcomeInput{Preflight: DeterministicPreflightResult{Status: DeterministicPreflightNotPresent}}
		}, mode: EffectiveExecutorBriefAdaptiveNoOperations, adaptive: true},
		{name: "partial failed", operations: true, coverage: "partial", input: func() DeterministicOutcomeInput { return failedOutcomeInput("partial") }, mode: EffectiveExecutorBriefAdaptivePreflightFailed, adaptive: true},
		{name: "complete failed", operations: true, coverage: "complete", input: func() DeterministicOutcomeInput { return failedOutcomeInput("complete") }, mode: EffectiveExecutorBriefAdaptivePreflightFailed, adaptive: true},
		{name: "partial applied", operations: true, coverage: "partial", input: func() DeterministicOutcomeInput { return appliedOutcomeInput("partial") }, mode: EffectiveExecutorBriefAdaptiveAfterPartialApplication, adaptive: true},
		{name: "complete applied", operations: true, coverage: "complete", input: func() DeterministicOutcomeInput { return appliedOutcomeInput("complete") }, mode: EffectiveExecutorBriefDeterministicComplete, adaptive: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newExecutionAssignmentFixture(t, test.operations, test.coverage)
			prepareExecutionAssignment(t, fixture)
			outcomes, err := NewDeterministicOutcomeService(fixture.store)
			if err != nil {
				t.Fatal(err)
			}
			input := test.input()
			input.RunID = fixture.run.RunID
			if _, err := outcomes.Persist(context.Background(), input); err != nil {
				t.Fatal(err)
			}
			service, err := NewEffectiveExecutorBriefService(fixture.store)
			if err != nil {
				t.Fatal(err)
			}
			result, err := service.Prepare(context.Background(), fixture.run.RunID)
			if err != nil {
				t.Fatal(err)
			}
			if result.Mode != test.mode || result.AdaptiveDispatchRequired != test.adaptive {
				t.Fatalf("result = %#v", result)
			}
			if test.adaptive {
				if result.Artifact == nil || len(result.Bytes) == 0 {
					t.Fatalf("adaptive result = %#v", result)
				}
				if !bytes.Contains(result.Bytes, []byte(testfixturesBrief(t))) {
					t.Fatal("full approved Ticket Design Brief is not embedded")
				}
				if !strings.HasSuffix(string(result.Bytes), "\n") || strings.HasSuffix(string(result.Bytes), "\n\n") {
					t.Fatal("brief does not have exactly one trailing newline")
				}
				if got := strings.Index(string(result.Bytes), "## Approved Authority Layers"); got < 0 || strings.Index(string(result.Bytes), "## Approved Ticket Design Brief") < got {
					t.Fatal("canonical section order changed")
				}
				if test.mode == EffectiveExecutorBriefAdaptivePreflightFailed && !bytes.Contains(result.Bytes, []byte("Source-state evidence only")) {
					t.Fatal("failed outcome evidence missing")
				}
				if test.mode == EffectiveExecutorBriefAdaptiveAfterPartialApplication && !bytes.Contains(result.Bytes, []byte("These mutations are already applied source state.")) {
					t.Fatal("applied outcome evidence missing")
				}
			} else if result.Artifact != nil || len(result.Bytes) != 0 {
				t.Fatalf("complete outcome created a brief: %#v", result)
			}
		})
	}
}

func TestPrepareEffectiveExecutorBriefIsIdempotentAndRejectsCompleteConflict(t *testing.T) {
	fixture := newExecutionAssignmentFixture(t, false, "")
	prepareExecutionAssignment(t, fixture)
	persistOutcome(t, fixture, DeterministicOutcomeInput{Preflight: DeterministicPreflightResult{Status: DeterministicPreflightNotPresent}})
	service, err := NewEffectiveExecutorBriefService(fixture.store)
	if err != nil {
		t.Fatal(err)
	}
	first, err := service.Prepare(context.Background(), fixture.run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.Prepare(context.Background(), fixture.run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if first.Artifact == nil || second.Artifact == nil || first.Artifact.ID != second.Artifact.ID || !bytes.Equal(first.Bytes, second.Bytes) {
		t.Fatalf("repeated result differs: %#v %#v", first, second)
	}

	complete := newExecutionAssignmentFixture(t, true, "complete")
	assignment := prepareExecutionAssignment(t, complete)
	persistOutcome(t, complete, appliedOutcomeInput("complete"))
	if err := complete.store.WithTx(context.Background(), func(tx *workflowstore.Tx) error {
		_, err := tx.CreateArtifact(context.Background(), workflowstore.CreateArtifactParams{ArtifactID: workflowstore.NewArtifactID(), OwnerType: workflowstore.ArtifactOwnerRun, RunRowID: sql.NullInt64{Int64: complete.run.ID, Valid: true}, Kind: effectiveExecutorBriefKind, RelativePath: "runs/" + complete.run.RunID + "/conflict.md", MediaType: effectiveExecutorBriefMediaType, SHA256: assignment.Artifact.SHA256, SizeBytes: assignment.Artifact.SizeBytes})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	completeService, err := NewEffectiveExecutorBriefService(complete.store)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := completeService.Prepare(context.Background(), complete.run.RunID); !errors.Is(err, ErrEffectiveExecutorBriefConflict) {
		t.Fatalf("complete conflict = %v", err)
	}
}

func TestLoadDeterministicOutcomeRejectsChangedBytes(t *testing.T) {
	fixture := newExecutionAssignmentFixture(t, false, "")
	prepareExecutionAssignment(t, fixture)
	result := persistOutcome(t, fixture, DeterministicOutcomeInput{Preflight: DeterministicPreflightResult{Status: DeterministicPreflightNotPresent}})
	if err := os.WriteFile(filepath.Join(fixture.store.ArtifactStore().Root(), filepath.FromSlash(result.Artifact.RelativePath)), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	service, err := NewDeterministicOutcomeService(fixture.store)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Load(context.Background(), fixture.run.RunID); !errors.Is(err, ErrDeterministicOutcomeConflict) {
		t.Fatalf("changed outcome = %v", err)
	}
}

func failedOutcomeInput(coverage string) DeterministicOutcomeInput {
	return DeterministicOutcomeInput{Preflight: DeterministicPreflightResult{Status: DeterministicPreflightFailed, Coverage: coverage, Failure: &DeterministicPreflightFailure{Code: "source_missing", OperationIndex: 1, Path: "internal/example.go", Expected: "exists=true", Observed: "exists=false"}}}
}

func appliedOutcomeInput(coverage string) DeterministicOutcomeInput {
	plan := testDeterministicCreatePlan(coverage)
	model, err := validateDeterministicPlan(plan)
	if err != nil {
		panic(err)
	}
	application := applicationResult(model)
	return DeterministicOutcomeInput{Preflight: DeterministicPreflightResult{Status: DeterministicPreflightReady, Coverage: coverage, Plan: plan}, Application: &application}
}

func persistOutcome(t *testing.T, fixture *executionAssignmentFixture, input DeterministicOutcomeInput) DeterministicOutcomeResult {
	t.Helper()
	service, err := NewDeterministicOutcomeService(fixture.store)
	if err != nil {
		t.Fatal(err)
	}
	input.RunID = fixture.run.RunID
	result, err := service.Persist(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func testfixturesBrief(t *testing.T) string { t.Helper(); return testfixtures.TicketDesignBrief }
