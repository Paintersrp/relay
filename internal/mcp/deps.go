package mcp

import (
	"log/slog"
	"os"
	"strings"

	driftapp "relay/internal/app/drift"
	appplans "relay/internal/app/plans"
	"relay/internal/store"
	workflowstore "relay/internal/store/workflow"
)

// MCPDeps holds external dependencies injected into the MCP server and its tools.
// All fields are optional at construction; tools that require a dependency will return a
// tool-level DEPENDENCY_ERROR if the required field is nil, rather than panicking.
type MCPDeps struct {
	Store                *store.Store
	WorkflowStore        *workflowstore.Store
	Log                  *slog.Logger
	ToolProfile          ToolProfile
	FileFetcher          FileParameterFetcher
	CanonicalFileFetcher CanonicalFileParameterFetcher

	// Drift is the optional internal drift reviewer service. It is constructed
	// with a nil provider by default, returning model_provider_unavailable until
	// a later pass configures a networked reviewer.
	Drift *driftapp.Service

	// Deprecated: use ToolProfile. Kept only so older callers still compile.
	ContextBrokerEnabled bool
}

// NewDepsFromEnv constructs MCPDeps by loading the profile from environment variables.
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
	profile := ToolProfileFromEnv(log)
	if profile == ToolProfileLocalOperator || profile == ToolProfileRestricted {
		if raw := strings.TrimSpace(os.Getenv(EnvMCPProfile)); raw == "" {
			profile = ToolProfilePlanner
		}
	}
	return &MCPDeps{
		WorkflowStore:        workflowStore,
		Log:                  log,
		ToolProfile:          profile,
		FileFetcher:          fetcher,
		CanonicalFileFetcher: fetcher,
	}
}
