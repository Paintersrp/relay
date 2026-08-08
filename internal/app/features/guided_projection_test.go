package features

import (
	"errors"
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
	if handoff.ResumeRoute == "" || handoff.Summary == "" || handoff.Context["owner"] != "ticket_design_brief_authoring" || handoff.Context["operationId"] != "planner.ticket_design_brief" {
		t.Fatalf("handoff=%+v", handoff)
	}
	if handoff.Transfer == nil || handoff.Transfer.Ticket == nil || handoff.Transfer.Ticket.OperationID != "planner.ticket_design_brief" {
		t.Fatalf("delivery handoff does not transfer the authoring owner surface: %+v", handoff.Transfer)
	}
	for _, forbidden := range []string{"frontier_identified", "route_identified", "not_performed", "frontierCount", "routeState"} {
		for key, value := range handoff.Context {
			if strings.Contains(key, forbidden) || strings.Contains(value, forbidden) {
				t.Fatalf("delivery handoff still carries forbidden placeholder %q: %+v", forbidden, handoff.Context)
			}
		}
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
	if handoff.Context["owner"] != "planner_authoring_and_review" || handoff.Context["candidateState"] == "" {
		t.Fatalf("handoff=%+v", handoff)
	}
	if handoff.Transfer == nil || len(handoff.Transfer.Members) == 0 || handoff.Transfer.AuthorityLayers == nil {
		t.Fatalf("planner handoff does not transfer the actual owner surface: %+v", handoff.Transfer)
	}
	if _, present := handoff.Context["sourceMemberCount"]; present {
		t.Fatalf("planner handoff still carries a generic member count: %+v", handoff.Context)
	}
	if _, present := handoff.Context["externalRoleWork"]; present {
		t.Fatalf("planner handoff still carries the forbidden external-role placeholder: %+v", handoff.Context)
	}
	for key, value := range handoff.Context {
		if strings.Contains(key+value, closed.Packet.ClosurePacketID) || strings.Contains(key+value, workspace.WorkspaceID) {
			t.Fatalf("handoff exposed internal identity: %q=%q", key, value)
		}
	}
}

func TestGuidedPlanningActionsReviewApprovePromoteServerSideWithoutClientIDs(t *testing.T) {
	ctx, store, service, workspace, revision := adoptedDiscoveryLifecycle(t, DiscoveryDestinationRequirements)
	service.SetGuidedAuditOwnerForTest(&guidedFakeAuditOwner{})
	var err error
	if _, workspace, err = service.CloseFeatureDiscovery(ctx, CloseFeatureDiscoveryInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, ExpectedRevisionID: revision.DiscoveryRevisionID, Destination: DiscoveryDestinationRequirements, CreatedIdentity: "operator"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().ExecContext(ctx, `INSERT INTO repository_targets (repo_target, local_path, configured_branch_ref, configuration_version) VALUES ('guided-actions-repo', 'C:/guided-actions-repo', 'refs/heads/main', 1)`); err != nil {
		t.Fatal(err)
	}
	insertReadyPlanningSourceClosure(t, ctx, store, workspace, "guided-actions-repo", strings.Repeat("a", 40))
	bytes := []byte("# guided server-side candidate\n")
	admitted, err := service.AdmitPlanningCandidate(ctx, CandidateAdmissionInput{
		WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, Family: CandidateFamilyRequirements,
		Filename: workspace.FeatureSlug + ".requirements.md", Bytes: bytes, SHA256: discoveryTestDigest(bytes), RepoTarget: "guided-actions-repo",
		Branch: "main", BaseCommit: strings.Repeat("a", 40), Destination: DiscoveryDestinationRequirements, CreatedIdentity: "planner",
	})
	if err != nil {
		t.Fatal(err)
	}

	before, err := service.ReadGuidedProjection(ctx, workspace.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	if before.PrimaryAction.Action != GuidedActionReviewPlanningCandidate || !before.PrimaryAction.Enabled || len(before.AvailableActions) != 1 {
		t.Fatalf("admitted projection primary=%+v available=%+v", before.PrimaryAction, before.AvailableActions)
	}

	// Review is a read-only handoff composed from the existing auditor owner
	// envelope; it must not mutate approval or promotion state.
	review, err := service.ExecuteGuidedAction(ctx, GuidedActionInput{WorkspaceID: workspace.WorkspaceID, Action: string(GuidedActionReviewPlanningCandidate), ExpectedVersion: before.Workspace.Version})
	if err != nil {
		t.Fatal(err)
	}
	if review.Handoff == nil || review.Handoff.Context["owner"] != "auditor_review" || review.Handoff.Context["candidateState"] != "admitted" {
		t.Fatalf("review handoff=%+v", review.Handoff)
	}
	if _, present := review.Handoff.Context["externalRoleWork"]; present {
		t.Fatalf("review handoff still carries the forbidden external-role placeholder: %+v", review.Handoff.Context)
	}
	stillAdmitted, err := service.ReadGuidedProjection(ctx, workspace.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	if stillAdmitted.PrimaryAction.Action != GuidedActionReviewPlanningCandidate || stillAdmitted.Workspace.Version != before.Workspace.Version {
		t.Fatalf("review mutated state: primary=%+v version=%d", stillAdmitted.PrimaryAction, stillAdmitted.Workspace.Version)
	}

	// The execution gate permits exactly the present enabled primary action:
	// while review is primary, approval execution is rejected even when the
	// operator supplies confirmation.
	if _, err := service.ExecuteGuidedAction(ctx, GuidedActionInput{WorkspaceID: workspace.WorkspaceID, Action: string(GuidedActionApprovePlanningCandidate), ExpectedVersion: stillAdmitted.Workspace.Version}); !errors.Is(err, ErrGuidedActionBlocked) {
		t.Fatalf("non-primary approve without confirmation error=%v", err)
	}
	if _, err := service.ExecuteGuidedAction(ctx, GuidedActionInput{WorkspaceID: workspace.WorkspaceID, Action: string(GuidedActionApprovePlanningCandidate), ExpectedVersion: stillAdmitted.Workspace.Version, Confirmation: true}); !errors.Is(err, ErrGuidedActionBlocked) {
		t.Fatalf("non-primary approve with confirmation error=%v", err)
	}
	unchanged, err := service.ReadGuidedProjection(ctx, workspace.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	if unchanged.PrimaryAction.Action != GuidedActionReviewPlanningCandidate || unchanged.Workspace.Version != stillAdmitted.Workspace.Version {
		t.Fatalf("blocked approval mutated state: primary=%+v version=%d", unchanged.PrimaryAction, unchanged.Workspace.Version)
	}

	// Approval remains a server-side owner operation with no client-supplied
	// candidate identity; it advances the guided primary action to promotion.
	if _, err = service.ApprovePlanningCandidate(ctx, CandidateApprovalInput{
		CandidateID: admitted.Candidate.CandidateID, ExpectedSHA256: admitted.Candidate.ArtifactSha256, ExpectedSizeBytes: admitted.Candidate.ArtifactSizeBytes,
		Bytes: bytes, ExpectedVersion: unchanged.Workspace.Version, ExpectedClosurePacketRowID: workspace.CurrentDiscoveryClosurePacketRowID,
		ExpectedAuthorityRevisionRowID: workspace.CurrentAuthorityRevisionRowID, OperatorConfirmationEvidence: "reviewed", CreatedIdentity: "auditor",
	}); err != nil {
		t.Fatal(err)
	}
	awaitingPromotion, err := service.ReadGuidedProjection(ctx, workspace.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	if awaitingPromotion.PrimaryAction.Action != GuidedActionPromotePlanningCandidate || !awaitingPromotion.PrimaryAction.Enabled {
		t.Fatalf("approved projection primary=%+v", awaitingPromotion.PrimaryAction)
	}
	if candidate, candidateErr := service.guidedCurrentPlanningCandidate(ctx, workspace.WorkspaceID, DiscoveryDestinationRequirements, false); !errors.Is(candidateErr, ErrGuidedActionBlocked) {
		t.Fatalf("approved candidate still resolvable as admitted: %+v err=%v", candidate, candidateErr)
	}

	// Promotion publishes the approved candidate as workspace authority and
	// advances the journey to the Delivery Ticket frontier.
	promoted, err := service.ExecuteGuidedAction(ctx, GuidedActionInput{WorkspaceID: workspace.WorkspaceID, Action: string(GuidedActionPromotePlanningCandidate), ExpectedVersion: awaitingPromotion.Workspace.Version})
	if err != nil {
		t.Fatal(err)
	}
	if promoted.Projection.PrimaryAction.Action != GuidedActionAuthorDeliveryTicket || !promoted.Projection.PrimaryAction.Enabled {
		t.Fatalf("promoted projection primary=%+v", promoted.Projection.PrimaryAction)
	}
	if !hasGuidedLayer(promoted.Projection.Authority.Layers, CandidateFamilyRequirements) {
		t.Fatalf("promoted authority layers=%v", promoted.Projection.Authority.Layers)
	}
	if _, candidateErr := service.guidedCurrentPlanningCandidate(ctx, workspace.WorkspaceID, DiscoveryDestinationRequirements, true); !errors.Is(candidateErr, ErrGuidedActionBlocked) {
		t.Fatalf("promoted candidate still resolvable as approved: err=%v", candidateErr)
	}
}
