package features

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"strings"
	"testing"

	workflowtickets "relay/internal/app/tickets"
	"relay/internal/speccompiler"
	workflowstore "relay/internal/store/workflow"
)

func TestDeliveryTicketCandidateAdmissionContinuesToReview(t *testing.T) {
	ctx, _, service, workspace, _ := deliveryTicketCandidateFixture(t, DiscoveryDestinationDirectDeliveryTicket)
	bytes := deliveryTicketCandidateBytes("P3-T1", workspace.FeatureSlug, "candidate-production", strings.Repeat("a", 40))
	result, err := service.AdmitPlanningCandidate(ctx, CandidateAdmissionInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, Family: CandidateFamilyDeliveryTicket, Filename: "discovery-proof.ticket-P3-T1.r1.delivery-ticket.json", Bytes: bytes, SHA256: digestForPlanningTest(bytes), RepoTarget: "candidate-production", Branch: "main", BaseCommit: strings.Repeat("a", 40), Destination: DiscoveryDestinationDirectDeliveryTicket, CreatedIdentity: "planner"})
	if err != nil || result.AuthorizedNextAction != "review_candidate" {
		t.Fatalf("admission=%+v err=%v", result, err)
	}
}

func TestDeliveryTicketCandidateReadyReviewImmediatelyApprovesBeforeProduction(t *testing.T) {
	ctx, _, service, workspace, _ := deliveryTicketCandidateFixture(t, DiscoveryDestinationDirectDeliveryTicket)
	bytes := deliveryTicketCandidateBytes("P3-T2", workspace.FeatureSlug, "candidate-production", strings.Repeat("a", 40))
	candidate, err := service.AdmitPlanningCandidate(ctx, CandidateAdmissionInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, Family: CandidateFamilyDeliveryTicket, Filename: "discovery-proof.ticket-P3-T2.r1.delivery-ticket.json", Bytes: bytes, SHA256: digestForPlanningTest(bytes), RepoTarget: "candidate-production", Branch: "main", BaseCommit: strings.Repeat("a", 40), Destination: DiscoveryDestinationDirectDeliveryTicket, CreatedIdentity: "planner"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.CompletePlanningCandidateReview(ctx, CompleteCandidateReviewInput{WorkspaceID: workspace.WorkspaceID, ReviewerIdentity: "auditor", Disposition: PlanningCandidateReviewReadyForApproval}); err != nil {
		t.Fatal(err)
	}
	approval, err := service.ApproveCurrentPlanningCandidate(ctx, CandidateApprovalInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, OperatorConfirmationEvidence: "approved exact candidate", CreatedIdentity: "operator"})
	if err != nil || approval.Candidate.CandidateID != candidate.Candidate.CandidateID || approval.Approval.ApprovalID == "" {
		t.Fatalf("approval=%+v err=%v", approval, err)
	}
	owner, err := workflowtickets.NewService(service.store)
	if err != nil {
		t.Fatal(err)
	}
	produced, err := owner.PromoteApprovedDeliveryTicketCandidate(ctx, workflowtickets.CandidateProductionInput{CandidateID: candidate.Candidate.CandidateID, ApprovalID: approval.Approval.ApprovalID, ExpectedVersion: workspace.Version, CreatedIdentity: "planner"})
	if err != nil || produced.Ticket.TicketID != "P3-T2" {
		t.Fatalf("production=%+v err=%v", produced, err)
	}
}

func TestDeliveryTicketCandidateAdmissionSupportsAllDeliveryDestinations(t *testing.T) {
	for _, destination := range []DiscoveryDestination{DiscoveryDestinationDirectDeliveryTicket, DiscoveryDestinationRequirements, DiscoveryDestinationSharedDesign, DiscoveryDestinationRequirementsThenSharedDesign, DiscoveryDestinationExistingRouteContinuation} {
		t.Run(string(destination), func(t *testing.T) {
			ctx, store, service, workspace, revision := adoptedDiscoveryLifecycle(t, destination)
			_, workspace, err := service.CloseFeatureDiscovery(ctx, CloseFeatureDiscoveryInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, ExpectedRevisionID: revision.DiscoveryRevisionID, Destination: destination, CreatedIdentity: "operator"})
			if err != nil {
				t.Fatal(err)
			}
			repoTarget := "candidate-" + strings.ReplaceAll(string(destination), "_", "-")
			if _, err := store.DB().ExecContext(ctx, `INSERT INTO repository_targets (repo_target, local_path, configured_branch_ref, configuration_version) VALUES (?, ?, 'refs/heads/main', 1)`, repoTarget, "C:/"+repoTarget); err != nil {
				t.Fatal(err)
			}
			bytes := deliveryTicketCandidateBytes("P3-T1", workspace.FeatureSlug, repoTarget, strings.Repeat("a", 40))
			candidate, err := service.AdmitPlanningCandidate(ctx, CandidateAdmissionInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, Family: CandidateFamilyDeliveryTicket, Filename: "discovery-proof.ticket-P3-T1.r1.delivery-ticket.json", Bytes: bytes, SHA256: digestForPlanningTest(bytes), RepoTarget: repoTarget, Branch: "main", BaseCommit: strings.Repeat("a", 40), Destination: destination, CreatedIdentity: "planner"})
			if err != nil || candidate.Candidate.Destination != string(destination) || candidate.AuthorizedNextAction != "review_candidate" {
				t.Fatalf("delivery candidate admission = %#v, %v", candidate, err)
			}
		})
	}
}

func TestApprovedDeliveryTicketCandidateProductionUsesCompilerIdentitiesAndApprovalOrdering(t *testing.T) {
	ctx, store, service, workspace, _ := deliveryTicketCandidateFixture(t, DiscoveryDestinationDirectDeliveryTicket)
	bytes := deliveryTicketCandidateBytes("P3-T-IDENTITIES", workspace.FeatureSlug, "candidate-production", strings.Repeat("a", 40))
	candidate, err := service.AdmitPlanningCandidate(ctx, CandidateAdmissionInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, Family: CandidateFamilyDeliveryTicket, Filename: "discovery-proof.ticket-P3-T-IDENTITIES.r1.delivery-ticket.json", Bytes: bytes, SHA256: digestForPlanningTest(bytes), RepoTarget: "candidate-production", Branch: "main", BaseCommit: strings.Repeat("a", 40), Destination: DiscoveryDestinationDirectDeliveryTicket, CreatedIdentity: "planner"})
	if err != nil {
		t.Fatal(err)
	}
	approval := approveCurrentPlanningCandidate(t, ctx, service, workspace, "exact delivery candidate")
	if _, err := store.DB().ExecContext(ctx, `CREATE TRIGGER candidate_production_requires_approval BEFORE UPDATE OF current_revision_row_id ON delivery_tickets WHEN NEW.current_revision_row_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM delivery_ticket_revision_approvals WHERE revision_row_id = NEW.current_revision_row_id AND approval_kind = 'delivery' AND approval_state = 'approved') BEGIN SELECT RAISE(ABORT, 'produced approval must precede current pointer'); END`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = store.DB().ExecContext(ctx, `DROP TRIGGER candidate_production_requires_approval`) })
	owner, err := workflowtickets.NewService(store)
	if err != nil {
		t.Fatal(err)
	}
	produced, err := owner.PromoteApprovedDeliveryTicketCandidate(ctx, workflowtickets.CandidateProductionInput{CandidateID: candidate.Candidate.CandidateID, ApprovalID: approval.Approval.ApprovalID, ExpectedVersion: workspace.Version, ExternalPriority: 7, CreatedIdentity: "planner"})
	if err != nil {
		t.Fatal(err)
	}
	compiled, document := speccompiler.CompileDeliveryTicket(candidate.Candidate.Filename, bytes)
	if len(compiled.Errors) != 0 || document == nil || compiled.OutputFilename == nil || compiled.Markdown == nil {
		t.Fatalf("fixture compiler result = %#v, document=%#v", compiled, document)
	}
	if produced.Canonical.SHA256 != digestForPlanningTest(bytes) || produced.Canonical.SizeBytes != int64(len(bytes)) || produced.Rendered.SHA256 != digestForPlanningTest([]byte(*compiled.Markdown)) || produced.Rendered.SizeBytes != int64(len(*compiled.Markdown)) {
		t.Fatalf("artifact identities = %#v %#v", produced.Canonical, produced.Rendered)
	}
	if produced.CandidateApproval.ID != approval.Approval.ID || produced.ProducedApproval.ApprovalID == approval.Approval.ApprovalID || produced.ProducedApproval.RevisionRowID != produced.Revision.ID || produced.ProductionLink.CandidateRowID != candidate.Candidate.ID || produced.ProductionLink.CanonicalJsonArtifactRowID == produced.ProductionLink.RenderedMarkdownArtifactRowID || produced.ProductionLink.ProducedRevisionIdentity != "P3-T-IDENTITIES:r1" {
		t.Fatalf("distinct production identities = %#v", produced)
	}
	current, err := store.GetDeliveryTicketByTicketID(ctx, "P3-T-IDENTITIES")
	if err != nil || !current.CurrentRevisionRowID.Valid || current.CurrentRevisionRowID.Int64 != produced.Revision.ID {
		t.Fatalf("current produced revision = %#v, %v", current, err)
	}
}

func TestApprovedDeliveryTicketCandidateProductionRollsBackAllStateOnBatchFailure(t *testing.T) {
	ctx, store, service, workspace, _ := deliveryTicketCandidateFixture(t, DiscoveryDestinationDirectDeliveryTicket)
	bytes := deliveryTicketCandidateBytes("P3-T-ROLLBACK", workspace.FeatureSlug, "candidate-production", strings.Repeat("a", 40))
	candidate, err := service.AdmitPlanningCandidate(ctx, CandidateAdmissionInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, Family: CandidateFamilyDeliveryTicket, Filename: "discovery-proof.ticket-P3-T-ROLLBACK.r1.delivery-ticket.json", Bytes: bytes, SHA256: digestForPlanningTest(bytes), RepoTarget: "candidate-production", Branch: "main", BaseCommit: strings.Repeat("a", 40), Destination: DiscoveryDestinationDirectDeliveryTicket, CreatedIdentity: "planner"})
	if err != nil {
		t.Fatal(err)
	}
	approval := approveCurrentPlanningCandidate(t, ctx, service, workspace, "rollback candidate")
	failErr := errors.New("forced candidate production batch failure")
	restoreHook := store.SetArtifactBatchPrepareCommitHookForTest(func() error { return failErr })
	defer restoreHook()
	owner, err := workflowtickets.NewService(store)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := owner.PromoteApprovedDeliveryTicketCandidate(ctx, workflowtickets.CandidateProductionInput{CandidateID: candidate.Candidate.CandidateID, ApprovalID: approval.Approval.ApprovalID, ExpectedVersion: workspace.Version, ExternalPriority: 9, CreatedIdentity: "planner"}); !errors.Is(err, failErr) {
		t.Fatalf("forced production error = %v", err)
	}
	if _, err := store.GetDeliveryTicketByTicketID(ctx, "P3-T-ROLLBACK"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("ticket after rollback = %v", err)
	}
	for _, table := range []string{"delivery_ticket_revisions", "delivery_ticket_revision_members", "delivery_ticket_revision_dependencies", "delivery_ticket_revision_approvals", "delivery_ticket_production_links", "feature_workspace_completion_reopenings"} {
		var count int
		if err := store.DB().QueryRowContext(ctx, `SELECT count(*) FROM `+table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("%s after rollback = %d, %v", table, count, err)
		}
	}
	workspaceAfter, err := store.GetFeatureWorkspaceByWorkspaceID(ctx, workspace.WorkspaceID)
	if err != nil || workspaceAfter.Version != workspace.Version {
		t.Fatalf("workspace after rollback = %#v, %v", workspaceAfter, err)
	}
}

func TestDeliveryTicketCandidateProductionRejectsSupersededCurrentBasis(t *testing.T) {
	ctx, store, service, workspace, _ := deliveryTicketCandidateFixture(t, DiscoveryDestinationDirectDeliveryTicket)
	bytes := deliveryTicketCandidateBytes("P3-T-SUPERSEDED", workspace.FeatureSlug, "candidate-production", strings.Repeat("a", 40))
	admitAndApprove := func() CandidateApprovalResult {
		if _, err := service.AdmitPlanningCandidate(ctx, CandidateAdmissionInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, Family: CandidateFamilyDeliveryTicket, Filename: "discovery-proof.ticket-P3-T-SUPERSEDED.r1.delivery-ticket.json", Bytes: bytes, SHA256: digestForPlanningTest(bytes), RepoTarget: "candidate-production", Branch: "main", BaseCommit: strings.Repeat("a", 40), Destination: DiscoveryDestinationDirectDeliveryTicket, CreatedIdentity: "planner"}); err != nil {
			t.Fatal(err)
		}
		return approveCurrentPlanningCandidate(t, ctx, service, workspace, "ready")
	}
	first := admitAndApprove()
	_ = admitAndApprove()
	owner, err := workflowtickets.NewService(store)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := owner.PromoteApprovedDeliveryTicketCandidate(ctx, workflowtickets.CandidateProductionInput{CandidateID: first.Candidate.CandidateID, ApprovalID: first.Approval.ApprovalID, ExpectedVersion: workspace.Version, CreatedIdentity: "planner"}); !errors.Is(err, workflowtickets.ErrStaleCandidateBasis) {
		t.Fatalf("superseded candidate production error = %v", err)
	}
	if _, err := store.GetDeliveryTicketByTicketID(ctx, "P3-T-SUPERSEDED"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("superseded candidate produced a ticket: %v", err)
	}
}

func TestDirectDeliveryCandidateUsesNullAuthorityOnlyWhileAuthorityIsAbsent(t *testing.T) {
	ctx, store, service, workspace, _ := deliveryTicketCandidateFixture(t, DiscoveryDestinationDirectDeliveryTicket)
	if !workspace.CurrentAuthorityRevisionRowID.Valid {
		t.Fatal("fixture did not establish authority")
	}
	historicalAuthority := workspace.CurrentAuthorityRevisionRowID
	if _, err := store.DB().ExecContext(ctx, `UPDATE feature_workspaces SET current_authority_revision_row_id = NULL, version = version + 1 WHERE id = ?`, workspace.ID); err != nil {
		t.Fatal(err)
	}
	workspace, err := store.GetFeatureWorkspaceByWorkspaceID(ctx, workspace.WorkspaceID)
	if err != nil || workspace.CurrentAuthorityRevisionRowID.Valid {
		t.Fatalf("direct workspace authority = %#v, %v", workspace.CurrentAuthorityRevisionRowID, err)
	}
	bytes := deliveryTicketCandidateBytes("P3-T-DIRECT", workspace.FeatureSlug, "candidate-production", strings.Repeat("a", 40))
	candidate, err := service.AdmitPlanningCandidate(ctx, CandidateAdmissionInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, Family: CandidateFamilyDeliveryTicket, Filename: "discovery-proof.ticket-P3-T-DIRECT.r1.delivery-ticket.json", Bytes: bytes, SHA256: digestForPlanningTest(bytes), RepoTarget: "candidate-production", Branch: "main", BaseCommit: strings.Repeat("a", 40), Destination: DiscoveryDestinationDirectDeliveryTicket, CreatedIdentity: "planner"})
	if err != nil || candidate.Candidate.AuthorityRevisionRowID.Valid {
		t.Fatalf("direct candidate = %#v, %v", candidate, err)
	}
	approval := approveCurrentPlanningCandidate(t, ctx, service, workspace, "approve direct candidate")
	owner, err := workflowtickets.NewService(store)
	if err != nil {
		t.Fatal(err)
	}
	produced, err := owner.PromoteApprovedDeliveryTicketCandidate(ctx, workflowtickets.CandidateProductionInput{CandidateID: candidate.Candidate.CandidateID, ApprovalID: approval.Approval.ApprovalID, ExpectedVersion: workspace.Version, ExternalPriority: 5, CreatedIdentity: "planner"})
	if err != nil || produced.ProducedApproval.AuthorityRevisionRowID.Valid {
		t.Fatalf("direct production = %#v, %v", produced, err)
	}
	detail, err := owner.Read(ctx, produced.Ticket.TicketID)
	if err != nil || !detail.Readiness.Ready {
		t.Fatalf("direct ticket readiness = %#v, %v", detail.Readiness, err)
	}
	if _, err := store.DB().ExecContext(ctx, `UPDATE feature_workspaces SET current_authority_revision_row_id = ?, version = version + 1 WHERE id = ?`, historicalAuthority.Int64, workspace.ID); err != nil {
		t.Fatal(err)
	}
	detail, err = owner.Read(ctx, produced.Ticket.TicketID)
	if err != nil || detail.Readiness.Ready || !ticketReadinessHasReason(detail.Readiness.Reasons, "approval_missing_or_stale") {
		t.Fatalf("authority-present null approval readiness = %#v, %v", detail.Readiness, err)
	}
}

func ticketReadinessHasReason(reasons []string, want string) bool {
	for _, reason := range reasons {
		if reason == want {
			return true
		}
	}
	return false
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
	bytes := deliveryTicketCandidateBytes("P3-T-STALE", workspace.FeatureSlug, "candidate-production", strings.Repeat("a", 40))
	if _, err := service.AdmitPlanningCandidate(ctx, CandidateAdmissionInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, Family: CandidateFamilyDeliveryTicket, Filename: "discovery-proof.ticket-P3-T-STALE.r1.delivery-ticket.json", Bytes: bytes, SHA256: digestForPlanningTest(bytes), RepoTarget: "candidate-production", Branch: "main", BaseCommit: strings.Repeat("a", 40), Destination: DiscoveryDestinationDirectDeliveryTicket, CreatedIdentity: "planner"}); !errors.Is(err, ErrStaleCandidateBasis) {
		t.Fatalf("stale authority source admission error = %v", err)
	}
}

func TestApprovedDeliveryTicketCandidateRejectsCompilerErrorsBeforePublication(t *testing.T) {
	ctx, store, service, workspace, _ := deliveryTicketCandidateFixture(t, DiscoveryDestinationDirectDeliveryTicket)
	bytes := []byte(`{"schema_version":"1.0","feature_slug":"discovery-proof"}`)
	candidate, err := service.AdmitPlanningCandidate(ctx, CandidateAdmissionInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, Family: CandidateFamilyDeliveryTicket, Filename: "discovery-proof.ticket-P3-T-INVALID.r1.delivery-ticket.json", Bytes: bytes, SHA256: digestForPlanningTest(bytes), RepoTarget: "candidate-production", Branch: "main", BaseCommit: strings.Repeat("a", 40), Destination: DiscoveryDestinationDirectDeliveryTicket, CreatedIdentity: "planner"})
	if err != nil {
		t.Fatal(err)
	}
	approval := approveCurrentPlanningCandidate(t, ctx, service, workspace, "invalid compiler input is still exact candidate bytes")
	owner, err := workflowtickets.NewService(store)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := owner.PromoteApprovedDeliveryTicketCandidate(ctx, workflowtickets.CandidateProductionInput{CandidateID: candidate.Candidate.CandidateID, ApprovalID: approval.Approval.ApprovalID, ExpectedVersion: workspace.Version, CreatedIdentity: "planner"}); !errors.Is(err, workflowtickets.ErrCandidateCompilation) {
		t.Fatalf("compiler rejection = %v", err)
	}
	if _, err := store.GetDeliveryTicketByTicketID(ctx, "P3-T-INVALID"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("compiler failure published ticket = %v", err)
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
	governing, err := service.RecordAuthorityApproval(ctx, RecordAuthorityApprovalInput{WorkspaceID: workspace.WorkspaceID, Family: "requirements", ArtifactRowID: sql.NullInt64{Int64: artifactID, Valid: true}, ArtifactSHA256: artifactSHA, OperatorConfirmationEvidence: "current governing authority"})
	if err != nil {
		t.Fatal(err)
	}
	if _, workspace, err = service.PublishAuthority(ctx, PublishAuthorityInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, SourceClosureID: sql.NullInt64{Int64: closureID, Valid: true}, Layers: []AuthorityLayerInput{{Kind: "requirements", ArtifactRowID: sql.NullInt64{Int64: artifactID, Valid: true}, ArtifactSHA256: artifactSHA, SourceClosureID: sql.NullInt64{Int64: closureID, Valid: true}, ApprovalRowID: sql.NullInt64{Int64: governing.Approval.ID, Valid: true}}}}); err != nil {
		t.Fatal(err)
	}
	if _, workspace, err = service.CloseFeatureDiscovery(ctx, CloseFeatureDiscoveryInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, ExpectedRevisionID: revision.DiscoveryRevisionID, Destination: destination, CreatedIdentity: "operator"}); err != nil {
		t.Fatal(err)
	}
	completion, err := service.Complete(ctx, CompletionInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, OperatorConfirmed: true})
	if err != nil {
		t.Fatal(err)
	}
	return ctx, store, service, completion.Workspace, revision
}

func deliveryTicketCandidateBytes(ticketID, featureSlug, repoTarget, commit string) []byte {
	return []byte(`{"schema_version":"1.0","feature_slug":"` + featureSlug + `","ticket_id":"` + ticketID + `","revision":1,"replaces_revision":null,"repo_target":"` + repoTarget + `","branch":"main","base_commit":"` + commit + `","goal":"Produce a deterministic Delivery Ticket.","context":"The candidate is compiled without changing its approved bytes.","scope":{"in_scope":["Compile the candidate."],"out_of_scope":["Mutate candidate bytes."]},"depends_on":[],"implementation_obligations":[{"path":"internal/app/tickets","obligation":"Produce the approved candidate exactly."}],"validation_intent":["Run focused candidate production tests."],"transition_applicability":"not_required","completion_criteria":["Canonical and rendered identities remain distinct."]}`)
}
func digestForPlanningTest(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}
