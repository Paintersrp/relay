package executor

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	workflowruns "relay/internal/app/runs/workflow"
	"relay/internal/applier"
	"relay/internal/pipeline"
	workflowstore "relay/internal/store/workflow"
)

func TestWorkflowStartCompletedApplierAvoidsModelAttempt(t *testing.T) {
	fixture := newWorkflowFixture(t)
	run := createRunWithCanonicalProjectionSpec(t, fixture, "applier-complete")
	fixture.service.runner = func(context.Context, string, string, []string, string, time.Duration, pipeline.AgentCommandStreamCallbacks, pipeline.ProcessController) pipeline.AgentCommandRunResult {
		t.Fatal("model runner must not be called after completed applier outcome")
		return pipeline.AgentCommandRunResult{}
	}
	fixture.service.applier = func(ctx context.Context, input applier.Input) (applier.Result, error) {
		artifact := writeFakeApplierEvidence(t, ctx, input.EvidenceWriter)
		return applier.Result{
			Outcome:      applier.OutcomeCompleted,
			ActorKind:    applier.ActorKindApplier,
			ChangedFiles: []string{"deterministic.txt"},
			Partition: applier.Partition{
				DeterministicPathChains: []string{"chain.1.1.file.1"},
				DeterministicFileWork:   []string{"1.1.file.1"},
				ProtectedPaths:          []string{"deterministic.txt"},
				CoveredFileWork:         []string{"1.1.file.1"},
			},
			Ledger:               applier.Ledger{Entries: []applier.LedgerEntry{{PathChainRef: "chain.1.1.file.1", FileWorkRefs: []string{"1.1.file.1"}, Disposition: applier.DispositionDeterministic, Outcome: applier.OperationApplied, ChangedPaths: []string{"deterministic.txt"}}}},
			ImplementationResult: applier.ImplementationResult{Outcome: applier.OutcomeCompleted, ActorKind: applier.ActorKindApplier, CompletedPathChains: []string{"chain.1.1.file.1"}, CompletedFileWork: []string{"1.1.file.1"}, ChangedFiles: []string{"deterministic.txt"}, ProtectedPaths: []string{"deterministic.txt"}},
			Evidence:             []applier.EvidenceArtifact{artifact},
		}, nil
	}
	result, err := fixture.service.Start(context.Background(), WorkflowStartInput{RunID: run.RunID, Adapter: "opencode_go", Model: "unused-model"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Attempt.AttemptID != "" || result.Applier == nil || result.Applier.Outcome != string(applier.OutcomeCompleted) {
		t.Fatalf("result = %+v", result)
	}
	current, err := fixture.store.GetRunByRunID(context.Background(), run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Status != workflowstore.RunStatusValidating {
		t.Fatalf("Run status = %q, want validating", current.Status)
	}
}

func TestWorkflowStartHybridPersistsAndInvokesOnlyResidualBrief(t *testing.T) {
	fixture := newWorkflowFixture(t)
	run := createRunWithCanonicalProjectionSpec(t, fixture, "applier-hybrid")
	fixture.service.runner = successfulRunner
	fixture.service.applier = func(ctx context.Context, input applier.Input) (applier.Result, error) {
		artifact := writeFakeApplierEvidence(t, ctx, input.EvidenceWriter)
		return applier.Result{
			Outcome:      applier.OutcomePartial,
			ActorKind:    applier.ActorKindApplier,
			ChangedFiles: []string{"deterministic.txt"},
			Partition: applier.Partition{
				DeterministicPathChains: []string{"chain.1.1.file.1"},
				ResidualPathChains:      []string{"chain.1.2.file.1"},
				DeterministicFileWork:   []string{"1.1.file.1"},
				ResidualFileWork:        []string{"1.2.file.1"},
				ProtectedPaths:          []string{"deterministic.txt"},
				CoveredFileWork:         []string{"1.1.file.1", "1.2.file.1"},
			},
			Ledger: applier.Ledger{Entries: []applier.LedgerEntry{
				{PathChainRef: "chain.1.1.file.1", FileWorkRefs: []string{"1.1.file.1"}, Disposition: applier.DispositionDeterministic, Outcome: applier.OperationApplied, ChangedPaths: []string{"deterministic.txt"}},
				{PathChainRef: "chain.1.2.file.1", FileWorkRefs: []string{"1.2.file.1"}, Disposition: applier.DispositionResidual, Outcome: applier.OperationResidual},
			}},
			ImplementationResult: applier.ImplementationResult{Outcome: applier.OutcomePartial, ActorKind: applier.ActorKindApplier, CompletedPathChains: []string{"chain.1.1.file.1"}, ResidualPathChains: []string{"chain.1.2.file.1"}, CompletedFileWork: []string{"1.1.file.1"}, ResidualFileWork: []string{"1.2.file.1"}, ChangedFiles: []string{"deterministic.txt"}, ProtectedPaths: []string{"deterministic.txt"}, ModelExecutorRequired: true},
			Evidence:             []applier.EvidenceArtifact{artifact},
		}, nil
	}
	result, err := fixture.service.Start(context.Background(), WorkflowStartInput{RunID: run.RunID, Adapter: "opencode_go", Model: "attempt-model"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Attempt.AttemptID == "" {
		t.Fatal("hybrid execution did not create an attempt")
	}
	if !strings.Contains(fixture.adapter.brief, "## Relay Deterministic Pre-Application") || !strings.Contains(fixture.adapter.brief, "residual.txt") || strings.Contains(fixture.adapter.brief, "`deterministic.txt` - Apply deterministic work") {
		t.Fatalf("unexpected effective brief:\n%s", fixture.adapter.brief)
	}
	artifacts, err := fixture.store.ListArtifactsByExecutionAttempt(context.Background(), result.Attempt.ID)
	if err != nil {
		t.Fatal(err)
	}
	var residual workflowstore.Artifact
	for _, artifact := range artifacts {
		if artifact.Kind == "executor_residual_brief" {
			residual = artifact
		}
	}
	if residual.ArtifactID == "" || residual.OwnerType != workflowstore.ArtifactOwnerExecutionAttempt {
		t.Fatalf("residual artifact = %+v", residual)
	}
	content, err := os.ReadFile(filepath.Join(fixture.store.ArtifactStore().Root(), filepath.FromSlash(residual.RelativePath)))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != fixture.adapter.brief {
		t.Fatal("adapter did not receive the exact persisted residual artifact")
	}
	attempt, err := fixture.store.GetExecutionAttemptByAttemptID(context.Background(), result.Attempt.AttemptID)
	if err != nil {
		t.Fatal(err)
	}
	var runtime workflowAttemptRuntime
	if err := json.Unmarshal([]byte(attempt.ResultJSON), &runtime); err != nil {
		t.Fatal(err)
	}
	if runtime.EffectiveBriefMode != "residual" || runtime.EffectiveBriefArtifactID != residual.ArtifactID || runtime.EffectiveBriefSHA256 != residual.SHA256 {
		t.Fatalf("runtime identity = %+v, residual = %+v", runtime, residual)
	}
}

func TestWorkflowStartBlockedApplierPreventsModelDispatch(t *testing.T) {
	fixture := newWorkflowFixture(t)
	run := createRunWithCanonicalProjectionSpec(t, fixture, "applier-blocked")
	fixture.service.runner = func(context.Context, string, string, []string, string, time.Duration, pipeline.AgentCommandStreamCallbacks, pipeline.ProcessController) pipeline.AgentCommandRunResult {
		t.Fatal("model runner must not be called after blocked applier outcome")
		return pipeline.AgentCommandRunResult{}
	}
	fixture.service.applier = func(ctx context.Context, input applier.Input) (applier.Result, error) {
		artifact := writeFakeApplierEvidence(t, ctx, input.EvidenceWriter)
		failure := &applier.FailurePacket{FailureClass: applier.FailureClassUnsafeSource, Summary: "source mismatch", BlockedPathChains: []string{"chain.1.1.file.1"}, BlockedFileWork: []string{"1.1.file.1"}}
		return applier.Result{
			Outcome:              applier.OutcomeBlocked,
			ActorKind:            applier.ActorKindApplier,
			FailurePacket:        failure,
			Ledger:               applier.Ledger{Entries: []applier.LedgerEntry{{PathChainRef: "chain.1.1.file.1", FileWorkRefs: []string{"1.1.file.1"}, Disposition: applier.DispositionBlocked, Outcome: applier.OperationBlocked, Failure: applier.FailureClassUnsafeSource, Reason: "source mismatch"}}},
			ImplementationResult: applier.ImplementationResult{Outcome: applier.OutcomeBlocked, ActorKind: applier.ActorKindApplier, BlockedPathChains: []string{"chain.1.1.file.1"}, BlockedFileWork: []string{"1.1.file.1"}, FailureClass: applier.FailureClassUnsafeSource, FailureReason: "source mismatch"},
			Evidence:             []applier.EvidenceArtifact{artifact},
		}, nil
	}
	result, err := fixture.service.Start(context.Background(), WorkflowStartInput{RunID: run.RunID, Adapter: "opencode_go", Model: "blocked-model"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Attempt.AttemptID != "" || result.Applier == nil || result.Applier.Outcome != string(applier.OutcomeBlocked) {
		t.Fatalf("result = %+v", result)
	}
	current, err := fixture.store.GetRunByRunID(context.Background(), run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Status != workflowstore.RunStatusNeedsRevision {
		t.Fatalf("Run status = %q, want needs_revision", current.Status)
	}
}

func TestWorkflowStartAllResidualUsesCanonicalBriefAndFullIdentity(t *testing.T) {
	fixture := newWorkflowFixture(t)
	fixture.service.runner = successfulRunner
	result, err := fixture.service.Start(context.Background(), WorkflowStartInput{RunID: fixture.run.RunID, Adapter: "opencode_go", Model: "full-model"})
	if err != nil {
		t.Fatal(err)
	}
	if fixture.adapter.brief != string(fixture.brief) {
		t.Fatal("full execution did not receive the canonical brief")
	}
	artifacts, err := fixture.store.ListArtifactsByExecutionAttempt(context.Background(), result.Attempt.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, artifact := range artifacts {
		if artifact.Kind == "executor_residual_brief" {
			t.Fatal("full execution created a residual artifact")
		}
	}
	attempt, err := fixture.store.GetExecutionAttemptByAttemptID(context.Background(), result.Attempt.AttemptID)
	if err != nil {
		t.Fatal(err)
	}
	var runtime workflowAttemptRuntime
	if err := json.Unmarshal([]byte(attempt.ResultJSON), &runtime); err != nil {
		t.Fatal(err)
	}
	if runtime.EffectiveBriefMode != "full" || runtime.EffectiveBriefArtifactID == "" || runtime.EffectiveBriefSHA256 == "" {
		t.Fatalf("runtime identity = %+v", runtime)
	}
}

func createRunWithCanonicalProjectionSpec(t *testing.T, fixture *workflowFixture, slug string) workflowstore.Run {
	t.Helper()
	repository, err := fixture.store.GetRepositoryTarget(context.Background(), "relay")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repository.LocalPath, "deterministic.txt"), []byte("before\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repository.LocalPath, "residual.txt"), []byte("source\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	canonical := []byte(`{
  "schema_version": "2.0",
  "feature_slug": "` + slug + `",
  "repo_target": "relay",
  "branch": "feat/simplification",
  "base_commit": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
  "goal": "Exercise deterministic-first workflow behavior.",
  "context": "Canonical workflow fixture projected directly by the compiler.",
  "scope": {
    "in_scope": ["Exercise deterministic and residual file work."],
    "out_of_scope": ["No unrelated behavior."]
  },
  "steps": [
    {
      "number": 1,
      "goal": "Provide deterministic and residual declarations.",
      "substeps": [
        {
          "number": 1,
          "instruction": "Apply deterministic work.",
          "files": [
            {
              "path": "deterministic.txt",
              "operation": "modify",
              "purpose": "Apply deterministic work.",
              "implementation": {
                "changes": [
                  {
                    "kind": "replace",
                    "old_text": "before\n",
                    "new_text": "after\n",
                    "expected_occurrences": 1
                  }
                ]
              }
            }
          ],
          "completion_criteria": ["The deterministic declaration is complete."]
        },
        {
          "number": 2,
          "instruction": "Apply residual work.",
          "files": [
            {
              "path": "residual.txt",
              "destination_path": "residual-renamed.txt",
              "operation": "rename",
              "purpose": "Exercise model-owned rename replacement content.",
              "implementation": {
                "content": "replacement\n"
              }
            }
          ],
          "completion_criteria": ["The residual declaration is complete."]
        }
      ],
      "completion_criteria": ["The selected declarations are complete."]
    }
  ],
  "validation": {
    "commands": [
      {
        "command": "go test ./internal/executor",
        "expected": "The focused executor tests pass."
      }
    ]
  },
  "completion_criteria": ["The combined deterministic and residual result is complete."]
}
`)
	created, err := fixture.runs.CreateRun(context.Background(), workflowruns.CreateRunInput{
		FeatureSlug:      slug,
		RepoTarget:       "relay",
		Branch:           "feat/simplification",
		BaseCommit:       strings.Repeat("b", 40),
		CanonicalJSON:    canonical,
		RenderedMarkdown: fixture.brief,
	})
	if err != nil {
		t.Fatal(err)
	}
	return created.Run
}

func writeFakeApplierEvidence(t *testing.T, ctx context.Context, writer applier.EvidenceWriter) applier.EvidenceArtifact {
	t.Helper()
	artifact, err := writer.WriteEvidence(ctx, applier.EvidenceFile{Kind: "applier_result_json", Filename: "applier-result.json", MediaType: "application/json", Data: []byte(`{"outcome":"test"}` + "\n")})
	if err != nil {
		t.Fatal(err)
	}
	return artifact
}
