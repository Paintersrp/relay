# ChatGPT Model Context Protocol (MCP) Integration

This document describes how to configure and use Relay's remote MCP endpoint to connect ChatGPT directly to your Relay daemon.

## Overview

Relay exposes a secure Model Context Protocol (MCP) endpoint over HTTPS. This allows ChatGPT to serve as a companion client that can retrieve status, list runs, dispatch planner handoffs, and submit review audits directly into your local or private Relay environment.

```
ChatGPT conversation
  -> ChatGPT App/Connector Tool Invocation (HTTPS)
  -> Secure Tunnel (ngrok, Cloudflare Tunnel, etc.)
  -> Local Go Daemon (POST /mcp)
  -> Relay Store / Artifacts
```

> [!NOTE]
> The command-line utility `bin/relay-mcpserver.exe` (or `relay-mcpserver`) is an optional developer tool for local stdio-based clients (such as Claude Desktop or Cursor). The primary integration path for ChatGPT is the remote `/mcp` HTTPS endpoint served by the main Go daemon (`cmd/relay`).

---

## Safety Boundaries

To protect the local development environment, the remote MCP endpoint operates under a strict sandbox design. **None of the following operations are exposed to ChatGPT**:
- **Shell Execution**: No arbitrary commands can be executed.
- **Arbitrary File Access**: ChatGPT cannot read or write arbitrary files on the server's filesystem.
- **Git Mutation**: ChatGPT cannot commit, push, create branches, or perform worktree changes.
- **Secrets Exposure**: No environment variables, private keys, or API tokens are exposed. All tool parameters are strictly validated, and the server refuses parameters containing auth secrets.

---

## Setup Instructions

### 1. Configure the Daemon Authentication (Optional)
The remote `/mcp` endpoint supports both Bearer Token authentication and unauthenticated access (for local/ngrok connector proof):

- **No-Auth Mode (Default for proofing)**:
  If the `RELAY_MCP_AUTH_TOKEN` environment variable is unset or empty, `/mcp` will accept unauthenticated requests. This is the recommended mode for local development and initial ChatGPT connector registration proof.

  When running in this mode, Relay will log a clear warning:
  `Relay MCP HTTP endpoint running without auth; intended for local connector proof only`

- **Token-Configured Mode (For hardening)**:
  To enforce Bearer Token authentication, set the `RELAY_MCP_AUTH_TOKEN` environment variable before launching the Relay daemon:

  ```bash
  # Windows PowerShell
  $env:RELAY_MCP_AUTH_TOKEN="your-secure-mcp-token"
  go run ./cmd/relay

  # Linux / macOS
  RELAY_MCP_AUTH_TOKEN="your-secure-mcp-token" go run ./cmd/relay
  ```

### 2. Expose the Local Daemon over HTTPS
ChatGPT requires a secure HTTPS URL to interact with your local daemon. You must expose the Go daemon (defaulting to port `8080`) using an HTTPS tunnel.

#### Using ngrok:
```bash
ngrok http 8080
```
This will provide a public forwarding URL, for example: `https://abcd-123-45-67.ngrok-free.app`.

### 3. Direct Validation Recipe (curl)
Before configuring ChatGPT, verify your endpoint responds correctly:

#### Test initialize:
```bash
curl -i -X POST https://<ngrok-host>/mcp \
  -H "Content-Type: application/json" \
  -H "Accept: application/json, text/event-stream" \
  -H "MCP-Protocol-Version: 2024-11-05" \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"curl","version":"test"}}}'
```

Expected Response:
`200 OK` with JSON matching the MCP initialization schema (including server capabilities and client info).

#### Test tools/list:
```bash
curl -i -X POST https://<ngrok-host>/mcp \
  -H "Content-Type: application/json" \
  -H "Accept: application/json, text/event-stream" \
  -H "MCP-Protocol-Version: 2024-11-05" \
  -d '{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}'
```

Expected Response:
`200 OK` listing the approved five tools.

### 4. Add the Connector in ChatGPT
1. Navigate to the ChatGPT Workspace Settings.
2. Select **Connected Data** or **Connectors** (Developer flow).
3. Add a new custom MCP Connector.
4. Fill in the form:
   - **Connection**: Server URL
   - **Authentication**: No Auth *(or Bearer Token if `RELAY_MCP_AUTH_TOKEN` is configured)*
   - **URL**: `https://<ngrok-host>/mcp`
5. Check any required acknowledgment/risk checkboxes.
6. Click **Create** to verify that the connector successfully connects and registers the approved tools.

---

## Approved Tools

The connector registers exactly five approved tools:

| Tool Name | When ChatGPT Should Call It | Description / Capabilities |
| :--- | :--- | :--- |
| `submit_test_audit_packet` | When validating connectivity | Feasibility gate check tool. Writes a synthetic audit packet directly to run ID `0` to prove write access. |
| `create_run_from_planner_handoff` | When the user asks to send a handoff | Submits planner handoff markdown from the current chat to Relay as a new run. ChatGPT extracts the handoff text from the conversation and passes it as the payload. |
| `list_open_runs` | When the user asks what runs exist | Lists active non-terminal runs. Returns a bounded summary (ID, title, repository, branch, status, lifecycle state, updated time, review URL). |
| `get_run_status` | When the user asks for a run status | Retrieves a detailed lifecycle snapshot of a single run by ID. |
| `submit_audit_packet` | When the user asks to submit an audit | Submits a completed audit packet (markdown) and decision back into Relay for an active run. |

### Tool Usage Examples

- **Example Prompt**: *"Send my current implementation handoff to Relay"*
  - **ChatGPT Action**: Call `create_run_from_planner_handoff` with the extracted markdown of the handoff.
- **Example Prompt**: *"What runs are currently active in my Relay workbench?"*
  - **ChatGPT Action**: Call `list_open_runs`.
- **Example Prompt**: *"Get the status of run #42"*
  - **ChatGPT Action**: Call `get_run_status` with `run_id="42"`.
- **Example Prompt**: *"Approve the code execution for run #42 with decision accepted"*
  - **ChatGPT Action**: Call `submit_audit_packet` with `run_id="42"`, the markdown audit details, and `decision="accepted"`.
