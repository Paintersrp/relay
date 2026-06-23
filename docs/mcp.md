# Relay MCP — Model Context Protocol Specification

> [!IMPORTANT]
> **Current GPT-Facing MCP Action Surface:**
> The current Planner Project-facing MCP actions are `create_run_from_planner_handoff` and `submit_planner_pass_plan` by default. The first submits a reviewed Planner handoff to Relay to create/start a run; the second submits a reviewed structured Plan of Passes JSON to create managed plan/pass records.
> 
> The Planner does **not** have status-query, run-listing, audit-submission, or downstream-dispatch MCP actions by default. Tools such as `list_open_runs`, `get_run_status`, `submit_audit_packet`, and `submit_test_audit_packet` exist in the local/dev/server inventory but are **not** current Planner Project actions unless configuration changes.
>
> Relay implements a profile-based tool registration system. Under the default `RELAY_MCP_PROFILE=local-operator` configuration, Relay registers 22 additional retrieval-only context-broker tools (e.g. `get_project`, `get_plan`, `get_pass`, snapshotting, and context packets) to support local-first workflows. These context broker tools stay separate from submission actions, and do not create runs, submit plans, dispatch executors, mutate git, run shell commands, or expose arbitrary filesystem access.
>
> The Relay web UI shows read-only managed pass context, planner handoff provenance, context packet IDs, source snapshot IDs, associated runs, and links to safe persisted source-context artifacts. This UI visibility does **not** invoke MCP broker tools directly, does **not** create context packets/source snapshots, and does **not** render raw source file contents.

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

## Project-Orchestrator Work Tools (Contract-Defined, Not Registered Yet)

*   `get_next_pass_work` and `get_next_audit_work` are defined by the `Paintersrp/relay-contracts` repository at `contracts/planner_mcp_orchestrator_work_contract.md`.
*   They serve as retrieval-only work-packet surfaces for project-scoped sequential orchestration.
*   They require `project_id` and `plan_id`. `get_next_audit_work` also accepts optional `pass_id` and `run_id`.
*   These tools are **not currently registered** in the `Paintersrp/relay` MCP server, and will be implemented in a subsequent vertical slice.
*   They do not submit plans, create runs, generate handoffs, generate audit judgments, apply audit decisions, dispatch executors, run shell commands, mutate git, or read/write arbitrary filesystem paths.

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

### ChatGPT Local Tunnel (Default)

For the local ChatGPT tunnel workflow, the default Relay integration path is stdio, not the HTTP `/mcp` route.

- `cmd/mcpserver` is Relay's real stdio MCP server.
- `scripts/local/relay-mcp-stdio.mjs` is the launcher used by the default ChatGPT tunnel profile.
- `npm run chatgpt-mcp:init` configures `tunnel-client` with `--mcp-command`, which launches the stdio MCP server through that wrapper.
- `cmd/relay` still exposes HTTP `/mcp`, but that route is optional/dev for explicit HTTP tunnel mode or local HTTP testing.
- Project-facing ChatGPT actions remain `create_run_from_planner_handoff` and `submit_planner_pass_plan` unless project configuration explicitly exposes more.

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
        "RELAY_ARTIFACTS_DIR": "/path/to/data/artifacts",
        "RELAY_MCP_PROFILE": "local-operator"
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
| `RELAY_MCP_PROFILE` | `local-operator` | Active tool profile: `local-operator` (enables context broker tools) or `restricted` (hides broker tools). |
| `RELAY_MCP_CONTEXT_BROKER_ENABLED` | (unset) | Legacy fallback if `RELAY_MCP_PROFILE` is unset (`true` maps to `local-operator`, `false` maps to `restricted`). |

The MCP server uses WAL mode and shares the database safely with the Go HTTP daemon.

---

## Registered Tools (Developer Tool Inventory)

The registered tool set is determined by the `RELAY_MCP_PROFILE` environment variable. By default, under the `local-operator` profile, the server registers both the core submission tools and the 22 context broker tools. Under the `restricted` profile, only the 6 core submission/status tools are registered.

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
  "repo_target": "string (optional) — falls back to handoff metadata/frontmatter repo_target",
  "branch_context": "string (optional) — falls back to handoff metadata/frontmatter branch_context or 'main'",
  "name": "string (optional) — run title",
  "source": "string (optional) — default 'mcp_chat'",
  "client_trace_id": "string (optional)",
  "plan_id": "string (optional) — Relay plan identifier to associate with the new run",
  "pass_id": "string (optional) — Relay pass identifier under plan_id; requires plan_id"
}
```

**Output:** `ok`, `tool`, `run_id`, `status`, `lifecycle_state`, `review_url`, `artifact_kinds`, `validation_summary`, `plan_id` (when associated), `pass_id` (when associated), `provenance`

**Uses:** durable planner handoff markdown as the only submission payload. Creates real run/artifacts/checks/events plus a `run_submission_provenance` row and `planner_handoff_provenance.json` artifact.

**Plan/pass association behavior:**
- `pass_id` requires `plan_id`.
- Unknown `plan_id`, unknown `pass_id`, terminal pass status (`completed`/`skipped`), or explicit handoff/plan metadata conflicts reject submission and create no run.
- When `plan_id`/`pass_id` point to an existing open plan/pass, the new run is associated with that pass and the pass status moves to `in_progress` only after run creation and provenance persistence succeed.
- Audit acceptance for an associated run moves the pass to `completed`.
- Audit revision for an associated run keeps/returns the pass to `in_progress`.
- Compatibility note: this documents current pre-PASS-002 runtime behavior. The project-scoped orchestrator contract defines `revision_required` as a distinct pass/work state that blocks advancement and returns the same managed pass/run for repair or follow-up. Later runtime passes own migrating persistence and transition behavior from this current `in_progress` fallback to the expanded state model.
- Provenance records include the handoff SHA-256, byte length, bounded handoff metadata, source/client trace data, optional plan/pass association, and optional `context_packet_id` / `source_snapshot_id`.

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

**Output:** `ok`, `tool`, `run_id`, `title`, `repo`, `branch`, `status`, `lifecycle_state`, `active_step`, `artifact_kinds` (names only), `latest_event_summaries` (latest 10 messages/levels), `review_url`, optional `plan_id`, `pass_id`, optional `plan_row_id`, `plan_pass_row_id`, and bounded `provenance`

No full artifact contents, no log dumps, no secrets, and no full handoff markdown.

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

**Shared audit semantics:**
- The MCP tool now uses the same backend decision-submission service as the HTTP audit endpoints.
- When Markdown is provided, Relay persists a manual `audit_packet` artifact plus `audit_decision_json`.
- `blocked` and `manual_review_required` preserve the original decision in `audit_decision_json` while mapping the run status to `revision_required`.
- Audit evidence remains local-only and artifact-backed. GitHub PRs, CI, and Actions are not read or required.

**Does NOT:** close the run, commit, push, stage, merge, branch, checkout, reset, or mutate the target repository.

### 6. `submit_planner_pass_plan`

**Purpose:** Submit a reviewed Planner pass plan JSON artifact to Relay. This creates `plans` and derived `plan_passes` records only, validates the full Plan v2 schema-backed payload, and stores plan/pass context metadata for later workflows; it does not create runs, attach runs to passes, dispatch executors, mutate git, or read chat context.

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

## Gated Context Broker Tools

These tools are registered when the `RELAY_MCP_PROFILE` is set to `local-operator`.

### `get_project`

Returns bounded project and repository policy metadata for a Relay project. Planner-facing output omits local absolute repository paths.

### `get_plan`

Returns bounded plan metadata plus persisted Plan v2 JSON fields (`plan_meta`, `project_context`, `mcp_capability_profile`, `global_context_rules`) and ordered pass summaries. Optional `include_raw` includes `raw_plan_json` only when it fits within a safe size cap.

### `get_pass`

Returns bounded pass metadata plus persisted `context_plan`, `source_snapshot_requirements`, `handoff_readiness_criteria`, `risk_level`, and `context_budget`.

### `get_pass_context`

Returns retrieval-only pass context plus optional latest source snapshot metadata and latest matching context packet metadata. This tool does not create source snapshots or context packets.

### `create_source_snapshot`

Creates a bounded source snapshot for registered repositories only. No arbitrary repo paths, no git mutation, and no raw diff dumps are exposed.

### `list_project_files`

Returns bounded snapshot-backed file inventory rows with provenance fields including project/repo/snapshot identity, content hash, redaction status, and indexed timestamp.

### `search_project_files`

Runs bounded fixed-string search only and returns provenance-rich matches. No arbitrary shell args, no arbitrary roots, and no regex mode are exposed.

### `read_project_file`

Returns a bounded repository-relative file read from a source snapshot with provenance, redaction status, truncation state, and blockers such as `source_snapshot_file_changed`.

### `get_repository_git_status`

Returns the current git status (`git status --short`) for a registered repository, including tracked/untracked changes.

### `get_repository_recent_commit`

Returns metadata for the most recent git commit in a registered repository.

### `list_repository_changed_files`

Lists files changed between the repository head and the target comparison ref.

### `get_repository_diff`

Returns a bounded git diff patch for modified files within the repository.

### `create_context_packet`

Creates a bounded context packet from seed file reads, seed searches, and optional inventory. The tool returns metadata and artifact paths only, not full packet contents.

### `get_context_packet`

Returns stored context packet metadata and optional source metadata rows. It does not read and return full packet or markdown artifact contents.

### `create_local_audit`

Creates a local-only run audit record from captured validation results and git diff state.

Accepts one of the following modes:
- `recent_commit`: Reviewing the most recent commit in one registered repository.
- `selected_pass_changes`: Reviewing current local changes for a managed plan/pass before or after a pass-associated run.
- `feature_slice`: Reviewing a bounded feature/system slice by repository-relative paths or search terms.
- `full_repository`: Performing a broad local repository health/audit scan without remote evidence.

Outputs remain local-only, bounded, and artifact-backed.

### `get_local_audit`

Retrieves metadata and results for a specific local audit run.

### `list_project_local_audits`

Lists recent local audit runs executed for the project.

### `search_project_context_memory`

Searches project-level context memory records matching keywords.

### `list_project_context_records`

Lists all context memory records stored for a project.

### `get_project_context_record`

Retrieves content and metadata for a specific project context memory record.

### `create_project_context_record`

Stores a new project context memory record.

### `supersede_project_context_record`

Archives or supersedes an existing project context memory record.

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
- **Broker retrieval is bounded and gated.** Context broker tools are enabled by default under the `local-operator` profile, register only under explicit profile selection, reject unknown input fields, and return bounded structured JSON with provenance fields.
- **No shell or arbitrary filesystem access from broker tools.** Context broker tools wrap registered project repositories, snapshot-backed file inventory/search/read, git status/diff, and context packet services only.
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
- **PASS-007 context broker:** Completed operator-facing documentation for local setup, registration, profiles, safety boundaries, and workflows.
- **PASS-008 compatibility cleanup:** Completed validation of standalone/managed compatibility, database auto-migrations with foreign keys, retained legacy redirects/routes, and local-only release verification scripting.

---

## ChatGPT Remote MCP Validation

> **Dev-Only Note:** This section describes local development validation of the remote `/mcp` endpoint and may exercise broader dev/server tool inventory, but does not redefine the current Planner Project-facing MCP action surface. For the Planner Project, the current submission actions are `create_run_from_planner_handoff` and `submit_planner_pass_plan` unless Project configuration deliberately changes.

### ChatGPT Local Tunnel

Relay supports both stdio and HTTP MCP transports for local development, but the preferred ChatGPT local tunnel workflow uses the real stdio MCP server (`cmd/mcpserver`) through `scripts/local/relay-mcp-stdio.mjs` and `tunnel-client --mcp-command`.

Configure the tunnel variables in the repo root `.env` or `.env.local` files, using `.env.example` as the placeholder template.

The HTTP `/mcp` endpoint exposed by `cmd/relay` remains available for explicit HTTP mode or local HTTP validation, but it is not the default single-command ChatGPT workflow after this pass.

The current HTTP `/mcp` endpoint uses No Auth; this is a temporary development proof only. Production use **must** restore authentication before exposing the endpoint beyond local validation. GPT-facing action boundaries remain unchanged regardless of transport.

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
