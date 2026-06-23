# Relay MCP — Model Context Protocol Specification

> [!IMPORTANT]
> **Current GPT-Facing MCP Action Surface:**
> The current Planner Project-facing MCP actions are `create_run_from_planner_handoff` and `submit_planner_pass_plan` by default. The first submits a reviewed Planner handoff to Relay to create/start a run; the second submits a reviewed structured Plan of Passes JSON to create managed plan/pass records.
> 
> The Planner does **not** have status-query, run-listing, audit-submission, or downstream-dispatch MCP actions by default. Tools such as `list_open_runs`, `get_run_status`, `submit_audit_packet`, and `submit_test_audit_packet` exist in the local/dev/server inventory but are **not** current Planner Project actions unless configuration changes.
>
> Relay now stores project/repository registry, source-policy configuration, internal source snapshot/git evidence backend state, internal file inventory/search/read service primitives, and internal PASS-005 context packet/context coverage report generation. These context packet services are not run submission, canonical packet compilation, or executor dispatch, and they do **not** expose Planner-facing context-broker MCP tools yet; that remains future work until PASS-007.

---

## Current GPT-Facing Action vs. Local/Dev Tool Inventory

1.  **Project MCP Actions (Production/GPT-Facing):**
    *   **Action:** `create_run_from_planner_handoff` — Submits a reviewed Planner handoff to Relay. Relay creates/starts the run and handles all downstream compiler, validator, and executor tasks.
    *   **Action:** `submit_planner_pass_plan` — Submits a reviewed structured Plan of Passes JSON artifact to Relay. Relay creates managed plan/pass records only.
    *   **User Gating:** Each submission action requires explicit user confirmation in chat before invocation.
2.  **Local/Dev/Server Tool Inventory (Optional/Developer-Only beyond the two submission actions):**
    *   The `mcpserver` implementation also registers status/list/audit tools used for local debugging, unit/smoke testing, or command-line developer workflows.
    *   `submit_planner_pass_plan` creates plan/pass records only and does not create runs, dispatch executors, mutate git, or read chat context.
    *   The additional status/list/audit tools are **not** currently exposed to the Planner Project unless the specific project configuration is modified to expose them.

---

## How It Works

**Relay does not read chat messages.** The MCP client/LLM decides, from the current chat context, when a Relay tool is appropriate. When it decides to call a tool, it passes the relevant chat-derived content as explicit tool arguments.

```
Chat conversation
      ↓
MCP client sees user request (e.g. "submit this handoff to Relay")
      ↓
MCP client extracts content from chat
      ↓
MCP client calls tool with content as arguments
      ↓
Relay validates arguments, writes artifacts, updates run state
      ↓
Relay returns bounded structured result (no full artifact dumps)
```

**Security note:** Do not include secrets, tokens, authentication headers, private keys, API keys, signed URLs, or any credential material in tool arguments. Relay stores argument content as persistent durable artifacts.

---

## Quick Start

### Build

```bash
make mcp-build
# Produces bin/relay-mcpserver.exe
```

### Test

```bash
make mcp-test     # unit tests for MCP package
make mcp-smoke    # builds binary and runs full smoke harness
```

### Smoke test output example

```
✓ initialize
✓ ping
✓ tools/list count=6
✓ tools/list approved:submit_test_audit_packet
✓ tools/list approved:create_run_from_planner_handoff
✓ tools/list approved:submit_planner_pass_plan
✓ tools/list approved:list_open_runs
✓ tools/list approved:get_run_status
✓ tools/list approved:submit_audit_packet
✓ submit_test_audit_packet ok=true
✓ submit_planner_pass_plan ok=true
✓ create_run_from_planner_handoff ok=true
✓ get_run_status ok=true
✓ list_open_runs contains created run
✓ submit_audit_packet status=revision_required
✓ unknown tool returns error
✓ invalid decision mentions VALIDATION_ERROR
PASS: 46
```

---

## MCP Client Configuration

### Claude Desktop

Add to `claude_desktop_config.json`:

```json
{
  "mcpServers": {
    "relay": {
      "command": "/path/to/bin/relay-mcpserver",
      "args": [],
      "env": {
        "RELAY_DB_PATH": "/path/to/data/relay.sqlite",
        "RELAY_ARTIFACTS_DIR": "/path/to/data/artifacts"
      }
    }
  }
}
```

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `RELAY_DB_PATH` | `data/relay.sqlite` | Path to the Relay SQLite database |
| `RELAY_ARTIFACTS_DIR` | `data/artifacts` | Path to artifact storage directory |

The MCP server uses WAL mode and shares the database safely with the Go HTTP daemon.

---

## Registered Tools (Developer Tool Inventory)

Exactly 6 tools are registered in the local/dev MCP server. Note that **tools #2 (`create_run_from_planner_handoff`) and #6 (`submit_planner_pass_plan`)** are currently exposed as Project MCP actions for the GPT-facing Planner by default. The remaining status/list/audit tools are kept for local debugging, testing, or future expansion, and are only Project-facing if external configuration explicitly exposes them. No shell execution, arbitrary file access, or git mutation tools are exposed.

PASS-005 adds internal source context service primitives and context packet generation for snapshot-backed file inventory, bounded file reads, rg-backed source search, context packet artifacts, and context coverage report artifacts. These primitives are not registered as MCP tools in this pass. Context packet creation is not run submission, canonical packet compilation, or executor dispatch; Planner-facing context broker MCP tools remain future work until PASS-007.

### 1. `submit_test_audit_packet`

**Purpose:** Pass 13A feasibility tool. Validates that the MCP bridge is reachable and can write a sentinel artifact. Preserved for backward compatibility and client verification.

**Input:**
```json
{
  "run_id": "mcp-test",
  "audit_packet_markdown": "# Test",
  "decision": "accepted"
}
```

**Output:** `ok`, `tool`, `run_id`, `decision`, `artifact_path`, `message`

---

### 2. `create_run_from_planner_handoff`

**Purpose:** Submit planner handoff markdown from the current chat conversation to Relay as a new run. Use when the user asks to send, submit, or register a handoff.

**The LLM should call this tool when:**
- The user says "submit this handoff to Relay"
- The user says "register this handoff"
- The user pastes a handoff and says "send it to Relay"

**Input:**
```json
{
  "planner_handoff_markdown": "string (required) — full handoff markdown content",
  "repo_target": "string (optional) — falls back to frontmatter repo/repo_target",
  "branch_context": "string (optional) — falls back to frontmatter branch_context or 'main'",
  "name": "string (optional) — run title",
  "source": "string (optional) — default 'mcp_chat'",
  "client_trace_id": "string (optional)",
  "plan_id": "string (optional) — Relay plan identifier to associate with the new run",
  "pass_id": "string (optional) — Relay pass identifier under plan_id; requires plan_id"
}
```

**Output:** `ok`, `tool`, `run_id`, `status`, `lifecycle_state`, `review_url`, `artifact_kinds`, `validation_summary`, `plan_id` (when associated), `pass_id` (when associated)

**Uses:** same intake semantics as `POST /api/intake/planner-handoff`. Creates real run/artifacts/checks/events through existing Relay store services.

**Plan/pass association behavior:**
- `pass_id` requires `plan_id`.
- When `plan_id`/`pass_id` point to an existing plan/pass, the new run is associated with that pass and the pass status moves to `in_progress`.
- Audit acceptance for an associated run moves the pass to `completed`.
- Audit revision for an associated run keeps/returns the pass to `in_progress`.

---

### 3. `list_open_runs`

**Purpose:** List recent non-terminal Relay runs. Returns bounded summaries only.

**Input:**
```json
{
  "limit": 10
}
```
Max limit: 25. Default: 10.

**Output:** `ok`, `tool`, `runs[]` (each with `run_id`, `title`, `repo`, `branch`, `status`, `lifecycle_state`, `updated_at`, `review_url`), `count`

No artifact content, no logs, no diffs are returned.

---

### 4. `get_run_status`

**Purpose:** Get a bounded status snapshot for a specific run. Use before deciding the next chat-derived handback action.

**Input:**
```json
{
  "run_id": "42"
}
```

**Output:** `ok`, `tool`, `run_id`, `title`, `repo`, `branch`, `status`, `lifecycle_state`, `active_step`, `artifact_kinds` (names only), `latest_event_summaries` (latest 10 messages/levels), `review_url`

No full artifact contents, no log dumps, no secrets.

---

### 5. `submit_audit_packet`

**Purpose:** Submit an audit or review result from the current chat back to an existing Relay run.

**The LLM should call this tool when:**
- The user has reviewed a run and wants to submit the review to Relay
- The user says "submit my audit", "mark this as accepted", "send back the decision"

**Input:**
```json
{
  "run_id": "42",
  "audit_packet_markdown": "string (required) — audit content from chat",
  "decision": "accepted | accepted_with_warnings | revision_required | blocked | manual_review_required",
  "notes": "string (optional)",
  "client_trace_id": "string (optional)"
}
```

**Status transitions:**

| Decision | Resulting Status |
|----------|-----------------|
| `accepted` | `accepted` |
| `accepted_with_warnings` | `accepted_with_warnings` |
| `revision_required` | `revision_required` |
| `blocked` | `revision_required` + event note |
| `manual_review_required` | `revision_required` + event note |

**Output:** `ok`, `tool`, `run_id`, `decision`, `status`, `lifecycle_state`, `artifact_kind`, `review_url`

**Does NOT:** close the run, commit, push, stage, merge, branch, checkout, reset, or mutate the target repository.

### 6. `submit_planner_pass_plan`

**Purpose:** Submit a reviewed Planner pass plan JSON artifact to Relay. This creates `plans` and derived `plan_passes` records only; it does not create runs, attach runs to passes, dispatch executors, mutate git, or read chat context.

**The LLM should call this tool when:**
- The user asks to submit a Planner pass plan JSON to Relay.
- The user has a reviewed `.planner-pass-plan.json` artifact to register.

**Input:**
```json
{
  "planner_pass_plan_json": "string (required) — reviewed Planner pass plan JSON content",
  "source_artifact_path": "string (optional) — repo-relative path to the source .planner-pass-plan.json artifact",
  "source": "string (optional) — caller/source label for audit context"
}
```

**Output:** `ok`, `tool`, `plan_id`, `plan_row_id`, `status`, `pass_count`, `passes[]` (each with `pass_id`, `row_id`, `sequence`, `name`, `status`), `validation`

**Validation failures:** Returned with `ok: false`, an `error`/`message`, and the `validation` report. Duplicate `plan_id` errors are reported in the validation report.

**Does NOT:** create runs, attach runs to passes, dispatch executors, mutate git, read chat context, or perform drift detection.

---

## Make Targets

| Target | Description |
|--------|-------------|
| `make mcp-build` | Build `bin/relay-mcpserver.exe` |
| `make mcp-test` | Run MCP unit tests |
| `make mcp-smoke` | Build binary and run full smoke harness |
| `make mcp-clean` | Remove MCP binary |

---

## Safety Boundaries

- **No shell execution.** No `exec`, `shell`, or `command` tools are exposed.
- **No arbitrary file read/write.** All artifact writes go through `relay/internal/artifacts` conventions which enforce path containment and allowed kind lists.
- **No git mutation.** No commit, push, stage, merge, branch, checkout, reset, or worktree operations.
- **No run closure.** `submit_audit_packet` applies a status transition and writes artifacts but does not close or complete runs.
- **No run/executor/git side effects from plan submission.** `submit_planner_pass_plan` creates plan/pass records only and does not create runs, dispatch executors, mutate git, or read chat context.
- **Bounded outputs.** No tool dumps full artifact contents, log files, or secret values.
- **Credential exclusion.** Tool descriptions warn callers not to pass secrets, tokens, auth headers, private keys, API keys, or signed URLs.

---

## Architecture

```
MCP Client (Claude Desktop / Cursor / OpenCode)
    │
    │ stdio (newline-delimited JSON-RPC 2.0)
    ▼
cmd/mcpserver (subprocess)
    │
    ├── internal/mcp/server.go     (JSON-RPC dispatch)
    ├── internal/mcp/tool_*.go     (tool handlers)
    ├── internal/intake/service.go (shared run creation)
    ├── internal/auditor/submit.go (shared audit submission)
    ├── internal/store/db.go       (SQLite, same DB as HTTP daemon)
    └── internal/artifacts/paths.go (artifact write conventions)
```

The MCP subprocess and the HTTP daemon (`cmd/relay`) share the same SQLite database file via WAL mode. They do not communicate directly; each reads/writes the DB independently.

---

## Pass History

- **Pass 13A (feasibility):** Added `submit_test_audit_packet` to prove stdio MCP bridge works. Gated real tools.
- **Pass 16 (real tools):** Implemented the 4 run/audit tools (`create_run_from_planner_handoff`, `list_open_runs`, `get_run_status`, `submit_audit_packet`), wired MCP server to real Relay DB, added executable `make mcp-smoke` harness.
- **Pass 16+ managed plans:** Added `submit_planner_pass_plan` for Planner-facing plan submission and updated smoke/docs to cover the 6-tool inventory.

---

## ChatGPT Remote MCP Validation

> **Dev-Only Note:** This section describes local development validation of the remote `/mcp` endpoint and may exercise broader dev/server tool inventory, but does not redefine the current Planner Project-facing MCP action surface. For the Planner Project, the current submission actions are `create_run_from_planner_handoff` and `submit_planner_pass_plan` unless Project configuration deliberately changes.

Relay exposes `/mcp` through the Go daemon (`cmd/relay`) for ChatGPT-facing remote MCP access. During local development, ChatGPT connects through an HTTPS tunnel (e.g. `ssh -R` or `ngrok`) that forwards to the local daemon.

The current `/mcp` endpoint uses No Auth; this is a temporary development proof only. Production use **must** restore authentication before exposing the endpoint beyond local validation.

Local stdio MCP (the `relay-mcpserver` binary), if retained, is optional/dev-only and is not the primary ChatGPT integration path. Remote HTTPS MCP is the intended ChatGPT integration channel.

---

## Remote MCP Smoke-Test Checklist

Run these checks against a local development instance of the Go daemon with an HTTPS tunnel active. This checklist is **not** production deployment guidance.

1. **Daemon starts without error** — `go run ./cmd/relay` binds on the configured port.
2. **Tunnel is reachable** — `curl -s -o /dev/null -w "%{http_code}" <tunnel-url>/mcp` returns `200` or `405`.
3. **ChatGPT can discover tools** — ChatGPT session successfully calls `tools/list` on the remote `/mcp` endpoint.
4. **Tools respond without auth errors** — Each tool returns a structured response, not an auth/403 body, in a local/dev validation configuration. Note that tools such as `submit_test_audit_packet`, `list_open_runs`, `get_run_status`, and `submit_audit_packet` are used here but are **not** the current Planner Project action inventory unless configuration explicitly exposes them. `create_run_from_planner_handoff` and `submit_planner_pass_plan` are the current Planner-facing submission actions.
5. **Artifact written to disk** — After calling a write tool, confirm an artifact file appears under `$RELAY_ARTIFACTS_DIR`.
6. **Run state persisted** — After a write tool, `get_run_status` returns the expected updated state.
7. **No credentials leaked** — Review tunnel and daemon logs; confirm no tokens, keys, or signed URLs appear in the output.
8. **Daemon stops cleanly** — `Ctrl+C` or `SIGTERM` shuts the process down without a panic.
