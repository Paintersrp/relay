package speccompiler

// DeliveryTicketProjection is the deterministic downstream-consumer projection
// of a validated Delivery Ticket v2.0 document. It preserves source order and
// carries every execution-relevant field without exposing compiler internals.
type DeliveryTicketProjection struct {
	FeatureSlug               string
	TicketID                  string
	Revision                  int64
	ReplacesRevision          *int64
	RepoTarget                string
	Branch                    string
	BaseCommit                string
	Goal                      string
	Context                   string
	Scope                     scopeModel
	DependsOn                 []DeliveryTicketDependency
	SharedDesignConstraints   []DeliveryTicketCrossMemberConstraint
	RequiredInvariants        []string
	ForbiddenBehaviors        []string
	ImplementationObligations []DeliveryTicketObligation
	ProofObligations          []string
	ValidationCommands        []DeliveryTicketValidationCommand
	TransitionApplicability   string
	ExplicitDeferrals         []string
	Cancellation              *DeliveryTicketCancellation
	Completion                []string
}

// ProjectDeliveryTicket produces the deterministic execution projection for a
// validated Delivery Ticket. It re-verifies the active/cancellation
// cardinality so downstream consumers receive only structurally complete
// tickets; a defect is returned as a projection_invariant diagnostic instead
// of a partial projection.
func ProjectDeliveryTicket(document *DeliveryTicketDocument) (DeliveryTicketProjection, []Diagnostic) {
	if document == nil {
		return DeliveryTicketProjection{}, []Diagnostic{{Code: "projection_invariant", Path: "", Message: "Delivery Ticket document is required."}}
	}
	projection := DeliveryTicketProjection{
		FeatureSlug:               document.FeatureSlug,
		TicketID:                  document.TicketID,
		Revision:                  document.Revision,
		ReplacesRevision:          document.ReplacesRevision,
		RepoTarget:                document.RepoTarget,
		Branch:                    document.Branch,
		BaseCommit:                document.BaseCommit,
		Goal:                      document.Goal,
		Context:                   document.Context,
		Scope:                     document.Scope,
		DependsOn:                 append([]DeliveryTicketDependency(nil), document.DependsOn...),
		SharedDesignConstraints:   append([]DeliveryTicketCrossMemberConstraint(nil), document.SharedDesignConstraints...),
		RequiredInvariants:        append([]string(nil), document.RequiredInvariants...),
		ForbiddenBehaviors:        append([]string(nil), document.ForbiddenBehaviors...),
		ImplementationObligations: append([]DeliveryTicketObligation(nil), document.ImplementationObligations...),
		ProofObligations:          append([]string(nil), document.ProofObligations...),
		ValidationCommands:        append([]DeliveryTicketValidationCommand(nil), document.ValidationCommands...),
		TransitionApplicability:   document.TransitionApplicability,
		ExplicitDeferrals:         append([]string(nil), document.ExplicitDeferrals...),
		Cancellation:              document.Cancellation,
		Completion:                append([]string(nil), document.Completion...),
	}
	if diagnostics := validateDeliveryTicketProjection(projection); len(diagnostics) != 0 {
		return DeliveryTicketProjection{}, diagnostics
	}
	return projection, nil
}

func validateDeliveryTicketProjection(projection DeliveryTicketProjection) []Diagnostic {
	for _, constraint := range projection.SharedDesignConstraints {
		if constraint.Kind != "atomicity" && constraint.Kind != "valid_intermediate_state" && constraint.Kind != "compatibility" && constraint.Kind != "required_invariant" && constraint.Kind != "forbidden_intermediate_state" {
			return []Diagnostic{{Code: "projection_invariant", Path: "/shared_design_constraints", Message: "Shared Design constraint kind is not supported."}}
		}
		if len(constraint.Requires) == 0 {
			return []Diagnostic{{Code: "projection_invariant", Path: "/shared_design_constraints", Message: "Shared Design constraints must require at least one Ticket."}}
		}
		for _, required := range constraint.Requires {
			if required.TicketID == "" || required.Revision < 1 {
				return []Diagnostic{{Code: "projection_invariant", Path: "/shared_design_constraints", Message: "Shared Design constraint references must identify a Ticket revision."}}
			}
		}
	}
	if projection.Cancellation != nil {
		var diagnostics []Diagnostic
		if len(projection.DependsOn) != 0 {
			diagnostics = append(diagnostics, Diagnostic{Code: "projection_invariant", Path: "/depends_on", Message: "Cancellation revisions must not declare dependencies."})
		}
		if len(projection.RequiredInvariants) != 0 {
			diagnostics = append(diagnostics, Diagnostic{Code: "projection_invariant", Path: "/required_invariants", Message: "Cancellation revisions must use an empty required_invariants array."})
		}
		if len(projection.ForbiddenBehaviors) != 0 {
			diagnostics = append(diagnostics, Diagnostic{Code: "projection_invariant", Path: "/forbidden_behaviors", Message: "Cancellation revisions must use an empty forbidden_behaviors array."})
		}
		if len(projection.ImplementationObligations) != 0 {
			diagnostics = append(diagnostics, Diagnostic{Code: "projection_invariant", Path: "/implementation_obligations", Message: "Cancellation revisions must not declare implementation obligations."})
		}
		if len(projection.ProofObligations) != 0 {
			diagnostics = append(diagnostics, Diagnostic{Code: "projection_invariant", Path: "/proof_obligations", Message: "Cancellation revisions must use an empty proof_obligations array."})
		}
		if len(projection.ValidationCommands) != 0 {
			diagnostics = append(diagnostics, Diagnostic{Code: "projection_invariant", Path: "/validation_commands", Message: "Cancellation revisions must use an empty validation_commands array."})
		}
		if projection.TransitionApplicability != "not_required" {
			diagnostics = append(diagnostics, Diagnostic{Code: "projection_invariant", Path: "/transition_applicability", Message: "Cancellation revisions must use not_required transition applicability."})
		}
		if len(projection.ExplicitDeferrals) != 0 {
			diagnostics = append(diagnostics, Diagnostic{Code: "projection_invariant", Path: "/explicit_deferrals", Message: "Cancellation revisions must use an empty explicit_deferrals array."})
		}
		return diagnostics
	}
	if len(projection.ImplementationObligations) == 0 {
		return []Diagnostic{{Code: "projection_invariant", Path: "/implementation_obligations", Message: "Active tickets must declare at least one implementation obligation."}}
	}
	if len(projection.ProofObligations) == 0 {
		return []Diagnostic{{Code: "projection_invariant", Path: "/proof_obligations", Message: "Active tickets must declare at least one proof obligation."}}
	}
	if len(projection.ValidationCommands) == 0 {
		return []Diagnostic{{Code: "projection_invariant", Path: "/validation_commands", Message: "Active tickets must declare at least one validation command."}}
	}
	return nil
}
