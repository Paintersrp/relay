// PASS-005: refactor backlog MCP tools.
//
// This file exposes the project-scoped refactor backlog (PASS-003 discovery
// tasks and pass-ready candidates) and the PASS-004 promotion / generated
// refactor-only plan behavior through the local-operator MCP profile. It is a
// thin, safety-preserving wrapper over internal/refactors: it registers strict
// tool schemas, decodes/validates arguments, enforces explicit confirmation on
// mutating actions, and returns bounded JSON envelopes.
//
// Safety boundaries (identical to the rest of the MCP surface):
//   - No shell execution.
//   - No arbitrary filesystem reads/writes.
//   - No git mutation.
//   - No model calls.
//   - No automatic plan submission and no run creation. Generated refactor-only
//     plans are reviewable artifacts only; submit_planner_pass_plan remains the
//     only plan-submission action and create_run_from_planner_handoff remains the
//     only reviewed handoff-to-run action.
//   - All business logic, validation, and persistence are delegated to
//     internal/refactors (PASS-003/PASS-004). This file does not embed lifecycle,
//     promotion, or generation business rules.
package mcp

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"relay/internal/refactors"
)

const (
	refactorBacklogMaxLimit     = 100
	refactorBacklogDefaultLimit = 50

	confirmPromoteCandidate = "promote_refactor_candidate_to_plan"
	confirmGeneratePlan     = "generate_reviewable_refactor_only_plan"

	generatedPlanSubmissionPolicy = "review_required_no_auto_submit"
)

// ---------------------------------------------------------------------------
// Schemas (strict: type=object, additionalProperties=false, project_id required)
// ---------------------------------------------------------------------------

var listRefactorDiscoveryTasksSchema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["project_id"],
  "properties": {
    "project_id": { "type": "string", "minLength": 1, "description": "Relay project identifier." },
    "status": { "type": "string", "description": "Optional discovery task status filter.", "enum": ["open", "completed", "closed", "superseded"] },
    "limit": { "type": "integer", "minimum": 1, "maximum": 100, "description": "Maximum rows to return (default 50, max 100)." }
  }
}`)

var getRefactorDiscoveryTaskSchema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["project_id", "discovery_task_id"],
  "properties": {
    "project_id": { "type": "string", "minLength": 1, "description": "Relay project identifier." },
    "discovery_task_id": { "type": "string", "minLength": 1, "description": "Refactor discovery task identifier." }
  }
}`)

var targetScopeSchemaFragment = `{
      "type": "object",
      "additionalProperties": false,
      "required": ["kind", "values"],
      "properties": {
        "kind": { "type": "string", "enum": ["repository", "subsystem", "directory", "file_set", "plan", "pass"] },
        "values": { "type": "array", "minItems": 1, "maxItems": 50, "items": { "type": "string", "minLength": 1 } }
      }
    }`

var createRefactorDiscoveryTaskSchema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["project_id", "discovery_task_id", "title", "analysis_prompt", "target_scope", "confirmed_user_intent"],
  "properties": {
    "project_id": { "type": "string", "minLength": 1 },
    "discovery_task_id": { "type": "string", "minLength": 1 },
    "title": { "type": "string", "minLength": 1 },
    "analysis_prompt": { "type": "string", "minLength": 1 },
    "target_scope": ` + targetScopeSchemaFragment + `,
    "priority": { "type": "string" },
    "tags": { "type": "array", "maxItems": 50, "items": { "type": "string", "minLength": 1 } },
    "metadata": { "type": "object", "additionalProperties": { "type": "string" } },
    "confirmed_user_intent": { "type": "boolean", "description": "Must be true to create the discovery task." }
  }
}`)

var updateRefactorDiscoveryTaskSchema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["project_id", "discovery_task_id", "title", "analysis_prompt", "target_scope", "confirmed_user_intent"],
  "properties": {
    "project_id": { "type": "string", "minLength": 1 },
    "discovery_task_id": { "type": "string", "minLength": 1 },
    "title": { "type": "string", "minLength": 1 },
    "analysis_prompt": { "type": "string", "minLength": 1 },
    "target_scope": ` + targetScopeSchemaFragment + `,
    "priority": { "type": "string" },
    "tags": { "type": "array", "maxItems": 50, "items": { "type": "string", "minLength": 1 } },
    "metadata": { "type": "object", "additionalProperties": { "type": "string" } },
    "confirmed_user_intent": { "type": "boolean", "description": "Must be true to update the discovery task." }
  }
}`)

var discoveryTaskLifecycleSchema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["project_id", "discovery_task_id", "confirmed_user_intent"],
  "properties": {
    "project_id": { "type": "string", "minLength": 1 },
    "discovery_task_id": { "type": "string", "minLength": 1 },
    "closure_reason": { "type": "string", "description": "Required when closing a discovery task." },
    "confirmed_user_intent": { "type": "boolean", "description": "Must be true to change discovery task lifecycle state." }
  }
}`)

var supersedeDiscoveryTaskSchema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["project_id", "discovery_task_id", "superseded_by_task_id", "confirmed_user_intent"],
  "properties": {
    "project_id": { "type": "string", "minLength": 1 },
    "discovery_task_id": { "type": "string", "minLength": 1 },
    "superseded_by_task_id": { "type": "string", "minLength": 1, "description": "Same-project discovery task that supersedes this one." },
    "closure_reason": { "type": "string" },
    "confirmed_user_intent": { "type": "boolean", "description": "Must be true to supersede the discovery task." }
  }
}`)

var listRefactorCandidatesSchema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["project_id"],
  "properties": {
    "project_id": { "type": "string", "minLength": 1 },
    "status": { "type": "string", "description": "Optional candidate status filter." },
    "query": { "type": "string", "description": "Optional literal query filter." },
    "limit": { "type": "integer", "minimum": 1, "maximum": 100, "description": "Maximum rows to return (default 50, max 100)." }
  }
}`)

var getRefactorCandidateSchema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["project_id", "candidate_id"],
  "properties": {
    "project_id": { "type": "string", "minLength": 1 },
    "candidate_id": { "type": "string", "minLength": 1 }
  }
}`)

var searchRefactorCandidatesSchema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["project_id", "query"],
  "properties": {
    "project_id": { "type": "string", "minLength": 1 },
    "query": { "type": "string", "minLength": 1, "description": "Literal query to match candidates." },
    "status": { "type": "string", "description": "Optional candidate status filter." },
    "limit": { "type": "integer", "minimum": 1, "maximum": 100, "description": "Maximum rows to return (default 50, max 100)." }
  }
}`)

var candidatePassReadyProperties = `
    "project_id": { "type": "string", "minLength": 1 },
    "candidate_id": { "type": "string", "minLength": 1 },
    "title": { "type": "string", "minLength": 1 },
    "problem_summary": { "type": "string", "minLength": 1 },
    "current_behavior": { "type": "string" },
    "desired_behavior": { "type": "string", "minLength": 1 },
    "rationale": { "type": "string", "minLength": 1 },
    "proposed_pass_name": { "type": "string", "minLength": 1 },
    "proposed_pass_goal": { "type": "string", "minLength": 1 },
    "proposed_pass_scope": { "type": "array", "minItems": 1, "maxItems": 100, "items": { "type": "string", "minLength": 1 } },
    "non_goals": { "type": "array", "minItems": 1, "maxItems": 100, "items": { "type": "string", "minLength": 1 } },
    "target_files": { "type": "array", "minItems": 1, "maxItems": 100, "items": { "type": "string", "minLength": 1 } },
    "validation_commands": { "type": "array", "minItems": 1, "maxItems": 100, "items": { "type": "string", "minLength": 1 } },
    "audit_focus": { "type": "array", "minItems": 1, "maxItems": 100, "items": { "type": "string", "minLength": 1 } },
    "constraints": { "type": "array", "maxItems": 100, "items": { "type": "string", "minLength": 1 } },
    "risk_level": { "type": "string", "enum": ["low", "medium", "high"] },
    "dependency_notes": { "type": "string" },
    "source_discovery_task_ids": { "type": "array", "maxItems": 100, "items": { "type": "string", "minLength": 1 } },
    "candidate_dependency_ids": { "type": "array", "maxItems": 100, "items": { "type": "string", "minLength": 1 } },
    "metadata": { "type": "object", "additionalProperties": { "type": "string" } },
    "confirmed_user_intent": { "type": "boolean" }`

var candidatePassReadyRequired = `["project_id", "candidate_id", "title", "problem_summary", "desired_behavior", "rationale", "proposed_pass_name", "proposed_pass_goal", "proposed_pass_scope", "non_goals", "target_files", "validation_commands", "audit_focus", "risk_level", "confirmed_user_intent"]`

var createRefactorCandidateSchema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ` + candidatePassReadyRequired + `,
  "properties": {` + candidatePassReadyProperties + `
  }
}`)

var updateRefactorCandidateSchema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ` + candidatePassReadyRequired + `,
  "properties": {` + candidatePassReadyProperties + `
  }
}`)

var candidateLifecycleSchema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["project_id", "candidate_id", "confirmed_user_intent"],
  "properties": {
    "project_id": { "type": "string", "minLength": 1 },
    "candidate_id": { "type": "string", "minLength": 1 },
    "defer_reason": { "type": "string", "description": "Required when deferring." },
    "reject_reason": { "type": "string", "description": "Required when rejecting." },
    "confirmed_user_intent": { "type": "boolean" }
  }
}`)

var supersedeCandidateSchema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["project_id", "candidate_id", "superseded_by_candidate_id", "confirmed_user_intent"],
  "properties": {
    "project_id": { "type": "string", "minLength": 1 },
    "candidate_id": { "type": "string", "minLength": 1 },
    "superseded_by_candidate_id": { "type": "string", "minLength": 1, "description": "Same-project candidate that supersedes this one." },
    "supersede_reason": { "type": "string" },
    "confirmed_user_intent": { "type": "boolean" }
  }
}`)

var suggestRefactorCandidatePlacementSchema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["project_id", "candidate_id", "plan_id"],
  "properties": {
    "project_id": { "type": "string", "minLength": 1 },
    "candidate_id": { "type": "string", "minLength": 1 },
    "plan_id": { "type": "string", "minLength": 1 }
  }
}`)

var promoteRefactorCandidateToPlanSchema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["project_id", "candidate_id", "plan_id", "confirmed_user_intent", "confirmation"],
  "properties": {
    "project_id": { "type": "string", "minLength": 1 },
    "candidate_id": { "type": "string", "minLength": 1 },
    "plan_id": { "type": "string", "minLength": 1 },
    "after_pass_id": { "type": "string", "description": "Optional explicit insertion anchor pass id." },
    "use_suggested_placement": { "type": "boolean", "description": "Apply the deterministic placement suggestion when no after_pass_id is given." },
    "note": { "type": "string" },
    "confirmed_user_intent": { "type": "boolean" },
    "confirmation": { "type": "string", "enum": ["promote_refactor_candidate_to_plan"] }
  }
}`)

var generateRefactorOnlyPlanSchema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["project_id", "candidate_ids", "confirmed_user_intent", "confirmation"],
  "properties": {
    "project_id": { "type": "string", "minLength": 1 },
    "candidate_ids": { "type": "array", "minItems": 1, "maxItems": 25, "uniqueItems": true, "items": { "type": "string", "minLength": 1 } },
    "title": { "type": "string" },
    "note": { "type": "string" },
    "confirmed_user_intent": { "type": "boolean" },
    "confirmation": { "type": "string", "enum": ["generate_reviewable_refactor_only_plan"] }
  }
}`)

// ---------------------------------------------------------------------------
// Tool definitions
// ---------------------------------------------------------------------------

var ToolListRefactorDiscoveryTasks = ToolDefinition{Name: "list_refactor_discovery_tasks", Description: "List bounded project-scoped refactor discovery tasks.", InputSchema: listRefactorDiscoveryTasksSchema}
var ToolGetRefactorDiscoveryTask = ToolDefinition{Name: "get_refactor_discovery_task", Description: "Return one project-scoped refactor discovery task by ID.", InputSchema: getRefactorDiscoveryTaskSchema}
var ToolCreateRefactorDiscoveryTask = ToolDefinition{Name: "create_refactor_discovery_task", Description: "Create a project-scoped refactor discovery task. Requires explicit confirmed user intent.", InputSchema: createRefactorDiscoveryTaskSchema}
var ToolUpdateRefactorDiscoveryTask = ToolDefinition{Name: "update_refactor_discovery_task", Description: "Update a project-scoped refactor discovery task. Requires explicit confirmed user intent.", InputSchema: updateRefactorDiscoveryTaskSchema}
var ToolCompleteRefactorDiscoveryTask = ToolDefinition{Name: "complete_refactor_discovery_task", Description: "Mark a project-scoped refactor discovery task completed. Requires explicit confirmed user intent.", InputSchema: discoveryTaskLifecycleSchema}
var ToolCloseRefactorDiscoveryTask = ToolDefinition{Name: "close_refactor_discovery_task", Description: "Close a project-scoped refactor discovery task without producing a candidate. Requires explicit confirmed user intent.", InputSchema: discoveryTaskLifecycleSchema}
var ToolSupersedeRefactorDiscoveryTask = ToolDefinition{Name: "supersede_refactor_discovery_task", Description: "Supersede a project-scoped refactor discovery task. Requires explicit confirmed user intent.", InputSchema: supersedeDiscoveryTaskSchema}
var ToolListRefactorCandidates = ToolDefinition{Name: "list_refactor_candidates", Description: "List bounded project-scoped pass-ready refactor candidates.", InputSchema: listRefactorCandidatesSchema}
var ToolGetRefactorCandidate = ToolDefinition{Name: "get_refactor_candidate", Description: "Return one project-scoped refactor candidate by ID.", InputSchema: getRefactorCandidateSchema}
var ToolSearchRefactorCandidates = ToolDefinition{Name: "search_refactor_candidates", Description: "Search bounded project-scoped refactor candidates by literal query.", InputSchema: searchRefactorCandidatesSchema}
var ToolCreateRefactorCandidate = ToolDefinition{Name: "create_refactor_candidate", Description: "Create a pass-ready project-scoped refactor candidate. Requires explicit confirmed user intent.", InputSchema: createRefactorCandidateSchema}
var ToolUpdateRefactorCandidate = ToolDefinition{Name: "update_refactor_candidate", Description: "Update a project-scoped refactor candidate. Requires explicit confirmed user intent.", InputSchema: updateRefactorCandidateSchema}
var ToolDeferRefactorCandidate = ToolDefinition{Name: "defer_refactor_candidate", Description: "Defer a ready project-scoped refactor candidate. Requires explicit confirmed user intent.", InputSchema: candidateLifecycleSchema}
var ToolRejectRefactorCandidate = ToolDefinition{Name: "reject_refactor_candidate", Description: "Reject a project-scoped refactor candidate. Requires explicit confirmed user intent.", InputSchema: candidateLifecycleSchema}
var ToolSupersedeRefactorCandidate = ToolDefinition{Name: "supersede_refactor_candidate", Description: "Supersede a project-scoped refactor candidate. Requires explicit confirmed user intent.", InputSchema: supersedeCandidateSchema}
var ToolSuggestRefactorCandidatePlacement = ToolDefinition{Name: "suggest_refactor_candidate_placement", Description: "Return deterministic placement suggestion for a ready refactor candidate in a project-owned plan. Retrieval only; no mutation.", InputSchema: suggestRefactorCandidatePlacementSchema}
var ToolPromoteRefactorCandidateToPlan = ToolDefinition{Name: "promote_refactor_candidate_to_plan", Description: "Promote a ready refactor candidate into an existing project-owned plan as a normal managed refactor pass. Requires explicit confirmation. Does not create a run.", InputSchema: promoteRefactorCandidateToPlanSchema}
var ToolGenerateRefactorOnlyPlan = ToolDefinition{Name: "generate_refactor_only_plan", Description: "Generate reviewable refactor-only Plan of Passes artifacts from selected ready candidates. Requires explicit confirmation. Does not submit the plan.", InputSchema: generateRefactorOnlyPlanSchema}

// refactorBacklogToolDefinitions returns the PASS-005 refactor backlog tools in a
// stable registration order: retrieval, backlog mutation, then plan
// mutation/artifact generation.
func refactorBacklogToolDefinitions() []ToolDefinition {
	return []ToolDefinition{
		ToolListRefactorDiscoveryTasks,
		ToolGetRefactorDiscoveryTask,
		ToolCreateRefactorDiscoveryTask,
		ToolUpdateRefactorDiscoveryTask,
		ToolCompleteRefactorDiscoveryTask,
		ToolCloseRefactorDiscoveryTask,
		ToolSupersedeRefactorDiscoveryTask,
		ToolListRefactorCandidates,
		ToolGetRefactorCandidate,
		ToolSearchRefactorCandidates,
		ToolCreateRefactorCandidate,
		ToolUpdateRefactorCandidate,
		ToolDeferRefactorCandidate,
		ToolRejectRefactorCandidate,
		ToolSupersedeRefactorCandidate,
		ToolSuggestRefactorCandidatePlacement,
		ToolPromoteRefactorCandidateToPlan,
		ToolGenerateRefactorOnlyPlan,
	}
}

// ---------------------------------------------------------------------------
// Argument structs (snake_case, strictly decoded)
// ---------------------------------------------------------------------------

type refactorListDiscoveryArgs struct {
	ProjectID string `json:"project_id"`
	Status    string `json:"status"`
	Limit     int64  `json:"limit"`
}

type refactorGetDiscoveryArgs struct {
	ProjectID       string `json:"project_id"`
	DiscoveryTaskID string `json:"discovery_task_id"`
}

type refactorDiscoveryTaskArgs struct {
	ProjectID           string                `json:"project_id"`
	DiscoveryTaskID     string                `json:"discovery_task_id"`
	Title               string                `json:"title"`
	AnalysisPrompt      string                `json:"analysis_prompt"`
	TargetScope         refactors.TargetScope `json:"target_scope"`
	Priority            string                `json:"priority"`
	Tags                []string              `json:"tags"`
	Metadata            map[string]string     `json:"metadata"`
	ConfirmedUserIntent bool                  `json:"confirmed_user_intent"`
}

type refactorDiscoveryLifecycleArgs struct {
	ProjectID           string `json:"project_id"`
	DiscoveryTaskID     string `json:"discovery_task_id"`
	ClosureReason       string `json:"closure_reason"`
	SupersededByTaskID  string `json:"superseded_by_task_id"`
	ConfirmedUserIntent bool   `json:"confirmed_user_intent"`
}

type refactorListCandidatesArgs struct {
	ProjectID string `json:"project_id"`
	Status    string `json:"status"`
	Query     string `json:"query"`
	Limit     int64  `json:"limit"`
}

type refactorGetCandidateArgs struct {
	ProjectID   string `json:"project_id"`
	CandidateID string `json:"candidate_id"`
}

type refactorSearchCandidatesArgs struct {
	ProjectID string `json:"project_id"`
	Query     string `json:"query"`
	Status    string `json:"status"`
	Limit     int64  `json:"limit"`
}

type refactorCandidateArgs struct {
	ProjectID              string            `json:"project_id"`
	CandidateID            string            `json:"candidate_id"`
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
	ConfirmedUserIntent    bool              `json:"confirmed_user_intent"`
}

type refactorCandidateLifecycleArgs struct {
	ProjectID               string `json:"project_id"`
	CandidateID             string `json:"candidate_id"`
	DeferReason             string `json:"defer_reason"`
	RejectReason            string `json:"reject_reason"`
	SupersedeReason         string `json:"supersede_reason"`
	SupersededByCandidateID string `json:"superseded_by_candidate_id"`
	ConfirmedUserIntent     bool   `json:"confirmed_user_intent"`
}

type refactorPlacementArgs struct {
	ProjectID   string `json:"project_id"`
	CandidateID string `json:"candidate_id"`
	PlanID      string `json:"plan_id"`
}

type refactorPromoteArgs struct {
	ProjectID             string `json:"project_id"`
	CandidateID           string `json:"candidate_id"`
	PlanID                string `json:"plan_id"`
	AfterPassID           string `json:"after_pass_id"`
	UseSuggestedPlacement bool   `json:"use_suggested_placement"`
	Note                  string `json:"note"`
	ConfirmedUserIntent   bool   `json:"confirmed_user_intent"`
	Confirmation          string `json:"confirmation"`
}

type refactorGeneratePlanArgs struct {
	ProjectID           string   `json:"project_id"`
	CandidateIDs        []string `json:"candidate_ids"`
	Title               string   `json:"title"`
	Note                string   `json:"note"`
	ConfirmedUserIntent bool     `json:"confirmed_user_intent"`
	Confirmation        string   `json:"confirmation"`
}

// ---------------------------------------------------------------------------
// Common helpers
// ---------------------------------------------------------------------------

// refactorService returns a refactor backlog service backed by the MCP store, or
// a tool-level dependency error when the server has no store wired.
func (s *Server) refactorService() (*refactors.Service, *ToolCallResult) {
	if s == nil || s.deps == nil || s.deps.Store == nil {
		r := brokerToolErr("DEPENDENCY_ERROR", "MCP server is not connected to a Relay store; start with RELAY_DB_PATH set")
		return nil, &r
	}
	return refactors.NewService(s.deps.Store), nil
}

// refactorRequireConfirmation rejects the call before any service write when the
// caller did not set confirmed_user_intent.
func refactorRequireConfirmation(confirmed bool) *ToolCallResult {
	if !confirmed {
		r := brokerToolErr("CONFIRMATION_REQUIRED", "confirmed_user_intent must be true for this refactor backlog mutation")
		return &r
	}
	return nil
}

// refactorRequireConfirmationString rejects the call before any service write
// when the explicit confirmation string does not exactly match the expected
// value for a gated plan mutation / artifact-generation tool.
func refactorRequireConfirmationString(got, want string) *ToolCallResult {
	if strings.TrimSpace(got) != want {
		r := brokerToolErr("CONFIRMATION_REQUIRED", "confirmation must equal "+want)
		return &r
	}
	return nil
}

// refactorEffectiveLimit clamps a requested limit into [1, max], defaulting to
// the backlog default when non-positive.
func refactorEffectiveLimit(limit int64) int64 {
	if limit <= 0 {
		return refactorBacklogDefaultLimit
	}
	if limit > refactorBacklogMaxLimit {
		return refactorBacklogMaxLimit
	}
	return limit
}

// refactorRequestTruncated reports whether the caller's requested limit exceeded
// the hard backlog cap and was therefore clamped.
func refactorRequestTruncated(limit int64) bool {
	return limit > refactorBacklogMaxLimit
}

// refactorRequireField trims a required string argument and returns a validation
// error result when it is blank.
func refactorRequireField(field, value string) *ToolCallResult {
	if strings.TrimSpace(value) == "" {
		r := brokerToolErr("VALIDATION_ERROR", field+" is required")
		return &r
	}
	return nil
}

// refactorLoadErr maps a service load error to a bounded tool error: a missing
// project/entity becomes NOT_FOUND; anything else becomes a generic
// INTERNAL_ERROR (the underlying message is intentionally not leaked).
func refactorLoadErr(action string, err error) ToolCallResult {
	if errors.Is(err, sql.ErrNoRows) {
		return brokerToolErr("NOT_FOUND", action+": not found in project")
	}
	return brokerToolErr("INTERNAL_ERROR", "failed to "+action)
}

// refactorValidationToolResult returns the structured validation envelope for
// service-reported validation issues (not JSON-RPC errors).
func refactorValidationToolResult(tool string, issues []refactors.ValidationIssue) ToolCallResult {
	if len(issues) == 0 {
		return brokerToolErr("VALIDATION_ERROR", "validation failed")
	}
	payload := map[string]interface{}{
		"ok":   false,
		"tool": tool,
		"blockers": []map[string]interface{}{
			{"code": "validation_error", "message": "validation failed", "recoverable": true},
		},
		"validation": issues,
	}
	return brokerToolOK(tool, payload)
}

// ---------------------------------------------------------------------------
// Retrieval handlers
// ---------------------------------------------------------------------------

// HandleListRefactorDiscoveryTasks lists bounded project-scoped discovery tasks.
func (s *Server) HandleListRefactorDiscoveryTasks(rawArgs json.RawMessage) ToolCallResult {
	const tool = "list_refactor_discovery_tasks"
	var args refactorListDiscoveryArgs
	if err := brokerDecodeStrict(rawArgs, &args); err != nil {
		return brokerToolErr("VALIDATION_ERROR", "invalid params: "+err.Error())
	}
	if r := refactorRequireField("project_id", args.ProjectID); r != nil {
		return *r
	}
	svc, depErr := s.refactorService()
	if depErr != nil {
		return *depErr
	}
	tasks, err := svc.ListDiscoveryTasks(context.Background(), strings.TrimSpace(args.ProjectID), strings.TrimSpace(args.Status), refactorEffectiveLimit(args.Limit))
	if err != nil {
		return refactorLoadErr("list discovery tasks", err)
	}
	return brokerToolOK(tool, map[string]interface{}{
		"project_id":      strings.TrimSpace(args.ProjectID),
		"discovery_tasks": tasks,
		"count":           len(tasks),
		"truncated":       refactorRequestTruncated(args.Limit),
	})
}

// HandleGetRefactorDiscoveryTask returns one project-scoped discovery task.
func (s *Server) HandleGetRefactorDiscoveryTask(rawArgs json.RawMessage) ToolCallResult {
	const tool = "get_refactor_discovery_task"
	var args refactorGetDiscoveryArgs
	if err := brokerDecodeStrict(rawArgs, &args); err != nil {
		return brokerToolErr("VALIDATION_ERROR", "invalid params: "+err.Error())
	}
	if r := refactorRequireField("project_id", args.ProjectID); r != nil {
		return *r
	}
	if r := refactorRequireField("discovery_task_id", args.DiscoveryTaskID); r != nil {
		return *r
	}
	svc, depErr := s.refactorService()
	if depErr != nil {
		return *depErr
	}
	task, err := svc.GetDiscoveryTask(context.Background(), strings.TrimSpace(args.ProjectID), strings.TrimSpace(args.DiscoveryTaskID))
	if err != nil {
		return refactorLoadErr("get discovery task", err)
	}
	return brokerToolOK(tool, map[string]interface{}{"discovery_task": task})
}

// HandleListRefactorCandidates lists bounded project-scoped candidates.
func (s *Server) HandleListRefactorCandidates(rawArgs json.RawMessage) ToolCallResult {
	const tool = "list_refactor_candidates"
	var args refactorListCandidatesArgs
	if err := brokerDecodeStrict(rawArgs, &args); err != nil {
		return brokerToolErr("VALIDATION_ERROR", "invalid params: "+err.Error())
	}
	if r := refactorRequireField("project_id", args.ProjectID); r != nil {
		return *r
	}
	svc, depErr := s.refactorService()
	if depErr != nil {
		return *depErr
	}
	candidates, err := svc.ListCandidates(context.Background(), strings.TrimSpace(args.ProjectID), strings.TrimSpace(args.Status), strings.TrimSpace(args.Query), refactorEffectiveLimit(args.Limit))
	if err != nil {
		return refactorLoadErr("list candidates", err)
	}
	return brokerToolOK(tool, map[string]interface{}{
		"project_id": strings.TrimSpace(args.ProjectID),
		"candidates": candidates,
		"count":      len(candidates),
		"truncated":  refactorRequestTruncated(args.Limit),
	})
}

// HandleGetRefactorCandidate returns one project-scoped candidate.
func (s *Server) HandleGetRefactorCandidate(rawArgs json.RawMessage) ToolCallResult {
	const tool = "get_refactor_candidate"
	var args refactorGetCandidateArgs
	if err := brokerDecodeStrict(rawArgs, &args); err != nil {
		return brokerToolErr("VALIDATION_ERROR", "invalid params: "+err.Error())
	}
	if r := refactorRequireField("project_id", args.ProjectID); r != nil {
		return *r
	}
	if r := refactorRequireField("candidate_id", args.CandidateID); r != nil {
		return *r
	}
	svc, depErr := s.refactorService()
	if depErr != nil {
		return *depErr
	}
	candidate, err := svc.GetCandidate(context.Background(), strings.TrimSpace(args.ProjectID), strings.TrimSpace(args.CandidateID))
	if err != nil {
		return refactorLoadErr("get candidate", err)
	}
	return brokerToolOK(tool, map[string]interface{}{"candidate": candidate})
}

// HandleSearchRefactorCandidates searches bounded project-scoped candidates.
func (s *Server) HandleSearchRefactorCandidates(rawArgs json.RawMessage) ToolCallResult {
	const tool = "search_refactor_candidates"
	var args refactorSearchCandidatesArgs
	if err := brokerDecodeStrict(rawArgs, &args); err != nil {
		return brokerToolErr("VALIDATION_ERROR", "invalid params: "+err.Error())
	}
	if r := refactorRequireField("project_id", args.ProjectID); r != nil {
		return *r
	}
	if r := refactorRequireField("query", args.Query); r != nil {
		return *r
	}
	svc, depErr := s.refactorService()
	if depErr != nil {
		return *depErr
	}
	candidates, err := svc.ListCandidates(context.Background(), strings.TrimSpace(args.ProjectID), strings.TrimSpace(args.Status), strings.TrimSpace(args.Query), refactorEffectiveLimit(args.Limit))
	if err != nil {
		return refactorLoadErr("search candidates", err)
	}
	return brokerToolOK(tool, map[string]interface{}{
		"project_id": strings.TrimSpace(args.ProjectID),
		"candidates": candidates,
		"count":      len(candidates),
		"truncated":  refactorRequestTruncated(args.Limit),
	})
}

// HandleSuggestRefactorCandidatePlacement returns a deterministic, advisory
// placement suggestion. It is retrieval-only and never mutates state.
func (s *Server) HandleSuggestRefactorCandidatePlacement(rawArgs json.RawMessage) ToolCallResult {
	const tool = "suggest_refactor_candidate_placement"
	var args refactorPlacementArgs
	if err := brokerDecodeStrict(rawArgs, &args); err != nil {
		return brokerToolErr("VALIDATION_ERROR", "invalid params: "+err.Error())
	}
	if r := refactorRequireField("project_id", args.ProjectID); r != nil {
		return *r
	}
	if r := refactorRequireField("candidate_id", args.CandidateID); r != nil {
		return *r
	}
	if r := refactorRequireField("plan_id", args.PlanID); r != nil {
		return *r
	}
	svc, depErr := s.refactorService()
	if depErr != nil {
		return *depErr
	}
	suggestion, issues, err := svc.SuggestCandidatePlacement(context.Background(), strings.TrimSpace(args.ProjectID), strings.TrimSpace(args.CandidateID), strings.TrimSpace(args.PlanID))
	if err != nil {
		return refactorLoadErr("suggest placement", err)
	}
	if len(issues) > 0 {
		return refactorValidationToolResult(tool, issues)
	}
	return brokerToolOK(tool, map[string]interface{}{
		"project_id":           strings.TrimSpace(args.ProjectID),
		"candidate_id":         strings.TrimSpace(args.CandidateID),
		"plan_id":              strings.TrimSpace(args.PlanID),
		"placement_suggestion": suggestion,
	})
}

// ---------------------------------------------------------------------------
// Backlog mutation handlers
// ---------------------------------------------------------------------------

// discoveryTaskInputFromArgs maps MCP discovery task args to the service input.
func discoveryTaskInputFromArgs(args refactorDiscoveryTaskArgs) refactors.DiscoveryTaskInput {
	return refactors.DiscoveryTaskInput{
		DiscoveryTaskID: strings.TrimSpace(args.DiscoveryTaskID),
		ProjectID:       strings.TrimSpace(args.ProjectID),
		Title:           args.Title,
		AnalysisPrompt:  args.AnalysisPrompt,
		TargetScope:     args.TargetScope,
		Priority:        args.Priority,
		Tags:            args.Tags,
		Metadata:        args.Metadata,
	}
}

// HandleCreateRefactorDiscoveryTask creates a discovery task.
func (s *Server) HandleCreateRefactorDiscoveryTask(rawArgs json.RawMessage) ToolCallResult {
	const tool = "create_refactor_discovery_task"
	var args refactorDiscoveryTaskArgs
	if err := brokerDecodeStrict(rawArgs, &args); err != nil {
		return brokerToolErr("VALIDATION_ERROR", "invalid params: "+err.Error())
	}
	if r := refactorRequireField("project_id", args.ProjectID); r != nil {
		return *r
	}
	if r := refactorRequireConfirmation(args.ConfirmedUserIntent); r != nil {
		return *r
	}
	svc, depErr := s.refactorService()
	if depErr != nil {
		return *depErr
	}
	task, issues, err := svc.CreateDiscoveryTask(context.Background(), strings.TrimSpace(args.ProjectID), discoveryTaskInputFromArgs(args))
	if err != nil {
		return refactorLoadErr("create discovery task", err)
	}
	if len(issues) > 0 {
		return refactorValidationToolResult(tool, issues)
	}
	return brokerToolOK(tool, map[string]interface{}{"discovery_task": task})
}

// HandleUpdateRefactorDiscoveryTask updates a discovery task.
func (s *Server) HandleUpdateRefactorDiscoveryTask(rawArgs json.RawMessage) ToolCallResult {
	const tool = "update_refactor_discovery_task"
	var args refactorDiscoveryTaskArgs
	if err := brokerDecodeStrict(rawArgs, &args); err != nil {
		return brokerToolErr("VALIDATION_ERROR", "invalid params: "+err.Error())
	}
	if r := refactorRequireField("project_id", args.ProjectID); r != nil {
		return *r
	}
	if r := refactorRequireField("discovery_task_id", args.DiscoveryTaskID); r != nil {
		return *r
	}
	if r := refactorRequireConfirmation(args.ConfirmedUserIntent); r != nil {
		return *r
	}
	svc, depErr := s.refactorService()
	if depErr != nil {
		return *depErr
	}
	task, issues, err := svc.UpdateDiscoveryTask(context.Background(), strings.TrimSpace(args.ProjectID), strings.TrimSpace(args.DiscoveryTaskID), discoveryTaskInputFromArgs(args))
	if err != nil {
		return refactorLoadErr("update discovery task", err)
	}
	if len(issues) > 0 {
		return refactorValidationToolResult(tool, issues)
	}
	return brokerToolOK(tool, map[string]interface{}{"discovery_task": task})
}

// HandleCompleteRefactorDiscoveryTask marks a discovery task completed.
func (s *Server) HandleCompleteRefactorDiscoveryTask(rawArgs json.RawMessage) ToolCallResult {
	const tool = "complete_refactor_discovery_task"
	args, errResult := decodeDiscoveryLifecycleArgs(rawArgs)
	if errResult != nil {
		return *errResult
	}
	svc, depErr := s.refactorService()
	if depErr != nil {
		return *depErr
	}
	task, issues, err := svc.CompleteDiscoveryTask(context.Background(), strings.TrimSpace(args.ProjectID), strings.TrimSpace(args.DiscoveryTaskID), refactors.DiscoveryTaskLifecycleInput{ClosureReason: args.ClosureReason})
	if err != nil {
		return refactorLoadErr("complete discovery task", err)
	}
	if len(issues) > 0 {
		return refactorValidationToolResult(tool, issues)
	}
	return brokerToolOK(tool, map[string]interface{}{"discovery_task": task})
}

// HandleCloseRefactorDiscoveryTask closes a discovery task.
func (s *Server) HandleCloseRefactorDiscoveryTask(rawArgs json.RawMessage) ToolCallResult {
	const tool = "close_refactor_discovery_task"
	args, errResult := decodeDiscoveryLifecycleArgs(rawArgs)
	if errResult != nil {
		return *errResult
	}
	svc, depErr := s.refactorService()
	if depErr != nil {
		return *depErr
	}
	task, issues, err := svc.CloseDiscoveryTask(context.Background(), strings.TrimSpace(args.ProjectID), strings.TrimSpace(args.DiscoveryTaskID), refactors.DiscoveryTaskLifecycleInput{ClosureReason: args.ClosureReason})
	if err != nil {
		return refactorLoadErr("close discovery task", err)
	}
	if len(issues) > 0 {
		return refactorValidationToolResult(tool, issues)
	}
	return brokerToolOK(tool, map[string]interface{}{"discovery_task": task})
}

// HandleSupersedeRefactorDiscoveryTask supersedes a discovery task.
func (s *Server) HandleSupersedeRefactorDiscoveryTask(rawArgs json.RawMessage) ToolCallResult {
	const tool = "supersede_refactor_discovery_task"
	var args refactorDiscoveryLifecycleArgs
	if err := brokerDecodeStrict(rawArgs, &args); err != nil {
		return brokerToolErr("VALIDATION_ERROR", "invalid params: "+err.Error())
	}
	if r := refactorRequireField("project_id", args.ProjectID); r != nil {
		return *r
	}
	if r := refactorRequireField("discovery_task_id", args.DiscoveryTaskID); r != nil {
		return *r
	}
	if r := refactorRequireField("superseded_by_task_id", args.SupersededByTaskID); r != nil {
		return *r
	}
	if r := refactorRequireConfirmation(args.ConfirmedUserIntent); r != nil {
		return *r
	}
	svc, depErr := s.refactorService()
	if depErr != nil {
		return *depErr
	}
	task, issues, err := svc.SupersedeDiscoveryTask(context.Background(), strings.TrimSpace(args.ProjectID), strings.TrimSpace(args.DiscoveryTaskID), refactors.DiscoveryTaskLifecycleInput{
		SupersededByTaskID: strings.TrimSpace(args.SupersededByTaskID),
		ClosureReason:      args.ClosureReason,
	})
	if err != nil {
		return refactorLoadErr("supersede discovery task", err)
	}
	if len(issues) > 0 {
		return refactorValidationToolResult(tool, issues)
	}
	return brokerToolOK(tool, map[string]interface{}{"discovery_task": task})
}

// decodeDiscoveryLifecycleArgs decodes and validates the shared
// complete/close lifecycle arguments (project_id, discovery_task_id, confirmation).
func decodeDiscoveryLifecycleArgs(rawArgs json.RawMessage) (refactorDiscoveryLifecycleArgs, *ToolCallResult) {
	var args refactorDiscoveryLifecycleArgs
	if err := brokerDecodeStrict(rawArgs, &args); err != nil {
		r := brokerToolErr("VALIDATION_ERROR", "invalid params: "+err.Error())
		return args, &r
	}
	if r := refactorRequireField("project_id", args.ProjectID); r != nil {
		return args, r
	}
	if r := refactorRequireField("discovery_task_id", args.DiscoveryTaskID); r != nil {
		return args, r
	}
	if r := refactorRequireConfirmation(args.ConfirmedUserIntent); r != nil {
		return args, r
	}
	return args, nil
}

// candidateInputFromArgs maps MCP candidate args to the service input.
func candidateInputFromArgs(args refactorCandidateArgs) refactors.CandidateInput {
	return refactors.CandidateInput{
		CandidateID:            strings.TrimSpace(args.CandidateID),
		ProjectID:              strings.TrimSpace(args.ProjectID),
		Title:                  args.Title,
		ProblemSummary:         args.ProblemSummary,
		CurrentBehavior:        args.CurrentBehavior,
		DesiredBehavior:        args.DesiredBehavior,
		Rationale:              args.Rationale,
		ProposedPassName:       args.ProposedPassName,
		ProposedPassGoal:       args.ProposedPassGoal,
		ProposedPassScope:      args.ProposedPassScope,
		NonGoals:               args.NonGoals,
		TargetFiles:            args.TargetFiles,
		ValidationCommands:     args.ValidationCommands,
		AuditFocus:             args.AuditFocus,
		Constraints:            args.Constraints,
		RiskLevel:              args.RiskLevel,
		DependencyNotes:        args.DependencyNotes,
		SourceDiscoveryTaskIDs: args.SourceDiscoveryTaskIDs,
		CandidateDependencyIDs: args.CandidateDependencyIDs,
		Metadata:               args.Metadata,
	}
}

// HandleCreateRefactorCandidate creates a pass-ready candidate.
func (s *Server) HandleCreateRefactorCandidate(rawArgs json.RawMessage) ToolCallResult {
	const tool = "create_refactor_candidate"
	var args refactorCandidateArgs
	if err := brokerDecodeStrict(rawArgs, &args); err != nil {
		return brokerToolErr("VALIDATION_ERROR", "invalid params: "+err.Error())
	}
	if r := refactorRequireField("project_id", args.ProjectID); r != nil {
		return *r
	}
	if r := refactorRequireConfirmation(args.ConfirmedUserIntent); r != nil {
		return *r
	}
	svc, depErr := s.refactorService()
	if depErr != nil {
		return *depErr
	}
	candidate, issues, err := svc.CreateCandidate(context.Background(), strings.TrimSpace(args.ProjectID), candidateInputFromArgs(args))
	if err != nil {
		return refactorLoadErr("create candidate", err)
	}
	if len(issues) > 0 {
		return refactorValidationToolResult(tool, issues)
	}
	return brokerToolOK(tool, map[string]interface{}{"candidate": candidate})
}

// HandleUpdateRefactorCandidate updates a candidate.
func (s *Server) HandleUpdateRefactorCandidate(rawArgs json.RawMessage) ToolCallResult {
	const tool = "update_refactor_candidate"
	var args refactorCandidateArgs
	if err := brokerDecodeStrict(rawArgs, &args); err != nil {
		return brokerToolErr("VALIDATION_ERROR", "invalid params: "+err.Error())
	}
	if r := refactorRequireField("project_id", args.ProjectID); r != nil {
		return *r
	}
	if r := refactorRequireField("candidate_id", args.CandidateID); r != nil {
		return *r
	}
	if r := refactorRequireConfirmation(args.ConfirmedUserIntent); r != nil {
		return *r
	}
	svc, depErr := s.refactorService()
	if depErr != nil {
		return *depErr
	}
	candidate, issues, err := svc.UpdateCandidate(context.Background(), strings.TrimSpace(args.ProjectID), strings.TrimSpace(args.CandidateID), candidateInputFromArgs(args))
	if err != nil {
		return refactorLoadErr("update candidate", err)
	}
	if len(issues) > 0 {
		return refactorValidationToolResult(tool, issues)
	}
	return brokerToolOK(tool, map[string]interface{}{"candidate": candidate})
}

// HandleDeferRefactorCandidate defers a ready candidate.
func (s *Server) HandleDeferRefactorCandidate(rawArgs json.RawMessage) ToolCallResult {
	const tool = "defer_refactor_candidate"
	args, errResult := decodeCandidateLifecycleArgs(rawArgs)
	if errResult != nil {
		return *errResult
	}
	svc, depErr := s.refactorService()
	if depErr != nil {
		return *depErr
	}
	candidate, issues, err := svc.DeferCandidate(context.Background(), strings.TrimSpace(args.ProjectID), strings.TrimSpace(args.CandidateID), refactors.CandidateLifecycleInput{DeferReason: args.DeferReason})
	if err != nil {
		return refactorLoadErr("defer candidate", err)
	}
	if len(issues) > 0 {
		return refactorValidationToolResult(tool, issues)
	}
	return brokerToolOK(tool, map[string]interface{}{"candidate": candidate})
}

// HandleRejectRefactorCandidate rejects a candidate.
func (s *Server) HandleRejectRefactorCandidate(rawArgs json.RawMessage) ToolCallResult {
	const tool = "reject_refactor_candidate"
	args, errResult := decodeCandidateLifecycleArgs(rawArgs)
	if errResult != nil {
		return *errResult
	}
	svc, depErr := s.refactorService()
	if depErr != nil {
		return *depErr
	}
	candidate, issues, err := svc.RejectCandidate(context.Background(), strings.TrimSpace(args.ProjectID), strings.TrimSpace(args.CandidateID), refactors.CandidateLifecycleInput{RejectReason: args.RejectReason})
	if err != nil {
		return refactorLoadErr("reject candidate", err)
	}
	if len(issues) > 0 {
		return refactorValidationToolResult(tool, issues)
	}
	return brokerToolOK(tool, map[string]interface{}{"candidate": candidate})
}

// HandleSupersedeRefactorCandidate supersedes a candidate.
func (s *Server) HandleSupersedeRefactorCandidate(rawArgs json.RawMessage) ToolCallResult {
	const tool = "supersede_refactor_candidate"
	var args refactorCandidateLifecycleArgs
	if err := brokerDecodeStrict(rawArgs, &args); err != nil {
		return brokerToolErr("VALIDATION_ERROR", "invalid params: "+err.Error())
	}
	if r := refactorRequireField("project_id", args.ProjectID); r != nil {
		return *r
	}
	if r := refactorRequireField("candidate_id", args.CandidateID); r != nil {
		return *r
	}
	if r := refactorRequireField("superseded_by_candidate_id", args.SupersededByCandidateID); r != nil {
		return *r
	}
	if r := refactorRequireConfirmation(args.ConfirmedUserIntent); r != nil {
		return *r
	}
	svc, depErr := s.refactorService()
	if depErr != nil {
		return *depErr
	}
	candidate, issues, err := svc.SupersedeCandidate(context.Background(), strings.TrimSpace(args.ProjectID), strings.TrimSpace(args.CandidateID), refactors.CandidateLifecycleInput{
		SupersededByCandidateID: strings.TrimSpace(args.SupersededByCandidateID),
		SupersedeReason:         args.SupersedeReason,
	})
	if err != nil {
		return refactorLoadErr("supersede candidate", err)
	}
	if len(issues) > 0 {
		return refactorValidationToolResult(tool, issues)
	}
	return brokerToolOK(tool, map[string]interface{}{"candidate": candidate})
}

// decodeCandidateLifecycleArgs decodes and validates shared defer/reject
// lifecycle arguments (project_id, candidate_id, confirmation).
func decodeCandidateLifecycleArgs(rawArgs json.RawMessage) (refactorCandidateLifecycleArgs, *ToolCallResult) {
	var args refactorCandidateLifecycleArgs
	if err := brokerDecodeStrict(rawArgs, &args); err != nil {
		r := brokerToolErr("VALIDATION_ERROR", "invalid params: "+err.Error())
		return args, &r
	}
	if r := refactorRequireField("project_id", args.ProjectID); r != nil {
		return args, r
	}
	if r := refactorRequireField("candidate_id", args.CandidateID); r != nil {
		return args, r
	}
	if r := refactorRequireConfirmation(args.ConfirmedUserIntent); r != nil {
		return args, r
	}
	return args, nil
}

// ---------------------------------------------------------------------------
// Plan mutation / artifact generation handlers
// ---------------------------------------------------------------------------

// HandlePromoteRefactorCandidateToPlan promotes a ready candidate into an
// existing project-owned plan as a normal managed refactor pass. It requires an
// explicit confirmation string and never creates a run, submits a plan, or
// dispatches an executor; all behavior is delegated to internal/refactors.
func (s *Server) HandlePromoteRefactorCandidateToPlan(rawArgs json.RawMessage) ToolCallResult {
	const tool = "promote_refactor_candidate_to_plan"
	var args refactorPromoteArgs
	if err := brokerDecodeStrict(rawArgs, &args); err != nil {
		return brokerToolErr("VALIDATION_ERROR", "invalid params: "+err.Error())
	}
	if r := refactorRequireField("project_id", args.ProjectID); r != nil {
		return *r
	}
	if r := refactorRequireField("candidate_id", args.CandidateID); r != nil {
		return *r
	}
	if r := refactorRequireField("plan_id", args.PlanID); r != nil {
		return *r
	}
	if r := refactorRequireConfirmation(args.ConfirmedUserIntent); r != nil {
		return *r
	}
	if r := refactorRequireConfirmationString(args.Confirmation, confirmPromoteCandidate); r != nil {
		return *r
	}
	svc, depErr := s.refactorService()
	if depErr != nil {
		return *depErr
	}
	result, issues, err := svc.PromoteCandidateToPlan(context.Background(), refactors.PromoteCandidateInput{
		ProjectID:             strings.TrimSpace(args.ProjectID),
		CandidateID:           strings.TrimSpace(args.CandidateID),
		PlanID:                strings.TrimSpace(args.PlanID),
		AfterPassID:           strings.TrimSpace(args.AfterPassID),
		UseSuggestedPlacement: args.UseSuggestedPlacement,
		Note:                  args.Note,
	})
	if err != nil {
		return refactorLoadErr("promote candidate", err)
	}
	if len(issues) > 0 {
		return refactorValidationToolResult(tool, issues)
	}
	// Bounded summary only: no generated plan content is returned.
	return brokerToolOK(tool, map[string]interface{}{
		"project_id":           strings.TrimSpace(args.ProjectID),
		"candidate_id":         result.CandidateID,
		"plan_id":              result.PlanID,
		"pass_id":              result.PassID,
		"sequence":             result.Sequence,
		"candidate_status":     result.CandidateStatus,
		"placement_reason":     result.Placement.PlacementReason,
		"after_pass_id":        result.Placement.AfterPassID,
		"scheduling_reference": result.SchedulingReference,
		"warnings":             result.Warnings,
	})
}

// HandleGenerateRefactorOnlyPlan generates reviewable refactor-only Plan of
// Passes artifacts from selected ready candidates. It requires an explicit
// confirmation string, returns artifact metadata only (never full plan JSON or
// Markdown), does not submit the plan, and does not change candidate statuses.
func (s *Server) HandleGenerateRefactorOnlyPlan(rawArgs json.RawMessage) ToolCallResult {
	const tool = "generate_refactor_only_plan"
	var args refactorGeneratePlanArgs
	if err := brokerDecodeStrict(rawArgs, &args); err != nil {
		return brokerToolErr("VALIDATION_ERROR", "invalid params: "+err.Error())
	}
	if r := refactorRequireField("project_id", args.ProjectID); r != nil {
		return *r
	}
	if len(args.CandidateIDs) == 0 {
		return brokerToolErr("VALIDATION_ERROR", "candidate_ids is required")
	}
	if r := refactorRequireConfirmation(args.ConfirmedUserIntent); r != nil {
		return *r
	}
	if r := refactorRequireConfirmationString(args.Confirmation, confirmGeneratePlan); r != nil {
		return *r
	}
	svc, depErr := s.refactorService()
	if depErr != nil {
		return *depErr
	}
	result, issues, err := svc.GenerateRefactorOnlyPlan(context.Background(), refactors.GenerateRefactorPlanInput{
		ProjectID:    strings.TrimSpace(args.ProjectID),
		CandidateIDs: args.CandidateIDs,
		Title:        args.Title,
		Note:         args.Note,
	})
	if err != nil {
		return refactorLoadErr("generate refactor-only plan", err)
	}
	if len(issues) > 0 {
		return refactorValidationToolResult(tool, issues)
	}
	// Bounded review metadata only: artifact paths and IDs, never raw content.
	return brokerToolOK(tool, map[string]interface{}{
		"project_id":             result.ProjectID,
		"plan_id":                result.PlanID,
		"candidate_ids":          result.CandidateIDs,
		"json_artifact_path":     result.JSONArtifactPath,
		"markdown_artifact_path": result.MarkdownArtifactPath,
		"submission_policy":      generatedPlanSubmissionPolicy,
		"warnings":               result.Warnings,
	})
}
