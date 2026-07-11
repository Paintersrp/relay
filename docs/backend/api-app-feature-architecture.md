# Backend API and Application Feature Architecture

Relay organizes workflow behavior by transport, application ownership, persistence, and composition. New work should extend the existing feature owner rather than introducing a parallel root API or compatibility layer.

## Composition roots

- `cmd/relay` opens the workflow store and starts the HTTP/API server.
- `cmd/mcpserver` opens the same workflow store and starts the stdio MCP server.
- `internal/server/workflow_routes.go` constructs current application services and mounts HTTP routes, browser redirects, and `/mcp`.

Composition roots wire dependencies. They do not own feature behavior.

## HTTP transport packages

| Package | Ownership |
| --- | --- |
| `internal/api/repositories` | Registered repository lookup and mutation transport |
| `internal/api/projects` | Project, note, and Project-repository transport |
| `internal/api/canonical` | Canonical artifact validation and Plan/Run submission transport |
| `internal/api/plans` | Plan and pass read/mutation transport |
| `internal/api/runs` | Run reads, execution controls, cancellation, retry, and status transport |
| `internal/api/artifacts` | Bounded workflow artifact transport |
| `internal/api/audits` | Audit readiness and packet preparation/read transport |
| `internal/api/shared` | Shared HTTP decoding, response, and error helpers |

Handlers translate HTTP requests and responses. They delegate business rules to application services and must not duplicate store transactions or MCP behavior.

## Application ownership

| Area | Package ownership |
| --- | --- |
| Workflow read models | `internal/app/workflow` |
| Project mutation | `internal/app/projects/workflow` |
| Plan mutation and pass lifecycle | `internal/app/plans/workflow` |
| Canonical validation and submission | `internal/app/submissions` |
| Run creation and lifecycle | `internal/app/runs/workflow` |
| Audit packet preparation, readback, and decisions | `internal/app/audits` |
| Execution attempts, cancellation, and reconciliation | `internal/executor` |
| Deterministic source application | `internal/applier` |

Application services own validation, state transitions, transaction boundaries, and durable mutation. Transport packages map external inputs to these services.

## MCP ownership

`internal/mcp` owns one profile-gated JSON-RPC registry. It calls the same current application and store owners used by HTTP. It does not own an alternate workflow implementation.

- Planner: canonical artifact validation, Project discovery, Plan submission/read, and Run creation.
- Auditor: canonical artifact validation, Run creation, packet/artifact readback, and decision recording.
- Local operator: ordered union.

Stdio and HTTP use the same registry and dispatch. No compatibility adapters, handoff handlers, context broker, seed handlers, refactor handlers, or generated-reference registry remain.

## Persistence

### Workflow store

`internal/store/workflow` is the handwritten persistence boundary. It owns:

- database opening and automatic migration;
- transactions;
- repository targets;
- Projects and notes;
- Plans, passes, and dependencies;
- Runs and remediation links;
- execution attempts;
- artifact metadata;
- audit packets and decisions;
- coordinated database/filesystem artifact commits and rollback.

### Generated queries

`internal/store/workflowgenerated` is sqlc-generated output. Its inputs are:

- `internal/db/workflow_migrations`;
- `internal/db/workflow_queries`;
- `sqlc.yaml`.

Change query behavior through those source-owned inputs and regenerate. Do not hand-edit generated files independently.

### Filesystem artifacts

`internal/artifacts/workflow` stages, hashes, validates, promotes, and rolls back artifact bytes. The workflow store coordinates artifact batches with database transactions so failed commits do not leave partially promoted evidence.

## Route construction

`internal/server/workflow_routes.go` constructs and mounts the retained repositories, Projects, canonical submission, Plans, Runs, execution, artifacts, audits, and MCP routes. Browser paths redirect to the React workbench; `/api/*` and `/mcp` remain Go-owned.

Feature handlers should be mounted through this composition root. Do not add another server, root handler family, or hidden compatibility router.

## Import direction

Preferred dependency direction:

```text
cmd -> internal/server -> internal/api -> internal/app -> internal/store
                     +-> internal/mcp -> internal/app/internal/store
internal/executor -> application/store/artifact owners
internal/applier  -> bounded source application primitives
```

Rules:

- API and MCP transports may depend on application interfaces and shared transport helpers.
- Application services may depend on workflow store and artifact interfaces.
- Store and artifact packages must not depend on HTTP, MCP, or UI packages.
- Generated query packages remain below handwritten store ownership.
- Feature packages must not import a removed compatibility package to avoid using their current owner.

## Error behavior

Application errors remain typed and are translated at transport boundaries:

- HTTP uses current structured status and error responses.
- MCP uses JSON-RPC errors for protocol failures and bounded blocked tool results for workflow state or safety failures.
- Persistence and artifact failures preserve full internal error context in logs while external responses avoid secret values and absolute local paths.

Do not make a stale documentation claim true by adding a compatibility adapter. Correct the documentation or use the current owner.

## Validation

Changes to a feature should use the narrowest current package tests, then broader proof when the shared boundary requires it. Repository-wide closeout uses `npm run release:smoke`.

The current browser/API contract is documented in [../api/frontend-api-contract.md](../api/frontend-api-contract.md). The canonical MCP contract is documented in [../mcp.md](../mcp.md).
