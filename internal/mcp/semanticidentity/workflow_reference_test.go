package semanticidentity

import "testing"

func TestCurrentWorkflowReferenceRequests(t *testing.T) {
	valid := []WorkflowReferenceRequest{
		{Kind: "feature_workspace", WorkspaceID: "workspace-1"},
		{Kind: "delivery_ticket", WorkspaceID: "workspace-1", TicketID: "ticket-1"},
		{Kind: "run", RunID: "run-1"},
		{Kind: "audit_decision", RunID: "run-1", AuditDecisionID: "decision-1"},
	}
	for _, reference := range valid {
		if err := validateWorkflowReference(reference); err != nil {
			t.Fatalf("%s reference rejected: %v", reference.Kind, err)
		}
	}
	for _, kind := range []string{"plan", "pass", "audit_packet"} {
		if err := validateWorkflowReference(WorkflowReferenceRequest{Kind: kind}); err == nil {
			t.Fatalf("legacy %s reference accepted", kind)
		}
	}
	foreign := WorkflowReferenceRequest{Kind: "feature_workspace", WorkspaceID: "workspace-1", RunID: "run-foreign"}
	if err := validateWorkflowReference(foreign); err == nil {
		t.Fatal("foreign branch field accepted")
	}
}

func TestWorkflowReferenceRequestRelationshipsAndMultiplicity(t *testing.T) {
	tests := []struct {
		name       string
		references []WorkflowReferenceRequest
		valid      bool
	}{
		{"audit_decision_only", []WorkflowReferenceRequest{{Kind: "audit_decision", RunID: "run-1", AuditDecisionID: "decision-1"}}, true},
		{"matching_workspace", []WorkflowReferenceRequest{{Kind: "feature_workspace", WorkspaceID: "workspace-1"}, {Kind: "delivery_ticket", WorkspaceID: "workspace-1", TicketID: "ticket-1"}}, true},
		{"mismatched_workspace", []WorkflowReferenceRequest{{Kind: "feature_workspace", WorkspaceID: "workspace-1"}, {Kind: "delivery_ticket", WorkspaceID: "workspace-2", TicketID: "ticket-1"}}, false},
		{"mismatched_run", []WorkflowReferenceRequest{{Kind: "run", RunID: "run-1"}, {Kind: "audit_decision", RunID: "run-2", AuditDecisionID: "decision-1"}}, false},
		{"duplicate_kind", []WorkflowReferenceRequest{{Kind: "run", RunID: "run-1"}, {Kind: "run", RunID: "run-2"}}, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validatePacketRequest("surface-1", 0, nil, nil, test.references, nil, nil, nil, "")
			if (err == nil) != test.valid {
				t.Fatalf("validation error = %v, valid = %t", err, test.valid)
			}
		})
	}
}
