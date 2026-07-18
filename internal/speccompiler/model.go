package speccompiler

import "encoding/json"

type scopeModel struct {
	InScope    []string `json:"in_scope"`
	OutOfScope []string `json:"out_of_scope"`
}

type ExecutionDocument struct {
	FeatureSlug string              `json:"feature_slug"`
	RepoTarget  string              `json:"repo_target"`
	Branch      string              `json:"branch"`
	BaseCommit  string              `json:"base_commit"`
	Goal        string              `json:"goal"`
	Context     string              `json:"context"`
	Scope       scopeModel          `json:"scope"`
	Steps       []ExecutionStep     `json:"steps"`
	Validation  ExecutionValidation `json:"validation"`
	Completion  []string            `json:"completion_criteria"`
}

type ExecutionStep struct {
	Number     int                `json:"number"`
	Goal       string             `json:"goal"`
	Substeps   []ExecutionSubstep `json:"substeps"`
	Completion []string           `json:"completion_criteria"`
}

type ExecutionSubstep struct {
	Number      int             `json:"number"`
	Instruction string          `json:"instruction"`
	DependsOn   []string        `json:"depends_on"`
	Atomic      *bool           `json:"atomic"`
	Files       []ExecutionFile `json:"files"`
	Completion  []string        `json:"completion_criteria"`
}

type ExecutionFile struct {
	Path            string                      `json:"path"`
	DestinationPath string                      `json:"destination_path"`
	Operation       string                      `json:"operation"`
	Purpose         string                      `json:"purpose"`
	Implementation  ExecutionFileImplementation `json:"implementation"`
}

type ExecutionFileImplementation struct {
	Changes         []ExecutionDirective `json:"changes"`
	Content         string               `json:"content"`
	DeleteFile      bool                 `json:"delete_file"`
	PreserveContent bool                 `json:"preserve_content"`
}

type ExecutionDirective struct {
	Kind                string `json:"kind"`
	OldText             string `json:"old_text"`
	NewText             string `json:"new_text"`
	Anchor              string `json:"anchor"`
	Content             string `json:"content"`
	ExpectedOccurrences int    `json:"expected_occurrences"`
}

type ExecutionValidation struct {
	Commands       []ExecutionValidationCommand `json:"commands"`
	ExecutorChecks []string                     `json:"executor_checks"`
}

type ExecutionValidationCommand struct {
	Command          string `json:"command"`
	WorkingDirectory string `json:"working_directory"`
	Expected         string `json:"expected"`
}

type DeliveryTicketDocument struct {
	FeatureSlug               string                      `json:"feature_slug"`
	TicketID                  string                      `json:"ticket_id"`
	Revision                  int64                       `json:"revision"`
	ReplacesRevision          *int64                      `json:"replaces_revision"`
	RepoTarget                string                      `json:"repo_target"`
	Branch                    string                      `json:"branch"`
	BaseCommit                string                      `json:"base_commit"`
	Goal                      string                      `json:"goal"`
	Context                   string                      `json:"context"`
	Scope                     scopeModel                  `json:"scope"`
	DependsOn                 []DeliveryTicketDependency  `json:"depends_on"`
	ImplementationObligations []DeliveryTicketObligation  `json:"implementation_obligations"`
	ValidationIntent          []string                    `json:"validation_intent"`
	TransitionApplicability   string                      `json:"transition_applicability"`
	Cancellation              *DeliveryTicketCancellation `json:"cancellation,omitempty"`
	Completion                []string                    `json:"completion_criteria"`
}

type DeliveryTicketDependency struct {
	TicketID string `json:"ticket_id"`
	Revision int64  `json:"revision"`
}

type DeliveryTicketObligation struct {
	Path       string `json:"path"`
	Obligation string `json:"obligation"`
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

type EffectiveBriefMode string

const (
	EffectiveBriefFull     EffectiveBriefMode = "full"
	EffectiveBriefResidual EffectiveBriefMode = "residual"
)

type EffectiveBriefSelection struct {
	Mode                  EffectiveBriefMode
	ResidualFileWorkRefs  []string
	CompletedFileWorkRefs []string
	ProtectedPaths        []string
}

func decodeExecutionDocument(raw []byte) (*ExecutionDocument, error) {
	var document ExecutionDocument
	if err := json.Unmarshal(raw, &document); err != nil {
		return nil, err
	}
	return &document, nil
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
