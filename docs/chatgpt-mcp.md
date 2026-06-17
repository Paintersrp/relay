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

### 1. Configure the Daemon Authentication
The remote `/mcp` endpoint enforces Bearer Token authentication by default. Before launching the Relay daemon, set the `RELAY_MCP_AUTH_TOKEN` environment variable to a secure opaque string:

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

### 3. Add the Connector in ChatGPT
1. Navigate to the ChatGPT Workspace Settings.
2. Select **Connected Data** or **Connectors** (Developer flow).
3. Add a new custom MCP Connector.
4. Set the **Server URL** to your HTTPS tunnel base URL with `/mcp` appended, e.g.:
   ```
   https://abcd-123-45-67.ngrok-free.app/mcp
   ```
5. Choose **Bearer Token** authentication and paste your secure token value (`your-secure-mcp-token`).
6. Save and verify that the connector successfully connects and registers the approved tools.

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
