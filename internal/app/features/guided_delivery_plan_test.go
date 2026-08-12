package features

import (
	"context"
	"errors"
	"strings"
	"testing"

	workflowtickets "relay/internal/app/tickets"
	"relay/internal/guidedapp"
	workflowstore "relay/internal/store/workflow"
)

// guidedDeliveryPlanFixture drives a closed SharedDesign workspace through the
// published Requirements authority and an approved, promoted Shared Design
// candidate, and binds the exact ticket/package/audit owners, so the guided
// journey reaches the Delivery Plan family with the enriched Ticket Frontier v2
// projection enabled.
func guidedDeliveryPlanFixture(t *testing.T) (context.Context, *Service, *workflowstore.Store, workflowstore.FeatureWorkspace) {
	t.Helper()
	ctx, store, service, workspace, _ := deliveryTicketCandidateFixture(t, DiscoveryDestinationSharedDesign)
	ticketOwner, err := workflowtickets.NewService(store)
	if err != nil {
		t.Fatal(err)
	}
	service.SetGuidedTicketOwnerForTest(ticketOwner)
	service.SetGuidedPackageOwnerForTest(&guidedFakePackageOwner{state: guidedapp.PackageState{State: "none"}})
	service.SetGuidedAuditOwnerForTest(&guidedFakeAuditOwner{})

	designBytes := []byte("# shared design candidate\n")
	design, err := service.AdmitPlanningCandidate(ctx, CandidateAdmissionInput{
		WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, Family: CandidateFamilySharedDesign,
		Filename: workspace.FeatureSlug + ".design.md", Bytes: designBytes, SHA256: digestForPlanningTest(designBytes),
		RepoTarget: "candidate-production", Branch: "main", BaseCommit: strings.Repeat("a", 40),
		Destination: DiscoveryDestinationSharedDesign, CreatedIdentity: "planner",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.CompletePlanningCandidateReview(ctx, CompleteCandidateReviewInput{WorkspaceID: workspace.WorkspaceID, CandidateID: design.Candidate.CandidateID, ReviewerIdentity: "auditor", Disposition: PlanningCandidateReviewReadyForApproval, ReviewedBytes: designBytes}); err != nil {
		t.Fatal(err)
	}
	designApproval, err := service.ApproveCurrentPlanningCandidate(ctx, CandidateApprovalInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, OperatorConfirmationEvidence: "guided delivery plan fixture", CreatedIdentity: "auditor"})
	if err != nil {
		t.Fatal(err)
	}
	promoted, err := service.PromoteApprovedPlanningCandidate(ctx, CandidatePromotionInput{CandidateID: design.Candidate.CandidateID, ApprovalID: designApproval.Approval.ApprovalID, ExpectedVersion: workspace.Version, CreatedIdentity: "operator"})
	if err != nil {
		t.Fatal(err)
	}
	return ctx, service, store, promoted.Workspace
}

// TestGuidedDeliveryPlanFamilySequencesAuthorReviewApprovePromote drives the
// Delivery Plan planning family through the exact guided flow after an approved
// Shared Design: author_delivery_plan, the shared read-only review handoff, the
// confirmed explicit approval, and server-side promotion into the workspace's
// current approved Delivery Plan.
func TestGuidedDeliveryPlanFamilySequencesAuthorReviewApprovePromote(t *testing.T) {
	ctx, service, _, workspace := guidedDeliveryPlanFixture(t)

	before, err := service.ReadGuidedProjection(ctx, workspace.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	if before.PrimaryAction.Action != GuidedActionAuthorDeliveryPlan || !before.PrimaryAction.Enabled || before.Planning.DeliveryPlan.State != "none" {
		t.Fatalf("delivery plan author projection primary=%+v planning=%+v", before.PrimaryAction, before.Planning.DeliveryPlan)
	}

	// The author handoff composes the existing planner authoring and review
	// envelope with the exact published planner.delivery_plan operation
	// identity resolved against the route registry.
	author, err := service.ExecuteGuidedAction(ctx, GuidedActionInput{WorkspaceID: workspace.WorkspaceID, Action: string(GuidedActionAuthorDeliveryPlan), ExpectedVersion: before.Workspace.Version})
	if err != nil {
		t.Fatal(err)
	}
	if author.Handoff == nil || author.Handoff.Context["owner"] != "planner_authoring_and_review" || author.Handoff.Context["candidateState"] != "promoted" || author.Handoff.Context["operationId"] != plannerDeliveryPlanOperation {
		t.Fatalf("delivery plan author handoff=%+v", author.Handoff)
	}

	bytes := deliveryPlanCandidateBytes(workspace.FeatureSlug)
	candidate, err := service.AdmitPlanningCandidate(ctx, CandidateAdmissionInput{
		WorkspaceID: workspace.WorkspaceID, ExpectedVersion: author.Projection.Workspace.Version, Family: CandidateFamilyDeliveryPlan,
		Filename: workspace.FeatureSlug + ".delivery-plan.json", Bytes: bytes, SHA256: digestForPlanningTest(bytes),
		RepoTarget: "candidate-production", Branch: "main", BaseCommit: strings.Repeat("a", 40),
		Destination: DiscoveryDestinationSharedDesign, CreatedIdentity: "planner",
	})
	if err != nil {
		t.Fatal(err)
	}

	admitted, err := service.ReadGuidedProjection(ctx, workspace.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	if admitted.PrimaryAction.Action != GuidedActionReviewPlanningCandidate || admitted.Planning.DeliveryPlan.State != "admitted" {
		t.Fatalf("admitted delivery plan projection primary=%+v planning=%+v", admitted.PrimaryAction, admitted.Planning.DeliveryPlan)
	}
	// The review handoff is the exact read-only auditor.delivery_plan_review
	// operation resolved against the published route registry through the
	// auditor review owner envelope.
	review, err := service.ExecuteGuidedAction(ctx, GuidedActionInput{WorkspaceID: workspace.WorkspaceID, Action: string(GuidedActionReviewPlanningCandidate), ExpectedVersion: admitted.Workspace.Version})
	if err != nil {
		t.Fatal(err)
	}
	if review.Handoff == nil || review.Handoff.Context["owner"] != "auditor_review" || review.Handoff.Context["operationId"] != auditorDeliveryPlanReviewOperation || review.Handoff.Transfer == nil {
		t.Fatalf("delivery plan review handoff=%+v", review.Handoff)
	}
	if _, err := service.CompletePlanningCandidateReview(ctx, CompleteCandidateReviewInput{WorkspaceID: workspace.WorkspaceID, CandidateID: candidate.Candidate.CandidateID, ReviewerIdentity: "auditor", Disposition: PlanningCandidateReviewReadyForApproval, ReviewedBytes: bytes}); err != nil {
		t.Fatal(err)
	}

	reviewed, err := service.ReadGuidedProjection(ctx, workspace.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	if reviewed.PrimaryAction.Action != GuidedActionApprovePlanningCandidate || reviewed.Planning.DeliveryPlan.State != "reviewed" {
		t.Fatalf("reviewed delivery plan projection primary=%+v planning=%+v", reviewed.PrimaryAction, reviewed.Planning.DeliveryPlan)
	}
	if _, err := service.ExecuteGuidedAction(ctx, GuidedActionInput{WorkspaceID: workspace.WorkspaceID, Action: string(GuidedActionApprovePlanningCandidate), ExpectedVersion: reviewed.Workspace.Version}); !errors.Is(err, ErrFeatureCompletionConfirmation) {
		t.Fatalf("delivery plan approval without confirmation error=%v", err)
	}
	approved, err := service.ExecuteGuidedAction(ctx, GuidedActionInput{WorkspaceID: workspace.WorkspaceID, Action: string(GuidedActionApprovePlanningCandidate), ExpectedVersion: reviewed.Workspace.Version, Confirmation: true})
	if err != nil {
		t.Fatal(err)
	}
	if approved.Projection.PrimaryAction.Action != GuidedActionPromotePlanningCandidate || approved.Projection.Planning.DeliveryPlan.State != "approved" {
		t.Fatalf("approved delivery plan projection primary=%+v planning=%+v", approved.Projection.PrimaryAction, approved.Projection.Planning.DeliveryPlan)
	}

	promoted, err := service.ExecuteGuidedAction(ctx, GuidedActionInput{WorkspaceID: workspace.WorkspaceID, Action: string(GuidedActionPromotePlanningCandidate), ExpectedVersion: approved.Projection.Workspace.Version})
	if err != nil {
		t.Fatal(err)
	}
	if promoted.Projection.Planning.DeliveryPlan.State != "promoted" || promoted.Projection.PrimaryAction.Action != GuidedActionAuthorDeliveryTicket {
		t.Fatalf("promoted delivery plan projection primary=%+v planning=%+v", promoted.Projection.PrimaryAction, promoted.Projection.Planning.DeliveryPlan)
	}
	// The promoted Plan is the workspace's current approved Plan and the
	// enriched Ticket Frontier v2 projects its planned units in Plan source
	// order with the exact v2 planned state.
	if promoted.Projection.Delivery.PlanSHA256 != digestForPlanningTest(bytes) {
		t.Fatalf("projected plan sha256 = %q", promoted.Projection.Delivery.PlanSHA256)
	}
	if len(promoted.Projection.Delivery.FrontierV2) != 2 {
		t.Fatalf("projected frontier v2 = %+v", promoted.Projection.Delivery.FrontierV2)
	}
	for index, want := range []string{"P3-T1", "P3-T2"} {
		entry := promoted.Projection.Delivery.FrontierV2[index]
		if entry.UnitID == nil || *entry.UnitID != want || entry.State != "planned" || entry.TicketID != nil {
			t.Fatalf("frontier v2 entry %d = %+v", index, entry)
		}
	}
}
