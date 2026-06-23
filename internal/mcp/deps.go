package mcp

import (
	"log/slog"

	"relay/internal/store"
)

// MCPDeps holds external dependencies injected into the MCP server and its tools.
// All fields are optional at construction; tools that require a dependency will return a
// tool-level DEPENDENCY_ERROR if the required field is nil, rather than panicking.
type MCPDeps struct {
	Store       *store.Store
	Log         *slog.Logger
	ToolProfile ToolProfile

	// Deprecated: use ToolProfile. Kept only so older callers still compile.
	ContextBrokerEnabled bool
}

// NewDepsFromEnv constructs MCPDeps by loading the profile from environment variables.
func NewDepsFromEnv(st *store.Store, log *slog.Logger) *MCPDeps {
	return &MCPDeps{
		Store:       st,
		Log:         log,
		ToolProfile: ToolProfileFromEnv(log),
	}
}
