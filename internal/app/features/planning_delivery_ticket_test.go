package features

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	workflowtickets "relay/internal/app/tickets"
	workflowartifacts "relay/internal/artifacts/workflow"
	"relay/internal/speccompiler"
	workflowstore "relay/internal/store/workflow"
	"relay/internal/testfixtures"
)

func TestDeliveryTicketCandidateAdmissionSupportsDirectAndAuthorityGovernedDestinations(t *testing.T) {
	for _, destination := range []DiscoveryDestination{
		DiscoveryDestinationDirectDeliveryTicket,
		DiscoveryDestinationRequirements,
		DiscoveryDestinationSharedDesign,
		DiscoveryDestinationRequirementsThenSharedDesign,
		DiscoveryDestinationExistingRouteContinuation,
	} {
		t.Run(string(destination), func(t *testing.T) {
			ctx, store, service, workspace, revision := adoptedDiscoveryLifecycle(t, destination)
			var err error
			_, workspace, err = service.CloseFeatureDiscovery(ctx, CloseFeatureDiscoveryInput{
				WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version,
				ExpectedRevisionID: revision.DiscoveryRevisionID, Destination: destination, CreatedIdentity: "operator",
			})
			if err != nil {
				t.Fatal(err)
			}
			repoTarget := "candidate-" + strings.ReplaceAll(string(destination), "_", "-")
			if _, err := store.DB().ExecContext(ctx, `INSERT INTO repository_targets (repo_target, local_path, configured_branch_ref, configuration_version) VALUES (?, ?, 'refs/heads/main', 1)`, repoTarget, "C:/"+repoTarget); err != nil {
				t.Fatal(err)
			}
			candidateBytes := deliveryTicketCandidateBytes("P3-T1", workspace.FeatureSlug, repoTarget, strings.Repeat("a", 40))
			candidate, err := service.AdmitPlanningCandidate(ctx, CandidateAdmissionInput{
				WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, Family: CandidateFamilyDeliveryTicket,
				Filename: "discovery-proof.ticket-P3-T1.r1.delivery-ticket.json", Bytes: candidateBytes,
				SHA256: digestForPlanningTest(candidateBytes), RepoTarget: repoTarget, Branch: "main", BaseCommit: strings.Repeat("a", 40),
				Destination: destination, CreatedIdentity: "planner",
			})
			if err != nil {
				t.Fatal(err)
			}
			if candidate.Candidate.Family != CandidateFamilyDeliveryTicket || candidate.Candidate.Destination != string(destination) || candidate.AuthorizedNextAction != "approve_candidate" {
				t.Fatalf("delivery candidate admission = %#v", candidate)
			}
		})
	}
}

func TestApprovedDeliveryTicketCandidateProductionUsesCompilerIdentitiesAndApprovalOrdering(t *testing.T) {
	ctx, store, featureService, workspace, _ := deliveryTicketCandidateFixture(t, DiscoveryDestinationDirectDeliveryTicket)
	candidateBytes := deliveryTicketCandidateBytes("P3-T1", workspace.FeatureSlug, "candidate-production", strings.Repeat("a", 40))
	candidate, err := featureService.AdmitPlanningCandidate(ctx, CandidateAdmissionInput{
		WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, Family: CandidateFamilyDeliveryTicket,
		Filename: "discovery-proof.ticket-P3-T1.r1.delivery-ticket.json", Bytes: candidateBytes,
		SHA256: digestForPlanningTest(candidateBytes), RepoTarget: "candidate-production", Branch: "main", BaseCommit: strings.Repeat("a", 40),
		Destination: DiscoveryDestinationDirectDeliveryTicket, CreatedIdentity: "planner",
	})
	if err != nil {
		t.Fatal(err)
	}
	completeReadyPlanningReview(t, ctx, featureService, workspace.WorkspaceID)
	candidateApproval, err := featureService.ApprovePlanningCandidate(ctx, CandidateApprovalInput{
		CandidateID: candidate.Candidate.CandidateID, ExpectedSHA256: candidate.Candidate.ArtifactSha256,
		ExpectedSizeBytes: candidate.Candidate.ArtifactSizeBytes, Bytes: candidateBytes, ExpectedVersion: workspace.Version,
		ExpectedClosurePacketRowID: workspace.CurrentDiscoveryClosurePacketRowID, ExpectedAuthorityRevisionRowID: workspace.CurrentAuthorityRevisionRowID,
		OperatorConfirmationEvidence: "exact approved candidate bytes", CreatedIdentity: "auditor",
	})
	if err != nil {
		t.Fatal(err)
	}

	// This trigger makes the required ordering observable: currentness may only
	// advance after the produced revision approval has been inserted.
	if _, err := store.DB().ExecContext(ctx, `CREATE TRIGGER candidate_production_requires_approval BEFORE UPDATE OF current_revision_row_id ON delivery_tickets WHEN NEW.current_revision_row_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM delivery_ticket_revision_approvals WHERE revision_row_id = NEW.current_revision_row_id AND approval_kind = 'delivery' AND approval_state = 'approved') BEGIN SELECT RAISE(ABORT, 'produced approval must precede current pointer'); END`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = store.DB().ExecContext(ctx, `DROP TRIGGER candidate_production_requires_approval`) })

	ticketService, err := workflowtickets.NewService(store)
	if err != nil {
		t.Fatal(err)
	}
	produced, err := ticketService.PromoteApprovedDeliveryTicketCandidate(ctx, workflowtickets.CandidateProductionInput{
		CandidateID: candidate.Candidate.CandidateID, ApprovalID: candidateApproval.Approval.ApprovalID,
		ExpectedVersion: workspace.Version, ExternalPriority: 7, CreatedIdentity: "planner",
	})
	if err != nil {
		t.Fatal(err)
	}
	compiled, document := speccompiler.CompileDeliveryTicket(candidate.Candidate.Filename, candidateBytes)
	if len(compiled.Errors) != 0 || document == nil || compiled.OutputFilename == nil || compiled.Markdown == nil {
		t.Fatalf("fixture compiler result = %#v, document=%#v", compiled, document)
	}
	if !strings.HasPrefix(produced.Canonical.RelativePath, "feature-discovery/"+workspace.WorkspaceID+"/") || !strings.HasSuffix(produced.Canonical.RelativePath, "/"+candidate.Candidate.Filename) || !strings.HasPrefix(produced.Rendered.RelativePath, "feature-discovery/"+workspace.WorkspaceID+"/") || !strings.HasSuffix(produced.Rendered.RelativePath, "/"+*compiled.OutputFilename) {
		t.Fatalf("compiler-qualified artifact paths = %#v %#v", produced.Canonical, produced.Rendered)
	}
	if produced.Canonical.SHA256 != digestForPlanningTest(candidateBytes) || produced.Canonical.SizeBytes != int64(len(candidateBytes)) || produced.Rendered.SHA256 != digestForPlanningTest([]byte(*compiled.Markdown)) || produced.Rendered.SizeBytes != int64(len(*compiled.Markdown)) {
		t.Fatalf("artifact identities = %#v %#v", produced.Canonical, produced.Rendered)
	}
	if produced.CandidateApproval.ID != candidateApproval.Approval.ID || produced.CandidateApproval.CandidateSha256 != produced.Candidate.ArtifactSha256 || produced.ProducedApproval.ApprovalID == candidateApproval.Approval.ApprovalID || produced.ProducedApproval.RevisionRowID != produced.Revision.ID || produced.ProductionLink.CandidateRowID != candidate.Candidate.ID || produced.ProductionLink.CandidateArtifactRowID != candidate.Candidate.ArtifactRowID || produced.ProductionLink.CanonicalJsonArtifactRowID == produced.ProductionLink.RenderedMarkdownArtifactRowID || produced.ProductionLink.ProducedRevisionRowID != produced.Revision.ID || produced.ProductionLink.ProducedRevisionIdentity != "P3-T1:r1" {
		t.Fatalf("distinct production identities = %#v", produced)
	}
	current, err := store.GetDeliveryTicketByTicketID(ctx, "P3-T1")
	if err != nil || !current.CurrentRevisionRowID.Valid || current.CurrentRevisionRowID.Int64 != produced.Revision.ID {
		t.Fatalf("current produced revision = %#v, %v", current, err)
	}
	approvals, err := store.ListDeliveryTicketRevisionApprovals(ctx, produced.Revision.ID)
	if err != nil || len(approvals) != 1 || approvals[0].ID != produced.ProducedApproval.ID || approvals[0].ApprovalState != "approved" {
		t.Fatalf("produced approvals = %#v, %v", approvals, err)
	}
	canonicalPath := filepath.Join(store.ArtifactStore().Root(), filepath.FromSlash(produced.Canonical.RelativePath))
	renderedPath := filepath.Join(store.ArtifactStore().Root(), filepath.FromSlash(produced.Rendered.RelativePath))
	canonical, err := os.ReadFile(canonicalPath)
	if err != nil || string(canonical) != string(candidateBytes) {
		t.Fatalf("canonical bytes = %q, %v", canonical, err)
	}
	rendered, err := os.ReadFile(renderedPath)
	if err != nil || string(rendered) != *compiled.Markdown {
		t.Fatalf("rendered bytes = %q, %v", rendered, err)
	}
	detail, err := ticketService.Read(ctx, "P3-T1")
	if err != nil || detail.Canonical.RelativePath != produced.Canonical.RelativePath || detail.Rendered.RelativePath != produced.Rendered.RelativePath {
		t.Fatalf("qualified ticket read = %#v, %v", detail, err)
	}
	if err := os.WriteFile(canonicalPath, []byte("tampered canonical"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ticketService.Read(ctx, "P3-T1"); !errors.Is(err, workflowtickets.ErrTicketArtifactIntegrity) {
		t.Fatalf("tampered production artifact read error = %v, want ErrTicketArtifactIntegrity", err)
	}
	if _, err := store.GetCurrentFeatureWorkspaceCompletionDecision(ctx, workspace.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("completion remained current after production: %v", err)
	}
	var reopenings int
	if err := store.DB().QueryRowContext(ctx, `SELECT count(*) FROM feature_workspace_completion_reopenings WHERE completion_decision_row_id IN (SELECT id FROM feature_workspace_completion_decisions WHERE workspace_row_id = ?) AND reopening_ticket_revision_row_id = ?`, workspace.ID, produced.Revision.ID).Scan(&reopenings); err != nil || reopenings != 1 {
		t.Fatalf("completion reopenings = %d, %v", reopenings, err)
	}
}

func TestDirectDeliveryCandidateUsesNullAuthorityOnlyWhileAuthorityIsAbsent(t *testing.T) {
	ctx, store, featureService, workspace, _ := deliveryTicketCandidateFixture(t, DiscoveryDestinationDirectDeliveryTicket)
	// Direct Delivery has no Requirements/Shared Design authority. The fixture
	// establishes one for its authority-governed coverage, so remove only the
	// current pointer before admitting the direct candidate.
	historicalAuthority := workspace.CurrentAuthorityRevisionRowID
	if !historicalAuthority.Valid {
		t.Fatal("fixture did not establish authority for stale-authority negative")
	}
	if _, err := store.DB().ExecContext(ctx, `UPDATE feature_workspaces SET current_authority_revision_row_id = NULL, version = version + 1 WHERE id = ?`, workspace.ID); err != nil {
		t.Fatal(err)
	}
	workspace, err := store.GetFeatureWorkspaceByWorkspaceID(ctx, workspace.WorkspaceID)
	if err != nil || workspace.CurrentAuthorityRevisionRowID.Valid {
		t.Fatalf("direct workspace authority = %#v, %v", workspace.CurrentAuthorityRevisionRowID, err)
	}
	bytes := deliveryTicketCandidateBytes("P3-DIRECT", workspace.FeatureSlug, "candidate-production", strings.Repeat("a", 40))
	candidate, err := featureService.AdmitPlanningCandidate(ctx, CandidateAdmissionInput{
		WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, Family: CandidateFamilyDeliveryTicket,
		Filename: "discovery-proof.ticket-P3-DIRECT.r1.delivery-ticket.json", Bytes: bytes, SHA256: digestForPlanningTest(bytes),
		RepoTarget: "candidate-production", Branch: "main", BaseCommit: strings.Repeat("a", 40),
		Destination: DiscoveryDestinationDirectDeliveryTicket, CreatedIdentity: "planner",
	})
	if err != nil {
		t.Fatal(err)
	}
	if candidate.Candidate.AuthorityRevisionRowID.Valid {
		t.Fatalf("direct candidate inherited authority: %#v", candidate.Candidate)
	}
	if _, err := featureService.CompletePlanningCandidateReview(ctx, CompleteCandidateReviewInput{WorkspaceID: workspace.WorkspaceID, ReviewerIdentity: "auditor", Disposition: PlanningCandidateReviewReadyForApproval}); err != nil {
		t.Fatal(err)
	}
	candidateApproval, err := featureService.ApprovePlanningCandidate(ctx, CandidateApprovalInput{
		CandidateID: candidate.Candidate.CandidateID, ExpectedSHA256: candidate.Candidate.ArtifactSha256, ExpectedSizeBytes: candidate.Candidate.ArtifactSizeBytes,
		Bytes: bytes, ExpectedVersion: workspace.Version, ExpectedClosurePacketRowID: workspace.CurrentDiscoveryClosurePacketRowID,
		ExpectedAuthorityRevisionRowID: sql.NullInt64{}, OperatorConfirmationEvidence: "approve exact direct candidate", CreatedIdentity: "operator",
	})
	if err != nil {
		t.Fatal(err)
	}
	ticketService, err := workflowtickets.NewService(store)
	if err != nil {
		t.Fatal(err)
	}
	produced, err := ticketService.PromoteApprovedDeliveryTicketCandidate(ctx, workflowtickets.CandidateProductionInput{
		CandidateID: candidate.Candidate.CandidateID, ApprovalID: candidateApproval.Approval.ApprovalID,
		ExpectedVersion: workspace.Version, ExternalPriority: 5, CreatedIdentity: "planner",
	})
	if err != nil {
		t.Fatal(err)
	}
	if produced.ProducedApproval.AuthorityRevisionRowID.Valid {
		t.Fatalf("direct produced approval inherited authority: %#v", produced.ProducedApproval)
	}
	detail, err := ticketService.Read(ctx, produced.Ticket.TicketID)
	if err != nil || !detail.Readiness.Ready {
		t.Fatalf("direct ticket readiness = %#v, %v", detail.Readiness, err)
	}
	frontier, err := ticketService.ListFrontier(ctx, workspace.WorkspaceID)
	if err != nil || len(frontier.Entries) != 1 || frontier.Entries[0].RevisionRowID != produced.Revision.ID {
		t.Fatalf("direct frontier = %#v, %v", frontier, err)
	}
	if _, err := ticketService.Select(ctx, workflowtickets.SelectInput{WorkspaceID: workspace.WorkspaceID, TicketID: produced.Ticket.TicketID, RevisionRowID: produced.Revision.ID, Rationale: "select direct ticket"}); err != nil {
		t.Fatal(err)
	}
	brief, err := ticketService.AdmitTicketDesignBrief(ctx, workflowtickets.TicketDesignBriefAdmissionInput{WorkspaceID: workspace.WorkspaceID, Bytes: []byte(testfixtures.TicketDesignBrief), CreatedIdentity: "planner"})
	if err != nil || brief.Brief.ID == 0 {
		t.Fatalf("direct brief admission = %#v, %v", brief, err)
	}
	var vaultID int64
	if err := store.DB().QueryRowContext(ctx, `SELECT vault_row_id FROM source_vault_closures WHERE id = ?`, produced.Revision.SourceClosureRowID).Scan(&vaultID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().ExecContext(ctx, `INSERT INTO source_vault_closures (closure_id, vault_row_id, commit_oid, tree_oid, generation, ref_name, state, import_started_at, verified_at) VALUES (?, ?, ?, ?, 2, ?, 'ready', '2026-08-02T00:00:00.000000000Z', '2026-08-02T00:00:01.000000000Z')`, "closure-direct-replacement", vaultID, produced.Revision.BaseCommit, strings.Repeat("d", 40), "refs/relay/closures/direct-replacement"); err != nil {
		t.Fatal(err)
	}
	detail, err = ticketService.Read(ctx, produced.Ticket.TicketID)
	if err != nil || detail.Readiness.Ready || !hasTicketReadinessReason(detail.Readiness.Reasons, "source_not_current") {
		t.Fatalf("stale direct closure readiness = %#v, %v", detail.Readiness, err)
	}

	// A null approval is valid only for the authority-absent branch. Restoring
	// an authority immediately makes it stale rather than silently accepting it.
	if _, err := store.DB().ExecContext(ctx, `UPDATE feature_workspaces SET current_authority_revision_row_id = ?, version = version + 1 WHERE id = ?`, historicalAuthority.Int64, workspace.ID); err != nil {
		t.Fatal(err)
	}
	detail, err = ticketService.Read(ctx, produced.Ticket.TicketID)
	if err != nil || detail.Readiness.Ready || !hasTicketReadinessReason(detail.Readiness.Reasons, "approval_missing_or_stale") {
		t.Fatalf("authority-present null approval readiness = %#v, %v", detail.Readiness, err)
	}
}

func hasTicketReadinessReason(reasons []string, want string) bool {
	for _, reason := range reasons {
		if reason == want {
			return true
		}
	}
	return false
}

func TestApprovedDeliveryTicketCandidateProductionRollsBackAllStateOnBatchFailure(t *testing.T) {
	ctx, store, featureService, workspace, _ := deliveryTicketCandidateFixture(t, DiscoveryDestinationDirectDeliveryTicket)
	candidateBytes := deliveryTicketCandidateBytes("P3-T2", workspace.FeatureSlug, "candidate-production", strings.Repeat("a", 40))
	candidate, err := featureService.AdmitPlanningCandidate(ctx, CandidateAdmissionInput{
		WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, Family: CandidateFamilyDeliveryTicket,
		Filename: "discovery-proof.ticket-P3-T2.r1.delivery-ticket.json", Bytes: candidateBytes,
		SHA256: digestForPlanningTest(candidateBytes), RepoTarget: "candidate-production", Branch: "main", BaseCommit: strings.Repeat("a", 40),
		Destination: DiscoveryDestinationDirectDeliveryTicket, CreatedIdentity: "planner",
	})
	if err != nil {
		t.Fatal(err)
	}
	completeReadyPlanningReview(t, ctx, featureService, workspace.WorkspaceID)
	candidateApproval, err := featureService.ApprovePlanningCandidate(ctx, CandidateApprovalInput{
		CandidateID: candidate.Candidate.CandidateID, ExpectedSHA256: candidate.Candidate.ArtifactSha256,
		ExpectedSizeBytes: candidate.Candidate.ArtifactSizeBytes, Bytes: candidateBytes, ExpectedVersion: workspace.Version,
		ExpectedClosurePacketRowID: workspace.CurrentDiscoveryClosurePacketRowID, ExpectedAuthorityRevisionRowID: workspace.CurrentAuthorityRevisionRowID,
		OperatorConfirmationEvidence: "rollback candidate approval", CreatedIdentity: "auditor",
	})
	if err != nil {
		t.Fatal(err)
	}
	var discoveryArtifactsBefore int
	if err := store.DB().QueryRowContext(ctx, `SELECT count(*) FROM feature_workspace_discovery_artifacts`).Scan(&discoveryArtifactsBefore); err != nil {
		t.Fatal(err)
	}
	candidateFilenameCountBefore := countPlanningTestFiles(t, store.ArtifactStore().Root(), candidate.Candidate.Filename)
	var failErr = errors.New("forced candidate production batch failure")
	restoreHook := store.SetArtifactBatchPrepareCommitHookForTest(func() error { return failErr })
	defer restoreHook()
	ticketService, err := workflowtickets.NewService(store)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ticketService.PromoteApprovedDeliveryTicketCandidate(ctx, workflowtickets.CandidateProductionInput{
		CandidateID: candidate.Candidate.CandidateID, ApprovalID: candidateApproval.Approval.ApprovalID,
		ExpectedVersion: workspace.Version, ExternalPriority: 9, CreatedIdentity: "planner",
	}); !errors.Is(err, failErr) {
		t.Fatalf("forced production error = %v, want %v", err, failErr)
	}
	if _, err := store.GetDeliveryTicketByTicketID(ctx, "P3-T2"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("ticket after rollback = %v", err)
	}
	for table := range map[string]struct{}{
		"delivery_ticket_revisions": {}, "delivery_ticket_revision_members": {}, "delivery_ticket_revision_dependencies": {},
		"delivery_ticket_revision_approvals": {}, "delivery_ticket_production_links": {}, "feature_workspace_completion_reopenings": {},
	} {
		var count int
		if err := store.DB().QueryRowContext(ctx, `SELECT count(*) FROM `+table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("%s after rollback = %d, %v", table, count, err)
		}
	}
	workspaceAfter, err := store.GetFeatureWorkspaceByWorkspaceID(ctx, workspace.WorkspaceID)
	if err != nil || workspaceAfter.Version != workspace.Version || workspaceAfter.CurrentDiscoveryClosurePacketRowID.Int64 != workspace.CurrentDiscoveryClosurePacketRowID.Int64 {
		t.Fatalf("workspace after rollback = %#v, %v", workspaceAfter, err)
	}
	var discoveryArtifactsAfter int
	if err := store.DB().QueryRowContext(ctx, `SELECT count(*) FROM feature_workspace_discovery_artifacts`).Scan(&discoveryArtifactsAfter); err != nil || discoveryArtifactsAfter != discoveryArtifactsBefore {
		t.Fatalf("discovery artifacts after rollback = %d, want %d; err=%v", discoveryArtifactsAfter, discoveryArtifactsBefore, err)
	}
	if got := countPlanningTestFiles(t, store.ArtifactStore().Root(), candidate.Candidate.Filename); got != candidateFilenameCountBefore {
		t.Fatalf("candidate-named files after rollback = %d, want %d", got, candidateFilenameCountBefore)
	}
	if _, err := store.GetPlanningCandidateApprovalByApprovalID(ctx, candidateApproval.Approval.ApprovalID); err != nil {
		t.Fatalf("candidate approval was rolled back: %v", err)
	}
	candidateArtifact, err := store.GetFeatureWorkspaceDiscoveryArtifactByRowID(ctx, candidate.Candidate.ArtifactRowID)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.ArtifactStore().ReadVerifiedFile(workflowstoreArtifactFile(candidateArtifact, candidate.Candidate), 1<<20); err != nil {
		t.Fatalf("candidate artifact after rollback = %v", err)
	}
}

func TestDeliveryTicketCandidateProductionRejectsSupersededCurrentBasis(t *testing.T) {
	ctx, store, featureService, workspace, _ := deliveryTicketCandidateFixture(t, DiscoveryDestinationDirectDeliveryTicket)
	bytes := deliveryTicketCandidateBytes("P3-T-SUPERSEDED", workspace.FeatureSlug, "candidate-production", strings.Repeat("a", 40))
	admitAndApprove := func() CandidateApprovalResult {
		candidate, err := featureService.AdmitPlanningCandidate(ctx, CandidateAdmissionInput{
			WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, Family: CandidateFamilyDeliveryTicket,
			Filename: "discovery-proof.ticket-P3-T-SUPERSEDED.r1.delivery-ticket.json", Bytes: bytes, SHA256: digestForPlanningTest(bytes),
			RepoTarget: "candidate-production", Branch: "main", BaseCommit: strings.Repeat("a", 40), Destination: DiscoveryDestinationDirectDeliveryTicket, CreatedIdentity: "planner",
		})
		if err != nil {
			t.Fatal(err)
		}
		completeReadyPlanningReview(t, ctx, featureService, workspace.WorkspaceID)
		approval, err := featureService.ApprovePlanningCandidate(ctx, CandidateApprovalInput{
			CandidateID: candidate.Candidate.CandidateID, ExpectedSHA256: candidate.Candidate.ArtifactSha256, ExpectedSizeBytes: candidate.Candidate.ArtifactSizeBytes,
			Bytes: bytes, ExpectedVersion: workspace.Version, ExpectedClosurePacketRowID: workspace.CurrentDiscoveryClosurePacketRowID, ExpectedAuthorityRevisionRowID: workspace.CurrentAuthorityRevisionRowID,
			OperatorConfirmationEvidence: "ready", CreatedIdentity: "auditor",
		})
		if err != nil {
			t.Fatal(err)
		}
		return approval
	}
	first := admitAndApprove()
	_ = admitAndApprove()
	owner, err := workflowtickets.NewService(store)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := owner.PromoteApprovedDeliveryTicketCandidate(ctx, workflowtickets.CandidateProductionInput{CandidateID: first.Candidate.CandidateID, ApprovalID: first.Approval.ApprovalID, ExpectedVersion: workspace.Version, ExternalPriority: 0, CreatedIdentity: "planner"}); !errors.Is(err, workflowtickets.ErrStaleCandidateBasis) {
		t.Fatalf("superseded candidate production error = %v", err)
	}
	if _, err := store.GetDeliveryTicketByTicketID(ctx, "P3-T-SUPERSEDED"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("superseded candidate produced a ticket: %v", err)
	}
}

func deliveryTicketCandidateFixture(t *testing.T, destination DiscoveryDestination) (context.Context, *workflowstore.Store, *Service, workflowstore.FeatureWorkspace, workflowstore.IntegratedDiscoveryRevision) {
	t.Helper()
	ctx, store, service, workspace, revision := adoptedDiscoveryLifecycle(t, destination)
	if _, err := store.DB().ExecContext(ctx, `INSERT INTO repository_targets (repo_target, local_path, configured_branch_ref, configuration_version) VALUES ('candidate-production', 'C:/candidate-production', 'refs/heads/main', 1)`); err != nil {
		t.Fatal(err)
	}
	var vaultID, closureID, artifactID int64
	if err := store.DB().QueryRowContext(ctx, `INSERT INTO source_vaults (vault_id, repo_target, relative_path) VALUES (?, 'candidate-production', 'vaults/candidate-production') RETURNING id`, "vault-"+workspace.WorkspaceID).Scan(&vaultID); err != nil {
		t.Fatal(err)
	}
	commit := strings.Repeat("a", 40)
	if err := store.DB().QueryRowContext(ctx, `INSERT INTO source_vault_closures (closure_id, vault_row_id, commit_oid, tree_oid, generation, ref_name, state, import_started_at, verified_at) VALUES (?, ?, ?, ?, 1, ?, 'ready', '2026-08-01T00:00:00.000000000Z', '2026-08-01T00:00:01.000000000Z') RETURNING id`, "closure-"+workspace.WorkspaceID, vaultID, commit, strings.Repeat("b", 40), "refs/relay/closures/"+workspace.WorkspaceID).Scan(&closureID); err != nil {
		t.Fatal(err)
	}
	if err := store.DB().QueryRowContext(ctx, `SELECT id FROM artifacts ORDER BY id LIMIT 1`).Scan(&artifactID); err != nil {
		t.Fatal(err)
	}
	var artifactSHA string
	if err := store.DB().QueryRowContext(ctx, `SELECT sha256 FROM artifacts WHERE id = ?`, artifactID).Scan(&artifactSHA); err != nil {
		t.Fatal(err)
	}
	governingApproval, err := service.RecordAuthorityApproval(ctx, RecordAuthorityApprovalInput{
		WorkspaceID: workspace.WorkspaceID, Family: "requirements", ArtifactRowID: sql.NullInt64{Int64: artifactID, Valid: true},
		ArtifactSHA256: artifactSHA, OperatorConfirmationEvidence: "current governing authority",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, workspace, err = service.PublishAuthority(ctx, PublishAuthorityInput{
		WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version,
		SourceClosureID: sql.NullInt64{Int64: closureID, Valid: true},
		Layers:          []AuthorityLayerInput{{Kind: "requirements", ArtifactRowID: sql.NullInt64{Int64: artifactID, Valid: true}, ArtifactSHA256: artifactSHA, SourceClosureID: sql.NullInt64{Int64: closureID, Valid: true}, ApprovalRowID: sql.NullInt64{Int64: governingApproval.Approval.ID, Valid: true}}},
	}); err != nil {
		t.Fatal(err)
	}
	var closeErr error
	if _, workspace, closeErr = service.CloseFeatureDiscovery(ctx, CloseFeatureDiscoveryInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, ExpectedRevisionID: revision.DiscoveryRevisionID, Destination: destination, CreatedIdentity: "operator"}); closeErr != nil {
		t.Fatal(closeErr)
	}
	completion, err := service.Complete(ctx, CompletionInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, OperatorConfirmed: true})
	if err != nil {
		t.Fatal(err)
	}
	workspace = completion.Workspace
	return ctx, store, service, workspace, revision
}

func deliveryTicketCandidateBytes(ticketID, featureSlug, repoTarget, commit string) []byte {
	return []byte(`{"schema_version":"1.0","feature_slug":"` + featureSlug + `","ticket_id":"` + ticketID + `","revision":1,"replaces_revision":null,"repo_target":"` + repoTarget + `","branch":"main","base_commit":"` + commit + `","goal":"Produce a deterministic Delivery Ticket.","context":"The candidate is compiled without changing its approved bytes.","scope":{"in_scope":["Compile the candidate."],"out_of_scope":["Mutate candidate bytes."]},"depends_on":[],"implementation_obligations":[{"path":"internal/app/tickets","obligation":"Produce the approved candidate exactly."}],"validation_intent":["Run focused candidate production tests."],"transition_applicability":"not_required","completion_criteria":["Canonical and rendered identities remain distinct."]}
`)
}

func digestForPlanningTest(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}

func workflowstoreArtifactFile(artifact workflowstore.FeatureWorkspaceDiscoveryArtifact, candidate workflowstore.PlanningCandidate) workflowartifacts.File {
	return workflowartifacts.File{RelativePath: artifact.RelativePath, SHA256: candidate.ArtifactSha256, SizeBytes: candidate.ArtifactSizeBytes, MediaType: artifact.MediaType}
}

func countPlanningTestFiles(t *testing.T, root, basename string) int {
	t.Helper()
	count := 0
	if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && entry.Name() == basename {
			count++
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return count
}

func TestDeliveryTicketCandidateAdmissionRejectsStaleAuthoritySource(t *testing.T) {
	ctx, store, service, workspace, _ := deliveryTicketCandidateFixture(t, DiscoveryDestinationDirectDeliveryTicket)
	authority, err := store.GetFeatureWorkspaceAuthorityRevisionByRowID(ctx, workspace.CurrentAuthorityRevisionRowID.Int64)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().ExecContext(ctx, `UPDATE source_vault_closures SET state = 'unavailable', failure_reason = 'source_commit_missing', verified_at = NULL WHERE id = ?`, authority.SourceClosureRowID.Int64); err != nil {
		t.Fatal(err)
	}
	candidateBytes := deliveryTicketCandidateBytes("P3-T4", workspace.FeatureSlug, "candidate-production", strings.Repeat("a", 40))
	if _, err := service.AdmitPlanningCandidate(ctx, CandidateAdmissionInput{
		WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, Family: CandidateFamilyDeliveryTicket,
		Filename: "discovery-proof.ticket-P3-T4.r1.delivery-ticket.json", Bytes: candidateBytes,
		SHA256: digestForPlanningTest(candidateBytes), RepoTarget: "candidate-production", Branch: "main", BaseCommit: strings.Repeat("a", 40),
		Destination: DiscoveryDestinationDirectDeliveryTicket, CreatedIdentity: "planner",
	}); !errors.Is(err, ErrStaleCandidateBasis) {
		t.Fatalf("stale authority source admission error = %v, want ErrStaleCandidateBasis", err)
	}
	var candidates int
	if err := store.DB().QueryRowContext(ctx, `SELECT count(*) FROM planning_candidates`).Scan(&candidates); err != nil {
		t.Fatal(err)
	}
	if candidates != 0 {
		t.Fatalf("planning candidates after stale authority rejection = %d, want 0", candidates)
	}
}

func TestApprovedDeliveryTicketCandidateRejectsCompilerErrorsBeforePublication(t *testing.T) {
	ctx, store, featureService, workspace, _ := deliveryTicketCandidateFixture(t, DiscoveryDestinationDirectDeliveryTicket)
	candidateBytes := []byte(`{"schema_version":"1.0","feature_slug":"discovery-proof"}
`)
	candidate, err := featureService.AdmitPlanningCandidate(ctx, CandidateAdmissionInput{
		WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, Family: CandidateFamilyDeliveryTicket,
		Filename: "discovery-proof.ticket-P3-T3.r1.delivery-ticket.json", Bytes: candidateBytes,
		SHA256: digestForPlanningTest(candidateBytes), RepoTarget: "candidate-production", Branch: "main", BaseCommit: strings.Repeat("a", 40),
		Destination: DiscoveryDestinationDirectDeliveryTicket, CreatedIdentity: "planner",
	})
	if err != nil {
		t.Fatal(err)
	}
	completeReadyPlanningReview(t, ctx, featureService, workspace.WorkspaceID)
	candidateApproval, err := featureService.ApprovePlanningCandidate(ctx, CandidateApprovalInput{
		CandidateID: candidate.Candidate.CandidateID, ExpectedSHA256: candidate.Candidate.ArtifactSha256,
		ExpectedSizeBytes: candidate.Candidate.ArtifactSizeBytes, Bytes: candidateBytes, ExpectedVersion: workspace.Version,
		ExpectedClosurePacketRowID: workspace.CurrentDiscoveryClosurePacketRowID, ExpectedAuthorityRevisionRowID: workspace.CurrentAuthorityRevisionRowID,
		OperatorConfirmationEvidence: "invalid compiler input is still exact candidate bytes", CreatedIdentity: "auditor",
	})
	if err != nil {
		t.Fatal(err)
	}
	var artifactsBefore int
	if err := store.DB().QueryRowContext(ctx, `SELECT count(*) FROM feature_workspace_discovery_artifacts`).Scan(&artifactsBefore); err != nil {
		t.Fatal(err)
	}
	ticketService, err := workflowtickets.NewService(store)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ticketService.PromoteApprovedDeliveryTicketCandidate(ctx, workflowtickets.CandidateProductionInput{
		CandidateID: candidate.Candidate.CandidateID, ApprovalID: candidateApproval.Approval.ApprovalID,
		ExpectedVersion: workspace.Version, ExternalPriority: 1, CreatedIdentity: "planner",
	}); !errors.Is(err, workflowtickets.ErrCandidateCompilation) {
		t.Fatalf("compiler rejection = %v", err)
	}
	if _, err := store.GetDeliveryTicketByTicketID(ctx, "P3-T3"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("compiler failure published ticket = %v", err)
	}
	var artifactsAfter int
	if err := store.DB().QueryRowContext(ctx, `SELECT count(*) FROM feature_workspace_discovery_artifacts`).Scan(&artifactsAfter); err != nil || artifactsAfter != artifactsBefore {
		t.Fatalf("artifacts after compiler rejection = %d, want %d; err=%v", artifactsAfter, artifactsBefore, err)
	}
}
