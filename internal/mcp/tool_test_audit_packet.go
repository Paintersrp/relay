package mcp

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"relay/internal/artifacts"
)

// submitTestAuditPacketInput is the expected input for the feasibility test tool.
type submitTestAuditPacketInput struct {
	RunID               string `json:"run_id"`
	AuditPacketMarkdown string `json:"audit_packet_markdown"`
	Decision            string `json:"decision"`
}

// submitTestAuditPacketOutput is the structured success payload returned to the MCP client.
type submitTestAuditPacketOutput struct {
	OK           bool   `json:"ok"`
	Tool         string `json:"tool"`
	RunID        string `json:"run_id"`
	Decision     string `json:"decision"`
	ArtifactPath string `json:"artifact_path"`
	Message      string `json:"message"`
}

// supportedTestDecisions are the valid values for the decision field.
var supportedTestDecisions = map[string]bool{
	"accepted":               true,
	"accepted_with_warnings": true,
	"revision_required":      true,
	"blocked":                true,
	"manual_review_required": true,
}

// testArtifactKind is registered in artifacts.allowedKinds via an addition to
// artifacts/paths.go. The kind must be in the allowed set before Write will accept it.
// We use "audit_packet" (already in allowed set) with the filename mcp_test_audit_packet.md
// so that it persists through relay artifact conventions without inventing new storage.
const testArtifactKind = "audit_packet"
const testArtifactFilename = "mcp_test_audit_packet.md"

// mcpTestRunID is a synthetic run ID used only for the feasibility test artifact.
// Run ID 0 is not a valid database run, so we use a sentinel directory. We use 0
// as a distinct directory that cannot collide with real run artifact directories.
const mcpTestRunID int64 = 0

// HandleSubmitTestAuditPacket implements the submit_test_audit_packet MCP tool.
//
// Accepted payload (mcp-test smoke):
//
//	{
//	  "run_id": "mcp-test",
//	  "audit_packet_markdown": "# Test Packet\n\nThis is a feasibility test.",
//	  "decision": "accepted"
//	}
//
// Returns structured success or a tool-level error.
func HandleSubmitTestAuditPacket(rawArgs json.RawMessage) ToolCallResult {
	var input submitTestAuditPacketInput
	if err := json.Unmarshal(rawArgs, &input); err != nil {
		return toolErr(fmt.Sprintf("invalid arguments: %s", err))
	}

	// Validate required fields.
	if strings.TrimSpace(input.RunID) == "" {
		return toolErr("VALIDATION_ERROR: run_id is required and must not be empty")
	}
	if strings.TrimSpace(input.AuditPacketMarkdown) == "" {
		return toolErr("VALIDATION_ERROR: audit_packet_markdown is required and must not be empty")
	}
	if input.Decision == "" {
		return toolErr("VALIDATION_ERROR: decision is required; must be one of: accepted, accepted_with_warnings, revision_required, blocked, manual_review_required")
	}
	if !supportedTestDecisions[input.Decision] {
		return toolErr(fmt.Sprintf("VALIDATION_ERROR: unsupported decision %q; must be one of: accepted, accepted_with_warnings, revision_required, blocked, manual_review_required", input.Decision))
	}

	// Build the durable artifact content through relay artifact conventions.
	content := fmt.Sprintf(
		"# MCP Feasibility Test Audit Packet\n\n"+
			"## Metadata\n\n"+
			"- Tool: submit_test_audit_packet\n"+
			"- Run ID (test): %s\n"+
			"- Decision: %s\n"+
			"- Submitted: %s\n"+
			"- Source: MCP feasibility gate (Pass 13A)\n\n"+
			"## Submitted Packet Content\n\n%s\n",
		input.RunID,
		input.Decision,
		time.Now().UTC().Format(time.RFC3339),
		input.AuditPacketMarkdown,
	)

	// Persist through existing relay artifact Write convention.
	// mcpTestRunID=0 creates data/artifacts/0/mcp_test_audit_packet.md.
	path, err := artifacts.Write(mcpTestRunID, testArtifactKind, testArtifactFilename, []byte(content))
	if err != nil {
		return toolErr(fmt.Sprintf("ARTIFACT_ERROR: failed to persist test artifact: %s", err))
	}

	out := submitTestAuditPacketOutput{
		OK:           true,
		Tool:         "submit_test_audit_packet",
		RunID:        input.RunID,
		Decision:     input.Decision,
		ArtifactPath: path,
		Message:      "Pass 13A feasibility test artifact written successfully. Validate manually that the artifact exists at the reported path before proceeding to Pass 13B.",
	}

	text, err := marshalTool(out)
	if err != nil {
		return toolErr(fmt.Sprintf("INTERNAL_ERROR: %s", err))
	}
	return toolOK(text)
}

// submitTestAuditPacketSchema is the JSON Schema for the submit_test_audit_packet input.
var submitTestAuditPacketSchema = json.RawMessage(`{
  "type": "object",
  "required": ["run_id", "audit_packet_markdown", "decision"],
  "properties": {
    "run_id": {
      "type": "string",
      "description": "Identifier for this test submission. Use 'mcp-test' for the smoke test."
    },
    "audit_packet_markdown": {
      "type": "string",
      "description": "Markdown content of the audit packet to persist as a test artifact."
    },
    "decision": {
      "type": "string",
      "description": "Audit decision. Must be one of: accepted, accepted_with_warnings, revision_required, blocked, manual_review_required.",
      "enum": ["accepted", "accepted_with_warnings", "revision_required", "blocked", "manual_review_required"]
    }
  }
}`)

// ToolSubmitTestAuditPacket is the ToolDefinition for submit_test_audit_packet.
var ToolSubmitTestAuditPacket = ToolDefinition{
	Name: "submit_test_audit_packet",
	Description: "Pass 13A feasibility tool. Validates that the Relay MCP bridge is reachable and " +
		"can write a durable test artifact through Relay artifact conventions. " +
		"Write a synthetic audit packet with run_id='mcp-test', any markdown, and decision='accepted'. " +
		"Pass 13B real tools (create_run_from_planner_handoff, list_open_runs, etc.) " +
		"are NOT available until this tool succeeds and the artifact is confirmed on disk.",
	InputSchema: submitTestAuditPacketSchema,
}
