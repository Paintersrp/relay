package features

import (
	"context"
	"errors"
	"strings"
	"testing"

	workflowtickets "relay/internal/app/tickets"
	"relay/internal/guidedapp"
	workflowstore "relay/internal/store/workflow"
	"relay/internal/testfixtures"
)

// guidedDeliveryTicketFixture closes a direct-delivery workspace, records
// completion, and produces one approved current Delivery Ticket so the guided
// journey reaches the selection frontier.
func guidedDeliveryTicketFixture(t *testing.T) (context.Context, *Service, workflowstore.FeatureWorkspace) {
	t.Helper()
	ctx, store, service, workspace, _ := deliveryTicketCandidateFixture(t, DiscoveryDestinationDirectDeliveryTicket)
	ticketService, err := workflowtickets.NewService(store)
	if err != nil {
		t.Fatal(err)
	}
	candidateBytes := deliveryTicketCandidateBytes("P3-TX", workspace.FeatureSlug, "candidate-production", strings.Repeat("a", 40))
	candidate, err := service.AdmitPlanningCandidate(ctx, CandidateAdmissionInput{
		WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, Family: CandidateFamilyDeliveryTicket,
		Filename: "discovery-proof.ticket-P3-TX.r1.delivery-ticket.json", Bytes: candidateBytes,
		SHA256: digestForPlanningTest(candidateBytes), RepoTarget: "candidate-production", Branch: "main", BaseCommit: strings.Repeat("a", 40),
		Destination: DiscoveryDestinationDirectDeliveryTicket, CreatedIdentity: "planner",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.CompletePlanningCandidateReview(ctx, CompleteCandidateReviewInput{WorkspaceID: workspace.WorkspaceID, ReviewerIdentity: "auditor", Disposition: PlanningCandidateReviewReadyForApproval}); err != nil {
		t.Fatal(err)
	}
	candidateApproval, err := service.ApprovePlanningCandidate(ctx, CandidateApprovalInput{
		CandidateID: candidate.Candidate.CandidateID, ExpectedSHA256: candidate.Candidate.ArtifactSha256,
		ExpectedSizeBytes: candidate.Candidate.ArtifactSizeBytes, Bytes: candidateBytes, ExpectedVersion: workspace.Version,
		ExpectedClosurePacketRowID: workspace.CurrentDiscoveryClosurePacketRowID, ExpectedAuthorityRevisionRowID: workspace.CurrentAuthorityRevisionRowID,
		OperatorConfirmationEvidence: "guided delivery fixture", CreatedIdentity: "auditor",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ticketService.PromoteApprovedDeliveryTicketCandidate(ctx, workflowtickets.CandidateProductionInput{
		CandidateID: candidate.Candidate.CandidateID, ApprovalID: candidateApproval.Approval.ApprovalID,
		ExpectedVersion: workspace.Version, ExternalPriority: 7, CreatedIdentity: "planner",
	}); err != nil {
		t.Fatal(err)
	}
	return ctx, service, workspace
}

// guidedFakePackageOwner is the packages-owner contract fake used to prove the
// guided dispatch delegates prepare and approve actions with server-resolved
// inputs and never accepts package identities or digests from the boundary.
type guidedFakePackageOwner struct {
	state         guidedapp.PackageState
	approved      guidedapp.PackageState
	prepareResult guidedapp.PreparePackageResult
	approveCalls  []guidedapp.ApprovePackageInput
	prepareCalls  []guidedapp.PreparePackageInput
}

func (f *guidedFakePackageOwner) ReadWorkspacePackageState(context.Context, string) (guidedapp.PackageState, error) {
	return f.state, nil
}

func (f *guidedFakePackageOwner) ApproveCurrentPackage(_ context.Context, in guidedapp.ApprovePackageInput) error {
	f.approveCalls = append(f.approveCalls, in)
	f.state = f.approved
	return nil
}

func (f *guidedFakePackageOwner) PrepareCurrentSelection(_ context.Context, in guidedapp.PreparePackageInput) (guidedapp.PreparePackageResult, error) {
	f.prepareCalls = append(f.prepareCalls, in)
	f.state = guidedapp.PackageState{State: "prepared", PackageID: f.prepareResult.PackageID}
	return f.prepareResult, nil
}

// guidedFakeAuditOwner is the audits-owner contract fake used by guided
// dispatch tests; it reports neutral audit and remediation states unless the
// test overrides them.
type guidedFakeAuditOwner struct {
	runAudit    guidedapp.RunAuditState
	remediation guidedapp.RemediationState
}

func (f *guidedFakeAuditOwner) ReadRunAuditState(context.Context, string) (guidedapp.RunAuditState, error) {
	return f.runAudit, nil
}

func (f *guidedFakeAuditOwner) ReadWorkspaceRemediationState(context.Context, string) (guidedapp.RemediationState, error) {
	return f.remediation, nil
}

func TestGuidedSelectDispatchResolvesFrontierHeadServerSide(t *testing.T) {
	ctx, service, workspace := guidedDeliveryTicketFixture(t)
	// The refreshed projection after selection composes the package owner
	// state; bind a contract fake reporting no package yet.
	fake := &guidedFakePackageOwner{state: guidedapp.PackageState{State: "none"}}
	service.SetGuidedPackageOwnerForTest(fake)
	service.SetGuidedAuditOwnerForTest(&guidedFakeAuditOwner{})

	before, err := service.ReadGuidedProjection(ctx, workspace.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	if before.PrimaryAction.Action != GuidedActionSelectDeliveryTicket || !before.PrimaryAction.Enabled || !before.PrimaryAction.RequiresConfirmation || len(before.Delivery.Frontier) != 1 {
		t.Fatalf("frontier projection primary=%+v delivery=%+v", before.PrimaryAction, before.Delivery)
	}
	if _, err := service.ExecuteGuidedAction(ctx, GuidedActionInput{WorkspaceID: workspace.WorkspaceID, Action: string(GuidedActionSelectDeliveryTicket), ExpectedVersion: before.Workspace.Version}); !errors.Is(err, ErrFeatureCompletionConfirmation) {
		t.Fatalf("selection without confirmation error = %v", err)
	}
	after, err := service.ExecuteGuidedAction(ctx, GuidedActionInput{WorkspaceID: workspace.WorkspaceID, Action: string(GuidedActionSelectDeliveryTicket), ExpectedVersion: before.Workspace.Version, Confirmation: true})
	if err != nil {
		t.Fatal(err)
	}
	// After selection the selected Ticket needs its durable Ticket Design
	// Brief; package preparation must not be reachable before the brief is
	// authored, reviewed, and explicitly approved.
	if after.Projection.Delivery.SelectionState != "active" || after.Projection.Delivery.BriefState != "none" || after.Projection.PrimaryAction.Action != GuidedActionAuthorTicketDesignBrief {
		t.Fatalf("after selection delivery=%+v primary=%+v", after.Projection.Delivery, after.Projection.PrimaryAction)
	}
}

func TestGuidedDeliveryTicketCandidateLifecycleUsesReviewApprovalAndTicketProduction(t *testing.T) {
	ctx, _, service, workspace, _ := deliveryTicketCandidateFixture(t, DiscoveryDestinationDirectDeliveryTicket)
	service.SetGuidedAuditOwnerForTest(&guidedFakeAuditOwner{})
	bytes := deliveryTicketCandidateBytes("P3-GUIDED", workspace.FeatureSlug, "candidate-production", strings.Repeat("a", 40))
	admit := func() CandidateAdmissionResult {
		result, err := service.AdmitPlanningCandidate(ctx, CandidateAdmissionInput{
			WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, Family: CandidateFamilyDeliveryTicket,
			Filename: "discovery-proof.ticket-P3-GUIDED.r1.delivery-ticket.json", Bytes: bytes, SHA256: digestForPlanningTest(bytes),
			RepoTarget: "candidate-production", Branch: "main", BaseCommit: strings.Repeat("a", 40), Destination: DiscoveryDestinationDirectDeliveryTicket, CreatedIdentity: "planner",
		})
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	first := admit()
	admitted, err := service.ReadGuidedProjection(ctx, workspace.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	if admitted.PrimaryAction.Action != GuidedActionReviewPlanningCandidate || admitted.Planning.DeliveryTicket.State != "admitted" {
		t.Fatalf("admitted delivery candidate projection=%+v", admitted)
	}
	review, err := service.ExecuteGuidedAction(ctx, GuidedActionInput{WorkspaceID: workspace.WorkspaceID, Action: string(GuidedActionReviewPlanningCandidate), ExpectedVersion: admitted.Workspace.Version})
	if err != nil {
		t.Fatal(err)
	}
	if review.Handoff == nil || review.Handoff.Context["owner"] != "auditor_delivery_ticket_review" || review.Handoff.Context["operationId"] != auditorDeliveryTicketReviewOperation {
		t.Fatalf("delivery candidate review handoff=%+v", review.Handoff)
	}
	if _, err := service.CompletePlanningCandidateReview(ctx, CompleteCandidateReviewInput{WorkspaceID: workspace.WorkspaceID, ReviewerIdentity: "auditor", Disposition: PlanningCandidateReviewNeedsRevision}); err != nil {
		t.Fatal(err)
	}
	needsRevision, err := service.ReadGuidedProjection(ctx, workspace.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	if needsRevision.PrimaryAction.Action != GuidedActionAuthorDeliveryTicket || needsRevision.Planning.DeliveryTicket.State != "needs_revision" {
		t.Fatalf("needs-revision delivery candidate projection=%+v", needsRevision)
	}
	remediation, err := service.ExecuteGuidedAction(ctx, GuidedActionInput{WorkspaceID: workspace.WorkspaceID, Action: string(GuidedActionAuthorDeliveryTicket), ExpectedVersion: needsRevision.Workspace.Version})
	if err != nil {
		t.Fatal(err)
	}
	if remediation.Handoff == nil || remediation.Handoff.Context["operationId"] != "planner.delivery_ticket_remediation" || remediation.Handoff.Context["owner"] == "audit_remediation" {
		t.Fatalf("candidate remediation handoff=%+v", remediation.Handoff)
	}
	if _, err := service.ApprovePlanningCandidate(ctx, CandidateApprovalInput{CandidateID: first.Candidate.CandidateID, ExpectedSHA256: first.Candidate.ArtifactSha256, ExpectedSizeBytes: first.Candidate.ArtifactSizeBytes, Bytes: bytes, ExpectedVersion: workspace.Version, ExpectedClosurePacketRowID: workspace.CurrentDiscoveryClosurePacketRowID, ExpectedAuthorityRevisionRowID: workspace.CurrentAuthorityRevisionRowID, OperatorConfirmationEvidence: "must fail", CreatedIdentity: "auditor"}); !errors.Is(err, ErrCandidateReviewIncomplete) {
		t.Fatalf("needs-revision candidate approval error=%v", err)
	}

	admit()
	if _, err := service.CompletePlanningCandidateReview(ctx, CompleteCandidateReviewInput{WorkspaceID: workspace.WorkspaceID, ReviewerIdentity: "auditor", Disposition: PlanningCandidateReviewReadyForApproval}); err != nil {
		t.Fatal(err)
	}
	ready, err := service.ReadGuidedProjection(ctx, workspace.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	if ready.PrimaryAction.Action != GuidedActionApprovePlanningCandidate || !ready.PrimaryAction.RequiresConfirmation || ready.Planning.DeliveryTicket.State != "ready_for_approval" {
		t.Fatalf("ready delivery candidate projection=%+v", ready)
	}
	if _, err := service.ExecuteGuidedAction(ctx, GuidedActionInput{WorkspaceID: workspace.WorkspaceID, Action: string(GuidedActionApprovePlanningCandidate), ExpectedVersion: ready.Workspace.Version}); !errors.Is(err, ErrFeatureCompletionConfirmation) {
		t.Fatalf("unconfirmed candidate approval error=%v", err)
	}
	approved, err := service.ExecuteGuidedAction(ctx, GuidedActionInput{WorkspaceID: workspace.WorkspaceID, Action: string(GuidedActionApprovePlanningCandidate), ExpectedVersion: ready.Workspace.Version, Confirmation: true})
	if err != nil {
		t.Fatal(err)
	}
	if approved.Projection.PrimaryAction.Action != GuidedActionPromotePlanningCandidate || approved.Projection.Planning.DeliveryTicket.State != "approved" {
		t.Fatalf("approved delivery candidate projection=%+v", approved.Projection)
	}
	produced, err := service.ExecuteGuidedAction(ctx, GuidedActionInput{WorkspaceID: workspace.WorkspaceID, Action: string(GuidedActionPromotePlanningCandidate), ExpectedVersion: approved.Projection.Workspace.Version})
	if err != nil {
		t.Fatal(err)
	}
	if produced.Projection.PrimaryAction.Action != GuidedActionSelectDeliveryTicket || len(produced.Projection.Delivery.Frontier) != 1 {
		t.Fatalf("produced delivery candidate projection=%+v", produced.Projection)
	}
	if candidate, err := service.guidedCurrentPlanningCandidate(ctx, workspace.WorkspaceID, DiscoveryDestinationDirectDeliveryTicket, true); !errors.Is(err, ErrGuidedActionBlocked) || candidate.CandidateID != "" {
		t.Fatalf("produced candidate remained progression authority: %+v err=%v", candidate, err)
	}
}

// guidedApprovedBriefFixture drives the selected workspace through the durable
// Ticket Design Brief lifecycle (author admission, narrow review completion,
// and explicit confirmed owner approval) so the guided journey reaches package
// preparation.
func guidedApprovedBriefFixture(t *testing.T, service *Service, workspace workflowstore.FeatureWorkspace) {
	t.Helper()
	ctx := context.Background()
	ticketService, err := workflowtickets.NewService(service.store)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ticketService.AdmitTicketDesignBrief(ctx, workflowtickets.TicketDesignBriefAdmissionInput{
		WorkspaceID: workspace.WorkspaceID, Bytes: []byte(testfixtures.TicketDesignBrief), CreatedIdentity: "planner",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := ticketService.CompleteTicketDesignBriefReview(ctx, workflowtickets.CompleteBriefReviewInput{WorkspaceID: workspace.WorkspaceID, ReviewerIdentity: "auditor", Disposition: workflowtickets.TicketDesignBriefReviewReadyForApproval}); err != nil {
		t.Fatal(err)
	}
	workspace, err = service.store.GetFeatureWorkspaceByWorkspaceID(ctx, workspace.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ticketService.ApproveTicketDesignBrief(ctx, workflowtickets.TicketDesignBriefApprovalInput{
		WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version,
		OperatorConfirmationEvidence: "reviewed and approved", CreatedIdentity: "auditor",
	}); err != nil {
		t.Fatal(err)
	}
}

func TestGuidedTicketDesignBriefHandoffsTransferSelectedTicketAndExactOperations(t *testing.T) {
	ctx, service, workspace := guidedDeliveryTicketFixture(t)
	fake := &guidedFakePackageOwner{state: guidedapp.PackageState{State: "none"}}
	service.SetGuidedPackageOwnerForTest(fake)
	service.SetGuidedAuditOwnerForTest(&guidedFakeAuditOwner{})
	selected, err := service.ExecuteGuidedAction(ctx, GuidedActionInput{WorkspaceID: workspace.WorkspaceID, Action: string(GuidedActionSelectDeliveryTicket), ExpectedVersion: workspace.Version, Confirmation: true})
	if err != nil {
		t.Fatal(err)
	}
	if selected.Projection.PrimaryAction.Action != GuidedActionAuthorTicketDesignBrief {
		t.Fatalf("after selection primary=%+v", selected.Projection.PrimaryAction)
	}

	// Authoring hands off to the exact planner.ticket_design_brief operation
	// and transfers the selected Ticket identity; it performs no mutation.
	author, err := service.ExecuteGuidedAction(ctx, GuidedActionInput{WorkspaceID: workspace.WorkspaceID, Action: string(GuidedActionAuthorTicketDesignBrief), ExpectedVersion: selected.Projection.Workspace.Version})
	if err != nil {
		t.Fatal(err)
	}
	if author.Handoff == nil || author.Handoff.Context["owner"] != "ticket_design_brief_authoring" || author.Handoff.Context["operationId"] != plannerTicketDesignBriefOperation ||
		author.Handoff.Transfer == nil || author.Handoff.Transfer.Ticket == nil || author.Handoff.Transfer.Ticket.OperationID != plannerTicketDesignBriefOperation || author.Handoff.Transfer.Ticket.TicketID == "" {
		t.Fatalf("author handoff=%+v", author.Handoff)
	}

	// Admit the authored brief through the delivery owner, then the journey
	// advances to the read-only auditor review handoff.
	ticketService, err := workflowtickets.NewService(service.store)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ticketService.AdmitTicketDesignBrief(ctx, workflowtickets.TicketDesignBriefAdmissionInput{
		WorkspaceID: workspace.WorkspaceID, Bytes: []byte(testfixtures.TicketDesignBrief), CreatedIdentity: "planner",
	}); err != nil {
		t.Fatal(err)
	}
	reviewable, err := service.ReadGuidedProjection(ctx, workspace.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	if reviewable.Delivery.BriefState != "authored" || reviewable.PrimaryAction.Action != GuidedActionReviewTicketDesignBrief || reviewable.PrimaryAction.RequiresConfirmation {
		t.Fatalf("authored projection delivery=%+v primary=%+v", reviewable.Delivery, reviewable.PrimaryAction)
	}
	// The review action is purely read-only: it returns the auditor handoff
	// surface and must not change the brief state, the workspace version, or
	// record any completion fact or review outcome.
	review, err := service.ExecuteGuidedAction(ctx, GuidedActionInput{WorkspaceID: workspace.WorkspaceID, Action: string(GuidedActionReviewTicketDesignBrief), ExpectedVersion: reviewable.Workspace.Version})
	if err != nil {
		t.Fatal(err)
	}
	if review.Handoff == nil || review.Handoff.Context["owner"] != "auditor_ticket_design_brief_review" || review.Handoff.Context["operationId"] != auditorTicketDesignBriefReviewOperation ||
		review.Handoff.Transfer == nil || review.Handoff.Transfer.Ticket == nil || review.Handoff.Transfer.Ticket.OperationID != auditorTicketDesignBriefReviewOperation {
		t.Fatalf("review handoff=%+v", review.Handoff)
	}
	afterReview, err := service.ReadGuidedProjection(ctx, workspace.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	if afterReview.Delivery.BriefState != "authored" || afterReview.PrimaryAction.Action != GuidedActionReviewTicketDesignBrief || afterReview.Workspace.Version != reviewable.Workspace.Version {
		t.Fatalf("review mutated state: delivery=%+v primary=%+v version=%d", afterReview.Delivery, afterReview.PrimaryAction, afterReview.Workspace.Version)
	}

	// The external auditor records the narrow completion fact through the
	// bounded delivery-owner entry after performing the read-only review; no
	// outcome, verdict, or content is accepted. Only then does the explicit
	// guided approval emerge.
	if _, err := ticketService.CompleteTicketDesignBriefReview(ctx, workflowtickets.CompleteBriefReviewInput{WorkspaceID: workspace.WorkspaceID, ReviewerIdentity: "auditor", Disposition: workflowtickets.TicketDesignBriefReviewReadyForApproval}); err != nil {
		t.Fatal(err)
	}
	reviewed, err := service.ReadGuidedProjection(ctx, workspace.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	if reviewed.Delivery.BriefState != "reviewed" || reviewed.Delivery.BriefReviewDisposition != string(workflowtickets.TicketDesignBriefReviewReadyForApproval) || reviewed.PrimaryAction.Action != GuidedActionApproveTicketDesignBrief || !reviewed.PrimaryAction.RequiresConfirmation {
		t.Fatalf("reviewed projection delivery=%+v primary=%+v", reviewed.Delivery, reviewed.PrimaryAction)
	}

	// The explicit approval is a confirmed guided mutation resolved
	// server-side: without confirmation it is rejected before any mutation.
	if _, err := service.ExecuteGuidedAction(ctx, GuidedActionInput{WorkspaceID: workspace.WorkspaceID, Action: string(GuidedActionApproveTicketDesignBrief), ExpectedVersion: reviewed.Workspace.Version}); !errors.Is(err, ErrFeatureCompletionConfirmation) {
		t.Fatalf("approve without confirmation error = %v", err)
	}
	unchanged, err := service.ReadGuidedProjection(ctx, workspace.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	if unchanged.Delivery.BriefState != "reviewed" || unchanged.Delivery.BriefReviewDisposition != string(workflowtickets.TicketDesignBriefReviewReadyForApproval) || unchanged.PrimaryAction.Action != GuidedActionApproveTicketDesignBrief {
		t.Fatalf("blocked approval mutated state: delivery=%+v primary=%+v", unchanged.Delivery, unchanged.PrimaryAction)
	}
	approved, err := service.ExecuteGuidedAction(ctx, GuidedActionInput{WorkspaceID: workspace.WorkspaceID, Action: string(GuidedActionApproveTicketDesignBrief), ExpectedVersion: reviewed.Workspace.Version, Confirmation: true})
	if err != nil {
		t.Fatal(err)
	}
	if approved.Projection.Delivery.BriefState != "approved" || approved.Projection.PrimaryAction.Action != GuidedActionPreparePackage {
		t.Fatalf("approved projection delivery=%+v primary=%+v", approved.Projection.Delivery, approved.Projection.PrimaryAction)
	}
}

func TestGuidedNeedsRevisionBriefTransfersRemediationAndBlocksApproval(t *testing.T) {
	ctx, service, workspace := guidedDeliveryTicketFixture(t)
	service.SetGuidedPackageOwnerForTest(&guidedFakePackageOwner{state: guidedapp.PackageState{State: "none"}})
	service.SetGuidedAuditOwnerForTest(&guidedFakeAuditOwner{})
	selected, err := service.ExecuteGuidedAction(ctx, GuidedActionInput{WorkspaceID: workspace.WorkspaceID, Action: string(GuidedActionSelectDeliveryTicket), ExpectedVersion: workspace.Version, Confirmation: true})
	if err != nil {
		t.Fatal(err)
	}
	ticketService, err := workflowtickets.NewService(service.store)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ticketService.AdmitTicketDesignBrief(ctx, workflowtickets.TicketDesignBriefAdmissionInput{WorkspaceID: workspace.WorkspaceID, Bytes: []byte(testfixtures.TicketDesignBrief), CreatedIdentity: "planner"}); err != nil {
		t.Fatal(err)
	}
	if _, err := ticketService.CompleteTicketDesignBriefReview(ctx, workflowtickets.CompleteBriefReviewInput{WorkspaceID: workspace.WorkspaceID, ReviewerIdentity: "auditor", Disposition: workflowtickets.TicketDesignBriefReviewNeedsRevision}); err != nil {
		t.Fatal(err)
	}
	needsRevision, err := service.ReadGuidedProjection(ctx, workspace.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	if needsRevision.Delivery.BriefState != "reviewed" || needsRevision.Delivery.BriefReviewDisposition != string(workflowtickets.TicketDesignBriefReviewNeedsRevision) || needsRevision.PrimaryAction.Action != GuidedActionAuthorTicketDesignBrief || needsRevision.PrimaryAction.RequiresConfirmation {
		t.Fatalf("needs-revision projection delivery=%+v primary=%+v", needsRevision.Delivery, needsRevision.PrimaryAction)
	}
	remediation, err := service.ExecuteGuidedAction(ctx, GuidedActionInput{WorkspaceID: workspace.WorkspaceID, Action: string(GuidedActionAuthorTicketDesignBrief), ExpectedVersion: selected.Projection.Workspace.Version})
	if err != nil {
		t.Fatal(err)
	}
	if remediation.Handoff == nil || remediation.Handoff.Context["owner"] != "ticket_design_brief_remediation" || remediation.Handoff.Context["operationId"] != plannerTicketDesignBriefRemediationOperation || remediation.Handoff.Transfer == nil || remediation.Handoff.Transfer.Ticket == nil || remediation.Handoff.Transfer.Ticket.OperationID != plannerTicketDesignBriefRemediationOperation {
		t.Fatalf("remediation handoff=%+v", remediation.Handoff)
	}
	if _, err := service.ExecuteGuidedAction(ctx, GuidedActionInput{WorkspaceID: workspace.WorkspaceID, Action: string(GuidedActionApproveTicketDesignBrief), ExpectedVersion: needsRevision.Workspace.Version, Confirmation: true}); !errors.Is(err, ErrGuidedActionBlocked) {
		t.Fatalf("needs-revision approval error=%v, want ErrGuidedActionBlocked", err)
	}
	state, err := ticketService.ReadWorkspaceBriefState(ctx, workspace.WorkspaceID)
	if err != nil || state.State != "reviewed" || state.ReviewDisposition != string(workflowtickets.TicketDesignBriefReviewNeedsRevision) {
		t.Fatalf("blocked approval state=%+v err=%v", state, err)
	}
	if _, err := ticketService.AdmitTicketDesignBrief(ctx, workflowtickets.TicketDesignBriefAdmissionInput{WorkspaceID: workspace.WorkspaceID, Bytes: []byte(testfixtures.TicketDesignBrief), CreatedIdentity: "planner"}); err != nil {
		t.Fatalf("remediation replacement admission = %v", err)
	}
	replaced, err := service.ReadGuidedProjection(ctx, workspace.WorkspaceID)
	if err != nil || replaced.Delivery.BriefState != "authored" || replaced.PrimaryAction.Action != GuidedActionReviewTicketDesignBrief {
		t.Fatalf("replacement projection delivery=%+v primary=%+v err=%v", replaced.Delivery, replaced.PrimaryAction, err)
	}
}

func TestGuidedPrepareAndApprovePackageDispatchDelegateOwnerWithServerResolvedBasis(t *testing.T) {
	ctx, service, workspace := guidedDeliveryTicketFixture(t)
	fake := &guidedFakePackageOwner{state: guidedapp.PackageState{State: "none"}, prepareResult: guidedapp.PreparePackageResult{PackageID: "package-guided", State: "prepared"}}
	service.SetGuidedPackageOwnerForTest(fake)
	service.SetGuidedAuditOwnerForTest(&guidedFakeAuditOwner{})
	if _, err := service.ExecuteGuidedAction(ctx, GuidedActionInput{WorkspaceID: workspace.WorkspaceID, Action: string(GuidedActionSelectDeliveryTicket), ExpectedVersion: workspace.Version, Confirmation: true}); err != nil {
		t.Fatal(err)
	}
	guidedApprovedBriefFixture(t, service, workspace)
	before, err := service.ReadGuidedProjection(ctx, workspace.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	if before.Delivery.BriefState != "approved" || before.PrimaryAction.Action != GuidedActionPreparePackage || !before.PrimaryAction.Enabled || before.Delivery.PackageState != "none" {
		t.Fatalf("approved brief projection delivery=%+v primary=%+v", before.Delivery, before.PrimaryAction)
	}

	// The guided prepare action delegates actual package preparation to the
	// package owner with only the workspace identity; no selection ID, brief
	// ID, or digest crosses the boundary.
	prepared, err := service.ExecuteGuidedAction(ctx, GuidedActionInput{WorkspaceID: workspace.WorkspaceID, Action: string(GuidedActionPreparePackage), ExpectedVersion: before.Workspace.Version})
	if err != nil {
		t.Fatal(err)
	}
	if len(fake.prepareCalls) != 1 || fake.prepareCalls[0].WorkspaceID != workspace.WorkspaceID {
		t.Fatalf("guided prepare delegation = %+v", fake.prepareCalls)
	}
	if prepared.Projection.Delivery.PackageState != "prepared" || prepared.Projection.Delivery.PackageID != "package-guided" || prepared.Projection.PrimaryAction.Action != GuidedActionApprovePackage {
		t.Fatalf("after prepare delivery=%+v primary=%+v", prepared.Projection.Delivery, prepared.Projection.PrimaryAction)
	}
	if prepared.Projection.PrimaryAction.RequiresConfirmation != true {
		t.Fatalf("package approval must require confirmation: %+v", prepared.Projection.PrimaryAction)
	}

	// Without confirmation the confirmed package approval is rejected; with
	// confirmation it delegates the server-resolved approve to the owner.
	if _, err := service.ExecuteGuidedAction(ctx, GuidedActionInput{WorkspaceID: workspace.WorkspaceID, Action: string(GuidedActionApprovePackage), ExpectedVersion: prepared.Projection.Workspace.Version}); !errors.Is(err, ErrFeatureCompletionConfirmation) {
		t.Fatalf("package approval without confirmation error = %v", err)
	}
	fake.approved = guidedapp.PackageState{State: "approved", PackageID: "package-guided", PackageSHA256: strings.Repeat("a", 64),
		RunID: "run-guided", RunStatus: "setup_ready", RunRepoTarget: "candidate-production", RunBranch: "main", RunBaseCommit: strings.Repeat("b", 40)}
	// The refreshed projection composes the run audit read, so the run the
	// package owner reports must exist in the store.
	insertGuidedRun(t, ctx, service, fake.approved)
	after, err := service.ExecuteGuidedAction(ctx, GuidedActionInput{WorkspaceID: workspace.WorkspaceID, Action: string(GuidedActionApprovePackage), ExpectedVersion: prepared.Projection.Workspace.Version, Confirmation: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(fake.approveCalls) != 1 || fake.approveCalls[0].WorkspaceID != workspace.WorkspaceID || fake.approveCalls[0].Evidence != guidedApprovalEvidence {
		t.Fatalf("guided approve delegation = %+v", fake.approveCalls)
	}
	if after.Projection.Delivery.PackageState != "approved" || after.Projection.Delivery.RunID != "run-guided" || after.Projection.PrimaryAction.Action != GuidedActionLaunchRun {
		t.Fatalf("after approval delivery=%+v primary=%+v", after.Projection.Delivery, after.Projection.PrimaryAction)
	}

	launch, err := service.ExecuteGuidedAction(ctx, GuidedActionInput{WorkspaceID: workspace.WorkspaceID, Action: string(GuidedActionLaunchRun), ExpectedVersion: after.Projection.Workspace.Version})
	if err != nil {
		t.Fatal(err)
	}
	if launch.Handoff == nil || launch.Handoff.Transfer == nil || launch.Handoff.Transfer.Run == nil || launch.Handoff.Transfer.Run.RunID != "run-guided" || launch.Handoff.Transfer.Run.Status != "setup_ready" || launch.Handoff.Transfer.Run.PackageID != "package-guided" {
		t.Fatalf("launch handoff=%+v", launch.Handoff)
	}
}

// insertGuidedRun inserts a package Run matching the state the package owner
// contract fake reports, so the refreshed projection can compose the run audit
// read.
func insertGuidedRun(t *testing.T, ctx context.Context, service *Service, state guidedapp.PackageState) {
	t.Helper()
	if _, err := service.store.DB().ExecContext(ctx, `
INSERT INTO runs (run_id, feature_slug, repo_target, status, branch, base_commit)
VALUES (?, 'discovery-proof', ?, 'created', ?, ?)`, state.RunID, state.RunRepoTarget, state.RunBranch, state.RunBaseCommit); err != nil {
		t.Fatal(err)
	}
	if err := service.store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		_, err := tx.TransitionRun(ctx, state.RunID, "created", "setup_ready")
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

func TestGuidedReopenDiscoveryExecutesOwnerMutationAndRefreshes(t *testing.T) {
	ctx, _, service, workspace, _ := deliveryTicketCandidateFixture(t, DiscoveryDestinationNoDeliveryWork)
	fakeAudit := &guidedFakeAuditOwner{}
	service.SetGuidedAuditOwnerForTest(fakeAudit)

	projection, err := service.ReadGuidedProjection(ctx, workspace.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	if projection.PrimaryAction.Action != GuidedActionReopenDiscovery || !projection.PrimaryAction.Enabled || !projection.PrimaryAction.RequiresConfirmation || projection.Discovery.ReopenState != "none" {
		t.Fatalf("completion recorded projection primary=%+v discovery=%+v", projection.PrimaryAction, projection.Discovery)
	}

	// Reopen is a confirmed primary mutation: without confirmation it is
	// rejected before any owner mutation.
	if _, err := service.ExecuteGuidedAction(ctx, GuidedActionInput{WorkspaceID: workspace.WorkspaceID, Action: string(GuidedActionReopenDiscovery), ExpectedVersion: projection.Workspace.Version}); !errors.Is(err, ErrFeatureCompletionConfirmation) {
		t.Fatalf("reopen without confirmation error = %v", err)
	}

	// With confirmation but without the operator-authored replacement content
	// the reopen owner rejects the consequence; no mutation occurs.
	if _, err := service.ExecuteGuidedAction(ctx, GuidedActionInput{WorkspaceID: workspace.WorkspaceID, Action: string(GuidedActionReopenDiscovery), ExpectedVersion: projection.Workspace.Version, Confirmation: true}); !errors.Is(err, ErrInvalidDiscoveryConsequence) {
		t.Fatalf("reopen without replacement error = %v", err)
	}

	replacement := []byte("# guided reopened discovery\n")
	result, err := service.ExecuteGuidedAction(ctx, GuidedActionInput{
		WorkspaceID: workspace.WorkspaceID, Action: string(GuidedActionReopenDiscovery),
		ExpectedVersion: projection.Workspace.Version, Confirmation: true,
		Cause: "guided reopen", Markdown: replacement,
		Destination: DiscoveryDestinationNoDeliveryWork,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Handoff != nil {
		t.Fatalf("reopen emitted a handoff instead of a refreshed mutation projection: %+v", result.Handoff)
	}
	if result.Projection.Workspace.Version <= projection.Workspace.Version {
		t.Fatalf("reopen did not advance the workspace version: %d -> %d", projection.Workspace.Version, result.Projection.Workspace.Version)
	}
	if result.Projection.Discovery.ReopenState != "reopened" || result.Projection.Discovery.State != "active" {
		t.Fatalf("reopened projection discovery=%+v", result.Projection.Discovery)
	}
	// The reopened workspace has no current closure packet until it is closed
	// again, so the refreshed projection reports the source-backed recovery
	// requirement rather than a fabricated continuation.
	if result.Projection.Recovery.State != "required" || result.Projection.Recovery.Category != "close_current_discovery" {
		t.Fatalf("reopened projection recovery=%+v", result.Projection.Recovery)
	}
	// Normal-entry proof: the guided request carried no digest, so the server
	// derived the replacement SHA-256 from the submitted markdown and the
	// reopen owner stored exactly that revision.
	frontier, err := service.ReadIntegratedDiscoveryFrontier(ctx, workspace.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	if frontier.Current.Artifact.SHA256 != discoveryTestDigest(replacement) || string(frontier.Current.Markdown) != string(replacement) {
		t.Fatalf("guided reopen stored a replacement with an unexpected server-derived digest: sha=%q markdown=%q", frontier.Current.Artifact.SHA256, frontier.Current.Markdown)
	}
}
