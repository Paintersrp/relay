package features

import (
	"errors"
	"strings"
	"testing"

	workflowtickets "relay/internal/app/tickets"
	"relay/internal/operations/registry"
	workflowstore "relay/internal/store/workflow"
)

func TestComposeGuidedFeatureProjectionCoversAllDestinationsAndOnePrimary(t *testing.T) {
	cases := []struct {
		destination DiscoveryDestination
		want        GuidedFeatureAction
	}{
		{DiscoveryDestinationNoDeliveryWork, GuidedActionCompleteFeature},
		{DiscoveryDestinationDirectDeliveryTicket, GuidedActionAuthorDeliveryTicket},
		{DiscoveryDestinationRequirements, GuidedActionAuthorRequirements},
		{DiscoveryDestinationSharedDesign, GuidedActionAuthorSharedDesign},
		{DiscoveryDestinationRequirementsThenSharedDesign, GuidedActionAuthorRequirements},
		{DiscoveryDestinationExistingRouteContinuation, GuidedActionContinueEstablishedRoute},
	}
	for _, tc := range cases {
		t.Run(string(tc.destination), func(t *testing.T) {
			assessment := DiscoveryAssessment{State: DiscoveryStateClosed, Destination: tc.destination, Currentness: DiscoveryCurrent, Revision: &workflowstore.IntegratedDiscoveryRevision{DiscoveryRevisionID: "revision"}}
			projection := composeGuidedFeatureProjection(workflowstore.FeatureWorkspace{WorkspaceID: "workspace", FeatureSlug: "payments", Version: 4}, workflowstore.Project{ProjectID: "project", Name: "Relay"}, assessment, FeatureCurrentnessDecision{Readiness: FeatureCurrent}, nil, CompletionStatus{Gates: []CompletionGate{{Name: "closure", Ready: true}, {Name: "authority", Ready: true}}}, GuidedPlanningSection{}, GuidedDeliverySection{}, GuidedPrototypeSection{})
			if projection.PrimaryAction.Action != tc.want || !projection.PrimaryAction.Primary || len(projection.AvailableActions) != 1 {
				t.Fatalf("primary=%+v", projection.PrimaryAction)
			}
		})
	}
}

func TestGuidedHandoffIsDistinctAndCarriesOwnerPreparationContext(t *testing.T) {
	ctx, _, service, workspace, revision := adoptedDiscoveryLifecycle(t, DiscoveryDestinationDirectDeliveryTicket)
	_, workspace, err := service.CloseFeatureDiscovery(ctx, CloseFeatureDiscoveryInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, ExpectedRevisionID: revision.DiscoveryRevisionID, Destination: DiscoveryDestinationDirectDeliveryTicket, CreatedIdentity: "operator"})
	if err != nil {
		t.Fatal(err)
	}
	handoff, err := service.guidedHandoff(ctx, workspace.WorkspaceID, GuidedActionAuthorDeliveryTicket, GuidedFeatureProjection{Workspace: GuidedWorkspaceSection{WorkspaceID: workspace.WorkspaceID}, Discovery: GuidedDiscoverySection{Destination: string(DiscoveryDestinationDirectDeliveryTicket), Continuation: "resume package"}, Currentness: GuidedCurrentnessSection{Readiness: string(FeatureCurrent)}, Delivery: GuidedDeliverySection{SelectionState: "none"}})
	if err != nil {
		t.Fatal(err)
	}
	if handoff.ResumeRoute == "" || handoff.Summary == "" || handoff.Context["owner"] != "delivery_ticket_authoring" || handoff.Context["operationId"] != plannerDeliveryTicketOperation {
		t.Fatalf("handoff=%+v", handoff)
	}
	if handoff.Transfer == nil || handoff.Transfer.Ticket == nil || handoff.Transfer.Ticket.OperationID != plannerDeliveryTicketOperation {
		t.Fatalf("delivery handoff does not transfer the owner operation: %+v", handoff.Transfer)
	}
	if strings.Contains(handoff.Summary, plannerTicketDesignBriefOperation) || strings.Contains(handoff.Summary, "ticket design brief") {
		t.Fatalf("delivery handoff substituted ticket-design-brief operation: %q", handoff.Summary)
	}
}

func TestGuidedProjectionSeparatesRecoveryAndDiagnostics(t *testing.T) {
	projection := composeGuidedFeatureProjection(workflowstore.FeatureWorkspace{WorkspaceID: "workspace", Version: 9}, workflowstore.Project{}, DiscoveryAssessment{State: DiscoveryStateClosed, Currentness: DiscoveryCurrent, Revision: &workflowstore.IntegratedDiscoveryRevision{DiscoveryRevisionID: "revision"}}, FeatureCurrentnessDecision{Readiness: FeatureStale, StaleOwner: "authority", RecoveryCategory: "publish_current_authority", BlockedOperation: "promotion", Effect: "no mutation", Basis: "authority mismatch", HistoricalIdentity: "historical"}, nil, CompletionStatus{}, GuidedPlanningSection{}, GuidedDeliverySection{}, GuidedPrototypeSection{})
	if projection.Recovery.State != "required" || projection.Recovery.Category != "publish_current_authority" || len(projection.Diagnostics.Stale) != 3 || len(projection.Diagnostics.Historical) != 1 {
		t.Fatalf("recovery=%+v diagnostics=%+v", projection.Recovery, projection.Diagnostics)
	}
}

func TestGuidedPlanningTracksImmediateApprovalPromotionAndHistoricalBasis(t *testing.T) {
	ctx, store, service, workspace, revision := adoptedDiscoveryLifecycle(t, DiscoveryDestinationRequirements)
	closed, workspace, err := service.CloseFeatureDiscovery(ctx, CloseFeatureDiscoveryInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, ExpectedRevisionID: revision.DiscoveryRevisionID, Destination: DiscoveryDestinationRequirements, CreatedIdentity: "operator"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().ExecContext(ctx, `INSERT INTO repository_targets (repo_target, local_path, configured_branch_ref, configuration_version) VALUES ('guided-planning', 'C:/guided-planning', 'refs/heads/main', 1)`); err != nil {
		t.Fatal(err)
	}
	insertReadyPlanningSourceClosure(t, ctx, store, workspace, "guided-planning", strings.Repeat("a", 40))
	bytes := []byte("# guided requirements\n")
	candidate, err := service.AdmitPlanningCandidate(ctx, CandidateAdmissionInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, Family: CandidateFamilyRequirements, Filename: workspace.FeatureSlug + ".requirements.md", Bytes: bytes, SHA256: discoveryTestDigest(bytes), RepoTarget: "guided-planning", Branch: "main", BaseCommit: strings.Repeat("a", 40), Destination: DiscoveryDestinationRequirements, CreatedIdentity: "planner"})
	if err != nil {
		t.Fatal(err)
	}
	planning, err := service.guidedPlanning(ctx, workspace, FeatureCurrentnessDecision{Readiness: FeatureCurrent}, nil)
	if err != nil || planning.AwaitingReview != 1 || planning.AwaitingPromotion != 0 || planning.Requirements.State != "admitted" {
		t.Fatalf("admitted planning=%+v err=%v", planning, err)
	}
	if _, err := service.CompletePlanningCandidateReview(ctx, CompleteCandidateReviewInput{WorkspaceID: workspace.WorkspaceID, ReviewerIdentity: "auditor", Disposition: PlanningCandidateReviewReadyForApproval}); err != nil {
		t.Fatal(err)
	}
	// A ready review arms the distinct explicit approval: the family is
	// reviewed and awaiting approval, not approved.
	planning, err = service.guidedPlanning(ctx, workspace, FeatureCurrentnessDecision{Readiness: FeatureCurrent}, nil)
	if err != nil || planning.AwaitingReview != 0 || planning.AwaitingApproval != 1 || planning.AwaitingPromotion != 0 || planning.Requirements.State != "reviewed" {
		t.Fatalf("reviewed planning=%+v err=%v", planning, err)
	}
	approval := approveCurrentPlanningCandidate(t, ctx, service, workspace, "guided exact approval")
	planning, err = service.guidedPlanning(ctx, workspace, FeatureCurrentnessDecision{Readiness: FeatureCurrent}, nil)
	if err != nil || planning.AwaitingReview != 0 || planning.AwaitingPromotion != 1 || planning.Requirements.State != "approved" {
		t.Fatalf("approved planning=%+v err=%v", planning, err)
	}
	promoted, err := service.PromoteApprovedPlanningCandidate(ctx, CandidatePromotionInput{CandidateID: candidate.Candidate.CandidateID, ApprovalID: approval.Approval.ApprovalID, ExpectedVersion: workspace.Version, CreatedIdentity: "planner"})
	if err != nil {
		t.Fatal(err)
	}
	authority, err := service.ReadAuthority(ctx, workspace.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	planning, err = service.guidedPlanning(ctx, promoted.Workspace, FeatureCurrentnessDecision{Readiness: FeatureCurrent}, authority)
	if err != nil || planning.Promoted != 1 || planning.Requirements.State != "promoted" {
		t.Fatalf("promoted planning=%+v err=%v", planning, err)
	}
	replacement := []byte("# reopened guided requirements\n")
	_, reopened, err := service.ReopenFeatureDiscovery(ctx, ReopenFeatureDiscoveryInput{WorkspaceID: promoted.Workspace.WorkspaceID, ExpectedVersion: promoted.Workspace.Version, ExpectedPacketID: closed.Packet.ClosurePacketID, OperatorConfirmed: true, Cause: "historical guided planning", CreatedIdentity: "operator", Markdown: replacement, SHA256: discoveryTestDigest(replacement), Destination: DiscoveryDestinationRequirements})
	if err != nil {
		t.Fatal(err)
	}
	if !service.planningCandidateHistorical(ctx, reopened, candidate.Candidate) {
		t.Fatal("reopened workspace treated prior candidate as current")
	}
}

func TestGuidedPlannerHandoffUsesOwnerCompositionWithoutInternalContext(t *testing.T) {
	ctx, _, service, workspace, revision := adoptedDiscoveryLifecycle(t, DiscoveryDestinationRequirements)
	closed, workspace, err := service.CloseFeatureDiscovery(ctx, CloseFeatureDiscoveryInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, ExpectedRevisionID: revision.DiscoveryRevisionID, Destination: DiscoveryDestinationRequirements, CreatedIdentity: "operator"})
	if err != nil {
		t.Fatal(err)
	}
	handoff, err := service.guidedHandoff(ctx, workspace.WorkspaceID, GuidedActionAuthorRequirements, GuidedFeatureProjection{Discovery: GuidedDiscoverySection{Destination: string(DiscoveryDestinationRequirements)}, Currentness: GuidedCurrentnessSection{Readiness: string(FeatureCurrent)}})
	if err != nil {
		t.Fatal(err)
	}
	if handoff.Context["owner"] != "planner_authoring_and_review" || handoff.Context["candidateState"] == "" || handoff.Transfer == nil || len(handoff.Transfer.Members) == 0 || handoff.Transfer.AuthorityLayers == nil {
		t.Fatalf("planner handoff=%+v", handoff)
	}
	for key, value := range handoff.Context {
		if strings.Contains(key+value, closed.Packet.ClosurePacketID) || strings.Contains(key+value, workspace.WorkspaceID) {
			t.Fatalf("handoff exposed internal identity: %q=%q", key, value)
		}
	}
}

func TestGuidedPlanningReviewCompletionResumesAtExplicitApprovalThenPromotion(t *testing.T) {
	ctx, store, service, workspace, revision := adoptedDiscoveryLifecycle(t, DiscoveryDestinationRequirements)
	_, workspace, err := service.CloseFeatureDiscovery(ctx, CloseFeatureDiscoveryInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, ExpectedRevisionID: revision.DiscoveryRevisionID, Destination: DiscoveryDestinationRequirements, CreatedIdentity: "operator"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().ExecContext(ctx, `INSERT INTO repository_targets (repo_target, local_path, configured_branch_ref, configuration_version) VALUES ('guided-projection-repo', 'C:/guided-projection-repo', 'refs/heads/main', 1)`); err != nil {
		t.Fatal(err)
	}
	insertReadyPlanningSourceClosure(t, ctx, store, workspace, "guided-projection-repo", strings.Repeat("a", 40))
	bytes := []byte("# guided requirements\n")
	admitted, err := service.AdmitPlanningCandidate(ctx, CandidateAdmissionInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, Family: CandidateFamilyRequirements, Filename: workspace.FeatureSlug + ".requirements.md", Bytes: bytes, SHA256: discoveryTestDigest(bytes), RepoTarget: "guided-projection-repo", Branch: "main", BaseCommit: strings.Repeat("a", 40), Destination: DiscoveryDestinationRequirements, CreatedIdentity: "planner"})
	if err != nil || admitted.AuthorizedNextAction != "review_candidate" {
		t.Fatalf("admission=%+v err=%v", admitted, err)
	}
	service.SetGuidedAuditOwnerForTest(&guidedFakeAuditOwner{})
	ticketOwner, err := workflowtickets.NewService(store)
	if err != nil {
		t.Fatal(err)
	}
	service.SetGuidedTicketOwnerForTest(ticketOwner)
	before, err := service.ReadGuidedProjection(ctx, workspace.WorkspaceID)
	if err != nil || before.PrimaryAction.Action != GuidedActionReviewPlanningCandidate {
		t.Fatalf("admitted projection=%+v err=%v", before, err)
	}
	ready, err := service.CompletePlanningCandidateReview(ctx, CompleteCandidateReviewInput{WorkspaceID: workspace.WorkspaceID, ReviewerIdentity: "auditor", Disposition: PlanningCandidateReviewReadyForApproval})
	if err != nil || ready.Disposition != PlanningCandidateReviewReadyForApproval || ready.Candidate.CandidateID != admitted.Candidate.CandidateID {
		t.Fatalf("ready review=%+v err=%v", ready, err)
	}
	// The ready review itself performs no approval: the projection advances to
	// the distinct explicit approval action, and approval without confirmation
	// is rejected.
	reviewed, err := service.ReadGuidedProjection(ctx, workspace.WorkspaceID)
	if err != nil || reviewed.PrimaryAction.Action != GuidedActionApprovePlanningCandidate || !reviewed.PrimaryAction.RequiresConfirmation || reviewed.Planning.Requirements.State != "reviewed" {
		t.Fatalf("reviewed projection=%+v err=%v", reviewed, err)
	}
	if _, err := service.ExecuteGuidedAction(ctx, GuidedActionInput{WorkspaceID: workspace.WorkspaceID, Action: string(GuidedActionApprovePlanningCandidate), ExpectedVersion: reviewed.Workspace.Version}); !errors.Is(err, ErrFeatureCompletionConfirmation) {
		t.Fatalf("approval without confirmation error=%v", err)
	}
	approved, err := service.ExecuteGuidedAction(ctx, GuidedActionInput{WorkspaceID: workspace.WorkspaceID, Action: string(GuidedActionApprovePlanningCandidate), ExpectedVersion: reviewed.Workspace.Version, Confirmation: true})
	if err != nil || approved.Projection.PrimaryAction.Action != GuidedActionPromotePlanningCandidate || approved.Projection.Planning.Requirements.State != "approved" {
		t.Fatalf("approved projection=%+v err=%v", approved.Projection, err)
	}
}

func TestGuidedPlannerAndAuditorOperationMappingsArePublishedSourceOperations(t *testing.T) {
	for _, tc := range []struct {
		operation string
		role      registry.Role
	}{
		{plannerRequirementsOperation, registry.Role("planner")}, {plannerSharedDesignOperation, registry.Role("planner")}, {plannerDeliveryTicketOperation, registry.Role("planner")}, {plannerTicketDesignBriefOperation, registry.Role("planner")},
		{auditorRequirementsReviewOperation, registry.Role("auditor")}, {auditorSharedDesignReviewOperation, registry.Role("auditor")}, {auditorDeliveryTicketReviewOperation, registry.Role("auditor")}, {auditorTicketDesignBriefReviewOperation, registry.Role("auditor")},
	} {
		if !validGuidedOperation(tc.operation, tc.role) {
			t.Fatalf("%s is not a published %s operation", tc.operation, tc.role)
		}
	}
}
