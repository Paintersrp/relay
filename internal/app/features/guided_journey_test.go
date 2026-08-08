package features

import "testing"

func TestDecideGuidedFeatureActionOwnsExactlyOnePrimaryAction(t *testing.T) {
	cases := []struct {
		name        string
		state       GuidedJourneyState
		currentness FeatureCurrentnessDecision
		completion  GuidedCompletion
		want        GuidedFeatureAction
		enabled     bool
	}{
		{"active discovery", GuidedJourneyState{State: DiscoveryStateActive, HasCurrentRevision: true}, FeatureCurrentnessDecision{Readiness: FeatureCurrent}, GuidedCompletion{}, GuidedActionContinueDiscovery, true},
		{"closure ready", GuidedJourneyState{State: DiscoveryStateActive, HasCurrentRevision: true, Destination: DiscoveryDestinationRequirements}, FeatureCurrentnessDecision{Readiness: FeatureCurrent}, GuidedCompletion{}, GuidedActionCloseDiscovery, true},
		{"closed completion ready", GuidedJourneyState{State: DiscoveryStateClosed}, FeatureCurrentnessDecision{Readiness: FeatureCurrent}, GuidedCompletion{Gates: []GuidedCompletionGate{{Name: "all", Ready: true}}}, GuidedActionCompleteFeature, true},
		{"direct delivery ticket", GuidedJourneyState{State: DiscoveryStateClosed, Destination: DiscoveryDestinationDirectDeliveryTicket}, FeatureCurrentnessDecision{Readiness: FeatureCurrent}, GuidedCompletion{}, GuidedActionAuthorDeliveryTicket, true},
		{"requirements authoring", GuidedJourneyState{State: DiscoveryStateClosed, Destination: DiscoveryDestinationRequirements}, FeatureCurrentnessDecision{Readiness: FeatureCurrent}, GuidedCompletion{}, GuidedActionAuthorRequirements, true},
		{"requirements ticket frontier", GuidedJourneyState{State: DiscoveryStateClosed, Destination: DiscoveryDestinationRequirements, AuthorityLayers: []string{"requirements"}}, FeatureCurrentnessDecision{Readiness: FeatureCurrent}, GuidedCompletion{}, GuidedActionAuthorDeliveryTicket, true},
		{"shared design authoring", GuidedJourneyState{State: DiscoveryStateClosed, Destination: DiscoveryDestinationSharedDesign}, FeatureCurrentnessDecision{Readiness: FeatureCurrent}, GuidedCompletion{}, GuidedActionAuthorSharedDesign, true},
		{"requirements then design", GuidedJourneyState{State: DiscoveryStateClosed, Destination: DiscoveryDestinationRequirementsThenSharedDesign, AuthorityLayers: []string{"requirements"}}, FeatureCurrentnessDecision{Readiness: FeatureCurrent}, GuidedCompletion{}, GuidedActionAuthorSharedDesign, true},
		{"requirements then design ticket frontier", GuidedJourneyState{State: DiscoveryStateClosed, Destination: DiscoveryDestinationRequirementsThenSharedDesign, AuthorityLayers: []string{"requirements", "shared_design"}}, FeatureCurrentnessDecision{Readiness: FeatureCurrent}, GuidedCompletion{}, GuidedActionAuthorDeliveryTicket, true},
		{"existing route continuation", GuidedJourneyState{State: DiscoveryStateClosed, Destination: DiscoveryDestinationExistingRouteContinuation, Continuation: "resume package"}, FeatureCurrentnessDecision{Readiness: FeatureCurrent}, GuidedCompletion{}, GuidedActionContinueEstablishedRoute, true},

		{"closed completion blocked", GuidedJourneyState{State: DiscoveryStateClosed}, FeatureCurrentnessDecision{Readiness: FeatureCurrent}, GuidedCompletion{Gates: []GuidedCompletionGate{{Name: "audit", Ready: false}}}, GuidedActionCompleteFeature, false},
		{"completion recorded", GuidedJourneyState{State: DiscoveryStateClosed}, FeatureCurrentnessDecision{Readiness: FeatureCurrent}, GuidedCompletion{Recorded: true}, GuidedActionCompletionRecorded, false},
		{"stale recovery", GuidedJourneyState{State: DiscoveryStateClosed, Destination: DiscoveryDestinationDirectDeliveryTicket}, FeatureCurrentnessDecision{Readiness: FeatureStale, RecoveryCategory: "replace_current_closure"}, GuidedCompletion{}, GuidedActionContinueDiscovery, false},
		{"legacy recovery", GuidedJourneyState{State: DiscoveryStateActive, HasCurrentRevision: true}, FeatureCurrentnessDecision{Readiness: FeatureLegacy, RecoveryCategory: "adopt_discovery_lifecycle"}, GuidedCompletion{}, GuidedActionContinueDiscovery, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := DecideGuidedFeatureAction(tc.state, tc.currentness, tc.completion)
			if got.PrimaryAction != tc.want || len(got.AvailableActions) != 1 || !got.AvailableActions[0].Primary || got.AvailableActions[0].Action != tc.want || got.AvailableActions[0].Enabled != tc.enabled {
				t.Fatalf("decision=%+v", got)
			}
		})
	}
}
