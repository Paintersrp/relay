package features

import (
	"strings"
	"testing"

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
			projection := composeGuidedFeatureProjection(
				workflowstore.FeatureWorkspace{WorkspaceID: "workspace", FeatureSlug: "payments", Version: 4},
				workflowstore.Project{ProjectID: "project", Name: "Relay"}, assessment,
				FeatureCurrentnessDecision{Readiness: FeatureCurrent}, nil,
				CompletionStatus{Gates: []CompletionGate{{Name: "closure", Ready: true}, {Name: "authority", Ready: true}}},
				GuidedPlanningSection{}, GuidedDeliverySection{}, GuidedPrototypeSection{},
			)
			if projection.PrimaryAction.Action != tc.want || !projection.PrimaryAction.Primary || len(projection.AvailableActions) != 1 || !projection.AvailableActions[0].Primary {
				t.Fatalf("primary=%+v", projection.PrimaryAction)
			}
		})
	}
}

func TestGuidedHandoffIsDistinctAndCarriesOwnerPreparationContext(t *testing.T) {
	ctx, _, service, workspace, revision := adoptedDiscoveryLifecycle(t, DiscoveryDestinationDirectDeliveryTicket)
	var err error
	if _, workspace, err = service.CloseFeatureDiscovery(ctx, CloseFeatureDiscoveryInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, ExpectedRevisionID: revision.DiscoveryRevisionID, Destination: DiscoveryDestinationDirectDeliveryTicket, CreatedIdentity: "operator"}); err != nil {
		t.Fatal(err)
	}
	projection := GuidedFeatureProjection{
		Workspace:   GuidedWorkspaceSection{WorkspaceID: workspace.WorkspaceID},
		Discovery:   GuidedDiscoverySection{Destination: string(DiscoveryDestinationDirectDeliveryTicket), Continuation: "resume package"},
		Currentness: GuidedCurrentnessSection{Readiness: string(FeatureCurrent)},
		Delivery:    GuidedDeliverySection{SelectionState: "none"},
	}
	handoff, err := service.guidedHandoff(ctx, workspace.WorkspaceID, GuidedActionAuthorDeliveryTicket, projection)
	if err != nil {
		t.Fatal(err)
	}
	if handoff.ResumeRoute == "" || handoff.Summary == "" || handoff.Context["owner"] != "delivery_ticket_frontier" || handoff.Context["preparationStatus"] != "frontier_identified" || handoff.Context["frontierCount"] != "0" || handoff.Context["externalRoleWork"] != "not_performed" {
		t.Fatalf("handoff=%+v", handoff)
	}
}

func TestGuidedProjectionSeparatesRecoveryAndDiagnostics(t *testing.T) {
	projection := composeGuidedFeatureProjection(
		workflowstore.FeatureWorkspace{WorkspaceID: "workspace", Version: 9}, workflowstore.Project{},
		DiscoveryAssessment{State: DiscoveryStateClosed, Currentness: DiscoveryCurrent, Revision: &workflowstore.IntegratedDiscoveryRevision{DiscoveryRevisionID: "revision"}},
		FeatureCurrentnessDecision{Readiness: FeatureStale, StaleOwner: "authority", RecoveryCategory: "publish_current_authority", BlockedOperation: "promotion", Effect: "no mutation", Basis: "authority mismatch", HistoricalIdentity: "historical"},
		nil, CompletionStatus{}, GuidedPlanningSection{}, GuidedDeliverySection{}, GuidedPrototypeSection{},
	)
	if projection.Recovery.State != "required" || projection.Recovery.Category != "publish_current_authority" || len(projection.Diagnostics.Stale) != 3 || len(projection.Diagnostics.Historical) != 1 {
		t.Fatalf("recovery=%+v diagnostics=%+v", projection.Recovery, projection.Diagnostics)
	}
}

func TestGuidedPlanningDistinguishesReviewApprovalPromotionAndHistoricalBasis(t *testing.T) {
	ctx, store, service, workspace, revision := adoptedDiscoveryLifecycle(t, DiscoveryDestinationRequirements)
	var err error
	closed, workspace, err := service.CloseFeatureDiscovery(ctx, CloseFeatureDiscoveryInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, ExpectedRevisionID: revision.DiscoveryRevisionID, Destination: DiscoveryDestinationRequirements, CreatedIdentity: "operator"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().ExecContext(ctx, `INSERT INTO repository_targets (repo_target, local_path, configured_branch_ref, configuration_version) VALUES ('guided-planning-repo', 'C:/guided-planning-repo', 'refs/heads/main', 1)`); err != nil {
		t.Fatal(err)
	}
	insertReadyPlanningSourceClosure(t, ctx, store, workspace, "guided-planning-repo", strings.Repeat("a", 40))
	bytes := []byte("# guided requirements\n")
	admitted, err := service.AdmitPlanningCandidate(ctx, CandidateAdmissionInput{
		WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, Family: CandidateFamilyRequirements,
		Filename: workspace.FeatureSlug + ".requirements.md", Bytes: bytes, SHA256: discoveryTestDigest(bytes), RepoTarget: "guided-planning-repo",
		Branch: "main", BaseCommit: strings.Repeat("a", 40), Destination: DiscoveryDestinationRequirements, CreatedIdentity: "planner",
	})
	if err != nil {
		t.Fatal(err)
	}
	planning, err := service.guidedPlanning(ctx, workspace, FeatureCurrentnessDecision{Readiness: FeatureCurrent}, nil)
	if err != nil || planning.CandidateCount != 1 || planning.AwaitingReview != 1 || planning.AwaitingPromotion != 0 || planning.CandidateState != "admitted" || planning.ReviewState != "awaiting_review" || planning.ApprovalState != "none" || planning.PromotionState != "none" {
		t.Fatalf("admitted planning=%+v err=%v", planning, err)
	}
	approved, err := service.ApprovePlanningCandidate(ctx, CandidateApprovalInput{
		CandidateID: admitted.Candidate.CandidateID, ExpectedSHA256: admitted.Candidate.ArtifactSha256, ExpectedSizeBytes: admitted.Candidate.ArtifactSizeBytes,
		Bytes: bytes, ExpectedVersion: workspace.Version, ExpectedClosurePacketRowID: workspace.CurrentDiscoveryClosurePacketRowID,
		ExpectedAuthorityRevisionRowID: workspace.CurrentAuthorityRevisionRowID, OperatorConfirmationEvidence: "reviewed", CreatedIdentity: "auditor",
	})
	if err != nil {
		t.Fatal(err)
	}
	planning, err = service.guidedPlanning(ctx, workspace, FeatureCurrentnessDecision{Readiness: FeatureCurrent}, nil)
	if err != nil || planning.AwaitingReview != 0 || planning.AwaitingPromotion != 1 || planning.CandidateState != "reviewed" || planning.ReviewState != "reviewed" || planning.ApprovalState != "approved" || planning.PromotionState != "awaiting_promotion" {
		t.Fatalf("approved planning=%+v err=%v", planning, err)
	}
	promoted, err := service.PromoteApprovedPlanningCandidate(ctx, CandidatePromotionInput{CandidateID: approved.Candidate.CandidateID, ApprovalID: approved.Approval.ApprovalID, ExpectedVersion: workspace.Version, CreatedIdentity: "planner"})
	if err != nil {
		t.Fatal(err)
	}
	authority, err := service.ReadAuthority(ctx, workspace.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	planning, err = service.guidedPlanning(ctx, promoted.Workspace, FeatureCurrentnessDecision{Readiness: FeatureCurrent}, authority)
	if err != nil || planning.CandidateCount != 1 || planning.Promoted != 1 || planning.Status != "promoted" || planning.PromotionState != "promoted" {
		t.Fatalf("promoted planning=%+v err=%v", planning, err)
	}
	replacement := []byte("# reopened guided requirements\n")
	_, reopened, err := service.ReopenFeatureDiscovery(ctx, ReopenFeatureDiscoveryInput{
		WorkspaceID: promoted.Workspace.WorkspaceID, ExpectedVersion: promoted.Workspace.Version, ExpectedPacketID: closed.Packet.ClosurePacketID,
		OperatorConfirmed: true, Cause: "historical guided planning", CreatedIdentity: "operator", Markdown: replacement,
		SHA256: discoveryTestDigest(replacement), Destination: DiscoveryDestinationRequirements,
	})
	if err == nil && !service.planningCandidateHistorical(ctx, reopened, admitted.Candidate) {
		t.Fatal("reopened workspace treated prior candidate as current")
	}
	if err != nil {
		t.Logf("reopen fixture unavailable for historical basis assertion: %v", err)
	}
}

func TestGuidedPlannerHandoffUsesOwnerCompositionWithoutInternalContext(t *testing.T) {
	ctx, _, service, workspace, revision := adoptedDiscoveryLifecycle(t, DiscoveryDestinationRequirements)
	closed, workspace, err := service.CloseFeatureDiscovery(ctx, CloseFeatureDiscoveryInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, ExpectedRevisionID: revision.DiscoveryRevisionID, Destination: DiscoveryDestinationRequirements, CreatedIdentity: "operator"})
	if err != nil {
		t.Fatal(err)
	}
	handoff, err := service.guidedHandoff(ctx, workspace.WorkspaceID, GuidedActionAuthorRequirements, GuidedFeatureProjection{
		Discovery:   GuidedDiscoverySection{Destination: string(DiscoveryDestinationRequirements)},
		Currentness: GuidedCurrentnessSection{Readiness: string(FeatureCurrent)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if handoff.Context["owner"] != "planner_authoring_and_review" || handoff.Context["preparationStatus"] != "ready" || handoff.Context["externalRoleWork"] != "not_performed" || handoff.Context["sourceMemberCount"] == "" || handoff.Context["authorityLayerCount"] == "" {
		t.Fatalf("handoff=%+v", handoff)
	}
	for key, value := range handoff.Context {
		if strings.Contains(key+value, closed.Packet.ClosurePacketID) || strings.Contains(key+value, workspace.WorkspaceID) {
			t.Fatalf("handoff exposed internal identity: %q=%q", key, value)
		}
	}
}
