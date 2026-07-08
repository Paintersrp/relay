package mcp

import (
	"log/slog"

	appaudits "relay/internal/app/audits"
	workflowstore "relay/internal/store/workflow"
)

// MCPDeps contains only the dependencies used by the Planner, Auditor, and local-operator tool profiles.
type MCPDeps struct {
	WorkflowStore        *workflowstore.Store
	Log                  *slog.Logger
	ToolProfile          ToolProfile
	ArtifactFileFetcher  ArtifactFileParameterFetcher
	WorkflowAuditService WorkflowAuditToolService
}

func NewWorkflowDepsFromEnv(workflowStore *workflowstore.Store, log *slog.Logger) *MCPDeps {
	fetcher := NewHTTPSFileParameterFetcher()
	auditService, _ := appaudits.NewWorkflowAuditService(workflowStore)
	return &MCPDeps{
		WorkflowStore:        workflowStore,
		Log:                  log,
		ToolProfile:          ToolProfileFromEnv(log),
		ArtifactFileFetcher:  fetcher,
		WorkflowAuditService: auditService,
	}
}
