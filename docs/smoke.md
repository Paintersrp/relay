# Relay Smoke Testing Guide

This guide details the local, deterministic smoke tests and port layout configurations for the Relay workbench.

For detailed local operator workflows and setup guides, please refer to the [Relay Operator Guide](operator-guide.md).

---

## Smoke Test Suite

To run the entire smoke verification suite (including Go E2E/mcp/server tests, local script tests, and React typecheck/vitest):

```bash
npm run smoke
```

### Release Verification

For final release-hardening verification, run the comprehensive release smoke script. This script verifies all Go tests, local script connector tests, React typecheck/vitest suites, React build bundles, root smoke suites, and validation reports:

```bash
bash scripts/release-smoke.sh
```

The release script runs focused Plan Seed checks early (`go test ./internal/app/projects ./internal/api/projects ./internal/mcp -run PlanSeed -count=1` and `go run ./cmd/plan-seed-smoke`) before the broader suite.

### Individual Components

You can run individual test suites for specific parts of the project:

*   **E2E Pipeline Integration**:
    ```bash
    go test ./internal/smoke
    ```
*   **MCP Server Surfaces**:
    ```bash
    go test ./internal/mcp
    ```
*   **Router Compatibility**:
    ```bash
    go test ./internal/server
    ```
*   **Local Stdio Connector Self-Test**:
    ```bash
    npm run test:local-scripts
    ```
*   **Plan Seed Smoke Harness**:
    ```bash
    go run ./cmd/plan-seed-smoke
    # or
    make plan-seed-smoke
    # or
    npm run plan-seed-smoke
    ```
*   **React Frontend Typecheck**:
    ```bash
    npm --prefix apps/web run typecheck
    ```
*   **React Frontend Unit Tests**:
    ```bash
    npm --prefix apps/web test
    ```

---

## Expected Port Layout

Relay uses the following ports for local development. Ensure they are free or correctly configured before starting:

*   **Relay API Service (`cmd/relay`)**: Port `8080` (e.g. `http://127.0.0.1:8080`). Binds to `PORT` environment variable if set.
*   **Relay MCP Service (HTTP Mode)**: Port `8081` (e.g. `http://127.0.0.1:8081/mcp`).
*   **ChatGPT MCP Tunnel Health/Admin Listener**: Port `8082` (e.g. `http://127.0.0.1:8082/ui`). Binds to `TUNNEL_HEALTH_LISTEN_ADDR` environment variable if set.
*   **React Web Development Server (`apps/web`)**: Dynamic port. The dev server uses the port reported by the startup output of `npm --prefix apps/web run dev` (typically port `3000` or `5173`). Configure your local environment if a fixed port is needed.

---

## Diagnostics and Troubleshooting

To diagnose local tunnel and MCP setup issues, run:

```bash
npm run chatgpt-mcp:doctor
```

If the doctor check reports failures, consult the troubleshooting items below:

### 1. Doctor fails with missing `tunnel-client`
*   Ensure the `tunnel-client` binary is installed and present in your system's `PATH`.
*   Alternatively, set `TUNNEL_CLIENT_PATH` in `.env.local` to the absolute path of the `tunnel-client` executable.

### 2. Doctor fails with missing credentials
*   Ensure that `TUNNEL_ID` and `CONTROL_PLANE_API_KEY` are configured in `.env.local` and do not use replacement placeholders (e.g. `sk-REPLACE_ME`).

### 3. Stdio self-test fails
*   Verify that you can build the project successfully (`make build` or `go build ./cmd/mcpserver`).
*   Ensure that `RELAY_MCP_PROFILE` is configured correctly. If you are using prebuilt binaries, check that `RELAY_MCP_SERVER_BIN` points to the correct location.

### 4. HTTP mode check fails
*   Ensure the Go daemon is running in a separate terminal (`go run ./cmd/relay`).
*   Verify that `RELAY_MCP_URL` in `.env.local` matches the actual port the HTTP daemon is listening on.

