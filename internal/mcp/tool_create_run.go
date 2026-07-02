package mcp

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"relay/internal/intake"
)

const maxPlannerHandoffFileBytes = 1 * 1024 * 1024

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

// createRunFromFileInput is the expected input for create_run_from_planner_handoff_file.
//
// WARNING: Do NOT include secrets, tokens, auth headers, private keys, or signed URLs
// in the supplied handoff file. Relay stores this content as a persistent artifact.
type createRunFromFileInput struct {
	// PlannerHandoffFile is the mounted MCP file-parameter path to one reviewed .md handoff.
	PlannerHandoffFile string `json:"planner_handoff_file"`
	// ExpectedSHA256 optionally pins the exact expected SHA-256 of the file bytes.
	ExpectedSHA256 string `json:"expected_sha256,omitempty"`
	// RepoTarget is the target repository name or path. Optional if present in frontmatter.
	RepoTarget string `json:"repo_target,omitempty"`
	// BranchContext is the target branch. Optional; falls back to frontmatter or "main".
	BranchContext string `json:"branch_context,omitempty"`
	// Name is an explicit run title. Optional; derived from frontmatter or H1 if absent.
	Name string `json:"name,omitempty"`
	// Source identifies the origin of this submission. Default "mcp_file_parameter".
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
	OK                     bool                       `json:"ok"`
	Tool                   string                     `json:"tool"`
	RunID                  int64                      `json:"run_id"`
	Status                 string                     `json:"status"`
	LifecycleState         string                     `json:"lifecycle_state"`
	ReviewURL              string                     `json:"review_url"`
	ArtifactKinds          []string                   `json:"artifact_kinds"`
	ValidationSummary      intake.ValidationSummary   `json:"validation_summary"`
	PlanID                 string                     `json:"plan_id,omitempty"`
	PassID                 string                     `json:"pass_id,omitempty"`
	Provenance             intake.ProvenanceSummary   `json:"provenance"`
	SubmittedHandoffSHA256 string                     `json:"submitted_handoff_sha256,omitempty"`
	ExpectedSHA256         string                     `json:"expected_sha256,omitempty"`
	SHAMatch               bool                       `json:"sha_match,omitempty"`
	SHAMatchStatus         string                     `json:"sha_match_status,omitempty"`
	SourceMode             string                     `json:"source_mode,omitempty"`
	ArtifactIdentity       *SubmittedArtifactIdentity `json:"artifact_identity,omitempty"`
}

// HandleCreateRunFromPlannerHandoff implements the create_run_from_planner_handoff MCP tool.
func (s *Server) HandleCreateRunFromPlannerHandoff(rawArgs json.RawMessage) ToolCallResult {
	if s.deps == nil || s.deps.Store == nil {
		return createRunBlocked("create_run_from_planner_handoff", MCPBlockerToolUnavailable, "DEPENDENCY_ERROR: MCP server is not connected to a Relay store.", false, nil, nil, nil)
	}

	var input createRunInput
	if err := json.Unmarshal(rawArgs, &input); err != nil {
		return createRunBlocked("create_run_from_planner_handoff", MCPBlockerSchemaMismatch, "invalid arguments: "+err.Error(), false, []MCPBlockerEvidence{{Kind: "schema", Ref: "create_run_from_planner_handoff"}}, []string{"Retry with arguments matching the create_run_from_planner_handoff schema."}, nil)
	}

	if strings.TrimSpace(input.PlannerHandoffMarkdown) == "" {
		return createRunBlocked("create_run_from_planner_handoff", MCPBlockerSchemaMismatch, "VALIDATION_ERROR: planner_handoff_markdown is required and must not be empty", false, []MCPBlockerEvidence{{Kind: "schema", Ref: "planner_handoff_markdown"}}, []string{"Provide reviewed Planner handoff markdown before submitting a run."}, nil)
	}

	source := input.Source
	if source == "" {
		source = "mcp_chat"
	}
	provenance := exactSubmissionProvenance([]byte(input.PlannerHandoffMarkdown), "", "inline", "inline-planner-handoff")
	preflight, err := intake.ValidatePlannerHandoffForCompile(intake.HandoffPreflightInput{
		Markdown:         input.PlannerHandoffMarkdown,
		RepoTarget:       input.RepoTarget,
		BranchContext:    input.BranchContext,
		PlanID:           input.PlanID,
		PassID:           input.PassID,
		ContextPacketID:  input.ContextPacketID,
		SourceSnapshotID: input.SourceSnapshotID,
		SourceMode:       "inline",
	})
	if err != nil {
		return createRunBlocked("create_run_from_planner_handoff", MCPBlockerToolUnavailable, "preflight dependency failed: "+err.Error(), true, []MCPBlockerEvidence{{Kind: "tool", Ref: "preflight"}}, []string{"Retry after preflight dependencies are available."}, map[string]any{"provenance": provenance})
	}
	if !preflight.OK {
		return toolBlockedResult("create_run_from_planner_handoff", prefixedMCPBlockers("INTAKE_ERROR: ", mcpBlockersFromPreflight(preflight.Blockers)), map[string]any{"provenance": provenance, "preflight": preflight})
	}

	svc := intake.NewService(s.deps.Store)
	out, err := svc.CreateRunFromHandoff(intake.CreateRunInput{
		Markdown:         input.PlannerHandoffMarkdown,
		RepoTarget:       input.RepoTarget,
		BranchContext:    input.BranchContext,
		Name:             input.Name,
		Source:           source,
		SourceMode:       "inline",
		ClientTraceID:    input.ClientTraceID,
		PlanID:           input.PlanID,
		PassID:           input.PassID,
		ContextPacketID:  input.ContextPacketID,
		SourceSnapshotID: input.SourceSnapshotID,
	})
	if err != nil {
		return toolBlockedResult("create_run_from_planner_handoff", []MCPBlocker{runSubmissionBlockerFromError(err)}, map[string]any{"provenance": provenance})
	}

	result := createRunOutput{
		OK:                     true,
		Tool:                   "create_run_from_planner_handoff",
		RunID:                  out.RunID,
		Status:                 out.Status,
		LifecycleState:         out.LifecycleState,
		ReviewURL:              out.ReviewURL,
		ArtifactKinds:          out.ArtifactKinds,
		ValidationSummary:      out.ValidationSummary,
		PlanID:                 out.PlanID,
		PassID:                 out.PassID,
		Provenance:             out.Provenance,
		SubmittedHandoffSHA256: provenance.SubmittedSHA256,
		SHAMatchStatus:         provenance.SHAMatchStatus,
		SourceMode:             provenance.SourceMode,
		ArtifactIdentity:       &provenance.ArtifactIdentity,
	}

	text, err := marshalTool(result)
	if err != nil {
		return toolErr(fmt.Sprintf("INTERNAL_ERROR: %s", err))
	}
	return toolOK(text)
}

// HandleCreateRunFromPlannerHandoffFile implements create_run_from_planner_handoff_file.
func (s *Server) HandleCreateRunFromPlannerHandoffFile(rawArgs json.RawMessage) ToolCallResult {
	if s.deps == nil || s.deps.Store == nil {
		return createRunBlocked("create_run_from_planner_handoff_file", MCPBlockerToolUnavailable, "DEPENDENCY_ERROR: MCP server is not connected to a Relay store.", false, nil, nil, nil)
	}

	var input createRunFromFileInput
	if err := json.Unmarshal(rawArgs, &input); err != nil {
		return createRunBlocked("create_run_from_planner_handoff_file", MCPBlockerSchemaMismatch, "invalid arguments: "+err.Error(), false, []MCPBlockerEvidence{{Kind: "schema", Ref: "create_run_from_planner_handoff_file"}}, []string{"Retry with arguments matching the create_run_from_planner_handoff_file schema."}, nil)
	}

	markdownBytes, submittedSHA, err := readPlannerHandoffFile(input.PlannerHandoffFile)
	if err != nil {
		return createRunBlocked("create_run_from_planner_handoff_file", MCPBlockerBlockedPath, "VALIDATION_ERROR: "+err.Error(), false, []MCPBlockerEvidence{{Kind: "artifact", Ref: "planner_handoff"}}, []string{"Provide one readable reviewed .md handoff file through the MCP file parameter."}, nil)
	}
	expectedSHA := strings.TrimSpace(input.ExpectedSHA256)
	provenance := exactSubmissionProvenance(markdownBytes, expectedSHA, "file_parameter", input.PlannerHandoffFile)
	if expectedSHA != "" {
		if err := validateExpectedSHA256(expectedSHA); err != nil {
			return createRunBlocked("create_run_from_planner_handoff_file", MCPBlockerSchemaMismatch, err.Error(), false, []MCPBlockerEvidence{{Kind: "schema", Ref: "expected_sha256"}}, []string{"Use a 64-character lowercase hex SHA-256 value."}, map[string]any{"provenance": provenance})
		}
		if expectedSHA != submittedSHA {
			return createRunBlocked("create_run_from_planner_handoff_file", MCPBlockerExpectedHashMismatch, "VALIDATION_ERROR: expected_sha256 does not match submitted handoff sha256", true, expectedHashMismatchEvidence(submittedSHA, expectedSHA, provenance.ArtifactIdentity.DisplayName), []string{"Recompute expected_sha256 from the reviewed handoff file or submit the exact reviewed bytes."}, map[string]any{"provenance": provenance})
		}
	}
	preflight, err := intake.ValidatePlannerHandoffForCompile(intake.HandoffPreflightInput{
		Markdown:         string(markdownBytes),
		RepoTarget:       input.RepoTarget,
		BranchContext:    input.BranchContext,
		PlanID:           input.PlanID,
		PassID:           input.PassID,
		ContextPacketID:  input.ContextPacketID,
		SourceSnapshotID: input.SourceSnapshotID,
		SourceMode:       "file_parameter",
	})
	if err != nil {
		return createRunBlocked("create_run_from_planner_handoff_file", MCPBlockerToolUnavailable, "preflight dependency failed: "+err.Error(), true, []MCPBlockerEvidence{{Kind: "tool", Ref: "preflight"}}, []string{"Retry after preflight dependencies are available."}, map[string]any{"provenance": provenance})
	}
	if !preflight.OK {
		return toolBlockedResult("create_run_from_planner_handoff_file", prefixedMCPBlockers("INTAKE_ERROR: ", mcpBlockersFromPreflight(preflight.Blockers)), map[string]any{"provenance": provenance, "preflight": preflight})
	}

	source := input.Source
	if source == "" {
		source = "mcp_file_parameter"
	}

	svc := intake.NewService(s.deps.Store)
	out, err := svc.CreateRunFromHandoff(intake.CreateRunInput{
		Markdown:         string(markdownBytes),
		RepoTarget:       input.RepoTarget,
		BranchContext:    input.BranchContext,
		Name:             input.Name,
		Source:           source,
		SourceMode:       "file_parameter",
		ClientTraceID:    input.ClientTraceID,
		PlanID:           input.PlanID,
		PassID:           input.PassID,
		ContextPacketID:  input.ContextPacketID,
		SourceSnapshotID: input.SourceSnapshotID,
	})
	if err != nil {
		return toolBlockedResult("create_run_from_planner_handoff_file", []MCPBlocker{runSubmissionBlockerFromError(err)}, map[string]any{"provenance": provenance})
	}

	result := createRunOutput{
		OK:                     true,
		Tool:                   "create_run_from_planner_handoff_file",
		RunID:                  out.RunID,
		Status:                 out.Status,
		LifecycleState:         out.LifecycleState,
		ReviewURL:              out.ReviewURL,
		ArtifactKinds:          out.ArtifactKinds,
		ValidationSummary:      out.ValidationSummary,
		PlanID:                 out.PlanID,
		PassID:                 out.PassID,
		Provenance:             out.Provenance,
		SubmittedHandoffSHA256: submittedSHA,
		ExpectedSHA256:         expectedSHA,
		SHAMatch:               true,
		SHAMatchStatus:         provenance.SHAMatchStatus,
		SourceMode:             "file_parameter",
		ArtifactIdentity:       &provenance.ArtifactIdentity,
	}

	text, err := marshalTool(result)
	if err != nil {
		return toolErr(fmt.Sprintf("INTERNAL_ERROR: %s", err))
	}
	return toolOK(text)
}

func readPlannerHandoffFile(path string) ([]byte, string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, "", fmt.Errorf("planner_handoff_file is required")
	}
	if !strings.EqualFold(filepath.Ext(path), ".md") {
		return nil, "", fmt.Errorf("planner_handoff_file must have .md extension")
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, "", fmt.Errorf("planner_handoff_file is not readable: %w", err)
	}
	if info.IsDir() {
		return nil, "", fmt.Errorf("planner_handoff_file must be a file, not a directory")
	}
	if info.Size() == 0 {
		return nil, "", fmt.Errorf("planner_handoff_file must not be empty")
	}
	if info.Size() > maxPlannerHandoffFileBytes {
		return nil, "", fmt.Errorf("planner_handoff_file must be at most 1 MiB")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", fmt.Errorf("read planner_handoff_file: %w", err)
	}
	if len(data) == 0 {
		return nil, "", fmt.Errorf("planner_handoff_file must not be empty")
	}
	if len(data) > maxPlannerHandoffFileBytes {
		return nil, "", fmt.Errorf("planner_handoff_file must be at most 1 MiB")
	}
	sum := sha256.Sum256(data)
	return data, hex.EncodeToString(sum[:]), nil
}

func validateExpectedSHA256(value string) error {
	if len(value) != 64 {
		return fmt.Errorf("expected_sha256 must be a 64-character lowercase hex SHA-256")
	}
	if value != strings.ToLower(value) {
		return fmt.Errorf("expected_sha256 must be lowercase hex")
	}
	if _, err := hex.DecodeString(value); err != nil {
		return fmt.Errorf("expected_sha256 must be lowercase hex")
	}
	return nil
}

func exactSubmissionProvenance(data []byte, expectedSHA, sourceMode, displayName string) ExactSubmissionProvenance {
	sum := sha256.Sum256(data)
	submittedSHA := hex.EncodeToString(sum[:])
	status := "not_supplied"
	if strings.TrimSpace(expectedSHA) != "" {
		status = "mismatched"
		if expectedSHA == submittedSHA {
			status = "matched"
		}
	}
	fallback := "inline-planner-handoff"
	if sourceMode == "file_parameter" {
		fallback = "planner-handoff.md"
	}
	return ExactSubmissionProvenance{
		SubmittedSHA256: submittedSHA,
		ExpectedSHA256:  strings.TrimSpace(expectedSHA),
		SHAMatchStatus:  status,
		SourceMode:      sourceMode,
		ArtifactIdentity: SubmittedArtifactIdentity{
			ArtifactKind: "planner_handoff",
			DisplayName:  safeArtifactDisplayName(displayName, fallback),
			ByteCount:    int64(len(data)),
		},
	}
}

func createRunBlocked(tool, code, message string, recoverable bool, evidence []MCPBlockerEvidence, actions []string, metadata any) ToolCallResult {
	return toolBlockedResult(tool, []MCPBlocker{newMCPBlocker(code, message, recoverable, evidence, actions)}, metadata)
}

func runSubmissionBlockerFromError(err error) MCPBlocker {
	message := "run submission could not complete safely"
	code := MCPBlockerToolUnavailable
	recoverable := false
	evidence := []MCPBlockerEvidence{{Kind: "tool", Ref: "create_run"}}
	actions := []string{"Retry after the Relay operator inspects service availability."}

	var inputErr *intake.InputError
	if errors.As(err, &inputErr) {
		message = inputErr.Message
		recoverable = true
		evidence = []MCPBlockerEvidence{{Kind: "field", Ref: "run_submission"}}
		actions = []string{"Correct the submitted association, provenance, or metadata and retry the tool."}
		switch inputErr.Code {
		case intake.ErrCodeNotFound:
			code = MCPBlockerUnknownResource
		case intake.ErrCodeValidation, "PASS_NOT_OPEN":
			code = MCPBlockerSchemaMismatch
			lower := strings.ToLower(inputErr.Message)
			switch {
			case strings.Contains(lower, "source/context provenance"):
				code = MCPBlockerRequiredContextMissing
			case strings.Contains(lower, "safe repo-relative path"):
				code = MCPBlockerBlockedPath
				evidence = []MCPBlockerEvidence{{Kind: "field", Ref: "source_artifact_path"}, {Kind: "field", Ref: "intended_handoff_path"}}
			case strings.Contains(lower, "not a path"):
				code = MCPBlockerSchemaMismatch
			}
		}
		return newMCPBlocker(code, message, recoverable, evidence, actions)
	}

	lower := strings.ToLower(err.Error())
	if strings.Contains(lower, "resolve repository") || strings.Contains(lower, "repo is required") || strings.Contains(lower, "repository target") {
		return newMCPBlocker(MCPBlockerUnknownRepository, "repository target could not be resolved to a registered identity", true, []MCPBlockerEvidence{{Kind: "field", Ref: "repo_target"}}, []string{"Register or correct the repository target and retry the tool."})
	}
	if strings.Contains(lower, "preflight blocked") || strings.Contains(lower, "handoff validation blocked") {
		return newMCPBlocker(MCPBlockerSchemaMismatch, "handoff failed deterministic validation", true, []MCPBlockerEvidence{{Kind: "field", Ref: "planner_handoff"}}, []string{"Correct the reviewed handoff and retry the tool."})
	}
	return newMCPBlocker(code, message, recoverable, evidence, actions)
}

func expectedHashMismatchEvidence(submittedSHA, expectedSHA, artifactName string) []MCPBlockerEvidence {
	return []MCPBlockerEvidence{
		{Kind: "submitted_sha256", Ref: submittedSHA},
		{Kind: "expected_sha256", Ref: expectedSHA},
		{Kind: "artifact_name", Ref: safeArtifactDisplayName(artifactName, "planner-handoff.md")},
	}
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

// createRunFromFileSchema is the JSON Schema for create_run_from_planner_handoff_file.
var createRunFromFileSchema = json.RawMessage(`{
  "type": "object",
  "required": ["planner_handoff_file"],
  "properties": {
    "planner_handoff_file": {
      "type": "string",
      "description": "Mounted MCP file-parameter path for one reviewed Planner handoff Markdown file. Relay reads only this supplied file, requires a .md extension, and stores its exact bytes as the planner_handoff artifact."
    },
    "expected_sha256": {
      "type": "string",
      "description": "Optional lowercase hex SHA-256 expected for the exact planner_handoff_file bytes. A mismatch blocks run creation."
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
      "description": "Origin tag for this submission. Default 'mcp_file_parameter'."
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

// ToolCreateRunFromPlannerHandoffFile is the ToolDefinition for create_run_from_planner_handoff_file.
var ToolCreateRunFromPlannerHandoffFile = ToolDefinition{
	Name: "create_run_from_planner_handoff_file",
	Description: "Submit one reviewed Planner handoff Markdown file to Relay as a new run. " +
		"Use this preferred tool when the reviewed handoff exists as a file and byte identity matters. " +
		"The MCP client passes planner_handoff_file as a mounted file-parameter path; Relay reads only that file, " +
		"requires .md content at most 1 MiB, computes its exact SHA-256, and optionally verifies expected_sha256 " +
		"before creating any run. Returns submitted_handoff_sha256, sha_match, source_mode, run_id, status, " +
		"lifecycle_state, review_url, and artifact_kinds. Does not expose generic file browsing or arbitrary file reads. " +
		"WARNING: do not include secrets, tokens, auth headers, private keys, or signed URLs in the handoff file.",
	InputSchema: createRunFromFileSchema,
}
