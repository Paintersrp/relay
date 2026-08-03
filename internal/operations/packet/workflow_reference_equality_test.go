package packet

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"relay/internal/operations/registry"
)

func TestWorkflowRecordReferenceClosedUnion(t *testing.T) {
	valid := WorkflowRecordReference{Kind: "plan_artifact", PlanID: "plan-1", ArtifactID: "artifact-1", ArtifactSHA256: strings.Repeat("a", 64)}
	if err := validateWorkflowRecordReference(valid); err != nil {
		t.Fatalf("valid workflow record reference: %v", err)
	}
	valid.RunID = "run-foreign"
	var validation *ValidationError
	if err := validateWorkflowRecordReference(valid); !errors.As(err, &validation) || validation.Code != "workflow_record_reference_closed" {
		t.Fatalf("closed workflow record reference error = %#v", err)
	}
}

func TestCurrentWorkflowReferenceClosedUnion(t *testing.T) {
	value := WorkflowReference{Kind: "feature_workspace", WorkspaceID: "workspace-1", WorkspaceVersion: 2, RouteStateID: "route-1", RouteSequence: 1, RouteWorkspaceVersion: 1, RouteState: "ready", RunID: "run-foreign"}
	var validation *ValidationError
	if err := validateWorkflowReference(value); !errors.As(err, &validation) || validation.Code != "workflow_reference_closed" {
		t.Fatalf("closed workflow reference error = %#v", err)
	}
}

func TestCurrentWorkflowReferenceOrdering(t *testing.T) {
	values := []WorkflowReference{
		{Kind: "audit_decision", RunID: "run-1", AuditDecisionID: "decision-1", Decision: "accepted", RecordedAt: "2026-07-15T16:04:05.123456789Z"},
		{Kind: "run", RunID: "run-1", ExecutionSpecArtifactID: "artifact-1", ExecutionSpecSHA256: strings.Repeat("a", 64)},
		{Kind: "delivery_ticket", WorkspaceID: "workspace-1", TicketID: "ticket-1", RevisionID: 1, RevisionNumber: 1, SourceClosureID: "closure-1"},
		{Kind: "feature_workspace", WorkspaceID: "workspace-1", WorkspaceVersion: 2, RouteStateID: "route-1", RouteSequence: 1, RouteWorkspaceVersion: 1, RouteState: "ready"},
	}
	operation := registry.OperationDefinition{WorkflowReferenceKinds: []registry.WorkflowReferenceKind{"feature_workspace", "delivery_ticket", "run", "audit_decision"}}
	ordered, err := canonicalWorkflowReferences(values, operation)
	if err != nil {
		t.Fatal(err)
	}
	want := []registry.WorkflowReferenceKind{"feature_workspace", "delivery_ticket", "run", "audit_decision"}
	for index := range want {
		if ordered[index].Kind != want[index] {
			t.Fatalf("reference order = %#v", ordered)
		}
	}
}

func TestWorkflowRecordReferenceCanonicalShape(t *testing.T) {
	value := WorkflowRecordReference{Kind: "run_execution_spec", RunID: "run-1", ArtifactID: "artifact-1", ArtifactSHA256: strings.Repeat("a", 64)}
	var output bytes.Buffer
	writeWorkflowRecordReference(&output, value)
	want := `{"kind":"run_execution_spec","run_id":"run-1","artifact_id":"artifact-1","artifact_sha256":"` + strings.Repeat("a", 64) + `"}`
	if output.String() != want {
		t.Fatalf("canonical workflow record = %s, want %s", output.String(), want)
	}
}
