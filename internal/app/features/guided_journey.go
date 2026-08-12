package features

// GuidedFeatureAction is the stable, feature-scoped operator intent accepted
// by the guided boundary. Internal artifact, revision, approval, package, and
// run identities are deliberately not part of this contract.
type GuidedFeatureAction string

const (
	GuidedActionContinueDiscovery        GuidedFeatureAction = "continue_discovery"
	GuidedActionCloseDiscovery           GuidedFeatureAction = "close_discovery"
	GuidedActionAuthorRequirements       GuidedFeatureAction = "author_requirements"
	GuidedActionAuthorSharedDesign       GuidedFeatureAction = "author_shared_design"
	GuidedActionAuthorDeliveryPlan       GuidedFeatureAction = "author_delivery_plan"
	GuidedActionAuthorDeliveryTicket     GuidedFeatureAction = "author_delivery_ticket"
	GuidedActionReviewPlanningCandidate  GuidedFeatureAction = "review_planning_candidate"
	GuidedActionApprovePlanningCandidate GuidedFeatureAction = "approve_planning_candidate"
	GuidedActionPromotePlanningCandidate GuidedFeatureAction = "promote_planning_candidate"
	GuidedActionContinueEstablishedRoute GuidedFeatureAction = "continue_established_route"
	GuidedActionCompleteFeature          GuidedFeatureAction = "complete_feature"
	GuidedActionAbandonFeature           GuidedFeatureAction = "abandon_feature"
	GuidedActionCompletionRecorded       GuidedFeatureAction = "completion_recorded"
	GuidedActionLegacyRecovery           GuidedFeatureAction = "legacy_recovery"
	GuidedActionReopenDiscovery          GuidedFeatureAction = "reopen_discovery"
	GuidedActionSelectDeliveryTicket     GuidedFeatureAction = "select_delivery_ticket"
	GuidedActionPreparePackage           GuidedFeatureAction = "prepare_package"
	GuidedActionApprovePackage           GuidedFeatureAction = "approve_package"
	GuidedActionLaunchRun                GuidedFeatureAction = "launch_run"
	GuidedActionContinueRun              GuidedFeatureAction = "continue_run"
	GuidedActionRecoverRun               GuidedFeatureAction = "recover_run"
	GuidedActionPrepareAudit             GuidedFeatureAction = "prepare_audit"
	GuidedActionRecordAuditDecision      GuidedFeatureAction = "record_audit_decision"
	GuidedActionRemediate                GuidedFeatureAction = "remediate"
	GuidedActionPrototypeExecute         GuidedFeatureAction = "prototype_execute"
	GuidedActionPrototypeCleanup         GuidedFeatureAction = "prototype_cleanup"
	GuidedActionPrototypeQA              GuidedFeatureAction = "prototype_qa"
)

// GuidedCompletion is the small completion-owner projection the Feature
// journey consumes. Keeping this at the application boundary avoids coupling
// the Feature owner to an operations implementation package.
type GuidedCompletion struct {
	Gates    []GuidedCompletionGate
	Recorded bool
}
type GuidedCompletionGate struct {
	Name  string
	Ready bool
}

// GuidedFeatureActionAvailability is server-owned. A client may display this
// result, but it must not infer a different progression action.
type GuidedFeatureActionAvailability struct {
	Action               GuidedFeatureAction
	Primary              bool
	Enabled              bool
	RequiresConfirmation bool
	BlockedReason        string
	Handoff              string
}
type GuidedFeatureDecision struct {
	PrimaryAction    GuidedFeatureAction
	AvailableActions []GuidedFeatureActionAvailability
	RecoveryCategory string
}

// GuidedJourneyState is the existing discovery-owner state needed to choose a
// progression action. It intentionally carries no persistence identifier.
// Planning carries the semantic planning progression (admitted, reviewed,
// approved, promoted) so the decision does not infer progression from the bare
// presence of an authority layer. Delivery and Prototype carry the material
// downstream and prototype owner states so the decision can emit precise
// primary actions for selection, package, Run, audit, remediation, and
// prototype cleanup/QA instead of generic continuation.
type GuidedJourneyState struct {
	State               DiscoveryState
	Destination         DiscoveryDestination
	HasCurrentRevision  bool
	AuthorityLayers     []string
	Planning            GuidedPlanningSection
	Delivery            GuidedDeliverySection
	Prototype           GuidedPrototypeSection
	Continuation        string
	Blockers            []string
	PendingIntegrations []string
	ActiveOperations    []string
	RouteMaterialOpen   []string
	RequiredEvidence    []string
}

func hasGuidedLayer(layers []string, wanted string) bool {
	for _, layer := range layers {
		if layer == wanted {
			return true
		}
	}
	return false
}

// DecideGuidedFeatureAction determines exactly one primary action for the
// current Feature basis. It is the Feature application semantic owner: it
// maps authoritative discovery and planning projections to a user-facing
// continuation without accepting or revealing internal identities.
func DecideGuidedFeatureAction(state GuidedJourneyState, currentness FeatureCurrentnessDecision, completion GuidedCompletion) GuidedFeatureDecision {
	available := []GuidedFeatureActionAvailability{}
	if currentness.Readiness == FeatureLegacy || currentness.Readiness == FeatureStale {
		reason := "A current discovery revision is required before discovery can continue."
		if currentness.Readiness == FeatureLegacy {
			reason = "This legacy workspace requires the existing adoption or recovery path before guided progression."
		} else {
			reason = "This workspace basis is stale. Follow the displayed recovery guidance before any progression."
		}
		if currentness.Readiness == FeatureLegacy {
			// Adoption is an existing discovery-owner mutation.  It is safe at the
			// guided boundary because the owner resolves the workspace and verifies
			// that no production work makes adoption unsafe.
			available = append(available, GuidedFeatureActionAvailability{Action: GuidedActionLegacyRecovery, Primary: true, Enabled: true, RequiresConfirmation: true, Handoff: "Adopt the existing discovery lifecycle before guided progression can resume."})
		} else {
			available = append(available, GuidedFeatureActionAvailability{Action: GuidedActionContinueDiscovery, Primary: true, Enabled: false, RequiresConfirmation: false, BlockedReason: reason})
		}
	} else if steps := guidedPrototypeAvailability(state.Prototype); steps != nil {
		// Prototype execution, cleanup reconciliation, and QA admission are
		// Feature-owned exploration work. When a prototype Run is in a
		// source-backed actionable state, it is the operator's next primary
		// action regardless of the downstream journey position.
		available = steps
	} else if state.State == DiscoveryStateClosed {
		available = guidedClosedDestinationAvailability(state, completion)
	} else if state.HasCurrentRevision && state.Destination != "" && state.State == DiscoveryStateActive && len(state.Blockers) == 0 && len(state.PendingIntegrations) == 0 && len(state.ActiveOperations) == 0 && len(state.RouteMaterialOpen) == 0 && len(state.RequiredEvidence) == 0 {
		available = append(available, GuidedFeatureActionAvailability{Action: GuidedActionCloseDiscovery, Primary: true, Enabled: true, RequiresConfirmation: true})
	} else {
		enabled := state.HasCurrentRevision
		reason := "Discovery has outstanding work or recovery information that must be recorded before closure."
		if !enabled {
			reason = "A current discovery revision is required before discovery can continue."
		}
		available = append(available, GuidedFeatureActionAvailability{Action: GuidedActionContinueDiscovery, Primary: true, Enabled: enabled, RequiresConfirmation: true, BlockedReason: reason})
	}
	return GuidedFeatureDecision{PrimaryAction: available[0].Action, RecoveryCategory: currentness.RecoveryCategory, AvailableActions: available}
}

// guidedClosedDestinationAvailability maps a closed discovery packet to the
// guided actions available to the operator. Planning-family destinations are
// driven by the semantic planning section so the operator advances through
// author -> review -> explicit approval -> promotion -> next family or ticket instead
// of inferring progression from authority layer presence.
func guidedClosedDestinationAvailability(state GuidedJourneyState, completion GuidedCompletion) []GuidedFeatureActionAvailability {
	switch state.Destination {
	case DiscoveryDestinationNoDeliveryWork, "":
		if completion.Recorded {
			// After completion is recorded the only source-backed continuation
			// is revising the closed discovery. The reopen owner reopens the
			// completion decision and returns the workspace to active
			// discovery; the server resolves the current closure packet basis
			// and requires operator confirmation of the replacement.
			return []GuidedFeatureActionAvailability{{Action: GuidedActionReopenDiscovery, Primary: true, Enabled: true, RequiresConfirmation: true, Handoff: "Revise the closed discovery through the existing reopen owner: author the replacement integrated revision, confirm the reopen, then reclose and complete again."}}
		}
		return guidedCompletionActions(completion)
	case DiscoveryDestinationDirectDeliveryTicket:
		return guidedDeliveryAvailability(state, completion)
	case DiscoveryDestinationRequirements:
		if guidedPlanningActive(state.Planning) {
			if steps := guidedPlanningStep(state.Planning.Requirements, GuidedActionAuthorRequirements, "Requirements", "Author Requirements, complete its read-only review, explicitly approve the exact reviewed candidate, then return to promote it."); steps != nil {
				return steps
			}
			return guidedDeliveryAvailability(state, completion)
		}
		if hasGuidedLayer(state.AuthorityLayers, "requirements") {
			return guidedDeliveryAvailability(state, completion)
		}
		return []GuidedFeatureActionAvailability{{Action: GuidedActionAuthorRequirements, Primary: true, Enabled: true, Handoff: "Author Requirements, complete its read-only review, explicitly approve the exact reviewed candidate, then return to promote it."}}
	case DiscoveryDestinationSharedDesign:
		if guidedPlanningActive(state.Planning) {
			if steps := guidedPlanningStep(state.Planning.SharedDesign, GuidedActionAuthorSharedDesign, "Shared Design", "Author Shared Design, complete its read-only review, explicitly approve the exact reviewed candidate, then return to promote it."); steps != nil {
				return steps
			}
			// An approved Shared Design is followed by the Delivery Plan family
			// on the shared-design routes. The Plan is promoted into the
			// workspace's current approved Delivery Plan and then governs the
			// Delivery Ticket frontier.
			if steps := guidedPlanningStep(state.Planning.DeliveryPlan, GuidedActionAuthorDeliveryPlan, "Delivery Plan", "Author the Delivery Plan, complete its read-only review, explicitly approve the exact reviewed candidate, then return to promote it into the workspace's current approved Plan."); steps != nil {
				return steps
			}
			return guidedDeliveryAvailability(state, completion)
		}
		if hasGuidedLayer(state.AuthorityLayers, "shared_design") {
			return guidedDeliveryAvailability(state, completion)
		}
		return []GuidedFeatureActionAvailability{{Action: GuidedActionAuthorSharedDesign, Primary: true, Enabled: true, Handoff: "Author Shared Design, complete its read-only review, then explicitly approve and promote it before returning."}}
	case DiscoveryDestinationRequirementsThenSharedDesign:
		if guidedPlanningActive(state.Planning) {
			switch {
			case state.Planning.SharedDesign.Count > 0:
				if steps := guidedPlanningStep(state.Planning.SharedDesign, GuidedActionAuthorSharedDesign, "Shared Design", "Author Shared Design, then explicitly approve and promote it."); steps != nil {
					return steps
				}
				// After Shared Design is promoted the Delivery Plan family is
				// the next planning family on this route; its promotion records
				// the workspace's current approved Delivery Plan.
				if steps := guidedPlanningStep(state.Planning.DeliveryPlan, GuidedActionAuthorDeliveryPlan, "Delivery Plan", "Author the Delivery Plan, then explicitly approve and promote it before Delivery Ticket authoring."); steps != nil {
					return steps
				}
				return guidedDeliveryAvailability(state, completion)
			case state.Planning.Requirements.Count > 0:
				if steps := guidedPlanningStep(state.Planning.Requirements, GuidedActionAuthorRequirements, "Requirements", "Author Requirements, then explicitly approve and promote it before Shared Design."); steps != nil {
					return steps
				}
				return []GuidedFeatureActionAvailability{{Action: GuidedActionAuthorSharedDesign, Primary: true, Enabled: true, Handoff: "Requirements authority is current. Author Shared Design, then explicitly approve and promote it."}}
			default:
				return []GuidedFeatureActionAvailability{{Action: GuidedActionAuthorRequirements, Primary: true, Enabled: true, Handoff: "Author Requirements, then explicitly approve and promote it before Shared Design."}}
			}
		}
		if hasGuidedLayer(state.AuthorityLayers, "shared_design") {
			return guidedDeliveryAvailability(state, completion)
		}
		if hasGuidedLayer(state.AuthorityLayers, "requirements") {
			return []GuidedFeatureActionAvailability{{Action: GuidedActionAuthorSharedDesign, Primary: true, Enabled: true, Handoff: "Requirements authority is current. Author Shared Design, then explicitly approve and promote it."}}
		}
		return []GuidedFeatureActionAvailability{{Action: GuidedActionAuthorRequirements, Primary: true, Enabled: true, Handoff: "Author Requirements, then explicitly approve and promote it before Shared Design."}}
	case DiscoveryDestinationExistingRouteContinuation:
		// A precise continuation resolves through the same source-backed
		// delivery stage (frontier, selection, brief, package, Run, audit, or
		// remediation) whenever one is available, instead of generic Planner
		// authoring for a route that already has delivery work.
		if state.Planning.DeliveryTicket.Count > 0 || guidedDeliveryStageAvailable(state.Delivery) {
			return guidedDeliveryAvailability(state, completion)
		}
		return []GuidedFeatureActionAvailability{{Action: GuidedActionContinueEstablishedRoute, Primary: true, Enabled: true, RequiresConfirmation: false, Handoff: state.Continuation}}
	default:
		return []GuidedFeatureActionAvailability{{Action: GuidedActionContinueDiscovery, Primary: true, Enabled: false, RequiresConfirmation: false, BlockedReason: "The closed discovery packet has no supported destination continuation."}}
	}
}

// guidedDeliveryAvailability maps the delivery owner state to the precise
// primary action: selection when the frontier has a ready ticket, package
// preparation or approval while a selection is active, Run launch while a
// package Run is pending execution, audit preparation or decision recording in
// the audit phase, remediation while an audit remediation seed is open, and
// Feature completion once the delivered Run has completed.
func guidedDeliveryAvailability(state GuidedJourneyState, completion GuidedCompletion) []GuidedFeatureActionAvailability {
	delivery := state.Delivery
	// A current Delivery Ticket candidate is still owned by the planning
	// candidate lifecycle. It must finish review, explicit approval, and
	// ticket-owner production before a frontier, selection, or audit stage can
	// become authoritative.
	if steps := guidedDeliveryTicketCandidateStep(state.Planning.DeliveryTicket); steps != nil {
		return steps
	}
	if delivery.RemediationState == "open" {
		return []GuidedFeatureActionAvailability{{Action: GuidedActionRemediate, Primary: true, Enabled: true, RequiresConfirmation: false, Handoff: "The current audit decision requires remediation. Publish the replacement Delivery Ticket revision through the existing owner, then return here to resume selection."}}
	}
	switch delivery.SelectionState {
	case "", "none":
		if len(delivery.Frontier) > 0 {
			return []GuidedFeatureActionAvailability{{Action: GuidedActionSelectDeliveryTicket, Primary: true, Enabled: true, RequiresConfirmation: true, Handoff: "Select the current frontier Delivery Ticket server-side; the selection resolves the exact current revision from the delivery owner."}}
		}
		return []GuidedFeatureActionAvailability{{Action: GuidedActionAuthorDeliveryTicket, Primary: true, Enabled: true, RequiresConfirmation: false, Handoff: "Delivery Ticket authoring is the current bounded role operation. Return here when it is complete."}}
	case "active":
		// The selected approved Delivery Ticket is the sole ticket semantic
		// authority; there is no separate Ticket Design Brief stage. The
		// package owner may prepare the execution package server-side from the
		// active selection and approved Ticket immediately.
		switch delivery.PackageState {
		case "", "none":
			return []GuidedFeatureActionAvailability{{Action: GuidedActionPreparePackage, Primary: true, Enabled: true, RequiresConfirmation: false, Handoff: "Prepare the execution package through the package owner using the current approved Delivery Ticket and active selection, then return here to approve it server-side."}}
		case "prepared":
			return []GuidedFeatureActionAvailability{{Action: GuidedActionApprovePackage, Primary: true, Enabled: true, RequiresConfirmation: true, Handoff: "Approve the prepared execution package server-side; the approval resolves the exact package identity and digest from the package owner."}}
		}
	}
	switch delivery.RunState {
	case "", "none":
		return []GuidedFeatureActionAvailability{{Action: GuidedActionPreparePackage, Primary: true, Enabled: true, RequiresConfirmation: false, Handoff: "Prepare the execution package through the package owner using the current approved Delivery Ticket and active selection, then return here to approve it server-side."}}
	case "created", "setup_ready":
		return []GuidedFeatureActionAvailability{{Action: GuidedActionLaunchRun, Primary: true, Enabled: true, RequiresConfirmation: false, Handoff: "The package Run is ready for its initial execution. Launch it through the Run owner, then return here for a fresh currentness check."}}
	case "executing":
		return []GuidedFeatureActionAvailability{{Action: GuidedActionContinueRun, Primary: true, Enabled: true, RequiresConfirmation: false, Handoff: "The package Run is already executing. Continue or view the active Run through the Run owner, then return here for a fresh currentness check."}}
	case "execution_failed", "cancelled":
		return []GuidedFeatureActionAvailability{{Action: GuidedActionRecoverRun, Primary: true, Enabled: true, RequiresConfirmation: false, Handoff: "The package Run did not complete. Use the Run owner's supported recovery or retry operation, then return here for a fresh currentness check."}}
	case "needs_revision":
		return []GuidedFeatureActionAvailability{{Action: GuidedActionRemediate, Primary: true, Enabled: true, RequiresConfirmation: false, Handoff: "The package Run requires a delivery revision. Continue the audit remediation owner to publish the required replacement Delivery Ticket revision, then return here."}}
	case "validating", "validation_failed", "audit_ready":
		switch delivery.AuditState {
		case "", "none", "awaiting_audit":
			return []GuidedFeatureActionAvailability{{Action: GuidedActionPrepareAudit, Primary: true, Enabled: true, RequiresConfirmation: false, Handoff: "Prepare the workflow audit packet through the audit owner for the current Run, then return here to record the audit decision."}}
		case "packet_recorded":
			return []GuidedFeatureActionAvailability{{Action: GuidedActionRecordAuditDecision, Primary: true, Enabled: true, RequiresConfirmation: false, Handoff: "Record the workflow audit decision through the audit owner for the current packet, then return here."}}
		}
		return guidedCompletionActions(completion)
	case "completed":
		return guidedCompletionActions(completion)
	}
	return guidedCompletionActions(completion)
}

func guidedCompletionBlockedReason(ready bool) string {
	if ready {
		return ""
	}
	return "Feature completion is blocked by one or more current completion gates."
}

// guidedCompletionActions emits the completion primary action and, only when
// no current decision is recorded and the completion gate matrix is ready, the
// enabled confirmed abandonment secondary on the same basis. Abandonment
// eligibility equals the completion-gate/current-basis eligibility; the primary
// action remains complete_feature.
func guidedCompletionActions(completion GuidedCompletion) []GuidedFeatureActionAvailability {
	completionReady := GuidedCompletionReady(completion.Gates)
	primary := GuidedFeatureActionAvailability{Action: GuidedActionCompleteFeature, Primary: true, Enabled: completionReady, RequiresConfirmation: true, BlockedReason: guidedCompletionBlockedReason(completionReady)}
	if !completionReady || completion.Recorded {
		return []GuidedFeatureActionAvailability{primary}
	}
	return []GuidedFeatureActionAvailability{
		primary,
		{Action: GuidedActionAbandonFeature, Primary: false, Enabled: true, RequiresConfirmation: true, Handoff: "Abandon this feature workspace: record an immutable abandoned closing decision on the current basis. Reopening discovery remains the resume path."},
	}
}

// guidedDeliveryStageAvailable reports whether the delivery owner already
// carries a source-backed stage (frontier, selection, package, Run, audit, or
// remediation) that a continuation should resume instead of generic Planner
// authoring.
func guidedDeliveryStageAvailable(delivery GuidedDeliverySection) bool {
	if len(delivery.Frontier) > 0 {
		return true
	}
	for _, state := range []string{delivery.SelectionState, delivery.PackageState, delivery.RunState, delivery.AuditState, delivery.RemediationState} {
		if state != "" && state != "none" {
			return true
		}
	}
	return false
}

// guidedPrototypeAvailability maps the prototype owner state to the precise
// primary action when a prototype Run is in a source-backed actionable state:
// launch when the approved Run is proposed, cleanup reconciliation when the
// Run is terminal with cleanup pending, and QA when the closed Run still needs
// operator QA evidence. It returns nil when no prototype action is due so the
// journey decision continues.
func guidedPrototypeAvailability(prototype GuidedPrototypeSection) []GuidedFeatureActionAvailability {
	switch {
	case prototype.RunState == "proposed":
		return []GuidedFeatureActionAvailability{{Action: GuidedActionPrototypeExecute, Primary: true, Enabled: true, RequiresConfirmation: false, Handoff: "The approved prototype execution is ready to launch. Launch it through the prototype owner, then return here."}}
	case prototype.CleanupState == "pending":
		return []GuidedFeatureActionAvailability{{Action: GuidedActionPrototypeCleanup, Primary: true, Enabled: true, RequiresConfirmation: false, Handoff: "The prototype Run has cleanup obligations pending. Reconcile cleanup through the prototype owner, then return here."}}
	case prototype.RunState == "closed" && prototype.CleanupState == "complete" && prototype.QAState != "admitted":
		return []GuidedFeatureActionAvailability{{Action: GuidedActionPrototypeQA, Primary: true, Enabled: true, RequiresConfirmation: false, Handoff: "The prototype Run is closed with cleanup complete. Prepare and admit the QA packet through the prototype QA owner, then return here."}}
	default:
		return nil
	}
}

// guidedPlanningActive reports whether the journey carries a semantic planning
// section. Callers that only expose authority layers (legacy compatibility
// surface) keep the historical authority-presence decision.
func guidedPlanningActive(planning GuidedPlanningSection) bool {
	return planning.Status != "" || planning.CandidateCount > 0 || planning.Requirements.Count > 0 || planning.SharedDesign.Count > 0
}

// guidedPlanningStep maps one planning family's semantic state to the guided
// actions available to the operator, preserving the sequence author -> review
// -> explicit approval -> promotion -> next family or ticket. It returns nil when the
// family is fully promoted so the caller advances to the next family or
// Delivery Ticket.
func guidedPlanningStep(family GuidedPlanningFamilySection, authorAction GuidedFeatureAction, familyName, authorHandoff string) []GuidedFeatureActionAvailability {
	if family.Count > 0 && family.Promoted == family.Count {
		return nil
	}
	if family.AwaitingApproval > 0 {
		return []GuidedFeatureActionAvailability{{
			Action: GuidedActionApprovePlanningCandidate, Primary: true, Enabled: true, RequiresConfirmation: true,
			Handoff: "The current " + familyName + " planning candidate review is ready. Explicitly approve the exact reviewed candidate server-side; promotion becomes available only after this durable approval.",
		}}
	}
	if family.AwaitingPromotion > 0 {
		return []GuidedFeatureActionAvailability{{
			Action: GuidedActionPromotePlanningCandidate, Primary: true, Enabled: true,
			Handoff: "The current " + familyName + " planning candidate is approved. Promote it server-side to publish it as workspace authority before continuing.",
		}}
	}
	if family.AwaitingReview > 0 {
		return []GuidedFeatureActionAvailability{{Action: GuidedActionReviewPlanningCandidate, Primary: true, Enabled: true, Handoff: "Review the current " + familyName + " planning candidate through the auditor review surface. A ready completion arms the distinct explicit approval action; a needs-revision completion returns the exact planner refresh input for a replacement candidate."}}
	}
	return []GuidedFeatureActionAvailability{{Action: authorAction, Primary: true, Enabled: true, Handoff: authorHandoff}}
}

// guidedDeliveryTicketCandidateStep maps the current Delivery Ticket planning
// candidate to its own lifecycle. It deliberately does not reuse audit
// remediation: a candidate review disposition is planning work, not a Run
// audit decision.
func guidedDeliveryTicketCandidateStep(family GuidedPlanningFamilySection) []GuidedFeatureActionAvailability {
	if family.Count == 0 || family.Produced == family.Count {
		return nil
	}
	if family.AwaitingApproval > 0 {
		return []GuidedFeatureActionAvailability{{Action: GuidedActionApprovePlanningCandidate, Primary: true, Enabled: true, RequiresConfirmation: true, Handoff: "The current Delivery Ticket candidate review is ready. Explicitly approve the exact reviewed candidate server-side; production becomes available only after this durable approval."}}
	}
	if family.AwaitingPromotion > 0 {
		return []GuidedFeatureActionAvailability{{Action: GuidedActionPromotePlanningCandidate, Primary: true, Enabled: true, Handoff: "The current Delivery Ticket candidate is explicitly approved. Produce and publish it through the delivery-ticket owner using the server-resolved candidate, approval, and current basis."}}
	}
	if family.AwaitingReview > 0 {
		return []GuidedFeatureActionAvailability{{Action: GuidedActionReviewPlanningCandidate, Primary: true, Enabled: true, Handoff: "Review the current Delivery Ticket candidate through the exact read-only auditor.delivery_ticket_review operation. A ready completion arms the distinct explicit approval action; needs revision returns the exact planner refresh input."}}
	}
	return nil
}

// guidedCandidateFamiliesForDestination returns the candidate families that
// may still carry in-flight (non-promoted) candidates, in decision priority
// order. The requirement-then-shared-design destination scans Shared Design
// first because Requirements must promote before Shared Design is admitted.
func guidedCandidateFamiliesForDestination(destination DiscoveryDestination) []string {
	switch destination {
	case DiscoveryDestinationRequirements:
		return []string{CandidateFamilyRequirements, CandidateFamilyDeliveryTicket}
	case DiscoveryDestinationSharedDesign:
		return []string{CandidateFamilySharedDesign, CandidateFamilyDeliveryPlan, CandidateFamilyDeliveryTicket}
	case DiscoveryDestinationRequirementsThenSharedDesign:
		return []string{CandidateFamilySharedDesign, CandidateFamilyRequirements, CandidateFamilyDeliveryPlan, CandidateFamilyDeliveryTicket}
	case DiscoveryDestinationDirectDeliveryTicket, DiscoveryDestinationExistingRouteContinuation:
		return []string{CandidateFamilyDeliveryTicket}
	default:
		return nil
	}
}

func GuidedCompletionReady(gates []GuidedCompletionGate) bool {
	for _, gate := range gates {
		if !gate.Ready {
			return false
		}
	}
	return true
}
