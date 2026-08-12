# Backend API and Application Feature Architecture

Relay organizes workflow behavior by transport, application ownership, persistence, and composition. New work should extend the existing feature owner rather than introducing a parallel root API or compatibility layer.

## Composition roots

- `cmd/relay` opens the workflow store and starts the HTTP/API server.
- `internal/server/workflow_routes.go` constructs current application services and mounts HTTP routes, browser redirects, and the role-app MCP routes.

Composition roots wire dependencies. They do not own feature behavior.

## HTTP transport packages

| Package | Ownership |
| --- | --- |
| `internal/api/repositories` | Registered repository lookup and mutation transport |
| `internal/api/projects` | Project, note, and Project-repository transport |
| `internal/api/canonical` | Retained canonical artifact transport |
| `internal/api/plans` | Historical Plan and pass read transport |
| `internal/api/runs` | Run reads, package execution controls, cancellation, reconciliation, and status transport |
| `internal/api/features`, `internal/api/tickets`, `internal/api/packages` | Feature Workspace, Ticket, selection, and execution-package transport |
| `internal/api/artifacts` | Bounded workflow artifact transport |
| `internal/api/audits` | Audit readiness and packet preparation/read transport |
| `internal/api/programs` | Program dispatch, handoff, Integration Assignment, Merge-result, and verification transport |
| `internal/api/shared` | Shared HTTP decoding, response, and error helpers |

Handlers translate HTTP requests and responses. They delegate business rules to application services and must not duplicate store transactions or MCP behavior.

## Application ownership

| Area | Package ownership |
| --- | --- |
| Workflow read models | `internal/app/workflow` |
| Project mutation | `internal/app/projects/workflow` |
| Historical Plan and pass reads | `internal/app/workflow` |
| Ticket, selection, and package workflow | `internal/app/tickets`, `internal/app/packages`, `internal/app/operations` |
| Package-linked Run lifecycle | `internal/app/runs/workflow` |
| Audit packet preparation, readback, and decisions | `internal/app/audits` |
| Program dispatch preparation, immutable dispatch, handoff projection, and Integration Assignment runtime | `internal/app/programs` |
| Execution attempts, cancellation, and reconciliation | `internal/executor` |
| Deterministic source application | `internal/applier` |

Application services own validation, state transitions, transaction boundaries, and durable mutation. Transport packages map external inputs to these services.

## MCP ownership

`internal/mcp` compiles fixed catalogs for the Wayfinder, Planner, and Auditor role apps. Each app dispatches only to its fixed internal routes and standing authority; request data cannot select another role or route. It calls the same current application and store owners used by HTTP.

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

`internal/server/workflow_routes.go` constructs and mounts repositories, Projects, Feature Workspaces, Tickets, packages, historical Plan reads, Runs, execution, artifacts, audits, and role-app MCP routes. Browser paths redirect to the React workbench; `/api/*` and the role-app MCP paths remain Go-owned.

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

## Ticket-Audit Package Approval Proof Chain

Ticket-oriented audit evidence carries an exact package approval chain from the audited commit back through every authority:

1. `audited_commit` → repository package evidence.
2. Package member → `delivery_ticket_revision` → `delivery_ticket` → `feature_workspace`.
3. Ticket revision approval → `authority_revision` / `source_closure`.
4. `execution_package` → `execution_package_approval` (immutable, once per package).
5. `execution_package_approval.package_sha256` = `execution_package.package_sha256`.
6. `runs.package_approval_row_id` = `execution_package_approval.id`.

The audit packet artifact `ticket_package_evidence` captures the package approval identity (`approval_id`, `approved_package_sha256`, `operator_confirmation_evidence`) in its Package section. Audit obligations and decision-effect rows independently store the same approval identity as `package_approval_row_id` and `approved_package_sha256` columns. Database triggers enforce that these values match the Run's linked approval and the execution package SHA transactionally.

Preparation, readback, and decision recording each re-resolve the package approval. Stale or missing approval blocks all ticket-aware effects. Successful accepted and needs-revision decisions retain the exact approval basis in the revision-decision row.
