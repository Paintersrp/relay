# Relay MCP — Model Context Protocol Specification

> [!IMPORTANT]
> **Current GPT-Facing MCP Action Surface:**
> The current Planner Project-facing MCP actions are `create_run_from_planner_handoff_file`, `create_run_from_planner_handoff`, and `submit_planner_pass_plan` by default. The file-based handoff tool is preferred when a reviewed Planner handoff exists as a durable Markdown file and exact byte identity matters; the inline handoff tool remains a fallback for chat-only drafts. `submit_planner_pass_plan` submits a reviewed structured Plan of Passes JSON to create managed plan/pass records.
> 
> The Planner does **not** have status-query, run-listing, audit-submission, or downstream-dispatch MCP actions by default. Tools such as `list_open_runs`, `get_run_status`, `submit_audit_packet`, and `submit_test_audit_packet` exist in the local/dev/server inventory but are **not** current Planner Project actions unless configuration changes.
>
> Relay implements a profile-based tool registration system. Under the default `RELAY_MCP_PROFILE=local-operator` configuration, Relay registers additional retrieval-only context-broker tools (e.g. `get_project`, `get_plan`, `get_pass`, `get_next_pass_work`, `get_next_audit_work`, snapshotting, and context packets), project-scoped refactor backlog tools, and Plan Seed tools to support local-first workflows. These tools stay separate from submission actions, and do not create runs, submit plans, dispatch executors, mutate git, run shell commands, or expose arbitrary filesystem access. The exact tool inventory depends on the active profile and project configuration.
>
> The Relay web UI shows read-only managed pass context, planner handoff provenance, context packet IDs, source snapshot IDs, associated runs, and links to safe persisted source-context artifacts. This UI visibility does **not** invoke MCP broker tools directly, does **not** create context packets/source snapshots, and does **not** render raw source file contents.

---

## Current GPT-Facing Action vs. Local/Dev Tool Inventory

1.  **Project MCP Actions (Production/GPT-Facing):**
    *   **Action:** `create_run_from_planner_handoff_file` - Preferred for reviewed Planner handoff Markdown files. Relay reads the single MCP-supplied `planner_handoff_file`, verifies optional `expected_sha256`, creates/starts the run, and records exact submitted-byte provenance.
    *   **Action:** `create_run_from_planner_handoff` - Fallback for chat-only reviewed handoff Markdown content. Relay creates/starts the run and handles all downstream compiler, validator, and executor tasks.
    *   **Action:** `submit_planner_pass_plan` — Submits a reviewed structured Plan of Passes JSON artifact to Relay. Relay creates managed plan/pass records only.
    *   **User Gating:** Each submission action requires explicit user confirmation in chat before invocation.
2.  **Local/Dev/Server Tool Inventory (Optional/Developer-Only beyond the two submission actions):**
    *   The `mcpserver` implementation also registers status/list/audit tools used for local debugging, unit/smoke testing, or command-line developer workflows.
    *   `submit_planner_pass_plan` creates plan/pass records only and does not create runs, dispatch executors, mutate git, or read chat context.
    *   The additional status/list/audit tools are **not** currently exposed to the Planner Project unless the specific project configuration is modified to expose them.

## Planned Context Workflow Grounded in Current MCP Inventory

This section describes planned/contracted target behavior for the context-gathering workflow. It is not the currently guaranteed GPT-facing action inventory. The current grounding signals remain: the GPT-facing submission actions `create_run_from_planner_handoff_file`, `create_run_from_planner_handoff`, and `submit_planner_pass_plan`; and the local-operator context broker inventory registered under `RELAY_MCP_PROFILE=local-operator`.

The planned contract-target surfaces are resource resolution, source snapshot freshness, required context bundles, prepared handoff context readiness, bounded artifact readback, compile-aware preflight, and exact artifact submission. These surfaces compose bounded primitives only. No high-level tool authorizes shell execution, git mutation, arbitrary filesystem reads, generic file browsing, unbounded source reads, unbounded artifact dumps, or secret persistence.

Intended safe Planner workflow:

1. Resolve project and repository aliases to registered Relay identities.
2. Acquire or reuse a fresh clean source snapshot and report stale or dirty-worktree blockers.
3. Receive required context bundle metadata from work-packet retrieval.
4. Prepare bounded handoff context and context packet readiness without embedding raw source file contents.
5. Read generated artifacts only through registered artifact kinds and bounded view modes.
6. Validate Planner handoff compile readiness before run creation without treating preflight success as submission approval.
7. Submit only the exact reviewed handoff artifact after explicit user confirmation. File submission preserves reviewed handoff bytes, computes submitted SHA-256, verifies optional `expected_sha256`, and blocks with `expected_hash_mismatch` before run creation when hashes differ.

Shared blocker envelope fields in order: `code`, `message`, `recoverable`, `evidence`, `next_actions`.
Blocked MCP tool results set `ok=false`, `status="blocked"`, `isError=true`, and expose the bounded envelope in `structuredContent`. Evidence is limited to safe identifiers or repo-relative slash paths; absolute local paths, traversal paths, control characters, raw diagnostics, and full content dumps are rejected or omitted. Evidence and next-action arrays are bounded to eight items.

Example:

```json
{
  "code": "source_snapshot_stale",
  "message": "The selected source snapshot is stale for the requested pass context.",
  "recoverable": true,
  "evidence": [
    {
      "kind": "path",
      "ref": "apps/web/src/routes/index.tsx"
    },
    {
      "kind": "source_snapshot_id",
      "ref": "snapshot-2026-07-01-001"
    }
  ],
  "next_actions": [
    "Acquire a fresh bounded source snapshot for the registered repository."
  ]
}
```

Required shared taxonomy codes: `unknown_resource`, `unknown_repository`, `alias_ambiguous`, `source_snapshot_stale`, `dirty_worktree`, `required_context_missing`, `required_context_truncated`, `blocked_path`, `redaction_failed`, `schema_mismatch`, `expected_hash_mismatch`, `tool_unavailable`, `tool_schema_stale`, `unsafe_request`.

Successful run submission responses include normalized exact-artifact provenance without exposing MCP mount paths: `submitted_handoff_sha256`, optional `expected_sha256`, `sha_match_status` (`not_supplied`, `matched`, or `mismatched`), `source_mode` (`inline` or `file_parameter`), and `artifact_identity` containing `artifact_kind="planner_handoff"`, a sanitized `display_name`, and `byte_count`.

## Project-Orchestrator Work Tools (Context-Broker Profile)

*   `get_next_pass_work`, `prepare_handoff_context`, and `get_next_audit_work` are defined by the `Paintersrp/relay-contracts` repository at `contracts/planner_mcp_orchestrator_work_contract.md`.
*   They serve as retrieval-only work-packet surfaces for project-scoped sequential orchestration.
*   These tools are registered when the MCP context-broker-enabled profile is active (`RELAY_MCP_PROFILE=local-operator`) and hidden under `RELAY_MCP_PROFILE=restricted`.
*   `get_next_pass_work` requires `project_id` and `plan_id` and returns the next eligible pass work packet or structured blockers.
*   When a pass is selected, `get_next_pass_work` includes a metadata-only `required_context_bundle` in structured content. The bundle summarizes `manifest_repo_id`, `manifest_path`, `manifest_hash` when snapshot metadata has it, `task_domain`, required/optional files, required/optional searches, `context_budget`, `readiness_criteria`, `context_coverage_expectations`, `blocked_if_missing`, and safe bundle blockers/next actions.
*   `required_context_bundle` reduces Planner rediscovery of manifest-derived source requirements. It does not replace source-controlled fetch verification before a final Act-mode Planner handoff, and it never includes raw source contents, context packet contents, artifact dumps, logs, secrets, local absolute paths, shell output, or arbitrary filesystem reads.
*   Missing manifest or required file metadata in the source snapshot is reported inside `required_context_bundle.blockers` as recoverable evidence guidance. `get_next_pass_work` remains bounded and does not submit plans, create runs, generate handoffs, dispatch executors, mutate git, run shell commands, or expose arbitrary filesystem access.
*   `prepare_handoff_context` requires explicit `project_id`, `plan_id`, and `pass_id`; it never selects a pass when `pass_id` is omitted. It prepares metadata-only source/context evidence for that selected pass and may create or reuse bounded source snapshot and context packet artifacts through the same acquisition path as `get_next_pass_work`.
*   `prepare_handoff_context` currently uses the selected pass's persisted context plan, context budget, source snapshot requirements, and backend acquisition strategy. Per-call refresh and budget override fields are intentionally not part of this surface until they are implemented deterministically.
*   `prepare_handoff_context` returns `readiness_state`, `source_snapshot_id`, `context_packet_id`, `repo_heads`, `required_coverage`, `optional_coverage`, `freshness_report`, `required_context_bundle` or a `bundle_unavailable` blocker, typed `blockers`, `recommended_next_action`, and `lower_level_recovery_actions`. It does not return raw source content, raw context packet content, raw artifacts, logs, secrets, local absolute paths, or generated handoff markdown.
*   `prepare_handoff_context.freshness_report` is derived from the same source snapshot freshness semantics as the source snapshot tools. Stale, drifted, unavailable, dirty-disallowed, missing-HEAD, or otherwise non-reusable freshness blocks handoff readiness with typed source blockers and safe recovery guidance.
*   Readiness states include `ready_for_handoff_authoring`, `ready_for_handoff_authoring_with_warnings`, `needs_source_snapshot`, `needs_required_context`, `context_acquisition_failed`, and other structured blocked states inherited from pass eligibility checks. Required missing, blocked, redaction-blocked, or truncated context returns `ok:false`; dirty-disallowed or stale/non-reusable source evidence returns `ok:false`; optional-only limitations may return warnings while keeping `ok:true`.
*   `prepare_handoff_context` does not replace lower-level recovery tools. Its `lower_level_recovery_actions` may reference safe existing tools such as `create_source_snapshot`, `create_context_packet`, `get_context_packet`, or `get_next_pass_work` so operators can correct bounded source/context evidence and call `prepare_handoff_context` again.
*   `get_next_audit_work` requires `project_id` and `plan_id` and accepts optional `pass_id` and `run_id` for scoped audit work selection.
*   Outputs are bounded work-packet JSON with either `ok:true` or `ok:false` and structured `blockers`.
*   These tools do not submit plans, create runs, generate handoffs, generate audit judgments, apply audit decisions, dispatch executors, run shell commands, mutate git, or read/write arbitrary filesystem paths.
*   They remain separate from submission tools (`submit_planner_pass_plan`, `create_run_from_planner_handoff`, `submit_audit_packet`) and are retrieval-only.

For the end-to-end operator loop that combines these retrieval tools with the Relay web UI, reviewed Planner handoffs, audit handbacks, and human gates, see [`docs/project-orchestrator-workflow.md`](project-orchestrator-workflow.md).

## Refactor Backlog Tools (Local Operator Profile)

*   The source-controlled refactor backlog semantics (refactor discovery tasks, pass-ready refactor candidates, scheduled refactor pass representation, generated refactor-only plan review, sidecar deferral, and audit-derived candidate completion) are defined in `Paintersrp/relay-contracts` at `contracts/refactor_backlog_contract.md`.
*   PASS-005 exposes the refactor backlog through MCP tools registered **only** when the context-broker-enabled profile is active (`RELAY_MCP_PROFILE=local-operator`). They are hidden under `RELAY_MCP_PROFILE=restricted`, where calling them returns an unknown-tool error.
*   Every tool is project-scoped and requires `project_id`. Callers must not infer `project_id` from repo, branch, chat, or working directory. All input schemas are strict (`additionalProperties: false`). All business logic, validation, and lifecycle rules are delegated to the `internal/refactors` service; the MCP layer is a thin wrapper.
*   The surface is organized into three classes:
    *   **Retrieval (bounded, no mutation):** `list_refactor_discovery_tasks`, `get_refactor_discovery_task`, `list_refactor_candidates`, `get_refactor_candidate`, `search_refactor_candidates`, `suggest_refactor_candidate_placement`. List/search outputs are bounded (default limit 50, max 100) and include `count` and `truncated`.
    *   **Backlog mutation (explicit confirmation):** `create_refactor_discovery_task`, `update_refactor_discovery_task`, `complete_refactor_discovery_task`, `close_refactor_discovery_task`, `supersede_refactor_discovery_task`, `create_refactor_candidate`, `update_refactor_candidate`, `defer_refactor_candidate`, `reject_refactor_candidate`, `supersede_refactor_candidate`. Each requires `confirmed_user_intent: true` and is rejected before any service write when confirmation is missing or false.
    *   **Plan mutation / artifact generation (explicit confirmation string):** `promote_refactor_candidate_to_plan` requires `confirmation: "promote_refactor_candidate_to_plan"` and inserts a normal managed `pass_type: "refactor"` pass (with `refactor_candidate` metadata) into an existing project-owned plan; it does not create a run. `generate_refactor_only_plan` requires `confirmation: "generate_reviewable_refactor_only_plan"` and returns reviewable artifact paths and a generated plan ID only.
*   Generated refactor-only plans are ordinary reviewable Plan of Passes JSON/Markdown artifacts. The tool returns artifact metadata only (paths, generated plan ID, candidate IDs, warnings, and `submission_policy: review_required_no_auto_submit`) and never the full plan JSON or Markdown. Selected candidates remain `ready`.
*   These tools preserve the same safety boundaries as the rest of the MCP surface: no shell execution, no arbitrary filesystem reads, no git mutation, no model calls, no automatic plan submission, and no run creation.
*   `submit_planner_pass_plan` remains the only plan-submission action and still requires explicit user review and confirmation. `create_run_from_planner_handoff` remains the only reviewed handoff-to-run action and still requires explicit user confirmation. There is no separate refactor submission action, and refactor metadata does not authorize automatic submission.

For the full refactor backlog concept overview, candidate lifecycle, promotion and generated refactor-only plan workflows, and audit-derived candidate completion behavior, see [`docs/refactor-backlog.md`](refactor-backlog.md). The refactor backlog hardening (backend service, orchestrator/audit mapping, and these MCP tools) is covered by the deterministic, local-only release smoke suite, runnable through `npm run release:smoke` (wrapping `scripts/release-smoke.sh`).

## Plan Seed Tools (Local/Dev Inventory)

The Plan Seed tools are registered under the `local-operator` profile and are part of the local/dev/server tool inventory. They are not default Planner Project-facing submission actions.

* `create_plan_seed` — Create a project-scoped Plan Seed in `captured` status.
* `list_plan_seeds` — List Plan Seeds for a project with optional `status` and `limit` filters.
* `get_plan_seed` — Read a single Plan Seed by ID.
* `get_plan_seed_planning_context` — Retrieve read-only planning context for a seed. This is retrieval-only: it does not create attempts, plans, runs, artifacts, or model calls.
* `create_plan_attempt_from_seed` — Register exactly one draft Plan Attempt and Intent Packet from a captured seed plus externally reviewed Plan of Passes JSON. It marks the seed `planned` and links `plan_attempt_id`. It does not submit managed plans or create runs.
* `update_plan_seed` — Update mutable capture fields of an editable `captured` or `deferred` seed.
* `defer_plan_seed` — Mark a `captured` seed as `deferred`.
* `reject_plan_seed` — Mark a `captured` or `deferred` seed as `rejected`.

There is no `link_plan_seed_result` MCP action. Managed plan linkage is internal.

For full Plan Seed semantics, capture boundaries, and lifecycle rules, see [`docs/project-planning-backlog-plan-seeds.md`](project-planning-backlog-plan-seeds.md).

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
- Project-facing ChatGPT actions remain `create_run_from_planner_handoff_file`, `create_run_from_planner_handoff`, and `submit_planner_pass_plan` unless project configuration explicitly exposes more.

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

The registered tool set is determined by the `RELAY_MCP_PROFILE` environment variable. By default, under the `local-operator` profile, the server registers the core submission tools, context broker tools, refactor backlog tools, and Plan Seed tools. Under the `restricted` profile, only the core submission/status tools are registered. The exact counts depend on the active profile and project configuration.

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

### 2. `create_run_from_planner_handoff_file`

**Purpose:** Submit one reviewed Planner handoff Markdown file to Relay as a new run, preserving exact file bytes. This is the preferred submission path when the reviewed handoff exists as a file.

**The LLM should call this tool when:**
- The user says "submit this reviewed handoff file to Relay"
- The user attaches or selects a reviewed `.md` handoff and asks to register it
- The operator needs the submitted handoff SHA to match the reviewed artifact SHA

**Input:**
```json
{
  "planner_handoff_file": "string (required) - MCP file-parameter path to the reviewed .md handoff",
  "expected_sha256": "string (optional) - lowercase hex SHA-256 of the exact file bytes",
  "repo_target": "string (optional) - falls back to handoff metadata/frontmatter repo_target",
  "branch_context": "string (optional) - falls back to handoff metadata/frontmatter branch_context or 'main'",
  "name": "string (optional) - run title",
  "source": "string (optional) - default 'mcp_file_parameter'",
  "client_trace_id": "string (optional)",
  "plan_id": "string (optional) - Relay plan identifier to associate with the new run",
  "pass_id": "string (optional) - Relay pass identifier under plan_id; requires plan_id",
  "source_snapshot_id": "string (optional)",
  "context_packet_id": "string (optional)"
}
```

**Output:** `ok`, `tool`, `run_id`, `status`, `lifecycle_state`, `review_url`, `artifact_kinds`, `validation_summary`, `plan_id` (when associated), `pass_id` (when associated), `provenance`, `submitted_handoff_sha256`, `expected_sha256` (when supplied), `sha_match`, `source_mode`

**Validation:** Relay reads only the supplied `planner_handoff_file`, requires a `.md` file, rejects directories, empty files, and files larger than 1 MiB, computes SHA-256 over the exact bytes before converting to text, and rejects `expected_sha256` mismatches before creating any run or artifact.

**Safety boundary:** This is a controlled run-submission ingestion path, not a context broker file-read tool. It does not browse paths, read repositories generically, execute shell commands, mutate git, or persist absolute local file paths as artifact identity.

**Preflight gate:** Before creating any run or artifact, both inline and file-based run submission handlers perform a deterministic compile-aware preflight (`validate_planner_handoff_for_compile`). Blocking preflight failures return a tool error and prevent durable run/provenance writes. The standalone preflight tool is also available for validation-only use without creating runs (see section 2a below).

---

### 2a. `validate_planner_handoff_for_compile`

**Purpose:** Validate a Planner handoff for compile readiness without creating a run, writing artifacts, or performing any durable workflow transition. This is a bounded deterministic preflight gate that checks handoff structure, compiler_input YAML, provenance, and managed plan/pass association consistency.

**The LLM should call this tool when:**
- The user asks to validate a handoff before committing to submission
- The user wants to verify compile readiness deterministically
- The reviewer needs structured issue diagnostics without altering run state

**Input:**
```json
{
  "additionalProperties": false,
  "planner_handoff_markdown": "string (one of markdown or file required) — full handoff markdown content",
  "planner_handoff_file": "string (one of markdown or file required) — MCP file-parameter path to the reviewed .md handoff",
  "expected_sha256": "string (optional) — lowercase hex SHA-256 of the exact file bytes (file mode only)",
  "repo_target": "string (optional) — falls back to handoff metadata/frontmatter repo_target",
  "branch_context": "string (optional) — falls back to handoff metadata/frontmatter branch_context or 'main'",
  "plan_id": "string (optional) — Relay plan identifier for managed pass association checks",
  "pass_id": "string (optional) — Relay pass identifier for managed pass association checks; requires plan_id",
  "context_packet_id": "string (optional)",
  "source_snapshot_id": "string (optional)"
}
```

**Output (structuredContent):** `ok`, `status`, `is_compile_ready`, `issue_counts` (error/warning totals), `issues[]` (each with `code`, `severity`, `location`, `message`, `repair_guidance`, `blocks_submission`), `submitted_handoff_sha256`, `byte_count`, `source_mode`, `plan_id` (when supplied), `pass_id` (when supplied), `context_packet_id` (when supplied), `source_snapshot_id` (when supplied), `generated_at`

**Example result (blocking):**
```json
{
  "ok": false,
  "status": "blocked",
  "is_compile_ready": false,
  "issue_counts": { "error": 2, "warning": 0 },
  "issues": [
    {
      "code": "compiler_input_missing",
      "severity": "error",
      "location": { "section": "compiler_input" },
      "message": "Required section is missing: compiler_input.",
      "repair_guidance": "Add a <compiler_input> section with a fenced YAML block.",
      "blocks_submission": true
    },
    {
      "code": "frontmatter_missing",
      "severity": "error",
      "location": { "section": "frontmatter" },
      "message": "Handoff is missing a valid frontmatter block.",
      "repair_guidance": "Add a YAML-style frontmatter block delimited by ---.",
      "blocks_submission": true
    }
  ],
  "submitted_handoff_sha256": "abc123...",
  "byte_count": 2048,
  "generated_at": "2026-07-01T00:00:00Z"
}
```

**Preflight error codes:**

| Code | Severity | Blocks |
|------|----------|--------|
| `handoff_empty` | error | yes |
| `frontmatter_missing` | error | yes |
| `repository_target_missing` | error | yes |
| `branch_context_missing` | error | yes |
| `semantic_section_missing` | warning | no |
| `compiler_input_missing` | error | yes |
| `compiler_input_yaml_invalid` | error | yes |
| `compiler_input_required_field_missing` | error | yes |
| `compiler_input_list_empty` | error | yes |
| `managed_plan_missing` | error | yes |
| `managed_pass_mismatch` | error | yes |

**Strict input:** Unknown top-level fields are rejected. Provide exactly one source field: `planner_handoff_markdown` or `planner_handoff_file`. `expected_sha256` is valid only with `planner_handoff_file`, and `pass_id` requires `plan_id`.

**Safety boundary:** This tool does not create runs, submit plans, dispatch executors, compile packets, mutate git, or browse arbitrary paths. It is a read-only validation gate. The text content block contains a short summary only; full issue payloads are in `structuredContent`.

**Relationship to run submission:** Run submission tools (`create_run_from_planner_handoff`, `create_run_from_planner_handoff_file`) perform the same preflight checks internally before creating any run. Blocking preflight failures prevent run/provenance/artifact writes. Preflight success is not a submission trigger — run submission still requires a reviewed Planner handoff and explicit user confirmation.

---

### 3. `create_run_from_planner_handoff`

**Purpose:** Submit planner handoff markdown from the current chat conversation to Relay as a new run. Use this fallback when the user has a reviewed chat-only handoff but no reviewed handoff file to pass through `create_run_from_planner_handoff_file`.

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

### 4. `list_open_runs`

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

### 5. `get_run_status`

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

### 6. `submit_audit_packet`

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

### 7. `submit_planner_pass_plan`

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

Snapshot creation returns an additive `freshness_report` object. Snapshot-bound inventory, search, and read tools also return `freshness_report`; `get_pass_context` includes it on latest source snapshot metadata when available, and `get_context_packet` may include it on returned source metadata rows.

`freshness_report` fields:

- `status`: one of `fresh`, `dirty_worktree`, `partial`, `blocked`, `stale_by_age`, or `drifted`.
- `reusable_for_handoff`: true only when the snapshot is fresh, clean, complete, and within current soft age guidance.
- `source_snapshot_id`, `generated_at`, `snapshot_created_at`, `snapshot_completed_at`, `age_seconds`, and `max_age_seconds`: bounded provenance and age metadata.
- `repository_reports`: per-repository freshness status using captured/current branch and HEAD SHA, dirty booleans/counts, and git availability. Raw git status porcelain text is not returned.
- `warnings`, `blockers`, and `next_actions`: typed recovery guidance using source blocker envelopes.

Freshness uses a soft default maximum age of 900 seconds (`source_snapshot_stale`) unless drift or dirty state makes the snapshot non-reusable sooner. A dirty captured or current worktree reports `source_snapshot_dirty_worktree`. Current HEAD/status-hash changes compared with captured snapshot metadata report `source_snapshot_drifted`. Missing or unavailable snapshot/repository metadata reports `source_snapshot_unavailable`. A changed requested file remains hard-blocked as `source_snapshot_file_changed`; unrelated repository drift can still return bounded unchanged file content while marking `reusable_for_handoff=false`.

Recovery guidance is intentionally narrow: clean up or correct repository configuration as needed, then call `create_source_snapshot` again. Freshness reporting does not authorize shell access, arbitrary file reads, raw git output, git mutation, filesystem locks/leases, worktree cleanup, stash, checkout, reset, or commit.

### `list_project_files`

Returns bounded snapshot-backed file inventory rows with provenance fields including project/repo/snapshot identity, content hash, redaction status, and indexed timestamp.

### `search_project_files`

Runs bounded fixed-string search only and returns provenance-rich matches. No arbitrary shell args, no arbitrary roots, and no regex mode are exposed.

### `read_project_file`

Returns a bounded repository-relative file read from a source snapshot with provenance, redaction status, truncation state, and blockers such as `source_snapshot_file_changed`.

### `resolve_project_repository`

Retrieval-only, project-scoped repository alias resolver. It maps a canonical registered repository ID or accepted alias to the canonical registered repository ID. Accepted aliases are derived only from registered repository IDs: the canonical ID itself and, for owner-qualified IDs, the suffix after the final `/`.

The response includes `project_id`, the input alias, `canonical_repo_id` on success, `accepted_aliases`, ambiguity `candidates`, and shared source blocker envelopes. Unknown aliases return `unknown_repository` with recoverable evidence and next actions. Ambiguous aliases return `alias_ambiguous` with candidate repository IDs and do not select a repository.

This tool does not read arbitrary filesystem paths, inspect CWD, parse Git remotes, mutate repository registrations, run shell commands, mutate git, create context packets, create runs, or submit plans.

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

### `get_run_artifact`

**Availability:** Local-operator (context-broker) profile only. Hidden under `restricted`.

**Purpose:** Bounded, safe readback of registered run artifacts by `run_id` and `artifact_kind`. Allows Planner and Auditor workflows to inspect validation reports, compiler outputs, canonical packets, executor summaries, and related diagnostics without arbitrary filesystem access or unbounded dumps.

**Safety boundaries:**
- Only reads artifacts registered in the Relay artifacts table with a matching `(run_id, artifact_kind)`.
- Does not accept arbitrary file paths, filenames, URLs, shell commands, globs, or filesystem handles.
- All content is bounded and sensitivity-filtered before return.
- Never returns local absolute paths or raw filesystem references.
- Large logs are never dumped by default.
- Not a replacement for downstream review gates, GitHub/CI readback, or shell execution.

**Inputs** (`additionalProperties: false`):

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `run_id` | string | yes | Numeric Relay run identifier (positive base-10 int64). |
| `artifact_kind` | string | yes | Registered artifact kind. Must be an eligible readback kind. |
| `view_mode` | string | yes | One of `metadata_only`, `summary`, `errors`, `bounded_excerpt`. |
| `max_bytes` | integer | no | Maximum bytes for content modes. Default 12000, hard cap 65536 (clamped). |
| `include_content_hash` | boolean | no | Include SHA-256 of full readback content. Default true for content modes. |

**View modes:**

| Mode | Behavior |
|------|----------|
| `metadata_only` | Returns artifact identity (kind, MIME, size, created_at, hash status) without content. |
| `bounded_excerpt` | Returns at most `max_bytes` of redacted content with truncation metadata. Rejects binary artifacts. |
| `summary` | For JSON: top-level key metadata and counts. For text/markdown: bounded heading/first-line summary. Never returns full payloads. |
| `errors` | For JSON: recursively extracts keys/values matching error/failure/blocker/warning patterns. For text: bounded lines matching error patterns. Returns `status: no_errors_found` when clean. |

**Blocker codes:**

All blocker responses use the shared blocker envelope:
```json
{
  "code": "string",
  "message": "bounded string",
  "recoverable": true,
  "evidence": [],
  "next_actions": []
}
```

Active blocker codes:
- `unsafe_request` — Invalid or unsafe arguments.
- `unknown_run` — Run not found.
- `artifact_kind_not_allowed` — Kind not in the readback eligibility allowlist.
- `artifact_not_found` — No artifact of the requested kind for the run.
- `unsafe_artifact_path` — Stored artifact path outside the run artifact directory.
- `artifact_binary_or_unsupported` — Binary or non-UTF-8 content blocked for content modes.
- `artifact_read_failed` — Filesystem read error.
- `artifact_redaction_blocked` — Content contains high-risk sensitive material that cannot be safely redacted.
- `artifact_oversized` — (Reserved) Artifact exceeds configured size limits.

**Response fields:**
- `ok` — Boolean success indicator (`true` for success, `false` for blockers).
- `tool`, `run_id`, `artifact_kind`, `view_mode`
- `artifact` — Metadata object (artifact_id, kind, mime_type, size_bytes, created_at, content_hash, content_hash_status, artifact_ref). Never includes local absolute paths.
  - `content_hash` is the SHA-256 of the full registered artifact when computed.
  - `content_hash_status` is one of `computed_full`, `omitted_by_request`, `omitted_oversized`, or `unavailable`. It is never a hash of only the bounded excerpt unless explicitly labeled as returned-content scope.
- `content` — Omitted for metadata_only; bounded/redacted content otherwise.
- `redaction_status` — One of `not_required`, `redacted`, `blocked`.
- `truncated`, `returned_bytes`, `max_bytes`, `blockers`.

**Eligible readback kinds:**
Validation reports (`validation_run_json`, `packet_validation_report`, `brief_validation_report`, etc.), compiler outputs (`canonical_packet`, `executor_brief`), executor diagnostics (`executor_result`, `executor_stdout`, `executor_stderr`, `command_log`), audit packets, git evidence, planner handoffs, parsed frontmatter, context packets, and related diagnostics. Not all write-allowed artifact kinds are readback-eligible.

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
- **No arbitrary file read/write.** All artifact writes go through `relay/internal/artifacts` conventions which enforce path containment and allowed kind lists. `get_run_artifact` reads only artifacts registered by `(run_id, artifact_kind)` with strict path validation and does not accept caller-supplied filesystem paths or URLs.
- **No git mutation.** No commit, push, stage, merge, branch, checkout, reset, or worktree operations.
- **No run closure.** `submit_audit_packet` applies a status transition and writes artifacts but does not close or complete runs.
- **No run/executor/git side effects from plan submission.** `submit_planner_pass_plan` creates plan/pass records only and does not create runs, dispatch executors, mutate git, or read chat context.
- **Broker retrieval is bounded and gated.** Context broker tools are enabled by default under the `local-operator` profile, register only under explicit profile selection, reject unknown input fields, and return bounded structured JSON with provenance fields.
- **No shell or arbitrary filesystem access from broker tools.** Context broker tools wrap registered project repositories, snapshot-backed file inventory/search/read, git status/diff, and context packet services only.
- **Repository alias resolution is registration-only.** `resolve_project_repository` derives aliases only from registered repository IDs and does not authorize arbitrary file browsing, shell execution, git mutation, repository registration mutation, context packet creation, plan submission, or run creation.
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
- **Project orchestrator PASS-008:** Added end-to-end orchestrator workflow hardening tests and operator documentation for Continue Plan, Audit Ready, MCP work-packet retrieval, and human-gated advancement.
- **Refactor backlog PASS-008 (tests/docs/release hardening):** Added the refactor backlog concept/workflow documentation (`docs/refactor-backlog.md`), operator-guide manual QA checklist and `revision_required` clarification, and the `npm run release:smoke` release validation alias. No new MCP tools, routes, product behavior, or expanded scope were introduced.

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
