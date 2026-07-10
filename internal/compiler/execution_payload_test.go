package compiler

import (
	"testing"

	"relay/internal/validation"
)

func TestExecutionSpecCompatibilityPreservesOptionalDeterministicMetadata(t *testing.T) {
	content := `<execution_spec>
execution_spec_id: exec-1
selected_pass:
  pass_id: PASS-1
execution_payload:
  goal: Preserve metadata
  scope: Compiler compatibility path
  non_goals: []
  file_targets:
    - path: internal/compiler/compiler.go
  implementation_steps:
    - id: S1
      title: Project metadata
      action: preserve it
      target_paths: [internal/compiler/compiler.go]
      instructions: Keep optional metadata.
      acceptance_criteria: [Metadata remains available.]
  code_requirements:
    - id: CR1
      requirement: Preserve optional execution payload metadata.
      applies_to: [internal/compiler/compiler.go]
  expected_behavior: [Metadata remains available.]
  validation_contract:
    mode: commands
    failure_policy: block
    commands:
      - id: V1
        command: go test ./internal/compiler
        required: true
  deterministic_operations:
    - id: op-1
      kind: replace
      paths: [internal/compiler/compiler.go]
  operation_groups:
    - id: group-1
      atomic: true
  changed_file_policy:
    mode: allowlist
  source_guards:
    branch: main
  execution_mode:
    preferred_mode: deterministic_packet
    fallback_mode: executor
</execution_spec>`

	projected, _, ok := projectExecutionSpecCompatibility(content)
	if !ok {
		t.Fatal("expected execution spec compatibility projection")
	}
	if len(projected.ExecutionPayloadMetadata["deterministic_operations"].([]map[string]interface{})) != 1 {
		t.Fatalf("deterministic operations were not preserved: %+v", projected.ExecutionPayloadMetadata)
	}
	if projected.ExecutionPayloadMetadata["execution_mode"].(map[string]interface{})["preferred_mode"] != "deterministic_packet" {
		t.Fatalf("execution mode was not preserved: %+v", projected.ExecutionPayloadMetadata)
	}
}

func TestExecutionPayloadConflictInvalidatesPacketValidation(t *testing.T) {
	report := &validation.ValidationReport{Valid: true}
	packet := []byte(`{"execution_payload":{"validation_contract":{"commands":[{"command":"go test ./...","required":true,"phase":"post_apply"}]},"validation_commands":[{"command":"go test ./...","required":false,"phase":"post_apply"}]}}`)
	applyExecutionPayloadProjectionDiagnostics(report, packet)
	if report.Valid || len(report.Errors) != 1 || report.Errors[0].Code != "validation_command_conflict" {
		t.Fatalf("expected validation command conflict, got %+v", report)
	}
}
