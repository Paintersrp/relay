package features

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	workflowtickets "relay/internal/app/tickets"
	workflowstore "relay/internal/store/workflow"
)

// closedNoDeliveryFixture builds the real backend possible state of the
// no-delivery route: an adopted workspace whose current discovery is closed to
// no_delivery_work with a verified closure packet, and no planning authority,
// Delivery Ticket, package, Run, or remediation anywhere. It is the production
// state AC5 requires; no synthetic completion-ready fixture is used.
func closedNoDeliveryFixture(t *testing.T) (context.Context, *workflowstore.Store, *Service, workflowstore.FeatureWorkspace, workflowstore.DiscoveryClosurePacket) {
	t.Helper()
	ctx, store, service, workspace, revision := adoptedDiscoveryLifecycle(t, DiscoveryDestinationNoDeliveryWork)
	closed, workspace, err := service.CloseFeatureDiscovery(ctx, CloseFeatureDiscoveryInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, ExpectedRevisionID: revision.DiscoveryRevisionID, Destination: DiscoveryDestinationNoDeliveryWork, CreatedIdentity: "operator"})
	if err != nil {
		t.Fatal(err)
	}
	workspace, err = store.GetFeatureWorkspaceByWorkspaceID(ctx, workspace.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	if workspace.CurrentAuthorityRevisionRowID.Valid {
		t.Fatal("no-delivery fixture unexpectedly carries planning authority")
	}
	return ctx, store, service, workspace, closed.Packet
}

func TestNoDeliveryFeatureCompletionRecordsExactPacketBasisWithoutAuthority(t *testing.T) {
	ctx, store, service, workspace, packet := closedNoDeliveryFixture(t)
	status, err := service.EvaluateCompletion(ctx, workspace.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	for _, gate := range status.Gates {
		if !gate.Ready {
			t.Fatalf("no-delivery completion gate %q not ready: %+v", gate.Name, status.Gates)
		}
	}
	if status.CurrentDecision != nil {
		t.Fatalf("unexpected current decision: %+v", status.CurrentDecision)
	}
	// Completion remains an explicit confirmed versioned action.
	if _, err := service.Complete(ctx, CompletionInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version}); !errors.Is(err, ErrFeatureCompletionConfirmation) {
		t.Fatalf("unconfirmed no-delivery completion = %v", err)
	}
	if _, err := service.Complete(ctx, CompletionInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version - 1, OperatorConfirmed: true}); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("stale no-delivery completion = %v", err)
	}
	completed, err := service.Complete(ctx, CompletionInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, OperatorConfirmed: true})
	if err != nil {
		t.Fatal(err)
	}
	// The durable immutable decision records completed with the exact
	// no-delivery discovery closure packet basis and no fake authority.
	if completed.Decision.Decision != "completed" {
		t.Fatalf("decision = %q, want completed", completed.Decision.Decision)
	}
	if completed.Decision.AuthorityRevisionRowID.Valid || completed.Decision.SourceClosureRowID.Valid {
		t.Fatalf("no-delivery decision fabricated an authority basis: %+v", completed.Decision)
	}
	if !completed.Decision.DiscoveryClosurePacketRowID.Valid || completed.Decision.DiscoveryClosurePacketRowID.Int64 != packet.ID {
		t.Fatalf("no-delivery decision packet = %+v, want row %d", completed.Decision.DiscoveryClosurePacketRowID, packet.ID)
	}
	if completed.Workspace.Version != workspace.Version+1 {
		t.Fatalf("completion did not bump the version: %d -> %d", workspace.Version, completed.Workspace.Version)
	}
	status, err = service.EvaluateCompletion(ctx, workspace.WorkspaceID)
	if err != nil || status.CurrentDecision == nil || status.CurrentDecision.ID != completed.Decision.ID || status.CurrentDecision.Decision != "completed" {
		t.Fatalf("current no-delivery decision = %+v, err=%v", status, err)
	}
	if _, err := service.Complete(ctx, CompletionInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: completed.Workspace.Version, OperatorConfirmed: true}); !errors.Is(err, ErrFeatureCompletionRecorded) {
		t.Fatalf("double no-delivery completion = %v", err)
	}
	if _, err := store.DB().ExecContext(ctx, `UPDATE feature_workspace_completion_decisions SET decision = 'abandoned' WHERE id = ?`, completed.Decision.ID); err == nil {
		t.Fatal("no-delivery decision was mutable")
	}
	if _, err := store.DB().ExecContext(ctx, `DELETE FROM feature_workspace_completion_decisions WHERE id = ?`, completed.Decision.ID); err == nil {
		t.Fatal("no-delivery decision was deletable")
	}
}

func TestNoDeliveryFeatureAbandonmentRecordsImmutableDecisionWithoutAuthority(t *testing.T) {
	ctx, store, service, workspace, packet := closedNoDeliveryFixture(t)
	if _, err := service.Abandon(ctx, CompletionInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version}); !errors.Is(err, ErrFeatureCompletionConfirmation) {
		t.Fatalf("unconfirmed no-delivery abandonment = %v", err)
	}
	if _, err := service.Abandon(ctx, CompletionInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version - 1, OperatorConfirmed: true}); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("stale no-delivery abandonment = %v", err)
	}
	abandoned, err := service.Abandon(ctx, CompletionInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, OperatorConfirmed: true})
	if err != nil {
		t.Fatal(err)
	}
	if abandoned.Decision.Decision != "abandoned" || abandoned.Decision.AuthorityRevisionRowID.Valid || abandoned.Decision.SourceClosureRowID.Valid || !abandoned.Decision.DiscoveryClosurePacketRowID.Valid || abandoned.Decision.DiscoveryClosurePacketRowID.Int64 != packet.ID {
		t.Fatalf("no-delivery abandonment decision = %+v", abandoned.Decision)
	}
	if abandoned.Workspace.Version != workspace.Version+1 {
		t.Fatalf("abandonment did not bump the version: %d -> %d", workspace.Version, abandoned.Workspace.Version)
	}
	status, err := service.EvaluateCompletion(ctx, workspace.WorkspaceID)
	if err != nil || status.CurrentDecision == nil || status.CurrentDecision.Decision != "abandoned" {
		t.Fatalf("current no-delivery abandonment = %+v, err=%v", status, err)
	}
	if _, err := store.DB().ExecContext(ctx, `UPDATE feature_workspace_completion_decisions SET decision = 'completed' WHERE id = ?`, abandoned.Decision.ID); err == nil {
		t.Fatal("abandoned no-delivery decision was mutable")
	}
}

// insertNoDeliveryTicketClosure creates the source-backed closure the delivery
// ticket revision guard requires so the delivery-bearing route can be modeled
// on the no-delivery workspace.
func insertNoDeliveryTicketClosure(t *testing.T, ctx context.Context, store *workflowstore.Store) int64 {
	t.Helper()
	if _, err := store.DB().ExecContext(ctx, `INSERT INTO repository_targets (repo_target, local_path, configured_branch_ref, configuration_version) VALUES ('relay', 'C:/relay', 'refs/heads/main', 1)`); err != nil {
		t.Fatal(err)
	}
	var vaultID, closureID int64
	if err := store.DB().QueryRowContext(ctx, `INSERT INTO source_vaults (vault_id, repo_target, relative_path) VALUES ('vault-no-delivery-guard', 'relay', 'vaults/features') RETURNING id`).Scan(&vaultID); err != nil {
		t.Fatal(err)
	}
	if err := store.DB().QueryRowContext(ctx, `INSERT INTO source_vault_closures (closure_id, vault_row_id, commit_oid, tree_oid, generation, ref_name, state, import_started_at, verified_at) VALUES ('closure-no-delivery-guard', ?, ?, ?, 1, 'refs/relay/closures/no-delivery-guard', 'ready', '2026-07-18T00:00:00.000000000Z', '2026-07-18T00:00:01.000000000Z') RETURNING id`, vaultID, strings.Repeat("d", 40), strings.Repeat("e", 40)).Scan(&closureID); err != nil {
		t.Fatal(err)
	}
	return closureID
}

func TestNoDeliveryClosingStaysBlockedWithUnsatisfiedDeliveryTicket(t *testing.T) {
	ctx, store, service, workspace, packet := closedNoDeliveryFixture(t)
	closureID := insertNoDeliveryTicketClosure(t, ctx, store)
	ticketService, err := workflowtickets.NewService(store)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ticketService.Publish(ctx, completionTicketPublishInput(workspace.WorkspaceID, closureID, "P-NO-DELIVERY-GUARD", "Delivery work must block no-delivery closure.")); err != nil {
		t.Fatal(err)
	}
	status, err := service.EvaluateCompletion(ctx, workspace.WorkspaceID)
	if err != nil || completionGateReady(status, "tickets") || completionGateReady(status, "audit") || completionGateReady(status, "authority") || completionGateReady(status, "currentness") {
		t.Fatalf("delivery-bearing no-delivery gates = %+v, err=%v", status, err)
	}
	if _, err := service.Complete(ctx, CompletionInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, OperatorConfirmed: true}); !errors.Is(err, ErrFeatureCompletionNotReady) {
		t.Fatalf("delivery-bearing no-delivery completion = %v", err)
	}
	if _, err := service.Abandon(ctx, CompletionInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, OperatorConfirmed: true}); !errors.Is(err, ErrFeatureCompletionNotReady) {
		t.Fatalf("delivery-bearing no-delivery abandonment = %v", err)
	}
	assertNoFeatureCompletionDecision(t, ctx, store, workspace.ID)
	// The DB guard independently rejects a forged no-delivery decision row
	// while unsatisfied delivery work exists.
	if _, err := store.DB().ExecContext(ctx, `INSERT INTO feature_workspace_completion_decisions (completion_decision_id, workspace_row_id, authority_revision_row_id, source_closure_row_id, discovery_closure_packet_row_id, decision) VALUES ('completion-forged-delivery-guard', ?, NULL, NULL, ?, 'completed')`, workspace.ID, packet.ID); err == nil {
		t.Fatal("DB guard accepted a no-delivery decision with unsatisfied delivery work")
	}
}

func TestNoDeliveryClosingRejectsActiveSelectionWithoutAuthority(t *testing.T) {
	ctx, store, service, workspace, _ := closedNoDeliveryFixture(t)
	if err := store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		_, err := tx.CreateDeliveryTicketSelection(ctx, workflowstore.CreateDeliveryTicketSelectionParams{
			SelectionID: "selection-no-delivery-active", WorkspaceRowID: workspace.ID, State: "active", Rationale: "hold no-delivery closure while work is active",
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	status, err := service.EvaluateCompletion(ctx, workspace.WorkspaceID)
	if err != nil || completionGateReady(status, "integration") || completionGateReady(status, "authority") {
		t.Fatalf("active-selection no-delivery gates = %+v, err=%v", status, err)
	}
	if _, err := service.Complete(ctx, CompletionInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, OperatorConfirmed: true}); !errors.Is(err, ErrFeatureCompletionNotReady) {
		t.Fatalf("active-selection no-delivery completion = %v", err)
	}
	assertNoFeatureCompletionDecision(t, ctx, store, workspace.ID)
}

func TestNoDeliveryCompletedAndAbandonedDecisionsReopenFromLaterDiscoveryChange(t *testing.T) {
	for _, decision := range []struct {
		name string
		act  func(context.Context, *Service, CompletionInput) (CompletionResult, error)
	}{
		{"completed", func(ctx context.Context, service *Service, input CompletionInput) (CompletionResult, error) { return service.Complete(ctx, input) }},
		{"abandoned", func(ctx context.Context, service *Service, input CompletionInput) (CompletionResult, error) { return service.Abandon(ctx, input) }},
	} {
		t.Run(decision.name, func(t *testing.T) {
			ctx, store, service, workspace, packet := closedNoDeliveryFixture(t)
			recorded, err := decision.act(ctx, service, CompletionInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, OperatorConfirmed: true})
			if err != nil {
				t.Fatal(err)
			}
			// A later valid discovery change reopens the historical decision
			// without any planning authority.
			replacement := []byte("# " + decision.name + " no-delivery reopened\n")
			newRevision, reopened, err := service.ReopenFeatureDiscovery(ctx, ReopenFeatureDiscoveryInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: recorded.Workspace.Version, ExpectedPacketID: packet.ClosurePacketID, OperatorConfirmed: true, Cause: "later discovery change", CreatedIdentity: "operator", Markdown: replacement, SHA256: discoveryTestDigest(replacement), Destination: DiscoveryDestinationNoDeliveryWork})
			if err != nil {
				t.Fatal(err)
			}
			status, err := service.EvaluateCompletion(ctx, reopened.WorkspaceID)
			if err != nil || status.CurrentDecision != nil {
				t.Fatalf("no-delivery reopen state = %+v, err=%v", status, err)
			}
			var reopeningKind string
			if err := store.DB().QueryRowContext(ctx, `SELECT reopening_kind FROM feature_workspace_completion_reopenings WHERE completion_decision_row_id = ?`, recorded.Decision.ID).Scan(&reopeningKind); err != nil || reopeningKind != "discovery_reopen" {
				t.Fatalf("no-delivery reopen kind = %q, err=%v", reopeningKind, err)
			}
			// The workspace reclosed on the replacement revision completes
			// again with the new exact packet; the historical decision keeps
			// its original packet basis.
			reclosed, workspace, err := service.CloseFeatureDiscovery(ctx, CloseFeatureDiscoveryInput{WorkspaceID: reopened.WorkspaceID, ExpectedVersion: reopened.Version, ExpectedRevisionID: newRevision.DiscoveryRevisionID, Destination: DiscoveryDestinationNoDeliveryWork, CreatedIdentity: "operator"})
			if err != nil {
				t.Fatal(err)
			}
			second, err := decision.act(ctx, service, CompletionInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, OperatorConfirmed: true})
			if err != nil {
				t.Fatal(err)
			}
			if !second.Decision.DiscoveryClosurePacketRowID.Valid || second.Decision.DiscoveryClosurePacketRowID.Int64 != reclosed.Packet.ID || second.Decision.AuthorityRevisionRowID.Valid {
				t.Fatalf("reclosed no-delivery decision = %+v", second.Decision)
			}
			var historicalPacket sql.NullInt64
			if err := store.DB().QueryRowContext(ctx, `SELECT discovery_closure_packet_row_id FROM feature_workspace_completion_decisions WHERE id = ?`, recorded.Decision.ID).Scan(&historicalPacket); err != nil || !historicalPacket.Valid || historicalPacket.Int64 != packet.ID {
				t.Fatalf("historical no-delivery decision packet = %#v, err=%v", historicalPacket, err)
			}
		})
	}
}

func TestGuidedProjectionUsesNoDeliveryOwnerReadinessWithoutAuthority(t *testing.T) {
	ctx, store, service, workspace, _ := closedNoDeliveryFixture(t)
	service.SetGuidedAuditOwnerForTest(&guidedFakeAuditOwner{})
	ticketOwner, err := workflowtickets.NewService(store)
	if err != nil {
		t.Fatal(err)
	}
	service.SetGuidedTicketOwnerForTest(ticketOwner)

	before, err := service.ReadGuidedProjection(ctx, workspace.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	if before.PrimaryAction.Action != GuidedActionCompleteFeature || !before.PrimaryAction.Enabled || before.Completion.Recorded || !GuidedCompletionReady(before.Completion.Gates) {
		t.Fatalf("no-delivery projection primary=%+v completion=%+v err=%v", before.PrimaryAction, before.Completion, err)
	}
	if len(before.AvailableActions) != 2 || before.AvailableActions[1].Action != GuidedActionAbandonFeature || !before.AvailableActions[1].Enabled || !before.AvailableActions[1].RequiresConfirmation {
		t.Fatalf("no-delivery projection abandon secondary=%+v", before.AvailableActions)
	}
	if _, err := service.ExecuteGuidedAction(ctx, GuidedActionInput{WorkspaceID: workspace.WorkspaceID, Action: string(GuidedActionCompleteFeature), ExpectedVersion: before.Workspace.Version}); !errors.Is(err, ErrFeatureCompletionConfirmation) {
		t.Fatalf("guided completion without confirmation = %v", err)
	}
	result, err := service.ExecuteGuidedAction(ctx, GuidedActionInput{WorkspaceID: workspace.WorkspaceID, Action: string(GuidedActionCompleteFeature), ExpectedVersion: before.Workspace.Version, Confirmation: true})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Projection.Completion.Recorded || result.Projection.Completion.Decision != "completed" {
		t.Fatalf("guided no-delivery completion projection=%+v", result.Projection.Completion)
	}
	if result.Projection.PrimaryAction.Action != GuidedActionReopenDiscovery {
		t.Fatalf("guided recorded projection primary=%+v", result.Projection.PrimaryAction)
	}
}

func TestGuidedNoDeliveryAbandonmentSecondaryRecordsAbandonedDecision(t *testing.T) {
	ctx, store, service, workspace, _ := closedNoDeliveryFixture(t)
	service.SetGuidedAuditOwnerForTest(&guidedFakeAuditOwner{})
	ticketOwner, err := workflowtickets.NewService(store)
	if err != nil {
		t.Fatal(err)
	}
	service.SetGuidedTicketOwnerForTest(ticketOwner)

	before, err := service.ReadGuidedProjection(ctx, workspace.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	if !guidedActionAvailable(before.AvailableActions, GuidedActionAbandonFeature) {
		t.Fatalf("no-delivery projection did not advertise abandonment: %+v", before.AvailableActions)
	}
	if _, err := service.ExecuteGuidedAction(ctx, GuidedActionInput{WorkspaceID: workspace.WorkspaceID, Action: string(GuidedActionAbandonFeature), ExpectedVersion: before.Workspace.Version}); !errors.Is(err, ErrFeatureCompletionConfirmation) {
		t.Fatalf("guided abandonment without confirmation = %v", err)
	}
	result, err := service.ExecuteGuidedAction(ctx, GuidedActionInput{WorkspaceID: workspace.WorkspaceID, Action: string(GuidedActionAbandonFeature), ExpectedVersion: before.Workspace.Version, Confirmation: true})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Projection.Completion.Recorded || result.Projection.Completion.Decision != "abandoned" {
		t.Fatalf("guided no-delivery abandonment projection=%+v", result.Projection.Completion)
	}
	if _, err := service.ExecuteGuidedAction(ctx, GuidedActionInput{WorkspaceID: workspace.WorkspaceID, Action: string(GuidedActionCompleteFeature), ExpectedVersion: result.Projection.Workspace.Version, Confirmation: true}); !errors.Is(err, ErrGuidedActionBlocked) {
		t.Fatalf("completion after guided abandonment = %v", err)
	}
}
