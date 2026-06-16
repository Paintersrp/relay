# Relay MCP Bridge

Relay exposes a **Model Context Protocol (MCP)** bridge that allows MCP-compatible clients (Claude Desktop, Cursor, OpenCode, etc.) to submit audit packets and manage runs through Relay's existing backend services.

## Pass Gate

**Pass 13A (feasibility) must succeed before Pass 13B tools are available.**

The server currently registers only `submit_test_audit_packet`. Real intake, run-list, run-status, and audit-handback tools are gated behind explicit target-client feasibility confirmation.

---

## Setup

### Build the MCP server binary

```bash
go build -o bin/relay-mcpserver ./cmd/mcpserver
```

### Configure your MCP client

Add the binary to your MCP client's server list. Example for **Claude Desktop** (`~/Library/Application Support/Claude/claude_desktop_config.json`):

```json
{
  "mcpServers": {
    "relay": {
      "command": "/absolute/path/to/bin/relay-mcpserver",
      "args": [],
      "env": {
        "RELAY_ARTIFACTS_DIR": "/absolute/path/to/relay/data/artifacts"
      }
    }
  }
}
```

No secrets, tokens, or credentials should be placed in this config. The MCP server reads only from the local filesystem.

**Environment variables:**

| Variable              | Default            | Purpose                                 |
|-----------------------|--------------------|-----------------------------------------|
| `RELAY_ARTIFACTS_DIR` | `data/artifacts`   | Base directory for artifact storage     |

### Transport

The server uses **stdio transport** (newline-delimited JSON-RPC 2.0). The MCP client launches the binary as a subprocess and communicates over stdin/stdout.

---

## Pass 13A — Feasibility Smoke Test

### Tool: `submit_test_audit_packet`

Validates that the Relay MCP bridge is reachable and can write a durable artifact through Relay's artifact conventions.

**Input schema:**

| Field                   | Type   | Required | Description                                                  |
|-------------------------|--------|----------|--------------------------------------------------------------|
| `run_id`                | string | ✅       | Identifier for this test. Use `"mcp-test"` for smoke test.   |
| `audit_packet_markdown` | string | ✅       | Markdown content to persist as a test artifact.              |
| `decision`              | string | ✅       | One of the supported audit decisions (see below).            |

**Supported decisions:**

- `accepted`
- `accepted_with_warnings`
- `revision_required`
- `blocked`
- `manual_review_required`

**Smoke payload:**

```json
{
  "run_id": "mcp-test",
  "audit_packet_markdown": "# Feasibility Test\n\nThis is the Pass 13A MCP smoke test.",
  "decision": "accepted"
}
```

**Expected success response:**

```json
{
  "ok": true,
  "tool": "submit_test_audit_packet",
  "run_id": "mcp-test",
  "decision": "accepted",
  "artifact_path": "data/artifacts/0/mcp_test_audit_packet.md",
  "message": "Pass 13A feasibility test artifact written successfully. ..."
}
```

**Artifact location:**

```
data/artifacts/0/mcp_test_audit_packet.md
```

Run ID `0` is a reserved sentinel directory used only for MCP test artifacts. It does not correspond to any database run.

### Manual validation checklist (Pass 13A)

1. Build the binary: `go build -o bin/relay-mcpserver ./cmd/mcpserver`
2. Configure your MCP client with the binary path.
3. Restart the MCP client.
4. Confirm the client lists `submit_test_audit_packet` in its available tools.
5. Call the tool with the smoke payload above.
6. Confirm the client receives a structured success response with `ok: true`.
7. Confirm the artifact exists on disk at `data/artifacts/0/mcp_test_audit_packet.md`.
8. Report success (or failure) so Pass 13B can proceed.

---

## Pass 13B — Real Tools (GATED)

> [!IMPORTANT]
> Pass 13B tools are **not registered** until Pass 13A target-client feasibility succeeds and the user explicitly confirms.

After the gate succeeds, the following tools will be added:

| Tool                              | Description                                               |
|-----------------------------------|-----------------------------------------------------------|
| `create_run_from_planner_handoff` | Create an intake-stage run from a planner handoff markdown |
| `submit_planner_handoff`          | Submit or attach a planner handoff to an existing run     |
| `list_open_runs`                  | List open (non-terminal) runs with bounded output         |
| `get_run_status`                  | Get a concise status snapshot for a specific run          |
| `submit_audit_packet`             | Submit a manual audit packet through the auditor service  |

These tools reuse existing Relay backend services and do not create a parallel pipeline.

---

## Safety Boundaries

> [!CAUTION]
> The MCP bridge enforces the following hard boundaries. These are not configurable.

- **No shell execution** — no `exec`, `os/exec`, or shell command is exposed.
- **No arbitrary file read/write** — all artifact writes go through `relay/internal/artifacts.Write` which enforces an allow-list of artifact kinds and path containment within the run artifact directory.
- **No git mutation** — no commit, push, branch creation, checkout, reset, or worktree mutation is exposed.
- **No audit auto-approval** — audit packet submission preserves existing evidence; commit/push is never triggered automatically.
- **No secrets in artifacts** — tool responses and artifacts must not contain tokens, credentials, private URLs, auth headers, or private keys.

---

## Known Limitations

- Pass 13B tools are not available until Pass 13A feasibility is confirmed by the user.
- The stdio transport requires the MCP client to support subprocess-based MCP servers.
- The test artifact (run ID 0) is not visible in the Relay web UI because it has no database run record.
- The MCP server does not share the same SQLite connection as the Relay HTTP server; for Pass 13B tools that need database access, the MCP server must open its own connection to the same `data/relay.sqlite` file.

---

## Local Smoke Test (Without MCP Client)

You can exercise the handler directly using Go tests:

```bash
go test ./internal/mcp/... -v
```

Or send a raw JSON-RPC 2.0 request to the binary on stdin:

```bash
echo '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{}}}' | ./bin/relay-mcpserver
echo '{"jsonrpc":"2.0","id":2,"method":"tools/list"}' | ./bin/relay-mcpserver
echo '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"submit_test_audit_packet","arguments":{"run_id":"mcp-test","audit_packet_markdown":"# Test\n\nFeasibility check.","decision":"accepted"}}}' | ./bin/relay-mcpserver
```

Each command outputs one JSON response line.

---

## No Credentials Warning

> [!WARNING]
> Do not store API keys, tokens, OAuth secrets, database passwords, private URLs, auth headers, signed URLs, or private keys in:
> - MCP client config files
> - Relay artifact files
> - Relay docs or handoffs
> - Environment variables committed to the repo
