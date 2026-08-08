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
	GuidedActionAuthorDeliveryTicket     GuidedFeatureAction = "author_delivery_ticket"
	GuidedActionReviewPlanningCandidate  GuidedFeatureAction = "review_planning_candidate"
	GuidedActionApprovePlanningCandidate GuidedFeatureAction = "approve_planning_candidate"
	GuidedActionPromotePlanningCandidate GuidedFeatureAction = "promote_planning_candidate"
	GuidedActionContinueEstablishedRoute GuidedFeatureAction = "continue_established_route"
	GuidedActionCompleteFeature          GuidedFeatureAction = "complete_feature"
	GuidedActionCompletionRecorded       GuidedFeatureAction = "completion_recorded"
	GuidedActionLegacyRecovery           GuidedFeatureAction = "legacy_recovery"
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
// presence of an authority layer.
type GuidedJourneyState struct {
	State               DiscoveryState
	Destination         DiscoveryDestination
	HasCurrentRevision  bool
	AuthorityLayers     []string
	Planning            GuidedPlanningSection
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
// author -> review -> approval -> promotion -> next family or ticket instead
// of inferring progression from authority layer presence.
func guidedClosedDestinationAvailability(state GuidedJourneyState, completion GuidedCompletion) []GuidedFeatureActionAvailability {
	completionReady := GuidedCompletionReady(completion.Gates)
	switch state.Destination {
	case DiscoveryDestinationNoDeliveryWork, "":
		if completion.Recorded {
			return []GuidedFeatureActionAvailability{{Action: GuidedActionCompletionRecorded, Primary: true, Enabled: false, RequiresConfirmation: false, BlockedReason: "Feature completion is already recorded."}}
		}
		enabled := completionReady
		reason := ""
		if !enabled {
			reason = "Feature completion is blocked by one or more current completion gates."
		}
		return []GuidedFeatureActionAvailability{{Action: GuidedActionCompleteFeature, Primary: true, Enabled: enabled, RequiresConfirmation: true, BlockedReason: reason}}
	case DiscoveryDestinationDirectDeliveryTicket:
		return []GuidedFeatureActionAvailability{{Action: GuidedActionAuthorDeliveryTicket, Primary: true, Enabled: true, RequiresConfirmation: false, Handoff: "Delivery Ticket authoring is the current bounded role operation. Return here when it is complete."}}
	case DiscoveryDestinationRequirements:
		if guidedPlanningActive(state.Planning) {
			if steps := guidedPlanningStep(state.Planning.Requirements, GuidedActionAuthorRequirements, "Requirements", "Author Requirements, complete its read-only review, then explicitly approve and promote it before returning."); steps != nil {
				return steps
			}
			return []GuidedFeatureActionAvailability{{Action: GuidedActionAuthorDeliveryTicket, Primary: true, Enabled: true, Handoff: "Requirements authority is current; continue with Delivery Ticket authoring."}}
		}
		if hasGuidedLayer(state.AuthorityLayers, "requirements") {
			return []GuidedFeatureActionAvailability{{Action: GuidedActionAuthorDeliveryTicket, Primary: true, Enabled: true, Handoff: "Requirements authority is current; continue with Delivery Ticket authoring."}}
		}
		return []GuidedFeatureActionAvailability{{Action: GuidedActionAuthorRequirements, Primary: true, Enabled: true, Handoff: "Author Requirements, complete its read-only review, then explicitly approve and promote it before returning."}}
	case DiscoveryDestinationSharedDesign:
		if guidedPlanningActive(state.Planning) {
			if steps := guidedPlanningStep(state.Planning.SharedDesign, GuidedActionAuthorSharedDesign, "Shared Design", "Author Shared Design, complete its read-only review, then explicitly approve and promote it before returning."); steps != nil {
				return steps
			}
			return []GuidedFeatureActionAvailability{{Action: GuidedActionAuthorDeliveryTicket, Primary: true, Enabled: true, Handoff: "Shared Design authority is current; continue with Delivery Ticket authoring."}}
		}
		if hasGuidedLayer(state.AuthorityLayers, "shared_design") {
			return []GuidedFeatureActionAvailability{{Action: GuidedActionAuthorDeliveryTicket, Primary: true, Enabled: true, Handoff: "Shared Design authority is current; continue with Delivery Ticket authoring."}}
		}
		return []GuidedFeatureActionAvailability{{Action: GuidedActionAuthorSharedDesign, Primary: true, Enabled: true, Handoff: "Author Shared Design, complete its read-only review, then explicitly approve and promote it before returning."}}
	case DiscoveryDestinationRequirementsThenSharedDesign:
		if guidedPlanningActive(state.Planning) {
			switch {
			case state.Planning.SharedDesign.Count > 0:
				if steps := guidedPlanningStep(state.Planning.SharedDesign, GuidedActionAuthorSharedDesign, "Shared Design", "Author Shared Design, then explicitly approve and promote it."); steps != nil {
					return steps
				}
				return []GuidedFeatureActionAvailability{{Action: GuidedActionAuthorDeliveryTicket, Primary: true, Enabled: true, Handoff: "Requirements and Shared Design authority are current; continue with Delivery Ticket authoring."}}
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
			return []GuidedFeatureActionAvailability{{Action: GuidedActionAuthorDeliveryTicket, Primary: true, Enabled: true, Handoff: "Requirements and Shared Design authority are current; continue with Delivery Ticket authoring."}}
		}
		if hasGuidedLayer(state.AuthorityLayers, "requirements") {
			return []GuidedFeatureActionAvailability{{Action: GuidedActionAuthorSharedDesign, Primary: true, Enabled: true, Handoff: "Requirements authority is current. Author Shared Design, then explicitly approve and promote it."}}
		}
		return []GuidedFeatureActionAvailability{{Action: GuidedActionAuthorRequirements, Primary: true, Enabled: true, Handoff: "Author Requirements, then explicitly approve and promote it before Shared Design."}}
	case DiscoveryDestinationExistingRouteContinuation:
		return []GuidedFeatureActionAvailability{{Action: GuidedActionContinueEstablishedRoute, Primary: true, Enabled: true, RequiresConfirmation: false, Handoff: state.Continuation}}
	default:
		return []GuidedFeatureActionAvailability{{Action: GuidedActionContinueDiscovery, Primary: true, Enabled: false, RequiresConfirmation: false, BlockedReason: "The closed discovery packet has no supported destination continuation."}}
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
// -> approval -> promotion -> next family or ticket. It returns nil when the
// family is fully promoted so the caller advances to the next family or
// Delivery Ticket.
func guidedPlanningStep(family GuidedPlanningFamilySection, authorAction GuidedFeatureAction, familyName, authorHandoff string) []GuidedFeatureActionAvailability {
	if family.Count > 0 && family.Promoted == family.Count {
		return nil
	}
	if family.AwaitingPromotion > 0 {
		return []GuidedFeatureActionAvailability{{
			Action: GuidedActionPromotePlanningCandidate, Primary: true, Enabled: true,
			Handoff: "The current " + familyName + " planning candidate is approved. Promote it server-side to publish it as workspace authority before continuing.",
		}}
	}
	if family.AwaitingReview > 0 {
		return []GuidedFeatureActionAvailability{{Action: GuidedActionReviewPlanningCandidate, Primary: true, Enabled: true, Handoff: "Review the current " + familyName + " planning candidate through the auditor review surface. After the owner records review and approval, refresh this workspace to promote it."}}
	}
	return []GuidedFeatureActionAvailability{{Action: authorAction, Primary: true, Enabled: true, Handoff: authorHandoff}}
}

// guidedCandidateFamiliesForDestination returns the candidate families that
// may still carry in-flight (non-promoted) candidates, in decision priority
// order. The requirement-then-shared-design destination scans Shared Design
// first because Requirements must promote before Shared Design is admitted.
func guidedCandidateFamiliesForDestination(destination DiscoveryDestination) []string {
	switch destination {
	case DiscoveryDestinationRequirements:
		return []string{CandidateFamilyRequirements}
	case DiscoveryDestinationSharedDesign:
		return []string{CandidateFamilySharedDesign}
	case DiscoveryDestinationRequirementsThenSharedDesign:
		return []string{CandidateFamilySharedDesign, CandidateFamilyRequirements}
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
