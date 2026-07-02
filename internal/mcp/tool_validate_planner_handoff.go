package mcp

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"relay/internal/intake"
)

type validatePlannerHandoffInput struct {
	PlannerHandoffMarkdown string `json:"planner_handoff_markdown,omitempty"`
	PlannerHandoffFile     string `json:"planner_handoff_file,omitempty"`
	ExpectedSHA256         string `json:"expected_sha256,omitempty"`
	RepoTarget             string `json:"repo_target,omitempty"`
	BranchContext          string `json:"branch_context,omitempty"`
	PlanID                 string `json:"plan_id,omitempty"`
	PassID                 string `json:"pass_id,omitempty"`
	ContextPacketID        string `json:"context_packet_id,omitempty"`
	SourceSnapshotID       string `json:"source_snapshot_id,omitempty"`
}

func (s *Server) HandleValidatePlannerHandoffForCompile(rawArgs json.RawMessage) ToolCallResult {
	if s.deps == nil || s.deps.Store == nil {
		return toolErr("DEPENDENCY_ERROR: MCP server is not connected to a Relay store; start with RELAY_DB_PATH set")
	}

	var input validatePlannerHandoffInput
	if err := json.Unmarshal(rawArgs, &input); err != nil {
		return toolErr(fmt.Sprintf("invalid arguments: %s", err))
	}

	markdownStr := strings.TrimSpace(input.PlannerHandoffMarkdown)
	filePath := strings.TrimSpace(input.PlannerHandoffFile)

	if markdownStr == "" && filePath == "" {
		return toolErr("VALIDATION_ERROR: exactly one of planner_handoff_markdown or planner_handoff_file is required")
	}
	if markdownStr != "" && filePath != "" {
		return toolErr("VALIDATION_ERROR: provide exactly one of planner_handoff_markdown or planner_handoff_file, not both")
	}

	var sourceMode string
	var submittedSHA string

	if filePath != "" {
		markdownBytes, sha, err := readPlannerHandoffFile(filePath)
		if err != nil {
			return toolErr(fmt.Sprintf("VALIDATION_ERROR: %s", err))
		}
		submittedSHA = sha
		sourceMode = "file_parameter"

		expectedSHA := strings.TrimSpace(input.ExpectedSHA256)
		if expectedSHA != "" {
			if err := validateExpectedSHA256(expectedSHA); err != nil {
				return toolErr(fmt.Sprintf("VALIDATION_ERROR: %s", err))
			}
			if expectedSHA != submittedSHA {
				return toolErr(fmt.Sprintf("VALIDATION_ERROR: expected_sha256 %q does not match submitted handoff sha256 %q", expectedSHA, submittedSHA))
			}
		}

		markdownStr = string(markdownBytes)
	} else {
		sourceMode = "mcp_chat"
		sum := sha256.Sum256([]byte(markdownStr))
		submittedSHA = hex.EncodeToString(sum[:])
	}

	result, err := intake.ValidatePlannerHandoffForCompile(intake.HandoffPreflightInput{
		Markdown:         markdownStr,
		RepoTarget:       input.RepoTarget,
		BranchContext:    input.BranchContext,
		PlanID:           input.PlanID,
		PassID:           input.PassID,
		ContextPacketID:  input.ContextPacketID,
		SourceSnapshotID: input.SourceSnapshotID,
		SourceMode:       sourceMode,
	})
	if err != nil {
		return toolErr(fmt.Sprintf("INTERNAL_ERROR: %s", err))
	}

	_ = submittedSHA

	var textSummary string
	if result.OK {
		textSummary = fmt.Sprintf("Preflight passed: handoff is compile-ready (%d warnings). SHA-256: %s", result.IssueCounts["warning"], result.SubmittedHandoffSHA256)
	} else {
		textSummary = fmt.Sprintf("Preflight blocked: %d error(s), %d warning(s) found. Handoff is not compile-ready.", result.IssueCounts["error"], result.IssueCounts["warning"])
	}

	mcpResult := ToolCallResult{
		Content:           []ContentBlock{{Type: "text", Text: textSummary}},
		StructuredContent: result,
	}

	return mcpResult
}

var validatePlannerHandoffSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "planner_handoff_markdown": {
      "type": "string",
      "description": "Full Planner handoff markdown content to validate. Provide exactly one of planner_handoff_markdown or planner_handoff_file."
    },
    "planner_handoff_file": {
      "type": "string",
      "description": "Mounted MCP file-parameter path to one reviewed Planner handoff Markdown file to validate. Provide exactly one of planner_handoff_markdown or planner_handoff_file."
    },
    "expected_sha256": {
      "type": "string",
      "description": "Optional lowercase hex SHA-256 expected for the exact planner_handoff_file bytes. A mismatch returns a validation error before preflight proceeds."
    },
    "repo_target": {
      "type": "string",
      "description": "Target repository name or path. Optional if the handoff frontmatter contains repo or repo_target."
    },
    "branch_context": {
      "type": "string",
      "description": "Target branch name. Optional; falls back to frontmatter branch_context or 'main'."
    },
    "plan_id": {
      "type": "string",
      "description": "Optional Relay plan identifier for managed pass association checks."
    },
    "pass_id": {
      "type": "string",
      "description": "Optional Relay pass identifier for managed pass association checks. Requires plan_id."
    },
    "context_packet_id": {
      "type": "string",
      "description": "Optional context packet identifier used to prepare the reviewed handoff."
    },
    "source_snapshot_id": {
      "type": "string",
      "description": "Optional source snapshot identifier used to prepare the reviewed handoff."
    }
  }
}`)

var ToolValidatePlannerHandoffForCompile = ToolDefinition{
	Name: "validate_planner_handoff_for_compile",
	Description: "Validate a Planner handoff for compile readiness without creating a run. " +
		"Use this bounded preflight tool to detect handoff structure, compiler_input, provenance, " +
		"and managed plan/pass association failures before committing to a workflow transition. " +
		"Returns deterministic structured issues with codes, severity, and repair guidance. " +
		"This tool does not create runs, submit plans, dispatch executors, compile packets, " +
		"mutate git, or browse arbitrary paths. " +
		"WARNING: do not include secrets, tokens, auth headers, private keys, or signed URLs.",
	InputSchema: validatePlannerHandoffSchema,
}
