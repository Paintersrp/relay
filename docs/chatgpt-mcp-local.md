# ChatGPT Local MCP Tunnel

This document details the configuration and operations for running the ChatGPT local tunnel connector with Relay.

For a broader overview of the local operator layout and setup, please refer to the [Relay Operator Guide](operator-guide.md). For detailed Model Context Protocol specifications and tool list, see the [MCP Specification](mcp.md).

---

## Default Stdio Workflow

The default ChatGPT local tunnel path uses Relay's stdio MCP server through `cmd/mcpserver`. 

> [!NOTE]
> In stdio mode, you do not need to run the Go HTTP daemon (`go run ./cmd/relay`) separately. The tunnel client automatically spawns the stdio MCP server.

To start the tunnel:
1.  Configure credentials and environment settings in `.env.local` (see below).
2.  Run the initialization script once:
    ```bash
    npm run chatgpt-mcp:init
    ```
3.  Start the tunnel client for daily use:
    ```bash
    npm run chatgpt-mcp:start
    ```
4.  Keep that terminal open while ChatGPT uses the connector.

---

## Environment Configuration

Create or edit `.env.local` in the root of the repository. `.env` and `.env.local` files are ignored by git and must never be committed.

### Required Values

*   `TUNNEL_PROFILE`: The tunnel profile name (defaults to `relay-mcp`).
*   `TUNNEL_ID`: The unique tunnel identifier (e.g. `tunnel_xxxx...`).
*   `CONTROL_PLANE_API_KEY`: The authorization key for the tunnel control plane.

### Optional Values

*   `TUNNEL_MCP_TRANSPORT`: MCP communication transport. Set to `stdio` (default) or `http`.
*   `TUNNEL_HEALTH_LISTEN_ADDR`: Health and admin UI listener address (defaults to `127.0.0.1:8082` to prevent port collisions with Relay's default `8080`).
*   `TUNNEL_CLIENT_PATH`: Path to the `tunnel-client` binary if it is not on your system's `PATH`.
*   `RELAY_MCP_SERVER_BIN`: Prebuilt Relay MCP binary path (if you want the stdio launcher to use it instead of running `go run ./cmd/mcpserver`).
*   `RELAY_MCP_STDIO_COMMAND`: Custom command to override the default node-spawn command.
*   `RELAY_MCP_PROFILE`: The active tool profile. Defaults to `local-operator` (enables context broker). Set to `restricted` to hide broker tools.
*   `RELAY_DB_PATH`: Custom path to the SQLite database (defaults to `data/relay.sqlite`).
*   `RELAY_ARTIFACTS_DIR`: Custom path to the artifacts directory (defaults to `data/artifacts`).

---

## Diagnostics (Doctor command)

To verify the tunnel configuration and check if the necessary tools are exposed:

```bash
npm run chatgpt-mcp:doctor
```

In the default `stdio` mode, this:
1.  Runs the local stdio launcher self-test to verify that `mcpserver` is working and registers the two default tools (`create_run_from_planner_handoff` and `submit_planner_pass_plan`).
2.  Invokes `tunnel-client doctor` to check tunnel connectivity and status.

---

## Optional HTTP Mode

For advanced or local development use, you can configure the tunnel to use HTTP transport:

```dotenv
TUNNEL_MCP_TRANSPORT=http
RELAY_MCP_URL=http://127.0.0.1:8081/mcp
```

When HTTP mode is selected:
1.  You **must** separately run the Relay HTTP daemon:
    ```bash
    go run ./cmd/relay
    ```
2.  The tunnel client forwards requests via HTTP POST JSON-RPC. 
    *   *Note: Accessing `/mcp` via GET is not supported by the protocol and returns HTTP 405.*

---

## Safety and Security

*   **Never commit `.env` or `.env.local` files.** Keep all credentials out of source control.
*   **Never commit tunnel IDs, control-plane keys, or other secrets.**
*   **Do not paste secrets into Planner handoffs or Relay MCP tool arguments.** All arguments and payloads are written as plaintext artifacts in the `data/artifacts` folder.
*   The current HTTP `/mcp` no-auth behavior is strictly for local validation/development use and is **not** production deployment guidance. Always enable authentication before exposing the daemon beyond local loops.
*   The local stdio transport limits the tunnel client to communicating with the local `mcpserver` process spawned on your machine, preventing external callers from accessing arbitrary network services.

