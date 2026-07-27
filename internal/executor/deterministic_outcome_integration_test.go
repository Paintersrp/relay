package executor

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestPersistDeterministicOutcomePersistsAllRuntimeModes(t *testing.T) {
	for _, test := range []struct {
		name       string
		operations bool
		coverage   string
		input      func() DeterministicOutcomeInput
		status     string
	}{
		{
			name: "not present", input: func() DeterministicOutcomeInput {
				return DeterministicOutcomeInput{Preflight: DeterministicPreflightResult{Status: DeterministicPreflightNotPresent}}
			}, status: "not_present",
		},
		{
			name: "partial preflight failed", operations: true, coverage: "partial", input: func() DeterministicOutcomeInput {
				return DeterministicOutcomeInput{Preflight: DeterministicPreflightResult{Status: DeterministicPreflightFailed, Coverage: "partial", Failure: &DeterministicPreflightFailure{Code: "source_missing", OperationIndex: 1, Path: "internal/example.go", Expected: "exists=true", Observed: "exists=false"}}}
			}, status: "preflight_failed",
		},
		{
			name: "complete preflight failed", operations: true, coverage: "complete", input: func() DeterministicOutcomeInput {
				return DeterministicOutcomeInput{Preflight: DeterministicPreflightResult{Status: DeterministicPreflightFailed, Coverage: "complete", Failure: &DeterministicPreflightFailure{Code: "source_missing", OperationIndex: 1, Path: "internal/example.go", Expected: "exists=true", Observed: "exists=false"}}}
			}, status: "preflight_failed",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newExecutionAssignmentFixture(t, test.operations, test.coverage)
			prepareExecutionAssignment(t, fixture)
			service, err := NewDeterministicOutcomeService(fixture.store, fixture.sourceVaultReader)
			if err != nil {
				t.Fatal(err)
			}
			input := test.input()
			input.RunID = fixture.run.RunID
			result, err := service.Persist(context.Background(), input)
			if err != nil {
				t.Fatal(err)
			}
			if result.Outcome.Outcome.Status != test.status || !strings.HasSuffix(string(result.Bytes), "\n") || strings.HasSuffix(string(result.Bytes), "\n\n") {
				t.Fatalf("outcome = %#v, bytes = %q", result.Outcome.Outcome, result.Bytes)
			}
			var decoded map[string]any
			if err := json.Unmarshal(result.Bytes, &decoded); err != nil {
				t.Fatal(err)
			}
			if decoded["application"] != nil && strings.Contains(string(result.Bytes), "bytes") {
				t.Fatal("application evidence persisted file bytes")
			}
		})
	}
}

func TestPersistDeterministicOutcomePersistsVerifiedApplicationAndIsIdempotent(t *testing.T) {
	for _, coverage := range []string{"partial", "complete"} {
		t.Run(coverage, func(t *testing.T) {
			fixture := newExecutionAssignmentFixture(t, true, coverage)
			prepareExecutionAssignment(t, fixture)
			service, err := NewDeterministicOutcomeService(fixture.store, fixture.sourceVaultReader)
			if err != nil {
				t.Fatal(err)
			}
			plan := testDeterministicCreatePlan(coverage)
			model, err := validateDeterministicPlan(plan)
			if err != nil {
				t.Fatal(err)
			}
			application := applicationResult(model)
			input := DeterministicOutcomeInput{RunID: fixture.run.RunID, Preflight: DeterministicPreflightResult{Status: DeterministicPreflightReady, Coverage: coverage, Plan: plan}, Application: &application}
			first, err := service.Persist(context.Background(), input)
			if err != nil {
				t.Fatal(err)
			}
			if first.Outcome.Outcome != (DeterministicOutcomeSummary{Status: "applied", Coverage: coverage}) || first.Outcome.Application == nil {
				t.Fatalf("outcome = %#v", first.Outcome)
			}
			if got, want := first.Outcome.Application.ChangedPaths, []string{"internal/example.go"}; !reflect.DeepEqual(got, want) {
				t.Fatalf("changed paths = %#v, want %#v", got, want)
			}
			second, err := service.Persist(context.Background(), input)
			if err != nil {
				t.Fatal(err)
			}
			if first.Artifact.ID != second.Artifact.ID || !reflect.DeepEqual(first.Bytes, second.Bytes) {
				t.Fatalf("repeated result = %#v, want %#v", second, first)
			}
			first.Bytes[0] = 'x'
			first.Outcome.Application.ChangedPaths[0] = "changed"
			if second.Bytes[0] == 'x' || second.Outcome.Application.ChangedPaths[0] == "changed" {
				t.Fatal("result exposed mutable outcome data")
			}
		})
	}
}

func TestPersistDeterministicOutcomeRejectsInvalidInputWithoutArtifact(t *testing.T) {
	fixture := newExecutionAssignmentFixture(t, true, "complete")
	service, err := NewDeterministicOutcomeService(fixture.store, fixture.sourceVaultReader)
	if err != nil {
		t.Fatal(err)
	}
	missing := DeterministicOutcomeInput{RunID: fixture.run.RunID, Preflight: DeterministicPreflightResult{Status: DeterministicPreflightReady, Coverage: "complete", Plan: testDeterministicCreatePlan("complete")}}
	if _, err := service.Persist(context.Background(), missing); err == nil {
		t.Fatal("missing assignment was accepted")
	}
	prepareExecutionAssignment(t, fixture)
	cases := []DeterministicOutcomeInput{
		{RunID: fixture.run.RunID, Preflight: DeterministicPreflightResult{Status: DeterministicPreflightNotPresent}},
		{RunID: fixture.run.RunID, Preflight: DeterministicPreflightResult{Status: DeterministicPreflightFailed, Coverage: "complete", Failure: &DeterministicPreflightFailure{Code: "bad", OperationIndex: 1, Path: "../unsafe"}}},
		{RunID: fixture.run.RunID, Preflight: DeterministicPreflightResult{Status: DeterministicPreflightReady, Coverage: "complete", Plan: testDeterministicCreatePlan("complete")}},
	}
	for _, input := range cases {
		if _, err := service.Persist(context.Background(), input); err == nil {
			t.Fatalf("invalid input was accepted: %#v", input)
		}
	}
	for _, artifact := range listRunArtifacts(t, fixture) {
		if artifact.Kind == deterministicOutcomeKind {
			t.Fatal("invalid input created an outcome artifact")
		}
	}
}

func TestPersistDeterministicOutcomeRejectsConflictingRecordedOutcome(t *testing.T) {
	fixture := newExecutionAssignmentFixture(t, false, "")
	prepareExecutionAssignment(t, fixture)
	service, err := NewDeterministicOutcomeService(fixture.store, fixture.sourceVaultReader)
	if err != nil {
		t.Fatal(err)
	}
	input := DeterministicOutcomeInput{RunID: fixture.run.RunID, Preflight: DeterministicPreflightResult{Status: DeterministicPreflightNotPresent}}
	result, err := service.Persist(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fixture.store.ArtifactStore().Root(), filepath.FromSlash(result.Artifact.RelativePath)), []byte("tampered\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Persist(context.Background(), input); !errors.Is(err, ErrDeterministicOutcomeConflict) {
		t.Fatalf("tampered outcome error = %v, want conflict", err)
	}
}

func testDeterministicCreatePlan(coverage string) *DeterministicMutationPlan {
	content := []byte("package example\n")
	return &DeterministicMutationPlan{Coverage: coverage, Operations: []PreparedDeterministicOperation{{Index: 1, Operation: "create", SourcePath: "internal/example.go", After: newFileState(content)}}}
}
