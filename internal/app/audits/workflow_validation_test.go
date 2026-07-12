package audits

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"relay/internal/speccompiler"
	workflowstore "relay/internal/store/workflow"
)

func TestMapWorkflowAuditValidationUsesCanonicalOrderAndStructuredStatuses(t *testing.T) {
	commands := []speccompiler.ProjectedValidationCommand{
		{Command: "go test ./internal/executor", Expected: "Executor tests pass."},
		{Command: "go test ./internal/app/audits", WorkingDirectory: "tools", Expected: "Audit tests pass."},
		{Command: "go run ./cmd/mcp-smoke", SuccessSignal: "Smoke passes."},
	}
	validationResults := []workflowAuditValidationEvidence{
		{Command: commands[0].Command, Expected: commands[0].Expected, Status: "passed", ConciseResult: "Validation command passed."},
		{Command: commands[1].Command, WorkingDirectory: "tools", Expected: commands[1].Expected, Status: "failed", ConciseResult: "assertion mismatch"},
		{Command: commands[2].Command, Expected: "Smoke passes.", Status: "not_run", ConciseResult: "environment unavailable"},
	}
	implementation := WorkflowImplementationEvidence{
		Executor: &WorkflowExecutorImplementationEvidence{
			ExecutionEvidenceArtifact: workflowstore.Artifact{ArtifactID: "artifact-evidence"},
			ExecutionEvidence:         workflowExecutionEvidencePayload{ValidationResults: validationResults},
		},
	}

	results, err := mapWorkflowAuditValidation(implementation, commands)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != len(commands) {
		t.Fatalf("results = %+v", results)
	}
	for index, command := range commands {
		if results[index].Command != command.Command || results[index].ArtifactReference != "artifact-evidence" || results[index].ExitCode != nil {
			t.Fatalf("result %d = %+v", index, results[index])
		}
	}
	if results[0].Status != "passed" || results[1].Status != "failed" || results[2].Status != "not_run" {
		t.Fatalf("statuses = %+v", results)
	}
	implementation.Executor.ExecutionEvidence.ValidationResults = []workflowAuditValidationEvidence{validationResults[1], validationResults[0]}
	if _, err := mapWorkflowAuditValidation(implementation, commands); err == nil {
		t.Fatal("expected noncanonical evidence order to be rejected")
	}
	implementation.Executor.ExecutionEvidence.ValidationResults = []workflowAuditValidationEvidence{{Command: "go test ./unknown", Expected: "passes", Status: "passed", ConciseResult: "Validation command passed."}}
	if _, err := mapWorkflowAuditValidation(implementation, commands); err == nil {
		t.Fatal("expected noncanonical command evidence to be rejected")
	}
	implementation.Executor.ExecutionEvidence.ValidationResults = []workflowAuditValidationEvidence{{Command: commands[0].Command, WorkingDirectory: "other", Expected: commands[0].Expected, Status: "passed", ConciseResult: "Validation command passed."}}
	if _, err := mapWorkflowAuditValidation(implementation, commands); err == nil {
		t.Fatal("expected working-directory mismatch to be rejected")
	}
	implementation.Executor.ExecutionEvidence.ValidationResults = []workflowAuditValidationEvidence{{Command: commands[0].Command, Expected: "different", Status: "passed", ConciseResult: "Validation command passed."}}
	if _, err := mapWorkflowAuditValidation(implementation, commands); err == nil {
		t.Fatal("expected expected-result mismatch to be rejected")
	}
}

func TestMapWorkflowAuditValidationUsesTruthfulUnavailableResults(t *testing.T) {
	commands := []speccompiler.ProjectedValidationCommand{
		{Command: "go test ./internal/executor", Expected: "Executor tests pass."},
		{Command: "go test ./internal/app/audits", Expected: "Audit tests pass."},
	}
	modelResults := []workflowAuditValidationEvidence{{Command: commands[0].Command, Expected: commands[0].Expected, Status: "passed", ConciseResult: "Validation command passed."}}
	model := WorkflowImplementationEvidence{
		Executor: &WorkflowExecutorImplementationEvidence{
			ExecutionEvidenceArtifact: workflowstore.Artifact{ArtifactID: "artifact-evidence"},
			ExecutionEvidence:         workflowExecutionEvidencePayload{ValidationResults: modelResults},
		},
	}
	results, err := mapWorkflowAuditValidation(model, commands)
	if err != nil {
		t.Fatal(err)
	}
	if results[0].Status != "passed" || results[0].ArtifactReference == "" {
		t.Fatalf("matched result = %+v", results[0])
	}
	if results[1].Status != "not_run" || results[1].ArtifactReference != "" || results[1].ExitCode != nil {
		t.Fatalf("missing result = %+v", results[1])
	}
	applierOnly, err := mapWorkflowAuditValidation(WorkflowImplementationEvidence{ActorKind: workflowstore.ImplementationActorApplier}, commands)
	if err != nil {
		t.Fatal(err)
	}
	for _, result := range applierOnly {
		if result.Status != "not_run" || result.ArtifactReference != "" || result.ExitCode != nil {
			t.Fatalf("applier-only result = %+v", result)
		}
	}
	empty, err := mapWorkflowAuditValidation(WorkflowImplementationEvidence{}, nil)
	if err != nil || len(empty) != 0 || empty == nil {
		t.Fatalf("zero-command result = %#v, err = %v", empty, err)
	}
}

func TestDecodeWorkflowExecutionEvidenceRejectsAmbiguousValidation(t *testing.T) {
	secret := "audit-validation-secret"
	t.Setenv("OPENAI_API_KEY", secret)
	base := map[string]any{
		"effective_brief_artifact_id": "artifact-brief",
		"effective_brief_sha256":      strings.Repeat("a", 64),
		"effective_brief_mode":        "full",
	}
	valid := func(status string) map[string]any {
		return map[string]any{"command": "go test ./...", "expected": "passes", "status": status, "concise_result": "result"}
	}
	tests := []struct {
		name    string
		results any
	}{
		{name: "null", results: nil},
		{name: "empty", results: []any{}},
		{name: "duplicate", results: []any{valid("passed"), valid("failed")}},
		{name: "unsupported status", results: []any{valid("success")}},
		{name: "invented exit code", results: []any{map[string]any{"command": "go test ./...", "expected": "passes", "status": "passed", "concise_result": "result", "exit_code": 0}}},
		{name: "oversized concise result", results: []any{map[string]any{"command": "go test ./...", "expected": "passes", "status": "failed", "concise_result": strings.Repeat("x", maxWorkflowAuditValidationConciseRunes+1)}}},
		{name: "unredacted concise result", results: []any{map[string]any{"command": "go test ./...", "expected": "passes", "status": "failed", "concise_result": secret}}},
		{name: "noncanonical command identity", results: []any{map[string]any{"command": " go test ./...", "expected": "passes", "status": "failed", "concise_result": "result"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload := map[string]any{}
			for key, value := range base {
				payload[key] = value
			}
			payload["validation_results"] = tt.results
			data, err := json.Marshal(payload)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := decodeWorkflowExecutionEvidence(data); err == nil {
				t.Fatal("expected invalid structured evidence to be rejected")
			}
		})
	}
}

func selectedAuditAttemptAndEvidence(t *testing.T, fixture *auditFixture) (workflowstore.ExecutionAttempt, workflowstore.Artifact) {
	t.Helper()
	attempt, found, err := fixture.store.GetLatestSucceededExecutionAttemptOptional(context.Background(), fixture.run.ID)
	if err != nil || !found {
		t.Fatalf("selected attempt = %+v, found = %v, err = %v", attempt, found, err)
	}
	artifacts, err := fixture.store.ListArtifactsByExecutionAttempt(context.Background(), attempt.ID)
	if err != nil {
		t.Fatal(err)
	}
	var evidence workflowstore.Artifact
	for _, artifact := range artifacts {
		if artifact.Kind != "execution_evidence" {
			continue
		}
		if evidence.ID != 0 {
			t.Fatal("fixture has multiple execution_evidence artifacts")
		}
		evidence = artifact
	}
	if evidence.ID == 0 {
		t.Fatal("fixture execution_evidence artifact is missing")
	}
	return attempt, evidence
}

func rewriteAuditArtifact(t *testing.T, fixture *auditFixture, artifact workflowstore.Artifact, data []byte) {
	t.Helper()
	path, err := workflowArtifactPath(fixture.store, artifact)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(data)
	if _, err := fixture.store.DB().Exec(`UPDATE artifacts SET sha256 = ?, size_bytes = ? WHERE id = ?`, hex.EncodeToString(digest[:]), len(data), artifact.ID); err != nil {
		t.Fatal(err)
	}
}

func stageDuplicateAuditExecutionEvidence(t *testing.T, fixture *auditFixture, attempt workflowstore.ExecutionAttempt, evidence workflowstore.Artifact) {
	t.Helper()
	path, err := workflowArtifactPath(fixture.store, evidence)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	batch, err := fixture.store.ArtifactStore().Begin("audit-duplicate/" + attempt.AttemptID)
	if err != nil {
		t.Fatal(err)
	}
	staged, err := batch.Stage("execution_evidence", "execution-evidence-duplicate.json", "application/json", data)
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.store.CommitArtifactBatch(context.Background(), batch, func(tx *workflowstore.Tx) error {
		_, err := tx.CreateArtifact(context.Background(), workflowstore.CreateArtifactParams{ArtifactID: workflowstore.NewArtifactID(), OwnerType: workflowstore.ArtifactOwnerExecutionAttempt, ExecutionAttemptRowID: sql.NullInt64{Int64: attempt.ID, Valid: true}, Kind: staged.Kind, RelativePath: staged.RelativePath, MediaType: staged.MediaType, SHA256: staged.SHA256, SizeBytes: staged.SizeBytes})
		return err
	}); err != nil {
		t.Fatal(err)
	}
}
