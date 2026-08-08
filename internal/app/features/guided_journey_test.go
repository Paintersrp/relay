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
		{"legacy recovery", GuidedJourneyState{State: DiscoveryStateActive, HasCurrentRevision: true}, FeatureCurrentnessDecision{Readiness: FeatureLegacy, RecoveryCategory: "adopt_discovery_lifecycle"}, GuidedCompletion{}, GuidedActionLegacyRecovery, true},
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

func TestDecideGuidedFeatureActionSequencesIntermediatePlanningStates(t *testing.T) {
	current := FeatureCurrentnessDecision{Readiness: FeatureCurrent}
	admittedRequirements := GuidedPlanningSection{
		Status: "in_progress", CandidateCount: 1, AwaitingReview: 1, CandidateState: "admitted", ReviewState: "awaiting_review", ApprovalState: "none", PromotionState: "none",
		Requirements: GuidedPlanningFamilySection{Count: 1, AwaitingReview: 1, State: "admitted"},
	}
	approvedRequirements := GuidedPlanningSection{
		Status: "in_progress", CandidateCount: 1, AwaitingPromotion: 1, CandidateState: "reviewed", ReviewState: "reviewed", ApprovalState: "approved", PromotionState: "awaiting_promotion",
		Requirements: GuidedPlanningFamilySection{Count: 1, AwaitingPromotion: 1, State: "approved"},
	}
	promotedRequirements := GuidedPlanningSection{
		Status: "promoted", CandidateCount: 1, Promoted: 1, CandidateState: "promoted", ReviewState: "reviewed", ApprovalState: "approved", PromotionState: "promoted",
		Requirements: GuidedPlanningFamilySection{Count: 1, Promoted: 1, State: "promoted"},
	}
	admittedSharedDesign := GuidedPlanningSection{
		Status: "in_progress", CandidateCount: 1, AwaitingReview: 1, CandidateState: "admitted", ReviewState: "awaiting_review", ApprovalState: "none", PromotionState: "none",
		SharedDesign: GuidedPlanningFamilySection{Count: 1, AwaitingReview: 1, State: "admitted"},
	}
	approvedSharedDesign := GuidedPlanningSection{
		Status: "in_progress", CandidateCount: 1, AwaitingPromotion: 1, CandidateState: "reviewed", ReviewState: "reviewed", ApprovalState: "approved", PromotionState: "awaiting_promotion",
		SharedDesign: GuidedPlanningFamilySection{Count: 1, AwaitingPromotion: 1, State: "approved"},
	}
	cases := []struct {
		name        string
		state       GuidedJourneyState
		wantPrimary GuidedFeatureAction
		wantCount   int
		wantApprove bool
	}{
		{"requirements admitted requires review", GuidedJourneyState{State: DiscoveryStateClosed, Destination: DiscoveryDestinationRequirements, HasCurrentRevision: true, Planning: admittedRequirements}, GuidedActionReviewPlanningCandidate, 1, false},
		{"requirements approved promotes server-side", GuidedJourneyState{State: DiscoveryStateClosed, Destination: DiscoveryDestinationRequirements, HasCurrentRevision: true, Planning: approvedRequirements}, GuidedActionPromotePlanningCandidate, 1, false},
		{"requirements promoted advances to delivery ticket", GuidedJourneyState{State: DiscoveryStateClosed, Destination: DiscoveryDestinationRequirements, HasCurrentRevision: true, Planning: promotedRequirements}, GuidedActionAuthorDeliveryTicket, 1, false},
		{"shared design admitted requires review", GuidedJourneyState{State: DiscoveryStateClosed, Destination: DiscoveryDestinationSharedDesign, HasCurrentRevision: true, Planning: admittedSharedDesign}, GuidedActionReviewPlanningCandidate, 1, false},
		{"shared design approved promotes server-side", GuidedJourneyState{State: DiscoveryStateClosed, Destination: DiscoveryDestinationSharedDesign, HasCurrentRevision: true, Planning: approvedSharedDesign}, GuidedActionPromotePlanningCandidate, 1, false},
		{"requirements then design requirements promoted authors design", GuidedJourneyState{State: DiscoveryStateClosed, Destination: DiscoveryDestinationRequirementsThenSharedDesign, HasCurrentRevision: true, Planning: promotedRequirements}, GuidedActionAuthorSharedDesign, 1, false},
		{"requirements then design requirements admitted reviews", GuidedJourneyState{State: DiscoveryStateClosed, Destination: DiscoveryDestinationRequirementsThenSharedDesign, HasCurrentRevision: true, Planning: admittedRequirements}, GuidedActionReviewPlanningCandidate, 1, false},
		{"requirements then design shared design admitted reviews", GuidedJourneyState{State: DiscoveryStateClosed, Destination: DiscoveryDestinationRequirementsThenSharedDesign, HasCurrentRevision: true, Planning: GuidedPlanningSection{
			Status: "in_progress", CandidateCount: 2, Promoted: 1, AwaitingReview: 1, CandidateState: "admitted", ReviewState: "awaiting_review", ApprovalState: "approved", PromotionState: "awaiting_promotion",
			Requirements: GuidedPlanningFamilySection{Count: 1, Promoted: 1, State: "promoted"},
			SharedDesign: GuidedPlanningFamilySection{Count: 1, AwaitingReview: 1, State: "admitted"},
		}}, GuidedActionReviewPlanningCandidate, 1, false},
		{"requirements then design both promoted advances to ticket", GuidedJourneyState{State: DiscoveryStateClosed, Destination: DiscoveryDestinationRequirementsThenSharedDesign, HasCurrentRevision: true, Planning: GuidedPlanningSection{
			Status: "promoted", CandidateCount: 2, Promoted: 2, CandidateState: "promoted", ReviewState: "reviewed", ApprovalState: "approved", PromotionState: "promoted",
			Requirements: GuidedPlanningFamilySection{Count: 1, Promoted: 1, State: "promoted"},
			SharedDesign: GuidedPlanningFamilySection{Count: 1, Promoted: 1, State: "promoted"},
		}}, GuidedActionAuthorDeliveryTicket, 1, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := DecideGuidedFeatureAction(tc.state, current, GuidedCompletion{})
			if got.PrimaryAction != tc.wantPrimary || len(got.AvailableActions) != tc.wantCount || !got.AvailableActions[0].Primary || got.AvailableActions[0].Action != tc.wantPrimary || !got.AvailableActions[0].Enabled {
				t.Fatalf("decision=%+v", got)
			}
			primaryCount := 0
			for _, action := range got.AvailableActions {
				if action.Primary {
					primaryCount++
				}
			}
			if primaryCount != 1 {
				t.Fatalf("decision has %d primary actions: %+v", primaryCount, got.AvailableActions)
			}
			if tc.wantApprove {
				approve := guidedAvailableAction(got.AvailableActions, GuidedActionApprovePlanningCandidate)
				if approve == nil || !approve.Enabled || !approve.RequiresConfirmation {
					t.Fatalf("approve availability=%+v", approve)
				}
			} else if guidedAvailableAction(got.AvailableActions, GuidedActionApprovePlanningCandidate) != nil {
				t.Fatalf("unexpected approve availability: %+v", got.AvailableActions)
			}
		})
	}
}

func guidedAvailableAction(available []GuidedFeatureActionAvailability, wanted GuidedFeatureAction) *GuidedFeatureActionAvailability {
	for i := range available {
		if available[i].Action == wanted {
			return &available[i]
		}
	}
	return nil
}
