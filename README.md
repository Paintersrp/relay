# Relay

Relay is a local-first workflow application for turning canonical Plans and Execution Specs into tracked implementation Runs. It provides a React operator workbench, a Go API and execution service, profile-gated MCP tools, SQLite workflow state, and filesystem-backed evidence artifacts.

Relay's current contract is canonical JSON. It does not ingest free-form Planner handoffs or transform prompts through the pre-pivot handoff pipeline.

## Workflow

### Projects, repositories, and Plans

1. Register each local repository with a stable `repo_target` and local path.
2. Optionally create a Project and attach registered repositories.
3. Submit a canonical Plan named `<feature-slug>.plan.json` to a Project. The compiler validates it and renders a Markdown Plan artifact; Relay persists the Plan and its ordered passes.
4. Inspect the Plan and pass records in the web application. A Plan is an optional coordination layer; standalone Runs remain supported.

A managed Run selects a Plan and positive pass number and uses an Execution Spec named `<feature-slug>.pass-<number>.execution-spec.json`. A standalone Run omits the Plan/pass association and uses `<feature-slug>.execution-spec.json`. Submission verifies the caller-provided SHA-256, validates the canonical document, renders the Executor Brief, and stores both source and rendered artifacts.

### Execution

Starting a Run requires an executor adapter and model. Relay verifies the repository path, branch, base commit, canonical Execution Spec, and rendered Executor Brief before implementation begins.

Execution is deterministic-first:

- If the Execution Spec contains supported deterministic operations, the applier checks paths, guards, occurrence counts, dependency ordering, and atomic groups before writing. It records an operation ledger, changed-file list, implementation result, and any failure packet.
- A completed deterministic application advances directly to validation without launching a model executor.
- A blocked deterministic application moves the Run to revision without executor fallback.
- A partial application passes bounded residual-operation context to the selected executor.
- If there are no applicable deterministic operations, Relay launches the executor directly.

The current adapters are OpenCode Go (`opencode_go`), Codex (`codex`), Antigravity (`antigravity`), and Kiro CLI (`kiro_cli`). Executor attempts are asynchronous and support status polling, cancellation, reconciliation, and retry from eligible failed or cancelled Runs. Relay stores command metadata, stdout, stderr, normalized executor output, and structured execution evidence as artifacts.

### Validation and audit

The Execution Spec is the authority for ordered validation commands. The Run lifecycle distinguishes `validating`, `validation_failed`, `audit_ready`, `needs_revision`, and `completed`, and the application service records pass/fail validation outcomes. The current HTTP/web surface does not yet expose the validation-result transition; operators cannot complete that transition solely through the workbench.

For an audit-ready Run, Relay can prepare an audit packet against a full audited commit SHA. Packet preparation resolves the selected implementation actor (deterministic applier or executor), verifies current repository evidence, captures the audited commit diff, and persists a content-addressed packet. Packet freshness, artifact ownership, size, and SHA-256 are rechecked on readback and decision submission.

Audit decisions are `accepted` or `needs_revision` and require explicit operator confirmation through the auditor or local-operator MCP profile. Acceptance completes the Run and its associated pass; the Plan completes when all passes are complete. Revision returns the Run to revision and keeps its managed pass in progress. The web audit view can prepare and inspect packets, but audit decision submission is currently MCP-only.

## Web application

The TanStack Start application in `apps/web` is the operator UI, normally at `http://localhost:3000`:

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

The current JSON endpoint contract is documented in [docs/api/frontend-api-contract.md](docs/api/frontend-api-contract.md). The backend layering conventions are described in [docs/backend/api-app-feature-architecture.md](docs/backend/api-app-feature-architecture.md).

## MCP

Relay serves the same JSON-RPC tool registry over stdio (`cmd/mcpserver`) and authenticated HTTP (`/mcp`). The registry is for canonical artifact submission, bounded workflow lookup, and audit exchange; it does not expose a shell, arbitrary file access, source browsing, or Git mutation.

`RELAY_MCP_PROFILE` selects the exact surface and defaults to `planner`:

| Profile | Tools |
| --- | --- |
| `planner` | `validate_artifact`, `list_projects`, `submit_plan`, `get_plan`, `create_run` |
| `auditor` | `validate_artifact`, `create_run`, `get_audit_packet`, `get_run_artifact`, `record_audit_decision` |
| `local_operator` | Union of the planner and auditor tools |

File-bearing submission tools accept one bounded HTTPS artifact, verify its exact bytes and optional expected SHA-256, then persist it through the same application services used by HTTP. HTTP MCP requires `RELAY_MCP_AUTH_TOKEN` unless `RELAY_MCP_DISABLE_AUTH=true` is explicitly set for local development.

## Persistence

Relay uses two coordinated local stores:

- SQLite metadata at `data/workflow/relay-workflow.sqlite`, configurable with `RELAY_WORKFLOW_DB_PATH`.
- Artifact files under `data/workflow/artifacts`, configurable with `RELAY_WORKFLOW_ARTIFACTS_DIR`.

SQLite tracks repository targets, Projects and notes, Project-repository associations, Plans, passes and dependencies, Runs and remediation links, execution attempts, artifacts, audit packets, and audit decisions. Artifact rows contain ownership, media type, relative path, size, and SHA-256; payload bytes remain on disk. Embedded Goose migrations run automatically when either server opens the workflow store. `sqlc` generates typed query code in `internal/store/workflowgenerated` from `internal/db/workflow_migrations` and `internal/db/workflow_queries`.

Manual migration inspection and repair are available through:

```bash
make workflow-db-status
make workflow-db-migrate
```

## Setup and operation

Prerequisites are Go 1.25.7 or compatible, Node.js with npm, `sqlc`, `templ`, Bash, and Make. `goose` is required only for the manual migration targets. Install the Go CLIs if needed:

```bash
go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest
go install github.com/pressly/goose/v3/cmd/goose@latest
go install github.com/a-h/templ/cmd/templ@latest
```

Install the web dependencies and generate/build the application:

```bash
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

Copy `apps/web/.env.example` to `apps/web/.env` to configure the browser API base URL. The API loads `.env` and `.env.local` automatically. Executor binaries must be installed and authenticated independently; `.env.example` lists adapter-specific settings.

Useful Make targets:

```bash
make build          # web assets, sqlc, templ, and Go server binary
make test           # all Go tests
make mcp-build      # stdio MCP binary
make mcp-test       # MCP and MCP command tests
make mcp-smoke      # build and exercise the stdio MCP workflow
make validate-full  # complete repository validation tier
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
scripts/validate.sh                tiered repository validation
```

## Current boundaries

Relay currently does not:

- accept free-form handoffs, manual agent-result intake, or legacy prompt/packet formats;
- create source snapshots, context packets, Plan Seeds, refactor backlogs, or agent-reference documents;
- expose validation-result recording through HTTP/web or audit decision recording through HTTP/web;
- automatically repair validation failures or audit revisions;
- create branches or worktrees, stage or commit changes, push, create pull requests, or run hosted CI;
- provide arbitrary MCP filesystem, shell, source-search, or Git tools;
- automatically select or start the next Plan pass.

Relay operates on an already registered local repository and its existing branch/worktree. Operators remain responsible for repository preparation, reviewing canonical artifacts, executor authentication, validation evidence outside the currently exposed transition, and final Git delivery.
