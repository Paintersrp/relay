// Package refactors implements the backend service layer for the project-scoped
// refactor backlog: discovery tasks (analysis prompts) and pass-ready refactor
// candidates plus their lifecycle operations.
//
// This package owns the business rules (project resolution, pass-ready
// validation, same-project reference validation, and lifecycle guards) so that
// HTTP handlers and future MCP/UI surfaces share a single deterministic service.
//
// Persistence is provided by the PASS-002 store wrappers in internal/store. The
// service-facing input/result structs in this file intentionally mirror the
// actual persisted candidate model (problem summary, current/desired behavior,
// rationale, proposed pass name/goal/scope, non-goals, target files, validation
// commands, audit focus, constraints, risk level) rather than any earlier draft
// field model, so that requests round-trip faithfully to the store.
package refactors

// Discovery task statuses.
const (
	DiscoveryStatusOpen       = "open"
	DiscoveryStatusCompleted  = "completed"
	DiscoveryStatusClosed     = "closed"
	DiscoveryStatusSuperseded = "superseded"
)

// Refactor candidate statuses. PASS-003 may write only ready/deferred/rejected/
// superseded; scheduled/completed states are read-compatible for later passes.
const (
	CandidateStatusReady                     = "ready"
	CandidateStatusScheduled                 = "scheduled"
	CandidateStatusScheduledRevisionRequired = "scheduled_revision_required"
	CandidateStatusCompleted                 = "completed"
	CandidateStatusCompletedWithWarnings     = "completed_with_warnings"
	CandidateStatusDeferred                  = "deferred"
	CandidateStatusRejected                  = "rejected"
	CandidateStatusSuperseded                = "superseded"
)

// Risk levels.
const (
	RiskLow    = "low"
	RiskMedium = "medium"
	RiskHigh   = "high"
)

// Listing limits.
const (
	DefaultListLimit = int64(50)
	MaxListLimit     = int64(100)
)

// allowedTargetScopeKinds mirrors the discovery target scope contract enforced
// at the store boundary.
var allowedTargetScopeKinds = map[string]bool{
	"repository": true,
	"subsystem":  true,
	"directory":  true,
	"file_set":   true,
	"plan":       true,
	"pass":       true,
}

// ValidationIssue is the structured, client-consumable validation error shape
// returned by the service before any store write occurs.
type ValidationIssue struct {
	Field   string `json:"field"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Validation issue codes.
const (
	CodeRequired              = "required"
	CodeInvalidStatus         = "invalid_status"
	CodeInvalidRiskLevel      = "invalid_risk_level"
	CodeInvalidTargetScope    = "invalid_target_scope"
	CodeNotPassReady          = "not_pass_ready"
	CodeNotFound              = "not_found"
	CodeCrossProjectReference = "cross_project_reference"
	CodeSelfReference         = "self_reference"
	CodeTerminalStatus        = "terminal_status"
	CodeInvalidTransition     = "invalid_transition"
	CodeSecretLikeValue       = "secret_like_value"
)

// TargetScope is the structured discovery task scope ({kind, values}). The JSON
// field names are single words, so the same shape serves both snake_case input
// and camelCase output.
type TargetScope struct {
	Kind   string   `json:"kind"`
	Values []string `json:"values"`
}

// DiscoveryTaskInput is the snake_case request payload for discovery task
// create/update.
type DiscoveryTaskInput struct {
	DiscoveryTaskID string            `json:"discovery_task_id"`
	ProjectID       string            `json:"project_id"`
	Title           string            `json:"title"`
	AnalysisPrompt  string            `json:"analysis_prompt"`
	TargetScope     TargetScope       `json:"target_scope"`
	Priority        string            `json:"priority"`
	Tags            []string          `json:"tags"`
	Metadata        map[string]string `json:"metadata"`
}

// DiscoveryTaskLifecycleInput carries lifecycle parameters for discovery task
// complete/close/supersede.
type DiscoveryTaskLifecycleInput struct {
	ClosureReason      string `json:"closure_reason"`
	SupersededByTaskID string `json:"superseded_by_task_id"`
}

// CandidateInput is the snake_case request payload for candidate create/update.
// Fields mirror the persisted pass-ready candidate model.
type CandidateInput struct {
	CandidateID            string            `json:"candidate_id"`
	ProjectID              string            `json:"project_id"`
	Title                  string            `json:"title"`
	ProblemSummary         string            `json:"problem_summary"`
	CurrentBehavior        string            `json:"current_behavior"`
	DesiredBehavior        string            `json:"desired_behavior"`
	Rationale              string            `json:"rationale"`
	ProposedPassName       string            `json:"proposed_pass_name"`
	ProposedPassGoal       string            `json:"proposed_pass_goal"`
	ProposedPassScope      []string          `json:"proposed_pass_scope"`
	NonGoals               []string          `json:"non_goals"`
	TargetFiles            []string          `json:"target_files"`
	ValidationCommands     []string          `json:"validation_commands"`
	AuditFocus             []string          `json:"audit_focus"`
	Constraints            []string          `json:"constraints"`
	RiskLevel              string            `json:"risk_level"`
	DependencyNotes        string            `json:"dependency_notes"`
	SourceDiscoveryTaskIDs []string          `json:"source_discovery_task_ids"`
	CandidateDependencyIDs []string          `json:"candidate_dependency_ids"`
	Metadata               map[string]string `json:"metadata"`
}

// CandidateLifecycleInput carries lifecycle parameters for candidate
// defer/reject/supersede.
type CandidateLifecycleInput struct {
	DeferReason             string `json:"defer_reason"`
	RejectReason            string `json:"reject_reason"`
	SupersedeReason         string `json:"supersede_reason"`
	SupersededByCandidateID string `json:"superseded_by_candidate_id"`
}

// DiscoveryTaskResult is the camelCase response shape for a discovery task.
type DiscoveryTaskResult struct {
	DiscoveryTaskID string            `json:"discoveryTaskId"`
	ProjectID       string            `json:"projectId"`
	Title           string            `json:"title"`
	AnalysisPrompt  string            `json:"analysisPrompt"`
	TargetScope     TargetScope       `json:"targetScope"`
	Status          string            `json:"status"`
	Priority        string            `json:"priority"`
	Tags            []string          `json:"tags"`
	CreatedFrom     string            `json:"createdFrom"`
	Metadata        map[string]string `json:"metadata"`
	ClosureReason   string            `json:"closureReason,omitempty"`
	CompletedAt     string            `json:"completedAt,omitempty"`
	ClosedAt        string            `json:"closedAt,omitempty"`
	CreatedAt       string            `json:"createdAt"`
	UpdatedAt       string            `json:"updatedAt"`
}

// CandidateResult is the camelCase response shape for a refactor candidate.
//
// Resolved source-discovery and dependency identifiers are not echoed here: the
// store persists them only as relational rows keyed by internal row IDs and does
// not expose a public row-ID to public-ID reverse lookup, so this result reflects
// the candidate record fields directly.
type CandidateResult struct {
	CandidateID             string            `json:"candidateId"`
	ProjectID               string            `json:"projectId"`
	Title                   string            `json:"title"`
	ProblemSummary          string            `json:"problemSummary"`
	CurrentBehavior         string            `json:"currentBehavior"`
	DesiredBehavior         string            `json:"desiredBehavior"`
	Rationale               string            `json:"rationale"`
	ProposedPassName        string            `json:"proposedPassName"`
	ProposedPassGoal        string            `json:"proposedPassGoal"`
	ProposedPassScope       []string          `json:"proposedPassScope"`
	NonGoals                []string          `json:"nonGoals"`
	TargetFiles             []string          `json:"targetFiles"`
	ValidationCommands      []string          `json:"validationCommands"`
	AuditFocus              []string          `json:"auditFocus"`
	Constraints             []string          `json:"constraints"`
	RiskLevel               string            `json:"riskLevel"`
	Status                  string            `json:"status"`
	DependencyNotes         string            `json:"dependencyNotes,omitempty"`
	DeferReason             string            `json:"deferReason,omitempty"`
	RejectReason            string            `json:"rejectReason,omitempty"`
	SupersededByCandidateID string            `json:"supersededByCandidateId,omitempty"`
	SupersedeReason         string            `json:"supersedeReason,omitempty"`
	Metadata                map[string]string `json:"metadata"`
	CreatedAt               string            `json:"createdAt"`
	UpdatedAt               string            `json:"updatedAt"`
}
