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
	IssuePlanProjectRequired            = "PLAN_PROJECT_REQUIRED"
	IssuePlanProjectUnknown             = "PLAN_PROJECT_UNKNOWN"
	IssuePlanPassStatusInvalidRuntime   = "PLAN_PASS_STATUS_INVALID_RUNTIME"
)

const (
	StatusPassPlanned          = "planned"
	StatusPassReadyForPlanner  = "ready_for_planner"
	StatusPassHandoffReady     = "handoff_ready"
	StatusPassRunCreated       = "run_created"
	StatusPassInProgress       = "in_progress"
	StatusPassAuditReady       = "audit_ready"
	StatusPassCompleted        = "completed"
	StatusPassRevisionRequired = "revision_required"
	StatusPassBlocked          = "blocked"
	StatusPassSkipped          = "skipped"
)

type PlannerPassPlan struct {
	PlanMeta           PlanMeta            `json:"plan_meta"`
	SourceIntent       SourceIntent        `json:"source_intent"`
	GlobalContextRules *GlobalContextRules `json:"global_context_rules,omitempty"`
	Passes             []PlanPassInput     `json:"passes"`
}

type PlanMeta struct {
	PlanID               string                `json:"plan_id"`
	SchemaVersion        string                `json:"schema_version"`
	CreatedAt            string                `json:"created_at"`
	Title                string                `json:"title"`
	Goal                 string                `json:"goal"`
	RepoTarget           string                `json:"repo_target"`
	BranchContext        string                `json:"branch_context"`
	Status               string                `json:"status"`
	ProjectID            string                `json:"project_id,omitempty"`
	ProjectContext       *ProjectContext       `json:"project_context,omitempty"`
	MCPCapabilityProfile *MCPCapabilityProfile `json:"mcp_capability_profile,omitempty"`
	SubmissionNote       string                `json:"submission_note,omitempty"`
}

type SourceIntent struct {
	Summary string `json:"summary"`
}

type ProjectContext struct {
	PrimaryProject        string   `json:"primary_project"`
	PrimaryRepository     string   `json:"primary_repository"`
	ContractRepository    string   `json:"contract_repository,omitempty"`
	GitHubRole            string   `json:"github_role"`
	ExcludedGitHubDomains []string `json:"excluded_github_domains,omitempty"`
	LocalFirstAssumption  string   `json:"local_first_assumption,omitempty"`
}

type MCPCapabilityProfile struct {
	ProfileID            string `json:"profile_id"`
	Mode                 string `json:"mode"`
	ContextBrokerEnabled *bool  `json:"context_broker_enabled"`
	Notes                string `json:"notes,omitempty"`
}

type GlobalContextRules struct {
	DefaultSourceOfTruth    string   `json:"default_source_of_truth"`
	PlannerContextBoundary  string   `json:"planner_context_boundary"`
	ForbiddenContextDomains []string `json:"forbidden_context_domains"`
	Notes                   []string `json:"notes,omitempty"`
}

type PlanPassInput struct {
	PassID                     string                     `json:"pass_id"`
	Sequence                   int64                      `json:"sequence"`
	Name                       string                     `json:"name"`
	Goal                       string                     `json:"goal"`
	IntendedExecutionScope     []string                   `json:"intended_execution_scope"`
	NonGoals                   []string                   `json:"non_goals"`
	Dependencies               []string                   `json:"dependencies"`
	Status                     string                     `json:"status"`
	PassType                   string                     `json:"pass_type"`
	ContextPlan                ContextPlan                `json:"context_plan"`
	SourceSnapshotRequirements SourceSnapshotRequirements `json:"source_snapshot_requirements"`
	HandoffReadinessCriteria   []string                   `json:"handoff_readiness_criteria"`
	RiskLevel                  string                     `json:"risk_level,omitempty"`
	ContextBudget              *ContextBudget             `json:"context_budget,omitempty"`
}

type ContextPlan struct {
	RequiredRepositories        []string            `json:"required_repositories"`
	SeedSearchTerms             []ContextSearchTerm `json:"seed_search_terms"`
	SeedFilesToRead             []ContextFileRead   `json:"seed_files_to_read"`
	ContextCoverageExpectations []string            `json:"context_coverage_expectations"`
	BlockedIfMissing            []string            `json:"blocked_if_missing"`
}

type ContextSearchTerm struct {
	RepoID   string `json:"repo_id"`
	Query    string `json:"query"`
	Purpose  string `json:"purpose"`
	Required *bool  `json:"required"`
}

type ContextFileRead struct {
	RepoID   string `json:"repo_id"`
	Path     string `json:"path"`
	Purpose  string `json:"purpose"`
	Required *bool  `json:"required"`
}

type SourceSnapshotRequirements struct {
	RequireGitStatus   *bool `json:"require_git_status"`
	RequireCommitSHA   *bool `json:"require_commit_sha"`
	AllowDirtyWorktree *bool `json:"allow_dirty_worktree"`
}

type ContextBudget struct {
	MaxFiles         *int64 `json:"max_files,omitempty"`
	MaxBytes         *int64 `json:"max_bytes,omitempty"`
	MaxSearchResults *int64 `json:"max_search_results,omitempty"`
	MaxContextLines  *int64 `json:"max_context_lines,omitempty"`
}

type SubmitPlanRequest struct {
	RawJSON            []byte
	SourceArtifactPath string
	ProjectID          string
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
