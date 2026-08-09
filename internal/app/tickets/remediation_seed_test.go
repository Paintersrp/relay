package tickets

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	workflowstore "relay/internal/store/workflow"
)

func TestPublishExplicitRemediationSeedConsumption(t *testing.T) {
	t.Run("replacement revision", func(t *testing.T) {
		ctx := context.Background()
		store, workspaceID, closure, _ := ticketFixture(t)
		createCompletionDecision(t, ctx, store, workspaceID, closure.ID)
		fixture := createRemediationSeedFixture(t, ctx, store, workspaceID, closure, "replacement")
		service, err := NewService(store)
		if err != nil {
			t.Fatal(err)
		}

		input := publishInput(workspaceID, fixture.ticket.TicketID, 61, 1, closure, "replacement-content", "")
		input.RemediationSeedID = fixture.seed.RemediationSeedID
		before := capturePublishState(t, ctx, store)
		result, err := service.Publish(ctx, input)
		if err != nil {
			t.Fatal(err)
		}
		if result.RemediationReopening == nil {
			t.Fatal("replacement did not return remediation reopening")
		}
		if result.RemediationReopening.ReopeningKind != "replacement_ticket_revision" ||
			result.RemediationReopening.ReopeningRevisionRowID != result.Revision.ID {
			t.Fatalf("replacement remediation reopening = %#v", result.RemediationReopening)
		}
		if !result.Revision.ReplacesRevisionRowID.Valid || result.Revision.ReplacesRevisionRowID.Int64 != fixture.revision.ID {
			t.Fatalf("replacement lineage = %#v", result.Revision)
		}
		assertPublishedArtifacts(t, store, result, input.Revision.CanonicalJSON, input.Revision.RenderedMarkdown)
		assertSeedImmutable(t, ctx, store, fixture)
		assertCounts(t, store, map[string]int{
			"delivery_tickets":                        before.counts["delivery_tickets"],
			"delivery_ticket_revisions":               before.counts["delivery_ticket_revisions"] + 1,
			"audit_remediation_seed_reopenings":       1,
			"feature_workspace_completion_reopenings": 1,
		})
		completion, err := currentCompletionReopening(t, ctx, store)
		if err != nil {
			t.Fatal(err)
		}
		if completion.ReopeningKind != "ticket_revision" || !completion.ReopeningTicketRevisionRowID.Valid || completion.ReopeningTicketRevisionRowID.Int64 != result.Revision.ID {
			t.Fatalf("completion reopening = %#v", completion)
		}
		seedReopening, err := store.GetAuditRemediationSeedReopening(ctx, fixture.seed.ID)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(seedReopening, *result.RemediationReopening) {
			t.Fatalf("returned seed reopening = %#v, stored = %#v", *result.RemediationReopening, seedReopening)
		}

		afterSuccess := capturePublishState(t, ctx, store)
		second := input
		second.ExpectedRevisionNumber = 2
		second.Revision.Goal = "second replacement must be rejected"
		second.Revision.CanonicalJSON = []byte(`{"ticket":"` + fixture.ticket.TicketID + `","revision":"second"}`)
		if _, err := service.Publish(ctx, second); !errors.Is(err, ErrRemediationSeed) {
			t.Fatalf("reused replacement seed error = %v", err)
		}
		assertPublishStateEqual(t, ctx, store, afterSuccess)

		remediation := publishInput(workspaceID, "P5-REMEDIATION", 62, 0, closure, "reused-remediation", "")
		remediation.RemediationSeedID = fixture.seed.RemediationSeedID
		if _, err := service.Publish(ctx, remediation); !errors.Is(err, ErrRemediationSeed) {
			t.Fatalf("reused remediation seed error = %v", err)
		}
		assertPublishStateEqual(t, ctx, store, afterSuccess)
	})

	t.Run("explicit remediation ticket", func(t *testing.T) {
		ctx := context.Background()
		store, workspaceID, closure, _ := ticketFixture(t)
		fixture := createRemediationSeedFixture(t, ctx, store, workspaceID, closure, "remediation")
		service, err := NewService(store)
		if err != nil {
			t.Fatal(err)
		}
		before := capturePublishState(t, ctx, store)
		input := publishInput(workspaceID, "P5-REMEDIATION", 63, 0, closure, "explicit-remediation", "")
		input.RemediationSeedID = fixture.seed.RemediationSeedID
		input.Revision.RepoTarget = "relay"
		input.Revision.Branch = "main"
		input.Revision.BaseCommit = closure.CommitOID
		input.Revision.SourcePath = "remediation/explicit-remediation.json"
		input.Revision.Goal = "Publish the caller-authored remediation ticket."
		input.Revision.Context = "The remediation caller supplies the complete revision content."
		input.Revision.TransitionApplicability = "required"
		input.Revision.Members = []RevisionMemberInput{
			{Kind: "scope_in", Path: "internal/app/tickets/service.go", Text: "Preserve the audited ticket while publishing remediation."},
			{Kind: "validation_intent", Path: "internal/app/tickets/remediation_seed_test.go", Text: "Verify caller-authored members and dependencies."},
		}
		input.Revision.Dependencies = []DependencyInput{{RevisionRowID: fixture.revision.ID, Outcome: "satisfied"}}
		auditedTicketBefore, err := store.GetDeliveryTicketByRowID(ctx, fixture.ticket.ID)
		if err != nil {
			t.Fatal(err)
		}
		auditedRevisionBefore, err := store.GetDeliveryTicketRevisionByRowID(ctx, fixture.revision.ID)
		if err != nil {
			t.Fatal(err)
		}
		auditedRevisionsBefore, err := store.ListDeliveryTicketRevisions(ctx, auditedTicketBefore.ID)
		if err != nil {
			t.Fatal(err)
		}
		auditedCurrentRevisionBefore := auditedTicketBefore.CurrentRevisionRowID
		ticketsBefore, err := store.ListDeliveryTicketsByWorkspace(ctx, auditedTicketBefore.WorkspaceRowID)
		if err != nil {
			t.Fatal(err)
		}
		result, err := service.Publish(ctx, input)
		if err != nil {
			t.Fatal(err)
		}
		if result.RemediationReopening == nil || result.RemediationReopening.ReopeningKind != "remediation_ticket" || result.RemediationReopening.ReopeningRevisionRowID != result.Revision.ID {
			t.Fatalf("remediation ticket reopening = %#v", result.RemediationReopening)
		}
		if result.Ticket.TicketID != input.TicketID || result.Revision.RevisionNumber != 1 {
			t.Fatalf("remediation ticket result = %#v", result)
		}
		if result.Revision.ReplacesRevisionRowID.Valid {
			t.Fatalf("new remediation revision unexpectedly replaces a revision: %#v", result.Revision.ReplacesRevisionRowID)
		}
		assertRevisionMatchesInput(t, result.Revision, input.Revision)
		members, err := store.ListDeliveryTicketRevisionMembers(ctx, result.Revision.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(members) != len(input.Revision.Members) {
			t.Fatalf("remediation revision members = %d, want %d", len(members), len(input.Revision.Members))
		}
		for index, member := range input.Revision.Members {
			stored := members[index]
			if stored.RevisionRowID != result.Revision.ID || stored.Sequence != int64(index+1) || stored.MemberKind != member.Kind || !reflect.DeepEqual(stored.MemberPath, nullableString(member.Path)) || stored.MemberText != member.Text {
				t.Fatalf("remediation revision member %d = %#v, want sequence %d kind %q path %q text %q", index, stored, index+1, member.Kind, member.Path, member.Text)
			}
		}
		dependencies, err := store.ListDeliveryTicketRevisionDependencies(ctx, result.Revision.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(dependencies) != len(input.Revision.Dependencies) {
			t.Fatalf("remediation revision dependencies = %d, want %d", len(dependencies), len(input.Revision.Dependencies))
		}
		for index, dependency := range input.Revision.Dependencies {
			stored := dependencies[index]
			if stored.RevisionRowID != result.Revision.ID || stored.Sequence != int64(index+1) || stored.DependsOnRevisionRowID != dependency.RevisionRowID || stored.Outcome != dependency.Outcome {
				t.Fatalf("remediation revision dependency %d = %#v, want sequence %d revision %d outcome %q", index, stored, index+1, dependency.RevisionRowID, dependency.Outcome)
			}
		}
		if before.counts["delivery_tickets"]+1 != countTable(t, store, "delivery_tickets") || before.counts["delivery_ticket_revisions"]+1 != countTable(t, store, "delivery_ticket_revisions") {
			t.Fatal("explicit remediation did not create exactly one ticket revision")
		}
		auditedTicketAfter, err := store.GetDeliveryTicketByRowID(ctx, fixture.ticket.ID)
		if err != nil {
			t.Fatal(err)
		}
		auditedRevisionAfter, err := store.GetDeliveryTicketRevisionByRowID(ctx, fixture.revision.ID)
		if err != nil {
			t.Fatal(err)
		}
		auditedRevisionsAfter, err := store.ListDeliveryTicketRevisions(ctx, auditedTicketAfter.ID)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(auditedTicketAfter, auditedTicketBefore) || !reflect.DeepEqual(auditedRevisionAfter, auditedRevisionBefore) || !reflect.DeepEqual(auditedTicketAfter.CurrentRevisionRowID, auditedCurrentRevisionBefore) || !reflect.DeepEqual(auditedRevisionsAfter, auditedRevisionsBefore) {
			t.Fatalf("audited ticket changed: before ticket=%#v revision=%#v revisions=%#v, after ticket=%#v revision=%#v revisions=%#v", auditedTicketBefore, auditedRevisionBefore, auditedRevisionsBefore, auditedTicketAfter, auditedRevisionAfter, auditedRevisionsAfter)
		}
		if !auditedTicketAfter.CurrentRevisionRowID.Valid || auditedTicketAfter.CurrentRevisionRowID.Int64 != fixture.revision.ID {
			t.Fatalf("audited ticket current revision = %#v, want %d", auditedTicketAfter.CurrentRevisionRowID, fixture.revision.ID)
		}
		if result.Ticket.ID == auditedTicketAfter.ID || result.Revision.DeliveryTicketRowID == auditedTicketAfter.ID {
			t.Fatal("remediation publication reused the audited ticket")
		}
		ticketsAfter, err := store.ListDeliveryTicketsByWorkspace(ctx, auditedTicketAfter.WorkspaceRowID)
		if err != nil {
			t.Fatal(err)
		}
		if len(ticketsAfter) != len(ticketsBefore)+1 {
			t.Fatalf("workspace ticket count after remediation = %d, want %d", len(ticketsAfter), len(ticketsBefore)+1)
		}
		beforeTicketIDs := make(map[int64]struct{}, len(ticketsBefore))
		for _, ticket := range ticketsBefore {
			beforeTicketIDs[ticket.ID] = struct{}{}
			found := false
			for _, after := range ticketsAfter {
				if ticket.ID == after.ID {
					found = true
					if !reflect.DeepEqual(ticket, after) {
						t.Fatalf("existing ticket changed: before=%#v after=%#v", ticket, after)
					}
					break
				}
			}
			if !found {
				t.Fatalf("existing ticket %d was removed", ticket.ID)
			}
		}
		for _, ticket := range ticketsAfter {
			if _, existed := beforeTicketIDs[ticket.ID]; !existed && ticket.ID != result.Ticket.ID {
				t.Fatalf("unexpected newly created ticket = %#v", ticket)
			}
		}
		assertPublishedArtifacts(t, store, result, input.Revision.CanonicalJSON, input.Revision.RenderedMarkdown)
		assertSeedImmutable(t, ctx, store, fixture)
		if countTable(t, store, "runs") != before.counts["runs"] || countTable(t, store, "plans") != before.counts["plans"] || countTable(t, store, "plan_passes") != before.counts["plan_passes"] {
			t.Fatal("remediation publication created execution work")
		}
	})

	t.Run("ordinary publication has no seed effect", func(t *testing.T) {
		ctx := context.Background()
		store, workspaceID, closure, _ := ticketFixture(t)
		fixture := createRemediationSeedFixture(t, ctx, store, workspaceID, closure, "ordinary")
		service, err := NewService(store)
		if err != nil {
			t.Fatal(err)
		}
		before := capturePublishState(t, ctx, store)
		input := publishInput(workspaceID, "P5-ORDINARY2", 64, 0, closure, "ordinary", "")
		result, err := service.Publish(ctx, input)
		if err != nil {
			t.Fatal(err)
		}
		if result.RemediationReopening != nil {
			t.Fatalf("ordinary publication returned remediation reopening = %#v", result.RemediationReopening)
		}
		if countTable(t, store, "audit_remediation_seed_reopenings") != before.counts["audit_remediation_seed_reopenings"] || countTable(t, store, "feature_workspace_completion_reopenings") != before.counts["feature_workspace_completion_reopenings"] {
			t.Fatal("ordinary publication changed remediation or completion reopening state")
		}
		assertSeedImmutable(t, ctx, store, fixture)
		if countTable(t, store, "runs") != before.counts["runs"] || countTable(t, store, "plans") != before.counts["plans"] || countTable(t, store, "plan_passes") != before.counts["plan_passes"] {
			t.Fatal("ordinary publication created execution work")
		}
	})
}

func TestPublishRemediationSeedRejectionMatrixIsAtomic(t *testing.T) {
	tests := []struct {
		name       string
		want       error
		withSecond bool
		input      func(string, remediationSeedFixture, workflowstore.SourceVaultClosure) PublishInput
	}{
		{name: "unknown seed", want: ErrRemediationSeed, input: func(workspace string, f remediationSeedFixture, closure workflowstore.SourceVaultClosure) PublishInput {
			input := publishInput(workspace, f.ticket.TicketID, 70, 1, closure, "unknown", "")
			input.RemediationSeedID = "remediation-unknown"
			return input
		}},
		{name: "outer whitespace", want: ErrInvalidTicket, input: func(workspace string, f remediationSeedFixture, closure workflowstore.SourceVaultClosure) PublishInput {
			input := publishInput(workspace, f.ticket.TicketID, 70, 1, closure, "whitespace", "")
			input.RemediationSeedID = " " + f.seed.RemediationSeedID
			return input
		}},
		{name: "not direct replacement", want: ErrRemediationSeed, withSecond: true, input: func(workspace string, f remediationSeedFixture, closure workflowstore.SourceVaultClosure) PublishInput {
			input := publishInput(workspace, f.ticket.TicketID, 70, 2, closure, "not-direct", "")
			input.RemediationSeedID = f.seed.RemediationSeedID
			return input
		}},
		{name: "cross workspace", want: ErrRemediationSeed, input: func(workspace string, f remediationSeedFixture, closure workflowstore.SourceVaultClosure) PublishInput {
			input := publishInput(workspace, "P5-CROSS", 70, 0, closure, "cross-workspace", "")
			input.RemediationSeedID = f.seed.RemediationSeedID
			return input
		}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			store, workspaceID, closure, _ := ticketFixture(t)
			createCompletionDecision(t, ctx, store, workspaceID, closure.ID)
			fixture := createRemediationSeedFixture(t, ctx, store, workspaceID, closure, strings.ReplaceAll(tc.name, " ", "-"))
			service, err := NewService(store)
			if err != nil {
				t.Fatal(err)
			}
			if tc.name == "cross workspace" {
				otherWorkspace, otherClosure := createOtherWorkspace(t, ctx, store)
				other := createRemediationSeedFixture(t, ctx, store, otherWorkspace, otherClosure, "other")
				fixture = other
			}
			if tc.withSecond {
				ordinary := publishInput(workspaceID, fixture.ticket.TicketID, 70, 1, closure, "intermediate", "")
				if _, err := service.Publish(ctx, ordinary); err != nil {
					t.Fatal(err)
				}
			}
			input := tc.input(workspaceID, fixture, closure)
			before := capturePublishState(t, ctx, store)
			if _, err := service.Publish(ctx, input); !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
			assertPublishStateEqual(t, ctx, store, before)
		})
	}

	t.Run("already consumed", func(t *testing.T) {
		ctx := context.Background()
		store, workspaceID, closure, _ := ticketFixture(t)
		fixture := createRemediationSeedFixture(t, ctx, store, workspaceID, closure, "consumed")
		service, err := NewService(store)
		if err != nil {
			t.Fatal(err)
		}
		first := publishInput(workspaceID, fixture.ticket.TicketID, 70, 1, closure, "first", "")
		first.RemediationSeedID = fixture.seed.RemediationSeedID
		if _, err := service.Publish(ctx, first); err != nil {
			t.Fatal(err)
		}
		before := capturePublishState(t, ctx, store)
		second := publishInput(workspaceID, fixture.ticket.TicketID, 70, 2, closure, "reused", "")
		second.RemediationSeedID = fixture.seed.RemediationSeedID
		if _, err := service.Publish(ctx, second); !errors.Is(err, ErrRemediationSeed) {
			t.Fatalf("already-consumed seed error = %v", err)
		}
		assertPublishStateEqual(t, ctx, store, before)
	})
}

type remediationSeedFixture struct {
	seed     workflowstore.AuditRemediationSeed
	finding  []workflowstore.AuditRemediationSeedFinding
	ticket   workflowstore.DeliveryTicket
	revision workflowstore.DeliveryTicketRevision
}

func createRemediationSeedFixture(t *testing.T, ctx context.Context, store *workflowstore.Store, workspaceID string, closure workflowstore.SourceVaultClosure, suffix string) remediationSeedFixture {
	t.Helper()
	workspace, err := store.GetFeatureWorkspaceByWorkspaceID(ctx, workspaceID)
	if err != nil {
		t.Fatal(err)
	}
	var fixture remediationSeedFixture
	if err := store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		fixture.ticket, err = tx.CreateDeliveryTicket(ctx, workflowstore.CreateDeliveryTicketParams{TicketID: "P5-" + strings.ToUpper(suffix[:min(len(suffix), 8)]), WorkspaceRowID: workspace.ID, ExternalPriority: 60})
		if err != nil {
			return err
		}
		fixture.revision, err = tx.CreateDeliveryTicketRevision(ctx, workflowstore.CreateDeliveryTicketRevisionParams{DeliveryTicketRowID: fixture.ticket.ID, RevisionNumber: 1, RepoTarget: "relay", Branch: "main", BaseCommit: closure.CommitOID, SourceClosureRowID: closure.ID, SourcePath: "tickets/" + strings.ToLower(suffix) + ".json", Goal: "Persist the audited ticket revision.", Context: "The package and audit packet bind exact immutable facts.", TransitionApplicability: "not_required"})
		if err != nil {
			return err
		}
		if _, err = tx.SetDeliveryTicketCurrentRevision(ctx, fixture.ticket.TicketID, fixture.revision.ID); err != nil {
			return err
		}
		approval, err := tx.CreateDeliveryTicketRevisionApproval(ctx, workflowstore.CreateDeliveryTicketRevisionApprovalParams{ApprovalID: "approval-" + suffix, RevisionRowID: fixture.revision.ID, ApprovalKind: "delivery", ApprovalState: "approved", Rationale: "Approved exact package ticket.", SourceClosureRowID: closure.ID, AuthorityRevisionRowID: sql.NullInt64{Int64: workspace.CurrentAuthorityRevisionRowID.Int64, Valid: true}})
		if err != nil {
			return err
		}
		selection, err := tx.CreateDeliveryTicketSelection(ctx, workflowstore.CreateDeliveryTicketSelectionParams{SelectionID: "selection-" + suffix, WorkspaceRowID: workspace.ID, State: "active", Rationale: "Select the exact audited ticket.", SourceClosureRowID: sql.NullInt64{Int64: closure.ID, Valid: true}})
		if err != nil {
			return err
		}
		selectionMember, err := tx.CreateDeliveryTicketSelectionMember(ctx, workflowstore.CreateDeliveryTicketSelectionMemberParams{SelectionRowID: selection.ID, Sequence: 1, RevisionRowID: fixture.revision.ID, ApprovalRowID: approval.ID})
		if err != nil {
			return err
		}
		packageRow, err := tx.CreateExecutionPackage(ctx, workflowstore.CreateExecutionPackageParams{PackageID: "package-" + suffix, SelectionRowID: selection.ID, WorkspaceRowID: workspace.ID, RepoTarget: "relay", Branch: "main", BaseCommit: closure.CommitOID, SourceClosureRowID: closure.ID, AuthorityRevisionRowID: workspace.CurrentAuthorityRevisionRowID.Int64, PackageSha256: strings.Repeat("1", 64), AuthoritySha256: strings.Repeat("2", 64), SourceSha256: strings.Repeat("3", 64), DesignBriefSha256: strings.Repeat("4", 64), DeterministicOperationsSha256: sql.NullString{String: strings.Repeat("5", 64), Valid: true}, DeterministicOperationsCoverage: sql.NullString{String: "complete", Valid: true}})
		if err != nil {
			return err
		}
		packageMember, err := tx.CreateExecutionPackageMember(ctx, workflowstore.CreateExecutionPackageMemberParams{PackageRowID: packageRow.ID, SelectionMemberRowID: selectionMember.ID, Sequence: 1, RevisionRowID: fixture.revision.ID, MemberSha256: strings.Repeat("6", 64)})
		if err != nil {
			return err
		}
		if _, err = tx.CreateExecutionPackageApprovalBinding(ctx, workflowstore.CreateExecutionPackageApprovalBindingParams{PackageRowID: packageRow.ID, PackageMemberRowID: packageMember.ID, ApprovalRowID: approval.ID, AuthorityRevisionRowID: workspace.CurrentAuthorityRevisionRowID.Int64, SourceClosureRowID: closure.ID, ApprovalBasisSha256: strings.Repeat("7", 64)}); err != nil {
			return err
		}
		if _, err = tx.ConsumeDeliveryTicketSelection(ctx, selection.SelectionID); err != nil {
			return err
		}
		packageApproval, err := tx.CreateExecutionPackageApproval(ctx, workflowstore.CreateExecutionPackageApprovalParams{ApprovalID: "pkg-approval-" + suffix, PackageRowID: packageRow.ID, PackageSha256: packageRow.PackageSha256, OperatorConfirmationEvidence: "Operator approved the exact immutable package."})
		if err != nil {
			return err
		}
		run, err := tx.CreateRun(ctx, workflowstore.CreateRunParams{RunID: "run-" + suffix, FeatureSlug: "ticket-" + suffix, RepoTarget: "relay", Status: workflowstore.RunStatusCreated, Branch: "main", BaseCommit: closure.CommitOID, CanonicalSHA256: strings.Repeat("8", 64)})
		if err != nil {
			return err
		}
		if _, err = tx.LinkRunToExecutionPackage(ctx, run.RunID, packageRow.ID); err != nil {
			return err
		}
		if _, err = tx.LinkRunToExecutionPackageApproval(ctx, workflowstore.LinkRunToExecutionPackageApprovalParams{PackageApprovalRowID: sql.NullInt64{Int64: packageApproval.ID, Valid: true}, RunID: run.RunID}); err != nil {
			return err
		}
		for _, transition := range [][2]string{{workflowstore.RunStatusCreated, workflowstore.RunStatusSetupReady}, {workflowstore.RunStatusSetupReady, workflowstore.RunStatusExecuting}, {workflowstore.RunStatusExecuting, workflowstore.RunStatusValidating}, {workflowstore.RunStatusValidating, workflowstore.RunStatusAuditReady}} {
			if _, err = tx.TransitionRun(ctx, run.RunID, transition[0], transition[1]); err != nil {
				return err
			}
		}
		artifact, err := tx.CreateArtifact(ctx, workflowstore.CreateArtifactParams{ArtifactID: "artifact-" + suffix, OwnerType: "run", RunRowID: sql.NullInt64{Int64: run.ID, Valid: true}, Kind: "audit_packet", RelativePath: "runs/" + run.RunID + "/audit-packet.json", MediaType: "application/json", SHA256: strings.Repeat("9", 64), SizeBytes: 1})
		if err != nil {
			return err
		}
		packet, err := tx.CreateAuditPacket(ctx, workflowstore.CreateAuditPacketParams{AuditPacketID: "packet-" + suffix, RunRowID: run.ID, ImplementationActorKind: "applier", ArtifactRowID: artifact.ID, BaseCommit: closure.CommitOID, AuditedCommit: strings.Repeat("d", 40), PacketSHA256: strings.Repeat("9", 64)})
		if err != nil {
			return err
		}
		obligation, err := tx.CreateAuditPacketTicketObligation(ctx, workflowstore.CreateAuditPacketTicketObligationParams{AuditPacketRowID: packet.ID, ExecutionPackageRowID: packageRow.ID, ExecutionPackageMemberRowID: packageMember.ID, DeliveryTicketRowID: fixture.ticket.ID, DeliveryTicketRevisionRowID: fixture.revision.ID, AuthorityRevisionRowID: workspace.CurrentAuthorityRevisionRowID.Int64, SourceClosureRowID: closure.ID, PackageApprovalRowID: sql.NullInt64{Int64: packageApproval.ID, Valid: true}, ApprovedPackageSha256: sql.NullString{String: packageRow.PackageSha256, Valid: true}})
		if err != nil {
			return err
		}
		decision, err := tx.CreateAuditDecision(ctx, workflowstore.CreateAuditDecisionParams{AuditDecisionID: "audit-" + suffix, RunRowID: run.ID, AuditPacketArtifactRowID: artifact.ID, AuditedCommit: packet.AuditedCommit, PacketSHA256: packet.PacketSHA256, Decision: workflowstore.AuditDecisionNeedsRevision, Rationale: "The package requires explicit ticket remediation."})
		if err != nil {
			return err
		}
		revisionDecision, err := tx.CreateAuditTicketRevisionDecision(ctx, workflowstore.CreateAuditTicketRevisionDecisionParams{AuditDecisionRowID: decision.ID, AuditPacketTicketObligationRowID: obligation.ID, PackageApprovalRowID: sql.NullInt64{Int64: packageApproval.ID, Valid: true}, ApprovedPackageSha256: sql.NullString{String: packageRow.PackageSha256, Valid: true}})
		if err != nil {
			return err
		}
		fixture.seed, err = tx.CreateAuditRemediationSeed(ctx, workflowstore.CreateAuditRemediationSeedParams{RemediationSeedID: "remediation-" + suffix, AuditTicketRevisionDecisionRowID: revisionDecision.ID, AuditPacketRowID: packet.ID, ExecutionPackageRowID: packageRow.ID, AuditedCommit: packet.AuditedCommit, DecisionRationale: decision.Rationale})
		if err != nil {
			return err
		}
		for sequence, classification := range []string{"implementation", "governing_package", "both"} {
			finding, findingErr := tx.CreateAuditRemediationSeedFinding(ctx, workflowstore.CreateAuditRemediationSeedFindingParams{RemediationSeedRowID: fixture.seed.ID, Sequence: int64(sequence + 1), UpstreamClassification: classification, Summary: "The package audit identified a durable remediation obligation.", Evidence: "The current package evidence records this exact finding.", RequiredRemediation: "Publish an explicit ticket revision addressing this finding."})
			if findingErr != nil {
				return findingErr
			}
			fixture.finding = append(fixture.finding, finding)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return fixture
}

func createCompletionDecision(t *testing.T, ctx context.Context, store *workflowstore.Store, workspaceID string, closureID int64) {
	t.Helper()
	workspace, err := store.GetFeatureWorkspaceByWorkspaceID(ctx, workspaceID)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		_, err := tx.CreateFeatureWorkspaceCompletionDecision(ctx, workflowstore.CreateFeatureWorkspaceCompletionDecisionParams{CompletionDecisionID: "completion-" + workspaceID, WorkspaceRowID: workspace.ID, AuthorityRevisionRowID: sql.NullInt64{Int64: workspace.CurrentAuthorityRevisionRowID.Int64, Valid: true}, SourceClosureRowID: sql.NullInt64{Int64: closureID, Valid: true}, Decision: "completed"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

func createOtherWorkspace(t *testing.T, ctx context.Context, store *workflowstore.Store) (string, workflowstore.SourceVaultClosure) {
	t.Helper()
	var workspaceID int64
	if err := store.DB().QueryRowContext(ctx, `INSERT INTO feature_workspaces (workspace_id, project_row_id, feature_slug) VALUES ('workspace-other', (SELECT id FROM projects WHERE project_id = 'project-ticket'), 'other') RETURNING id`).Scan(&workspaceID); err != nil {
		t.Fatal(err)
	}
	closure := addClosure(t, ctx, store, strings.Repeat("b", 40), "closure-other")
	setCurrentAuthority(t, ctx, store, "workspace-other", closure.ID, "authority-other")
	return "workspace-other", closure
}

type publishState struct {
	counts    map[string]int
	current   map[string]sql.NullInt64
	seeds     []workflowstore.AuditRemediationSeed
	findings  []workflowstore.AuditRemediationSeedFinding
	artifacts []string
	staging   []string
}

func capturePublishState(t *testing.T, ctx context.Context, store *workflowstore.Store) publishState {
	t.Helper()
	state := publishState{counts: map[string]int{}, current: map[string]sql.NullInt64{}}
	for _, table := range []string{"delivery_tickets", "delivery_ticket_revisions", "delivery_ticket_revision_members", "delivery_ticket_revision_dependencies", "feature_workspace_completion_reopenings", "audit_remediation_seed_reopenings", "runs", "plans", "plan_passes"} {
		state.counts[table] = countTable(t, store, table)
	}
	rows, err := store.DB().QueryContext(ctx, `SELECT ticket_id, current_revision_row_id FROM delivery_tickets ORDER BY ticket_id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var ticketID string
		var current sql.NullInt64
		if err := rows.Scan(&ticketID, &current); err != nil {
			t.Fatal(err)
		}
		state.current[ticketID] = current
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	seedRows, err := store.DB().QueryContext(ctx, `
SELECT id, remediation_seed_id, audit_ticket_revision_decision_row_id, audit_packet_row_id,
       execution_package_row_id, audited_commit, decision_rationale, created_at
FROM audit_remediation_seeds
ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	for seedRows.Next() {
		var seed workflowstore.AuditRemediationSeed
		if err := seedRows.Scan(&seed.ID, &seed.RemediationSeedID, &seed.AuditTicketRevisionDecisionRowID, &seed.AuditPacketRowID, &seed.ExecutionPackageRowID, &seed.AuditedCommit, &seed.DecisionRationale, &seed.CreatedAt); err != nil {
			seedRows.Close()
			t.Fatal(err)
		}
		state.seeds = append(state.seeds, seed)
	}
	if err := seedRows.Err(); err != nil {
		seedRows.Close()
		t.Fatal(err)
	}
	seedRows.Close()
	findingsRows, err := store.DB().QueryContext(ctx, `
SELECT id, remediation_seed_row_id, sequence, upstream_classification, summary,
       evidence, required_remediation, created_at
FROM audit_remediation_seed_findings
ORDER BY remediation_seed_row_id, sequence, id`)
	if err != nil {
		t.Fatal(err)
	}
	for findingsRows.Next() {
		var finding workflowstore.AuditRemediationSeedFinding
		if err := findingsRows.Scan(&finding.ID, &finding.RemediationSeedRowID, &finding.Sequence, &finding.UpstreamClassification, &finding.Summary, &finding.Evidence, &finding.RequiredRemediation, &finding.CreatedAt); err != nil {
			findingsRows.Close()
			t.Fatal(err)
		}
		state.findings = append(state.findings, finding)
	}
	if err := findingsRows.Err(); err != nil {
		findingsRows.Close()
		t.Fatal(err)
	}
	findingsRows.Close()
	state.artifacts = artifactTree(t, filepath.Join(store.ArtifactStore().Root(), "delivery-tickets"))
	state.staging = artifactTree(t, filepath.Join(store.ArtifactStore().Root(), ".staging"))
	return state
}
func assertPublishStateEqual(t *testing.T, ctx context.Context, store *workflowstore.Store, want publishState) {
	t.Helper()
	got := capturePublishState(t, ctx, store)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("publish state after failed seed consumption = %#v, want %#v", got, want)
	}
}

func assertCounts(t *testing.T, store *workflowstore.Store, want map[string]int) {
	t.Helper()
	for table, count := range want {
		if got := countTable(t, store, table); got != count {
			t.Fatalf("%s count = %d, want %d", table, got, count)
		}
	}
}

func countTable(t *testing.T, store *workflowstore.Store, table string) int {
	t.Helper()
	var count int
	if err := store.DB().QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func artifactTree(t *testing.T, root string) []string {
	t.Helper()
	entries := []string{}
	if _, err := os.Stat(root); errors.Is(err, os.ErrNotExist) {
		return entries
	}
	if err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if path == root {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		entries = append(entries, filepath.ToSlash(relative)+":"+string(rune(info.Mode().Perm())))
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	sort.Strings(entries)
	return entries
}

func assertRevisionMatchesInput(t *testing.T, revision workflowstore.DeliveryTicketRevision, input RevisionInput) {
	t.Helper()
	if revision.CancellationReason != nullableString(input.CancellationReason) ||
		revision.RepoTarget != input.RepoTarget ||
		revision.Branch != input.Branch ||
		revision.BaseCommit != input.BaseCommit ||
		revision.SourceClosureRowID != input.SourceClosureRowID ||
		revision.SourcePath != input.SourcePath ||
		revision.Goal != input.Goal ||
		revision.Context != input.Context ||
		revision.TransitionApplicability != input.TransitionApplicability {
		t.Fatalf("stored remediation revision = %#v, want caller input fields repo=%q branch=%q base=%q closure=%d path=%q goal=%q context=%q transition=%q cancellation=%q", revision, input.RepoTarget, input.Branch, input.BaseCommit, input.SourceClosureRowID, input.SourcePath, input.Goal, input.Context, input.TransitionApplicability, input.CancellationReason)
	}
}

func assertPublishedArtifacts(t *testing.T, store *workflowstore.Store, result PublishedRevision, canonical, rendered []byte) {
	t.Helper()
	for _, artifact := range []struct {
		stored StoredArtifact
		want   []byte
	}{
		{result.Canonical, canonical},
		{result.Rendered, rendered},
	} {
		got, err := os.ReadFile(filepath.Join(store.ArtifactStore().Root(), filepath.FromSlash(artifact.stored.RelativePath)))
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, artifact.want) {
			t.Fatalf("published artifact %s = %q, want %q", artifact.stored.RelativePath, got, artifact.want)
		}
	}
}

func assertSeedImmutable(t *testing.T, ctx context.Context, store *workflowstore.Store, fixture remediationSeedFixture) {
	t.Helper()
	seed, err := store.GetAuditRemediationSeedBySeedID(ctx, fixture.seed.RemediationSeedID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(seed, fixture.seed) {
		t.Fatalf("seed changed = %#v, want %#v", seed, fixture.seed)
	}
	findings, err := store.ListAuditRemediationSeedFindings(ctx, fixture.seed.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(findings, fixture.finding) {
		t.Fatalf("findings changed = %#v, want %#v", findings, fixture.finding)
	}
}

func currentCompletionReopening(t *testing.T, ctx context.Context, store *workflowstore.Store) (workflowstore.FeatureWorkspaceCompletionReopening, error) {
	t.Helper()
	var reopening workflowstore.FeatureWorkspaceCompletionReopening
	err := store.DB().QueryRowContext(ctx, `SELECT id, completion_decision_row_id, reopening_kind, reopening_ticket_revision_row_id, reopening_authority_revision_row_id, reopening_remediation_seed_row_id, created_at FROM feature_workspace_completion_reopenings LIMIT 1`).Scan(&reopening.ID, &reopening.CompletionDecisionRowID, &reopening.ReopeningKind, &reopening.ReopeningTicketRevisionRowID, &reopening.ReopeningAuthorityRevisionRowID, &reopening.ReopeningRemediationSeedRowID, &reopening.CreatedAt)
	return reopening, err
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
