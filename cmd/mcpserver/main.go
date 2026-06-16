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
//	      "args": []
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
package main

import (
	"log/slog"
	"os"

	"relay/internal/artifacts"
	"relay/internal/config"
	"relay/internal/mcp"
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	if err := config.LoadDotenvFiles(".", ".env", ".env.local"); err != nil {
		log.Warn("loading local env files", "error", err)
	}

	// Configure artifact base dir if overridden.
	if dir := os.Getenv("RELAY_ARTIFACTS_DIR"); dir != "" {
		artifacts.SetBaseDir(dir)
	}

	log.Info("relay MCP server starting", "transport", "stdio", "protocol", mcp.MCPProtocolVersion)

	srv := mcp.NewServer(log)
	if err := srv.Serve(os.Stdin, os.Stdout); err != nil {
		log.Error("mcp serve error", "error", err)
		os.Exit(1)
	}
}
