// Command mcpserver starts the Relay MCP stdio server.
//
// MCP clients (Claude Desktop, Cursor, etc.) launch this binary as a subprocess
// and communicate with it over stdin/stdout using newline-delimited JSON-RPC 2.0.
//
// Example MCP client config (Claude Desktop):
//
//	{
//	  "mcpServers": {
//	    "relay": {
//	      "command": "/path/to/relay-mcpserver",
//	      "args": [],
//	      "env": {
//	        "RELAY_DB_PATH": "/path/to/data/relay.sqlite",
//	        "RELAY_ARTIFACTS_DIR": "/path/to/data/artifacts"
//	      }
//	    }
//	  }
//	}
//
// Environment variables:
//   - RELAY_DB_PATH: path to the SQLite database (default: data/relay.sqlite)
//   - RELAY_ARTIFACTS_DIR: path to artifact storage (default: data/artifacts)
//
// Safety boundaries: no shell execution, no arbitrary file read/write,
// no git commit/push/branch mutation is exposed through MCP tools.
// Tool arguments must not contain secrets, tokens, auth headers, private keys,
// or signed URLs — these are stored as persistent artifacts.
package main

import (
	"log/slog"
	"os"

	"relay/internal/artifacts"
	"relay/internal/config"
	"relay/internal/mcp"
	"relay/internal/store"
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	if err := config.LoadDotenvFiles(".", ".env", ".env.local"); err != nil {
		log.Warn("loading local env files", "error", err)
	}

	// Configure artifact base directory.
	artifactsDir := os.Getenv("RELAY_ARTIFACTS_DIR")
	if artifactsDir == "" {
		artifactsDir = "data/artifacts"
	}
	artifacts.SetBaseDir(artifactsDir)

	// Open the Relay SQLite store. The store auto-migrates on first open.
	dbPath := os.Getenv("RELAY_DB_PATH")
	if dbPath == "" {
		dbPath = "data/relay.sqlite"
	}

	s, err := store.Open(dbPath, log)
	if err != nil {
		log.Error("relay MCP server: cannot open database", "path", dbPath, "error", err)
		os.Exit(1)
	}
	defer s.Close()

	log.Info("relay MCP server starting",
		"transport", "stdio",
		"protocol", mcp.MCPProtocolVersion,
		"db_path", dbPath,
		"artifacts_dir", artifactsDir,
	)

	deps := mcp.NewDepsFromEnv(s, log)
	log.Info("relay MCP profile selected", "mcp_profile", deps.ToolProfile)

	srv := mcp.NewServer(log, deps)
	if err := srv.Serve(os.Stdin, os.Stdout); err != nil {
		log.Error("mcp serve error", "error", err)
		os.Exit(1)
	}
}
