package speccompiler

func ProjectTransitionPlan(document *TransitionPlanDocument) (TransitionPlanProjection, []Diagnostic) {
	if document == nil {
		return TransitionPlanProjection{}, []Diagnostic{{Code: "projection_invariant", Path: "", Message: "Transition Plan document is required."}}
	}
	projection := TransitionPlanProjection{
		FeatureSlug:           document.FeatureSlug,
		TicketID:              document.TicketID,
		TicketRevision:        document.TicketRevision,
		CutoverPrerequisites:  append([]string(nil), document.CutoverPrerequisites...),
		ActivationObligations: append([]string(nil), document.ActivationObligations...),
		RollbackEligibility:   document.RollbackEligibility,
		RollbackObligations:   append([]string(nil), document.RollbackObligations...),
		CompletionCriteria:    append([]string(nil), document.CompletionCriteria...),
	}
	if projection.RollbackEligibility == "eligible" && len(projection.RollbackObligations) == 0 {
		return TransitionPlanProjection{}, []Diagnostic{{Code: "projection_invariant", Path: "/rollback_obligations", Message: "Eligible rollback must declare at least one rollback obligation before the boundary."}}
	}
	if projection.RollbackEligibility == "not_eligible" && len(projection.RollbackObligations) != 0 {
		return TransitionPlanProjection{}, []Diagnostic{{Code: "projection_invariant", Path: "/rollback_obligations", Message: "A one-way transition boundary must not declare rollback obligations."}}
	}
	return projection, nil
}
