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
		wantActions int
	}{
		{"active discovery", GuidedJourneyState{State: DiscoveryStateActive, HasCurrentRevision: true}, FeatureCurrentnessDecision{Readiness: FeatureCurrent}, GuidedCompletion{}, GuidedActionContinueDiscovery, true, 1},
		{"closure ready", GuidedJourneyState{State: DiscoveryStateActive, HasCurrentRevision: true, Destination: DiscoveryDestinationRequirements}, FeatureCurrentnessDecision{Readiness: FeatureCurrent}, GuidedCompletion{}, GuidedActionCloseDiscovery, true, 1},
		{"closed completion ready", GuidedJourneyState{State: DiscoveryStateClosed}, FeatureCurrentnessDecision{Readiness: FeatureCurrent}, GuidedCompletion{Gates: []GuidedCompletionGate{{Name: "all", Ready: true}}}, GuidedActionCompleteFeature, true, 2},
		{"direct delivery ticket", GuidedJourneyState{State: DiscoveryStateClosed, Destination: DiscoveryDestinationDirectDeliveryTicket}, FeatureCurrentnessDecision{Readiness: FeatureCurrent}, GuidedCompletion{}, GuidedActionAuthorDeliveryTicket, true, 1},
		{"requirements authoring", GuidedJourneyState{State: DiscoveryStateClosed, Destination: DiscoveryDestinationRequirements}, FeatureCurrentnessDecision{Readiness: FeatureCurrent}, GuidedCompletion{}, GuidedActionAuthorRequirements, true, 1},
		{"requirements ticket frontier", GuidedJourneyState{State: DiscoveryStateClosed, Destination: DiscoveryDestinationRequirements, AuthorityLayers: []string{"requirements"}}, FeatureCurrentnessDecision{Readiness: FeatureCurrent}, GuidedCompletion{}, GuidedActionAuthorDeliveryTicket, true, 1},
		{"shared design authoring", GuidedJourneyState{State: DiscoveryStateClosed, Destination: DiscoveryDestinationSharedDesign}, FeatureCurrentnessDecision{Readiness: FeatureCurrent}, GuidedCompletion{}, GuidedActionAuthorSharedDesign, true, 1},
		{"requirements then design", GuidedJourneyState{State: DiscoveryStateClosed, Destination: DiscoveryDestinationRequirementsThenSharedDesign, AuthorityLayers: []string{"requirements"}}, FeatureCurrentnessDecision{Readiness: FeatureCurrent}, GuidedCompletion{}, GuidedActionAuthorSharedDesign, true, 1},
		{"requirements then design ticket frontier", GuidedJourneyState{State: DiscoveryStateClosed, Destination: DiscoveryDestinationRequirementsThenSharedDesign, AuthorityLayers: []string{"requirements", "shared_design"}}, FeatureCurrentnessDecision{Readiness: FeatureCurrent}, GuidedCompletion{}, GuidedActionAuthorDeliveryTicket, true, 1},
		{"existing route continuation", GuidedJourneyState{State: DiscoveryStateClosed, Destination: DiscoveryDestinationExistingRouteContinuation, Continuation: "resume package"}, FeatureCurrentnessDecision{Readiness: FeatureCurrent}, GuidedCompletion{}, GuidedActionContinueEstablishedRoute, true, 1},

		{"closed completion blocked", GuidedJourneyState{State: DiscoveryStateClosed}, FeatureCurrentnessDecision{Readiness: FeatureCurrent}, GuidedCompletion{Gates: []GuidedCompletionGate{{Name: "audit", Ready: false}}}, GuidedActionCompleteFeature, false, 1},
		{"completion recorded reopens discovery", GuidedJourneyState{State: DiscoveryStateClosed}, FeatureCurrentnessDecision{Readiness: FeatureCurrent}, GuidedCompletion{Recorded: true}, GuidedActionReopenDiscovery, true, 1},
		{"stale recovery", GuidedJourneyState{State: DiscoveryStateClosed, Destination: DiscoveryDestinationDirectDeliveryTicket}, FeatureCurrentnessDecision{Readiness: FeatureStale, RecoveryCategory: "replace_current_closure"}, GuidedCompletion{}, GuidedActionContinueDiscovery, false, 1},
		{"legacy recovery", GuidedJourneyState{State: DiscoveryStateActive, HasCurrentRevision: true}, FeatureCurrentnessDecision{Readiness: FeatureLegacy, RecoveryCategory: "adopt_discovery_lifecycle"}, GuidedCompletion{}, GuidedActionLegacyRecovery, true, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := DecideGuidedFeatureAction(tc.state, tc.currentness, tc.completion)
			if got.PrimaryAction != tc.want || len(got.AvailableActions) != tc.wantActions || !got.AvailableActions[0].Primary || got.AvailableActions[0].Action != tc.want || got.AvailableActions[0].Enabled != tc.enabled {
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
		})
	}
}

// TestDecideGuidedFeatureActionAddsConfirmedAbandonmentSecondaryOnlyWhenCompletionReady
// asserts the abandonment secondary appears exactly when completion is the
// primary action, no current decision is recorded, and the server gate matrix
// is ready. The primary action remains complete_feature and the secondary is
// the only accepted alternative.
func TestDecideGuidedFeatureActionAddsConfirmedAbandonmentSecondaryOnlyWhenCompletionReady(t *testing.T) {
	current := FeatureCurrentnessDecision{Readiness: FeatureCurrent}
	ready := GuidedCompletion{Gates: []GuidedCompletionGate{{Name: "closure", Ready: true}, {Name: "authority", Ready: true}, {Name: "tickets", Ready: true}, {Name: "integration", Ready: true}, {Name: "transitions", Ready: true}, {Name: "package", Ready: true}, {Name: "remediation", Ready: true}, {Name: "audit", Ready: true}}}
	blocked := GuidedCompletion{Gates: []GuidedCompletionGate{{Name: "audit", Ready: false}}}
	recorded := GuidedCompletion{Recorded: true}

	closedState := GuidedJourneyState{State: DiscoveryStateClosed}
	completedRun := GuidedJourneyState{State: DiscoveryStateClosed, Destination: DiscoveryDestinationDirectDeliveryTicket, Delivery: GuidedDeliverySection{SelectionState: "consumed", PackageState: "approved", RunState: "completed"}}

	for _, tc := range []struct {
		name       string
		state      GuidedJourneyState
		completion GuidedCompletion
		primary    GuidedFeatureAction
		secondary  GuidedFeatureAction
	}{
		{"no delivery work ready", closedState, ready, GuidedActionCompleteFeature, GuidedActionAbandonFeature},
		{"no delivery work blocked", closedState, blocked, GuidedActionCompleteFeature, ""},
		{"no delivery work recorded reopens", closedState, recorded, GuidedActionReopenDiscovery, ""},
		{"completed run ready", completedRun, ready, GuidedActionCompleteFeature, GuidedActionAbandonFeature},
		{"completed run recorded has no secondary", completedRun, recorded, GuidedActionCompleteFeature, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := DecideGuidedFeatureAction(tc.state, current, tc.completion)
			if got.PrimaryAction != tc.primary || len(got.AvailableActions) == 0 || !got.AvailableActions[0].Primary || got.AvailableActions[0].Action != tc.primary {
				t.Fatalf("primary decision=%+v", got)
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
			if tc.secondary == "" {
				for _, action := range got.AvailableActions {
					if action.Action == GuidedActionAbandonFeature {
						t.Fatalf("unexpected abandonment secondary: %+v", got.AvailableActions)
					}
				}
				return
			}
			secondary := got.AvailableActions[1]
			if secondary.Action != tc.secondary || secondary.Primary || !secondary.Enabled || !secondary.RequiresConfirmation {
				t.Fatalf("abandonment secondary=%+v want enabled confirmed %s", secondary, tc.secondary)
			}
			if len(got.AvailableActions) != 2 {
				t.Fatalf("available actions=%+v", got.AvailableActions)
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
	reviewedRequirements := GuidedPlanningSection{
		Status: "in_progress", CandidateCount: 1, AwaitingApproval: 1, CandidateState: "reviewed", ReviewState: "reviewed", ApprovalState: "awaiting_approval", PromotionState: "none",
		Requirements: GuidedPlanningFamilySection{Count: 1, AwaitingApproval: 1, State: "reviewed"},
	}
	reviewedSharedDesign := GuidedPlanningSection{
		Status: "in_progress", CandidateCount: 1, AwaitingApproval: 1, CandidateState: "reviewed", ReviewState: "reviewed", ApprovalState: "awaiting_approval", PromotionState: "none",
		SharedDesign: GuidedPlanningFamilySection{Count: 1, AwaitingApproval: 1, State: "reviewed"},
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
	}{
		{"requirements admitted requires review", GuidedJourneyState{State: DiscoveryStateClosed, Destination: DiscoveryDestinationRequirements, HasCurrentRevision: true, Planning: admittedRequirements}, GuidedActionReviewPlanningCandidate, 1},
		{"requirements reviewed requires explicit approval", GuidedJourneyState{State: DiscoveryStateClosed, Destination: DiscoveryDestinationRequirements, HasCurrentRevision: true, Planning: reviewedRequirements}, GuidedActionApprovePlanningCandidate, 1},
		{"requirements approved promotes server-side", GuidedJourneyState{State: DiscoveryStateClosed, Destination: DiscoveryDestinationRequirements, HasCurrentRevision: true, Planning: approvedRequirements}, GuidedActionPromotePlanningCandidate, 1},
		{"requirements promoted advances to delivery ticket", GuidedJourneyState{State: DiscoveryStateClosed, Destination: DiscoveryDestinationRequirements, HasCurrentRevision: true, Planning: promotedRequirements}, GuidedActionAuthorDeliveryTicket, 1},
		{"shared design admitted requires review", GuidedJourneyState{State: DiscoveryStateClosed, Destination: DiscoveryDestinationSharedDesign, HasCurrentRevision: true, Planning: admittedSharedDesign}, GuidedActionReviewPlanningCandidate, 1},
		{"shared design reviewed requires explicit approval", GuidedJourneyState{State: DiscoveryStateClosed, Destination: DiscoveryDestinationSharedDesign, HasCurrentRevision: true, Planning: reviewedSharedDesign}, GuidedActionApprovePlanningCandidate, 1},
		{"shared design approved promotes server-side", GuidedJourneyState{State: DiscoveryStateClosed, Destination: DiscoveryDestinationSharedDesign, HasCurrentRevision: true, Planning: approvedSharedDesign}, GuidedActionPromotePlanningCandidate, 1},
		{"requirements then design requirements promoted authors design", GuidedJourneyState{State: DiscoveryStateClosed, Destination: DiscoveryDestinationRequirementsThenSharedDesign, HasCurrentRevision: true, Planning: promotedRequirements}, GuidedActionAuthorSharedDesign, 1},
		{"requirements then design requirements admitted reviews", GuidedJourneyState{State: DiscoveryStateClosed, Destination: DiscoveryDestinationRequirementsThenSharedDesign, HasCurrentRevision: true, Planning: admittedRequirements}, GuidedActionReviewPlanningCandidate, 1},
		{"requirements then design shared design admitted reviews", GuidedJourneyState{State: DiscoveryStateClosed, Destination: DiscoveryDestinationRequirementsThenSharedDesign, HasCurrentRevision: true, Planning: GuidedPlanningSection{
			Status: "in_progress", CandidateCount: 2, Promoted: 1, AwaitingReview: 1, CandidateState: "admitted", ReviewState: "awaiting_review", ApprovalState: "approved", PromotionState: "awaiting_promotion",
			Requirements: GuidedPlanningFamilySection{Count: 1, Promoted: 1, State: "promoted"},
			SharedDesign: GuidedPlanningFamilySection{Count: 1, AwaitingReview: 1, State: "admitted"},
		}}, GuidedActionReviewPlanningCandidate, 1},
		{"requirements then design both promoted advances to ticket", GuidedJourneyState{State: DiscoveryStateClosed, Destination: DiscoveryDestinationRequirementsThenSharedDesign, HasCurrentRevision: true, Planning: GuidedPlanningSection{
			Status: "promoted", CandidateCount: 2, Promoted: 2, CandidateState: "promoted", ReviewState: "reviewed", ApprovalState: "approved", PromotionState: "promoted",
			Requirements: GuidedPlanningFamilySection{Count: 1, Promoted: 1, State: "promoted"},
			SharedDesign: GuidedPlanningFamilySection{Count: 1, Promoted: 1, State: "promoted"},
		}}, GuidedActionAuthorDeliveryTicket, 1},
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
		})
	}
}

func TestDecideGuidedFeatureActionEmitsPreciseDeliveryPrimaryActions(t *testing.T) {
	current := FeatureCurrentnessDecision{Readiness: FeatureCurrent}
	frontier := []GuidedFrontierEntry{{TicketID: "P5-T1", RevisionNumber: 1, ExternalPriority: 50}}
	delivery := func(mutate func(*GuidedDeliverySection)) GuidedDeliverySection {
		section := GuidedDeliverySection{SelectionState: "none", PackageState: "none", RunState: "none", AuditState: "none", RemediationState: "none"}
		mutate(&section)
		return section
	}
	cases := []struct {
		name    string
		state   GuidedJourneyState
		want    GuidedFeatureAction
		enabled bool
		wantLen int
	}{
		{"frontier ready selects server-side", GuidedJourneyState{State: DiscoveryStateClosed, Destination: DiscoveryDestinationDirectDeliveryTicket, Delivery: delivery(func(d *GuidedDeliverySection) { d.Frontier = frontier })}, GuidedActionSelectDeliveryTicket, true, 1},
		{"no frontier authors ticket", GuidedJourneyState{State: DiscoveryStateClosed, Destination: DiscoveryDestinationDirectDeliveryTicket}, GuidedActionAuthorDeliveryTicket, true, 1},
		{"active selection prepares package", GuidedJourneyState{State: DiscoveryStateClosed, Destination: DiscoveryDestinationDirectDeliveryTicket, Delivery: delivery(func(d *GuidedDeliverySection) { d.SelectionState = "active" })}, GuidedActionPreparePackage, true, 1},
		{"active selection with package state prepares", GuidedJourneyState{State: DiscoveryStateClosed, Destination: DiscoveryDestinationDirectDeliveryTicket, Delivery: delivery(func(d *GuidedDeliverySection) { d.SelectionState = "active"; d.PackageState = "none" })}, GuidedActionPreparePackage, true, 1},
		{"prepared package approves server-side", GuidedJourneyState{State: DiscoveryStateClosed, Destination: DiscoveryDestinationDirectDeliveryTicket, Delivery: delivery(func(d *GuidedDeliverySection) {
			d.SelectionState = "active"
			d.PackageState = "prepared"
		})}, GuidedActionApprovePackage, true, 1},
		{"approved package launches run", GuidedJourneyState{State: DiscoveryStateClosed, Destination: DiscoveryDestinationDirectDeliveryTicket, Delivery: delivery(func(d *GuidedDeliverySection) {
			d.SelectionState = "consumed"
			d.PackageState = "approved"
			d.RunState = "setup_ready"
		})}, GuidedActionLaunchRun, true, 1},
		{"executing run continues execution", GuidedJourneyState{State: DiscoveryStateClosed, Destination: DiscoveryDestinationDirectDeliveryTicket, Delivery: delivery(func(d *GuidedDeliverySection) {
			d.SelectionState = "consumed"
			d.PackageState = "approved"
			d.RunState = "executing"
		})}, GuidedActionContinueRun, true, 1},
		{"failed run recovers through run owner", GuidedJourneyState{State: DiscoveryStateClosed, Destination: DiscoveryDestinationDirectDeliveryTicket, Delivery: delivery(func(d *GuidedDeliverySection) {
			d.SelectionState = "consumed"
			d.PackageState = "approved"
			d.RunState = "execution_failed"
		})}, GuidedActionRecoverRun, true, 1},
		{"cancelled run recovers through run owner", GuidedJourneyState{State: DiscoveryStateClosed, Destination: DiscoveryDestinationDirectDeliveryTicket, Delivery: delivery(func(d *GuidedDeliverySection) {
			d.SelectionState = "consumed"
			d.PackageState = "approved"
			d.RunState = "cancelled"
		})}, GuidedActionRecoverRun, true, 1},
		{"needs revision run enters remediation", GuidedJourneyState{State: DiscoveryStateClosed, Destination: DiscoveryDestinationDirectDeliveryTicket, Delivery: delivery(func(d *GuidedDeliverySection) {
			d.SelectionState = "consumed"
			d.PackageState = "approved"
			d.RunState = "needs_revision"
		})}, GuidedActionRemediate, true, 1},
		{"validating run prepares audit", GuidedJourneyState{State: DiscoveryStateClosed, Destination: DiscoveryDestinationDirectDeliveryTicket, Delivery: delivery(func(d *GuidedDeliverySection) {
			d.SelectionState = "consumed"
			d.PackageState = "approved"
			d.RunState = "audit_ready"
			d.AuditState = "awaiting_audit"
		})}, GuidedActionPrepareAudit, true, 1},
		{"audit packet recorded records decision", GuidedJourneyState{State: DiscoveryStateClosed, Destination: DiscoveryDestinationDirectDeliveryTicket, Delivery: delivery(func(d *GuidedDeliverySection) {
			d.SelectionState = "consumed"
			d.PackageState = "approved"
			d.RunState = "audit_ready"
			d.AuditState = "packet_recorded"
		})}, GuidedActionRecordAuditDecision, true, 1},
		{"open remediation publishes replacement", GuidedJourneyState{State: DiscoveryStateClosed, Destination: DiscoveryDestinationDirectDeliveryTicket, Delivery: delivery(func(d *GuidedDeliverySection) { d.RemediationState = "open" })}, GuidedActionRemediate, true, 1},
		{"completed run completes feature", GuidedJourneyState{State: DiscoveryStateClosed, Destination: DiscoveryDestinationDirectDeliveryTicket, Delivery: delivery(func(d *GuidedDeliverySection) {
			d.SelectionState = "consumed"
			d.PackageState = "approved"
			d.RunState = "completed"
		})}, GuidedActionCompleteFeature, true, 2},
		{"requirements promoted with frontier selects", GuidedJourneyState{State: DiscoveryStateClosed, Destination: DiscoveryDestinationRequirements, HasCurrentRevision: true, Planning: GuidedPlanningSection{Status: "promoted", CandidateCount: 1, Promoted: 1, Requirements: GuidedPlanningFamilySection{Count: 1, Promoted: 1, State: "promoted"}}, Delivery: delivery(func(d *GuidedDeliverySection) { d.Frontier = frontier })}, GuidedActionSelectDeliveryTicket, true, 1},
		{"existing route with delivery stage selects", GuidedJourneyState{State: DiscoveryStateClosed, Destination: DiscoveryDestinationExistingRouteContinuation, Continuation: "resume package", Delivery: delivery(func(d *GuidedDeliverySection) { d.Frontier = frontier })}, GuidedActionSelectDeliveryTicket, true, 1},
		{"existing route with active selection prepares", GuidedJourneyState{State: DiscoveryStateClosed, Destination: DiscoveryDestinationExistingRouteContinuation, Continuation: "resume package", Delivery: delivery(func(d *GuidedDeliverySection) { d.SelectionState = "active" })}, GuidedActionPreparePackage, true, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := DecideGuidedFeatureAction(tc.state, current, GuidedCompletion{})
			if got.PrimaryAction != tc.want || len(got.AvailableActions) != tc.wantLen || !got.AvailableActions[0].Primary || got.AvailableActions[0].Action != tc.want || got.AvailableActions[0].Enabled != tc.enabled {
				t.Fatalf("decision=%+v", got)
			}
			if tc.wantLen > 1 && (got.AvailableActions[1].Action != GuidedActionAbandonFeature || !got.AvailableActions[1].Enabled || !got.AvailableActions[1].RequiresConfirmation) {
				t.Fatalf("secondary abandonment availability=%+v", got.AvailableActions[1])
			}
		})
	}
}

func TestDecideGuidedFeatureActionActiveSelectionPreparesPackage(t *testing.T) {
	current := FeatureCurrentnessDecision{Readiness: FeatureCurrent}
	state := GuidedJourneyState{State: DiscoveryStateClosed, Destination: DiscoveryDestinationDirectDeliveryTicket, Delivery: GuidedDeliverySection{SelectionState: "active"}}
	got := DecideGuidedFeatureAction(state, current, GuidedCompletion{})
	if got.PrimaryAction != GuidedActionPreparePackage || len(got.AvailableActions) != 1 ||
		!got.AvailableActions[0].Primary || !got.AvailableActions[0].Enabled || got.AvailableActions[0].RequiresConfirmation {
		t.Fatalf("active selection decision=%+v", got)
	}
}

func TestGuidedPlanningReviewDispositionNeverTreatsReviewExistenceAsApproval(t *testing.T) {
	current := FeatureCurrentnessDecision{Readiness: FeatureCurrent}
	cases := []struct {
		name  string
		state GuidedJourneyState
		want  GuidedFeatureAction
	}{
		{"existing route exposes delivery review", GuidedJourneyState{State: DiscoveryStateClosed, Destination: DiscoveryDestinationExistingRouteContinuation, Planning: GuidedPlanningSection{Status: "in_progress", CandidateCount: 1, DeliveryTicket: GuidedPlanningFamilySection{Count: 1, AwaitingReview: 1, State: "admitted"}}}, GuidedActionReviewPlanningCandidate},
		{"existing route exposes delivery explicit approval", GuidedJourneyState{State: DiscoveryStateClosed, Destination: DiscoveryDestinationExistingRouteContinuation, Planning: GuidedPlanningSection{Status: "in_progress", CandidateCount: 1, DeliveryTicket: GuidedPlanningFamilySection{Count: 1, AwaitingApproval: 1, State: "reviewed"}}}, GuidedActionApprovePlanningCandidate},
		{"existing route exposes delivery production", GuidedJourneyState{State: DiscoveryStateClosed, Destination: DiscoveryDestinationExistingRouteContinuation, Planning: GuidedPlanningSection{Status: "in_progress", CandidateCount: 1, DeliveryTicket: GuidedPlanningFamilySection{Count: 1, AwaitingPromotion: 1, State: "approved"}}}, GuidedActionPromotePlanningCandidate},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			decision := DecideGuidedFeatureAction(tc.state, current, GuidedCompletion{})
			if decision.PrimaryAction != tc.want || len(decision.AvailableActions) != 1 || decision.AvailableActions[0].Action != tc.want {
				t.Fatalf("decision=%+v", decision)
			}
		})
	}
}

func TestDecideGuidedFeatureActionEmitsPrototypePrimaryActions(t *testing.T) {
	current := FeatureCurrentnessDecision{Readiness: FeatureCurrent}
	prototype := func(run, cleanup, qa string) GuidedPrototypeSection {
		return GuidedPrototypeSection{RunState: run, CleanupState: cleanup, QAState: qa, EvidenceState: "none"}
	}
	cases := []struct {
		name    string
		state   GuidedJourneyState
		want    GuidedFeatureAction
		enabled bool
	}{
		{"proposed run launches prototype", GuidedJourneyState{State: DiscoveryStateClosed, Destination: DiscoveryDestinationDirectDeliveryTicket, Prototype: prototype("proposed", "none", "none")}, GuidedActionPrototypeExecute, true},
		{"pending cleanup reconciles", GuidedJourneyState{State: DiscoveryStateClosed, Destination: DiscoveryDestinationDirectDeliveryTicket, Prototype: prototype("cleanup_required", "pending", "none")}, GuidedActionPrototypeCleanup, true},
		{"closed run prepares QA", GuidedJourneyState{State: DiscoveryStateClosed, Destination: DiscoveryDestinationDirectDeliveryTicket, Prototype: prototype("closed", "complete", "prepared")}, GuidedActionPrototypeQA, true},
		{"admitted QA resumes journey", GuidedJourneyState{State: DiscoveryStateClosed, Destination: DiscoveryDestinationDirectDeliveryTicket, Prototype: prototype("closed", "complete", "admitted")}, GuidedActionAuthorDeliveryTicket, true},
		{"running run continues journey", GuidedJourneyState{State: DiscoveryStateClosed, Destination: DiscoveryDestinationDirectDeliveryTicket, Prototype: prototype("running", "none", "none")}, GuidedActionAuthorDeliveryTicket, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := DecideGuidedFeatureAction(tc.state, current, GuidedCompletion{})
			if got.PrimaryAction != tc.want || len(got.AvailableActions) != 1 || !got.AvailableActions[0].Primary || got.AvailableActions[0].Action != tc.want || got.AvailableActions[0].Enabled != tc.enabled {
				t.Fatalf("decision=%+v", got)
			}
		})
	}
}
