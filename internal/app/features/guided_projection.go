package features

import (
	"context"
	"errors"
	"strings"

	apptickets "relay/internal/app/tickets"
	"relay/internal/guidedapp"
	workflowstore "relay/internal/store/workflow"
)

var (
	ErrGuidedActionBlocked           = errors.New("guided action is not the presently enabled primary action")
	ErrGuidedPackageOwnerUnavailable = errors.New("guided package owner is unavailable")
	ErrGuidedAuditOwnerUnavailable   = errors.New("guided audit owner is unavailable")
)

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
	State, Destination, Rationale, Continuation, Currentness, Basis, ReopenState                             string
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

// GuidedFrontierEntry is the delivery-owner semantic frontier identity. It
// carries only the public ticket identity and revision number resolved by the
// delivery owner; no row identifiers or digests.
type GuidedFrontierEntry struct {
	TicketID         string
	RevisionNumber   int64
	ExternalPriority int64
	RepoTarget       string
	Branch           string
}

// GuidedDeliverySection is the delivery-owner semantic read state consumed by
// the guided decision. Frontier, selection, package, Run, audit, and
// remediation states are composed from the tickets, packages, and audits
// owners; the Feature layer never re-derives lifecycle strings from rows.
type GuidedDeliverySection struct {
	Frontier         []GuidedFrontierEntry
	SelectionState   string // none | active | consumed | superseded
	PackageState     string // none | prepared | approved
	PackageID        string
	RunState         string // none | created | setup_ready | executing | validating | audit_ready | needs_revision | completed | ...
	RunID            string
	AuditState       string // none | awaiting_audit | packet_recorded | decision_recorded
	AuditPacketID    string
	RemediationState string // none | open | reopened
	Diagnostics      []string
}

type GuidedPrototypeSection struct {
	RunState, CleanupState, QAState, EvidenceState, ProcessOutcome string
	RunID                                                          string
	Diagnostics                                                    []string
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
	Stale, Historical, Discovery, Delivery, Prototype []string
}

// GuidedHandoff transfers the actual owner-composed operation surface for the
// bounded role operation instead of generic counts or routes. Transfer carries
// only public owner identities and semantic state resolved server-side.
type GuidedHandoff struct {
	Role, Summary, ResumeRoute string
	Context                    map[string]string
	Transfer                   *GuidedOperationTransfer
}

type GuidedOperationTransfer struct {
	Frontier        []GuidedFrontierEntry
	Members         []string // planning closure member semantic roles
	AuthorityLayers []string // planning authority layer kinds
	Ticket          *GuidedTicketTransfer
	Package         *GuidedPackageTransfer
	Run             *GuidedRunTransfer
	Audit           *GuidedAuditTransfer
	Remediation     *GuidedRemediationTransfer
	Prototype       *GuidedPrototypeTransfer
}

type GuidedTicketTransfer struct {
	TicketID       string
	RevisionNumber int64
	Readiness      []string
	DesignBrief    string
}
type GuidedPackageTransfer struct {
	PackageID string
	State     string
}
type GuidedRunTransfer struct {
	RunID, Status, RepoTarget, Branch, BaseCommit, PackageID string
}
type GuidedAuditTransfer struct {
	RunID, RunStatus, AuditState, AuditPacketID, AuditedCommit string
}
type GuidedRemediationTransfer struct {
	State   string
	SeedIDs []string
}
type GuidedPrototypeTransfer struct {
	RunID, RunState, ProcessOutcome string
	Cleanup                         []GuidedCleanupTransfer
	QAPackets                       []GuidedQAPacketTransfer
}
type GuidedCleanupTransfer struct {
	Kind, Status string
}
type GuidedQAPacketTransfer struct {
	PacketID string
	Status   string
	Evidence []string
}

type GuidedActionInput struct {
	WorkspaceID, Action string
	ExpectedVersion     int64
	Confirmation        bool
	Destination         DiscoveryDestination
	// ReopenDiscovery content: the operator-authored replacement integrated
	// revision. These are user content, not internal identities; the current
	// closure packet basis is resolved server-side and the replacement digest
	// is derived from the submitted markdown by the server. The client never
	// supplies a SHA-256 digest for a guided action.
	Cause        string
	Markdown     []byte
	Continuation string
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
		AuthorityLayers: layers, Planning: planning, Delivery: delivery, Prototype: prototype,
		Continuation: assessment.Continuation, Blockers: assessment.Blockers,
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
		Discovery:   GuidedDiscoverySection{State: string(assessment.State), Destination: string(assessment.Destination), Rationale: assessment.Rationale, Continuation: assessment.Continuation, Currentness: string(assessment.Currentness), Basis: currentness.Basis, ReopenState: guidedReopenState(assessment), HasCurrentRevision: assessment.Revision != nil, Blockers: append([]string(nil), assessment.Blockers...), RestorationActions: append([]string(nil), assessment.RestorationActions...), PendingIntegrations: append([]string(nil), assessment.PendingIntegrations...), ActiveOperations: append([]string(nil), assessment.ActiveOperations...), RouteMaterialOpen: append([]string(nil), assessment.RouteMaterialOpen...), RequiredEvidence: append([]string(nil), assessment.RequiredEvidence...)},
		Currentness: GuidedCurrentnessSection{Readiness: string(currentness.Readiness), Owner: currentness.StaleOwner, BlockedOperation: currentness.BlockedOperation, Effect: currentness.Effect, RecoveryCategory: currentness.RecoveryCategory},
		Authority:   GuidedAuthoritySection{CurrentRevisionNumber: currentRevisionNumber, Layers: append([]string(nil), layers...)},
		Planning:    planning, Delivery: delivery, Prototype: prototype,
		Completion:       GuidedCompletionSection{Gates: gates, Ready: GuidedCompletionReady(gates), Recorded: completion.CurrentDecision != nil},
		Recovery:         recovery,
		Diagnostics:      GuidedDiagnosticsSection{Stale: nonEmpty(currentness.StaleOwner, currentness.BlockedOperation, currentness.Effect), Historical: guidedHistoricalDiagnostics(currentness, assessment), Discovery: append([]string(nil), assessment.Blockers...), Delivery: append([]string(nil), delivery.Diagnostics...), Prototype: append([]string(nil), prototype.Diagnostics...)},
		AvailableActions: append([]GuidedFeatureActionAvailability(nil), decision.AvailableActions...),
		PrimaryAction:    primary,
	}
}

// guidedReopenState exposes the discovery reopen/reclosure basis: none for a
// workspace whose current revision was not produced by reopen, reopened while
// the replacement revision is open, and reclosed once the replacement revision
// is closed again.
func guidedReopenState(assessment DiscoveryAssessment) string {
	if assessment.Revision == nil || !assessment.Revision.PredecessorRevisionRowID.Valid {
		return "none"
	}
	if assessment.State == DiscoveryStateClosed {
		return "reclosed"
	}
	return "reopened"
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

// guidedDelivery composes the tickets, packages, and audits-owner semantic
// reads into the delivery projection. The Feature layer resolves identities
// from the owner reads and never derives lifecycle strings from rows itself.
func (s *Service) guidedDelivery(ctx context.Context, workspace workflowstore.FeatureWorkspace) (GuidedDeliverySection, error) {
	result := GuidedDeliverySection{SelectionState: "none", PackageState: "none", RunState: "none", AuditState: "none", RemediationState: "none"}
	tickets, err := apptickets.NewService(s.store)
	if err != nil {
		return result, err
	}
	frontier, err := tickets.ListFrontier(ctx, workspace.WorkspaceID)
	if err != nil {
		return result, err
	}
	for _, entry := range frontier.Entries {
		result.Frontier = append(result.Frontier, GuidedFrontierEntry{TicketID: entry.TicketID, RevisionNumber: entry.RevisionNumber, ExternalPriority: entry.ExternalPriority, RepoTarget: entry.RepoTarget, Branch: entry.Branch})
	}
	selection, err := tickets.ReadWorkspaceSelection(ctx, workspace.WorkspaceID)
	if err != nil {
		return result, err
	}
	result.SelectionState = selection.State
	if selection.State != "none" {
		if s.guidedPackages == nil {
			return result, ErrGuidedPackageOwnerUnavailable
		}
		packageState, err := s.guidedPackages.ReadWorkspacePackageState(ctx, workspace.WorkspaceID)
		if err != nil {
			return result, err
		}
		result.PackageState = packageState.State
		result.PackageID = packageState.PackageID
		result.RunState = packageState.RunStatus
		result.RunID = packageState.RunID
		if packageState.RunID != "" {
			if s.guidedAudit == nil {
				return result, ErrGuidedAuditOwnerUnavailable
			}
			auditState, err := s.guidedAudit.ReadRunAuditState(ctx, packageState.RunID)
			if err != nil {
				return result, err
			}
			result.AuditState = auditState.State
			result.AuditPacketID = auditState.AuditPacketID
			result.Diagnostics = append(result.Diagnostics, auditState.Diagnostics...)
		}
	}
	if s.guidedAudit == nil {
		return result, ErrGuidedAuditOwnerUnavailable
	}
	remediation, err := s.guidedAudit.ReadWorkspaceRemediationState(ctx, workspace.WorkspaceID)
	if err != nil {
		return result, err
	}
	result.RemediationState = remediation.State
	if remediation.State == "open" {
		result.Diagnostics = append(result.Diagnostics, "remediation_open")
	}
	if result.RunState == "needs_revision" {
		result.Diagnostics = append(result.Diagnostics, "run_needs_revision")
	}
	return result, nil
}

// guidedPrototype composes the Feature-owned prototype execution, cleanup, and
// QA semantic read for the current prototype Run. The Feature owner resolves
// the current Run and derives cleanup/QA states; this section only composes.
func (s *Service) guidedPrototype(ctx context.Context, workspace workflowstore.FeatureWorkspace) (GuidedPrototypeSection, error) {
	return s.ReadCurrentPrototypeState(ctx, workspace.WorkspaceID)
}

func guidedPrototypeDiagnostics(prototype GuidedPrototypeSection) []string {
	var diagnostics []string
	switch {
	case prototype.RunState == "proposed":
		diagnostics = append(diagnostics, "execution_ready_to_launch")
	case prototype.RunState == "cleanup_required" || prototype.CleanupState == "pending":
		diagnostics = append(diagnostics, "cleanup_pending")
	case prototype.RunState == "closed" && prototype.QAState == "prepared":
		diagnostics = append(diagnostics, "qa_evidence_pending")
	}
	if prototype.ProcessOutcome != "" {
		diagnostics = append(diagnostics, "process_outcome:"+prototype.ProcessOutcome)
	}
	return diagnostics
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
	case GuidedActionReopenDiscovery:
		_, _, err = s.guidedReopenDiscovery(ctx, input)
	case GuidedActionLegacyRecovery:
		_, _, err = s.AdoptFeatureDiscoveryLifecycle(ctx, AdoptFeatureDiscoveryLifecycleInput{WorkspaceID: input.WorkspaceID, ExpectedVersion: input.ExpectedVersion, OperatorIdentity: "guided-operator"})
	case GuidedActionAuthorRequirements, GuidedActionAuthorSharedDesign, GuidedActionAuthorDeliveryTicket, GuidedActionContinueEstablishedRoute, GuidedActionReviewPlanningCandidate,
		GuidedActionPreparePackage, GuidedActionLaunchRun, GuidedActionPrepareAudit, GuidedActionRecordAuditDecision,
		GuidedActionRemediate, GuidedActionPrototypeExecute, GuidedActionPrototypeCleanup, GuidedActionPrototypeQA:
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
	case GuidedActionSelectDeliveryTicket:
		err = s.guidedSelectFrontierTicket(ctx, input)
	case GuidedActionApprovePackage:
		err = s.guidedApproveCurrentPackage(ctx, input)
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
	case GuidedActionAuthorRequirements, GuidedActionAuthorSharedDesign, GuidedActionAuthorDeliveryTicket, GuidedActionContinueEstablishedRoute:
		// Compose the existing planner authoring and auditor review envelopes with
		// workspace context only. The guided handoff prepares the owner surface;
		// it never authors, reviews, approves, or promotes a candidate or ticket.
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
		handoff.Context["candidateState"] = candidateState
		handoff.Context["continuation"] = projection.Discovery.Continuation
		handoff.Transfer = &GuidedOperationTransfer{Members: closureMemberRoles(planner.Members), AuthorityLayers: authorityLayerKinds(review.Authority)}
		handoff.Summary = "Planner authoring and review are prepared through their existing owners. Author the current planning artifact, complete its read-only review, then explicitly approve and promote it before resuming the guided workspace."
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
		handoff.Context["candidateState"] = candidateState
		handoff.Transfer = &GuidedOperationTransfer{Members: closureMemberRoles(review.Members), AuthorityLayers: authorityLayerKinds(review.Authority)}
		handoff.Summary = "The auditor review surface is prepared through its existing owner envelope. Review the current planning candidate, then explicitly approve and promote it before resuming the guided workspace."
	case GuidedActionPreparePackage:
		ticket, err := s.guidedSelectedTicketTransfer(ctx, workspaceID)
		if err != nil {
			return GuidedHandoff{}, err
		}
		handoff.Context["owner"] = "execution_package_preparation"
		handoff.Transfer = &GuidedOperationTransfer{Ticket: ticket}
		handoff.Summary = "The selected Delivery Ticket is identified through the delivery owner. Prepare the execution package with the ticket design brief, then return here to approve it server-side."
	case GuidedActionLaunchRun:
		if s.guidedPackages == nil {
			return GuidedHandoff{}, ErrGuidedPackageOwnerUnavailable
		}
		packageState, err := s.guidedPackages.ReadWorkspacePackageState(ctx, workspaceID)
		if err != nil {
			return GuidedHandoff{}, err
		}
		if packageState.RunID == "" {
			return GuidedHandoff{}, ErrGuidedActionBlocked
		}
		transfer := &GuidedRunTransfer{RunID: packageState.RunID, Status: packageState.RunStatus, RepoTarget: packageState.RunRepoTarget, Branch: packageState.RunBranch, BaseCommit: packageState.RunBaseCommit, PackageID: packageState.PackageID}
		handoff.Context["owner"] = "package_run"
		handoff.Transfer = &GuidedOperationTransfer{Run: transfer}
		handoff.Summary = "The package Run is identified through its existing owner. Continue its execution through the Run owner, then return here for a fresh currentness check."
	case GuidedActionPrepareAudit, GuidedActionRecordAuditDecision:
		if projection.Delivery.RunID == "" {
			return GuidedHandoff{}, ErrGuidedActionBlocked
		}
		if s.guidedAudit == nil {
			return GuidedHandoff{}, ErrGuidedAuditOwnerUnavailable
		}
		auditState, err := s.guidedAudit.ReadRunAuditState(ctx, projection.Delivery.RunID)
		if err != nil {
			return GuidedHandoff{}, err
		}
		transfer := &GuidedAuditTransfer{RunID: auditState.RunID, RunStatus: auditState.RunStatus, AuditState: auditState.State, AuditPacketID: auditState.AuditPacketID, AuditedCommit: auditState.AuditedCommit}
		handoff.Context["owner"] = "workflow_audit"
		handoff.Transfer = &GuidedOperationTransfer{Audit: transfer}
		handoff.Summary = "The workflow audit state is identified through the audit owner. Complete the audit preparation or decision through the audit owner, then return here."
	case GuidedActionRemediate:
		if s.guidedAudit == nil {
			return GuidedHandoff{}, ErrGuidedAuditOwnerUnavailable
		}
		remediation, err := s.guidedAudit.ReadWorkspaceRemediationState(ctx, workspaceID)
		if err != nil {
			return GuidedHandoff{}, err
		}
		transfer := &GuidedRemediationTransfer{State: remediation.State, SeedIDs: append([]string(nil), remediation.SeedIDs...)}
		handoff.Context["owner"] = "audit_remediation"
		handoff.Transfer = &GuidedOperationTransfer{Remediation: transfer}
		handoff.Summary = "The audit remediation seed is identified through the audit owner. Publish the replacement Delivery Ticket revision bound to that seed, then return here to resume selection."
	case GuidedActionPrototypeExecute, GuidedActionPrototypeCleanup, GuidedActionPrototypeQA:
		transfer, err := s.guidedPrototypeTransfer(ctx, workspaceID, projection.Prototype.RunID)
		if err != nil {
			return GuidedHandoff{}, err
		}
		handoff.Context["owner"] = "prototype_execution"
		handoff.Transfer = &GuidedOperationTransfer{Prototype: transfer}
		handoff.Summary = "The prototype Run is identified through the prototype owner. Complete its execution, cleanup, or QA through the prototype owner, then return here."
	}
	return handoff, nil
}

func closureMemberRoles(members []workflowstore.DiscoveryClosurePacketMember) []string {
	roles := make([]string, 0, len(members))
	for _, member := range members {
		roles = append(roles, member.SemanticRole)
	}
	return roles
}

func authorityLayerKinds(layers []workflowstore.FeatureWorkspaceAuthorityLayer) []string {
	kinds := make([]string, 0, len(layers))
	for _, layer := range layers {
		kinds = append(kinds, layer.LayerKind)
	}
	return kinds
}

// currentDiscoveryClosureContent resolves the current closure packet server-side
// and verifies it through the discovery owner read.
func (s *Service) currentDiscoveryClosureContent(ctx context.Context, workspaceID string) (DiscoveryPacketContent, error) {
	workspace, err := s.store.GetFeatureWorkspaceByWorkspaceID(ctx, workspaceID)
	if err != nil {
		return DiscoveryPacketContent{}, err
	}
	if !workspace.CurrentDiscoveryClosurePacketRowID.Valid {
		return DiscoveryPacketContent{}, ErrDiscoveryNotClosed
	}
	packet, err := s.store.GetDiscoveryClosurePacketByRowID(ctx, workspace.CurrentDiscoveryClosurePacketRowID.Int64)
	if err != nil {
		return DiscoveryPacketContent{}, err
	}
	return s.ReadDiscoveryClosurePacket(ctx, workspaceID, packet.ClosurePacketID)
}

// guidedReopenDiscovery reopens the closed discovery through the Feature reopen
// owner. The current closure packet identity and expected basis are resolved
// server-side from the workspace; only the operator-authored replacement
// content and confirmation are accepted from the guided request.
func (s *Service) guidedReopenDiscovery(ctx context.Context, input GuidedActionInput) (workflowstore.IntegratedDiscoveryRevision, workflowstore.FeatureWorkspace, error) {
	content, err := s.currentDiscoveryClosureContent(ctx, strings.TrimSpace(input.WorkspaceID))
	if err != nil {
		return workflowstore.IntegratedDiscoveryRevision{}, workflowstore.FeatureWorkspace{}, err
	}
	return s.ReopenFeatureDiscovery(ctx, ReopenFeatureDiscoveryInput{
		WorkspaceID:       input.WorkspaceID,
		ExpectedVersion:   input.ExpectedVersion,
		ExpectedPacketID:  content.Packet.ClosurePacketID,
		OperatorConfirmed: input.Confirmation,
		Cause:             input.Cause,
		CreatedIdentity:   "guided-operator",
		SHA256:            digest(input.Markdown),
		Markdown:          input.Markdown,
		Destination:       input.Destination,
		Continuation:      input.Continuation,
	})
}

// guidedSelectedTicketTransfer resolves the currently selected Delivery Ticket
// through the delivery owner and transfers its public identity and readiness.
func (s *Service) guidedSelectedTicketTransfer(ctx context.Context, workspaceID string) (*GuidedTicketTransfer, error) {
	owner, err := apptickets.NewService(s.store)
	if err != nil {
		return nil, err
	}
	selection, err := owner.ReadWorkspaceSelection(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	if selection.TicketID == "" {
		return nil, ErrGuidedActionBlocked
	}
	detail, err := owner.Read(ctx, selection.TicketID)
	if err != nil {
		return nil, err
	}
	transfer := &GuidedTicketTransfer{TicketID: detail.Ticket.TicketID, RevisionNumber: detail.Revision.RevisionNumber, Readiness: append([]string(nil), detail.Readiness.Reasons...), DesignBrief: detail.Canonical.RelativePath}
	return transfer, nil
}

// guidedPrototypeTransfer composes the prototype owner view for the current
// prototype Run into the operation transfer surface.
func (s *Service) guidedPrototypeTransfer(ctx context.Context, workspaceID, runID string) (*GuidedPrototypeTransfer, error) {
	if runID == "" {
		return nil, ErrGuidedActionBlocked
	}
	view, err := s.ReadPrototypeEvidenceForWayfinder(ctx, workspaceID, runID)
	if err != nil {
		return nil, err
	}
	transfer := &GuidedPrototypeTransfer{RunID: view.RunID, RunState: view.RunState, ProcessOutcome: view.ProcessOutcome, Cleanup: make([]GuidedCleanupTransfer, 0, len(view.Cleanup)), QAPackets: make([]GuidedQAPacketTransfer, 0, len(view.QAPackets))}
	for _, obligation := range view.Cleanup {
		transfer.Cleanup = append(transfer.Cleanup, GuidedCleanupTransfer{Kind: obligation.ObligationKind, Status: obligation.Status})
	}
	for _, packet := range view.QAPackets {
		qapacket := GuidedQAPacketTransfer{PacketID: packet.Packet.QAPacketID, Status: packet.Packet.Status, Evidence: make([]string, 0, len(packet.Evidence))}
		for _, evidence := range packet.Evidence {
			qapacket.Evidence = append(qapacket.Evidence, evidence.SemanticRole)
		}
		transfer.QAPackets = append(transfer.QAPackets, qapacket)
	}
	return transfer, nil
}

// guidedSelectFrontierTicket resolves the current frontier head server-side and
// delegates the exact selection to the delivery owner. No ticket or revision
// identity is accepted from the client.
func (s *Service) guidedSelectFrontierTicket(ctx context.Context, input GuidedActionInput) error {
	owner, err := apptickets.NewService(s.store)
	if err != nil {
		return err
	}
	frontier, err := owner.ListFrontier(ctx, input.WorkspaceID)
	if err != nil {
		return err
	}
	if len(frontier.Entries) == 0 {
		return ErrGuidedActionBlocked
	}
	head := frontier.Entries[0]
	_, err = owner.Select(ctx, apptickets.SelectInput{
		WorkspaceID: input.WorkspaceID, TicketID: head.TicketID, RevisionRowID: head.RevisionRowID,
		Rationale: guidedApprovalEvidence,
	})
	return err
}

// guidedApproveCurrentPackage delegates the current prepared execution package
// approval to the package owner, which resolves the exact package identity and
// digest server-side. No package identity or digest is accepted from the
// client.
func (s *Service) guidedApproveCurrentPackage(ctx context.Context, input GuidedActionInput) error {
	if s.guidedPackages == nil {
		return ErrGuidedPackageOwnerUnavailable
	}
	return s.guidedPackages.ApproveCurrentPackage(ctx, guidedapp.ApprovePackageInput{WorkspaceID: input.WorkspaceID, Evidence: guidedApprovalEvidence})
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
