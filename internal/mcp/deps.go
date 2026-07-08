package mcp

import (
	"log/slog"

	appaudits "relay/internal/app/audits"
	workflowstore "relay/internal/store/workflow"
)

// MCPDeps contains only the dependencies used by the canonical Planner,
// Auditor, and local-operator tool profiles.
type MCPDeps struct {
	WorkflowStore        *workflowstore.Store
	Log                  *slog.Logger
	ToolProfile          ToolProfile
	CanonicalFileFetcher CanonicalFileParameterFetcher
	WorkflowAuditService WorkflowAuditToolService
}

func NewCanonicalDepsFromEnv(workflowStore *workflowstore.Store, log *slog.Logger) *MCPDeps {
	fetcher := NewHTTPSFileParameterFetcher()
	auditService, _ := appaudits.NewWorkflowAuditService(workflowStore)
	return &MCPDeps{
		WorkflowStore:        workflowStore,
		Log:                  log,
		ToolProfile:          ToolProfileFromEnv(log),
		CanonicalFileFetcher: fetcher,
		WorkflowAuditService: auditService,
	}
}