package features

import (
	"context"
	"errors"
	"strings"

	apptickets "relay/internal/app/tickets"
	appworkflow "relay/internal/app/workflow"
	workflowstore "relay/internal/store/workflow"
)

var ErrGuidedActionBlocked = errors.New("guided action is not the presently enabled primary action")

// GuidedFeatureProjection is the application-owned semantic journey. It is
// deliberately free of artifact, revision, approval, candidate, package, Run,
// and digest identities; those are resolved by the owning services.
type GuidedFeatureProjection struct {
	Workspace        GuidedWorkspaceSection
	Project          GuidedProjectSection
	Discovery        GuidedDiscoverySection
	Currentness      GuidedCurrentnessSection
	Authority        GuidedAuthoritySection
	Planning         GuidedPlanningSection
	Delivery         GuidedDeliverySection
	Prototype        GuidedPrototypeSection
	Completion       GuidedCompletionSection
	Recovery         GuidedRecoverySection
	Diagnostics      GuidedDiagnosticsSection
	AvailableActions []GuidedFeatureActionAvailability
	PrimaryAction    GuidedFeatureActionAvailability
	Handoff          *GuidedHandoff
}

type GuidedWorkspaceSection struct {
	WorkspaceID string
	FeatureSlug string
	State       string
	Version     int64
	CreatedAt   string
	UpdatedAt   string
}
type GuidedProjectSection struct{ ProjectID, Name string }
type GuidedDiscoverySection struct {
	State, Destination, Rationale, Continuation, Currentness                                                 string
	HasCurrentRevision                                                                                       bool
	Blockers, RestorationActions, PendingIntegrations, ActiveOperations, RouteMaterialOpen, RequiredEvidence []string
}
type GuidedCurrentnessSection struct {
	Readiness, Owner, BlockedOperation, Effect, RecoveryCategory string
}

type GuidedAuthoritySection struct {
	CurrentRevisionNumber int
	Layers                []string
}
type GuidedPlanningSection struct {
	Status, CandidateState, ReviewState, ApprovalState, PromotionState            string
	CandidateCount, AwaitingReview, AwaitingApproval, AwaitingPromotion, Promoted int
	HistoricalCount                                                               int
	Requirements, SharedDesign                                                    GuidedPlanningFamilySection
}

// GuidedPlanningFamilySection carries the semantic progression of one planning
// family so the guided decision can sequence author -> review -> approval ->
// promotion -> next family or ticket without relying on authority presence.
type GuidedPlanningFamilySection struct {
	Count, AwaitingReview, AwaitingPromotion, Promoted int
	State                                              string // none | admitted | approved | promoted
}

type GuidedDeliverySection struct {
	FrontierCount                                                        int
	SelectionState, PackageState, RunState, AuditState, RemediationState string
}
type GuidedPrototypeSection struct {
	RunState, CleanupState, QAState, EvidenceState string
}
type GuidedCompletionSection struct {
	Gates           []GuidedCompletionGate
	Ready, Recorded bool
}
type GuidedRecoverySection struct {
	State, Category string
	Available       []string
}
type GuidedDiagnosticsSection struct {
	Stale, Historical, Discovery []string
}
type GuidedHandoff struct {
	Role, Summary, ResumeRoute string
	Context                    map[string]string
}

type GuidedActionInput struct {
	WorkspaceID, Action string
	ExpectedVersion     int64
	Confirmation        bool
	Destination         DiscoveryDestination
}
type GuidedActionResult struct {
	Projection GuidedFeatureProjection
	Handoff    *GuidedHandoff
}

// ReadGuidedProjection composes all currently exposed Feature-owned state and
// the existing downstream store owners into one resumable semantic journey.
func (s *Service) ReadGuidedProjection(ctx context.Context, workspaceID string) (GuidedFeatureProjection, error) {
	workspace, err := s.store.GetFeatureWorkspaceByWorkspaceID(ctx, strings.TrimSpace(workspaceID))
	if err != nil {
		return GuidedFeatureProjection{}, err
	}
	project, err := s.store.GetProjectByRowID(ctx, workspace.ProjectRowID)
	if err != nil {
		return GuidedFeatureProjection{}, err
	}
	assessment, err := s.AssessDiscoveryDestination(ctx, workspace.WorkspaceID)
	if err != nil {
		return GuidedFeatureProjection{}, err
	}
	currentness, err := s.Currentness(ctx, workspace.WorkspaceID)
	if err != nil {
		return GuidedFeatureProjection{}, err
	}
	authority, err := s.ReadAuthority(ctx, workspace.WorkspaceID)
	if err != nil {
		return GuidedFeatureProjection{}, err
	}
	completion, err := s.EvaluateCompletion(ctx, workspace.WorkspaceID)
	if err != nil {
		return GuidedFeatureProjection{}, err
	}
	planning, err := s.guidedPlanning(ctx, workspace, currentness, authority)
	if err != nil {
		return GuidedFeatureProjection{}, err
	}
	delivery, err := s.guidedDelivery(ctx, workspace)
	if err != nil {
		return GuidedFeatureProjection{}, err
	}
	prototype, err := s.guidedPrototype(ctx, workspace)
	if err != nil {
		return GuidedFeatureProjection{}, err
	}
	return composeGuidedFeatureProjection(workspace, project, assessment, currentness, authority, completion, planning, delivery, prototype), nil
}

func composeGuidedFeatureProjection(workspace workflowstore.FeatureWorkspace, project workflowstore.Project, assessment DiscoveryAssessment, currentness FeatureCurrentnessDecision, authority []AuthorityRevisionDetail, completion CompletionStatus, planning GuidedPlanningSection, delivery GuidedDeliverySection, prototype GuidedPrototypeSection) GuidedFeatureProjection {
	layers := make([]string, 0)
	currentRevisionNumber := 0
	for _, revision := range authority {
		if revision.Historical {
			continue
		}
		currentRevisionNumber = int(revision.Revision.RevisionNumber)
		for _, layer := range revision.Layers {
			layers = append(layers, layer.LayerKind)
		}
	}
	gates := make([]GuidedCompletionGate, 0, len(completion.Gates))
	for _, gate := range completion.Gates {
		gates = append(gates, GuidedCompletionGate{Name: gate.Name, Ready: gate.Ready})
	}
	decision := DecideGuidedFeatureAction(GuidedJourneyState{
		State: assessment.State, Destination: assessment.Destination, HasCurrentRevision: assessment.Revision != nil,
		AuthorityLayers: layers, Planning: planning, Continuation: assessment.Continuation, Blockers: assessment.Blockers,
		PendingIntegrations: assessment.PendingIntegrations, ActiveOperations: assessment.ActiveOperations,
		RouteMaterialOpen: assessment.RouteMaterialOpen, RequiredEvidence: assessment.RequiredEvidence,
	}, currentness, GuidedCompletion{Gates: gates, Recorded: completion.CurrentDecision != nil})
	primary := decision.AvailableActions[0]
	recovery := GuidedRecoverySection{State: "none", Category: decision.RecoveryCategory}
	if currentness.Readiness != FeatureCurrent {
		recovery.State = "required"
		recovery.Available = []string{currentness.RecoveryCategory}
	}
	return GuidedFeatureProjection{
		Workspace:   GuidedWorkspaceSection{WorkspaceID: workspace.WorkspaceID, FeatureSlug: workspace.FeatureSlug, State: workspace.State, Version: workspace.Version, CreatedAt: workspace.CreatedAt, UpdatedAt: workspace.UpdatedAt},
		Project:     GuidedProjectSection{ProjectID: project.ProjectID, Name: project.Name},
		Discovery:   GuidedDiscoverySection{State: string(assessment.State), Destination: string(assessment.Destination), Rationale: assessment.Rationale, Continuation: assessment.Continuation, Currentness: string(assessment.Currentness), HasCurrentRevision: assessment.Revision != nil, Blockers: append([]string(nil), assessment.Blockers...), RestorationActions: append([]string(nil), assessment.RestorationActions...), PendingIntegrations: append([]string(nil), assessment.PendingIntegrations...), ActiveOperations: append([]string(nil), assessment.ActiveOperations...), RouteMaterialOpen: append([]string(nil), assessment.RouteMaterialOpen...), RequiredEvidence: append([]string(nil), assessment.RequiredEvidence...)},
		Currentness: GuidedCurrentnessSection{Readiness: string(currentness.Readiness), Owner: currentness.StaleOwner, BlockedOperation: currentness.BlockedOperation, Effect: currentness.Effect, RecoveryCategory: currentness.RecoveryCategory},
		Authority:   GuidedAuthoritySection{CurrentRevisionNumber: currentRevisionNumber, Layers: append([]string(nil), layers...)},
		Planning:    planning, Delivery: delivery, Prototype: prototype,
		Completion:       GuidedCompletionSection{Gates: gates, Ready: GuidedCompletionReady(gates), Recorded: completion.CurrentDecision != nil},
		Recovery:         recovery,
		Diagnostics:      GuidedDiagnosticsSection{Stale: nonEmpty(currentness.StaleOwner, currentness.BlockedOperation, currentness.Effect), Historical: guidedHistoricalDiagnostics(currentness, assessment), Discovery: append([]string(nil), assessment.Blockers...)},
		AvailableActions: append([]GuidedFeatureActionAvailability(nil), decision.AvailableActions...),
		PrimaryAction:    primary,
	}
}

func nonEmpty(values ...string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			result = append(result, value)
		}
	}
	return result
}

func guidedHistoricalDiagnostics(currentness FeatureCurrentnessDecision, assessment DiscoveryAssessment) []string {
	if currentness.Readiness == FeatureStale || assessment.Currentness == DiscoveryHistorical {
		return []string{"historical_basis_requires_recovery"}
	}
	return []string{}
}

func candidatePromotedInCurrentAuthority(candidate workflowstore.PlanningCandidate, authority []AuthorityRevisionDetail) bool {
	for _, revision := range authority {
		if revision.Historical {
			continue
		}
		if candidatePromotedInCurrentAuthorityLayers(candidate, revision.Layers) {
			return true
		}
	}
	return false
}

func candidatePromotedInCurrentAuthorityLayers(candidate workflowstore.PlanningCandidate, layers []workflowstore.FeatureWorkspaceAuthorityLayer) bool {
	for _, layer := range layers {
		if layer.CandidateArtifactRowID.Valid && layer.CandidateArtifactRowID.Int64 == candidate.ArtifactRowID {
			return true
		}
	}
	return false
}

func (s *Service) guidedPlanning(ctx context.Context, workspace workflowstore.FeatureWorkspace, currentness FeatureCurrentnessDecision, authority []AuthorityRevisionDetail) (GuidedPlanningSection, error) {
	candidates, err := s.store.ListPlanningCandidatesByWorkspace(ctx, workspace.ID)
	if err != nil {
		return GuidedPlanningSection{}, err
	}
	result := GuidedPlanningSection{Status: "not_started", CandidateState: "none", ReviewState: "none", ApprovalState: "none", PromotionState: "none"}
	requirements := GuidedPlanningFamilySection{}
	sharedDesign := GuidedPlanningFamilySection{}
	for _, candidate := range candidates {
		promoted := candidatePromotedInCurrentAuthority(candidate, authority)
		historical := s.planningCandidateHistorical(ctx, workspace, candidate)
		if historical && !(promoted && workspace.CurrentDiscoveryClosurePacketRowID.Valid && candidate.DiscoveryClosurePacketRowID == workspace.CurrentDiscoveryClosurePacketRowID.Int64) {
			result.HistoricalCount++
			continue
		}
		result.CandidateCount++
		approvals, approvalErr := s.store.ListPlanningCandidateApprovalsByCandidate(ctx, candidate.ID)
		if approvalErr != nil {
			return GuidedPlanningSection{}, approvalErr
		}
		var family *GuidedPlanningFamilySection
		switch candidate.Family {
		case CandidateFamilyRequirements:
			family = &requirements
		case CandidateFamilySharedDesign:
			family = &sharedDesign
		}
		switch {
		case promoted:
			result.Promoted++
			if family != nil {
				family.Promoted++
			}
		case len(approvals) > 0:
			result.AwaitingPromotion++
			if family != nil {
				family.AwaitingPromotion++
			}
		default:
			result.AwaitingReview++
			if family != nil {
				family.AwaitingReview++
			}
		}
		if family != nil {
			family.Count++
		}
	}
	if result.CandidateCount > 0 {
		result.Status = "in_progress"
		if result.Promoted == result.CandidateCount {
			result.Status = "promoted"
			result.CandidateState, result.ReviewState, result.ApprovalState, result.PromotionState = "promoted", "reviewed", "approved", "promoted"
		} else if result.AwaitingPromotion > 0 {
			result.CandidateState, result.ReviewState, result.ApprovalState, result.PromotionState = "reviewed", "reviewed", "approved", "awaiting_promotion"
		} else if result.AwaitingReview > 0 {
			result.CandidateState, result.ReviewState, result.ApprovalState, result.PromotionState = "admitted", "awaiting_review", "none", "none"
		}
	}
	if currentness.Readiness != FeatureCurrent && result.CandidateCount == 0 {
		result.Status = "blocked"
	}
	requirements.State = guidedPlanningFamilyState(requirements)
	sharedDesign.State = guidedPlanningFamilyState(sharedDesign)
	result.Requirements = requirements
	result.SharedDesign = sharedDesign
	return result, nil
}

func guidedPlanningFamilyState(family GuidedPlanningFamilySection) string {
	switch {
	case family.Count > 0 && family.Promoted == family.Count:
		return "promoted"
	case family.AwaitingPromotion > 0:
		return "approved"
	case family.AwaitingReview > 0:
		return "admitted"
	default:
		return "none"
	}
}

func (s *Service) guidedDelivery(ctx context.Context, workspace workflowstore.FeatureWorkspace) (GuidedDeliverySection, error) {
	owner, err := appworkflow.NewService(s.store)
	if err != nil {
		return GuidedDeliverySection{}, err
	}
	state, err := owner.ReadWorkspaceDeliveryState(ctx, workspace.WorkspaceID)
	if err != nil {
		return GuidedDeliverySection{}, err
	}
	return GuidedDeliverySection{FrontierCount: state.FrontierCount, SelectionState: state.SelectionState, PackageState: state.PackageState, RunState: state.RunState, AuditState: state.AuditState, RemediationState: state.RemediationState}, nil
}

func (s *Service) guidedPrototype(ctx context.Context, workspace workflowstore.FeatureWorkspace) (GuidedPrototypeSection, error) {
	result := GuidedPrototypeSection{RunState: "none", CleanupState: "none", QAState: "none", EvidenceState: "none"}
	runs, err := s.store.ListPrototypeRunsByWorkspace(ctx, workspace.ID)
	if err != nil {
		return result, err
	}
	if len(runs) > 0 {
		result.RunState = runs[0].LifecycleState
		result.CleanupState = runs[0].CleanupStatus
	}
	packets, err := s.store.ListPrototypeQAPacketsByWorkspace(ctx, workspace.ID)
	if err != nil {
		return result, err
	}
	if len(packets) > 0 {
		result.QAState = "prepared"
	}
	for _, packet := range packets {
		if packet.Status == "admitted" {
			result.QAState = "admitted"
			result.EvidenceState = "admitted"
			break
		}
	}
	return result, nil
}

// ExecuteGuidedAction rechecks the current projection immediately before any
// mutation. Handoffs read the existing owner context and return a distinct
// resume projection; they do not create a second lifecycle or accept internal
// identities from the guided request.
func (s *Service) ExecuteGuidedAction(ctx context.Context, input GuidedActionInput) (GuidedActionResult, error) {
	if strings.TrimSpace(input.WorkspaceID) == "" || input.ExpectedVersion < 1 || strings.TrimSpace(input.Action) == "" {
		return GuidedActionResult{}, ErrGuidedActionBlocked
	}
	before, err := s.ReadGuidedProjection(ctx, input.WorkspaceID)
	if err != nil {
		return GuidedActionResult{}, err
	}
	if before.Workspace.Version != input.ExpectedVersion {
		return GuidedActionResult{}, ErrVersionConflict
	}
	requested := GuidedFeatureAction(input.Action)
	// The guided boundary executes exactly the presently enabled primary
	// action. Remaining advertised actions are display surface for the
	// operator; attempting them is rejected so the journey cannot advance out
	// of sequence.
	if before.PrimaryAction.Action != requested || !before.PrimaryAction.Enabled {
		return GuidedActionResult{}, ErrGuidedActionBlocked
	}
	if before.PrimaryAction.RequiresConfirmation && !input.Confirmation {
		return GuidedActionResult{}, ErrFeatureCompletionConfirmation
	}
	switch requested {
	case GuidedActionContinueDiscovery:
		_, _, err = s.RecordDiscoveryDestinationAssessment(ctx, RecordDiscoveryDestinationAssessmentInput{WorkspaceID: input.WorkspaceID, ExpectedVersion: input.ExpectedVersion, CreatedIdentity: "guided-operator"})
	case GuidedActionCloseDiscovery:
		assessment, assessmentErr := s.AssessDiscoveryDestination(ctx, input.WorkspaceID)
		if assessmentErr != nil {
			return GuidedActionResult{}, assessmentErr
		}
		if assessment.Revision == nil {
			return GuidedActionResult{}, ErrDiscoveryNotStarted
		}
		destination := assessment.Destination
		if input.Destination != "" {
			destination = input.Destination
		}
		_, _, err = s.CloseFeatureDiscovery(ctx, CloseFeatureDiscoveryInput{WorkspaceID: input.WorkspaceID, ExpectedVersion: input.ExpectedVersion, ExpectedRevisionID: assessment.Revision.DiscoveryRevisionID, Destination: destination, CreatedIdentity: "guided-operator"})
	case GuidedActionCompleteFeature:
		_, err = s.Complete(ctx, CompletionInput{WorkspaceID: input.WorkspaceID, ExpectedVersion: input.ExpectedVersion, OperatorConfirmed: input.Confirmation})
	case GuidedActionLegacyRecovery:
		_, _, err = s.AdoptFeatureDiscoveryLifecycle(ctx, AdoptFeatureDiscoveryLifecycleInput{WorkspaceID: input.WorkspaceID, ExpectedVersion: input.ExpectedVersion, OperatorIdentity: "guided-operator"})
	case GuidedActionAuthorRequirements, GuidedActionAuthorSharedDesign, GuidedActionAuthorDeliveryTicket, GuidedActionContinueEstablishedRoute, GuidedActionReviewPlanningCandidate:
		handoff, handoffErr := s.guidedHandoff(ctx, input.WorkspaceID, requested, before)
		if handoffErr != nil {
			return GuidedActionResult{}, handoffErr
		}
		before.Handoff = &handoff
		return GuidedActionResult{Projection: before, Handoff: &handoff}, nil
	case GuidedActionApprovePlanningCandidate:
		_, err = s.guidedApproveCurrentCandidate(ctx, input, DiscoveryDestination(before.Discovery.Destination))
	case GuidedActionPromotePlanningCandidate:
		_, err = s.guidedPromoteCurrentCandidate(ctx, input, DiscoveryDestination(before.Discovery.Destination))
	default:
		return GuidedActionResult{}, ErrGuidedActionBlocked
	}
	if err != nil {
		return GuidedActionResult{}, err
	}
	after, err := s.ReadGuidedProjection(ctx, input.WorkspaceID)
	return GuidedActionResult{Projection: after}, err
}

func (s *Service) guidedHandoff(ctx context.Context, workspaceID string, action GuidedFeatureAction, projection GuidedFeatureProjection) (GuidedHandoff, error) {
	handoff := GuidedHandoff{Role: string(action), ResumeRoute: "/feature-workspaces/" + workspaceID + "/guided", Context: map[string]string{"destination": projection.Discovery.Destination, "currentness": projection.Currentness.Readiness}}
	switch action {
	case GuidedActionAuthorRequirements, GuidedActionAuthorSharedDesign:
		// Compose the existing planner authoring and auditor review envelopes with
		// workspace context only. The guided handoff prepares the owner surface;
		// it never authors, reviews, approves, or promotes a candidate.
		planner, err := s.ComposePlannerAuthoring(ctx, PlannerAuthoringInput{WorkspaceID: workspaceID})
		if err != nil {
			return GuidedHandoff{}, err
		}
		review, err := s.ReadAuditorReview(ctx, AuditorReviewInput{WorkspaceID: workspaceID})
		if err != nil {
			return GuidedHandoff{}, err
		}
		workspace, err := s.store.GetFeatureWorkspaceByWorkspaceID(ctx, workspaceID)
		if err != nil {
			return GuidedHandoff{}, err
		}
		candidateState, err := s.guidedFamilyCandidateState(ctx, workspace, DiscoveryDestination(projection.Discovery.Destination), planner.Authority)
		if err != nil {
			return GuidedHandoff{}, err
		}
		handoff.Context["owner"] = "planner_authoring_and_review"
		handoff.Context["preparationStatus"] = "ready"
		handoff.Context["sourceMemberCount"] = stringInt(len(planner.Members))
		handoff.Context["authorityLayerCount"] = stringInt(len(review.Authority))
		handoff.Context["candidateState"] = candidateState
		handoff.Context["externalRoleWork"] = "not_performed"
		handoff.Summary = "Planner authoring and review are prepared through their existing owners. External role work remains outside this handoff; approve and promote explicitly before resuming the guided workspace."
	case GuidedActionReviewPlanningCandidate:
		// Review is a read-only auditor preparation step. It composes the same
		// owner envelope the planner review path uses and never writes review or
		// approval state; approval is a separate explicit server-side action.
		review, err := s.ReadAuditorReview(ctx, AuditorReviewInput{WorkspaceID: workspaceID})
		if err != nil {
			return GuidedHandoff{}, err
		}
		workspace, err := s.store.GetFeatureWorkspaceByWorkspaceID(ctx, workspaceID)
		if err != nil {
			return GuidedHandoff{}, err
		}
		candidateState, err := s.guidedFamilyCandidateState(ctx, workspace, DiscoveryDestination(projection.Discovery.Destination), review.Authority)
		if err != nil {
			return GuidedHandoff{}, err
		}
		handoff.Context["owner"] = "auditor_review"
		handoff.Context["preparationStatus"] = "ready"
		handoff.Context["candidateState"] = candidateState
		handoff.Context["sourceMemberCount"] = stringInt(len(review.Members))
		handoff.Context["authorityLayerCount"] = stringInt(len(review.Authority))
		handoff.Context["externalRoleWork"] = "not_performed"
		handoff.Summary = "The auditor review surface is prepared through its existing owner envelope. Review the current planning candidate, then explicitly approve and promote it before resuming the guided workspace."
	case GuidedActionAuthorDeliveryTicket:
		owner, err := apptickets.NewService(s.store)
		if err != nil {
			return GuidedHandoff{}, err
		}
		frontier, err := owner.ListFrontier(ctx, workspaceID)
		if err != nil {
			return GuidedHandoff{}, err
		}
		handoff.Context["owner"] = "delivery_ticket_frontier"
		handoff.Context["preparationStatus"] = "frontier_identified"
		handoff.Context["frontierCount"] = stringInt(len(frontier.Entries))
		handoff.Context["selectionState"] = projection.Delivery.SelectionState
		handoff.Context["externalRoleWork"] = "not_performed"
		handoff.Summary = "The existing Delivery Ticket frontier is prepared for the bounded role operation. No ticket authoring or selection is performed by this handoff; return here for a fresh currentness check."
	case GuidedActionContinueEstablishedRoute:
		workspace, err := s.store.GetFeatureWorkspaceByWorkspaceID(ctx, workspaceID)
		if err != nil {
			return GuidedHandoff{}, err
		}
		routes, err := s.store.ListFeatureWorkspaceRouteStates(ctx, workspace.ID)
		if err != nil {
			return GuidedHandoff{}, err
		}
		routeState := "not_recorded"
		if len(routes) > 0 {
			routeState = routes[len(routes)-1].State
		}
		handoff.Context["owner"] = "established_route"
		handoff.Context["preparationStatus"] = "route_identified"
		handoff.Context["routeState"] = routeState
		handoff.Context["routeCount"] = stringInt(len(routes))
		handoff.Context["externalRoleWork"] = "not_performed"
		handoff.Summary = "The current established route operation is identified through its existing owner read surface. No route work is performed by this handoff; return here for a fresh currentness check."
	}
	return handoff, nil
}

// guidedApprovalEvidence is the server-side confirmation evidence recorded when
// the guided boundary approves the current planning candidate. The operator's
// confirmation of the guided approval action is enforced by the action gate.
const guidedApprovalEvidence = "guided-operator-approval"

// guidedFamilyCandidateState resolves the semantic candidate state for the
// family currently in flight, matching the guided decision's family priority.
func (s *Service) guidedFamilyCandidateState(ctx context.Context, workspace workflowstore.FeatureWorkspace, destination DiscoveryDestination, layers []workflowstore.FeatureWorkspaceAuthorityLayer) (string, error) {
	candidates, err := s.store.ListPlanningCandidatesByWorkspace(ctx, workspace.ID)
	if err != nil {
		return "", err
	}
	for _, family := range guidedCandidateFamiliesForDestination(destination) {
		for _, candidate := range candidates {
			promoted := candidatePromotedInCurrentAuthorityLayers(candidate, layers)
			historical := s.planningCandidateHistorical(ctx, workspace, candidate)
			if candidate.Family != family || (historical && !(promoted && workspace.CurrentDiscoveryClosurePacketRowID.Valid && candidate.DiscoveryClosurePacketRowID == workspace.CurrentDiscoveryClosurePacketRowID.Int64)) {
				continue
			}
			approvals, approvalErr := s.store.ListPlanningCandidateApprovalsByCandidate(ctx, candidate.ID)
			if approvalErr != nil {
				return "", approvalErr
			}
			state := "admitted"
			if promoted {
				state = "promoted"
			} else if len(approvals) > 0 {
				state = "approved_awaiting_promotion"
			}
			return state, nil
		}
	}
	return "not_admitted", nil
}

// guidedCurrentPlanningCandidate resolves, without any client-supplied
// identity, the current-basis planning candidate for the family the closed
// destination requires. wantApproved selects approved (awaiting promotion)
// versus admitted (awaiting review and approval) candidates.
func (s *Service) guidedCurrentPlanningCandidate(ctx context.Context, workspaceID string, destination DiscoveryDestination, wantApproved bool) (workflowstore.PlanningCandidate, error) {
	workspace, err := s.store.GetFeatureWorkspaceByWorkspaceID(ctx, strings.TrimSpace(workspaceID))
	if err != nil {
		return workflowstore.PlanningCandidate{}, err
	}
	authority, err := s.ReadAuthority(ctx, strings.TrimSpace(workspaceID))
	if err != nil {
		return workflowstore.PlanningCandidate{}, err
	}
	candidates, err := s.store.ListPlanningCandidatesByWorkspace(ctx, workspace.ID)
	if err != nil {
		return workflowstore.PlanningCandidate{}, err
	}
	for _, family := range guidedCandidateFamiliesForDestination(destination) {
		for _, candidate := range candidates {
			if candidate.Family != family || s.planningCandidateHistorical(ctx, workspace, candidate) || candidatePromotedInCurrentAuthority(candidate, authority) {
				continue
			}
			approvals, approvalErr := s.store.ListPlanningCandidateApprovalsByCandidate(ctx, candidate.ID)
			if approvalErr != nil {
				return workflowstore.PlanningCandidate{}, approvalErr
			}
			if (len(approvals) > 0) != wantApproved {
				continue
			}
			return candidate, nil
		}
	}
	return workflowstore.PlanningCandidate{}, ErrGuidedActionBlocked
}

// guidedApproveCurrentCandidate approves the current appropriate planning
// candidate server-side: the candidate identity, artifact bytes, and basis are
// all resolved from the workspace rather than accepted from the client.
func (s *Service) guidedApproveCurrentCandidate(ctx context.Context, input GuidedActionInput, destination DiscoveryDestination) (CandidateApprovalResult, error) {
	candidate, err := s.guidedCurrentPlanningCandidate(ctx, input.WorkspaceID, destination, false)
	if err != nil {
		return CandidateApprovalResult{}, err
	}
	bytes, err := s.store.ReadPlanningCandidateBytes(ctx, candidate.CandidateID, int(candidate.ArtifactSizeBytes))
	if err != nil {
		return CandidateApprovalResult{}, ErrCandidateBytesMismatch
	}
	workspace, err := s.store.GetFeatureWorkspaceByWorkspaceID(ctx, strings.TrimSpace(input.WorkspaceID))
	if err != nil {
		return CandidateApprovalResult{}, err
	}
	return s.ApprovePlanningCandidate(ctx, CandidateApprovalInput{
		CandidateID: candidate.CandidateID, ExpectedSHA256: candidate.ArtifactSha256, ExpectedSizeBytes: candidate.ArtifactSizeBytes,
		Bytes: bytes, ExpectedVersion: input.ExpectedVersion,
		ExpectedClosurePacketRowID:     workspace.CurrentDiscoveryClosurePacketRowID,
		ExpectedAuthorityRevisionRowID: workspace.CurrentAuthorityRevisionRowID,
		OperatorConfirmationEvidence:   guidedApprovalEvidence,
		CreatedIdentity:                "guided-operator",
	})
}

// guidedPromoteCurrentCandidate promotes the current approved planning
// candidate server-side. The candidate and its approval are resolved from the
// workspace; the client never supplies their identities.
func (s *Service) guidedPromoteCurrentCandidate(ctx context.Context, input GuidedActionInput, destination DiscoveryDestination) (CandidatePromotionResult, error) {
	candidate, err := s.guidedCurrentPlanningCandidate(ctx, input.WorkspaceID, destination, true)
	if err != nil {
		return CandidatePromotionResult{}, err
	}
	approvals, err := s.store.ListPlanningCandidateApprovalsByCandidate(ctx, candidate.ID)
	if err != nil || len(approvals) == 0 {
		return CandidatePromotionResult{}, ErrGuidedActionBlocked
	}
	return s.PromoteApprovedPlanningCandidate(ctx, CandidatePromotionInput{
		CandidateID: candidate.CandidateID, ApprovalID: approvals[0].ApprovalID,
		ExpectedVersion: input.ExpectedVersion, CreatedIdentity: "guided-operator",
	})
}

func stringInt(value int) string { return strconvItoa(value) }
func strconvItoa(value int) string {
	if value == 0 {
		return "0"
	}
	return fmtInt(value)
}
func fmtInt(value int) string {
	negative := value < 0
	if negative {
		value = -value
	}
	buf := [20]byte{}
	i := len(buf)
	for value > 0 {
		i--
		buf[i] = byte('0' + value%10)
		value /= 10
	}
	if negative {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
