package speccompiler

import "testing"

func TestProjectExecutionPayloadPrefersValidationContractAndPreservesMetadata(t *testing.T) {
	raw := []byte(`{"validation":{"commands":[{"command":"go test ./..."}]},"execution_payload":{"deterministic_operations":[{"id":"op-1","kind":"replace","paths":["a.go"]}],"operation_groups":[{"id":"group-1","atomic":true}],"changed_file_policy":{"mode":"allowlist"},"source_guards":{"branch":"main"},"execution_mode":{"preferred_mode":"deterministic_packet","fallback_mode":"executor"},"validation_contract":{"commands":[{"id":"V1","command":"go test ./...","required":true,"phase":"post_apply","severity":"hard"}]}}}`)
	projection, diagnostics := ProjectExecutionPayload(raw)
	if len(diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %+v", diagnostics)
	}
	if projection.ValidationCommandSource != "execution_payload.validation_contract.commands" {
		t.Fatalf("source = %q", projection.ValidationCommandSource)
	}
	if len(projection.ValidationCommands) != 1 || projection.ValidationCommands[0].ID != "V1" {
		t.Fatalf("commands = %+v", projection.ValidationCommands)
	}
	if len(projection.DeterministicOperations) != 1 || projection.DeterministicOperations[0].ID != "op-1" {
		t.Fatalf("operations = %+v", projection.DeterministicOperations)
	}
	if len(projection.OperationGroups) != 1 || projection.ExecutionMode.PreferredMode != "deterministic_packet" {
		t.Fatalf("metadata was not preserved: %+v", projection)
	}
}

func TestProjectExecutionPayloadAllowsCanonicalEnrichment(t *testing.T) {
	raw := []byte(`{"execution_payload":{"validation_contract":{"commands":[{"command":"go test ./...","required":true,"phase":"post_apply"}]},"validation_commands":[{"command":"go test ./..."}]}}`)
	_, diagnostics := ProjectExecutionPayload(raw)
	if len(diagnostics) != 0 {
		t.Fatalf("canonical enrichment should not conflict: %+v", diagnostics)
	}
}

func TestProjectExecutionPayloadKeepsOrdinaryPacketsValid(t *testing.T) {
	raw := []byte(`{"execution_payload":{"validation_contract":{"commands":[{"command":"go test ./..."}]}}}`)
	projection, diagnostics := ProjectExecutionPayload(raw)
	if len(diagnostics) != 0 {
		t.Fatalf("ordinary packet should remain valid: %+v", diagnostics)
	}
	if len(projection.DeterministicOperations) != 0 || projection.ExecutionMode.PreferredMode != "" {
		t.Fatalf("ordinary packet unexpectedly gained deterministic metadata: %+v", projection)
	}
}

func TestProjectExecutionPayloadBlocksExplicitValidationPolicyConflict(t *testing.T) {
	raw := []byte(`{"execution_payload":{"validation_contract":{"commands":[{"command":"go test ./...","required":true,"phase":"post_apply","severity":"hard"}]},"validation_commands":[{"command":"go test ./...","required":false,"phase":"post_executor","severity":"advisory"}]}}`)
	_, diagnostics := ProjectExecutionPayload(raw)
	if len(diagnostics) != 1 || diagnostics[0].Code != "validation_command_conflict" {
		t.Fatalf("diagnostics = %+v", diagnostics)
	}
}
