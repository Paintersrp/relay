# Relay MCP

Relay has one aggregate compatibility surface and three ChatGPT-facing role apps. The role apps compile from the seven immutable internal route manifests without changing their route path, handler ownership, schemas, manifest digest, or standing-authority binding.

## Transports

### Stdio and aggregate HTTP

`cmd/mcpserver` opens the profile-selected aggregate registry over newline-delimited JSON-RPC 2.0 on stdin/stdout. `scripts/local/relay-mcp-stdio.mjs` is the supported local launcher and includes an executable self-test for initialization, ping, paginated `tools/list`, exact ordered inventory, and OpenAI file-parameter metadata.

`cmd/relay` also serves aggregate `POST /mcp` on the normal Relay daemon, which defaults to `http://localhost:8080`. The aggregate surface is not a role-app connector URL: it returns HTTP `409` while a cutover activation is active. It remains separate from the three role apps and does not close or redirect them.

### Role-app HTTP

Use exactly one of these role-level app URLs for a ChatGPT HTTP connector:

| Public app surface | HTTP endpoint | Compiled internal route members |
| --- | --- | --- |
| Wayfinder | `POST /mcp/wayfinder` | 3 |
| Planner | `POST /mcp/planner` | 2 |
| Auditor | `POST /mcp/auditor` | 2 |

Methods other than POST return HTTP 405. When `RELAY_MCP_AUTH_TOKEN` is configured, each endpoint requires `Authorization: Bearer <token>`. An empty token leaves the endpoint unauthenticated and emits a warning; that mode is only for loopback connector proof. `RELAY_MCP_DISABLE_AUTH=true` explicitly disables enforcement for local development and is not production exposure guidance.

The seven former `/mcp/v1/...` routes are removed and return HTTP `404`; no legacy route redirects to a role app.

### Generated catalog and inventory

`BuildMCPAppSurfaceManifests` is the single generated and tested inventory table for the public role apps. Each `AppSurfaceManifest.Tools` row maps these identities without hand-maintained aliases:

| Public app surface | Public advertised tool name | Internal tool name | Internal route path | Surface contract |
| --- | --- | --- | --- | --- |
| `AppSurfaceManifest.Surface` | `AppToolManifest.AdvertisedName` | `AppToolManifest.InternalToolName` | `AppToolManifest.InternalRoutePath` | `AppToolManifest.SurfaceContract` |

Only `AdvertisedName` is exposed as the MCP tool name in that app's `tools/list`. If an internal tool name collides within one role app, its public name is the deterministic catalog alias `<surface-contract-with-dots-replaced-by-hyphens>__<internal-tool-name>`; otherwise it remains the internal tool name. Public aliases are catalog-only: a request cannot select a route, a role, an internal name, or authority context. Each alias is statically bound to exactly one compiled internal route and its standing authority. The route-contract tests build and verify every generated inventory row, including public name, internal name, route path, surface contract, manifest digest, and authority identity.

## Private role-app ingress

`cmd/relay` supervises three isolated private listeners, each forwarding to one fixed role-app URL on the main Relay daemon. Request content cannot select another upstream, another app, an internal route, or aggregate `/mcp`.

| Mapping | Role-app route | Listener override | Default |
| --- | --- | --- | --- |
| `wayfinder` | `/mcp/wayfinder` | `RELAY_MCP_INGRESS_WAYFINDER_ADDR` | `127.0.0.1:18101` |
| `planner` | `/mcp/planner` | `RELAY_MCP_INGRESS_PLANNER_ADDR` | `127.0.0.1:18102` |
| `auditor` | `/mcp/auditor` | `RELAY_MCP_INGRESS_AUDITOR_ADDR` | `127.0.0.1:18103` |

Listener overrides accept only loopback, RFC 1918 private IPv4, or IPv6 unique-local IP literals with nonzero ports. Hostnames, wildcard, unspecified, public, link-local, multicast, and port-zero addresses are rejected.

`RELAY_MCP_INGRESS_UPSTREAM_BASE_URL` optionally replaces the default `http://127.0.0.1:<Relay port>` private upstream base. It must use `http` or `https`, an IP-literal private or loopback host, an explicit nonzero port, and no path, query, fragment, or user information. Relay appends the fixed role-app route for each mapping.

Each listener accepts only `POST` to its exact role-app route and `GET /healthz`. A mapping probes its fixed upstream route independently, reports only bounded health metadata, and restarts independently. One listener, upstream, trace, or client failure cannot redirect to another app, stop another mapping, or stop the main Relay daemon.

### Local-hop bearer

`RELAY_MCP_INGRESS_UPSTREAM_BEARER_TOKEN` optionally configures the bearer used from private ingress to the main Relay handler. The ingress always removes client `Authorization`; when configured, it injects exactly one upstream bearer. Startup output reports only whether a bearer is configured. The value is never included in health, traces, errors, descriptors, tool arguments, responses, or logs.

### Metadata traces

`RELAY_MCP_TRACE_DIR` selects the trace root; the default is `data/transport/mcp-traces`. Each mapping writes independent canonical JSON Lines segments with directory mode `0700` and file mode `0600`.

A trace contains route and request identities, allowlisted source identities, byte counts, the SHA-256 of exact response bytes attempted downstream, completion classification, bounded outcome and error classes, and downstream write evidence. It never stores request or response bodies, source content, artifacts, conversations, mutation payloads, credentials, authorization, signed URLs, raw cursors, raw paths, or protected diagnostics.

Retention is the earlier of:

- `RELAY_MCP_TRACE_MAX_AGE`, default and maximum `336h`, minimum `1h`;
- `RELAY_MCP_TRACE_MAX_BYTES`, default and maximum `104857600`, minimum `1048576`.

Segments rotate at eight mebibytes. Trace persistence failure leaves the authoritative MCP response unchanged and marks only that mapping unhealthy.

## Aggregate profiles

`RELAY_MCP_PROFILE` controls only the aggregate stdio and `/mcp` surfaces. It accepts exactly `planner`, `auditor`, or `local_operator`. Missing or invalid input fails closed to `planner`.

| Profile | Ordered tools |
| --- | --- |
| `planner` | `validate_artifact`, `list_projects`, `submit_plan`, `get_plan`, `create_run` |
| `auditor` | `validate_artifact`, `create_run`, `get_audit_packet`, `get_run_artifact`, `record_audit_decision` |
| `local_operator` | `validate_artifact`, `list_projects`, `submit_plan`, `get_plan`, `create_run`, `get_audit_packet`, `get_run_artifact`, `record_audit_decision` |

A tool outside the active aggregate profile is not registered and returns JSON-RPC method-not-found when called. The server registers the selected definitions before dispatch, so every advertised name reaches one canonical handler branch.

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

## Cutover tools

Cutover readiness, prepare, activate, rollback, history, boundary, and roll-forward operations expose retained exact authority through the operation registry. Each mutation requires the exact Transition Plan ticket, authority revision, and workspace association present at preparation.

The MCP surface delegates to the same `internal/app/cutover` service used by HTTP. No direct store bypass is possible.

## JSON-RPC behavior

The server supports:

- `initialize`;
- `notifications/initialized` as a notification;
- `ping`;
- paginated `tools/list`;
- `tools/call`.

Aggregate `tools/list` preserves profile order. Role-app `tools/list` exposes only its deterministic advertised catalog. Unknown JSON-RPC methods and unregistered tool names return method-not-found. Invalid parameters use strict schemas with `additionalProperties: false` where defined.

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
go test ./internal/mcp -run 'Trace'
go test ./internal/transport/transporttrace
go test ./internal/transport/mcpingress
go test ./internal/server -run 'MCPIngress|MCPRoutes'
go test ./cmd/relay -run 'PrivateMCPIngress'
make mcp-test
make mcp-smoke
npm run test:local-scripts
npm run release:smoke
```

For a separately running authenticated HTTP daemon, use one role app:

```bash
make mcp-http-smoke RELAY_MCP_URL=http://localhost:8080/mcp/planner RELAY_MCP_AUTH_TOKEN=dev-token
```

See [smoke.md](smoke.md) for the complete validation matrix and [chatgpt-mcp-local.md](chatgpt-mcp-local.md) for secure local tunnel setup.
