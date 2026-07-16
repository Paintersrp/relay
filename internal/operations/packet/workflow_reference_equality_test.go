package packet

import (
	"errors"
	"strings"
	"testing"
)

func TestWorkflowInputReferenceRequiresCompleteResolvedEquality(t *testing.T) {
	shaA := strings.Repeat("a", 64)
	shaB := strings.Repeat("b", 64)

	tests := []struct {
		name      string
		reference WorkflowReference
		mutations []func(*WorkflowReference)
	}{
		{
			name: "plan",
			reference: WorkflowReference{
				Kind:                    "plan",
				PlanID:                  "plan-1",
				CanonicalArtifactID:     "artifact-plan",
				CanonicalArtifactSHA256: shaA,
			},
			mutations: []func(*WorkflowReference){
				func(value *WorkflowReference) { value.PlanID = "plan-2" },
				func(value *WorkflowReference) { value.CanonicalArtifactID = "artifact-other" },
				func(value *WorkflowReference) { value.CanonicalArtifactSHA256 = shaB },
			},
		},
		{
			name: "pass",
			reference: WorkflowReference{
				Kind:       "pass",
				PlanID:     "plan-1",
				PassID:     "pass-1",
				PassNumber: 1,
			},
			mutations: []func(*WorkflowReference){
				func(value *WorkflowReference) { value.PlanID = "plan-2" },
				func(value *WorkflowReference) { value.PassID = "pass-2" },
				func(value *WorkflowReference) { value.PassNumber = 2 },
			},
		},
		{
			name: "run",
			reference: WorkflowReference{
				Kind:                    "run",
				RunID:                   "run-1",
				ExecutionSpecArtifactID: "artifact-spec",
				ExecutionSpecSHA256:     shaA,
			},
			mutations: []func(*WorkflowReference){
				func(value *WorkflowReference) { value.RunID = "run-2" },
				func(value *WorkflowReference) { value.ExecutionSpecArtifactID = "artifact-other" },
				func(value *WorkflowReference) { value.ExecutionSpecSHA256 = shaB },
			},
		},
		{
			name: "audit_packet",
			reference: WorkflowReference{
				Kind:              "audit_packet",
				RunID:             "run-1",
				AuditPacketID:     "audit-packet-1",
				AuditPacketSHA256: shaA,
			},
			mutations: []func(*WorkflowReference){
				func(value *WorkflowReference) { value.RunID = "run-2" },
				func(value *WorkflowReference) { value.AuditPacketID = "audit-packet-2" },
				func(value *WorkflowReference) { value.AuditPacketSHA256 = shaB },
			},
		},
		{
			name: "audit_decision",
			reference: WorkflowReference{
				Kind:            "audit_decision",
				RunID:           "run-1",
				AuditDecisionID: "audit-decision-1",
				Decision:        "accepted",
				RecordedAt:      "2026-07-16T12:00:00Z",
			},
			mutations: []func(*WorkflowReference){
				func(value *WorkflowReference) { value.RunID = "run-2" },
				func(value *WorkflowReference) { value.AuditDecisionID = "audit-decision-2" },
				func(value *WorkflowReference) { value.Decision = "needs_revision" },
				func(value *WorkflowReference) { value.RecordedAt = "2026-07-16T12:00:01Z" },
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if !workflowInputReferencePresent(test.reference, []WorkflowReference{test.reference}) {
				t.Fatal("exact resolved workflow reference was rejected")
			}
			if err := validateWorkflowInputReferences(
				[]InputBinding{workflowRecordBinding(test.reference)},
				[]WorkflowReference{test.reference},
			); err != nil {
				t.Fatalf("exact resolved workflow reference failed validation: %v", err)
			}

			for index, mutate := range test.mutations {
				t.Run("mismatch_"+decimalTestIndex(index), func(t *testing.T) {
					contradictory := test.reference
					mutate(&contradictory)
					if workflowInputReferencePresent(contradictory, []WorkflowReference{test.reference}) {
						t.Fatalf("contradictory resolved workflow reference was accepted: %+v", contradictory)
					}
					err := validateWorkflowInputReferences(
						[]InputBinding{workflowRecordBinding(contradictory)},
						[]WorkflowReference{test.reference},
					)
					assertWorkflowRecordReferenceError(t, err)
				})
			}
		})
	}
}

func TestAuditPacketWorkflowRecordDoesNotMatchRunReference(t *testing.T) {
	sha := strings.Repeat("a", 64)
	record := WorkflowReference{
		Kind:              "audit_packet",
		RunID:             "run-1",
		AuditPacketID:     "audit-packet-1",
		AuditPacketSHA256: sha,
	}
	run := WorkflowReference{
		Kind:                    "run",
		RunID:                   "run-1",
		ExecutionSpecArtifactID: "artifact-spec",
		ExecutionSpecSHA256:     sha,
	}
	if workflowInputReferencePresent(record, []WorkflowReference{run}) {
		t.Fatal("audit-packet workflow record matched a top-level Run reference")
	}
	assertWorkflowRecordReferenceError(
		t,
		validateWorkflowInputReferences(
			[]InputBinding{workflowRecordBinding(record)},
			[]WorkflowReference{run},
		),
	)
}

func TestWorkflowInputReferenceMismatchErrorIsBounded(t *testing.T) {
	record := WorkflowReference{
		Kind:                    "plan",
		PlanID:                  strings.Repeat("caller-secret-marker", 40),
		CanonicalArtifactID:     "artifact-plan",
		CanonicalArtifactSHA256: strings.Repeat("a", 64),
	}
	err := validateWorkflowInputReferences(
		[]InputBinding{workflowRecordBinding(record)},
		[]WorkflowReference{},
	)
	assertWorkflowRecordReferenceError(t, err)
	if strings.Contains(err.Error(), "caller-secret-marker") || len(err.Error()) > 96 {
		t.Fatalf("workflow-record error is not bounded: %q", err.Error())
	}
}

func TestWorkflowReferenceClosedUnionStillRejectsInactiveFields(t *testing.T) {
	value := WorkflowReference{
		Kind:                    "plan",
		PlanID:                  "plan-1",
		CanonicalArtifactID:     "artifact-plan",
		CanonicalArtifactSHA256: strings.Repeat("a", 64),
		RunID:                   "run-inactive",
	}
	var validation *ValidationError
	if err := validateWorkflowReference(value); !errors.As(err, &validation) || validation.Code != "workflow_reference_closed" {
		t.Fatalf("closed workflow reference error = %#v", err)
	}
}

func workflowRecordBinding(reference WorkflowReference) InputBinding {
	return InputBinding{
		SourceKind: InputSourceWorkflowRecord,
		Source: InputSource{
			Kind:              InputSourceWorkflowRecord,
			WorkflowReference: reference,
		},
	}
}

func assertWorkflowRecordReferenceError(t *testing.T, err error) {
	t.Helper()
	var validation *ValidationError
	if !errors.As(err, &validation) || validation.Code != "workflow_record_reference" {
		t.Fatalf("workflow-record error = %#v", err)
	}
}

func decimalTestIndex(value int) string {
	const digits = "0123456789"
	if value < 10 {
		return string(digits[value])
	}
	return "many"
}
