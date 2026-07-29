package operations

import (
	"bytes"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	appackages "relay/internal/app/packages"
	apptickets "relay/internal/app/tickets"
	"relay/internal/mcp/semanticidentity"
	"relay/internal/operations/packet"
	workflowstore "relay/internal/store/workflow"
	"relay/internal/testfixtures"
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
	result       apptickets.PublishedRevision
	approval     workflowstore.DeliveryTicketRevisionApproval
	selection    apptickets.SelectionResult
	canonical    []byte
	rendered     []byte
	members      []apptickets.RevisionMemberInput
	dependencies []apptickets.DependencyInput
}

func publishRemediationBriefTicket(t *testing.T, fixture remediationLifecycleFixture, directReplacement bool) remediationBriefPublication {
	return publishRemediationBriefTicketWithDependencies(t, fixture, directReplacement, nil)
}

func publishRemediationBriefTicketWithDependencies(t *testing.T, fixture remediationLifecycleFixture, directReplacement bool, dependencies []apptickets.DependencyInput) remediationBriefPublication {
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
	goal := "Retain the exact remediation brief ticket."
	contextText := "The remediation brief uses a fresh, zero-dependency ticket publication."
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
	canonical := []byte(fmt.Sprintf(`{"schema_version":"1.0","feature_slug":"remediation","ticket_id":%q,"revision":%d,"replaces_revision":%s,"repo_target":"project","branch":"main","base_commit":%q,"goal":%q,"context":%q,"scope":{"in_scope":["Exact remediation brief ticket."],"out_of_scope":["Unrelated work."]},"depends_on":[%s],"implementation_obligations":[{"path":"internal/app/operations/lifecycle_prepare.go","obligation":"Preserve the exact remediation materialization."}],"validation_intent":["Verify every retained remediation input byte-for-byte."],"transition_applicability":"not_required","completion_criteria":["The exact remediation package is prepared."]}`, ticketID, revisionNumber, replacesRevision, fixture.closure.CommitOID, goal, contextText, dependencyJSON))
	rendered := []byte(fmt.Sprintf("# Remediation brief: %s\n\nExact caller-authored markdown.\n", ticketID))
	members := []apptickets.RevisionMemberInput{
		{Kind: "implementation_obligation", Path: "internal/app/operations/lifecycle_prepare.go", Text: "Preserve the exact remediation materialization."},
		{Kind: "validation_intent", Path: "internal/app/operations/lifecycle_remediation_brief_test.go", Text: "Verify every retained remediation input byte-for-byte."},
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
			return remediationBriefPublication{result: result, canonical: canonical, rendered: rendered, members: members, dependencies: dependencies}
		}
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
	return remediationBriefPublication{result: result, approval: approval, selection: selection, canonical: canonical, rendered: rendered, members: members, dependencies: dependencies}
}

func createCompletedRemediationDependency(t *testing.T, fixture remediationLifecycleFixture) workflowstore.DeliveryTicketRevision {
	t.Helper()
	ctx := fixture.ctx
	ticketService, err := apptickets.NewService(fixture.store)
	if err != nil {
		t.Fatal(err)
	}
	canonical := []byte(`{"schema_version":"1.0","feature_slug":"remediation","ticket_id":"TICKET-REMEDIATION-DEPENDENCY","revision":1,"replaces_revision":null,"repo_target":"project","branch":"main","base_commit":"` + fixture.closure.CommitOID + `","goal":"Complete the remediation dependency.","context":"The dependency has an accepted audit outcome.","scope":{"in_scope":["Dependency completion."],"out_of_scope":["Unrelated work."]},"depends_on":[],"implementation_obligations":[],"validation_intent":["Verify dependency completion."],"transition_applicability":"not_required","completion_criteria":["The dependency is accepted."],"cancellation":null}`)
	published, err := ticketService.Publish(ctx, apptickets.PublishInput{
		WorkspaceID: fixture.workspace.WorkspaceID, TicketID: "TICKET-REMEDIATION-DEPENDENCY", ExternalPriority: 5,
		Revision: apptickets.RevisionInput{RepoTarget: "project", Branch: "main", BaseCommit: fixture.closure.CommitOID, SourceClosureRowID: fixture.closure.ID, SourcePath: "tickets/remediation-dependency.delivery-ticket.json", Goal: "Complete the remediation dependency.", Context: "The dependency has an accepted audit outcome.", TransitionApplicability: "not_required", CanonicalJSON: canonical, RenderedMarkdown: []byte("# Remediation dependency\n"), Members: []apptickets.RevisionMemberInput{{Kind: "scope_in", Path: "internal/app/operations", Text: "Complete dependency."}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ticketService.Approve(ctx, apptickets.ApproveInput{TicketID: published.Ticket.TicketID, RevisionRowID: published.Revision.ID, AuthorityRevisionID: fixture.authority.AuthorityRevisionID, Rationale: "Approve the completed remediation dependency."}); err != nil {
		t.Fatal(err)
	}
	selection, err := ticketService.Select(ctx, apptickets.SelectInput{WorkspaceID: fixture.workspace.WorkspaceID, TicketID: published.Ticket.TicketID, RevisionRowID: published.Revision.ID, Rationale: "Select the remediation dependency."})
	if err != nil {
		t.Fatal(err)
	}
	packageService, err := appackages.NewService(fixture.store)
	if err != nil {
		t.Fatal(err)
	}
	brief := []byte(testfixtures.TicketDesignBrief)
	briefName := fmt.Sprintf("%s.ticket-%s.r1.design-brief.md", fixture.workspace.FeatureSlug, published.Ticket.TicketID)
	prepared, err := packageService.Prepare(ctx, appackages.PrepareInput{SelectionID: selection.Selection.SelectionID, TicketDesignBrief: appackages.ArtifactInput{DisplayName: briefName, ExpectedSHA256: lifecycleSHA(brief), Bytes: brief}})
	if err != nil {
		t.Fatal(err)
	}
	approved, err := packageService.Approve(ctx, appackages.ApproveInput{PackageID: prepared.Package.PackageID, ExpectedPackageSha256: prepared.Package.PackageSha256, OperatorConfirmationEvidence: "Approve the dependency package for audit completion."})
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		current, err := tx.GetRunByRunID(ctx, approved.Run.RunID)
		if err != nil {
			return err
		}
		for _, transition := range [][2]string{{workflowstore.RunStatusSetupReady, workflowstore.RunStatusExecuting}, {workflowstore.RunStatusExecuting, workflowstore.RunStatusValidating}, {workflowstore.RunStatusValidating, workflowstore.RunStatusAuditReady}} {
			current, err = tx.TransitionRun(ctx, current.RunID, transition[0], transition[1])
			if err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	artifact := createRemediationArtifact(t, fixture.store, ctx, approved.Run.ID, "dependency-audit", "audit_packet", []byte(`{"audit":"accepted dependency"}`))
	if err := fixture.store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		packet, err := tx.CreateAuditPacket(ctx, workflowstore.CreateAuditPacketParams{AuditPacketID: "packet-remediation-dependency", RunRowID: approved.Run.ID, ImplementationActorKind: "applier", ArtifactRowID: artifact.ID, BaseCommit: fixture.closure.CommitOID, AuditedCommit: fixture.closure.CommitOID, PacketSHA256: artifact.SHA256})
		if err != nil {
			return err
		}
		members, err := tx.ListExecutionPackageMembers(ctx, prepared.Package.ID)
		if err != nil || len(members) != 1 {
			return fmt.Errorf("dependency package member unavailable: %w", err)
		}
		packageApproval, err := tx.GetExecutionPackageApprovalByPackageRowID(ctx, prepared.Package.ID)
		if err != nil {
			return err
		}
		obligation, err := tx.CreateAuditPacketTicketObligation(ctx, workflowstore.CreateAuditPacketTicketObligationParams{AuditPacketRowID: packet.ID, ExecutionPackageRowID: prepared.Package.ID, ExecutionPackageMemberRowID: members[0].ID, DeliveryTicketRowID: published.Ticket.ID, DeliveryTicketRevisionRowID: published.Revision.ID, AuthorityRevisionRowID: fixture.authority.ID, SourceClosureRowID: fixture.closure.ID, PackageApprovalRowID: sql.NullInt64{Int64: packageApproval.ID, Valid: true}, ApprovedPackageSha256: sql.NullString{String: prepared.Package.PackageSha256, Valid: true}})
		if err != nil {
			return err
		}
		decision, err := tx.CreateAuditDecision(ctx, workflowstore.CreateAuditDecisionParams{AuditDecisionID: "audit-remediation-dependency", RunRowID: approved.Run.ID, AuditPacketArtifactRowID: artifact.ID, AuditedCommit: fixture.closure.CommitOID, PacketSHA256: packet.PacketSHA256, Decision: workflowstore.AuditDecisionAccepted, Rationale: "The remediation dependency was accepted."})
		if err != nil {
			return err
		}
		revisionDecision, err := tx.CreateAuditTicketRevisionDecision(ctx, workflowstore.CreateAuditTicketRevisionDecisionParams{AuditDecisionRowID: decision.ID, AuditPacketTicketObligationRowID: obligation.ID, PackageApprovalRowID: sql.NullInt64{Int64: packageApproval.ID, Valid: true}, ApprovedPackageSha256: sql.NullString{String: prepared.Package.PackageSha256, Valid: true}})
		if err != nil {
			return err
		}
		_, err = tx.CreateDeliveryTicketRevisionSatisfaction(ctx, workflowstore.CreateDeliveryTicketRevisionSatisfactionParams{DeliveryTicketRevisionRowID: published.Revision.ID, AuditTicketRevisionDecisionRowID: revisionDecision.ID})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return published.Revision
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

func remediationBriefRefreshIdentity(fixture remediationLifecycleFixture, packetID string) semanticidentity.RefreshOperationPacket {
	return semanticidentity.RefreshOperationPacket{
		SurfaceContract:  "planner-authoring.v1",
		ExpectedPacketID: packetID,
		WorkflowReferences: []semanticidentity.WorkflowReferenceRequest{{
			Kind: "audit_decision", RunID: fixture.run.RunID, AuditDecisionID: fixture.decision.AuditDecisionID,
		}},
	}
}

func TestLifecycleRemediationBriefRefreshSameDurableStateRebuildsAllRetainedInputs(t *testing.T) {
	fixture := newRemediationLifecycleFixture(t)
	publication := publishRemediationBriefTicket(t, fixture, false)
	created, err := fixture.service.Create(fixture.ctx, CreateLifecycleInput{MutationID: "create-remediation-brief-same-state", Identity: remediationBriefIdentity(fixture)})
	if err != nil {
		t.Fatal(err)
	}
	packetService, err := NewService(fixture.store)
	if err != nil {
		t.Fatal(err)
	}
	document, err := decodeCanonicalAuthoringPacket(created.Packet.DocumentBytes, created.Packet.Summary.PacketSHA256)
	if err != nil {
		t.Fatal(err)
	}
	before := assertVerifiedRemediationBriefInputs(t, fixture, packetService, created.Packet, document)
	refreshed, err := fixture.service.Refresh(fixture.ctx, RefreshLifecycleInput{
		MutationID:    "refresh-remediation-brief-same-state",
		PriorPacketID: created.Packet.Summary.PacketID,
		Identity:      remediationBriefRefreshIdentity(fixture, created.Packet.Summary.PacketID),
	})
	if err != nil {
		t.Fatal(err)
	}
	refreshedDocument, err := decodeCanonicalAuthoringPacket(refreshed.Packet.DocumentBytes, refreshed.Packet.Summary.PacketSHA256)
	if err != nil {
		t.Fatal(err)
	}
	after := assertVerifiedRemediationBriefInputs(t, fixture, packetService, refreshed.Packet, refreshedDocument)
	for _, name := range []string{"remediation_seed", "selected_remediation_ticket", "completed_dependency_outcomes", "current_approved_authority"} {
		if !bytes.Equal(before[name], after[name]) {
			t.Fatalf("same-state refresh changed %s", name)
		}
	}
	if refreshed.Prior.LifecycleState != workflowstore.OperationPacketLifecycleSuperseded || refreshed.Prior.ReplacementPacket == nil || refreshed.Prior.ReplacementPacket.PacketID != refreshed.Packet.Summary.PacketID || refreshed.Packet.Summary.PacketID == created.Packet.Summary.PacketID {
		t.Fatalf("refresh lifecycle = %#v", refreshed)
	}
	oldIntegrity, err := fixture.store.GetOperationPacketPublicationIntegrity(fixture.ctx, viewPublicationID(t, fixture, created.Packet))
	if err != nil {
		t.Fatal(err)
	}
	newIntegrity, err := fixture.store.GetOperationPacketPublicationIntegrity(fixture.ctx, viewPublicationID(t, fixture, refreshed.Packet))
	if err != nil {
		t.Fatal(err)
	}
	oldIDs, newIDs := map[string]bool{}, map[string]bool{}
	for _, artifact := range oldIntegrity.RetainedArtifacts {
		oldIDs[artifact.ArtifactID] = true
	}
	for _, artifact := range newIntegrity.RetainedArtifacts {
		newIDs[artifact.ArtifactID] = true
	}
	for id := range oldIDs {
		if newIDs[id] {
			t.Fatalf("same-state refresh reused retained artifact identity %q", id)
		}
	}
	_ = publication
}

func TestLifecycleRemediationBriefCompletedDependencyRetainsExactAuditCompletionIdentity(t *testing.T) {
	fixture := newRemediationLifecycleFixture(t)
	dependencyRevision := createCompletedRemediationDependency(t, fixture)
	publication := publishRemediationBriefTicketWithDependencies(t, fixture, false, []apptickets.DependencyInput{{RevisionRowID: dependencyRevision.ID, Outcome: "satisfied"}})
	created, err := fixture.service.Create(fixture.ctx, CreateLifecycleInput{MutationID: "create-remediation-brief-completed-dependency", Identity: remediationBriefIdentity(fixture)})
	if err != nil {
		t.Fatal(err)
	}
	document, err := decodeCanonicalAuthoringPacket(created.Packet.DocumentBytes, created.Packet.Summary.PacketSHA256)
	if err != nil {
		t.Fatal(err)
	}
	packetService, err := NewService(fixture.store)
	if err != nil {
		t.Fatal(err)
	}
	inputs := assertVerifiedRemediationBriefInputs(t, fixture, packetService, created.Packet, document)
	assertSelectedRemediationTicket(t, fixture, inputs["selected_remediation_ticket"], publication, false)
	assertCompletedRemediationDependencies(t, fixture, inputs["completed_dependency_outcomes"], publication)
}

func TestLifecycleRemediationBriefRejectsDependencyAndCallerSuppliedDerivedStateAtomically(t *testing.T) {
	for _, test := range []struct {
		name        string
		dependency  string
		callerInput string
	}{
		{name: "unsatisfied dependency", dependency: "blocked"},
		{name: "caller supplied completed dependency outcomes", callerInput: "completed_dependency_outcomes"},
		{name: "caller supplied current approved authority", callerInput: "current_approved_authority"},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newRemediationLifecycleFixture(t)
			var publication remediationBriefPublication
			if test.dependency != "" {
				dependency := createCompletedRemediationDependency(t, fixture)
				publication = publishRemediationBriefTicketWithDependencies(t, fixture, false, []apptickets.DependencyInput{{RevisionRowID: dependency.ID, Outcome: test.dependency}})
			} else {
				publication = publishRemediationBriefTicket(t, fixture, false)
			}
			identity := remediationBriefIdentity(fixture)
			if test.callerInput != "" {
				identity.Inputs = []semanticidentity.InputBinding{{InputName: test.callerInput, SourceKind: "inline_text", DisplayName: test.callerInput + ".json", MediaType: "application/json", ExpectedSHA256: strings.Repeat("a", 64), Source: semanticidentity.InputBindingSource{Text: "{}"}}}
			}
			before := remediationStateSnapshot(t, fixture)
			if _, err := fixture.service.Create(fixture.ctx, CreateLifecycleInput{MutationID: "reject-remediation-brief-" + strings.ReplaceAll(test.name, " ", "-"), Identity: identity}); err == nil {
				t.Fatal("invalid remediation brief request was accepted")
			}
			if after := remediationStateSnapshot(t, fixture); !reflect.DeepEqual(before, after) {
				t.Fatalf("failed remediation brief create changed state")
			}
			_ = publication
		})
	}
}

func TestLifecycleRemediationBriefRefreshRejectsStaleDependencyAtomically(t *testing.T) {
	fixture := newRemediationLifecycleFixture(t)
	dependencyRevision := createCompletedRemediationDependency(t, fixture)
	publication := publishRemediationBriefTicketWithDependencies(t, fixture, false, []apptickets.DependencyInput{{RevisionRowID: dependencyRevision.ID, Outcome: "satisfied"}})
	created, err := fixture.service.Create(fixture.ctx, CreateLifecycleInput{MutationID: "create-remediation-brief-stale-dependency", Identity: remediationBriefIdentity(fixture)})
	if err != nil {
		t.Fatal(err)
	}
	ticketService, err := apptickets.NewService(fixture.store)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ticketService.Publish(fixture.ctx, apptickets.PublishInput{WorkspaceID: fixture.workspace.WorkspaceID, TicketID: "TICKET-REMEDIATION-DEPENDENCY", ExpectedRevisionNumber: 1, ExternalPriority: 5, Revision: apptickets.RevisionInput{RepoTarget: "project", Branch: "main", BaseCommit: fixture.closure.CommitOID, SourceClosureRowID: fixture.closure.ID, SourcePath: "tickets/remediation-dependency-v2.delivery-ticket.json", Goal: "Complete the remediation dependency again.", Context: "The dependency advanced after packet creation.", TransitionApplicability: "not_required", CanonicalJSON: []byte(`{"dependency":"advanced"}`), RenderedMarkdown: []byte("# Advanced dependency\n"), Members: []apptickets.RevisionMemberInput{{Kind: "scope_in", Path: "internal/app/operations", Text: "Advance dependency."}}}}); err != nil {
		t.Fatal(err)
	}
	before := remediationStateSnapshot(t, fixture)
	if _, err := fixture.service.Refresh(fixture.ctx, RefreshLifecycleInput{MutationID: "refresh-remediation-brief-stale-dependency", PriorPacketID: created.Packet.Summary.PacketID, Identity: remediationBriefRefreshIdentity(fixture, created.Packet.Summary.PacketID)}); err == nil {
		t.Fatal("refresh accepted an advanced dependency")
	}
	if after := remediationStateSnapshot(t, fixture); !reflect.DeepEqual(before, after) {
		t.Fatal("failed stale-dependency refresh changed state")
	}
	_ = publication
}

func TestLifecycleRemediationBriefCreateRejectsStaleSelectionAtomically(t *testing.T) {
	for _, test := range []struct {
		name  string
		other bool
	}{
		{name: "missing active selection"},
		{name: "active selection for another revision", other: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newRemediationLifecycleFixture(t)
			publication := publishRemediationBriefTicket(t, fixture, false)
			consumeRemediationBriefSelection(t, fixture, publication)
			if test.other {
				ticketService, err := apptickets.NewService(fixture.store)
				if err != nil {
					t.Fatal(err)
				}
				other, err := ticketService.Publish(fixture.ctx, apptickets.PublishInput{WorkspaceID: fixture.workspace.WorkspaceID, TicketID: "TICKET-REMEDIATION-OTHER", ExternalPriority: 1, Revision: apptickets.RevisionInput{RepoTarget: "project", Branch: "main", BaseCommit: fixture.closure.CommitOID, SourceClosureRowID: fixture.closure.ID, SourcePath: "tickets/remediation-other.ticket-TICKET-REMEDIATION-OTHER.r1.delivery-ticket.json", Goal: "Select another revision.", Context: "The active selection is intentionally different.", TransitionApplicability: "not_required", CanonicalJSON: []byte(`{"schema_version":"1.0","feature_slug":"remediation","ticket_id":"TICKET-REMEDIATION-OTHER","revision":1,"replaces_revision":null,"repo_target":"project","branch":"main","base_commit":"` + fixture.closure.CommitOID + `","goal":"Select another revision.","context":"The active selection is intentionally different.","scope":{"in_scope":["Selection."],"out_of_scope":["Other work."]},"depends_on":[],"implementation_obligations":[{"path":"internal/app/operations","obligation":"Select another revision."}],"validation_intent":["Verify selection rejection."],"transition_applicability":"not_required","completion_criteria":["Selection is explicit."]}`), RenderedMarkdown: []byte("# Other revision\n"), Members: []apptickets.RevisionMemberInput{{Kind: "implementation_obligation", Path: "internal/app/operations", Text: "Select another revision."}}}})
				if err != nil {
					t.Fatal(err)
				}
				if _, err := ticketService.Approve(fixture.ctx, apptickets.ApproveInput{TicketID: other.Ticket.TicketID, RevisionRowID: other.Revision.ID, AuthorityRevisionID: fixture.authority.AuthorityRevisionID, Rationale: "Approve the other revision."}); err != nil {
					t.Fatal(err)
				}
				if _, err := ticketService.Select(fixture.ctx, apptickets.SelectInput{WorkspaceID: fixture.workspace.WorkspaceID, TicketID: other.Ticket.TicketID, RevisionRowID: other.Revision.ID, Rationale: "Select the other revision."}); err != nil {
					t.Fatal(err)
				}
			}
			before := remediationStateSnapshot(t, fixture)
			if _, err := fixture.service.Create(fixture.ctx, CreateLifecycleInput{MutationID: "reject-remediation-brief-stale-selection-" + strings.ReplaceAll(test.name, " ", "-"), Identity: remediationBriefIdentity(fixture)}); err == nil {
				t.Fatal("stale selection was accepted")
			}
			if after := remediationStateSnapshot(t, fixture); !reflect.DeepEqual(before, after) {
				t.Fatal("failed stale-selection create changed state")
			}
		})
	}
}

func TestLifecycleRemediationBriefRefreshRejectsAdvancedTicketAtomically(t *testing.T) {
	fixture := newRemediationLifecycleFixture(t)
	publication := publishRemediationBriefTicket(t, fixture, false)
	created, err := fixture.service.Create(fixture.ctx, CreateLifecycleInput{MutationID: "create-remediation-brief-advanced-ticket", Identity: remediationBriefIdentity(fixture)})
	if err != nil {
		t.Fatal(err)
	}
	ticketService, err := apptickets.NewService(fixture.store)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ticketService.Publish(fixture.ctx, apptickets.PublishInput{WorkspaceID: fixture.workspace.WorkspaceID, TicketID: publication.result.Ticket.TicketID, ExpectedRevisionNumber: 1, ExternalPriority: publication.result.Ticket.ExternalPriority, Revision: apptickets.RevisionInput{RepoTarget: "project", Branch: "main", BaseCommit: fixture.closure.CommitOID, SourceClosureRowID: fixture.closure.ID, SourcePath: "tickets/remediation-advanced.ticket-TICKET-REMEDIATION-BRIEF-SEPARATE.r2.delivery-ticket.json", Goal: "Advance the remediation ticket.", Context: "The remediation ticket advanced after packet creation.", TransitionApplicability: "not_required", CanonicalJSON: []byte(`{"schema_version":"1.0","feature_slug":"remediation","ticket_id":"TICKET-REMEDIATION-BRIEF-SEPARATE","revision":2,"replaces_revision":1,"repo_target":"project","branch":"main","base_commit":"` + fixture.closure.CommitOID + `","goal":"Advance the remediation ticket.","context":"The remediation ticket advanced after packet creation.","scope":{"in_scope":["Advance."],"out_of_scope":["Other work."]},"depends_on":[],"implementation_obligations":[{"path":"internal/app/operations","obligation":"Advance the remediation ticket."}],"validation_intent":["Verify stale refresh rejection."],"transition_applicability":"not_required","completion_criteria":["The advanced ticket is current."]}`), RenderedMarkdown: []byte("# Advanced remediation\n"), Members: []apptickets.RevisionMemberInput{{Kind: "implementation_obligation", Path: "internal/app/operations", Text: "Advance the remediation ticket."}}}}); err != nil {
		t.Fatal(err)
	}
	before := remediationStateSnapshot(t, fixture)
	if _, err := fixture.service.Refresh(fixture.ctx, RefreshLifecycleInput{MutationID: "refresh-remediation-brief-advanced-ticket", PriorPacketID: created.Packet.Summary.PacketID, Identity: remediationBriefRefreshIdentity(fixture, created.Packet.Summary.PacketID)}); err == nil {
		t.Fatal("advanced remediation ticket was accepted")
	}
	if after := remediationStateSnapshot(t, fixture); !reflect.DeepEqual(before, after) {
		t.Fatal("failed advanced-ticket refresh changed state")
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
	if len(document.ManifestDomain.Members) != 2 || manifestMemberPath(t, document.ManifestDomain.Members[0]) != "contracts/cross-cutting.md" || manifestMemberPath(t, document.ManifestDomain.Members[1]) != "contracts/ticket-design-brief.md" {
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

func manifestMemberPath(t *testing.T, member packet.ManifestMember) string {
	t.Helper()
	value, err := base64.StdEncoding.Strict().DecodeString(member.Path.PathBytesBase64)
	if err != nil {
		t.Fatalf("manifest member path %q: %v", member.Path.PathBytesBase64, err)
	}
	return string(value)
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
		selected.WorkspaceID != fixture.workspace.WorkspaceID || selected.ExternalPriority != remediationTicket.ExternalPriority || selected.RepoTarget != remediationRevision.RepoTarget || selected.Branch != remediationRevision.Branch || selected.BaseCommit != remediationRevision.BaseCommit ||
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
	if selected.Dependencies == nil || len(selected.Dependencies) != len(publication.dependencies) {
		t.Fatalf("selected dependencies = %#v", selected.Dependencies)
	}
	for index, dependency := range publication.dependencies {
		if selected.Dependencies[index].Sequence != int64(index+1) || selected.Dependencies[index].DependencyRevisionRowID != dependency.RevisionRowID || selected.Dependencies[index].DependencyOutcome != dependency.Outcome {
			t.Fatalf("selected dependency %d = %#v, want revision %d outcome %q", index, selected.Dependencies[index], dependency.RevisionRowID, dependency.Outcome)
		}
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
	if err != nil || len(selections) < 2 {
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
	if err != nil || !bytes.Equal(canonical, data) || document.RemediationTicketID != publication.result.Ticket.TicketID || document.RemediationRevisionRowID != publication.result.Revision.ID || document.RemediationRevisionNumber != publication.result.Revision.RevisionNumber || document.Dependencies == nil || len(document.Dependencies) != len(publication.dependencies) {
		t.Fatalf("completed dependencies = %#v", document)
	}
	for index, dependency := range publication.dependencies {
		dependencyRevision, err := fixture.store.GetDeliveryTicketRevisionByRowID(fixture.ctx, dependency.RevisionRowID)
		if err != nil {
			t.Fatal(err)
		}
		dependencyTicket, err := fixture.store.GetDeliveryTicketByRowID(fixture.ctx, dependencyRevision.DeliveryTicketRowID)
		if err != nil {
			t.Fatal(err)
		}
		satisfaction, err := fixture.store.GetDeliveryTicketRevisionSatisfaction(fixture.ctx, dependencyRevision.ID)
		if err != nil {
			t.Fatal(err)
		}
		got := document.Dependencies[index]
		if got.Sequence != int64(index+1) || got.DependencyTicketID != dependencyTicket.TicketID || got.DependencyRevisionRowID != dependencyRevision.ID || got.DependencyRevisionNumber != dependencyRevision.RevisionNumber || got.DeclaredOutcome != dependency.Outcome || got.CurrentDependencyRevision.TicketID != dependencyTicket.TicketID || got.CurrentDependencyRevision.RevisionRowID != dependencyRevision.ID || got.CurrentDependencyRevision.RevisionNumber != dependencyRevision.RevisionNumber || got.Completion.SatisfactionRowID != satisfaction.ID || got.Completion.AuditTicketRevisionDecisionRowID != satisfaction.AuditTicketRevisionDecisionRowID {
			t.Fatalf("completed dependency %d = %#v", index, got)
		}
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
