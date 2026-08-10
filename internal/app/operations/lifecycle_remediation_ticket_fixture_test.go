package operations

import (
	"database/sql"
	"fmt"
	"testing"

	apptickets "relay/internal/app/tickets"
	workflowstore "relay/internal/store/workflow"
)

func TestLifecycleRemediationDirectReplacementRetainsReplacedRevisionIdentity(t *testing.T) {
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

func TestLifecycleRemediationSeparateTicketOmitsReplacementRevisionIdentity(t *testing.T) {
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

// remediationTicketPublication carries the published, approved, and selected
// remediation Delivery Ticket revision. The Ticket Design Brief never
// participates: the selected approved Ticket is the sole package basis.
type remediationTicketPublication struct {
	result       apptickets.PublishedRevision
	approval     workflowstore.DeliveryTicketRevisionApproval
	selection    apptickets.SelectionResult
	canonical    []byte
	rendered     []byte
	members      []apptickets.RevisionMemberInput
	dependencies []apptickets.DependencyInput
}

func publishRemediationTicket(t *testing.T, fixture remediationLifecycleFixture, directReplacement bool) remediationTicketPublication {
	return publishRemediationTicketWithDependencies(t, fixture, directReplacement, nil)
}

func publishRemediationTicketWithDependencies(t *testing.T, fixture remediationLifecycleFixture, directReplacement bool, dependencies []apptickets.DependencyInput) remediationTicketPublication {
	t.Helper()
	service, err := apptickets.NewService(fixture.store)
	if err != nil {
		t.Fatal(err)
	}
	ticketID := "TICKET-REMEDIATION-SEPARATE"
	expectedRevisionNumber := int64(0)
	if directReplacement {
		ticketID = fixture.ticket.TicketID
		expectedRevisionNumber = fixture.revision.RevisionNumber
	}
	goal := "Retain the exact remediation ticket."
	contextText := "The remediation ticket uses a fresh, zero-dependency ticket publication."
	revisionNumber := expectedRevisionNumber + 1
	replacesRevision := "null"
	if directReplacement {
		revisionNumber = 2
		replacesRevision = "1"
	}
	dependencyJSON := ""
	for index, dependency := range dependencies {
		if index > 0 {
			dependencyJSON += ","
		}
		dependencyTicket, err := fixture.store.GetDeliveryTicketRevisionByRowID(fixture.ctx, dependency.RevisionRowID)
		if err != nil {
			t.Fatal(err)
		}
		dependencyOwner, err := fixture.store.GetDeliveryTicketByRowID(fixture.ctx, dependencyTicket.DeliveryTicketRowID)
		if err != nil {
			t.Fatal(err)
		}
		dependencyJSON += fmt.Sprintf(`{"ticket_id":%q,"revision":%d}`, dependencyOwner.TicketID, dependencyTicket.RevisionNumber)
	}
	canonical := []byte(fmt.Sprintf(`{"schema_version":"2.0","feature_slug":"remediation","ticket_id":%q,"revision":%d,"replaces_revision":%s,"repo_target":"project","branch":"main","base_commit":%q,"goal":%q,"context":%q,"scope":{"in_scope":["Exact remediation ticket."],"out_of_scope":["Unrelated work."]},"depends_on":[%s],"required_invariants":["The remediation package binds the exact approved Ticket."],"forbidden_behaviors":[],"implementation_obligations":[{"source_area":"internal/app/operations/lifecycle_prepare.go","obligation":"Preserve the exact remediation materialization.","prerequisites":[]}],"proof_obligations":["Prove every retained remediation input byte-for-byte."],"validation_commands":[{"working_directory":"","command":"go test ./internal/app/operations","expected":"all tests pass"}],"transition_applicability":"not_required","explicit_deferrals":[],"completion_criteria":["The exact remediation package is prepared."]}`, ticketID, revisionNumber, replacesRevision, fixture.closure.CommitOID, goal, contextText, dependencyJSON))
	rendered := []byte(fmt.Sprintf("# Remediation: %s\n\nExact caller-authored markdown.\n", ticketID))
	members := []apptickets.RevisionMemberInput{
		{Kind: "implementation_obligation", Path: "internal/app/operations/lifecycle_prepare.go", Text: "Preserve the exact remediation materialization."},
		{Kind: "validation_intent", Path: "internal/app/operations/lifecycle_remediation_package_test.go", Text: "Verify every retained remediation input byte-for-byte."},
	}
	sourcePath := fmt.Sprintf("tickets/%s.ticket-%s.r%d.delivery-ticket.json", fixture.workspace.FeatureSlug, ticketID, revisionNumber)
	publish := apptickets.PublishInput{
		WorkspaceID: fixture.workspace.WorkspaceID, TicketID: ticketID, ExternalPriority: 37,
		ExpectedRevisionNumber: expectedRevisionNumber, RemediationSeedID: fixture.seed.RemediationSeedID,
		Revision: apptickets.RevisionInput{
			RepoTarget: "project", Branch: "main", BaseCommit: fixture.closure.CommitOID, SourceClosureRowID: fixture.closure.ID,
			SourcePath: sourcePath, Goal: goal,
			Context: contextText, TransitionApplicability: "not_required",
			CanonicalJSON: canonical, RenderedMarkdown: rendered,
			Members:      members,
			Dependencies: dependencies,
		},
	}
	result, err := service.Publish(fixture.ctx, publish)
	if err != nil {
		t.Fatal(err)
	}
	if result.RemediationReopening == nil {
		t.Fatal("remediation seed was not consumed")
	}
	for _, dependency := range dependencies {
		if dependency.Outcome != "satisfied" {
			return remediationTicketPublication{result: result, canonical: canonical, rendered: rendered, members: members, dependencies: dependencies}
		}
	}
	approval, err := service.Approve(fixture.ctx, apptickets.ApproveInput{
		TicketID: result.Ticket.TicketID, RevisionRowID: result.Revision.ID, AuthorityRevisionID: fixture.authority.AuthorityRevisionID,
		Rationale: "Approve the exact remediation publication.",
	})
	if err != nil {
		t.Fatal(err)
	}
	selection, err := service.Select(fixture.ctx, apptickets.SelectInput{
		WorkspaceID: fixture.workspace.WorkspaceID, TicketID: result.Ticket.TicketID, RevisionRowID: result.Revision.ID,
		Rationale: "Select the exact remediation revision.",
	})
	if err != nil {
		t.Fatal(err)
	}
	return remediationTicketPublication{result: result, approval: approval, selection: selection, canonical: canonical, rendered: rendered, members: members, dependencies: dependencies}
}
