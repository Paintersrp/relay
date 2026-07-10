package mcp

import (
	"context"
	"encoding/json"
	"strings"

	appaudits "relay/internal/app/audits"
)

const defaultWorkflowArtifactReadBytes = 12000

var getRunArtifactSchema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["run_id", "artifact_reference"],
  "properties": {
    "run_id": {"type": "string", "minLength": 1},
    "artifact_reference": {
      "type": "string",
      "minLength": 1,
      "maxLength": 128,
      "description": "Exact artifact_reference declared in the current audit packet artifacts collection."
    },
    "max_bytes": {"type": "integer", "minimum": 1, "maximum": 65536}
  }
}`)

var ToolGetRunArtifact = ToolDefinition{
	Name:        "get_run_artifact",
	Description: "Retrieve bounded exact stored evidence only by an artifact reference declared in the current authoritative audit packet. Revalidates packet freshness, Run and attempt ownership, metadata, size, and SHA-256. Does not accept paths, URLs, artifact kinds, or shell input.",
	InputSchema: getRunArtifactSchema,
}

type getRunArtifactArgs struct {
	RunID             string `json:"run_id"`
	ArtifactReference string `json:"artifact_reference"`
	MaxBytes          int    `json:"max_bytes,omitempty"`
}

func (s *Server) HandleGetRunArtifact(rawArgs json.RawMessage) ToolCallResult {
	var input getRunArtifactArgs
	if err := brokerDecodeStrict(rawArgs, &input); err != nil {
		return workflowBlocked("get_run_artifact", MCPBlockerSchemaMismatch, "invalid arguments: "+err.Error(), false, "artifact_reference", nil)
	}
	service, err := s.workflowAuditService()
	if err != nil {
		return workflowBlocked("get_run_artifact", MCPBlockerToolUnavailable, "workflow audit service is unavailable", false, "workflow_store", nil)
	}
	maxBytes := input.MaxBytes
	if maxBytes == 0 {
		maxBytes = defaultWorkflowArtifactReadBytes
	}
	result, err := service.GetCurrentArtifact(context.Background(), appaudits.GetWorkflowAuditArtifactInput{
		RunID:             strings.TrimSpace(input.RunID),
		ArtifactReference: strings.TrimSpace(input.ArtifactReference),
		MaxBytes:          maxBytes,
	})
	if err != nil {
		return workflowAuditBlocked("get_run_artifact", err)
	}
	return workflowOK(map[string]any{
		"ok":                 true,
		"tool":               "get_run_artifact",
		"run_id":             result.Run.RunID,
		"audit_packet_id":    result.Packet.AuditPacketID,
		"packet_sha256":      result.Packet.PacketSHA256,
		"artifact_reference": input.ArtifactReference,
		"artifact": map[string]any{
			"artifact_id":    result.Artifact.ArtifactID,
			"kind":           result.Artifact.Kind,
			"media_type":     result.Artifact.MediaType,
			"sha256":         result.Artifact.SHA256,
			"size_bytes":     result.Artifact.SizeBytes,
			"offset":         0,
			"returned_bytes": len(result.Content),
			"encoding":       "utf-8",
			"truncated":      result.Truncated,
		},
		"content": string(result.Content),
	})
}
