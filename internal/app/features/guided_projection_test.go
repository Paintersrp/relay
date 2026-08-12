package features

import (
	"context"
	"database/sql"
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
			projection := composeGuidedFeatureProjection(workflowstore.FeatureWorkspace{WorkspaceID: "workspace", FeatureSlug: "payments", Version: 4}, workflowstore.Project{ProjectID: "project", Name: "Relay"}, assessment, FeatureCurrentnessDecision{Readiness: FeatureCurrent}, nil, CompletionStatus{Gates: []CompletionGate{{Name: "closure", Ready: true}, {Name: "authority", Ready: true}}}, GuidedPlanningSection{}, GuidedDeliverySection{}, GuidedPrototypeSection{}, GuidedProgramSection{})
			if projection.PrimaryAction.Action != tc.want || !projection.PrimaryAction.Primary {
				t.Fatalf("primary=%+v", projection.PrimaryAction)
			}
			primaryCount := 0
			for _, action := range projection.AvailableActions {
				if action.Primary {
					primaryCount++
				}
			}
			if primaryCount != 1 {
				t.Fatalf("projection has %d primary actions: %+v", primaryCount, projection.AvailableActions)
			}
			if tc.destination == DiscoveryDestinationNoDeliveryWork {
				// Completion is ready here: the primary stays complete_feature
				// and the enabled confirmed abandonment secondary is the only
				// other availability.
				if len(projection.AvailableActions) != 2 || projection.AvailableActions[1].Action != GuidedActionAbandonFeature || !projection.AvailableActions[1].Enabled || !projection.AvailableActions[1].RequiresConfirmation {
					t.Fatalf("abandonment availability=%+v", projection.AvailableActions)
				}
			} else if len(projection.AvailableActions) != 1 {
				t.Fatalf("available actions=%+v", projection.AvailableActions)
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
	if strings.Contains(handoff.Summary, "ticket design brief") {
		t.Fatalf("delivery handoff substituted ticket-design-brief operation: %q", handoff.Summary)
	}
}

func TestGuidedProjectionSeparatesRecoveryAndDiagnostics(t *testing.T) {
	projection := composeGuidedFeatureProjection(workflowstore.FeatureWorkspace{WorkspaceID: "workspace", Version: 9}, workflowstore.Project{}, DiscoveryAssessment{State: DiscoveryStateClosed, Currentness: DiscoveryCurrent, Revision: &workflowstore.IntegratedDiscoveryRevision{DiscoveryRevisionID: "revision"}}, FeatureCurrentnessDecision{Readiness: FeatureStale, StaleOwner: "authority", RecoveryCategory: "publish_current_authority", BlockedOperation: "promotion", Effect: "no mutation", Basis: "authority mismatch", HistoricalIdentity: "historical"}, nil, CompletionStatus{}, GuidedPlanningSection{}, GuidedDeliverySection{}, GuidedPrototypeSection{}, GuidedProgramSection{})
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
	if _, err := service.CompletePlanningCandidateReview(ctx, CompleteCandidateReviewInput{WorkspaceID: workspace.WorkspaceID, CandidateID: candidate.Candidate.CandidateID, ReviewerIdentity: "auditor", Disposition: PlanningCandidateReviewReadyForApproval, ReviewedBytes: bytes}); err != nil {
		t.Fatal(err)
	}
	// A ready review arms the distinct explicit approval: the family is
	// reviewed and awaiting approval, not approved.
	planning, err = service.guidedPlanning(ctx, workspace, FeatureCurrentnessDecision{Readiness: FeatureCurrent}, nil)
	if err != nil || planning.AwaitingReview != 0 || planning.AwaitingApproval != 1 || planning.AwaitingPromotion != 0 || planning.Requirements.State != "reviewed" {
		t.Fatalf("reviewed planning=%+v err=%v", planning, err)
	}
	approval := approveCurrentPlanningCandidate(t, ctx, service, workspace, candidate.Candidate.CandidateID, "guided exact approval", bytes)
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
	ready, err := service.CompletePlanningCandidateReview(ctx, CompleteCandidateReviewInput{WorkspaceID: workspace.WorkspaceID, CandidateID: admitted.Candidate.CandidateID, ReviewerIdentity: "auditor", Disposition: PlanningCandidateReviewReadyForApproval, ReviewedBytes: bytes})
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
		{plannerRequirementsOperation, registry.Role("planner")}, {plannerSharedDesignOperation, registry.Role("planner")}, {plannerDeliveryPlanOperation, registry.Role("planner")}, {plannerDeliveryTicketOperation, registry.Role("planner")},
		{auditorRequirementsReviewOperation, registry.Role("auditor")}, {auditorSharedDesignReviewOperation, registry.Role("auditor")}, {auditorDeliveryPlanReviewOperation, registry.Role("auditor")}, {auditorDeliveryTicketReviewOperation, registry.Role("auditor")},
	} {
		if !validGuidedOperation(tc.operation, tc.role) {
			t.Fatalf("%s is not a published %s operation", tc.operation, tc.role)
		}
	}
}

// guidedAbandonFixture builds an adopted, closed, no-delivery-work workspace
// whose completion gates are ready and whose current closing decision is not
// yet recorded, so the guided journey advertises complete_feature with the
// enabled confirmed abandonment secondary.
func guidedAbandonFixture(t *testing.T) (context.Context, *workflowstore.Store, *Service, workflowstore.FeatureWorkspace) {
	t.Helper()
	ctx, store, service, workspace, revision := adoptedDiscoveryLifecycle(t, DiscoveryDestinationNoDeliveryWork)
	var err error
	if _, workspace, err = service.CloseFeatureDiscovery(ctx, CloseFeatureDiscoveryInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, ExpectedRevisionID: revision.DiscoveryRevisionID, Destination: DiscoveryDestinationNoDeliveryWork, CreatedIdentity: "operator"}); err != nil {
		t.Fatal(err)
	}
	var artifactID, vaultID int64
	if err := store.DB().QueryRowContext(ctx, `SELECT id FROM artifacts ORDER BY id LIMIT 1`).Scan(&artifactID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().ExecContext(ctx, `INSERT INTO repository_targets (repo_target, local_path, configured_branch_ref, configuration_version) VALUES ('relay', 'C:/relay', 'refs/heads/main', 1)`); err != nil {
		t.Fatal(err)
	}
	if err := store.DB().QueryRowContext(ctx, `INSERT INTO source_vaults (vault_id, repo_target, relative_path) VALUES ('vault-abandon-fixture', 'relay', 'vaults/features') RETURNING id`).Scan(&vaultID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().ExecContext(ctx, `INSERT INTO source_vault_closures (closure_id, vault_row_id, commit_oid, tree_oid, generation, ref_name, state, import_started_at, verified_at) VALUES ('closure-abandon-fixture', ?, ?, ?, 1, 'refs/relay/closures/abandon-fixture', 'ready', '2026-07-18T00:00:00.000000000Z', '2026-07-18T00:00:01.000000000Z')`, vaultID, strings.Repeat("d", 40), strings.Repeat("e", 40)); err != nil {
		t.Fatal(err)
	}
	closure, err := store.GetSourceVaultClosureByClosureID(ctx, "closure-abandon-fixture")
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
	return ctx, store, service, workspace
}

// TestGuidedAbandonExecutesOwnerMutationAndSelectsNormalReopenPath asserts the
// enabled secondary abandonment action is accepted by ExecuteGuidedAction only
// with its own confirmation, records the server-projected abandoned outcome,
// and leaves the guided journey on the normal existing reopen path. It never
// turns abandonment into completion or introduces a browser lifecycle identity.
func TestGuidedAbandonExecutesOwnerMutationAndSelectsNormalReopenPath(t *testing.T) {
	ctx, store, service, workspace := guidedAbandonFixture(t)
	service.SetGuidedAuditOwnerForTest(&guidedFakeAuditOwner{})
	ticketOwner, err := workflowtickets.NewService(store)
	if err != nil {
		t.Fatal(err)
	}
	service.SetGuidedTicketOwnerForTest(ticketOwner)

	before, err := service.ReadGuidedProjection(ctx, workspace.WorkspaceID)
	if err != nil || before.PrimaryAction.Action != GuidedActionCompleteFeature || !before.PrimaryAction.Enabled || before.Completion.Recorded || before.Completion.Decision != "" {
		t.Fatalf("completion-ready projection primary=%+v completion=%+v err=%v", before.PrimaryAction, before.Completion, err)
	}
	if !guidedActionAvailable(before.AvailableActions, GuidedActionAbandonFeature) {
		t.Fatalf("completion-ready projection did not advertise the abandon secondary: %+v", before.AvailableActions)
	}

	// Abandon is a confirmed secondary mutation: without confirmation it is
	// rejected before any owner mutation, exactly like completion.
	if _, err := service.ExecuteGuidedAction(ctx, GuidedActionInput{WorkspaceID: workspace.WorkspaceID, Action: string(GuidedActionAbandonFeature), ExpectedVersion: before.Workspace.Version}); !errors.Is(err, ErrFeatureCompletionConfirmation) {
		t.Fatalf("abandon without confirmation error = %v", err)
	}

	result, err := service.ExecuteGuidedAction(ctx, GuidedActionInput{WorkspaceID: workspace.WorkspaceID, Action: string(GuidedActionAbandonFeature), ExpectedVersion: before.Workspace.Version, Confirmation: true})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Projection.Completion.Recorded || result.Projection.Completion.Decision != "abandoned" {
		t.Fatalf("abandoned projection completion=%+v", result.Projection.Completion)
	}
	if result.Projection.Workspace.Version <= before.Workspace.Version {
		t.Fatalf("abandon did not advance the workspace version: %d -> %d", before.Workspace.Version, result.Projection.Workspace.Version)
	}
	// The server selects the normal existing reopen path for the abandoned
	// decision, exactly as for a completed decision; abandonment is not
	// completion and nothing makes it current automatically.
	if result.Projection.PrimaryAction.Action != GuidedActionReopenDiscovery || !result.Projection.PrimaryAction.Enabled || !result.Projection.PrimaryAction.RequiresConfirmation {
		t.Fatalf("abandoned projection primary=%+v", result.Projection.PrimaryAction)
	}
	// After the abandoned decision is recorded neither abandonment nor
	// completion is an advertised action; the primary-only guard rejects both.
	if _, err := service.ExecuteGuidedAction(ctx, GuidedActionInput{WorkspaceID: workspace.WorkspaceID, Action: string(GuidedActionAbandonFeature), ExpectedVersion: result.Projection.Workspace.Version, Confirmation: true}); !errors.Is(err, ErrGuidedActionBlocked) {
		t.Fatalf("second abandonment error = %v", err)
	}
	if _, err := service.ExecuteGuidedAction(ctx, GuidedActionInput{WorkspaceID: workspace.WorkspaceID, Action: string(GuidedActionCompleteFeature), ExpectedVersion: result.Projection.Workspace.Version, Confirmation: true}); !errors.Is(err, ErrGuidedActionBlocked) {
		t.Fatalf("completion after abandonment error = %v", err)
	}
}
