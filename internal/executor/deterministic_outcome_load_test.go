package executor

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"relay/internal/speccompiler"
)

func TestLoadDeterministicOutcomeBindsAuthoredWholeFileStates(t *testing.T) {
	tests := []struct {
		name     string
		document *speccompiler.DeterministicOperationsDocument
		plan     *DeterministicMutationPlan
		mutate   func(*DeterministicOutcome)
	}{
		{
			name:     "create hash",
			document: deterministicOutcomeDocument("complete", operation("created.txt", "create", implContent("created\n"))),
			plan:     deterministicOutcomePlan("complete", preparedCreate("created.txt", "created\n")),
			mutate: func(outcome *DeterministicOutcome) {
				outcome.Application.Operations[0].SourceAfter.SHA256 = strings.Repeat("0", 64)
			},
		},
		{
			name:     "create size",
			document: deterministicOutcomeDocument("complete", operation("created.txt", "create", implContent("created\n"))),
			plan:     deterministicOutcomePlan("complete", preparedCreate("created.txt", "created\n")),
			mutate: func(outcome *DeterministicOutcome) {
				outcome.Application.Operations[0].SourceAfter.Size++
			},
		},
		{
			name:     "delete expected hash",
			document: deterministicOutcomeDocument("complete", operation("deleted.txt", "delete", implExpected("deleted\n"))),
			plan:     deterministicOutcomePlan("complete", preparedDelete("deleted.txt", "deleted\n")),
			mutate: func(outcome *DeterministicOutcome) {
				outcome.Application.Operations[0].SourceBefore.SHA256 = strings.Repeat("0", 64)
			},
		},
		{
			name:     "delete expected size",
			document: deterministicOutcomeDocument("complete", operation("deleted.txt", "delete", implExpected("deleted\n"))),
			plan:     deterministicOutcomePlan("complete", preparedDelete("deleted.txt", "deleted\n")),
			mutate: func(outcome *DeterministicOutcome) {
				outcome.Application.Operations[0].SourceBefore.Size++
			},
		},
		{
			name:     "rename source hash",
			document: deterministicOutcomeDocument("complete", rename("source.txt", "target.txt", "source\n", true, "")),
			plan:     deterministicOutcomePlan("complete", preparedRename("source.txt", "target.txt", "source\n", "source\n")),
			mutate: func(outcome *DeterministicOutcome) {
				outcome.Application.Operations[0].SourceBefore.SHA256 = strings.Repeat("0", 64)
			},
		},
		{
			name:     "preserving rename destination",
			document: deterministicOutcomeDocument("complete", rename("source.txt", "target.txt", "source\n", true, "")),
			plan:     deterministicOutcomePlan("complete", preparedRename("source.txt", "target.txt", "source\n", "source\n")),
			mutate: func(outcome *DeterministicOutcome) {
				outcome.Application.Operations[0].DestinationAfter.SHA256 = strings.Repeat("0", 64)
			},
		},
		{
			name:     "replacing rename destination",
			document: deterministicOutcomeDocument("complete", renameWithReplacement("source.txt", "target.txt", "source\n", "replacement\n")),
			plan:     deterministicOutcomePlan("complete", preparedRename("source.txt", "target.txt", "source\n", "replacement\n")),
			mutate: func(outcome *DeterministicOutcome) {
				outcome.Application.Operations[0].DestinationAfter.Size++
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newOutcomeLoadFixture(t, test.document)
			result := persistOutcome(t, fixture, DeterministicOutcomeInput{Preflight: DeterministicPreflightResult{Status: DeterministicPreflightReady, Coverage: "complete", Plan: test.plan}, Application: applicationForPlan(t, test.plan)})
			replaceManagedOutcome(t, fixture, result, test.mutate)
			loadOutcomeConflict(t, fixture)
		})
	}
}

func TestLoadDeterministicOutcomeRejectsApplicationEvidenceShapesAndPaths(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*DeterministicOutcome)
	}{
		{name: "reordered changed paths", mutate: func(outcome *DeterministicOutcome) { outcome.Application.ChangedPaths = []string{"b.txt", "a.txt"} }},
		{name: "duplicate changed paths", mutate: func(outcome *DeterministicOutcome) { outcome.Application.ChangedPaths = []string{"a.txt", "a.txt"} }},
		{name: "missing changed path", mutate: func(outcome *DeterministicOutcome) { outcome.Application.ChangedPaths = []string{"a.txt"} }},
		{name: "extra changed path", mutate: func(outcome *DeterministicOutcome) {
			outcome.Application.ChangedPaths = []string{"a.txt", "b.txt", "c.txt"}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			document := deterministicOutcomeDocument("complete", operation("a.txt", "create", implContent("a\n")), operation("b.txt", "create", implContent("b\n")))
			plan := deterministicOutcomePlan("complete", preparedCreate("a.txt", "a\n"), preparedCreate("b.txt", "b\n"))
			fixture := newOutcomeLoadFixture(t, document)
			result := persistOutcome(t, fixture, DeterministicOutcomeInput{Preflight: DeterministicPreflightResult{Status: DeterministicPreflightReady, Coverage: "complete", Plan: plan}, Application: applicationForPlan(t, plan)})
			replaceManagedOutcome(t, fixture, result, test.mutate)
			loadOutcomeConflict(t, fixture)
		})
	}
}

func TestLoadDeterministicOutcomeRejectsMalformedOperationShapes(t *testing.T) {
	tests := []struct {
		name     string
		document *speccompiler.DeterministicOperationsDocument
		plan     *DeterministicMutationPlan
		mutate   func(*DeterministicOutcome)
	}{
		{name: "create", document: deterministicOutcomeDocument("complete", operation("a.txt", "create", implContent("a\n"))), plan: deterministicOutcomePlan("complete", preparedCreate("a.txt", "a\n")), mutate: func(outcome *DeterministicOutcome) { outcome.Application.Operations[0].SourceBefore.Exists = true }},
		{name: "delete", document: deterministicOutcomeDocument("complete", operation("a.txt", "delete", implExpected("a\n"))), plan: deterministicOutcomePlan("complete", preparedDelete("a.txt", "a\n")), mutate: func(outcome *DeterministicOutcome) { outcome.Application.Operations[0].SourceAfter.Exists = true }},
		{name: "rename", document: deterministicOutcomeDocument("complete", rename("a.txt", "b.txt", "a\n", true, "")), plan: deterministicOutcomePlan("complete", preparedRename("a.txt", "b.txt", "a\n", "a\n")), mutate: func(outcome *DeterministicOutcome) { outcome.Application.Operations[0].DestinationAfter.Exists = false }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newOutcomeLoadFixture(t, test.document)
			result := persistOutcome(t, fixture, DeterministicOutcomeInput{Preflight: DeterministicPreflightResult{Status: DeterministicPreflightReady, Coverage: "complete", Plan: test.plan}, Application: applicationForPlan(t, test.plan)})
			replaceManagedOutcome(t, fixture, result, test.mutate)
			loadOutcomeConflict(t, fixture)
		})
	}
}

func TestLoadDeterministicOutcomeReadbackAcceptsValidOperationsAndCoverage(t *testing.T) {
	for _, coverage := range []string{"partial", "complete"} {
		t.Run("create-"+coverage, func(t *testing.T) {
			document := deterministicOutcomeDocument(coverage, operation("a.txt", "create", implContent("a\n")))
			plan := deterministicOutcomePlan(coverage, preparedCreate("a.txt", "a\n"))
			assertValidOutcomeReadback(t, document, plan, coverage)
		})
	}
	for _, test := range []struct {
		name     string
		document *speccompiler.DeterministicOperationsDocument
		plan     *DeterministicMutationPlan
	}{
		{name: "delete", document: deterministicOutcomeDocument("complete", operation("a.txt", "delete", implExpected("a\n"))), plan: deterministicOutcomePlan("complete", preparedDelete("a.txt", "a\n"))},
		{name: "preserving rename", document: deterministicOutcomeDocument("complete", rename("a.txt", "b.txt", "a\n", true, "")), plan: deterministicOutcomePlan("complete", preparedRename("a.txt", "b.txt", "a\n", "a\n"))},
		{name: "replacing rename", document: deterministicOutcomeDocument("complete", renameWithReplacement("a.txt", "b.txt", "a\n", "b\n")), plan: deterministicOutcomePlan("complete", preparedRename("a.txt", "b.txt", "a\n", "b\n"))},
		{name: "rename chain", document: deterministicOutcomeDocument("complete", rename("a.txt", "b.txt", "a\n", true, ""), renameWithReplacement("b.txt", "c.txt", "a\n", "c\n")), plan: deterministicOutcomePlan("complete", preparedRename("a.txt", "b.txt", "a\n", "a\n"), preparedRename("b.txt", "c.txt", "a\n", "c\n"))},
	} {
		t.Run(test.name, func(t *testing.T) { assertValidOutcomeReadback(t, test.document, test.plan, "complete") })
	}
}

func TestLoadDeterministicOutcomeRejectsFailureEvidenceBinding(t *testing.T) {
	tests := []struct {
		name     string
		document *speccompiler.DeterministicOperationsDocument
		failure  DeterministicPreflightFailure
	}{
		{name: "operation index outside list", document: deterministicOutcomeDocument("complete", operation("a.txt", "create", implContent("a\n"))), failure: DeterministicPreflightFailure{Code: "destination_exists", OperationIndex: 2, Path: "a.txt"}},
		{name: "directive index outside modify", document: deterministicOutcomeDocument("complete", operation("a.txt", "modify", speccompiler.DeterministicImplementation{Changes: []speccompiler.DeterministicChange{{Kind: "remove", OldText: "a", ExpectedOccurrences: 1}}})), failure: DeterministicPreflightFailure{Code: "selector_occurrence_mismatch", OperationIndex: 1, DirectiveIndex: 2, Path: "a.txt"}},
		{name: "directive index on non-modify", document: deterministicOutcomeDocument("complete", operation("a.txt", "create", implContent("a\n"))), failure: DeterministicPreflightFailure{Code: "destination_exists", OperationIndex: 1, DirectiveIndex: 1, Path: "a.txt"}},
		{name: "unrelated failure path", document: deterministicOutcomeDocument("complete", operation("a.txt", "create", implContent("a\n"))), failure: DeterministicPreflightFailure{Code: "destination_exists", OperationIndex: 1, Path: "b.txt"}},
		{name: "destination on non-rename", document: deterministicOutcomeDocument("complete", operation("a.txt", "create", implContent("a\n"))), failure: DeterministicPreflightFailure{Code: "destination_exists", OperationIndex: 1, Path: "a.txt", Destination: "b.txt"}},
		{name: "wrong rename destination", document: deterministicOutcomeDocument("complete", rename("a.txt", "b.txt", "a\n", true, "")), failure: DeterministicPreflightFailure{Code: "destination_exists", OperationIndex: 1, Path: "a.txt", Destination: "c.txt"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newOutcomeLoadFixture(t, test.document)
			result := persistOutcome(t, fixture, DeterministicOutcomeInput{Preflight: DeterministicPreflightResult{Status: DeterministicPreflightFailed, Coverage: "complete", Failure: &test.failure}})
			replaceManagedOutcome(t, fixture, result, func(outcome *DeterministicOutcome) {
				outcome.PreflightFailure = &DeterministicOutcomePreflightFailure{Code: test.failure.Code, OperationIndex: test.failure.OperationIndex, DirectiveIndex: test.failure.DirectiveIndex, Path: test.failure.Path, Destination: test.failure.Destination, Expected: test.failure.Expected, Observed: test.failure.Observed}
			})
			loadOutcomeConflict(t, fixture)
		})
	}
}

func newOutcomeLoadFixture(t *testing.T, document *speccompiler.DeterministicOperationsDocument) *executionAssignmentFixture {
	t.Helper()
	raw, err := marshalOutcomeTestOperations(document)
	if err != nil {
		t.Fatal(err)
	}
	fixture := newExecutionAssignmentFixtureWithOperations(t, true, document.Coverage, raw)
	prepareExecutionAssignment(t, fixture)
	return fixture
}

func marshalOutcomeTestOperations(document *speccompiler.DeterministicOperationsDocument) ([]byte, error) {
	type implementation struct {
		Changes         []speccompiler.DeterministicChange `json:"changes,omitempty"`
		ExpectedContent string                             `json:"expected_content,omitempty"`
		PreserveContent *bool                              `json:"preserve_content,omitempty"`
		Content         string                             `json:"content,omitempty"`
	}
	type operation struct {
		Path            string         `json:"path"`
		DestinationPath string         `json:"destination_path,omitempty"`
		Operation       string         `json:"operation"`
		Implementation  implementation `json:"implementation"`
	}
	type documentValue struct {
		SchemaVersion any         `json:"schema_version,omitempty"`
		FeatureSlug   string      `json:"feature_slug"`
		RepoTarget    string      `json:"repo_target"`
		Branch        string      `json:"branch"`
		BaseCommit    string      `json:"base_commit"`
		Coverage      string      `json:"coverage"`
		Operations    []operation `json:"operations"`
	}
	value := documentValue{SchemaVersion: document.SchemaVersion, FeatureSlug: document.FeatureSlug, RepoTarget: document.RepoTarget, Branch: document.Branch, BaseCommit: document.BaseCommit, Coverage: document.Coverage}
	value.Operations = make([]operation, len(document.Operations))
	for index, item := range document.Operations {
		value.Operations[index] = operation{Path: item.Path, DestinationPath: item.DestinationPath, Operation: item.Operation, Implementation: implementation{Changes: item.Implementation.Changes, ExpectedContent: item.Implementation.ExpectedContent, PreserveContent: item.Implementation.PreserveContent, Content: item.Implementation.Content}}
	}
	return json.Marshal(value)
}

func deterministicOutcomeDocument(coverage string, operations ...speccompiler.DeterministicOperation) *speccompiler.DeterministicOperationsDocument {
	return &speccompiler.DeterministicOperationsDocument{SchemaVersion: "1.0", FeatureSlug: "checkout", RepoTarget: "relay", Branch: "main", BaseCommit: strings.Repeat("a", 40), Coverage: coverage, Operations: operations}
}

func renameWithReplacement(source, destination, expected, content string) speccompiler.DeterministicOperation {
	return speccompiler.DeterministicOperation{Path: source, DestinationPath: destination, Operation: "rename", Implementation: speccompiler.DeterministicImplementation{ExpectedContent: expected, Content: content}}
}

func deterministicOutcomePlan(coverage string, operations ...PreparedDeterministicOperation) *DeterministicMutationPlan {
	for index := range operations {
		operations[index].Index = index + 1
	}
	return &DeterministicMutationPlan{Coverage: coverage, Operations: operations}
}

func preparedCreate(path, content string) PreparedDeterministicOperation {
	return PreparedDeterministicOperation{Index: 1, Operation: "create", SourcePath: path, After: newFileState([]byte(content))}
}

func preparedDelete(path, content string) PreparedDeterministicOperation {
	return PreparedDeterministicOperation{Index: 1, Operation: "delete", SourcePath: path, Before: newFileState([]byte(content))}
}

func preparedRename(source, destination, before, after string) PreparedDeterministicOperation {
	return PreparedDeterministicOperation{Index: 1, Operation: "rename", SourcePath: source, DestinationPath: destination, Before: newFileState([]byte(before)), DestinationAfter: newFileState([]byte(after))}
}

func applicationForPlan(t *testing.T, plan *DeterministicMutationPlan) *DeterministicApplicationResult {
	t.Helper()
	model, err := validateDeterministicPlan(plan)
	if err != nil {
		t.Fatal(err)
	}
	application := applicationResult(model)
	return &application
}

func assertValidOutcomeReadback(t *testing.T, document *speccompiler.DeterministicOperationsDocument, plan *DeterministicMutationPlan, coverage string) {
	t.Helper()
	fixture := newOutcomeLoadFixture(t, document)
	result := persistOutcome(t, fixture, DeterministicOutcomeInput{Preflight: DeterministicPreflightResult{Status: DeterministicPreflightReady, Coverage: coverage, Plan: plan}, Application: applicationForPlan(t, plan)})
	service, err := NewDeterministicOutcomeService(fixture.store)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Load(context.Background(), fixture.run.RunID); err != nil {
		t.Fatalf("valid outcome load = %v", err)
	}
	if result.Outcome.Application == nil {
		t.Fatal("persisted valid outcome omitted application")
	}
}

func replaceManagedOutcome(t *testing.T, fixture *executionAssignmentFixture, result DeterministicOutcomeResult, mutate func(*DeterministicOutcome)) {
	t.Helper()
	path := filepath.Join(fixture.store.ArtifactStore().Root(), filepath.FromSlash(result.Artifact.RelativePath))
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var outcome DeterministicOutcome
	if err := json.Unmarshal(content, &outcome); err != nil {
		t.Fatal(err)
	}
	mutate(&outcome)
	replacement, err := marshalDeterministicOutcome(outcome)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, replacement, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.store.DB().Exec("UPDATE artifacts SET sha256 = ?, size_bytes = ? WHERE id = ?", sha256Hex(replacement), len(replacement), result.Artifact.ID); err != nil {
		t.Fatal(err)
	}
}

func loadOutcomeConflict(t *testing.T, fixture *executionAssignmentFixture) {
	t.Helper()
	service, err := NewDeterministicOutcomeService(fixture.store)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Load(context.Background(), fixture.run.RunID); !errors.Is(err, ErrDeterministicOutcomeConflict) {
		t.Fatalf("load error = %v, want conflict", err)
	}
}
