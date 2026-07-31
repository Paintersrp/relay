package mcp

import (
	"context"
	"fmt"

	appaudits "relay/internal/app/audits"
)

// WorkflowAuditToolService is the audit boundary used by the Auditor route
// dispatchers.
type WorkflowAuditToolService interface {
	GetCurrentPacket(context.Context, string) (appaudits.GetWorkflowAuditPacketResult, error)
	GetCurrentArtifact(context.Context, appaudits.GetWorkflowAuditArtifactInput) (appaudits.GetWorkflowAuditArtifactResult, error)
	RecordDecision(context.Context, appaudits.RecordWorkflowAuditDecisionInput) (appaudits.RecordWorkflowAuditDecisionResult, error)
}

func workflowOK(out any) ToolCallResult {
	text, err := marshalTool(out)
	if err != nil {
		return toolErr(fmt.Sprintf("INTERNAL_ERROR: %s", err))
	}
	return ToolCallResult{
		Content:           []ContentBlock{{Type: "text", Text: text}},
		StructuredContent: out,
	}
}
