package features

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"relay/internal/guidedapp"
	workflowstore "relay/internal/store/workflow"
)

type unreadableIntegrityPackageOwner struct{}

func (unreadableIntegrityPackageOwner) ReadWorkspacePackageState(context.Context, string) (guidedapp.PackageState, error) {
	return guidedapp.PackageState{}, errors.New("package read unavailable")
}
func (unreadableIntegrityPackageOwner) ApproveCurrentPackage(context.Context, guidedapp.ApprovePackageInput) error {
	return errors.New("not used")
}
func (unreadableIntegrityPackageOwner) PrepareCurrentSelection(context.Context, guidedapp.PreparePackageInput) (guidedapp.PreparePackageResult, error) {
	return guidedapp.PreparePackageResult{}, errors.New("not used")
}

func TestGuidedIntegrityReportsUnreadableOwnerWithoutInventingIdentity(t *testing.T) {
	ctx, _, service, workspace, _ := adoptedDiscoveryLifecycle(t, DiscoveryDestinationRequirements)
	service.SetGuidedPackageOwnerForTest(unreadableIntegrityPackageOwner{})
	integrity := guidedIntegrity(ctx, service, workspace, DiscoveryAssessment{}, nil, GuidedDeliverySection{}, GuidedPrototypeSection{})
	if integrity.Delivery.Package != nil {
		t.Fatalf("package identity from failed read = %+v", integrity.Delivery.Package)
	}
	for _, diagnostic := range integrity.Diagnostics {
		if diagnostic.Domain == "delivery.package" && diagnostic.Condition == "unreadable" {
			return
		}
	}
	t.Fatalf("integrity diagnostics = %+v", integrity.Diagnostics)
}

// TestGuidedIntegrityComposesTypedDiscoveryAuthorityPlanningIdentities drives
// the real journey from adopted discovery through close, candidate admission,
// review, approval, promotion, and reopen, then asserts the exact typed
// integrity projection: current revision, current closure packet ID+digest,
// revision history with predecessor and reopen linkage, authority revisions
// with layers resolved to public artifact identities, and planning candidates
// with promotion linkage and approval identities.
func TestGuidedIntegrityComposesTypedDiscoveryAuthorityPlanningIdentities(t *testing.T) {
	ctx, store, service, workspace, revision := adoptedDiscoveryLifecycle(t, DiscoveryDestinationRequirements)
	service.SetGuidedAuditOwnerForTest(&guidedFakeAuditOwner{})
	if _, err := store.DB().ExecContext(ctx, `INSERT INTO repository_targets (repo_target, local_path, configured_branch_ref, configuration_version) VALUES ('guided-integrity-repo', 'C:/guided-integrity-repo', 'refs/heads/main', 1)`); err != nil {
		t.Fatal(err)
	}
	insertReadyPlanningSourceClosure(t, ctx, store, workspace, "guided-integrity-repo", strings.Repeat("a", 40))
	closed, workspace, err := service.CloseFeatureDiscovery(ctx, CloseFeatureDiscoveryInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, ExpectedRevisionID: revision.DiscoveryRevisionID, Destination: DiscoveryDestinationRequirements, CreatedIdentity: "operator"})
	if err != nil {
		t.Fatal(err)
	}
	bytes := []byte("# guided integrity requirements\n")
	admitted, err := service.AdmitPlanningCandidate(ctx, CandidateAdmissionInput{
		WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, Family: CandidateFamilyRequirements,
		Filename: workspace.FeatureSlug + ".requirements.md", Bytes: bytes, SHA256: discoveryTestDigest(bytes), RepoTarget: "guided-integrity-repo",
		Branch: "main", BaseCommit: strings.Repeat("a", 40), Destination: DiscoveryDestinationRequirements, CreatedIdentity: "planner",
	})
	if err != nil {
		t.Fatal(err)
	}
	approved, err := service.CompleteAndApprovePlanningCandidate(ctx, CompleteCandidateReviewInput{WorkspaceID: workspace.WorkspaceID, ReviewerIdentity: "auditor", Disposition: PlanningCandidateReviewReadyForApproval}, CandidateApprovalInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, OperatorConfirmationEvidence: "integrity review", CreatedIdentity: "auditor"})
	if err != nil {
		t.Fatal(err)
	}
	promoted, err := service.PromoteApprovedPlanningCandidate(ctx, CandidatePromotionInput{CandidateID: approved.Candidate.CandidateID, ApprovalID: approved.Approval.ApprovalID, ExpectedVersion: workspace.Version, CreatedIdentity: "planner"})
	if err != nil {
		t.Fatal(err)
	}
	projection, err := service.ReadGuidedProjection(ctx, promoted.Workspace.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	integrity := projection.Diagnostics.Integrity

	// AC10: the current discovery revision identity.
	if integrity.Discovery.CurrentRevisionID != revision.DiscoveryRevisionID {
		t.Fatalf("current revision id = %q, want %q", integrity.Discovery.CurrentRevisionID, revision.DiscoveryRevisionID)
	}
	// AC11: the current closure packet identity plus its manifest digest.
	if integrity.Discovery.CurrentPacket == nil || integrity.Discovery.CurrentPacket.ClosurePacketID != closed.Packet.ClosurePacketID || integrity.Discovery.CurrentPacket.SHA256 != closed.Packet.ManifestSha256 {
		t.Fatalf("current packet = %+v, want %q/%q", integrity.Discovery.CurrentPacket, closed.Packet.ClosurePacketID, closed.Packet.ManifestSha256)
	}
	// AC13/AC25: exactly one authority revision, current, with identity+number.
	if len(integrity.Authority) != 1 {
		t.Fatalf("authority revisions = %d, want 1: %+v", len(integrity.Authority), integrity.Authority)
	}
	revisionEntry := integrity.Authority[0]
	if revisionEntry.AuthorityRevisionID != promoted.Detail.Revision.AuthorityRevisionID || revisionEntry.RevisionNumber != 1 || revisionEntry.Historical {
		t.Fatalf("authority revision = %+v", revisionEntry)
	}
	// AC14: layer kind, exact stable artifact identity (public domain ID, not
	// the row ID), digest, and source closure identity.
	if len(revisionEntry.Layers) != 1 || revisionEntry.Layers[0].Kind != CandidateFamilyRequirements {
		t.Fatalf("authority layers = %+v", revisionEntry.Layers)
	}
	candidateArtifact, err := store.GetFeatureWorkspaceDiscoveryArtifactByRowID(ctx, admitted.Candidate.ArtifactRowID)
	if err != nil {
		t.Fatal(err)
	}
	layer := revisionEntry.Layers[0]
	if layer.ArtifactID != candidateArtifact.DiscoveryArtifactID || layer.ArtifactID == fmt.Sprint(admitted.Candidate.ArtifactRowID) {
		t.Fatalf("layer artifact identity = %q (row %d), want public domain ID %q", layer.ArtifactID, admitted.Candidate.ArtifactRowID, candidateArtifact.DiscoveryArtifactID)
	}
	if layer.SHA256 != admitted.Candidate.ArtifactSha256 || layer.SourceClosureID == "" {
		t.Fatalf("layer = %+v", layer)
	}
	// AC15/AC16/AC25: the planning candidate with promotion linkage, exact
	// artifact identity/digest/size, and its approval identity.
	if len(integrity.Planning) != 1 {
		t.Fatalf("planning candidates = %d, want 1: %+v", len(integrity.Planning), integrity.Planning)
	}
	candidateEntry := integrity.Planning[0]
	if candidateEntry.CandidateID != admitted.Candidate.CandidateID || candidateEntry.Family != CandidateFamilyRequirements || candidateEntry.ArtifactID != candidateArtifact.DiscoveryArtifactID || candidateEntry.SHA256 != admitted.Candidate.ArtifactSha256 || candidateEntry.SizeBytes != admitted.Candidate.ArtifactSizeBytes || candidateEntry.Historical || !candidateEntry.Promoted {
		t.Fatalf("planning candidate = %+v", candidateEntry)
	}
	if len(candidateEntry.Approvals) != 1 || candidateEntry.Approvals[0] != approved.Approval.ApprovalID {
		t.Fatalf("candidate approvals = %v, want [%s]", candidateEntry.Approvals, approved.Approval.ApprovalID)
	}

	// Reopen the closed discovery: the current basis advances to the
	// replacement revision, the prior revision and packet become inspectable
	// historical evidence, and the reopen event records the replacement
	// linkage.
	replacement := []byte("# reopened integrity discovery\n")
	reopenedRevision, reopenedWorkspace, err := service.ReopenFeatureDiscovery(ctx, ReopenFeatureDiscoveryInput{
		WorkspaceID: promoted.Workspace.WorkspaceID, ExpectedVersion: promoted.Workspace.Version, ExpectedPacketID: closed.Packet.ClosurePacketID,
		OperatorConfirmed: true, Cause: "integrity review", CreatedIdentity: "operator", Markdown: replacement,
		SHA256: discoveryTestDigest(replacement), Destination: DiscoveryDestinationRequirements,
	})
	if err != nil {
		t.Fatal(err)
	}
	projection, err = service.ReadGuidedProjection(ctx, reopenedWorkspace.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	integrity = projection.Diagnostics.Integrity
	// AC10/AC11/AC25 after reopen: the current revision advances and the
	// reopened workspace has no current closure packet until reclosure.
	if integrity.Discovery.CurrentRevisionID != reopenedRevision.DiscoveryRevisionID || integrity.Discovery.CurrentPacket != nil {
		t.Fatalf("reopened discovery integrity = %+v", integrity.Discovery)
	}
	// AC12/AC25: both revisions appear in history with explicit
	// current/historical markers, predecessor linkage, and the closed packet
	// binding on the historical entry.
	var current, historical *GuidedIntegrityDiscoveryRevision
	for index := range integrity.Discovery.History {
		entry := &integrity.Discovery.History[index]
		if entry.Historical {
			historical = entry
		} else {
			current = entry
		}
	}
	if current == nil || historical == nil || current.RevisionID != reopenedRevision.DiscoveryRevisionID || current.PredecessorID != revision.DiscoveryRevisionID || historical.RevisionID != revision.DiscoveryRevisionID || historical.ClosurePacketID != closed.Packet.ClosurePacketID || historical.PacketSHA256 != closed.Packet.ManifestSha256 {
		t.Fatalf("discovery history = %+v", integrity.Discovery.History)
	}
	// AC12: reopen/replacement linkage names the reopened packet and the
	// replacement revision.
	if len(integrity.Discovery.ReopenEvents) != 1 || integrity.Discovery.ReopenEvents[0].ReopenEventID == "" || integrity.Discovery.ReopenEvents[0].ReopenedPacketID != closed.Packet.ClosurePacketID || integrity.Discovery.ReopenEvents[0].ReplacementRevisionID != reopenedRevision.DiscoveryRevisionID {
		t.Fatalf("reopen events = %+v", integrity.Discovery.ReopenEvents)
	}
	// AC25: the promoted candidate is now historical evidence, inspectable but
	// no longer the current basis. The authority revision pointer is unchanged
	// by the discovery reopen, so the authority identity remains current.
	if len(integrity.Planning) != 1 || !integrity.Planning[0].Historical {
		t.Fatalf("planning candidates after reopen = %+v", integrity.Planning)
	}
	if len(integrity.Authority) != 1 || integrity.Authority[0].Historical || integrity.Authority[0].AuthorityRevisionID != revisionEntry.AuthorityRevisionID {
		t.Fatalf("authority after reopen = %+v", integrity.Authority)
	}
}

// TestGuidedIntegrityComposesDeliveryOwnerIdentities binds the packages and
// audits owners with full identity state and a real delivery selection, then
// asserts the typed delivery integrity surface: ticket, selection, package,
// package approval, Run basis, audit packet/decision, and remediation seeds.
func TestGuidedIntegrityComposesDeliveryOwnerIdentities(t *testing.T) {
	ctx, service, workspace := guidedDeliveryTicketFixture(t)
	fakePackage := &guidedFakePackageOwner{state: guidedapp.PackageState{
		State: "approved", PackageID: "package-integrity", PackageSHA256: strings.Repeat("c", 64), PackageApprovalID: "pkg-approval-integrity",
		RunID: "run-integrity", RunStatus: "validating", RunRepoTarget: "relay", RunBranch: "main", RunBaseCommit: strings.Repeat("a", 40),
	}}
	fakeAudit := &guidedFakeAuditOwner{runAudit: guidedapp.RunAuditState{
		RunID: "run-integrity", RunStatus: "validating", State: "decision_recorded", AuditPacketID: "packet-integrity", AuditDecisionID: "audit-integrity", AuditedCommit: strings.Repeat("b", 40),
	}, remediation: guidedapp.RemediationState{State: "none"}}
	service.SetGuidedPackageOwnerForTest(fakePackage)
	service.SetGuidedAuditOwnerForTest(fakeAudit)

	before, err := service.ReadGuidedProjection(ctx, workspace.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	// AC17: the Delivery Ticket frontier carries the public ticket identity and
	// revision number while the ticket is unselected.
	if len(before.Diagnostics.Integrity.Delivery.Frontier) != 1 || before.Diagnostics.Integrity.Delivery.Frontier[0].TicketID == "" || before.Diagnostics.Integrity.Delivery.Frontier[0].RevisionNumber < 1 {
		t.Fatalf("delivery frontier = %+v", before.Diagnostics.Integrity.Delivery.Frontier)
	}
	if _, err := service.ExecuteGuidedAction(ctx, GuidedActionInput{WorkspaceID: workspace.WorkspaceID, Action: string(GuidedActionSelectDeliveryTicket), ExpectedVersion: before.Workspace.Version, Confirmation: true}); err != nil {
		t.Fatal(err)
	}
	projection, err := service.ReadGuidedProjection(ctx, workspace.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	integrity := projection.Diagnostics.Integrity

	// AC18: the selection owner's public selection identity.
	if integrity.Delivery.Selection == nil || integrity.Delivery.Selection.SelectionID == "" {
		t.Fatalf("delivery selection = %+v", integrity.Delivery.Selection)
	}
	// AC19/AC20: package ID, package SHA, and the packages-owner approval ID.
	if integrity.Delivery.Package == nil || integrity.Delivery.Package.PackageID != "package-integrity" || integrity.Delivery.Package.SHA256 != fakePackage.state.PackageSHA256 || integrity.Delivery.Package.ApprovalID != "pkg-approval-integrity" {
		t.Fatalf("delivery package = %+v", integrity.Delivery.Package)
	}
	// AC21: Run identity with its package/current basis.
	if integrity.Delivery.Run == nil || integrity.Delivery.Run.RunID != "run-integrity" || integrity.Delivery.Run.PackageID != "package-integrity" || integrity.Delivery.Run.RepoTarget == "" || integrity.Delivery.Run.Branch == "" || integrity.Delivery.Run.BaseCommit == "" {
		t.Fatalf("delivery run = %+v", integrity.Delivery.Run)
	}
	// AC22: audit packet and decision identities plus the audited commit.
	if integrity.Delivery.Audit == nil || integrity.Delivery.Audit.AuditPacketID != "packet-integrity" || integrity.Delivery.Audit.AuditDecisionID != "audit-integrity" || integrity.Delivery.Audit.AuditedCommit == "" {
		t.Fatalf("delivery audit = %+v", integrity.Delivery.Audit)
	}
	// AC23: remediation seed identities surface once the audits owner reports
	// an open remediation seed.
	fakeAudit.remediation = guidedapp.RemediationState{State: "open", SeedIDs: []string{"remediation-integrity-1"}}
	projection, err = service.ReadGuidedProjection(ctx, workspace.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	integrity = projection.Diagnostics.Integrity
	if integrity.Delivery.Remediation == nil || len(integrity.Delivery.Remediation.SeedIDs) != 1 || integrity.Delivery.Remediation.SeedIDs[0] != "remediation-integrity-1" {
		t.Fatalf("delivery remediation = %+v", integrity.Delivery.Remediation)
	}
}

// TestGuidedIntegrityComposesPrototypeIdentities prepares and approves a
// prototype Run, seeds exact cleanup obligation semantic keys and one QA
// packet with admission and evidence identities, then asserts the typed
// prototype integrity surface including the discovery basis binding.
func TestGuidedIntegrityComposesPrototypeIdentities(t *testing.T) {
	ctx, store, service, workspace, _, proposal, authorization, run := preparedPrototype(t)
	service.SetGuidedAuditOwnerForTest(&guidedFakeAuditOwner{})
	approval, _, err := service.ApprovePrototypeExecution(ctx, approvalInput(workspace, proposal, authorization, run))
	if err != nil {
		t.Fatal(err)
	}
	evidenceSHA := strings.Repeat("d", 64)
	if err := store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		if _, e := tx.GetOrCreatePrototypeCleanupObligation(ctx, run.ID, "worktree", "exact cleanup key"); e != nil {
			return e
		}
		if _, e := tx.CompletePrototypeCleanupObligation(ctx, run.ID, "worktree"); e != nil {
			return e
		}
		artifact, e := tx.CreateDiscoveryArtifact(ctx, workflowstore.DiscoveryArtifact{DiscoveryArtifactID: "discovery-artifact-prototype-qa-integrity", WorkspaceRowID: workspace.ID, RelativePath: "feature-discovery/" + workspace.WorkspaceID + "/discovery-artifact-prototype-qa-integrity/evidence.json", SHA256: evidenceSHA, MediaType: "application/json", SizeBytes: 4})
		if e != nil {
			return e
		}
		packet, e := tx.CreatePrototypeQAPacket(ctx, workflowstore.PrototypeQAPacket{QAPacketID: "prototype-qa-packet-integrity", WorkspaceRowID: workspace.ID, RunRowID: run.ID, MutationIdentity: "integrity-qa", ExpectedRunVersion: run.Version, MemberCount: 1, TotalBytes: 4})
		if e != nil {
			return e
		}
		if _, e := tx.CreatePrototypeQAEvidence(ctx, workflowstore.PrototypeQAEvidence{QAPacketRowID: packet.ID, Sequence: 1, SemanticRole: "result-envelope", ArtifactRowID: artifact.ID, SHA256: evidenceSHA, MediaType: "application/json", SizeBytes: 4}); e != nil {
			return e
		}
		if _, e := tx.CreatePrototypeQAAdmission(ctx, workflowstore.PrototypeQAAdmission{QAPacketRowID: packet.ID, MutationIdentity: "integrity-qa", OperatorConfirmationEvidence: "confirmed", AdmittedMemberCount: 1, AdmittedTotalBytes: 4}); e != nil {
			return e
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	currentRevision, err := store.GetCurrentIntegratedDiscoveryRevision(ctx, workspace.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	projection, err := service.ReadGuidedProjection(ctx, workspace.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	integrity := projection.Diagnostics.Integrity
	// AC24: prototype Run, proposal, authorization, approval, and discovery
	// basis binding identities.
	if integrity.Prototype == nil {
		t.Fatalf("prototype integrity = %+v", integrity.Prototype)
	}
	if integrity.Prototype.RunID != run.PrototypeRunID || integrity.Prototype.RunState != "approved" || integrity.Prototype.ProposalID != proposal.ProposalID || integrity.Prototype.AuthorizationID != authorization.AuthorizationID || integrity.Prototype.ApprovalID != approval.ApprovalID || integrity.Prototype.DiscoveryRevisionID != currentRevision.DiscoveryRevisionID {
		t.Fatalf("prototype integrity = %+v", integrity.Prototype)
	}
	// AC24: cleanup exact semantic keys.
	if len(integrity.Prototype.Cleanup) != 1 || integrity.Prototype.Cleanup[0].CleanupObligationID == "" || integrity.Prototype.Cleanup[0].Kind != "worktree" || integrity.Prototype.Cleanup[0].Status != "complete" {
		t.Fatalf("prototype cleanup = %+v", integrity.Prototype.Cleanup)
	}
	// AC24: QA packet identity, admission identity, and evidence identities
	// with digests.
	if len(integrity.Prototype.QAPackets) != 1 {
		t.Fatalf("prototype QA packets = %+v", integrity.Prototype.QAPackets)
	}
	qaPacket := integrity.Prototype.QAPackets[0]
	if qaPacket.QAPacketID != "prototype-qa-packet-integrity" || qaPacket.AdmissionID == "" || len(qaPacket.Evidence) != 1 {
		t.Fatalf("prototype QA packet = %+v", qaPacket)
	}
	qaEvidence := qaPacket.Evidence[0]
	if qaEvidence.QaEvidenceID == "" || qaEvidence.SemanticRole != "result-envelope" || qaEvidence.SHA256 != evidenceSHA || qaEvidence.SizeBytes != 4 || qaEvidence.MediaType != "application/json" {
		t.Fatalf("prototype QA evidence = %+v", qaEvidence)
	}
}
