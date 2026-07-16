package registry

import (
	"strings"
	"testing"
)

func TestWorkflowReferenceSemanticMultiplicityAndRelationships(t *testing.T) {
	references := []map[string]any{
		{"kind": "pass", "plan_id": "plan-b", "pass_id": "pass-b"},
		{"kind": "plan", "plan_id": "plan-b"},
		{"kind": "pass", "plan_id": "plan-a", "pass_id": "pass-a"},
		{"kind": "plan", "plan_id": "plan-a"},
	}
	if err := validateAndSortReferences(references, []WorkflowReferenceKind{"plan", "pass"}); err != nil {
		t.Fatalf("multiple distinct references of one kind were rejected: %v", err)
	}
	if references[0]["kind"] != "plan" || references[0]["plan_id"] != "plan-a" || references[2]["kind"] != "pass" || references[2]["plan_id"] != "plan-a" {
		t.Fatalf("canonical reference order = %#v", references)
	}

	duplicateTuple := []map[string]any{
		{"kind": "run", "run_id": "run-1"},
		{"kind": "audit_packet", "run_id": "run-1", "audit_packet_id": "packet-1", "expected_audit_packet_sha256": strings.Repeat("a", 64)},
		{"kind": "audit_packet", "run_id": "run-1", "audit_packet_id": "packet-1", "expected_audit_packet_sha256": strings.Repeat("b", 64)},
	}
	if err := validateAndSortReferences(duplicateTuple, []WorkflowReferenceKind{"run", "audit_packet"}); err == nil {
		t.Fatal("duplicate audit-packet identity tuple was accepted")
	}

	mismatchedPass := []map[string]any{
		{"kind": "plan", "plan_id": "plan-1"},
		{"kind": "pass", "plan_id": "plan-2", "pass_id": "pass-1"},
	}
	if err := validateAndSortReferences(mismatchedPass, []WorkflowReferenceKind{"plan", "pass"}); err == nil {
		t.Fatal("pass unrelated to the supplied plan was accepted")
	}

	mismatchedDecision := []map[string]any{
		{"kind": "run", "run_id": "run-1"},
		{"kind": "audit_decision", "run_id": "run-2", "audit_decision_id": "decision-1"},
	}
	if err := validateAndSortReferences(mismatchedDecision, []WorkflowReferenceKind{"run", "audit_decision"}); err == nil {
		t.Fatal("audit decision unrelated to the supplied run was accepted")
	}
}

func TestWorkflowRecordInputsRequireEqualOrDerivableReferences(t *testing.T) {
	inputs := []map[string]any{{
		"input_name":  "current_audit_packet",
		"source_kind": "workflow_record",
		"source": map[string]any{
			"workflow_record": map[string]any{
				"kind":            "audit_packet",
				"run_id":          "run-1",
				"audit_packet_id": "packet-1",
			},
		},
	}}
	if err := validateWorkflowRecordReferenceLinks(inputs, []map[string]any{{"kind": "run", "run_id": "run-1"}}); err != nil {
		t.Fatalf("audit packet was not derived from its run: %v", err)
	}
	if err := validateWorkflowRecordReferenceLinks(inputs, []map[string]any{{"kind": "run", "run_id": "run-2"}}); err == nil {
		t.Fatal("workflow record unrelated to packet references was accepted")
	}
}
