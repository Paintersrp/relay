package mcp

import (
	"log/slog"

	appaudits "relay/internal/app/audits"
	workflowpackages "relay/internal/app/packages"
	workflowstore "relay/internal/store/workflow"
)

// MCPDeps contains dependencies shared by the role-specific MCP applications.
type MCPDeps struct {
	WorkflowStore        *workflowstore.Store
	Log                  *slog.Logger
	ArtifactFileFetcher  ArtifactFileParameterFetcher
	WorkflowAuditService WorkflowAuditToolService
}

func NewWorkflowDepsFromEnv(workflowStore *workflowstore.Store, log *slog.Logger, sourceVaults workflowpackages.SourceVaultReader) *MCPDeps {
	fetcher := NewHTTPSFileParameterFetcher()
	var auditService WorkflowAuditToolService
	if workflowStore != nil && sourceVaults != nil {
		service, _ := appaudits.NewWorkflowAuditServiceWithSourceVaults(workflowStore, sourceVaults)
		auditService = service
	}
	return &MCPDeps{
		WorkflowStore:        workflowStore,
		Log:                  log,
		ArtifactFileFetcher:  fetcher,
		WorkflowAuditService: auditService,
	}
}
