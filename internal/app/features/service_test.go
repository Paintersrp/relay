package features

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	workflowtickets "relay/internal/app/tickets"
	workflowstore "relay/internal/store/workflow"
	"relay/internal/testsupport/workflowfixture"
)

func TestAuthorityPublicationKeepsReplacementHistoryAndAllowsNoAuthority(t *testing.T) {
	ctx := context.Background()
	store, firstArtifact, secondArtifact := openFeatureServiceStore(t, ctx)
	service, err := NewServiceWithIDs(store, &featureTestIDs{})
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := createFeatureWorkspace(ctx, store, "workspace-feature-history", "history")
	if err != nil {
		t.Fatal(err)
	}
	firstApproval, err := service.RecordAuthorityApproval(ctx, RecordAuthorityApprovalInput{
		WorkspaceID: workspace.WorkspaceID, Family: "requirements",
		ArtifactRowID: sql.NullInt64{Int64: firstArtifact, Valid: true}, ArtifactSHA256: strings.Repeat("b", 64),
		OperatorConfirmationEvidence: "confirmed",
	})
	if err != nil {
		t.Fatal(err)
	}
	secondApproval, err := service.RecordAuthorityApproval(ctx, RecordAuthorityApprovalInput{
		WorkspaceID: workspace.WorkspaceID, Family: "design",
		ArtifactRowID: sql.NullInt64{Int64: secondArtifact, Valid: true}, ArtifactSHA256: strings.Repeat("c", 64),
		OperatorConfirmationEvidence: "confirmed",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.PublishAuthority(ctx, PublishAuthorityInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, Layers: []AuthorityLayerInput{
		{Kind: "plan", ArtifactRowID: sql.NullInt64{Int64: firstArtifact, Valid: true}, ArtifactSHA256: strings.Repeat("b", 64), ApprovalRowID: sql.NullInt64{Int64: firstApproval.Approval.ID, Valid: true}},
	}}); err == nil {
		t.Fatal("ordinary plan authority was accepted")
	}
	empty, err := service.ReadAuthority(ctx, workspace.WorkspaceID)
	if err != nil || len(empty) != 0 {
		t.Fatalf("optional authority = %#v, %v", empty, err)
	}
	first, workspace, err := service.PublishAuthority(ctx, PublishAuthorityInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, Layers: []AuthorityLayerInput{
		{Kind: "requirements", ArtifactRowID: sql.NullInt64{Int64: firstArtifact, Valid: true}, ArtifactSHA256: strings.Repeat("b", 64), ApprovalRowID: sql.NullInt64{Int64: firstApproval.Approval.ID, Valid: true}},
		{Kind: "design", ArtifactRowID: sql.NullInt64{Int64: secondArtifact, Valid: true}, ArtifactSHA256: strings.Repeat("c", 64), ApprovalRowID: sql.NullInt64{Int64: secondApproval.Approval.ID, Valid: true}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	thirdApproval, err := service.RecordAuthorityApproval(ctx, RecordAuthorityApprovalInput{
		WorkspaceID: workspace.WorkspaceID, Family: "transition_plan",
		ArtifactRowID: sql.NullInt64{Int64: firstArtifact, Valid: true}, ArtifactSHA256: strings.Repeat("b", 64),
		OperatorConfirmationEvidence: "confirmed",
	})
	if err != nil {
		t.Fatal(err)
	}
	second, workspace, err := service.PublishAuthority(ctx, PublishAuthorityInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, Layers: []AuthorityLayerInput{
		{Kind: "transition_plan", ArtifactRowID: sql.NullInt64{Int64: firstArtifact, Valid: true}, ArtifactSHA256: strings.Repeat("b", 64), ApprovalRowID: sql.NullInt64{Int64: thirdApproval.Approval.ID, Valid: true}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if first.Revision.RevisionNumber != 1 || second.Revision.RevisionNumber != 2 || workspace.CurrentAuthorityRevisionRowID.Int64 != second.Revision.ID {
		t.Fatalf("publication results = %#v %#v %#v", first, second, workspace)
	}
	history, err := service.ReadAuthority(ctx, workspace.WorkspaceID)
	if err != nil || len(history) != 2 || len(history[0].Layers) != 2 || len(history[1].Layers) != 1 || history[1].Layers[0].LayerKind != "transition_plan" {
		t.Fatalf("authority history = %#v, %v", history, err)
	}
}

func TestFeatureCompletionIsExplicitGuardedAndReopensForCurrentDefinitionChanges(t *testing.T) {
	ctx := context.Background()
	store, firstArtifact, secondArtifact := openFeatureServiceStore(t, ctx)
	if _, err := store.DB().ExecContext(ctx, `INSERT INTO repository_targets (repo_target, local_path, configured_branch_ref, configuration_version) VALUES ('relay', 'C:/relay', 'refs/heads/main', 1)`); err != nil {
		t.Fatal(err)
	}
	var vaultID, closureID int64
	if err := store.DB().QueryRowContext(ctx, `INSERT INTO source_vaults (vault_id, repo_target, relative_path) VALUES ('vault-feature-completion', 'relay', 'vaults/features') RETURNING id`).Scan(&vaultID); err != nil {
		t.Fatal(err)
	}
	if err := store.DB().QueryRowContext(ctx, `
INSERT INTO source_vault_closures (closure_id, vault_row_id, commit_oid, tree_oid, generation, ref_name, state, import_started_at, verified_at)
VALUES ('closure-feature-completion', ?, ?, ?, 1, 'refs/relay/closures/feature-completion', 'ready', '2026-07-18T00:00:00.000000000Z', '2026-07-18T00:00:01.000000000Z')
RETURNING id`, vaultID, strings.Repeat("d", 40), strings.Repeat("e", 40)).Scan(&closureID); err != nil {
		t.Fatal(err)
	}
	service, err := NewServiceWithIDs(store, &featureTestIDs{})
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := createFeatureWorkspace(ctx, store, "workspace-feature-completion", "completion")
	if err != nil {
		t.Fatal(err)
	}
	firstApproval, err := service.RecordAuthorityApproval(ctx, RecordAuthorityApprovalInput{
		WorkspaceID: workspace.WorkspaceID, Family: "requirements",
		ArtifactRowID: sql.NullInt64{Int64: firstArtifact, Valid: true}, ArtifactSHA256: strings.Repeat("b", 64),
		OperatorConfirmationEvidence: "confirmed",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, workspace, err = service.PublishAuthority(ctx, PublishAuthorityInput{
		WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, SourceClosureID: sql.NullInt64{Int64: closureID, Valid: true},
		Layers: []AuthorityLayerInput{{Kind: "requirements", ArtifactRowID: sql.NullInt64{Int64: firstArtifact, Valid: true}, ArtifactSHA256: strings.Repeat("b", 64), SourceClosureID: sql.NullInt64{Int64: closureID, Valid: true}, ApprovalRowID: sql.NullInt64{Int64: firstApproval.Approval.ID, Valid: true}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	status, err := service.EvaluateCompletion(ctx, workspace.WorkspaceID)
	if err != nil || !completionGateReady(status, "authority") || !completionGateReady(status, "tickets") || !completionGateReady(status, "integration") || !completionGateReady(status, "transitions") || !completionGateReady(status, "remediation") || !completionGateReady(status, "audit") {
		t.Fatalf("initial completion gates = %#v, err=%v", status, err)
	}
	if _, err := service.Complete(ctx, CompletionInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version}); !errors.Is(err, ErrFeatureCompletionConfirmation) {
		t.Fatalf("unconfirmed completion error = %v", err)
	}
	completed, err := service.Complete(ctx, CompletionInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, OperatorConfirmed: true})
	if err != nil || completed.Decision.Decision != "completed" || completed.Decision.DiscoveryClosurePacketRowID.Valid {
		t.Fatalf("explicit completion = %#v, err=%v", completed, err)
	}
	status, err = service.EvaluateCompletion(ctx, workspace.WorkspaceID)
	if err != nil || status.CurrentDecision == nil || status.CurrentDecision.ID != completed.Decision.ID {
		t.Fatalf("current explicit completion = %#v, err=%v", status, err)
	}
	if _, err := service.Complete(ctx, CompletionInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, OperatorConfirmed: true}); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("stale completion error = %v", err)
	}
	secondApproval, err := service.RecordAuthorityApproval(ctx, RecordAuthorityApprovalInput{
		WorkspaceID: workspace.WorkspaceID, Family: "design",
		ArtifactRowID: sql.NullInt64{Int64: secondArtifact, Valid: true}, ArtifactSHA256: strings.Repeat("c", 64),
		OperatorConfirmationEvidence: "confirmed",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, workspace, err = service.PublishAuthority(ctx, PublishAuthorityInput{
		WorkspaceID: workspace.WorkspaceID, ExpectedVersion: status.Workspace.Version, SourceClosureID: sql.NullInt64{Int64: closureID, Valid: true},
		Layers: []AuthorityLayerInput{{Kind: "design", ArtifactRowID: sql.NullInt64{Int64: secondArtifact, Valid: true}, ArtifactSHA256: strings.Repeat("c", 64), SourceClosureID: sql.NullInt64{Int64: closureID, Valid: true}, ApprovalRowID: sql.NullInt64{Int64: secondApproval.Approval.ID, Valid: true}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	status, err = service.EvaluateCompletion(ctx, workspace.WorkspaceID)
	if err != nil || status.CurrentDecision != nil {
		t.Fatalf("authority reopening completion state = %#v, err=%v", status, err)
	}
	workspace, err = service.SetIntegratedDiscoveryCapability(ctx, workspace.WorkspaceID, workspace.Version, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, workspace, err = service.AdoptFeatureDiscoveryLifecycle(ctx, AdoptFeatureDiscoveryLifecycleInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, OperatorIdentity: "operator"}); err != nil {
		t.Fatal(err)
	}
	if _, err = service.Complete(ctx, CompletionInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, OperatorConfirmed: true}); !errors.Is(err, ErrFeatureCompletionNotReady) {
		t.Fatalf("adopted completion without packet = %v", err)
	}
	discovery := []byte("# Completion discovery\n")
	started, workspace, err := service.StartIntegratedDiscovery(ctx, StartIntegratedDiscoveryInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, Markdown: discovery, SHA256: discoveryTestDigest(discovery), CreatedIdentity: "operator", Destination: DiscoveryDestinationRequirements})
	if err != nil {
		t.Fatal(err)
	}
	closed, workspace, err := service.CloseFeatureDiscovery(ctx, CloseFeatureDiscoveryInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, ExpectedRevisionID: started.Revision.DiscoveryRevisionID, Destination: DiscoveryDestinationRequirements, CreatedIdentity: "operator"})
	if err != nil {
		t.Fatal(err)
	}
	manifestArtifact, err := store.GetDiscoveryArtifactByRowID(ctx, closed.Packet.ManifestArtifactRowID)
	if err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(store.ArtifactStore().Root(), filepath.FromSlash(manifestArtifact.RelativePath))
	if err = os.WriteFile(manifestPath, []byte("corrupt manifest\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err = service.Complete(ctx, CompletionInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, OperatorConfirmed: true}); !errors.Is(err, ErrFeatureCompletionNotReady) {
		t.Fatalf("corrupt manifest completion = %v", err)
	}
	if err = os.WriteFile(manifestPath, closed.Manifest, 0o600); err != nil {
		t.Fatal(err)
	}
	memberArtifact, err := store.GetDiscoveryArtifactByRowID(ctx, closed.Members[0].ArtifactRowID)
	if err != nil {
		t.Fatal(err)
	}
	memberPath := filepath.Join(store.ArtifactStore().Root(), filepath.FromSlash(memberArtifact.RelativePath))
	if err = os.WriteFile(memberPath, []byte("corrupt member\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	status, err = service.EvaluateCompletion(ctx, workspace.WorkspaceID)
	if err != nil || completionGateReady(status, "closure") || completionGateReady(status, "currentness") {
		t.Fatalf("corrupt member completion projection = %+v, err=%v", status, err)
	}
	if _, err = service.Complete(ctx, CompletionInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, OperatorConfirmed: true}); !errors.Is(err, ErrFeatureCompletionNotReady) {
		t.Fatalf("corrupt member completion = %v", err)
	}
	if err = os.WriteFile(memberPath, discovery, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err = service.Complete(ctx, CompletionInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version - 1, OperatorConfirmed: true}); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("stale completion = %v", err)
	}
	completed, err = service.Complete(ctx, CompletionInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, OperatorConfirmed: true})
	if err != nil || !completed.Decision.DiscoveryClosurePacketRowID.Valid || completed.Decision.DiscoveryClosurePacketRowID.Int64 != closed.Packet.ID {
		t.Fatalf("adopted completion = %#v, %v", completed, err)
	}
	priorCompleted := completed.Decision
	replacement := []byte("# Completion reopened\n")
	newRevision, workspace, err := service.ReopenFeatureDiscovery(ctx, ReopenFeatureDiscoveryInput{WorkspaceID: completed.Workspace.WorkspaceID, ExpectedVersion: completed.Workspace.Version, ExpectedPacketID: closed.Packet.ClosurePacketID, OperatorConfirmed: true, Cause: "new completion evidence", CreatedIdentity: "operator", Markdown: replacement, SHA256: discoveryTestDigest(replacement), Destination: DiscoveryDestinationRequirements})
	if err != nil || workspace.CurrentDiscoveryClosurePacketRowID.Valid || newRevision.PredecessorRevisionRowID.Int64 != started.Revision.ID {
		t.Fatalf("completion reopen = %#v, %#v, %v", newRevision, workspace, err)
	}
	status, err = service.EvaluateCompletion(ctx, workspace.WorkspaceID)
	if err != nil || status.CurrentDecision != nil || priorCompleted.DiscoveryClosurePacketRowID.Int64 != closed.Packet.ID {
		t.Fatalf("completion invalidation = %#v, %#v, %v", priorCompleted, status, err)
	}
	if _, err = service.Complete(ctx, CompletionInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, OperatorConfirmed: true}); !errors.Is(err, ErrFeatureCompletionNotReady) {
		t.Fatalf("reopened completion = %v", err)
	}
	var historicalPacket sql.NullInt64
	if err = store.DB().QueryRowContext(ctx, `SELECT discovery_closure_packet_row_id FROM feature_workspace_completion_decisions WHERE id = ?`, priorCompleted.ID).Scan(&historicalPacket); err != nil || !historicalPacket.Valid || historicalPacket.Int64 != closed.Packet.ID {
		t.Fatalf("historical completion packet = %#v, %v", historicalPacket, err)
	}

	ticketService, err := workflowtickets.NewService(store)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ticketService.Publish(ctx, workflowtickets.PublishInput{
		WorkspaceID: workspace.WorkspaceID, TicketID: "P6-FEATURE", ExternalPriority: 1, ExpectedRevisionNumber: 0,
		Revision: workflowtickets.RevisionInput{
			RepoTarget: "relay", Branch: "main", BaseCommit: strings.Repeat("d", 40), SourceClosureRowID: closureID,
			SourcePath: "tickets/P6-FEATURE.delivery-ticket.json", Goal: "Reopen the completed workspace.", Context: "A new current ticket is unfinished.",
			TransitionApplicability: "required", CanonicalJSON: []byte(`{"ticket":"P6-FEATURE"}`), RenderedMarkdown: []byte("# P6-FEATURE\n"),
			Members: []workflowtickets.RevisionMemberInput{{Kind: "implementation_obligation", Path: "internal/app/features", Text: "Require explicit completion."}},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		_, err := tx.CreateDeliveryTicketSelection(ctx, workflowstore.CreateDeliveryTicketSelectionParams{
			SelectionID: "selection-feature-completion", WorkspaceRowID: status.Workspace.ID, State: "active", Rationale: "exercise the integration completion gate",
			SourceClosureRowID: sql.NullInt64{Int64: closureID, Valid: true},
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	status, err = service.EvaluateCompletion(ctx, workspace.WorkspaceID)
	if err != nil || status.CurrentDecision != nil || completionGateReady(status, "tickets") || completionGateReady(status, "integration") || completionGateReady(status, "transitions") || completionGateReady(status, "audit") {
		t.Fatalf("ticket reopening completion state = %#v, err=%v", status, err)
	}
}

func TestFeatureCompletionE3BlocksActiveSelectionUntilTerminal(t *testing.T) {
	ctx, store, service, workspace, _ := completedDiscoveryFixture(t)
	if err := store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		_, err := tx.CreateDeliveryTicketSelection(ctx, workflowstore.CreateDeliveryTicketSelectionParams{
			SelectionID: "selection-completion-e3", WorkspaceRowID: workspace.ID, State: "active", Rationale: "hold completion while the selected work is active",
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	status, err := service.EvaluateCompletion(ctx, workspace.WorkspaceID)
	if err != nil || completionGateReady(status, "integration") {
		t.Fatalf("active selection completion gates = %#v, err=%v", status, err)
	}
	if _, err := service.Complete(ctx, CompletionInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, OperatorConfirmed: true}); !errors.Is(err, ErrFeatureCompletionNotReady) {
		t.Fatalf("active selection completion = %v", err)
	}
	assertNoFeatureCompletionDecision(t, ctx, store, workspace.ID)
	if err := store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		_, err := tx.TransitionDeliveryTicketSelection(ctx, "selection-completion-e3", "superseded")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	status, err = service.EvaluateCompletion(ctx, workspace.WorkspaceID)
	if err != nil || !completionGateReady(status, "integration") {
		t.Fatalf("superseded selection completion gates = %#v, err=%v", status, err)
	}
	if _, err := service.Complete(ctx, CompletionInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, OperatorConfirmed: true}); err != nil {
		t.Fatal(err)
	}
}

func TestFeatureCompletionE7RejectsActiveBlockedAndPendingDiscoveryIntegration(t *testing.T) {
	for _, test := range []struct {
		name    string
		prepare func(context.Context, *workflowstore.Store, workflowstore.FeatureWorkspace) error
		assert  func(DiscoveryAssessment) bool
	}{
		{
			name: "active",
			prepare: func(ctx context.Context, store *workflowstore.Store, workspace workflowstore.FeatureWorkspace) error {
				return store.WithTx(ctx, func(tx *workflowstore.Tx) error {
					_, err := tx.CreateFeatureWorkspaceDiscoveryTicket(ctx, workflowstore.CreateFeatureWorkspaceDiscoveryTicketParams{DiscoveryTicketID: "discovery-completion-active", WorkspaceRowID: workspace.ID, TicketKey: "completion-active", Subject: "active completion work"})
					return err
				})
			},
			assert: func(assessment DiscoveryAssessment) bool { return len(assessment.ActiveOperations) == 1 },
		},
		{
			name: "blocked",
			prepare: func(ctx context.Context, store *workflowstore.Store, workspace workflowstore.FeatureWorkspace) error {
				if _, err := store.DB().ExecContext(ctx, `DROP TRIGGER discovery_closed_ticket_mutation_guard`); err != nil {
					return err
				}
				return store.WithTx(ctx, func(tx *workflowstore.Tx) error {
					ticket, err := tx.CreateFeatureWorkspaceDiscoveryTicket(ctx, workflowstore.CreateFeatureWorkspaceDiscoveryTicketParams{DiscoveryTicketID: "discovery-completion-blocked", WorkspaceRowID: workspace.ID, TicketKey: "completion-blocked", Subject: "blocked completion work"})
					if err != nil {
						return err
					}
					_, err = tx.TransitionFeatureWorkspaceDiscoveryTicket(ctx, ticket.DiscoveryTicketID, "open", "blocked", ticket.Version)
					return err
				})
			},
			assert: func(assessment DiscoveryAssessment) bool { return len(assessment.Blockers) == 1 },
		},
		{
			name: "pending integration",
			prepare: func(ctx context.Context, store *workflowstore.Store, workspace workflowstore.FeatureWorkspace) error {
				if _, err := store.DB().ExecContext(ctx, `DROP TRIGGER discovery_closed_ticket_mutation_guard`); err != nil {
					return err
				}
				if _, err := store.DB().ExecContext(ctx, `DROP TRIGGER discovery_closed_resolution_guard`); err != nil {
					return err
				}
				var artifactID int64
				if err := store.DB().QueryRowContext(ctx, `SELECT id FROM artifacts ORDER BY id LIMIT 1`).Scan(&artifactID); err != nil {
					return err
				}
				return store.WithTx(ctx, func(tx *workflowstore.Tx) error {
					ticket, err := tx.CreateFeatureWorkspaceDiscoveryTicket(ctx, workflowstore.CreateFeatureWorkspaceDiscoveryTicketParams{DiscoveryTicketID: "discovery-completion-pending", WorkspaceRowID: workspace.ID, TicketKey: "completion-pending", Subject: "pending completion integration"})
					if err != nil {
						return err
					}
					if _, err = tx.CreateFeatureWorkspaceTicketResolution(ctx, workflowstore.CreateFeatureWorkspaceTicketResolutionParams{ResolutionID: "resolution-completion-pending", TicketRowID: ticket.ID, Sequence: 1, ResolutionKind: "resolved", ArtifactRowID: sql.NullInt64{Int64: artifactID, Valid: true}, ArtifactSha256: strings.Repeat("b", 64)}); err != nil {
						return err
					}
					_, err = tx.TransitionFeatureWorkspaceDiscoveryTicket(ctx, ticket.DiscoveryTicketID, "open", "resolved", ticket.Version)
					return err
				})
			},
			assert: func(assessment DiscoveryAssessment) bool { return len(assessment.PendingIntegrations) == 1 },
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, store, service, workspace, _ := completedDiscoveryFixture(t)
			if err := test.prepare(ctx, store, workspace); err != nil {
				t.Fatal(err)
			}
			assessment, err := service.AssessDiscoveryDestination(ctx, workspace.WorkspaceID)
			if err != nil || !test.assert(assessment) {
				t.Fatalf("discovery assessment = %#v, err=%v", assessment, err)
			}
			if _, err := service.Complete(ctx, CompletionInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, OperatorConfirmed: true}); !errors.Is(err, ErrFeatureCompletionNotReady) {
				t.Fatalf("%s completion = %v", test.name, err)
			}
			assertNoFeatureCompletionDecision(t, ctx, store, workspace.ID)
		})
	}
}

func TestFeatureCompletionF2RequiresCurrentTicketAuditSatisfaction(t *testing.T) {
	ctx, store, service, workspace, closure := completedDiscoveryFixture(t)
	ticketService, err := workflowtickets.NewService(store)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = ticketService.Publish(ctx, completionTicketPublishInput(workspace.WorkspaceID, closure.ID, "P-COMPLETION-F2", "Require the current ticket audit.")); err != nil {
		t.Fatal(err)
	}
	status, err := service.EvaluateCompletion(ctx, workspace.WorkspaceID)
	if err != nil || completionGateReady(status, "tickets") || completionGateReady(status, "audit") {
		t.Fatalf("unaudited ticket completion gates = %#v, err=%v", status, err)
	}
	if _, err = service.Complete(ctx, CompletionInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, OperatorConfirmed: true}); !errors.Is(err, ErrFeatureCompletionNotReady) {
		t.Fatalf("unaudited ticket completion = %v", err)
	}
	assertNoFeatureCompletionDecision(t, ctx, store, workspace.ID)
}

func TestFeatureCompletionF3TicketPublicationReopensCurrentDecision(t *testing.T) {
	ctx, store, service, workspace, closure := completedDiscoveryFixture(t)
	completed, err := service.Complete(ctx, CompletionInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, OperatorConfirmed: true})
	if err != nil {
		t.Fatal(err)
	}
	ticketService, err := workflowtickets.NewService(store)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = ticketService.Publish(ctx, completionTicketPublishInput(workspace.WorkspaceID, closure.ID, "P-COMPLETION-F3", "Reopen the completed workspace.")); err != nil {
		t.Fatal(err)
	}
	status, err := service.EvaluateCompletion(ctx, workspace.WorkspaceID)
	if err != nil || status.CurrentDecision != nil || completionGateReady(status, "tickets") || completionGateReady(status, "audit") {
		t.Fatalf("ticket publication completion state = %#v, err=%v", status, err)
	}
	var reopeningKind string
	var reopeningRevision sql.NullInt64
	if err = store.DB().QueryRowContext(ctx, `SELECT reopening_kind, reopening_ticket_revision_row_id FROM feature_workspace_completion_reopenings WHERE completion_decision_row_id = ?`, completed.Decision.ID).Scan(&reopeningKind, &reopeningRevision); err != nil || reopeningKind != "ticket_revision" || !reopeningRevision.Valid {
		t.Fatalf("ticket completion reopening = %q %#v, err=%v", reopeningKind, reopeningRevision, err)
	}
}

func TestFeatureAbandonmentRecordsImmutableDecisionAndReopensLikeCompletion(t *testing.T) {
	ctx, store, service, workspace, _ := completedDiscoveryFixture(t)
	if _, err := service.Abandon(ctx, CompletionInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version}); !errors.Is(err, ErrFeatureCompletionConfirmation) {
		t.Fatalf("unconfirmed abandonment error = %v", err)
	}
	if _, err := service.Abandon(ctx, CompletionInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version - 1, OperatorConfirmed: true}); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("stale abandonment error = %v", err)
	}
	abandoned, err := service.Abandon(ctx, CompletionInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, OperatorConfirmed: true})
	if err != nil || abandoned.Decision.Decision != "abandoned" || !abandoned.Decision.DiscoveryClosurePacketRowID.Valid {
		t.Fatalf("explicit abandonment = %#v, err=%v", abandoned, err)
	}
	if abandoned.Workspace.Version != workspace.Version+1 {
		t.Fatalf("abandonment did not bump the workspace version: %d -> %d", workspace.Version, abandoned.Workspace.Version)
	}
	status, err := service.EvaluateCompletion(ctx, workspace.WorkspaceID)
	if err != nil || status.CurrentDecision == nil || status.CurrentDecision.ID != abandoned.Decision.ID || status.CurrentDecision.Decision != "abandoned" {
		t.Fatalf("current abandonment decision = %#v, err=%v", status, err)
	}
	if _, err := service.Complete(ctx, CompletionInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: abandoned.Workspace.Version, OperatorConfirmed: true}); !errors.Is(err, ErrFeatureCompletionRecorded) {
		t.Fatalf("completion after abandonment = %v", err)
	}
	if _, err := service.Abandon(ctx, CompletionInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: abandoned.Workspace.Version, OperatorConfirmed: true}); !errors.Is(err, ErrFeatureCompletionRecorded) {
		t.Fatalf("double abandonment = %v", err)
	}
	if _, err := store.DB().ExecContext(ctx, `UPDATE feature_workspace_completion_decisions SET decision = 'completed' WHERE id = ?`, abandoned.Decision.ID); err == nil {
		t.Fatal("abandoned decision was mutable")
	}
	packet, err := store.GetDiscoveryClosurePacketByRowID(ctx, abandoned.Workspace.CurrentDiscoveryClosurePacketRowID.Int64)
	if err != nil {
		t.Fatal(err)
	}
	replacement := []byte("# Abandonment reopened\n")
	_, reopened, err := service.ReopenFeatureDiscovery(ctx, ReopenFeatureDiscoveryInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: abandoned.Workspace.Version, ExpectedPacketID: packet.ClosurePacketID, OperatorConfirmed: true, Cause: "resume after abandonment", CreatedIdentity: "operator", Markdown: replacement, SHA256: discoveryTestDigest(replacement), Destination: DiscoveryDestinationRequirements})
	if err != nil {
		t.Fatal(err)
	}
	// The existing reopen mechanics reopen this current closing decision: after
	// reopen the workspace has no current decision, exactly as for completion.
	status, err = service.EvaluateCompletion(ctx, reopened.WorkspaceID)
	if err != nil || status.CurrentDecision != nil {
		t.Fatalf("abandoned decision reopening state = %#v, err=%v", status, err)
	}
	var reopeningKind string
	if err = store.DB().QueryRowContext(ctx, `SELECT reopening_kind FROM feature_workspace_completion_reopenings WHERE completion_decision_row_id = ?`, abandoned.Decision.ID).Scan(&reopeningKind); err != nil || reopeningKind != "authority_revision" {
		t.Fatalf("abandoned decision reopening = %q, err=%v", reopeningKind, err)
	}
}

func TestFeatureAbandonmentRejectsUnreadyGatesWithoutMutation(t *testing.T) {
	ctx, store, service, workspace, _ := completedDiscoveryFixture(t)
	if err := store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		_, err := tx.CreateDeliveryTicketSelection(ctx, workflowstore.CreateDeliveryTicketSelectionParams{
			SelectionID: "selection-abandon-not-ready", WorkspaceRowID: workspace.ID, State: "active", Rationale: "hold abandonment while work is active",
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	status, err := service.EvaluateCompletion(ctx, workspace.WorkspaceID)
	if err != nil || completionGateReady(status, "integration") {
		t.Fatalf("active selection abandonment gates = %#v, err=%v", status, err)
	}
	if _, err := service.Abandon(ctx, CompletionInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, OperatorConfirmed: true}); !errors.Is(err, ErrFeatureCompletionNotReady) {
		t.Fatalf("unready abandonment = %v", err)
	}
	assertNoFeatureCompletionDecision(t, ctx, store, workspace.ID)
}

func TestFeatureCompletionRejectsStaleDiscoveryPacketRevisionBinding(t *testing.T) {
	ctx, store, service, workspace, _ := completedDiscoveryFixture(t)
	before := workspace
	if _, err := store.DB().ExecContext(ctx, `DROP TRIGGER feature_workspace_current_discovery_packet_guard`); err != nil {
		t.Fatal(err)
	}
	if err := store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		current, err := tx.GetCurrentIntegratedDiscoveryRevision(ctx, workspace.WorkspaceID)
		if err != nil {
			return err
		}
		replacement, err := tx.CreateIntegratedDiscoveryRevision(ctx, workflowstore.IntegratedDiscoveryRevision{DiscoveryRevisionID: "discovery-revision-stale-completion", WorkspaceRowID: workspace.ID, RevisionNumber: current.RevisionNumber + 1, ArtifactRowID: current.ArtifactRowID, PredecessorRevisionRowID: sql.NullInt64{Int64: current.ID, Valid: true}, CreatedIdentity: "operator", SettledDestination: current.SettledDestination, ContinuationJSON: current.ContinuationJSON})
		if err != nil {
			return err
		}
		_, err = tx.SetCurrentIntegratedDiscoveryRevision(ctx, workspace.WorkspaceID, replacement.ID, workspace.Version)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	workspace, err := store.GetFeatureWorkspaceByWorkspaceID(ctx, workspace.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.Complete(ctx, CompletionInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, OperatorConfirmed: true}); !errors.Is(err, ErrFeatureCompletionNotReady) {
		t.Fatalf("stale packet binding completion = %v", err)
	}
	assertNoFeatureCompletionDecision(t, ctx, store, workspace.ID)
	current, err := store.GetFeatureWorkspaceByWorkspaceID(ctx, workspace.WorkspaceID)
	if err != nil || current.Version != workspace.Version || !current.CurrentDiscoveryClosurePacketRowID.Valid || current.CurrentDiscoveryClosurePacketRowID != before.CurrentDiscoveryClosurePacketRowID {
		t.Fatalf("stale binding state = %#v, %v", current, err)
	}
}

func TestFeatureCompletionEnabledUnadoptedKeepsNullDiscoveryPacket(t *testing.T) {
	ctx := context.Background()
	store, artifactID, _ := openFeatureServiceStore(t, ctx)
	service, err := NewServiceWithIDs(store, &featureTestIDs{})
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := createFeatureWorkspace(ctx, store, "workspace-enabled-unadopted-completion", "enabled-unadopted-completion")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.DB().ExecContext(ctx, `INSERT INTO repository_targets (repo_target, local_path, configured_branch_ref, configuration_version) VALUES ('relay', 'C:/relay', 'refs/heads/main', 1)`); err != nil {
		t.Fatal(err)
	}
	var vaultID, closureID int64
	if err = store.DB().QueryRowContext(ctx, `INSERT INTO source_vaults (vault_id, repo_target, relative_path) VALUES ('vault-enabled-unadopted', 'relay', 'vaults/enabled-unadopted') RETURNING id`).Scan(&vaultID); err != nil {
		t.Fatal(err)
	}
	if err = store.DB().QueryRowContext(ctx, `INSERT INTO source_vault_closures (closure_id, vault_row_id, commit_oid, tree_oid, generation, ref_name, state, import_started_at, verified_at) VALUES ('closure-enabled-unadopted', ?, ?, ?, 1, 'refs/relay/closures/enabled-unadopted', 'ready', '2026-08-05T00:00:00.000000000Z', '2026-08-05T00:00:01.000000000Z') RETURNING id`, vaultID, strings.Repeat("d", 40), strings.Repeat("e", 40)).Scan(&closureID); err != nil {
		t.Fatal(err)
	}
	approval, err := service.RecordAuthorityApproval(ctx, RecordAuthorityApprovalInput{WorkspaceID: workspace.WorkspaceID, Family: "requirements", ArtifactRowID: sql.NullInt64{Int64: artifactID, Valid: true}, ArtifactSHA256: strings.Repeat("b", 64), OperatorConfirmationEvidence: "confirmed"})
	if err != nil {
		t.Fatal(err)
	}
	_, workspace, err = service.PublishAuthority(ctx, PublishAuthorityInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, SourceClosureID: sql.NullInt64{Int64: closureID, Valid: true}, Layers: []AuthorityLayerInput{{Kind: "requirements", ArtifactRowID: sql.NullInt64{Int64: artifactID, Valid: true}, ArtifactSHA256: strings.Repeat("b", 64), SourceClosureID: sql.NullInt64{Int64: closureID, Valid: true}, ApprovalRowID: sql.NullInt64{Int64: approval.Approval.ID, Valid: true}}}})
	if err != nil {
		t.Fatal(err)
	}
	workspace, err = service.SetIntegratedDiscoveryCapability(ctx, workspace.WorkspaceID, workspace.Version, true)
	if err != nil {
		t.Fatal(err)
	}
	completed, err := service.Complete(ctx, CompletionInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, OperatorConfirmed: true})
	if err != nil || completed.Decision.DiscoveryClosurePacketRowID.Valid {
		t.Fatalf("enabled unadopted completion = %#v, %v", completed, err)
	}
}

func TestHistoricalNullDiscoveryPacketCompletionRemainsReadable(t *testing.T) {
	ctx := context.Background()
	store, artifactID, _ := openFeatureServiceStore(t, ctx)
	service, err := NewServiceWithIDs(store, &featureTestIDs{})
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := createFeatureWorkspace(ctx, store, "workspace-historical-null-packet", "historical-null-packet")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.DB().ExecContext(ctx, `INSERT INTO repository_targets (repo_target, local_path, configured_branch_ref, configuration_version) VALUES ('relay', 'C:/relay', 'refs/heads/main', 1)`); err != nil {
		t.Fatal(err)
	}
	var vaultID, closureID int64
	if err = store.DB().QueryRowContext(ctx, `INSERT INTO source_vaults (vault_id, repo_target, relative_path) VALUES ('vault-historical-null', 'relay', 'vaults/historical-null') RETURNING id`).Scan(&vaultID); err != nil {
		t.Fatal(err)
	}
	if err = store.DB().QueryRowContext(ctx, `INSERT INTO source_vault_closures (closure_id, vault_row_id, commit_oid, tree_oid, generation, ref_name, state, import_started_at, verified_at) VALUES ('closure-historical-null', ?, ?, ?, 1, 'refs/relay/closures/historical-null', 'ready', '2026-08-05T00:00:00.000000000Z', '2026-08-05T00:00:01.000000000Z') RETURNING id`, vaultID, strings.Repeat("d", 40), strings.Repeat("e", 40)).Scan(&closureID); err != nil {
		t.Fatal(err)
	}
	approval, err := service.RecordAuthorityApproval(ctx, RecordAuthorityApprovalInput{WorkspaceID: workspace.WorkspaceID, Family: "requirements", ArtifactRowID: sql.NullInt64{Int64: artifactID, Valid: true}, ArtifactSHA256: strings.Repeat("b", 64), OperatorConfirmationEvidence: "confirmed"})
	if err != nil {
		t.Fatal(err)
	}
	_, workspace, err = service.PublishAuthority(ctx, PublishAuthorityInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, SourceClosureID: sql.NullInt64{Int64: closureID, Valid: true}, Layers: []AuthorityLayerInput{{Kind: "requirements", ArtifactRowID: sql.NullInt64{Int64: artifactID, Valid: true}, ArtifactSHA256: strings.Repeat("b", 64), SourceClosureID: sql.NullInt64{Int64: closureID, Valid: true}, ApprovalRowID: sql.NullInt64{Int64: approval.Approval.ID, Valid: true}}}})
	if err != nil {
		t.Fatal(err)
	}
	completed, err := service.Complete(ctx, CompletionInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, OperatorConfirmed: true})
	if err != nil || completed.Decision.DiscoveryClosurePacketRowID.Valid {
		t.Fatalf("null packet completion = %#v, %v", completed, err)
	}
	immutable := completed.Decision
	approval, err = service.RecordAuthorityApproval(ctx, RecordAuthorityApprovalInput{WorkspaceID: workspace.WorkspaceID, Family: "design", ArtifactRowID: sql.NullInt64{Int64: artifactID, Valid: true}, ArtifactSHA256: strings.Repeat("b", 64), OperatorConfirmationEvidence: "confirmed"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = service.PublishAuthority(ctx, PublishAuthorityInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: completed.Workspace.Version, SourceClosureID: sql.NullInt64{Int64: closureID, Valid: true}, Layers: []AuthorityLayerInput{{Kind: "design", ArtifactRowID: sql.NullInt64{Int64: artifactID, Valid: true}, ArtifactSHA256: strings.Repeat("b", 64), SourceClosureID: sql.NullInt64{Int64: closureID, Valid: true}, ApprovalRowID: sql.NullInt64{Int64: approval.Approval.ID, Valid: true}}}}); err != nil {
		t.Fatal(err)
	}
	var persisted workflowstore.FeatureWorkspaceCompletionDecision
	if err = store.DB().QueryRowContext(ctx, `SELECT id, completion_decision_id, workspace_row_id, authority_revision_row_id, discovery_closure_packet_row_id, decision, created_at FROM feature_workspace_completion_decisions WHERE id = ?`, immutable.ID).Scan(&persisted.ID, &persisted.CompletionDecisionID, &persisted.WorkspaceRowID, &persisted.AuthorityRevisionRowID, &persisted.DiscoveryClosurePacketRowID, &persisted.Decision, &persisted.CreatedAt); err != nil || persisted.ID != immutable.ID || persisted.CompletionDecisionID != immutable.CompletionDecisionID || persisted.DiscoveryClosurePacketRowID.Valid {
		t.Fatalf("historical null decision = %#v, %v", persisted, err)
	}
}

type featureTestIDs struct{ next int }

func (ids *featureTestIDs) AuthorityRevisionID() string {
	ids.next++
	return fmt.Sprintf("authority-feature-%d", ids.next)
}

func (ids *featureTestIDs) CompletionDecisionID() string {
	ids.next++
	return fmt.Sprintf("completion-feature-%d", ids.next)
}

func (ids *featureTestIDs) GoverningArtifactApprovalID() string {
	ids.next++
	return fmt.Sprintf("ga-approval-feature-%d", ids.next)
}

func (ids *featureTestIDs) DiscoveryArtifactID() string {
	ids.next++
	return fmt.Sprintf("discovery-artifact-feature-%d", ids.next)
}

func (ids *featureTestIDs) DiscoveryRevisionID() string {
	ids.next++
	return fmt.Sprintf("discovery-revision-feature-%d", ids.next)
}

func openFeatureServiceStore(t *testing.T, ctx context.Context) (*workflowstore.Store, int64, int64) {
	t.Helper()
	store := workflowfixture.Open(t, workflowstore.Open)
	var projectID, planID, first, second int64
	if err := store.DB().QueryRowContext(ctx, `INSERT INTO projects (project_id, name) VALUES ('project-feature-service', 'Features') RETURNING id`).Scan(&projectID); err != nil {
		t.Fatal(err)
	}
	if err := store.DB().QueryRowContext(ctx, `INSERT INTO plans (project_row_id, plan_id, feature_slug, canonical_sha256) VALUES (?, 'plan-feature-service', 'features', ?) RETURNING id`, projectID, strings.Repeat("a", 64)).Scan(&planID); err != nil {
		t.Fatal(err)
	}
	if err := store.DB().QueryRowContext(ctx, `INSERT INTO artifacts (artifact_id, owner_type, plan_row_id, kind, relative_path, media_type, sha256, size_bytes) VALUES ('artifact-feature-requirements', 'plan', ?, 'requirements', 'plans/features/requirements.json', 'application/json', ?, 2) RETURNING id`, planID, strings.Repeat("b", 64)).Scan(&first); err != nil {
		t.Fatal(err)
	}
	if err := store.DB().QueryRowContext(ctx, `INSERT INTO artifacts (artifact_id, owner_type, plan_row_id, kind, relative_path, media_type, sha256, size_bytes) VALUES ('artifact-feature-design', 'plan', ?, 'design', 'plans/features/design.json', 'application/json', ?, 2) RETURNING id`, planID, strings.Repeat("c", 64)).Scan(&second); err != nil {
		t.Fatal(err)
	}
	return store, first, second
}

func createFeatureWorkspace(ctx context.Context, store *workflowstore.Store, workspaceID, slug string) (workflowstore.FeatureWorkspace, error) {
	var workspace workflowstore.FeatureWorkspace
	err := store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		project, err := tx.GetProjectByProjectID(ctx, "project-feature-service")
		if err != nil {
			return err
		}
		workspace, err = tx.CreateFeatureWorkspace(ctx, workflowstore.CreateFeatureWorkspaceParams{WorkspaceID: workspaceID, ProjectRowID: project.ID, FeatureSlug: slug})
		return err
	})
	return workspace, err
}

func completionGateReady(status CompletionStatus, name string) bool {
	for _, gate := range status.Gates {
		if gate.Name == name {
			return gate.Ready
		}
	}
	return false
}

func completedDiscoveryFixture(t *testing.T) (context.Context, *workflowstore.Store, *Service, workflowstore.FeatureWorkspace, workflowstore.SourceVaultClosure) {
	t.Helper()
	ctx, store, service, workspace, revision := adoptedDiscoveryLifecycle(t, DiscoveryDestinationRequirements)
	var err error
	if _, workspace, err = service.CloseFeatureDiscovery(ctx, CloseFeatureDiscoveryInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, ExpectedRevisionID: revision.DiscoveryRevisionID, Destination: DiscoveryDestinationRequirements, CreatedIdentity: "operator"}); err != nil {
		t.Fatal(err)
	}
	var artifactID, vaultID int64
	if err := store.DB().QueryRowContext(ctx, `SELECT id FROM artifacts ORDER BY id LIMIT 1`).Scan(&artifactID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().ExecContext(ctx, `INSERT INTO repository_targets (repo_target, local_path, configured_branch_ref, configuration_version) VALUES ('relay', 'C:/relay', 'refs/heads/main', 1)`); err != nil {
		t.Fatal(err)
	}
	if err := store.DB().QueryRowContext(ctx, `INSERT INTO source_vaults (vault_id, repo_target, relative_path) VALUES ('vault-completion-fixture', 'relay', 'vaults/features') RETURNING id`).Scan(&vaultID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().ExecContext(ctx, `INSERT INTO source_vault_closures (closure_id, vault_row_id, commit_oid, tree_oid, generation, ref_name, state, import_started_at, verified_at) VALUES ('closure-completion-fixture', ?, ?, ?, 1, 'refs/relay/closures/completion-fixture', 'ready', '2026-07-18T00:00:00.000000000Z', '2026-07-18T00:00:01.000000000Z')`, vaultID, strings.Repeat("d", 40), strings.Repeat("e", 40)); err != nil {
		t.Fatal(err)
	}
	closure, err := store.GetSourceVaultClosureByClosureID(ctx, "closure-completion-fixture")
	if err != nil {
		t.Fatal(err)
	}
	approval, err := service.RecordAuthorityApproval(ctx, RecordAuthorityApprovalInput{WorkspaceID: workspace.WorkspaceID, Family: "requirements", ArtifactRowID: sql.NullInt64{Int64: artifactID, Valid: true}, ArtifactSHA256: strings.Repeat("b", 64), OperatorConfirmationEvidence: "confirmed"})
	if err != nil {
		t.Fatal(err)
	}
	if _, workspace, err = service.PublishAuthority(ctx, PublishAuthorityInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, SourceClosureID: sql.NullInt64{Int64: closure.ID, Valid: true}, Layers: []AuthorityLayerInput{{Kind: "requirements", ArtifactRowID: sql.NullInt64{Int64: artifactID, Valid: true}, ArtifactSHA256: strings.Repeat("b", 64), SourceClosureID: sql.NullInt64{Int64: closure.ID, Valid: true}, ApprovalRowID: sql.NullInt64{Int64: approval.Approval.ID, Valid: true}}}}); err != nil {
		t.Fatal(err)
	}
	return ctx, store, service, workspace, closure
}

func assertNoFeatureCompletionDecision(t *testing.T, ctx context.Context, store *workflowstore.Store, workspaceRowID int64) {
	t.Helper()
	if _, err := store.GetCurrentFeatureWorkspaceCompletionDecision(ctx, workspaceRowID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("current completion decision = %v, want none", err)
	}
}

func completionTicketPublishInput(workspaceID string, closureID int64, ticketID, goal string) workflowtickets.PublishInput {
	return workflowtickets.PublishInput{
		WorkspaceID: workspaceID, TicketID: ticketID, ExternalPriority: 1, ExpectedRevisionNumber: 0,
		Revision: workflowtickets.RevisionInput{
			RepoTarget: "relay", Branch: "main", BaseCommit: strings.Repeat("d", 40), SourceClosureRowID: closureID,
			SourcePath: "tickets/" + ticketID + ".delivery-ticket.json", Goal: goal, Context: "Exercise feature completion gates.",
			TransitionApplicability: "not_required", CanonicalJSON: []byte(`{"ticket":"` + ticketID + `"}`), RenderedMarkdown: []byte("# " + ticketID + "\n"),
			Members: []workflowtickets.RevisionMemberInput{{Kind: "implementation_obligation", Path: "internal/app/features", Text: goal}},
		},
	}
}

func TestRecordApprovalRejectsEmptyAndWhitespaceEvidence(t *testing.T) {
	ctx := context.Background()
	store, artifactID, _ := openFeatureServiceStore(t, ctx)
	service, err := NewServiceWithIDs(store, &featureTestIDs{})
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := createFeatureWorkspace(ctx, store, "workspace-evidence-svc", "evidence-svc")
	if err != nil {
		t.Fatal(err)
	}

	invalidEvidences := []string{"", "   ", "\t", "\n"}
	for _, evidence := range invalidEvidences {
		_, err := service.RecordAuthorityApproval(ctx, RecordAuthorityApprovalInput{
			WorkspaceID:                  workspace.WorkspaceID,
			Family:                       "requirements",
			ArtifactRowID:                sql.NullInt64{Int64: artifactID, Valid: true},
			ArtifactSHA256:               strings.Repeat("b", 64),
			OperatorConfirmationEvidence: evidence,
		})
		if !errors.Is(err, ErrInvalidApprovalInput) {
			t.Fatalf("empty/whitespace evidence %q: %v", evidence, err)
		}
	}

	surrounding := "  confirmed  "
	approval, err := service.RecordAuthorityApproval(ctx, RecordAuthorityApprovalInput{
		WorkspaceID:                  workspace.WorkspaceID,
		Family:                       "requirements",
		ArtifactRowID:                sql.NullInt64{Int64: artifactID, Valid: true},
		ArtifactSHA256:               strings.Repeat("b", 64),
		OperatorConfirmationEvidence: surrounding,
	})
	if err != nil {
		t.Fatal(err)
	}
	if approval.Approval.OperatorConfirmationEvidence != "confirmed" {
		t.Fatalf("whitespace not trimmed: %q", approval.Approval.OperatorConfirmationEvidence)
	}

	tooLong := strings.Repeat("x", 4097)
	_, err = service.RecordAuthorityApproval(ctx, RecordAuthorityApprovalInput{
		WorkspaceID:                  workspace.WorkspaceID,
		Family:                       "design",
		ArtifactRowID:                sql.NullInt64{Int64: artifactID, Valid: true},
		ArtifactSHA256:               strings.Repeat("c", 64),
		OperatorConfirmationEvidence: tooLong,
	})
	if !errors.Is(err, ErrInvalidApprovalInput) {
		t.Fatalf("4097-length evidence: %v", err)
	}
}
