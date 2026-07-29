package operations

import (
	"database/sql"
	"testing"

	workflowstore "relay/internal/store/workflow"
)

func TestLifecycleRemediationBriefDirectReplacementRetainsReplacedRevisionIdentity(t *testing.T) {
	// The remediation revision is a new row, but its replacement identity must
	// remain the audited row it directly replaces.
	remediationRevision := workflowstore.DeliveryTicketRevision{
		ID:                    42,
		ReplacesRevisionRowID: sql.NullInt64{Int64: 17, Valid: true},
	}

	replacementRevisionRowID := remediationReplacementRevisionRowID("replacement_ticket_revision", remediationRevision)
	selected := selectedRemediationTicketInput{
		ReopeningKind:            "replacement_ticket_revision",
		AuditedRevisionRowID:     17,
		RemediationRevisionRowID: remediationRevision.ID,
		ReplacementRevisionRowID: replacementRevisionRowID,
	}
	if selected.ReplacementRevisionRowID == nil || *selected.ReplacementRevisionRowID != selected.AuditedRevisionRowID {
		t.Fatalf("replacement revision row ID = %#v, want audited revision %d", selected.ReplacementRevisionRowID, selected.AuditedRevisionRowID)
	}
	if *selected.ReplacementRevisionRowID == selected.RemediationRevisionRowID {
		t.Fatalf("replacement revision row ID must not identify remediation revision %d", selected.RemediationRevisionRowID)
	}
}

func TestLifecycleRemediationBriefSeparateTicketOmitsReplacementRevisionIdentity(t *testing.T) {
	selected := selectedRemediationTicketInput{
		ReopeningKind:            "remediation_ticket",
		AuditedRevisionRowID:     17,
		RemediationRevisionRowID: 42,
	}
	selected.ReplacementRevisionRowID = remediationReplacementRevisionRowID(selected.ReopeningKind, workflowstore.DeliveryTicketRevision{ID: selected.RemediationRevisionRowID})
	if selected.ReplacementRevisionRowID != nil {
		t.Fatalf("separate remediation ticket replacement revision row ID = %d, want absent", *selected.ReplacementRevisionRowID)
	}
}
