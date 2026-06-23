package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"relay/internal/projectmemory"
)

var searchProjectContextMemorySchema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["project_id"],
  "properties": {
    "project_id": { "type": "string", "minLength": 1 },
    "query": { "type": "string", "maxLength": 500 },
    "kinds": {
      "type": "array",
      "items": { "type": "string", "enum": ["decision", "constraint", "architecture_rationale", "operator_preference", "project_principle", "risk", "terminology", "supersession", "open_question"] },
      "maxItems": 9
    },
    "statuses": {
      "type": "array",
      "items": { "type": "string", "enum": ["active", "superseded", "archived"] },
      "maxItems": 3
    },
    "importance": {
      "type": "array",
      "items": { "type": "string", "enum": ["low", "normal", "high", "critical"] },
      "maxItems": 4
    },
    "tags": {
      "type": "array",
      "items": { "type": "string", "minLength": 1, "maxLength": 40 },
      "maxItems": 20
    },
    "limit": { "type": "integer", "minimum": 1, "maximum": 50 },
    "include_body": { "type": "boolean" }
  }
}`)

var listProjectContextRecordsSchema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["project_id"],
  "properties": {
    "project_id": { "type": "string", "minLength": 1 },
    "kinds": {
      "type": "array",
      "items": { "type": "string", "enum": ["decision", "constraint", "architecture_rationale", "operator_preference", "project_principle", "risk", "terminology", "supersession", "open_question"] },
      "maxItems": 9
    },
    "statuses": {
      "type": "array",
      "items": { "type": "string", "enum": ["active", "superseded", "archived"] },
      "maxItems": 3
    },
    "importance": {
      "type": "array",
      "items": { "type": "string", "enum": ["low", "normal", "high", "critical"] },
      "maxItems": 4
    },
    "tags": {
      "type": "array",
      "items": { "type": "string", "minLength": 1, "maxLength": 40 },
      "maxItems": 20
    },
    "limit": { "type": "integer", "minimum": 1, "maximum": 50 }
  }
}`)

var getProjectContextRecordSchema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["project_id", "record_id"],
  "properties": {
    "project_id": { "type": "string", "minLength": 1 },
    "record_id": { "type": "string", "minLength": 1 }
  }
}`)

var createProjectContextRecordSchema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["project_id", "kind", "title", "body", "dedupe_reason"],
  "properties": {
    "project_id": { "type": "string", "minLength": 1 },
    "kind": { "type": "string", "enum": ["decision", "constraint", "architecture_rationale", "operator_preference", "project_principle", "risk", "terminology", "supersession", "open_question"] },
    "title": { "type": "string", "minLength": 1, "maxLength": 180 },
    "body": { "type": "string", "minLength": 1, "maxLength": 32768 },
    "importance": { "type": "string", "enum": ["low", "normal", "high", "critical"] },
    "tags": {
      "type": "array",
      "items": { "type": "string", "minLength": 1, "maxLength": 40 },
      "maxItems": 20
    },
    "source": { "type": "string", "enum": ["chat", "operator_statement", "handoff", "audit", "source_doc", "manual"] },
    "created_by": { "type": "string", "enum": ["chat_agent", "operator", "system"] },
    "dedupe_reason": { "type": "string", "minLength": 1, "maxLength": 500 }
  }
}`)

var supersedeProjectContextRecordSchema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["project_id", "record_id", "kind", "title", "body", "dedupe_reason"],
  "properties": {
    "project_id": { "type": "string", "minLength": 1 },
    "record_id": { "type": "string", "minLength": 1 },
    "kind": { "type": "string", "enum": ["decision", "constraint", "architecture_rationale", "operator_preference", "project_principle", "risk", "terminology", "supersession", "open_question"] },
    "title": { "type": "string", "minLength": 1, "maxLength": 180 },
    "body": { "type": "string", "minLength": 1, "maxLength": 32768 },
    "importance": { "type": "string", "enum": ["low", "normal", "high", "critical"] },
    "tags": {
      "type": "array",
      "items": { "type": "string", "minLength": 1, "maxLength": 40 },
      "maxItems": 20
    },
    "source": { "type": "string", "enum": ["chat", "operator_statement", "handoff", "audit", "source_doc", "manual"] },
    "created_by": { "type": "string", "enum": ["chat_agent", "operator", "system"] },
    "dedupe_reason": { "type": "string", "minLength": 1, "maxLength": 500 }
  }
}`)

var (
	ToolSearchProjectContextMemory = ToolDefinition{
		Name:        "search_project_context_memory",
		Description: "Search bounded durable long-form project context memory. Use before creating records; do not use memory for temporary pass/run state, ordinary progress updates, or current task status.",
		InputSchema: searchProjectContextMemorySchema,
	}
	ToolListProjectContextRecords = ToolDefinition{
		Name:        "list_project_context_records",
		Description: "List bounded durable project context memory records, defaulting to active records. This is not a plan/pass tracker or audit system.",
		InputSchema: listProjectContextRecordsSchema,
	}
	ToolGetProjectContextRecord = ToolDefinition{
		Name:        "get_project_context_record",
		Description: "Get one bounded full project context memory record by ID for operator/chat-agent review.",
		InputSchema: getProjectContextRecordSchema,
	}
	ToolCreateProjectContextRecord = ToolDefinition{
		Name:        "create_project_context_record",
		Description: "Create durable long-form project context only after searching existing memory and checking active project/plan context. Create only when important and materially absent; do not save temporary pass/run state or routine progress. Ask the operator before saving behavior-changing, architecture-changing, scope-changing, or contradictory high/critical context.",
		InputSchema: createProjectContextRecordSchema,
	}
	ToolSupersedeProjectContextRecord = ToolDefinition{
		Name:        "supersede_project_context_record",
		Description: "Append a replacement for an active durable project context record and mark the old record superseded. Use when important context materially changes; ask the operator before saving behavior-changing, architecture-changing, scope-changing, or contradictory high/critical context.",
		InputSchema: supersedeProjectContextRecordSchema,
	}
)

type searchProjectContextMemoryArgs struct {
	ProjectID   string   `json:"project_id"`
	Query       string   `json:"query"`
	Kinds       []string `json:"kinds"`
	Statuses    []string `json:"statuses"`
	Importance  []string `json:"importance"`
	Tags        []string `json:"tags"`
	Limit       int      `json:"limit"`
	IncludeBody bool     `json:"include_body"`
}

type listProjectContextRecordsArgs struct {
	ProjectID  string   `json:"project_id"`
	Kinds      []string `json:"kinds"`
	Statuses   []string `json:"statuses"`
	Importance []string `json:"importance"`
	Tags       []string `json:"tags"`
	Limit      int      `json:"limit"`
}

type getProjectContextRecordArgs struct {
	ProjectID string `json:"project_id"`
	RecordID  string `json:"record_id"`
}

type createProjectContextRecordArgs struct {
	ProjectID    string   `json:"project_id"`
	Kind         string   `json:"kind"`
	Title        string   `json:"title"`
	Body         string   `json:"body"`
	Importance   string   `json:"importance"`
	Tags         []string `json:"tags"`
	Source       string   `json:"source"`
	CreatedBy    string   `json:"created_by"`
	DedupeReason string   `json:"dedupe_reason"`
}

type supersedeProjectContextRecordArgs struct {
	ProjectID    string   `json:"project_id"`
	RecordID     string   `json:"record_id"`
	Kind         string   `json:"kind"`
	Title        string   `json:"title"`
	Body         string   `json:"body"`
	Importance   string   `json:"importance"`
	Tags         []string `json:"tags"`
	Source       string   `json:"source"`
	CreatedBy    string   `json:"created_by"`
	DedupeReason string   `json:"dedupe_reason"`
}

func (s *Server) HandleSearchProjectContextMemory(rawArgs json.RawMessage) ToolCallResult {
	var args searchProjectContextMemoryArgs
	if err := brokerDecodeStrict(rawArgs, &args); err != nil {
		return brokerToolErr("VALIDATION_ERROR", "invalid params: "+err.Error())
	}
	svc, ok := s.projectContextMemoryService()
	if !ok {
		return brokerToolErr("DEPENDENCY_ERROR", "MCP server is not connected to a Relay store; start with RELAY_DB_PATH set")
	}
	result, err := svc.SearchProjectContextMemory(contextTODO(), projectmemory.SearchInput{
		ProjectID:   args.ProjectID,
		Query:       args.Query,
		Kinds:       args.Kinds,
		Statuses:    args.Statuses,
		Importance:  args.Importance,
		Tags:        args.Tags,
		Limit:       args.Limit,
		IncludeBody: args.IncludeBody,
	})
	if err != nil {
		return projectMemoryWrappedErr(err)
	}
	return brokerToolOK(ToolSearchProjectContextMemory.Name, result)
}

func (s *Server) HandleListProjectContextRecords(rawArgs json.RawMessage) ToolCallResult {
	var args listProjectContextRecordsArgs
	if err := brokerDecodeStrict(rawArgs, &args); err != nil {
		return brokerToolErr("VALIDATION_ERROR", "invalid params: "+err.Error())
	}
	svc, ok := s.projectContextMemoryService()
	if !ok {
		return brokerToolErr("DEPENDENCY_ERROR", "MCP server is not connected to a Relay store; start with RELAY_DB_PATH set")
	}
	result, err := svc.ListProjectContextRecords(contextTODO(), projectmemory.ListInput{
		ProjectID:  args.ProjectID,
		Kinds:      args.Kinds,
		Statuses:   args.Statuses,
		Importance: args.Importance,
		Tags:       args.Tags,
		Limit:      args.Limit,
	})
	if err != nil {
		return projectMemoryWrappedErr(err)
	}
	return brokerToolOK(ToolListProjectContextRecords.Name, result)
}

func (s *Server) HandleGetProjectContextRecord(rawArgs json.RawMessage) ToolCallResult {
	var args getProjectContextRecordArgs
	if err := brokerDecodeStrict(rawArgs, &args); err != nil {
		return brokerToolErr("VALIDATION_ERROR", "invalid params: "+err.Error())
	}
	svc, ok := s.projectContextMemoryService()
	if !ok {
		return brokerToolErr("DEPENDENCY_ERROR", "MCP server is not connected to a Relay store; start with RELAY_DB_PATH set")
	}
	result, err := svc.GetProjectContextRecord(contextTODO(), projectmemory.GetInput{ProjectID: args.ProjectID, RecordID: args.RecordID})
	if err != nil {
		return projectMemoryWrappedErr(err)
	}
	return brokerToolOK(ToolGetProjectContextRecord.Name, result)
}

func (s *Server) HandleCreateProjectContextRecord(rawArgs json.RawMessage) ToolCallResult {
	var args createProjectContextRecordArgs
	if err := brokerDecodeStrict(rawArgs, &args); err != nil {
		return brokerToolErr("VALIDATION_ERROR", "invalid params: "+err.Error())
	}
	svc, ok := s.projectContextMemoryService()
	if !ok {
		return brokerToolErr("DEPENDENCY_ERROR", "MCP server is not connected to a Relay store; start with RELAY_DB_PATH set")
	}
	result, issues, err := svc.CreateProjectContextRecord(contextTODO(), projectmemory.CreateInput{
		ProjectID:    args.ProjectID,
		Kind:         args.Kind,
		Title:        args.Title,
		Body:         args.Body,
		Importance:   args.Importance,
		Tags:         args.Tags,
		Source:       args.Source,
		CreatedBy:    args.CreatedBy,
		DedupeReason: args.DedupeReason,
	})
	if err != nil {
		return projectMemoryWrappedErr(err)
	}
	if len(issues) > 0 {
		return brokerToolErr("VALIDATION_ERROR", projectMemoryIssuesMessage(issues))
	}
	return brokerToolOK(ToolCreateProjectContextRecord.Name, result)
}

func (s *Server) HandleSupersedeProjectContextRecord(rawArgs json.RawMessage) ToolCallResult {
	var args supersedeProjectContextRecordArgs
	if err := brokerDecodeStrict(rawArgs, &args); err != nil {
		return brokerToolErr("VALIDATION_ERROR", "invalid params: "+err.Error())
	}
	svc, ok := s.projectContextMemoryService()
	if !ok {
		return brokerToolErr("DEPENDENCY_ERROR", "MCP server is not connected to a Relay store; start with RELAY_DB_PATH set")
	}
	result, issues, err := svc.SupersedeProjectContextRecord(contextTODO(), projectmemory.SupersedeInput{
		ProjectID:    args.ProjectID,
		RecordID:     args.RecordID,
		Kind:         args.Kind,
		Title:        args.Title,
		Body:         args.Body,
		Importance:   args.Importance,
		Tags:         args.Tags,
		Source:       args.Source,
		CreatedBy:    args.CreatedBy,
		DedupeReason: args.DedupeReason,
	})
	if err != nil {
		return projectMemoryWrappedErr(err)
	}
	if len(issues) > 0 {
		return brokerToolErr("VALIDATION_ERROR", projectMemoryIssuesMessage(issues))
	}
	return brokerToolOK(ToolSupersedeProjectContextRecord.Name, result)
}

func (s *Server) projectContextMemoryService() (*projectmemory.Service, bool) {
	if s == nil || s.deps == nil || s.deps.Store == nil {
		return nil, false
	}
	return projectmemory.NewService(s.deps.Store), true
}

func projectMemoryWrappedErr(err error) ToolCallResult {
	if code, message, ok := projectmemory.ErrorCode(err); ok {
		return brokerToolErr(code, message)
	}
	return brokerToolErr("INTERNAL_ERROR", "unexpected project context memory error")
}

func projectMemoryIssuesMessage(issues []projectmemory.ValidationIssue) string {
	parts := make([]string, 0, len(issues))
	for _, issue := range issues {
		parts = append(parts, fmt.Sprintf("%s:%s:%s", issue.Field, issue.Code, issue.Message))
	}
	return strings.Join(parts, "; ")
}

func contextTODO() context.Context {
	return context.Background()
}
