# Relay Smoke Testing Guide

This guide details the local, deterministic smoke tests and port layout configurations for the Relay workbench.

## Smoke Test Suite

To run the entire smoke verification suite (including Go E2E/mcp/server tests and React typecheck/vitest):

```bash
npm run smoke
```

### Individual Components

You can run Go tests for specific components:

- **E2E Pipeline Integration**: `go test ./internal/smoke`
- **MCP Server Surfaces**: `go test ./internal/mcp`
- **Router Compatibility**: `go test ./internal/server`

You can run React frontend checks from the root directory:

- **TypeScript Typecheck**: `npm --prefix apps/web run typecheck`
- **Unit Tests**: `npm --prefix apps/web test`

## Expected Port Layout

Relay uses the following default ports for local services. Ensure these ports are free before starting the application:

- **Relay API Service**: `8080` (e.g. `http://127.0.0.1:8080`)
- **Relay MCP Service (HTTP Mode)**: `8081` (e.g. `http://127.0.0.1:8081/mcp`)
- **ChatGPT MCP Tunnel Health/Admin Listener**: `8082` (e.g. `http://127.0.0.1:8082/ui`)
- **Vite Web Development Server**: `5173` (e.g. `http://127.0.0.1:5173`)

## Tunnel Port Collision Diagnostics

The ChatGPT MCP local tunnel script (`scripts/local/chatgpt-mcp.mjs`) automatically performs port collision checks when running diagnostics or starting the tunnel client:

```bash
npm run chatgpt-mcp:doctor
```

If the tunnel health listener port collides with the Relay API, Relay MCP, or Vite dev server, a warning will be displayed and the doctor check will log the collision.
