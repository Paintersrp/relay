package plans

import "relay/internal/store"

const defaultSchemaPath = "relay-contracts/schema/planner_pass_plan.schema.json"

const (
	IssuePlanJSONSyntax                 = "PLAN_JSON_SYNTAX"
	IssuePlanSchemaInvalid              = "PLAN_SCHEMA_INVALID"
	IssuePlanSecretDetected             = "PLAN_SECRET_DETECTED"
	IssuePlanStatusInvalidForSubmission = "PLAN_STATUS_INVALID_FOR_SUBMISSION"
	IssuePlanPassStatusInvalid          = "PLAN_PASS_STATUS_INVALID_FOR_SUBMISSION"
	IssuePlanDuplicatePlanID            = "PLAN_DUPLICATE_PLAN_ID"
	IssuePlanDuplicatePassID            = "PLAN_DUPLICATE_PASS_ID"
	IssuePlanDuplicateSequence          = "PLAN_DUPLICATE_SEQUENCE"
	IssuePlanDependencyUnknown          = "PLAN_DEPENDENCY_UNKNOWN"
	IssuePlanDependencySelf             = "PLAN_DEPENDENCY_SELF"
	IssuePlanDependencyDuplicate        = "PLAN_DEPENDENCY_DUPLICATE"
	IssuePlanEmptyRequiredValue         = "PLAN_EMPTY_REQUIRED_VALUE"
	IssuePlanEmptyRequiredArray         = "PLAN_EMPTY_REQUIRED_ARRAY"
	IssuePlanStorageFailed              = "PLAN_STORAGE_FAILED"
)

type PlannerPassPlan struct {
	PlanMeta     PlanMeta        `json:"plan_meta"`
	SourceIntent SourceIntent    `json:"source_intent"`
	Passes       []PlanPassInput `json:"passes"`
}

type PlanMeta struct {
	PlanID        string `json:"plan_id"`
	SchemaVersion string `json:"schema_version"`
	CreatedAt     string `json:"created_at"`
	Title         string `json:"title"`
	Goal          string `json:"goal"`
	RepoTarget    string `json:"repo_target"`
	BranchContext string `json:"branch_context"`
	Status        string `json:"status"`
}

type SourceIntent struct {
	Summary string `json:"summary"`
}

type PlanPassInput struct {
	PassID                 string   `json:"pass_id"`
	Sequence               int64    `json:"sequence"`
	Name                   string   `json:"name"`
	Goal                   string   `json:"goal"`
	IntendedExecutionScope []string `json:"intended_execution_scope"`
	NonGoals               []string `json:"non_goals"`
	Dependencies           []string `json:"dependencies"`
	Status                 string   `json:"status"`
}

type SubmitPlanRequest struct {
	RawJSON            []byte
	SourceArtifactPath string
}

type SubmitPlanResult struct {
	Plan   store.Plan
	Passes []store.PlanPass
	Report PlanValidationReport
}

type PlanValidationReport struct {
	Valid  bool                  `json:"valid"`
	Issues []PlanValidationIssue `json:"issues"`
}

type PlanValidationIssue struct {
	Code    string `json:"code"`
	Path    string `json:"path,omitempty"`
	Message string `json:"message"`
}
