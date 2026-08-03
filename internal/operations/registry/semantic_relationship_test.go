package registry

import "testing"

func TestWorkflowReferenceSemanticMultiplicityAndRelationships(t *testing.T) {
	references := []map[string]any{
		{"kind": "delivery_ticket", "workspace_id": "workspace-1", "ticket_id": "ticket-1"},
		{"kind": "feature_workspace", "workspace_id": "workspace-1"},
	}
	if err := validateAndSortReferences(references, []WorkflowReferenceKind{"feature_workspace", "delivery_ticket"}); err != nil {
		t.Fatalf("current references were rejected: %v", err)
	}
	if references[0]["kind"] != "feature_workspace" || references[1]["kind"] != "delivery_ticket" {
		t.Fatalf("canonical reference order = %#v", references)
	}

	duplicateKind := []map[string]any{
		{"kind": "run", "run_id": "run-1"},
		{"kind": "run", "run_id": "run-2"},
	}
	if err := validateAndSortReferences(duplicateKind, []WorkflowReferenceKind{"run"}); err == nil {
		t.Fatal("duplicate run kind was accepted")
	}

	mismatchedWorkspace := []map[string]any{
		{"kind": "feature_workspace", "workspace_id": "workspace-1"},
		{"kind": "delivery_ticket", "workspace_id": "workspace-2", "ticket_id": "ticket-1"},
	}
	if err := validateAndSortReferences(mismatchedWorkspace, []WorkflowReferenceKind{"feature_workspace", "delivery_ticket"}); err == nil {
		t.Fatal("ticket unrelated to the supplied workspace was accepted")
	}

	mismatchedDecision := []map[string]any{
		{"kind": "run", "run_id": "run-1"},
		{"kind": "audit_decision", "run_id": "run-2", "audit_decision_id": "decision-1"},
	}
	if err := validateAndSortReferences(mismatchedDecision, []WorkflowReferenceKind{"run", "audit_decision"}); err == nil {
		t.Fatal("audit decision unrelated to the supplied run was accepted")
	}
}

func TestAuditDecisionDoesNotRequireRunReference(t *testing.T) {
	references := []map[string]any{{"kind": "audit_decision", "run_id": "run-1", "audit_decision_id": "decision-1"}}
	if err := validateAndSortReferences(references, []WorkflowReferenceKind{"audit_decision"}); err != nil {
		t.Fatalf("audit-decision-only reference was rejected: %v", err)
	}
}
