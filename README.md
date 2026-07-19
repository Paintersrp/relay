# Relay

Relay is a local-first workflow application for turning canonical Plans and Execution Specs into tracked implementation Runs. It provides a React operator workbench, a Go API and execution service, profile-gated MCP tools, SQLite workflow state, and filesystem-backed evidence artifacts.

Relay accepts canonical JSON. It does not ingest free-form Planner handoffs or operate the retired handoff, context-packet, Plan Seed, refactor-backlog, local-audit, or generated-reference pipelines.

## Workflow

### Projects, repositories, and Plans

1. Register each local repository with a stable `repo_target` and local path.
2. Optionally create a Project and attach registered repositories.
3. Submit a canonical Plan named `<feature-slug>.plan.json` to an active Project. Relay validates it, renders the Markdown Plan, and persists the Plan and ordered passes.
4. Inspect the Plan and pass records in the web application. A Plan is an optional coordination layer; standalone Runs remain supported.

A managed Run selects a Plan and positive pass number and uses `<feature-slug>.pass-<number>.execution-spec.json`. A standalone Run omits the Plan/pass association and uses `<feature-slug>.execution-spec.json`. Submission verifies the caller-provided SHA-256, validates the canonical document, renders the Executor Brief, and stores both source and rendered artifacts.

### Execution

Starting a Run requires an executor adapter and model. Relay verifies the repository path, branch, base commit, canonical Execution Spec, and rendered Executor Brief before implementation begins.

Execution is deterministic-first:

- Supported deterministic operations are checked for safe paths, guards, occurrence counts, dependency ordering, and atomic groups before writing.
- A completed deterministic application advances directly to validation without launching a model executor.
- A blocked deterministic application moves the Run to revision without executor fallback.
- A partial application passes bounded residual-operation context to the selected executor.
- A Run with no applicable deterministic operations launches the executor directly.

The current adapters are OpenCode Go (`opencode_go`), Codex (`codex`), Antigravity (`antigravity`), and Kiro CLI (`kiro_cli`). Attempts are asynchronous and support polling, cancellation, reconciliation, and retry from eligible failed or cancelled Runs. Relay stores command metadata, stdout, stderr, normalized executor output, and structured execution evidence as artifacts.

### Validation and audit

The Execution Spec is authoritative for ordered validation commands. The Run lifecycle distinguishes `validating`, `validation_failed`, `audit_ready`, `needs_revision`, and `completed`. The application service records pass/fail validation outcomes, but the current HTTP/web surface does not expose the validation-result transition.

For an audit-ready Run, Relay prepares an audit packet against a full audited commit SHA. Packet preparation resolves the selected implementation actor, verifies repository evidence, captures the audited commit diff, and persists a content-addressed packet. Packet freshness, artifact ownership, size, and SHA-256 are rechecked on readback and decision submission.

Audit decisions are `accepted` or `needs_revision` and require explicit operator confirmation through the Auditor or `local_operator` MCP profile. Acceptance completes the Run and associated pass; the Plan completes when all passes complete. The web audit view prepares and inspects packets, but decision submission is MCP-only.

## Web application

The TanStack Start application in `apps/web` is normally available at `http://localhost:3000`:

| Route | Surface |
| --- | --- |
| `/` | Workflow overview |
| `/projects`, `/projects/new`, `/projects/{projectId}` | Project registry, repositories, notes, and associated Plans |
| `/plans`, `/plans/new` | Plan registry and canonical Plan submission |
| `/plans/{planId}` | Plan and pass overview |
| `/plans/{planId}/passes/{passId}` | Pass detail and associated Runs |
| `/runs`, `/runs/new` | Run registry and canonical Execution Spec submission |
| `/runs/{runId}/specification` | Canonical spec and rendered Executor Brief |
| `/runs/{runId}/execute` | Adapter/model selection, attempts, cancellation, and evidence |
| `/runs/{runId}/audit` | Audit readiness, packet preparation, and packet status |

The Go server normally listens at `http://localhost:8080`, owns `/api/*` and `/mcp`, and redirects non-API browser routes to the web application. Set `RELAY_WEB_BASE_URL` when the web origin differs from `http://localhost:3000`; set `VITE_RELAY_API_BASE_URL` in `apps/web/.env` when the API origin differs from `http://localhost:8080`.

## MCP

Relay serves one JSON-RPC registry over stdio (`cmd/mcpserver`) and HTTP (`/mcp`). `RELAY_MCP_PROFILE` defaults to `planner` and selects the exact ordered surface:

| Profile | Tools |
| --- | --- |
| `planner` | `validate_artifact`, `list_projects`, `submit_plan`, `get_plan`, `create_run` |
| `auditor` | `validate_artifact`, `create_run`, `get_audit_packet`, `get_run_artifact`, `record_audit_decision` |
| `local_operator` | Ordered eight-tool union of the Planner and Auditor profiles |

File-bearing submission tools accept one bounded HTTPS artifact, validate the exact downloaded bytes, and use the same application services as HTTP. The registry does not expose shell, arbitrary filesystem, source-search, or Git mutation tools.

HTTP MCP accepts POST JSON-RPC only. When `RELAY_MCP_AUTH_TOKEN` is set, the endpoint requires `Authorization: Bearer <token>`. An empty token leaves the endpoint unauthenticated and emits a warning; that mode is for loopback-only connector proof and must not be exposed.

For the complete action and transport contract, see [docs/mcp.md](docs/mcp.md).

## Secure local ChatGPT tunnel

The supported operator interface is the root package scripts:

```bash
npm run chatgpt-mcp:help
npm run chatgpt-mcp:init
npm run chatgpt-mcp:doctor
npm run chatgpt-mcp:start
```

Stdio is the default and launches the canonical `cmd/mcpserver` process through `scripts/local/relay-mcp-stdio.mjs`. Advanced HTTP mode uses `http://127.0.0.1:8080/mcp` and requires the Relay daemon to be running. Keep tunnel credentials only in ignored `.env`, `.env.local`, or process environment values.

See [docs/chatgpt-mcp-local.md](docs/chatgpt-mcp-local.md) for setup, diagnostics, profiles, and troubleshooting.

## Ticket-oriented cutover

Relay's ordinary admission transitions from legacy Plan/Run submission to ticket-oriented admission through one guarded application-owned cutover.

### Activation prerequisites

Before activation the operator must ensure:
- The exact Transition Plan ticket and authority revision are current in the workspace.
- Every declared prerequisite has recorded evidence.
- Every activation obligation has recorded evidence.
- Roll-forward criteria are declared.

### New-work admission

After activation, new legacy Plan submissions and managed Plan/pass Run creation are rejected. All new ordinary work enters through the ticket-oriented pipeline using delivery tickets, execution packages, and mutation leases.

### Historical reads

Historical Plan, pass, and Run reads remain available. In-progress legacy Runs continue to completion. Remediation of eligible persisted legacy Runs is always permitted regardless of cutover state.

### Rollback boundary

Rollback is available only while the cutover is active and the execution boundary is open. Once the first ticket-oriented Run crosses the boundary, the cutover becomes one-way and rollback is permanently forbidden.

### Safe roll-forward

The operator records evidence for each declared roll-forward criterion. When all criteria are satisfied, the cutover completes roll-forward.

## Persistence

Relay uses two coordinated local stores:

- SQLite metadata at `data/workflow/relay-workflow.sqlite`, configurable with `RELAY_WORKFLOW_DB_PATH`.
- Artifact files under `data/workflow/artifacts`, configurable with `RELAY_WORKFLOW_ARTIFACTS_DIR`.

SQLite tracks repository targets, Projects and notes, Project-repository associations, Plans, passes and dependencies, Runs and remediation links, execution attempts, artifacts, audit packets, and audit decisions. Artifact rows contain ownership, media type, relative path, size, and SHA-256; payload bytes remain on disk. Embedded Goose migrations run automatically when either server opens the workflow store. `sqlc` generates typed query code in `internal/store/workflowgenerated` from `internal/db/workflow_migrations` and `internal/db/workflow_queries`.

Manual migration inspection and repair:

```bash
make workflow-db-status
make workflow-db-migrate
```

## Setup and operation

Prerequisites are Go 1.25.7 or compatible, Node.js with npm, `sqlc`, `templ`, Bash, and Make. `goose` is required only for manual migration targets.

```bash
go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest
go install github.com/pressly/goose/v3/cmd/goose@latest
go install github.com/a-h/templ/cmd/templ@latest
npm --prefix apps/web install
sqlc generate
templ generate
npm run build
go build -o bin/relay.exe ./cmd/relay
go build -o bin/relay-mcpserver.exe ./cmd/mcpserver
```

Run the API and web application in separate terminals:

```bash
go run ./cmd/relay
npm run dev:web
```

Copy `apps/web/.env.example` to `apps/web/.env` to configure the browser API base URL. Copy selected values from `.env.example` to ignored `.env` or `.env.local` for executor and tunnel configuration. Executor binaries must be installed and authenticated independently.

Useful targets and scripts:

```bash
make build
make test
make mcp-build
make mcp-test
make mcp-smoke
make validate
npm run test:local-scripts
npm run release:smoke
```

## Architecture map

```text
cmd/relay/                         HTTP/API composition root
cmd/mcpserver/                     stdio MCP composition root
apps/web/src/routes/               TanStack Start route surfaces
apps/web/src/components/relay/     operator workflow components
apps/web/src/features/             typed API clients and workflow state
internal/server/                   HTTP router and service wiring
internal/api/                      transport handlers by feature
internal/app/                      Project, Plan, Run, submission, and audit use cases
internal/speccompiler/             canonical JSON validation and Markdown rendering
internal/applier/                  guarded deterministic operations and evidence
internal/executor/                 adapters, attempts, cancellation, and fallback execution
internal/mcp/                      profile-gated JSON-RPC registry and handlers
internal/store/workflow/           SQLite transactions and artifact coordination
internal/store/workflowgenerated/  sqlc-generated query layer
internal/db/workflow_migrations/   embedded Goose schema migrations
internal/db/workflow_queries/      sqlc query inputs
internal/artifacts/workflow/       filesystem artifact batches
```

## Current documentation

- [Operator guide](docs/operator-guide.md)
- [Canonical MCP specification](docs/mcp.md)
- [Secure local ChatGPT tunnel](docs/chatgpt-mcp-local.md)
- [Validation and smoke checks](docs/smoke.md)
- [Frontend API contract](docs/api/frontend-api-contract.md)
- [Backend API/application architecture](docs/backend/api-app-feature-architecture.md)

## Current boundaries

Relay currently does not:

- accept free-form handoffs, manual agent-result intake, or legacy prompt/packet formats;
- create source snapshots, context packets, Plan Seeds, refactor backlogs, local audits, intent-drift reviews, or agent-reference documents;
- expose validation-result recording through HTTP/web or audit decision recording through HTTP/web;
- automatically repair validation failures or audit revisions;
- create branches or worktrees, stage or commit changes, push, create pull requests, or run hosted CI;
- provide arbitrary MCP filesystem, shell, source-search, or Git tools;
- automatically select or start the next Plan pass.

Relay operates on an already registered local repository and its existing branch/worktree. Operators remain responsible for repository preparation, canonical artifact review, executor authentication, validation evidence outside the currently exposed transition, and final Git delivery.
