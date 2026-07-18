# Relay MCP

Relay exposes one canonical JSON-RPC tool registry over stdio and HTTP. The registry is implemented in `internal/mcp`; both transports list and dispatch the same profile-selected definitions.

## Transports

### Stdio

`cmd/mcpserver` opens the workflow store and serves newline-delimited JSON-RPC 2.0 on stdin/stdout. `scripts/local/relay-mcp-stdio.mjs` is the supported local launcher and includes an executable self-test for initialization, ping, paginated `tools/list`, exact ordered inventory, and OpenAI file-parameter metadata.

### HTTP

`cmd/relay` serves the same registry at `POST /mcp` on the normal Relay daemon, which defaults to `http://localhost:8080`.

- Methods other than POST return HTTP 405.
- When `RELAY_MCP_AUTH_TOKEN` is configured, requests require `Authorization: Bearer <token>`.
- An empty token leaves the endpoint unauthenticated and emits a warning. That mode is for loopback-only connector proof and must not be exposed.
- `RELAY_MCP_DISABLE_AUTH=true` explicitly disables enforcement for local development; it is not production exposure guidance.

## Profiles

`RELAY_MCP_PROFILE` accepts exactly `planner`, `auditor`, or `local_operator`. Missing or invalid input fails closed to `planner`.

| Profile | Ordered tools |
| --- | --- |
| `planner` | `validate_artifact`, `list_projects`, `submit_plan`, `get_plan`, `create_run` |
| `auditor` | `validate_artifact`, `create_run`, `get_audit_packet`, `get_run_artifact`, `record_audit_decision` |
| `local_operator` | `validate_artifact`, `list_projects`, `submit_plan`, `get_plan`, `create_run`, `get_audit_packet`, `get_run_artifact`, `record_audit_decision` |

A tool outside the active profile is not registered and returns JSON-RPC method-not-found when called. The server registers the selected definitions before dispatch, so every advertised name reaches one canonical handler branch.

## File parameters

`validate_artifact`, `submit_plan`, and `create_run` advertise one OpenAI file parameter named `artifact_file`. It contains:

- `download_url` — bounded HTTPS source URL;
- `file_id` — nonempty external file identity;
- `file_name` — a canonical `.plan.json` or `.execution-spec.json` basename;
- optional `mime_type`.

For `validate_artifact`, the accepted filename forms also include `.requirements.md`, `.design.md`, and ticket-qualified `.design-brief.md`; `submit_plan` and `create_run` remain JSON-only admission paths.

The fetcher retrieves exact bytes, rejects unsafe or unsupported references, and never returns signed download URLs in tool output. Submission actions compare the downloaded bytes with the required `expected_sha256` before durable mutation.

## Actions

### `validate_artifact`

**Profiles:** Planner, Auditor, local operator.

**Input:** `artifact_file`.

Validates one canonical Plan or Execution Spec JSON artifact, or authored Requirements, Shared Design, or Ticket Design Brief Markdown artifact, by exact downloaded bytes. Markdown validation checks only required headings: it does not score or interpret content. It returns the computed SHA-256, artifact kind, bounded diagnostics, and notices. It does not persist the artifact, admit a ticket, or return its body.

### `list_projects`

**Profiles:** Planner, local operator.

**Input:** bounded status and limit filters. Planner workflows use `status: "active"` and an explicit limit.

Returns bounded Project metadata for operator selection. It does not create or mutate Projects, infer a Project from repository state, or expose Project notes as hidden planning context.

### `submit_plan`

**Profiles:** Planner, local operator.

**Required input:** `project_id`, `artifact_file`, and lowercase 64-character `expected_sha256`.

Downloads and verifies one approved canonical Plan, recompiles it deterministically, and atomically creates the Plan, passes, repository associations, canonical source artifact, and rendered Plan artifact under the selected active Project. Project selection is external metadata and never changes canonical Plan bytes.

### `get_plan`

**Profiles:** Planner, local operator.

**Required input:** `plan_id`.

Returns bounded Project, Plan, pass, and artifact metadata. It does not return canonical Plan JSON or rendered Markdown bodies.

### `create_run`

**Profiles:** Planner, Auditor, local operator.

**Required input:** `artifact_file` and lowercase 64-character `expected_sha256`.

Optional managed association uses `plan_id` and positive `pass_number` together. Optional `remediates_run_id` associates a standalone remediation Run where current application rules allow it.

The action verifies exact bytes, recompiles the Execution Spec, and atomically creates a setup-ready Run plus canonical source and rendered Executor Brief artifacts. It does not start execution, mutate Git, or select a model.

### `get_audit_packet`

**Profiles:** Auditor, local operator.

**Required input:** `run_id`.

Returns the current authoritative audit packet for one Run. Readback revalidates packet freshness against the selected execution attempt and current local repository. The output includes packet identity, packet SHA-256, audited commit, Run status, and the bounded packet body.

### `get_run_artifact`

**Profiles:** Auditor, local operator.

**Required identity:** `run_id` and an `artifact_reference` declared by the current audit packet.

Returns bounded UTF-8 content for an audit-declared Run artifact. The audit service verifies packet declaration, attempt ownership, safe paths, size, SHA-256, and supported content. It is not generic filesystem or repository access.

### `record_audit_decision`

**Profiles:** Auditor, local operator.

**Required input:**

- `run_id`;
- `audit_packet_id`;
- lowercase 64-character `packet_sha256`;
- full lowercase 40-character `audited_commit`;
- `decision`, exactly `accepted` or `needs_revision`;
- `rationale`;
- `operator_confirmed: true`.

The action records one decision only against the exact current packet and audited commit. Acceptance completes the Run and managed pass; revision returns the Run to revision. Stale packets, mismatched hashes, conflicting audit state, or missing operator confirmation block before mutation.

## JSON-RPC behavior

The server supports:

- `initialize`;
- `notifications/initialized` as a notification;
- `ping`;
- paginated `tools/list`;
- `tools/call`.

`tools/list` preserves profile order. Unknown JSON-RPC methods and unregistered tool names return method-not-found. Invalid parameters use strict schemas with `additionalProperties: false` where defined.

Tool results distinguish successful workflow output from blocked business state. Blockers are bounded, omit secret values and absolute local paths, and identify recoverability and the affected field or resource.

## Safety boundaries

Relay MCP does not expose:

- arbitrary filesystem reads or writes;
- repository source browsing or search;
- shell execution;
- Git status, diff, branch, worktree, staging, commit, push, or pull-request mutation;
- executor dispatch or validation-result recording;
- Project mutation;
- automatic pass selection;
- historical compatibility actions.

The canonical runtime has no handoff, context-broker, source-snapshot, Plan Seed, refactor-backlog, local-audit, intent-drift, closeout, or generated-reference action surface.

## Validation

Use the current repository-owned checks:

```bash
make mcp-test
make mcp-smoke
npm run test:local-scripts
npm run release:smoke
```

For a separately running authenticated HTTP daemon:

```bash
make mcp-http-smoke RELAY_MCP_URL=http://localhost:8080/mcp RELAY_MCP_AUTH_TOKEN=dev-token
```

See [smoke.md](smoke.md) for the complete validation matrix and [chatgpt-mcp-local.md](chatgpt-mcp-local.md) for secure local tunnel setup.
