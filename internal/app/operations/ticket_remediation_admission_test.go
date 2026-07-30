package operations

import (
	"context"
	"errors"
	"strings"
	"testing"

	apptickets "relay/internal/app/tickets"
)

type remediationTicketOwner struct {
	*apptickets.Service
	publishCalls int
}

func (o *remediationTicketOwner) Publish(ctx context.Context, input apptickets.PublishInput) (apptickets.PublishedRevision, error) {
	o.publishCalls++
	return o.Service.Publish(ctx, input)
}

func remediationPublishInput(fixture remediationLifecycleFixture) apptickets.PublishInput {
	return apptickets.PublishInput{
		WorkspaceID: fixture.workspace.WorkspaceID, TicketID: fixture.ticket.TicketID, ExternalPriority: fixture.ticket.ExternalPriority,
		ExpectedRevisionNumber: fixture.revision.RevisionNumber, RemediationSeedID: fixture.seed.RemediationSeedID,
		Revision: apptickets.RevisionInput{
			RepoTarget: "project", Branch: "main", BaseCommit: fixture.closure.CommitOID, SourceClosureRowID: fixture.closure.ID,
			SourcePath: "tickets/remediation-authored.json", Goal: "Retain the exact caller-authored remediation revision.",
			Context: "The remediation packet authoring context is retained and verified.", TransitionApplicability: "required",
			CanonicalJSON: []byte(`{"ticket":"remediation","caller":"exact"}`), RenderedMarkdown: []byte("# Exact remediation\n"),
			Members:      []apptickets.RevisionMemberInput{{Kind: "scope_in", Path: "internal/app/tickets/service.go", Text: "Preserve the audited behavior."}},
			Dependencies: []apptickets.DependencyInput{{RevisionRowID: fixture.revision.ID, Outcome: "satisfied"}},
		},
	}
}

func TestTicketWorkflowRemediationPublicationUsesExactPlannerAuthoringPacket(t *testing.T) {
	fixture := newRemediationLifecycleFixture(t)
	planner, err := fixture.service.Create(fixture.ctx, CreateLifecycleInput{MutationID: "remediation-authoring", Identity: remediationIdentity(fixture)})
	if err != nil {
		t.Fatal(err)
	}
	packets, err := NewService(fixture.store)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := apptickets.NewService(fixture.store)
	if err != nil {
		t.Fatal(err)
	}
	workflow, err := NewTicketWorkflowService(packets, owner)
	if err != nil {
		t.Fatal(err)
	}
	input := remediationPublishInput(fixture)
	result, err := workflow.Publish(fixture.ctx, input, &RemediationAuthoringReference{
		PacketID: planner.Packet.Summary.PacketID, ExpectedPacketSHA256: planner.Packet.Summary.PacketSHA256,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.RemediationReopening == nil || result.RemediationReopening.ReopeningRevisionRowID != result.Revision.ID ||
		result.RemediationReopening.ReopeningKind != "replacement_ticket_revision" {
		t.Fatalf("remediation reopening = %#v", result.RemediationReopening)
	}
	if result.Ticket.CurrentRevisionRowID.Int64 != result.Revision.ID {
		t.Fatalf("current revision = %#v, want %d", result.Ticket.CurrentRevisionRowID, result.Revision.ID)
	}
}

func TestTicketWorkflowRemediationPublicationRejectsWrongOrUnexpectedAuthoringReference(t *testing.T) {
	fixture := newRemediationLifecycleFixture(t)
	planner, err := fixture.service.Create(fixture.ctx, CreateLifecycleInput{MutationID: "remediation-authoring-reject", Identity: remediationIdentity(fixture)})
	if err != nil {
		t.Fatal(err)
	}
	packets, err := NewService(fixture.store)
	if err != nil {
		t.Fatal(err)
	}
	ticketService, err := apptickets.NewService(fixture.store)
	if err != nil {
		t.Fatal(err)
	}
	owner := &remediationTicketOwner{Service: ticketService}
	workflow, err := NewTicketWorkflowService(packets, owner)
	if err != nil {
		t.Fatal(err)
	}
	input := remediationPublishInput(fixture)
	before, err := fixture.store.ListDeliveryTicketRevisions(fixture.ctx, fixture.ticket.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := workflow.Publish(fixture.ctx, input, &RemediationAuthoringReference{PacketID: planner.Packet.Summary.PacketID, ExpectedPacketSHA256: strings.Repeat("f", 64)}); !errors.Is(err, ErrTicketAdmission) {
		t.Fatalf("wrong packet SHA error = %v", err)
	}
	if owner.publishCalls != 0 {
		t.Fatalf("wrong authoring packet reached owner %d times", owner.publishCalls)
	}
	if after, err := fixture.store.ListDeliveryTicketRevisions(fixture.ctx, fixture.ticket.ID); err != nil || len(after) != len(before) {
		t.Fatalf("wrong authoring packet changed revisions: %#v err=%v", after, err)
	}
	ordinary := input
	ordinary.RemediationSeedID = ""
	if _, err := workflow.Publish(fixture.ctx, ordinary, &RemediationAuthoringReference{PacketID: planner.Packet.Summary.PacketID, ExpectedPacketSHA256: planner.Packet.Summary.PacketSHA256}); !errors.Is(err, ErrTicketAdmission) {
		t.Fatalf("ordinary publication accepted remediation reference: %v", err)
	}
}
