package operations

import (
	"bytes"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	apptickets "relay/internal/app/tickets"
	"relay/internal/mcp/semanticidentity"
	"relay/internal/operations/packet"
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

type remediationBriefPublication struct {
	result    apptickets.PublishedRevision
	approval  workflowstore.DeliveryTicketRevisionApproval
	selection apptickets.SelectionResult
	canonical []byte
	rendered  []byte
	members   []apptickets.RevisionMemberInput
}

func publishRemediationBriefTicket(t *testing.T, fixture remediationLifecycleFixture, directReplacement bool) remediationBriefPublication {
	t.Helper()
	service, err := apptickets.NewService(fixture.store)
	if err != nil {
		t.Fatal(err)
	}
	ticketID := "TICKET-REMEDIATION-BRIEF-SEPARATE"
	expectedRevisionNumber := int64(0)
	if directReplacement {
		ticketID = fixture.ticket.TicketID
		expectedRevisionNumber = fixture.revision.RevisionNumber
	}
	canonical := []byte(fmt.Sprintf(`{"ticket_id":%q,"purpose":"remediation brief","member_count":2}`, ticketID))
	rendered := []byte(fmt.Sprintf("# Remediation brief: %s\n\nExact caller-authored markdown.\n", ticketID))
	members := []apptickets.RevisionMemberInput{
		{Kind: "scope_in", Path: "internal/app/operations/lifecycle_prepare.go", Text: "Preserve the exact remediation materialization."},
		{Kind: "validation_intent", Path: "internal/app/operations/lifecycle_remediation_brief_test.go", Text: "Verify every retained remediation input byte-for-byte."},
	}
	publish := apptickets.PublishInput{
		WorkspaceID: fixture.workspace.WorkspaceID, TicketID: ticketID, ExternalPriority: 37,
		ExpectedRevisionNumber: expectedRevisionNumber, RemediationSeedID: fixture.seed.RemediationSeedID,
		Revision: apptickets.RevisionInput{
			RepoTarget: "project", Branch: "main", BaseCommit: fixture.closure.CommitOID, SourceClosureRowID: fixture.closure.ID,
			SourcePath: "tickets/remediation-brief.delivery-ticket.json", Goal: "Retain the exact remediation brief ticket.",
			Context: "The remediation brief uses a fresh, zero-dependency ticket publication.", TransitionApplicability: "not_required",
			CanonicalJSON: canonical, RenderedMarkdown: rendered,
			Members:      members,
			Dependencies: []apptickets.DependencyInput{},
		},
	}
	result, err := service.Publish(fixture.ctx, publish)
	if err != nil {
		t.Fatal(err)
	}
	if result.RemediationReopening == nil {
		t.Fatal("remediation seed was not consumed")
	}
	approval, err := service.Approve(fixture.ctx, apptickets.ApproveInput{
		TicketID: result.Ticket.TicketID, RevisionRowID: result.Revision.ID, AuthorityRevisionID: fixture.authority.AuthorityRevisionID,
		Rationale: "Approve the exact remediation brief publication.",
	})
	if err != nil {
		t.Fatal(err)
	}
	selection, err := service.Select(fixture.ctx, apptickets.SelectInput{
		WorkspaceID: fixture.workspace.WorkspaceID, TicketID: result.Ticket.TicketID, RevisionRowID: result.Revision.ID,
		Rationale: "Select the exact remediation brief revision.",
	})
	if err != nil {
		t.Fatal(err)
	}
	return remediationBriefPublication{result: result, approval: approval, selection: selection, canonical: canonical, rendered: rendered, members: members}
}

func remediationBriefIdentity(fixture remediationLifecycleFixture) semanticidentity.CreateOperationPacket {
	return semanticidentity.CreateOperationPacket{
		SurfaceContract: "planner-authoring.v1", OperationID: "planner.ticket_design_brief_remediation", ProjectID: fixture.projectID,
		WorkflowReferences: []semanticidentity.WorkflowReferenceRequest{{Kind: "audit_decision", RunID: fixture.run.RunID, AuditDecisionID: fixture.decision.AuditDecisionID}},
	}
}

func TestLifecycleRemediationBriefCreatesVerifiedPacketsForBothTicketShapes(t *testing.T) {
	for _, test := range []struct {
		name   string
		direct bool
	}{
		{name: "direct replacement revision", direct: true},
		{name: "separate remediation Ticket"},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newRemediationLifecycleFixture(t)
			publication := publishRemediationBriefTicket(t, fixture, test.direct)
			before := remediationStateSnapshot(t, fixture)
			created, err := fixture.service.Create(fixture.ctx, CreateLifecycleInput{MutationID: "create-remediation-brief-" + strings.ReplaceAll(test.name, " ", "-"), Identity: remediationBriefIdentity(fixture)})
			if err != nil {
				t.Fatalf("create remediation brief: %v", err)
			}

			packetService, err := NewService(fixture.store)
			if err != nil {
				t.Fatal(err)
			}
			document, err := decodeCanonicalAuthoringPacket(created.Packet.DocumentBytes, created.Packet.Summary.PacketSHA256)
			if err != nil {
				t.Fatal(err)
			}
			assertRemediationBriefPacketContract(t, fixture, created.Packet, document, publication, test.direct)
			inputs := assertVerifiedRemediationBriefInputs(t, fixture, packetService, created.Packet, document)
			assertRemediationSeed(t, fixture, inputs["remediation_seed"])
			assertSelectedRemediationTicket(t, fixture, inputs["selected_remediation_ticket"], publication, test.direct)
			assertCompletedRemediationDependencies(t, fixture, inputs["completed_dependency_outcomes"], publication)
			assertCurrentAuthority(t, fixture, inputs["current_approved_authority"], fixture.authority, fixture.authorityLayers)
			assertNoRemediationBriefConversationData(t, inputs)
			assertRemediationBoundaryStable(t, before, remediationStateSnapshot(t, fixture))
		})
	}
}

func assertRemediationBriefPacketContract(t *testing.T, fixture remediationLifecycleFixture, view PacketView, document packet.Document, publication remediationBriefPublication, directReplacement bool) {
	t.Helper()
	if view.Summary.Role != "planner" || view.Summary.SurfaceContract != "planner-authoring.v1" || view.Summary.OperationID != "planner.ticket_design_brief_remediation" ||
		document.Role != "planner" || document.SurfaceContract != "planner-authoring.v1" || document.OperationID != "planner.ticket_design_brief_remediation" ||
		document.ManifestDomain.Domain != "ticket_design_brief" || document.Output.OutputKind != "ticket_design_brief_markdown" || document.Output.OutputPersistence != "chat_unrecorded" ||
		document.ReadinessState != "ready" || view.Summary.LifecycleState != workflowstore.OperationPacketLifecycleActive || len(document.AllowedActions) != 0 ||
		len(document.WorkflowReferences) != 1 || document.WorkflowReferences[0].Kind != "audit_decision" || document.WorkflowReferences[0].RunID != fixture.run.RunID ||
		document.WorkflowReferences[0].AuditDecisionID != fixture.decision.AuditDecisionID || document.WorkflowReferences[0].Decision != workflowstore.AuditDecisionNeedsRevision || len(document.Attestations) != 0 || len(document.Inputs) != 4 {
		t.Fatalf("remediation brief packet contract = %#v", document)
	}
	if len(document.ManifestDomain.Members) != 2 || !strings.HasSuffix(document.ManifestDomain.Members[0].Path.PathID, "contracts/cross-cutting.md") || !strings.HasSuffix(document.ManifestDomain.Members[1].Path.PathID, "contracts/ticket-design-brief.md") {
		t.Fatalf("manifest domain members = %#v", document.ManifestDomain.Members)
	}
	if directReplacement {
		if publication.result.Ticket.TicketID != fixture.ticket.TicketID || publication.result.Revision.ReplacesRevisionRowID.Int64 != fixture.revision.ID {
			t.Fatalf("direct replacement publication = %#v", publication.result)
		}
	} else if publication.result.Ticket.TicketID == fixture.ticket.TicketID {
		t.Fatalf("separate remediation publication reused audited Ticket: %#v", publication.result.Ticket)
	}
	if publication.result.Ticket.TicketID != map[bool]string{true: fixture.ticket.TicketID, false: "TICKET-REMEDIATION-BRIEF-SEPARATE"}[directReplacement] || publication.result.Revision.RevisionNumber != map[bool]int64{true: 2, false: 1}[directReplacement] {
		t.Fatalf("publication identity = %#v", publication.result)
	}
}

func assertVerifiedRemediationBriefInputs(t *testing.T, fixture remediationLifecycleFixture, service *Service, view PacketView, document packet.Document) map[string][]byte {
	t.Helper()
	wantNames := []string{"remediation_seed", "selected_remediation_ticket", "completed_dependency_outcomes", "current_approved_authority"}
	if len(document.Inputs) != len(wantNames) {
		t.Fatalf("input count = %d", len(document.Inputs))
	}
	integrity, err := fixture.store.GetOperationPacketPublicationIntegrity(fixture.ctx, viewPublicationID(t, fixture, view))
	if err != nil {
		t.Fatal(err)
	}
	seenArtifacts := map[string]bool{}
	values := make(map[string][]byte, len(wantNames))
	for index, input := range document.Inputs {
		if input.InputName != wantNames[index] || input.InputRole != "governing" || input.AttestationKind != "derived_authority" || input.SourceKind != "inline_text" || input.Source.Kind != "inline_text" || input.Source.ArtifactID == "" || input.MediaType != "application/json" {
			t.Fatalf("input %d = %#v", index, input)
		}
		if seenArtifacts[input.Source.ArtifactID] {
			t.Fatalf("duplicate retained artifact ID %q", input.Source.ArtifactID)
		}
		seenArtifacts[input.Source.ArtifactID] = true
		var binding workflowstore.OperationPacketArtifactBinding
		bindingCount := 0
		for _, candidate := range integrity.Bindings {
			if candidate.DependencyClass == workflowSnapshotDependency && candidate.DependencyKey == input.InputName {
				binding, bindingCount = candidate, bindingCount+1
			}
		}
		if bindingCount != 1 || !binding.RetainedArtifactRowID.Valid || binding.RetainedArtifactRowID.Int64 == 0 {
			t.Fatalf("input binding %q = %#v", input.InputName, binding)
		}
		retained, err := fixture.store.GetOperationPacketRetainedArtifactByRowID(fixture.ctx, binding.RetainedArtifactRowID.Int64)
		if err != nil {
			t.Fatal(err)
		}
		if retained.ArtifactID != input.Source.ArtifactID || retained.PublicationID != integrity.Publication.PublicationID || retained.Kind != workflowstore.OperationPacketRetainedArtifactWorkflowSnapshot || retained.MediaType != input.MediaType || retained.SHA256 != input.SHA256 || retained.SizeBytes != input.SizeBytes {
			t.Fatalf("retained row for %q = %#v", input.InputName, retained)
		}
		value, err := service.ReadVerifiedRetainedInput(fixture.ctx, view.Summary.PacketID, input.InputName)
		if err != nil {
			t.Fatal(err)
		}
		if lifecycleSHA(value) != input.SHA256 || int64(len(value)) != input.SizeBytes {
			t.Fatalf("verified input %q integrity failed", input.InputName)
		}
		values[input.InputName] = append([]byte(nil), value...)
		if len(value) == 0 {
			t.Fatal("verified input is empty")
		}
		value[0] ^= 0xff
		again, err := service.ReadVerifiedRetainedInput(fixture.ctx, view.Summary.PacketID, input.InputName)
		if err != nil || !bytes.Equal(again, values[input.InputName]) {
			t.Fatalf("verified input %q was not defensively copied: %q err=%v", input.InputName, again, err)
		}
	}
	if len(seenArtifacts) != 4 || lifecycleSHA(view.DocumentBytes) != view.Summary.PacketSHA256 || int64(len(view.DocumentBytes)) != view.DocumentSizeBytes {
		t.Fatalf("packet canonical integrity failed: summary=%#v", view.Summary)
	}
	return values
}

func assertSelectedRemediationTicket(t *testing.T, fixture remediationLifecycleFixture, data []byte, publication remediationBriefPublication, directReplacement bool) {
	t.Helper()
	var selected selectedRemediationTicketInput
	if err := json.Unmarshal(data, &selected); err != nil {
		t.Fatal(err)
	}
	canonical, err := canonicalJSON(selected)
	if err != nil || !bytes.Equal(canonical, data) {
		t.Fatal("selected remediation Ticket is not canonical")
	}
	remediationTicket, err := fixture.store.GetDeliveryTicketByTicketID(fixture.ctx, publication.result.Ticket.TicketID)
	if err != nil {
		t.Fatal(err)
	}
	remediationRevision, err := fixture.store.GetDeliveryTicketRevisionByRowID(fixture.ctx, publication.result.Revision.ID)
	if err != nil {
		t.Fatal(err)
	}
	auditedTicket, err := fixture.store.GetDeliveryTicketByRowID(fixture.ctx, fixture.ticket.ID)
	if err != nil {
		t.Fatal(err)
	}
	auditedRevision, err := fixture.store.GetDeliveryTicketRevisionByRowID(fixture.ctx, fixture.revision.ID)
	if err != nil {
		t.Fatal(err)
	}
	reopening, err := fixture.store.GetAuditRemediationSeedReopening(fixture.ctx, fixture.seed.ID)
	if err != nil {
		t.Fatal(err)
	}
	if selected.RemediationSeedID != fixture.seed.RemediationSeedID || selected.AuditDecisionID != fixture.decision.AuditDecisionID || selected.ReopeningKind != reopening.ReopeningKind ||
		selected.AuditedTicketID != auditedTicket.TicketID || selected.AuditedRevisionRowID != auditedRevision.ID || selected.AuditedRevisionNumber != auditedRevision.RevisionNumber ||
		selected.RemediationTicketID != remediationTicket.TicketID || selected.RemediationRevisionRowID != remediationRevision.ID || selected.RemediationRevisionNumber != remediationRevision.RevisionNumber ||
		selected.WorkspaceID != fixture.workspace.WorkspaceID || selected.ExternalPriority != remediationTicket.ExternalPriority || selected.ExternalPriority != 37 || selected.RepoTarget != remediationRevision.RepoTarget || selected.Branch != remediationRevision.Branch || selected.BaseCommit != remediationRevision.BaseCommit ||
		selected.SourceClosureRowID != remediationRevision.SourceClosureRowID || selected.SourceClosureID != fixture.closure.ClosureID || selected.SourceClosureCommit != fixture.closure.CommitOID || selected.SourcePath != remediationRevision.SourcePath || selected.Goal != remediationRevision.Goal || selected.Context != remediationRevision.Context || selected.TransitionApplicability != remediationRevision.TransitionApplicability {
		t.Fatalf("selected remediation Ticket identity = %#v", selected)
	}
	if remediationRevision.CancellationReason.Valid {
		if selected.CancellationReason != remediationRevision.CancellationReason.String {
			t.Fatalf("cancellation reason = %q", selected.CancellationReason)
		}
	} else if selected.CancellationReason != "" {
		t.Fatalf("cancellation reason = %q", selected.CancellationReason)
	}
	if directReplacement {
		if selected.RemediationTicketID != selected.AuditedTicketID || selected.ReplacementRevisionRowID == nil || *selected.ReplacementRevisionRowID != auditedRevision.ID || *selected.ReplacementRevisionRowID == remediationRevision.ID || !remediationRevision.ReplacesRevisionRowID.Valid || remediationRevision.ReplacesRevisionRowID.Int64 != auditedRevision.ID {
			t.Fatalf("direct replacement identity = %#v revision=%#v", selected, remediationRevision)
		}
	} else {
		if selected.RemediationTicketID == selected.AuditedTicketID || selected.ReplacementRevisionRowID != nil || auditedTicket.CurrentRevisionRowID.Int64 != fixture.revision.ID {
			t.Fatalf("separate remediation identity = %#v audited=%#v", selected, auditedTicket)
		}
		revisions, err := fixture.store.ListDeliveryTicketRevisions(fixture.ctx, fixture.ticket.ID)
		if err != nil || len(revisions) != 1 {
			t.Fatalf("audited Ticket revisions = %#v err=%v", revisions, err)
		}
	}
	assertRemediationBriefArtifacts(t, fixture, selected, publication)
	assertRemediationBriefApprovalAndSelection(t, fixture, selected, remediationRevision, publication)
	members, err := fixture.store.ListDeliveryTicketRevisionMembers(fixture.ctx, remediationRevision.ID)
	if err != nil || len(members) != len(publication.members) {
		t.Fatalf("Ticket members = %#v err=%v", members, err)
	}
	if len(selected.Members) != len(publication.members) {
		t.Fatalf("selected Ticket members = %#v", selected.Members)
	}
	for index, want := range publication.members {
		got := selected.Members[index]
		if got.Sequence != int64(index+1) || got.Kind != want.Kind || got.Path != want.Path || got.Text != want.Text || members[index].Sequence != int64(index+1) || members[index].MemberKind != want.Kind || !members[index].MemberPath.Valid || members[index].MemberPath.String != want.Path || members[index].MemberText != want.Text {
			t.Fatalf("Ticket member %d = %#v durable=%#v", index, got, members[index])
		}
	}
	if selected.Dependencies == nil || len(selected.Dependencies) != 0 {
		t.Fatalf("selected dependencies = %#v", selected.Dependencies)
	}
}

func assertRemediationBriefArtifacts(t *testing.T, fixture remediationLifecycleFixture, selected selectedRemediationTicketInput, publication remediationBriefPublication) {
	t.Helper()
	for _, value := range []struct {
		artifact selectedRemediationTicketArtifact
		bytes    []byte
		suffix   string
	}{
		{artifact: selected.Canonical, bytes: publication.canonical, suffix: "delivery-ticket.json"},
		{artifact: selected.Rendered, bytes: publication.rendered, suffix: "delivery-ticket.md"},
	} {
		if !strings.HasSuffix(value.artifact.RelativePath, value.suffix) || value.artifact.SHA256 != lifecycleSHA(value.bytes) || value.artifact.SizeBytes != int64(len(value.bytes)) {
			t.Fatalf("retained Ticket artifact = %#v", value.artifact)
		}
		decoded, err := base64.StdEncoding.Strict().DecodeString(value.artifact.BytesBase64)
		if err != nil || !bytes.Equal(decoded, value.bytes) {
			t.Fatalf("retained Ticket artifact bytes = %q err=%v", decoded, err)
		}
		stored, err := os.ReadFile(filepath.Join(fixture.store.ArtifactStore().Root(), filepath.FromSlash(value.artifact.RelativePath)))
		if err != nil || !bytes.Equal(stored, value.bytes) || lifecycleSHA(stored) != value.artifact.SHA256 || int64(len(stored)) != value.artifact.SizeBytes {
			t.Fatalf("stored Ticket artifact %q = %q err=%v", value.artifact.RelativePath, stored, err)
		}
	}
	if selected.Canonical.RelativePath == selected.Rendered.RelativePath || selected.Canonical.SHA256 == selected.Rendered.SHA256 {
		t.Fatal("canonical and rendered Ticket artifacts must differ")
	}
}

func assertRemediationBriefApprovalAndSelection(t *testing.T, fixture remediationLifecycleFixture, selected selectedRemediationTicketInput, revision workflowstore.DeliveryTicketRevision, publication remediationBriefPublication) {
	t.Helper()
	approvals, err := fixture.store.ListDeliveryTicketRevisionApprovals(fixture.ctx, revision.ID)
	if err != nil || len(approvals) != 1 || approvals[0].ID != publication.approval.ID {
		t.Fatalf("approvals = %#v err=%v", approvals, err)
	}
	approval := approvals[0]
	if selected.Approval.ApprovalRowID != approval.ID || selected.Approval.ApprovalID != approval.ApprovalID || selected.Approval.AuthorityRevisionRowID != fixture.authority.ID || selected.Approval.SourceClosureRowID != approval.SourceClosureRowID || approval.AuthorityRevisionRowID.Int64 != fixture.authority.ID || approval.SourceClosureRowID != fixture.closure.ID {
		t.Fatalf("approval = %#v selected=%#v", approval, selected.Approval)
	}
	selections, err := fixture.store.ListDeliveryTicketSelectionsByWorkspace(fixture.ctx, fixture.workspace.ID)
	if err != nil || len(selections) != 2 {
		t.Fatalf("selections = %#v err=%v", selections, err)
	}
	var selection workflowstore.DeliveryTicketSelection
	for _, candidate := range selections {
		if candidate.ID == publication.selection.Selection.ID {
			selection = candidate
		}
	}
	if selection.ID == 0 || selected.Selection.SelectionRowID != selection.ID || selected.Selection.SelectionID != selection.SelectionID || selected.Selection.State != selection.State || selected.Selection.SourceClosureRowID != selection.SourceClosureRowID.Int64 || selection.State != "active" || selection.SourceClosureRowID.Int64 != fixture.closure.ID {
		t.Fatalf("selection = %#v selected=%#v", selection, selected.Selection)
	}
	members, err := fixture.store.ListDeliveryTicketSelectionMembers(fixture.ctx, selection.ID)
	if err != nil || len(members) != 1 {
		t.Fatalf("selection members = %#v err=%v", members, err)
	}
	member := members[0]
	if selected.Selection.MemberRowID != member.ID || selected.Selection.MemberSequence != member.Sequence || selected.Selection.MemberRevisionRowID != member.RevisionRowID || selected.Selection.MemberApprovalRowID != member.ApprovalRowID || member.Sequence != 1 || member.RevisionRowID != revision.ID || member.ApprovalRowID != approval.ID {
		t.Fatalf("selection member = %#v selected=%#v", member, selected.Selection)
	}
}

func assertCompletedRemediationDependencies(t *testing.T, fixture remediationLifecycleFixture, data []byte, publication remediationBriefPublication) {
	t.Helper()
	var document completedDependencyOutcomesInput
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	canonical, err := canonicalJSON(document)
	if err != nil || !bytes.Equal(canonical, data) || document.RemediationTicketID != publication.result.Ticket.TicketID || document.RemediationRevisionRowID != publication.result.Revision.ID || document.RemediationRevisionNumber != publication.result.Revision.RevisionNumber || document.Dependencies == nil || len(document.Dependencies) != 0 {
		t.Fatalf("completed dependencies = %#v", document)
	}
}

func assertNoRemediationBriefConversationData(t *testing.T, values map[string][]byte) {
	t.Helper()
	for name, data := range values {
		assertRemediationBriefJSONIsolation(t, name, data)
	}
}

func assertRemediationBriefJSONIsolation(t *testing.T, name string, data []byte) {
	t.Helper()
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatal(err)
	}
	forbidden := []string{"execution attempt transcript", "attempt messages", "execution evidence", "validation stdout", "validation stderr", "effective executor brief", "deterministic application trace", "previous planner conversation", "auditor conversation", "prior operation-packet document"}
	var visit func(any)
	visit = func(current any) {
		switch current := current.(type) {
		case map[string]any:
			for key, child := range current {
				lower := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(key, "_", " "), "-", " "))
				for _, banned := range forbidden {
					if lower == banned {
						t.Fatalf("%s contains forbidden field %q", name, key)
					}
				}
				visit(child)
			}
		case []any:
			for _, child := range current {
				visit(child)
			}
		}
	}
	visit(value)
	lowerData := strings.ToLower(string(data))
	for _, banned := range forbidden {
		if strings.Contains(lowerData, strings.ReplaceAll(banned, " ", "_")) {
			t.Fatalf("%s contains supplementary forbidden text %q", name, banned)
		}
	}
}
