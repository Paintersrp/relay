package mcp

import (
	"log/slog"

	driftapp "relay/internal/app/drift"
	appplans "relay/internal/app/plans"
	"relay/internal/store"
	workflowstore "relay/internal/store/workflow"
)

// MCPDeps holds external dependencies injected into the MCP server and its tools.
// All fields are optional at construction; tools that require a dependency return
// a bounded tool-level blocker when the required dependency is unavailable.
type MCPDeps struct {
	Store                *store.Store
	WorkflowStore        *workflowstore.Store
	Log                  *slog.Logger
	ToolProfile          ToolProfile
	FileFetcher          FileParameterFetcher
	CanonicalFileFetcher CanonicalFileParameterFetcher

	// Drift is retained only for direct compile-time legacy handler tests. Legacy
	// handlers are not registered in any production MCP profile.
	Drift *driftapp.Service

	// Deprecated: retained only so older direct callers still compile.
	ContextBrokerEnabled bool
}

// NewDepsFromEnv retains legacy dependencies for direct handler compatibility,
// but NewServer always exposes only the canonical profile registry.
func NewDepsFromEnv(st *store.Store, log *slog.Logger) *MCPDeps {
	deps := &MCPDeps{
		Store:       st,
		Log:         log,
		ToolProfile: ToolProfileFromEnv(log),
		FileFetcher: NewHTTPSFileParameterFetcher(),
	}
	deps.Drift = driftapp.NewService(appplans.NewService(st), driftapp.NewReviewerFromEnv(log), log)
	return deps
}

func NewCanonicalDepsFromEnv(workflowStore *workflowstore.Store, log *slog.Logger) *MCPDeps {
	fetcher := NewHTTPSFileParameterFetcher()
	return &MCPDeps{
		WorkflowStore:        workflowStore,
		Log:                  log,
		ToolProfile:          ToolProfileFromEnv(log),
		FileFetcher:          fetcher,
		CanonicalFileFetcher: fetcher,
	}
}
