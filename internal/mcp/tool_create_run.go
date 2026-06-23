package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"relay/internal/intake"
)

// createRunInput is the expected input for create_run_from_planner_handoff.
//
// WARNING: Do NOT include secrets, tokens, auth headers, private keys, or signed URLs
// in planner_handoff_markdown. Relay stores this content as a persistent artifact.
type createRunInput struct {
	// PlannerHandoffMarkdown is the full handoff markdown text from the current chat.
	// The MCP client/LLM should pass this as an explicit argument derived from chat context.
	// Relay does not read chat messages directly; the client is responsible for extracting
	// and passing the relevant content here.
	PlannerHandoffMarkdown string `json:"planner_handoff_markdown"`
	// RepoTarget is the target repository name or path. Optional if present in frontmatter.
	RepoTarget string `json:"repo_target,omitempty"`
	// BranchContext is the target branch. Optional; falls back to frontmatter or "main".
	BranchContext string `json:"branch_context,omitempty"`
	// Name is an explicit run title. Optional; derived from frontmatter or H1 if absent.
	Name string `json:"name,omitempty"`
	// Source identifies the origin of this submission. Default "mcp_chat".
	Source string `json:"source,omitempty"`
	// ClientTraceID is an optional opaque trace identifier from the calling client.
	ClientTraceID string `json:"client_trace_id,omitempty"`
	// PlanID optionally associates the run to an existing Relay plan.
	PlanID string `json:"plan_id,omitempty"`
	// PassID optionally associates the run to an existing Relay plan pass.
	PassID string `json:"pass_id,omitempty"`
	// SourceSnapshotID optionally records the source snapshot used to prepare the handoff.
	SourceSnapshotID string `json:"source_snapshot_id,omitempty"`
	// ContextPacketID optionally records the context packet used to prepare the handoff.
	ContextPacketID string `json:"context_packet_id,omitempty"`
}

// createRunOutput is the structured success payload for create_run_from_planner_handoff.
type createRunOutput struct {
	OK                bool                     `json:"ok"`
	Tool              string                   `json:"tool"`
	RunID             int64                    `json:"run_id"`
	Status            string                   `json:"status"`
	LifecycleState    string                   `json:"lifecycle_state"`
	ReviewURL         string                   `json:"review_url"`
	ArtifactKinds     []string                 `json:"artifact_kinds"`
	ValidationSummary intake.ValidationSummary `json:"validation_summary"`
	PlanID            string                   `json:"plan_id,omitempty"`
	PassID            string                   `json:"pass_id,omitempty"`
	Provenance        intake.ProvenanceSummary `json:"provenance"`
}

// HandleCreateRunFromPlannerHandoff implements the create_run_from_planner_handoff MCP tool.
func (s *Server) HandleCreateRunFromPlannerHandoff(rawArgs json.RawMessage) ToolCallResult {
	if s.deps == nil || s.deps.Store == nil {
		return toolErr("DEPENDENCY_ERROR: MCP server is not connected to a Relay store; start with RELAY_DB_PATH set")
	}

	var input createRunInput
	if err := json.Unmarshal(rawArgs, &input); err != nil {
		return toolErr(fmt.Sprintf("invalid arguments: %s", err))
	}

	if strings.TrimSpace(input.PlannerHandoffMarkdown) == "" {
		return toolErr("VALIDATION_ERROR: planner_handoff_markdown is required and must not be empty")
	}

	source := input.Source
	if source == "" {
		source = "mcp_chat"
	}

	svc := intake.NewService(s.deps.Store)
	out, err := svc.CreateRunFromHandoff(intake.CreateRunInput{
		Markdown:         input.PlannerHandoffMarkdown,
		RepoTarget:       input.RepoTarget,
		BranchContext:    input.BranchContext,
		Name:             input.Name,
		Source:           source,
		ClientTraceID:    input.ClientTraceID,
		PlanID:           input.PlanID,
		PassID:           input.PassID,
		ContextPacketID:  input.ContextPacketID,
		SourceSnapshotID: input.SourceSnapshotID,
	})
	if err != nil {
		var inputErr *intake.InputError
		if errors.As(err, &inputErr) {
			return toolErr(fmt.Sprintf("%s: %s", inputErr.Code, inputErr.Message))
		}
		return toolErr(fmt.Sprintf("INTAKE_ERROR: %s", err))
	}

	result := createRunOutput{
		OK:                true,
		Tool:              "create_run_from_planner_handoff",
		RunID:             out.RunID,
		Status:            out.Status,
		LifecycleState:    out.LifecycleState,
		ReviewURL:         out.ReviewURL,
		ArtifactKinds:     out.ArtifactKinds,
		ValidationSummary: out.ValidationSummary,
		PlanID:            out.PlanID,
		PassID:            out.PassID,
		Provenance:        out.Provenance,
	}

	text, err := marshalTool(result)
	if err != nil {
		return toolErr(fmt.Sprintf("INTERNAL_ERROR: %s", err))
	}
	return toolOK(text)
}

// createRunSchema is the JSON Schema for create_run_from_planner_handoff.
var createRunSchema = json.RawMessage(`{
  "type": "object",
  "required": ["planner_handoff_markdown"],
  "properties": {
    "planner_handoff_markdown": {
      "type": "string",
      "description": "Full planner handoff markdown content from the current chat conversation. Pass this when the user asks to submit a handoff to Relay. Relay does not read chat directly; you must extract and pass the content here. WARNING: do not include secrets, tokens, auth headers, private keys, or signed URLs."
    },
    "repo_target": {
      "type": "string",
      "description": "Target repository name or path. Optional if the handoff frontmatter contains repo or repo_target."
    },
    "branch_context": {
      "type": "string",
      "description": "Target branch name. Optional; falls back to frontmatter branch_context or 'main'."
    },
    "name": {
      "type": "string",
      "description": "Explicit run title. Optional; derived from frontmatter title or first H1 heading if absent."
    },
    "source": {
      "type": "string",
      "description": "Origin tag for this submission. Default 'mcp_chat'."
    },
    "client_trace_id": {
      "type": "string",
      "description": "Optional opaque trace identifier from the calling MCP client."
    },
    "plan_id": {
      "type": "string",
      "description": "Optional Relay plan identifier to associate with the created run."
    },
    "pass_id": {
      "type": "string",
      "description": "Optional Relay pass identifier to associate with the created run. Requires plan_id."
    },
    "source_snapshot_id": {
      "type": "string",
      "description": "Optional source snapshot identifier used to prepare the reviewed handoff."
    },
    "context_packet_id": {
      "type": "string",
      "description": "Optional context packet identifier used to prepare the reviewed handoff."
    }
  }
}`)

// ToolCreateRunFromPlannerHandoff is the ToolDefinition for create_run_from_planner_handoff.
var ToolCreateRunFromPlannerHandoff = ToolDefinition{
	Name: "create_run_from_planner_handoff",
	Description: "Submit planner handoff markdown from the current chat conversation to Relay as a new run. " +
		"Use this tool when the user asks to send, submit, or register a handoff in Relay. " +
		"The MCP client/LLM must extract the handoff content from the chat and pass it as the " +
		"planner_handoff_markdown argument — Relay does not read chat messages directly. " +
		"Returns a bounded summary with run_id, status, lifecycle_state, review_url, and artifact_kinds. " +
		"Does not return full artifact contents. " +
		"WARNING: do not include secrets, tokens, auth headers, private keys, or signed URLs in the markdown.",
	InputSchema: createRunSchema,
}
