package speccompiler

import "encoding/json"

type scopeModel struct {
	InScope    []string `json:"in_scope"`
	OutOfScope []string `json:"out_of_scope"`
}

type DeliveryTicketDocument struct {
	FeatureSlug               string                            `json:"feature_slug"`
	TicketID                  string                            `json:"ticket_id"`
	Revision                  int64                             `json:"revision"`
	ReplacesRevision          *int64                            `json:"replaces_revision"`
	RepoTarget                string                            `json:"repo_target"`
	Branch                    string                            `json:"branch"`
	BaseCommit                string                            `json:"base_commit"`
	Goal                      string                            `json:"goal"`
	Context                   string                            `json:"context"`
	Scope                     scopeModel                        `json:"scope"`
	DependsOn                 []DeliveryTicketDependency        `json:"depends_on"`
	RequiredInvariants        []string                          `json:"required_invariants"`
	ForbiddenBehaviors        []string                          `json:"forbidden_behaviors"`
	ImplementationObligations []DeliveryTicketObligation        `json:"implementation_obligations"`
	ProofObligations          []string                          `json:"proof_obligations"`
	ValidationCommands        []DeliveryTicketValidationCommand `json:"validation_commands"`
	TransitionApplicability   string                            `json:"transition_applicability"`
	ExplicitDeferrals         []string                          `json:"explicit_deferrals"`
	Cancellation              *DeliveryTicketCancellation       `json:"cancellation,omitempty"`
	Completion                []string                          `json:"completion_criteria"`
}

type DeliveryTicketDependency struct {
	TicketID string `json:"ticket_id"`
	Revision int64  `json:"revision"`
}

type DeliveryTicketObligation struct {
	SourceArea    *string  `json:"source_area"`
	Obligation    string   `json:"obligation"`
	Prerequisites []string `json:"prerequisites"`
}

type DeliveryTicketValidationCommand struct {
	WorkingDirectory string `json:"working_directory"`
	Command          string `json:"command"`
	Expected         string `json:"expected"`
}

type DeliveryTicketCancellation struct {
	Reason string `json:"reason"`
}

type TransitionPlanDocument struct {
	FeatureSlug           string   `json:"feature_slug"`
	TicketID              string   `json:"ticket_id"`
	TicketRevision        int64    `json:"ticket_revision"`
	CutoverPrerequisites  []string `json:"cutover_prerequisites"`
	ActivationObligations []string `json:"activation_obligations"`
	RollbackEligibility   string   `json:"rollback_eligibility"`
	RollbackObligations   []string `json:"rollback_obligations"`
	CompletionCriteria    []string `json:"completion_criteria"`
}

type TransitionPlanProjection struct {
	FeatureSlug           string
	TicketID              string
	TicketRevision        int64
	CutoverPrerequisites  []string
	ActivationObligations []string
	RollbackEligibility   string
	RollbackObligations   []string
	CompletionCriteria    []string
}

func decodeDeliveryTicketDocument(raw []byte) (*DeliveryTicketDocument, error) {
	var document DeliveryTicketDocument
	if err := json.Unmarshal(raw, &document); err != nil {
		return nil, err
	}
	return &document, nil
}

func decodeTransitionPlanDocument(raw []byte) (*TransitionPlanDocument, error) {
	var document TransitionPlanDocument
	if err := json.Unmarshal(raw, &document); err != nil {
		return nil, err
	}
	return &document, nil
}

type planModel struct {
	SchemaVersion json.RawMessage         `json:"schema_version"`
	FeatureSlug   string                  `json:"feature_slug"`
	Goal          string                  `json:"goal"`
	Context       string                  `json:"context"`
	Scope         scopeModel              `json:"scope"`
	RepoTargets   []repositoryTargetModel `json:"repo_targets"`
	Passes        []passModel             `json:"passes"`
	Completion    []string                `json:"completion_criteria"`
}

type repositoryTargetModel struct {
	RepoTarget         string `json:"repo_target"`
	Branch             string `json:"branch"`
	PlanningBaseCommit string `json:"planning_base_commit"`
}

type passModel struct {
	Number           int                 `json:"number"`
	Name             string              `json:"name"`
	RepoTarget       string              `json:"repo_target"`
	Goal             string              `json:"goal"`
	Context          string              `json:"context"`
	Scope            scopeModel          `json:"scope"`
	DependsOn        []int               `json:"depends_on"`
	Outcomes         []string            `json:"outcomes"`
	SourceTargets    []sourceTargetModel `json:"source_targets"`
	ValidationIntent []string            `json:"validation_intent"`
	Completion       []string            `json:"completion_criteria"`
}

type sourceTargetModel struct {
	Path    string `json:"path"`
	Purpose string `json:"purpose"`
}
