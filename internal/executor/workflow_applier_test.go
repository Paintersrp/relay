package executor

import (
	"context"
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
	run := createRunWithDeterministicPayload(t, fixture, "deterministic-applier-complete")
	fixture.service.runner = func(context.Context, string, string, []string, string, time.Duration, pipeline.AgentCommandStreamCallbacks, pipeline.ProcessController) pipeline.AgentCommandRunResult {
		t.Fatal("model runner must not be called after completed applier outcome")
		return pipeline.AgentCommandRunResult{}
	}
	fixture.service.applier = func(ctx context.Context, input applier.Input) (applier.Result, error) {
		artifact := writeFakeApplierEvidence(t, ctx, input.EvidenceWriter)
		return applier.Result{
			Outcome:              applier.OutcomeCompleted,
			ActorKind:            applier.ActorKindApplier,
			ChangedFiles:         []string{"internal/example/config.go"},
			Ledger:               applier.Ledger{Entries: []applier.LedgerEntry{{OperationID: "op-replace", Outcome: applier.OperationApplied, ChangedFiles: []string{"internal/example/config.go"}}}},
			ImplementationResult: applier.ImplementationResult{Outcome: applier.OutcomeCompleted, ActorKind: applier.ActorKindApplier, CompletedOperations: []string{"op-replace"}, ChangedFiles: []string{"internal/example/config.go"}},
			Evidence:             []applier.EvidenceArtifact{artifact},
		}, nil
	}
	result, err := fixture.service.Start(context.Background(), WorkflowStartInput{RunID: run.RunID, Adapter: "opencode_go", Model: "unused-model"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Attempt.AttemptID != "" {
		t.Fatalf("applier-completed run created a model attempt: %+v", result.Attempt)
	}
	if result.Applier == nil || result.Applier.Outcome != string(applier.OutcomeCompleted) {
		t.Fatalf("missing completed applier result: %+v", result.Applier)
	}
	current, err := fixture.store.GetRunByRunID(context.Background(), run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Status != workflowstore.RunStatusValidating {
		t.Fatalf("Run status = %q, want validating", current.Status)
	}
	attempts, err := fixture.store.ListExecutionAttemptsByRun(context.Background(), run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(attempts) != 0 {
		t.Fatalf("created %d model attempts for applier-only completion", len(attempts))
	}
	assertRunHasApplierArtifact(t, fixture, run.ID)
}

func TestWorkflowStartPartialApplierAddsResidualContextToOrdinaryAttempt(t *testing.T) {
	fixture := newWorkflowFixture(t)
	run := createRunWithDeterministicPayload(t, fixture, "deterministic-applier-partial")
	fixture.service.runner = successfulRunner
	fixture.service.applier = func(ctx context.Context, input applier.Input) (applier.Result, error) {
		artifact := writeFakeApplierEvidence(t, ctx, input.EvidenceWriter)
		return applier.Result{
			Outcome:      applier.OutcomePartial,
			ActorKind:    applier.ActorKindApplier,
			ChangedFiles: []string{"internal/example/config.go"},
			Ledger: applier.Ledger{Entries: []applier.LedgerEntry{
				{OperationID: "op-replace", Outcome: applier.OperationApplied, ChangedFiles: []string{"internal/example/config.go"}},
				{OperationID: "op-model", Outcome: applier.OperationResidual, Reason: "operation is marked for model-backed execution"},
			}},
			ImplementationResult: applier.ImplementationResult{Outcome: applier.OutcomePartial, ActorKind: applier.ActorKindApplier, CompletedOperations: []string{"op-replace"}, ResidualOperations: []string{"op-model"}, ChangedFiles: []string{"internal/example/config.go"}, ModelExecutorRequired: true},
			Evidence:             []applier.EvidenceArtifact{artifact},
		}, nil
	}
	result, err := fixture.service.Start(context.Background(), WorkflowStartInput{RunID: run.RunID, Adapter: "opencode_go", Model: "attempt-model"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Attempt.AttemptID == "" {
		t.Fatal("partial applier outcome did not create ordinary model attempt")
	}
	if result.Applier == nil || result.Applier.Outcome != string(applier.OutcomePartial) {
		t.Fatalf("missing partial applier result: %+v", result.Applier)
	}
	if !strings.Contains(fixture.adapter.brief, "# Executor Brief") || !strings.Contains(fixture.adapter.brief, "Relay deterministic pre-application context") || !strings.Contains(fixture.adapter.brief, "op-model") {
		t.Fatalf("residual context was not supplied with the ordinary brief: %q", fixture.adapter.brief)
	}
	attempts, err := fixture.store.ListExecutionAttemptsByRun(context.Background(), run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(attempts) != 1 || attempts[0].Status != workflowstore.AttemptStatusSucceeded {
		t.Fatalf("ordinary attempt history = %+v", attempts)
	}
	assertRunHasApplierArtifact(t, fixture, run.ID)
}

func TestWorkflowStartBlockedApplierPreventsModelDispatch(t *testing.T) {
	fixture := newWorkflowFixture(t)
	run := createRunWithDeterministicPayload(t, fixture, "deterministic-applier-blocked")
	fixture.service.runner = func(context.Context, string, string, []string, string, time.Duration, pipeline.AgentCommandStreamCallbacks, pipeline.ProcessController) pipeline.AgentCommandRunResult {
		t.Fatal("model runner must not be called after blocked applier outcome")
		return pipeline.AgentCommandRunResult{}
	}
	fixture.service.applier = func(ctx context.Context, input applier.Input) (applier.Result, error) {
		artifact := writeFakeApplierEvidence(t, ctx, input.EvidenceWriter)
		failure := &applier.FailurePacket{FailureClass: applier.FailureClassUnsafeSource, Summary: "source guard failed", BlockedOperations: []string{"op-blocked"}}
		return applier.Result{
			Outcome:              applier.OutcomeBlocked,
			ActorKind:            applier.ActorKindApplier,
			FailurePacket:        failure,
			Ledger:               applier.Ledger{Entries: []applier.LedgerEntry{{OperationID: "op-blocked", Outcome: applier.OperationBlocked, Failure: applier.FailureClassUnsafeSource, Reason: "source guard failed"}}},
			ImplementationResult: applier.ImplementationResult{Outcome: applier.OutcomeBlocked, ActorKind: applier.ActorKindApplier, BlockedOperations: []string{"op-blocked"}, FailureClass: applier.FailureClassUnsafeSource, FailureReason: "source guard failed"},
			Evidence:             []applier.EvidenceArtifact{artifact},
		}, nil
	}
	result, err := fixture.service.Start(context.Background(), WorkflowStartInput{RunID: run.RunID, Adapter: "opencode_go", Model: "blocked-model"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Attempt.AttemptID != "" {
		t.Fatalf("blocked applier created a model attempt: %+v", result.Attempt)
	}
	if result.Applier == nil || result.Applier.Outcome != string(applier.OutcomeBlocked) {
		t.Fatalf("missing blocked applier result: %+v", result.Applier)
	}
	current, err := fixture.store.GetRunByRunID(context.Background(), run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Status != workflowstore.RunStatusNeedsRevision {
		t.Fatalf("Run status = %q, want needs_revision", current.Status)
	}
	attempts, err := fixture.store.ListExecutionAttemptsByRun(context.Background(), run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(attempts) != 0 {
		t.Fatalf("created %d model attempts for blocked deterministic application", len(attempts))
	}
	assertRunHasApplierArtifact(t, fixture, run.ID)
}

func TestWorkflowStartWithoutDeterministicOperationsSkipsApplier(t *testing.T) {
	fixture := newWorkflowFixture(t)
	fixture.service.runner = successfulRunner
	fixture.service.applier = func(context.Context, applier.Input) (applier.Result, error) {
		t.Fatal("applier must not be called when deterministic operations are absent")
		return applier.Result{}, nil
	}
	result, err := fixture.service.Start(context.Background(), WorkflowStartInput{RunID: fixture.run.RunID, Adapter: "opencode_go", Model: "ordinary-model"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Applier != nil {
		t.Fatalf("unexpected applier result for ordinary run: %+v", result.Applier)
	}
	if result.Attempt.AttemptID == "" {
		t.Fatal("ordinary run did not create a model attempt")
	}
}

func createRunWithDeterministicPayload(t *testing.T, fixture *workflowFixture, slug string) workflowstore.Run {
	t.Helper()
	canonical := []byte(`{"execution_payload":{"deterministic_operations":[{"id":"op-replace","kind":"replace","mode":"exact","paths":["internal/example/config.go"],"payload":{"old_text":"const enabled = false\\n","new_text":"const enabled = true\\n"},"on_failure":"residual"}]}}`)
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

func assertRunHasApplierArtifact(t *testing.T, fixture *workflowFixture, runRowID int64) {
	t.Helper()
	artifacts, err := fixture.store.ListArtifactsByRun(context.Background(), runRowID)
	if err != nil {
		t.Fatal(err)
	}
	for _, artifact := range artifacts {
		if artifact.Kind == "applier_result_json" {
			return
		}
	}
	t.Fatalf("run artifacts do not include applier_result_json: %+v", artifacts)
}
