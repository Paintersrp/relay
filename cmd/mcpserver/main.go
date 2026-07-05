// Command mcpserver starts the canonical Relay MCP stdio server.
//
// MCP clients launch this binary as a subprocess and communicate with it over
// stdin/stdout using newline-delimited JSON-RPC 2.0.
//
// Environment variables:
//   - RELAY_WORKFLOW_DB_PATH: workflow SQLite database path
//     (default: data/workflow/relay-workflow.sqlite)
//   - RELAY_WORKFLOW_ARTIFACTS_DIR: workflow artifact root
//     (default: data/workflow/artifacts)
//   - RELAY_MCP_PROFILE: planner, auditor, or local_operator
//     (default and invalid-value fallback: planner)
//
// Safety boundaries: no shell execution, no arbitrary local file browsing, and
// no git mutation are exposed through MCP tools.
package main

import (
	"log/slog"
	"os"

	"relay/internal/config"
	"relay/internal/mcp"
	workflowstore "relay/internal/store/workflow"
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	if err := config.LoadDotenvFiles(".", ".env", ".env.local"); err != nil {
		log.Warn("loading local env files", "error", err)
	}

	workflowDBPath := os.Getenv("RELAY_WORKFLOW_DB_PATH")
	if workflowDBPath == "" {
		workflowDBPath = "data/workflow/relay-workflow.sqlite"
	}
	workflowArtifactsDir := os.Getenv("RELAY_WORKFLOW_ARTIFACTS_DIR")
	if workflowArtifactsDir == "" {
		workflowArtifactsDir = "data/workflow/artifacts"
	}

	workflowStore, err := workflowstore.Open(workflowDBPath, workflowArtifactsDir)
	if err != nil {
		log.Error("relay MCP server: cannot open workflow database", "path", workflowDBPath, "error", err)
		os.Exit(1)
	}
	defer workflowStore.Close()

	deps := mcp.NewCanonicalDepsFromEnv(workflowStore, log)
	log.Info(
		"relay MCP server starting",
		"transport", "stdio",
		"protocol", mcp.MCPProtocolVersion,
		"mcp_profile", deps.ToolProfile,
		"workflow_db_path", workflowDBPath,
		"workflow_artifacts_dir", workflowArtifactsDir,
	)

	srv := mcp.NewServer(log, deps)
	if err := srv.Serve(os.Stdin, os.Stdout); err != nil {
		log.Error("mcp serve error", "error", err)
		os.Exit(1)
	}
}
