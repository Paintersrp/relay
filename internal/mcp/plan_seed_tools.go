package mcp

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	appprojects "relay/internal/app/projects"
)

const (
	toolCreatePlanSeed = "create_plan_seed"
	toolListPlanSeeds  = "list_plan_seeds"
	toolGetPlanSeed    = "get_plan_seed"
	toolUpdatePlanSeed = "update_plan_seed"
	toolDeferPlanSeed  = "defer_plan_seed"
	toolRejectPlanSeed = "reject_plan_seed"
)

var createPlanSeedSchema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["project_id", "title", "quick_context"],
  "properties": {
    "project_id": { "type": "string", "minLength": 1, "description": "Relay project identifier." },
    "title": { "type": "string", "minLength": 1, "maxLength": 200, "description": "Title of the plan seed." },
    "quick_context": { "type": "string", "minLength": 1, "maxLength": 6000, "description": "Quick context description." },
    "priority": { "type": "string", "maxLength": 80, "description": "Optional priority (e.g. normal, high)." },
    "tags": { "type": "array", "maxItems": 50, "items": { "type": "string", "minLength": 1, "maxLength": 500 }, "description": "Optional tags." },
    "constraints": { "type": "array", "maxItems": 50, "items": { "type": "string", "minLength": 1, "maxLength": 500 }, "description": "Optional constraints." },
    "non_goals": { "type": "array", "maxItems": 50, "items": { "type": "string", "minLength": 1, "maxLength": 500 }, "description": "Optional non-goals." },
    "source_label": { "type": "string", "maxLength": 200, "description": "Optional label tracking the source of this seed." }
  }
}`)

var listPlanSeedsSchema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["project_id"],
  "properties": {
    "project_id": { "type": "string", "minLength": 1, "description": "Relay project identifier." },
    "status": { "type": "string", "description": "Optional status filter (captured, planned, deferred, rejected)." },
    "limit": { "type": "integer", "minimum": 1, "maximum": 100, "description": "Maximum seeds to return." }
  }
}`)

var getPlanSeedSchema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["project_id", "seed_id"],
  "properties": {
    "project_id": { "type": "string", "minLength": 1, "description": "Relay project identifier." },
    "seed_id": { "type": "string", "minLength": 1, "description": "The plan seed identifier." }
  }
}`)

var updatePlanSeedSchema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["project_id", "seed_id"],
  "properties": {
    "project_id": { "type": "string", "minLength": 1, "description": "Relay project identifier." },
    "seed_id": { "type": "string", "minLength": 1, "description": "The plan seed identifier to update." },
    "title": { "type": "string", "maxLength": 200 },
    "quick_context": { "type": "string", "maxLength": 6000 },
    "priority": { "type": "string", "maxLength": 80 },
    "tags": { "type": "array", "maxItems": 50, "items": { "type": "string", "minLength": 1, "maxLength": 500 } },
    "constraints": { "type": "array", "maxItems": 50, "items": { "type": "string", "minLength": 1, "maxLength": 500 } },
    "non_goals": { "type": "array", "maxItems": 50, "items": { "type": "string", "minLength": 1, "maxLength": 500 } }
  }
}`)

var deferPlanSeedSchema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["project_id", "seed_id"],
  "properties": {
    "project_id": { "type": "string", "minLength": 1, "description": "Relay project identifier." },
    "seed_id": { "type": "string", "minLength": 1, "description": "The plan seed identifier." },
    "defer_reason": { "type": "string", "maxLength": 6000, "description": "Deferral reason." }
  }
}`)

var rejectPlanSeedSchema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["project_id", "seed_id"],
  "properties": {
    "project_id": { "type": "string", "minLength": 1, "description": "Relay project identifier." },
    "seed_id": { "type": "string", "minLength": 1, "description": "The plan seed identifier." },
    "reject_reason": { "type": "string", "maxLength": 6000, "description": "Rejection reason." }
  }
}`)

type planSeedArgs struct {
	ProjectID    string   `json:"project_id"`
	SeedID       string   `json:"seed_id,omitempty"`
	Title        string   `json:"title,omitempty"`
	QuickContext string   `json:"quick_context,omitempty"`
	Priority     string   `json:"priority,omitempty"`
	Tags         []string `json:"tags,omitempty"`
	Constraints  []string `json:"constraints,omitempty"`
	NonGoals     []string `json:"non_goals,omitempty"`
	SourceLabel  string   `json:"source_label,omitempty"`
	Status       string   `json:"status,omitempty"`
	Limit        int64    `json:"limit,omitempty"`
	DeferReason  string   `json:"defer_reason,omitempty"`
	RejectReason string   `json:"reject_reason,omitempty"`
}

type planSeedToolOutput struct {
	OK        bool               `json:"ok"`
	Tool      string             `json:"tool"`
	ProjectID string             `json:"project_id"`
	Seed      *planSeedToolSeed  `json:"seed,omitempty"`
	Seeds     []planSeedToolSeed `json:"seeds,omitempty"`
	Count     int                `json:"count,omitempty"`
}

type planSeedToolSeed struct {
	SeedID        string   `json:"seed_id"`
	ProjectID     string   `json:"project_id"`
	Title         string   `json:"title"`
	QuickContext  string   `json:"quick_context"`
	Constraints   []string `json:"constraints"`
	NonGoals      []string `json:"non_goals"`
	Tags          []string `json:"tags"`
	Priority      string   `json:"priority"`
	Status        string   `json:"status"`
	SourceType    string   `json:"source_type"`
	SourceLabel   string   `json:"source_label"`
	SourceRefID   string   `json:"source_ref_id"`
	PlanAttemptID string   `json:"plan_attempt_id"`
	ManagedPlanID string   `json:"managed_plan_id"`
	PlannedAt     string   `json:"planned_at"`
	DeferReason   string   `json:"defer_reason"`
	RejectReason  string   `json:"reject_reason"`
	CreatedAt     string   `json:"created_at"`
	UpdatedAt     string   `json:"updated_at"`
}

type planSeedToolErrorOutput struct {
	OK         bool                                  `json:"ok"`
	Tool       string                                `json:"tool"`
	Error      string                                `json:"error"`
	Message    string                                `json:"message"`
	Validation []appprojects.PlanSeedValidationIssue `json:"validation,omitempty"`
}

func planSeedToolDefinitions() []ToolDefinition {
	return []ToolDefinition{
		{
			Name:        toolCreatePlanSeed,
			Description: "Create a new project-scoped Plan Seed in captured status. Does not submit plans or create runs.",
			InputSchema: createPlanSeedSchema,
		},
		{
			Name:        toolListPlanSeeds,
			Description: "List captured, planned, deferred, or rejected Plan Seeds under a project.",
			InputSchema: listPlanSeedsSchema,
		},
		{
			Name:        toolGetPlanSeed,
			Description: "Get the details and planned status of a specific Plan Seed by ID.",
			InputSchema: getPlanSeedSchema,
		},
		{
			Name:        toolUpdatePlanSeed,
			Description: "Update mutable capture fields of an editable captured or deferred Plan Seed. Does not allow changing status or linkage.",
			InputSchema: updatePlanSeedSchema,
		},
		{
			Name:        toolDeferPlanSeed,
			Description: "Defer a captured Plan Seed with a reason.",
			InputSchema: deferPlanSeedSchema,
		},
		{
			Name:        toolRejectPlanSeed,
			Description: "Reject and close a captured or deferred Plan Seed with a reason.",
			InputSchema: rejectPlanSeedSchema,
		},
	}
}

func (s *Server) planSeedServiceOrErr(tool string) (*appprojects.Service, *planSeedToolErrorOutput) {
	if s == nil || s.deps == nil || s.deps.Store == nil {
		return nil, &planSeedToolErrorOutput{
			OK:      false,
			Tool:    tool,
			Error:   "DEPENDENCY_ERROR",
			Message: "MCP server is not connected to a Relay store; start with RELAY_DB_PATH set",
		}
	}
	return appprojects.NewService(s.deps.Store), nil
}

func planSeedToolErr(tool, code, message string, validation []appprojects.PlanSeedValidationIssue) ToolCallResult {
	text, err := marshalTool(planSeedToolErrorOutput{
		OK:         false,
		Tool:       tool,
		Error:      code,
		Message:    message,
		Validation: validation,
	})
	if err != nil {
		return toolErr(`{"ok":false,"tool":"` + tool + `","error":"INTERNAL_ERROR","message":"failed to marshal tool error"}`)
	}
	return toolErr(text)
}

func mapPlanSeedToolSeed(seed appprojects.PlanSeedResult) planSeedToolSeed {
	return planSeedToolSeed{
		SeedID:        seed.SeedID,
		ProjectID:     seed.ProjectID,
		Title:         seed.Title,
		QuickContext:  seed.QuickContext,
		Constraints:   seed.Constraints,
		NonGoals:      seed.NonGoals,
		Tags:          seed.Tags,
		Priority:      seed.Priority,
		Status:        seed.Status,
		SourceType:    seed.SourceType,
		SourceLabel:   seed.SourceLabel,
		SourceRefID:   seed.SourceRefID,
		PlanAttemptID: seed.PlanAttemptID,
		ManagedPlanID: seed.ManagedPlanID,
		PlannedAt:     seed.PlannedAt,
		DeferReason:   seed.DeferReason,
		RejectReason:  seed.RejectReason,
		CreatedAt:     seed.CreatedAt,
		UpdatedAt:     seed.UpdatedAt,
	}
}

func planSeedToolSeedPtr(s planSeedToolSeed) *planSeedToolSeed {
	return &s
}

func (s *Server) HandleCreatePlanSeed(args json.RawMessage) ToolCallResult {
	var in planSeedArgs
	if err := brokerDecodeStrict(args, &in); err != nil {
		return planSeedToolErr(toolCreatePlanSeed, "VALIDATION_ERROR", "invalid params: "+err.Error(), nil)
	}

	projectID := strings.TrimSpace(in.ProjectID)
	if projectID == "" {
		return planSeedToolErr(toolCreatePlanSeed, "VALIDATION_ERROR", "project_id is required", nil)
	}

	title := strings.TrimSpace(in.Title)
	if title == "" {
		return planSeedToolErr(toolCreatePlanSeed, "VALIDATION_ERROR", "title is required", nil)
	}

	quickContext := strings.TrimSpace(in.QuickContext)
	if quickContext == "" {
		return planSeedToolErr(toolCreatePlanSeed, "VALIDATION_ERROR", "quick_context is required", nil)
	}

	// Validate bounds check
	if len(title) > 200 {
		return planSeedToolErr(toolCreatePlanSeed, "VALIDATION_ERROR", "title must be at most 200 characters", nil)
	}
	if len(quickContext) > 6000 {
		return planSeedToolErr(toolCreatePlanSeed, "VALIDATION_ERROR", "quick_context must be at most 6000 characters", nil)
	}
	if len(in.Priority) > 80 {
		return planSeedToolErr(toolCreatePlanSeed, "VALIDATION_ERROR", "priority must be at most 80 characters", nil)
	}
	if len(in.SourceLabel) > 200 {
		return planSeedToolErr(toolCreatePlanSeed, "VALIDATION_ERROR", "source_label must be at most 200 characters", nil)
	}
	if len(in.Tags) > 50 {
		return planSeedToolErr(toolCreatePlanSeed, "VALIDATION_ERROR", "tags must have at most 50 items", nil)
	}
	for i, t := range in.Tags {
		if len(t) > 500 {
			return planSeedToolErr(toolCreatePlanSeed, "VALIDATION_ERROR", fmt.Sprintf("tags[%d] must be at most 500 characters", i), nil)
		}
	}
	if len(in.Constraints) > 50 {
		return planSeedToolErr(toolCreatePlanSeed, "VALIDATION_ERROR", "constraints must have at most 50 items", nil)
	}
	for i, c := range in.Constraints {
		if len(c) > 500 {
			return planSeedToolErr(toolCreatePlanSeed, "VALIDATION_ERROR", fmt.Sprintf("constraints[%d] must be at most 500 characters", i), nil)
		}
	}
	if len(in.NonGoals) > 50 {
		return planSeedToolErr(toolCreatePlanSeed, "VALIDATION_ERROR", "non_goals must have at most 50 items", nil)
	}
	for i, ng := range in.NonGoals {
		if len(ng) > 500 {
			return planSeedToolErr(toolCreatePlanSeed, "VALIDATION_ERROR", fmt.Sprintf("non_goals[%d] must be at most 500 characters", i), nil)
		}
	}

	svc, depErr := s.planSeedServiceOrErr(toolCreatePlanSeed)
	if depErr != nil {
		return planSeedToolErr(toolCreatePlanSeed, depErr.Error, depErr.Message, nil)
	}

	res, validation, err := svc.CreatePlanSeed(context.Background(), projectID, appprojects.PlanSeedInput{
		Title:        title,
		QuickContext: quickContext,
		Priority:     in.Priority,
		Constraints:  in.Constraints,
		NonGoals:     in.NonGoals,
		Tags:         in.Tags,
		SourceType:   appprojects.PlanSeedSourceMCP,
		SourceLabel:  in.SourceLabel,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return planSeedToolErr(toolCreatePlanSeed, "NOT_FOUND", "Project not found", nil)
		}
		return planSeedToolErr(toolCreatePlanSeed, "PLAN_SEED_ERROR", err.Error(), nil)
	}
	if len(validation) > 0 {
		return planSeedToolErr(toolCreatePlanSeed, "VALIDATION_ERROR", "plan seed validation failed", validation)
	}

	out := planSeedToolOutput{
		OK:        true,
		Tool:      toolCreatePlanSeed,
		ProjectID: projectID,
		Seed:      planSeedToolSeedPtr(mapPlanSeedToolSeed(*res)),
	}
	text, err := marshalTool(out)
	if err != nil {
		return planSeedToolErr(toolCreatePlanSeed, "PLAN_SEED_ERROR", "marshal failed: "+err.Error(), nil)
	}
	return toolOK(text)
}

func (s *Server) HandleListPlanSeeds(args json.RawMessage) ToolCallResult {
	var in planSeedArgs
	if err := brokerDecodeStrict(args, &in); err != nil {
		return planSeedToolErr(toolListPlanSeeds, "VALIDATION_ERROR", "invalid params: "+err.Error(), nil)
	}

	projectID := strings.TrimSpace(in.ProjectID)
	if projectID == "" {
		return planSeedToolErr(toolListPlanSeeds, "VALIDATION_ERROR", "project_id is required", nil)
	}

	svc, depErr := s.planSeedServiceOrErr(toolListPlanSeeds)
	if depErr != nil {
		return planSeedToolErr(toolListPlanSeeds, depErr.Error, depErr.Message, nil)
	}

	res, validation, err := svc.ListPlanSeeds(context.Background(), projectID, in.Status, in.Limit)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return planSeedToolErr(toolListPlanSeeds, "NOT_FOUND", "Project not found", nil)
		}
		return planSeedToolErr(toolListPlanSeeds, "PLAN_SEED_ERROR", err.Error(), nil)
	}
	if len(validation) > 0 {
		return planSeedToolErr(toolListPlanSeeds, "VALIDATION_ERROR", "plan seed validation failed", validation)
	}

	seeds := make([]planSeedToolSeed, len(res))
	for i, r := range res {
		seeds[i] = mapPlanSeedToolSeed(r)
	}

	out := planSeedToolOutput{
		OK:        true,
		Tool:      toolListPlanSeeds,
		ProjectID: projectID,
		Seeds:     seeds,
		Count:     len(seeds),
	}
	text, err := marshalTool(out)
	if err != nil {
		return planSeedToolErr(toolListPlanSeeds, "PLAN_SEED_ERROR", "marshal failed: "+err.Error(), nil)
	}
	return toolOK(text)
}

func (s *Server) HandleGetPlanSeed(args json.RawMessage) ToolCallResult {
	var in planSeedArgs
	if err := brokerDecodeStrict(args, &in); err != nil {
		return planSeedToolErr(toolGetPlanSeed, "VALIDATION_ERROR", "invalid params: "+err.Error(), nil)
	}

	projectID := strings.TrimSpace(in.ProjectID)
	seedID := strings.TrimSpace(in.SeedID)
	if projectID == "" || seedID == "" {
		return planSeedToolErr(toolGetPlanSeed, "VALIDATION_ERROR", "project_id and seed_id are required", nil)
	}

	svc, depErr := s.planSeedServiceOrErr(toolGetPlanSeed)
	if depErr != nil {
		return planSeedToolErr(toolGetPlanSeed, depErr.Error, depErr.Message, nil)
	}

	res, err := svc.GetPlanSeed(context.Background(), projectID, seedID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return planSeedToolErr(toolGetPlanSeed, "NOT_FOUND", "Project or Plan Seed not found", nil)
		}
		return planSeedToolErr(toolGetPlanSeed, "PLAN_SEED_ERROR", err.Error(), nil)
	}

	out := planSeedToolOutput{
		OK:        true,
		Tool:      toolGetPlanSeed,
		ProjectID: projectID,
		Seed:      planSeedToolSeedPtr(mapPlanSeedToolSeed(*res)),
	}
	text, err := marshalTool(out)
	if err != nil {
		return planSeedToolErr(toolGetPlanSeed, "PLAN_SEED_ERROR", "marshal failed: "+err.Error(), nil)
	}
	return toolOK(text)
}

func (s *Server) HandleUpdatePlanSeed(args json.RawMessage) ToolCallResult {
	var in planSeedArgs
	if err := brokerDecodeStrict(args, &in); err != nil {
		return planSeedToolErr(toolUpdatePlanSeed, "VALIDATION_ERROR", "invalid params: "+err.Error(), nil)
	}

	projectID := strings.TrimSpace(in.ProjectID)
	seedID := strings.TrimSpace(in.SeedID)
	if projectID == "" || seedID == "" {
		return planSeedToolErr(toolUpdatePlanSeed, "VALIDATION_ERROR", "project_id and seed_id are required", nil)
	}

	// Validate bounds check
	if len(in.Title) > 200 {
		return planSeedToolErr(toolUpdatePlanSeed, "VALIDATION_ERROR", "title must be at most 200 characters", nil)
	}
	if len(in.QuickContext) > 6000 {
		return planSeedToolErr(toolUpdatePlanSeed, "VALIDATION_ERROR", "quick_context must be at most 6000 characters", nil)
	}
	if len(in.Priority) > 80 {
		return planSeedToolErr(toolUpdatePlanSeed, "VALIDATION_ERROR", "priority must be at most 80 characters", nil)
	}
	if len(in.Tags) > 50 {
		return planSeedToolErr(toolUpdatePlanSeed, "VALIDATION_ERROR", "tags must have at most 50 items", nil)
	}
	for i, t := range in.Tags {
		if len(t) > 500 {
			return planSeedToolErr(toolUpdatePlanSeed, "VALIDATION_ERROR", fmt.Sprintf("tags[%d] must be at most 500 characters", i), nil)
		}
	}
	if len(in.Constraints) > 50 {
		return planSeedToolErr(toolUpdatePlanSeed, "VALIDATION_ERROR", "constraints must have at most 50 items", nil)
	}
	for i, c := range in.Constraints {
		if len(c) > 500 {
			return planSeedToolErr(toolUpdatePlanSeed, "VALIDATION_ERROR", fmt.Sprintf("constraints[%d] must be at most 500 characters", i), nil)
		}
	}
	if len(in.NonGoals) > 50 {
		return planSeedToolErr(toolUpdatePlanSeed, "VALIDATION_ERROR", "non_goals must have at most 50 items", nil)
	}
	for i, ng := range in.NonGoals {
		if len(ng) > 500 {
			return planSeedToolErr(toolUpdatePlanSeed, "VALIDATION_ERROR", fmt.Sprintf("non_goals[%d] must be at most 500 characters", i), nil)
		}
	}

	svc, depErr := s.planSeedServiceOrErr(toolUpdatePlanSeed)
	if depErr != nil {
		return planSeedToolErr(toolUpdatePlanSeed, depErr.Error, depErr.Message, nil)
	}

	// First load the seed to check existence and preserve SourceType.
	existing, err := svc.GetPlanSeed(context.Background(), projectID, seedID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return planSeedToolErr(toolUpdatePlanSeed, "NOT_FOUND", "Project or Plan Seed not found", nil)
		}
		return planSeedToolErr(toolUpdatePlanSeed, "PLAN_SEED_ERROR", err.Error(), nil)
	}

	res, validation, err := svc.UpdatePlanSeed(context.Background(), projectID, seedID, appprojects.PlanSeedInput{
		Title:        in.Title,
		QuickContext: in.QuickContext,
		Priority:     in.Priority,
		Constraints:  in.Constraints,
		NonGoals:     in.NonGoals,
		Tags:         in.Tags,
		SourceType:   existing.SourceType,
		SourceLabel:  existing.SourceLabel,
		SourceRefID:  existing.SourceRefID,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return planSeedToolErr(toolUpdatePlanSeed, "NOT_FOUND", "Project or Plan Seed not found", nil)
		}
		return planSeedToolErr(toolUpdatePlanSeed, "PLAN_SEED_ERROR", err.Error(), nil)
	}
	if len(validation) > 0 {
		return planSeedToolErr(toolUpdatePlanSeed, "VALIDATION_ERROR", "plan seed validation failed", validation)
	}

	out := planSeedToolOutput{
		OK:        true,
		Tool:      toolUpdatePlanSeed,
		ProjectID: projectID,
		Seed:      planSeedToolSeedPtr(mapPlanSeedToolSeed(*res)),
	}
	text, err := marshalTool(out)
	if err != nil {
		return planSeedToolErr(toolUpdatePlanSeed, "PLAN_SEED_ERROR", "marshal failed: "+err.Error(), nil)
	}
	return toolOK(text)
}

func (s *Server) HandleDeferPlanSeed(args json.RawMessage) ToolCallResult {
	var in planSeedArgs
	if err := brokerDecodeStrict(args, &in); err != nil {
		return planSeedToolErr(toolDeferPlanSeed, "VALIDATION_ERROR", "invalid params: "+err.Error(), nil)
	}

	projectID := strings.TrimSpace(in.ProjectID)
	seedID := strings.TrimSpace(in.SeedID)
	if projectID == "" || seedID == "" {
		return planSeedToolErr(toolDeferPlanSeed, "VALIDATION_ERROR", "project_id and seed_id are required", nil)
	}

	svc, depErr := s.planSeedServiceOrErr(toolDeferPlanSeed)
	if depErr != nil {
		return planSeedToolErr(toolDeferPlanSeed, depErr.Error, depErr.Message, nil)
	}

	res, validation, err := svc.DeferPlanSeed(context.Background(), projectID, seedID, appprojects.PlanSeedLifecycleInput{
		DeferReason: in.DeferReason,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return planSeedToolErr(toolDeferPlanSeed, "NOT_FOUND", "Project or Plan Seed not found", nil)
		}
		return planSeedToolErr(toolDeferPlanSeed, "PLAN_SEED_ERROR", err.Error(), nil)
	}
	if len(validation) > 0 {
		return planSeedToolErr(toolDeferPlanSeed, "VALIDATION_ERROR", "plan seed validation failed", validation)
	}

	out := planSeedToolOutput{
		OK:        true,
		Tool:      toolDeferPlanSeed,
		ProjectID: projectID,
		Seed:      planSeedToolSeedPtr(mapPlanSeedToolSeed(*res)),
	}
	text, err := marshalTool(out)
	if err != nil {
		return planSeedToolErr(toolDeferPlanSeed, "PLAN_SEED_ERROR", "marshal failed: "+err.Error(), nil)
	}
	return toolOK(text)
}

func (s *Server) HandleRejectPlanSeed(args json.RawMessage) ToolCallResult {
	var in planSeedArgs
	if err := brokerDecodeStrict(args, &in); err != nil {
		return planSeedToolErr(toolRejectPlanSeed, "VALIDATION_ERROR", "invalid params: "+err.Error(), nil)
	}

	projectID := strings.TrimSpace(in.ProjectID)
	seedID := strings.TrimSpace(in.SeedID)
	if projectID == "" || seedID == "" {
		return planSeedToolErr(toolRejectPlanSeed, "VALIDATION_ERROR", "project_id and seed_id are required", nil)
	}

	svc, depErr := s.planSeedServiceOrErr(toolRejectPlanSeed)
	if depErr != nil {
		return planSeedToolErr(toolRejectPlanSeed, depErr.Error, depErr.Message, nil)
	}

	res, validation, err := svc.RejectPlanSeed(context.Background(), projectID, seedID, appprojects.PlanSeedLifecycleInput{
		RejectReason: in.RejectReason,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return planSeedToolErr(toolRejectPlanSeed, "NOT_FOUND", "Project or Plan Seed not found", nil)
		}
		return planSeedToolErr(toolRejectPlanSeed, "PLAN_SEED_ERROR", err.Error(), nil)
	}
	if len(validation) > 0 {
		return planSeedToolErr(toolRejectPlanSeed, "VALIDATION_ERROR", "plan seed validation failed", validation)
	}

	out := planSeedToolOutput{
		OK:        true,
		Tool:      toolRejectPlanSeed,
		ProjectID: projectID,
		Seed:      planSeedToolSeedPtr(mapPlanSeedToolSeed(*res)),
	}
	text, err := marshalTool(out)
	if err != nil {
		return planSeedToolErr(toolRejectPlanSeed, "PLAN_SEED_ERROR", "marshal failed: "+err.Error(), nil)
	}
	return toolOK(text)
}
