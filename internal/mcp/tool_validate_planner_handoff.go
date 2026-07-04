package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"relay/internal/intake"
)

type validatePlannerHandoffInput struct {
	PlannerHandoffMarkdown string                `json:"planner_handoff_markdown,omitempty"`
	PlannerHandoffFile     *ChatGPTFileReference `json:"planner_handoff_file,omitempty"`
	ExpectedSHA256         string                `json:"expected_sha256,omitempty"`
	RepoTarget             string                `json:"repo_target,omitempty"`
	BranchContext          string                `json:"branch_context,omitempty"`
	PlanID                 string                `json:"plan_id,omitempty"`
	PassID                 string                `json:"pass_id,omitempty"`
	ContextPacketID        string                `json:"context_packet_id,omitempty"`
	SourceSnapshotID       string                `json:"source_snapshot_id,omitempty"`
}

func (s *Server) HandleValidatePlannerHandoffForCompile(rawArgs json.RawMessage) ToolCallResult {
	if s.deps == nil || s.deps.Store == nil {
		return toolBlockedResult("validate_planner_handoff_for_compile", []MCPBlocker{
			newMCPBlocker(MCPBlockerToolUnavailable, "MCP server is not connected to a Relay store.", false, []MCPBlockerEvidence{{Kind: "tool", Ref: "validate_planner_handoff_for_compile"}}, []string{"Start Relay MCP with RELAY_DB_PATH configured, then retry preflight."}),
		}, nil)
	}

	var input validatePlannerHandoffInput
	if err := brokerDecodeStrict(rawArgs, &input); err != nil {
		return toolBlockedResult("validate_planner_handoff_for_compile", []MCPBlocker{
			newMCPBlocker(MCPBlockerSchemaMismatch, "invalid arguments: "+err.Error(), false, []MCPBlockerEvidence{{Kind: "schema", Ref: "validate_planner_handoff_for_compile"}}, []string{"Retry with arguments matching the validate_planner_handoff_for_compile schema."}),
		}, nil)
	}

	markdownStr := strings.TrimSpace(input.PlannerHandoffMarkdown)
	hasFile := input.PlannerHandoffFile != nil

	if markdownStr == "" && !hasFile {
		return toolBlockedResult("validate_planner_handoff_for_compile", []MCPBlocker{
			newMCPBlocker(MCPBlockerSchemaMismatch, "exactly one of planner_handoff_markdown or planner_handoff_file is required", false, []MCPBlockerEvidence{{Kind: "schema", Ref: "planner_handoff_source"}}, []string{"Provide exactly one reviewed handoff source."}),
		}, nil)
	}
	if markdownStr != "" && hasFile {
		return toolBlockedResult("validate_planner_handoff_for_compile", []MCPBlocker{
			newMCPBlocker(MCPBlockerSchemaMismatch, "provide exactly one of planner_handoff_markdown or planner_handoff_file, not both", false, []MCPBlockerEvidence{{Kind: "schema", Ref: "planner_handoff_source"}}, []string{"Submit either inline markdown or one file parameter, not both."}),
		}, nil)
	}

	var sourceMode string
	var submittedSHA string

	if hasFile {
		content, fetchErr := s.fileParameterFetcher().FetchPlannerHandoff(context.Background(), *input.PlannerHandoffFile)
		if fetchErr != nil {
			return toolBlockedResult("validate_planner_handoff_for_compile", []MCPBlocker{fileParameterBlocker(fetchErr)}, nil)
		}
		markdownBytes := content.Bytes
		sha := sha256Hex(markdownBytes)
		submittedSHA = sha
		sourceMode = "file_parameter"

		expectedSHA := strings.TrimSpace(input.ExpectedSHA256)
		if expectedSHA != "" {
			if err := validateExpectedSHA256(expectedSHA); err != nil {
				return toolBlockedResult("validate_planner_handoff_for_compile", []MCPBlocker{
					newMCPBlocker(MCPBlockerSchemaMismatch, err.Error(), false, []MCPBlockerEvidence{{Kind: "schema", Ref: "expected_sha256"}}, []string{"Use a 64-character lowercase hex SHA-256 value."}),
				}, nil)
			}
			if expectedSHA != submittedSHA {
				prov := ExactSubmissionProvenance{
					SubmittedSHA256: submittedSHA,
					ExpectedSHA256:  expectedSHA,
					SHAMatchStatus:  "mismatched",
					SourceMode:      "file_parameter",
					ArtifactIdentity: SubmittedArtifactIdentity{
						ArtifactKind: "planner_handoff",
						DisplayName:  safeArtifactDisplayName(content.DisplayName, "planner-handoff.md"),
						ByteCount:    int64(len(markdownBytes)),
					},
				}
				return toolBlockedResult("validate_planner_handoff_for_compile", []MCPBlocker{
					newMCPBlocker(MCPBlockerExpectedHashMismatch, "expected_sha256 does not match submitted handoff sha256", true, expectedHashMismatchEvidence(submittedSHA, expectedSHA, prov.ArtifactIdentity.DisplayName), []string{"Recompute expected_sha256 from the reviewed uploaded handoff file or submit the exact reviewed bytes."}),
				}, map[string]any{"provenance": prov})
			}
		}

		markdownStr = string(markdownBytes)
	} else {
		if strings.TrimSpace(input.ExpectedSHA256) != "" {
			return toolBlockedResult("validate_planner_handoff_for_compile", []MCPBlocker{
				newMCPBlocker(MCPBlockerSchemaMismatch, "expected_sha256 is only supported with planner_handoff_file", false, []MCPBlockerEvidence{{Kind: "schema", Ref: "expected_sha256"}}, []string{"Use planner_handoff_file when pinning exact file bytes by expected_sha256."}),
			}, nil)
		}
		sourceMode = "inline"
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
		return toolBlockedResult("validate_planner_handoff_for_compile", []MCPBlocker{
			newMCPBlocker(MCPBlockerToolUnavailable, "preflight dependency failed: "+err.Error(), true, []MCPBlockerEvidence{{Kind: "tool", Ref: "preflight"}}, []string{"Retry after the preflight dependency is available."}),
		}, nil)
	}

	if result.OK {
		textSummary := fmt.Sprintf("Preflight passed: handoff is compile-ready (%d warnings). SHA-256: %s", result.IssueCounts["warning"], result.SubmittedHandoffSHA256)
		return ToolCallResult{
			Content:           []ContentBlock{{Type: "text", Text: textSummary}},
			StructuredContent: result,
		}
	}

	return toolBlockedResult("validate_planner_handoff_for_compile", mcpBlockersFromPreflight(result.Blockers), map[string]any{
		"preflight": boundedPreflightMetadata(result, submittedSHA, sourceMode),
	})
}

func mcpBlockersFromPreflight(blockers []intake.HandoffPreflightBlocker) []MCPBlocker {
	out := make([]MCPBlocker, 0, len(blockers))
	for _, blocker := range blockers {
		evidence := make([]MCPBlockerEvidence, 0, len(blocker.Evidence))
		for _, item := range blocker.Evidence {
			evidence = append(evidence, MCPBlockerEvidence{Kind: item.Kind, Ref: item.Ref, Detail: item.Detail})
		}
		out = append(out, newMCPBlocker(blocker.Code, blocker.Message, blocker.Recoverable, evidence, blocker.NextActions))
	}
	return out
}

func prefixedMCPBlockers(prefix string, blockers []MCPBlocker) []MCPBlocker {
	out := make([]MCPBlocker, 0, len(blockers))
	for _, blocker := range blockers {
		if !strings.HasPrefix(blocker.Message, prefix) {
			blocker.Message = prefix + blocker.Message
		}
		out = append(out, blocker)
	}
	return out
}

func boundedPreflightMetadata(result intake.HandoffPreflightResult, submittedSHA string, sourceMode string) map[string]any {
	issues := make([]map[string]any, 0, len(result.Issues))
	for _, issue := range result.Issues {
		if len(issues) >= maxBlockerEvidence {
			break
		}
		issues = append(issues, map[string]any{
			"code":              sanitizeBlockerCode(issue.Code),
			"severity":          string(issue.Severity),
			"blocks_submission": issue.BlocksSubmission,
		})
	}
	sha := result.SubmittedHandoffSHA256
	if sha == "" {
		sha = submittedSHA
	}
	return map[string]any{
		"status":                   "blocked",
		"is_compile_ready":         false,
		"issue_counts":             result.IssueCounts,
		"issues":                   issues,
		"submitted_handoff_sha256": sha,
		"byte_count":               result.ByteCount,
		"source_mode":              sourceMode,
	}
}

var validatePlannerHandoffSchema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "planner_handoff_markdown": {
      "type": "string",
      "description": "Full Planner handoff markdown content to validate. Provide exactly one of planner_handoff_markdown or planner_handoff_file."
    },
    "planner_handoff_file": {
      "type": "object",
      "additionalProperties": false,
      "required": ["download_url", "file_id"],
      "description": "Structured ChatGPT file reference for one reviewed Planner handoff Markdown file to validate. Provide exactly one of planner_handoff_markdown or planner_handoff_file.",
      "properties": {
        "download_url": {
          "type": "string",
          "format": "uri"
        },
        "file_id": {
          "type": "string",
          "minLength": 1
        },
        "mime_type": {
          "type": "string"
        },
        "file_name": {
          "type": "string"
        }
      }
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
	Meta:        map[string]any{"openai/fileParams": []string{"planner_handoff_file"}},
}
