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
type GuidedJourneyState struct {
	State               DiscoveryState
	Destination         DiscoveryDestination
	HasCurrentRevision  bool
	AuthorityLayers     []string
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
// maps authoritative discovery and authority projections to a user-facing
// continuation without accepting or revealing internal identities.
func DecideGuidedFeatureAction(state GuidedJourneyState, currentness FeatureCurrentnessDecision, completion GuidedCompletion) GuidedFeatureDecision {
	completionReady := GuidedCompletionReady(completion.Gates)
	action, enabled, confirmation := GuidedActionContinueDiscovery, state.HasCurrentRevision, true
	reason, handoff := "A current discovery revision is required before discovery can continue.", ""

	if currentness.Readiness == FeatureLegacy || currentness.Readiness == FeatureStale {
		enabled, confirmation = false, false
		if currentness.Readiness == FeatureLegacy {
			reason = "This legacy workspace requires the existing adoption or recovery path before guided progression."
		} else {
			reason = "This workspace basis is stale. Follow the displayed recovery guidance before any progression."
		}
	} else if state.State == DiscoveryStateClosed {
		switch state.Destination {
		case DiscoveryDestinationNoDeliveryWork, "":
			if completion.Recorded {
				action, enabled, confirmation, reason = GuidedActionCompletionRecorded, false, false, "Feature completion is already recorded."
			} else {
				action, enabled, confirmation = GuidedActionCompleteFeature, completionReady, true
				if !enabled {
					reason = "Feature completion is blocked by one or more current completion gates."
				} else {
					reason = ""
				}
			}
		case DiscoveryDestinationDirectDeliveryTicket:
			action, enabled, confirmation, reason, handoff = GuidedActionAuthorDeliveryTicket, true, false, "", "Delivery Ticket authoring is the current bounded role operation. Return here when it is complete."
		case DiscoveryDestinationRequirements:
			if hasGuidedLayer(state.AuthorityLayers, "requirements") {
				action, handoff = GuidedActionAuthorDeliveryTicket, "Requirements authority is current; continue with Delivery Ticket authoring."
			} else {
				action, handoff = GuidedActionAuthorRequirements, "Author Requirements, complete its read-only review, then explicitly approve and promote it before returning."
			}
			enabled, confirmation, reason = true, false, ""
		case DiscoveryDestinationSharedDesign:
			if hasGuidedLayer(state.AuthorityLayers, "shared_design") {
				action, handoff = GuidedActionAuthorDeliveryTicket, "Shared Design authority is current; continue with Delivery Ticket authoring."
			} else {
				action, handoff = GuidedActionAuthorSharedDesign, "Author Shared Design, complete its read-only review, then explicitly approve and promote it before returning."
			}
			enabled, confirmation, reason = true, false, ""
		case DiscoveryDestinationRequirementsThenSharedDesign:
			if hasGuidedLayer(state.AuthorityLayers, "shared_design") {
				action, handoff = GuidedActionAuthorDeliveryTicket, "Requirements and Shared Design authority are current; continue with Delivery Ticket authoring."
			} else if hasGuidedLayer(state.AuthorityLayers, "requirements") {
				action, handoff = GuidedActionAuthorSharedDesign, "Requirements authority is current. Author Shared Design, then explicitly approve and promote it."
			} else {
				action, handoff = GuidedActionAuthorRequirements, "Author Requirements, then explicitly approve and promote it before Shared Design."
			}
			enabled, confirmation, reason = true, false, ""
		case DiscoveryDestinationExistingRouteContinuation:
			action, enabled, confirmation, reason, handoff = GuidedActionContinueEstablishedRoute, true, false, "", state.Continuation
		default:
			enabled, reason = false, "The closed discovery packet has no supported destination continuation."
		}
	} else if state.HasCurrentRevision && state.Destination != "" && state.State == DiscoveryStateActive && len(state.Blockers) == 0 && len(state.PendingIntegrations) == 0 && len(state.ActiveOperations) == 0 && len(state.RouteMaterialOpen) == 0 && len(state.RequiredEvidence) == 0 {
		action, enabled, reason = GuidedActionCloseDiscovery, true, ""
	} else if enabled {
		reason = "Discovery has outstanding work or recovery information that must be recorded before closure."
	}

	return GuidedFeatureDecision{PrimaryAction: action, RecoveryCategory: currentness.RecoveryCategory, AvailableActions: []GuidedFeatureActionAvailability{{Action: action, Primary: true, Enabled: enabled, RequiresConfirmation: confirmation, BlockedReason: reason, Handoff: handoff}}}
}
func GuidedCompletionReady(gates []GuidedCompletionGate) bool {
	for _, gate := range gates {
		if !gate.Ready {
			return false
		}
	}
	return true
}
