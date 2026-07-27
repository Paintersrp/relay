package executor

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
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
			outcomes, err := NewDeterministicOutcomeService(fixture.store, fixture.sourceVaultReader)
			if err != nil {
				t.Fatal(err)
			}
			input := test.input()
			input.RunID = fixture.run.RunID
			if _, err := outcomes.Persist(context.Background(), input); err != nil {
				t.Fatal(err)
			}
			service, err := NewEffectiveExecutorBriefService(fixture.store, fixture.sourceVaultReader)
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
			if result.Artifact == nil || len(result.Bytes) == 0 {
				t.Fatalf("result = %#v", result)
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
			if test.mode == EffectiveExecutorBriefAdaptiveAfterPartialApplication && !bytes.Contains(result.Bytes, []byte("Application evidence is execution evidence")) {
				t.Fatal("applied outcome evidence missing")
			}
			if test.mode == EffectiveExecutorBriefDeterministicComplete {
				for _, fact := range []string{
					"- Mode: deterministic_complete",
					"- Adaptive Executor dispatch required: no",
					"- Deterministic Operations coverage: complete",
					"- Deterministic application: applied successfully",
					"- Required behavior: Do not dispatch an adaptive Executor.",
					"- Audit requirement: Audit the complete approved Ticket Design Brief against the resulting source, not merely the deterministic operations.",
				} {
					if !bytes.Contains(result.Bytes, []byte(fact)) {
						t.Fatalf("complete-mode fact missing: %q", fact)
					}
				}
				for _, evidence := range []string{"- Coverage: complete", "- Changed paths:", "Applied operations:", "- Operation 1:", "- Source path:", "- Destination path:", "Source before:", "Source after:", "Destination before:", "Destination after:", "Application evidence is execution evidence and does not replace or narrow the complete approved Ticket Design Brief."} {
					if !bytes.Contains(result.Bytes, []byte(evidence)) {
						t.Fatalf("complete application evidence missing: %q", evidence)
					}
				}
			}
		})
	}
}

func TestPrepareEffectiveExecutorBriefIsIdempotentAndRejectsCompleteConflict(t *testing.T) {
	fixture := newExecutionAssignmentFixture(t, false, "")
	prepareExecutionAssignment(t, fixture)
	persistOutcome(t, fixture, DeterministicOutcomeInput{Preflight: DeterministicPreflightResult{Status: DeterministicPreflightNotPresent}})
	service, err := NewEffectiveExecutorBriefService(fixture.store, fixture.sourceVaultReader)
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
	completeService, err := NewEffectiveExecutorBriefService(complete.store, complete.sourceVaultReader)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := completeService.Prepare(context.Background(), complete.run.RunID); !errors.Is(err, ErrEffectiveExecutorBriefConflict) {
		t.Fatalf("complete conflict = %v", err)
	}
}

func TestPrepareEffectiveExecutorBriefCompleteIsIdempotent(t *testing.T) {
	fixture := newExecutionAssignmentFixture(t, true, "complete")
	prepareExecutionAssignment(t, fixture)
	persistOutcome(t, fixture, appliedOutcomeInput("complete"))
	service, err := NewEffectiveExecutorBriefService(fixture.store, fixture.sourceVaultReader)
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
	if first.Mode != EffectiveExecutorBriefDeterministicComplete || first.AdaptiveDispatchRequired || first.Artifact == nil || second.Artifact == nil || first.Artifact.ID != second.Artifact.ID || first.Artifact.RelativePath != second.Artifact.RelativePath || !bytes.Equal(first.Bytes, second.Bytes) {
		t.Fatalf("repeated complete result differs: %#v %#v", first, second)
	}
}

func TestLoadEffectiveExecutorBriefCompleteVerifiesArtifact(t *testing.T) {
	fixture := newExecutionAssignmentFixture(t, true, "complete")
	prepareExecutionAssignment(t, fixture)
	persistOutcome(t, fixture, appliedOutcomeInput("complete"))
	service, err := NewEffectiveExecutorBriefService(fixture.store, fixture.sourceVaultReader)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := service.Prepare(context.Background(), fixture.run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := service.Load(context.Background(), fixture.run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Mode != EffectiveExecutorBriefDeterministicComplete || loaded.AdaptiveDispatchRequired || loaded.Artifact == nil || loaded.Artifact.ID != prepared.Artifact.ID || !bytes.Equal(loaded.Bytes, prepared.Bytes) {
		t.Fatalf("loaded complete result = %#v, prepared = %#v", loaded, prepared)
	}
}

func TestLoadEffectiveExecutorBriefCompleteRejectsMissingDuplicateMalformedAndChangedArtifact(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(t *testing.T, fixture *executionAssignmentFixture, artifact workflowstore.Artifact)
	}{
		{name: "missing", mutate: func(t *testing.T, fixture *executionAssignmentFixture, artifact workflowstore.Artifact) {
			if _, err := fixture.store.DB().ExecContext(context.Background(), "DELETE FROM artifacts WHERE id = ?", artifact.ID); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "malformed", mutate: func(t *testing.T, fixture *executionAssignmentFixture, artifact workflowstore.Artifact) {
			if _, err := fixture.store.DB().ExecContext(context.Background(), "UPDATE artifacts SET media_type = ? WHERE id = ?", "application/json", artifact.ID); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "changed bytes", mutate: func(t *testing.T, fixture *executionAssignmentFixture, artifact workflowstore.Artifact) {
			if err := os.WriteFile(filepath.Join(fixture.store.ArtifactStore().Root(), filepath.FromSlash(artifact.RelativePath)), []byte("changed\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newExecutionAssignmentFixture(t, true, "complete")
			prepareExecutionAssignment(t, fixture)
			persistOutcome(t, fixture, appliedOutcomeInput("complete"))
			service, err := NewEffectiveExecutorBriefService(fixture.store, fixture.sourceVaultReader)
			if err != nil {
				t.Fatal(err)
			}
			prepared, err := service.Prepare(context.Background(), fixture.run.RunID)
			if err != nil {
				t.Fatal(err)
			}
			test.mutate(t, fixture, *prepared.Artifact)
			if _, err := service.Load(context.Background(), fixture.run.RunID); !errors.Is(err, ErrEffectiveExecutorBriefConflict) {
				t.Fatalf("complete load error = %v", err)
			}
		})
	}
}

func TestLoadEffectiveExecutorBriefCompleteRejectsDuplicateArtifactRecords(t *testing.T) {
	fixture := newExecutionAssignmentFixture(t, true, "complete")
	prepareExecutionAssignment(t, fixture)
	persistOutcome(t, fixture, appliedOutcomeInput("complete"))
	service, err := NewEffectiveExecutorBriefService(fixture.store, fixture.sourceVaultReader)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := service.Prepare(context.Background(), fixture.run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	duplicate := *prepared.Artifact
	duplicate.ID++
	duplicate.ArtifactID = workflowstore.NewArtifactID()
	if _, err := findEffectiveExecutorBrief([]workflowstore.Artifact{*prepared.Artifact, duplicate}, fixture.run); !errors.Is(err, ErrEffectiveExecutorBriefConflict) {
		t.Fatalf("duplicate artifact error = %v", err)
	}
}

func TestLoadDeterministicOutcomeRejectsChangedBytes(t *testing.T) {
	fixture := newExecutionAssignmentFixture(t, false, "")
	prepareExecutionAssignment(t, fixture)
	result := persistOutcome(t, fixture, DeterministicOutcomeInput{Preflight: DeterministicPreflightResult{Status: DeterministicPreflightNotPresent}})
	if err := os.WriteFile(filepath.Join(fixture.store.ArtifactStore().Root(), filepath.FromSlash(result.Artifact.RelativePath)), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	service, err := NewDeterministicOutcomeService(fixture.store, fixture.sourceVaultReader)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Load(context.Background(), fixture.run.RunID); !errors.Is(err, ErrDeterministicOutcomeConflict) {
		t.Fatalf("changed outcome = %v", err)
	}
}

func TestEffectiveExecutorBriefContentUsesExactSourceRepresentation(t *testing.T) {
	for _, test := range []struct {
		name    string
		content []byte
		base64  bool
	}{
		{name: "LF", content: []byte("line one\nline two\n")},
		{name: "without trailing newline", content: []byte("line one\nline two"), base64: true},
		{name: "CRLF", content: []byte("line one\r\nline two\r\n")},
		{name: "trailing spaces", content: []byte("line with spaces   \n")},
		{name: "long backtick run", content: []byte("before `````` after\n")},
		{name: "empty", content: []byte{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var rendered strings.Builder
			digest := sha256Hex(test.content)
			writeFencedContent(&rendered, "text/plain", test.content, int64(len(test.content)), digest)
			value := rendered.String()
			if !strings.Contains(value, "- Source byte count: "+fmt.Sprint(len(test.content))+"\n") || !strings.Contains(value, "- Source SHA-256: "+digest+"\n") {
				t.Fatalf("metadata missing: %q", value)
			}
			if test.base64 {
				if !strings.Contains(value, "Source representation: base64") {
					t.Fatalf("base64 representation missing: %q", value)
				}
				encoded := strings.TrimSuffix(strings.TrimPrefix(strings.Split(value, "```text/base64\n")[1], ""), "\n```\n")
				decoded, err := base64.StdEncoding.DecodeString(encoded)
				if err != nil || !bytes.Equal(decoded, test.content) {
					t.Fatalf("decoded content=%q err=%v", decoded, err)
				}
				return
			}
			fence := deterministicFence(test.content)
			if len(fence) <= longestBacktickRun(test.content) {
				t.Fatalf("fence=%q is not longer than content run", fence)
			}
			opening := "\n" + fence + "text/plain\n"
			start := strings.Index(value, opening)
			if start < 0 {
				t.Fatalf("opening fence missing: %q", value)
			}
			body := value[start+len(opening):]
			closing := fence + "\n"
			if !strings.HasSuffix(body, closing) || !bytes.Equal([]byte(strings.TrimSuffix(body, closing)), test.content) {
				t.Fatalf("fenced content changed: %q", body)
			}
		})
	}
}

func longestBacktickRun(content []byte) int {
	longest, current := 0, 0
	for _, value := range content {
		if value == '`' {
			current++
			if current > longest {
				longest = current
			}
		} else {
			current = 0
		}
	}
	return longest
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
	service, err := NewDeterministicOutcomeService(fixture.store, fixture.sourceVaultReader)
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
