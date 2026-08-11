# Relay

Relay is a local-first workflow application for turning approved execution packages into tracked implementation Runs. It provides a React operator workbench, a Go API and execution service, role-app MCP tools, SQLite workflow state, and filesystem-backed evidence artifacts.

## Active workflow

The active lifecycle is:

```text
Delivery Ticket v2 revision → exact revision approval → selection → package preparation
→ exact package approval → package-linked Run → immutable ExecutionAssignment
→ Orchestrator execution → audit → optional remediation
```

Zero or one Deterministic Operations artifact is authored with the Delivery Ticket, embedded in the approved package, and applied during execution before any adaptive dispatch.

The authority chain is retained governing authority, the exact approved Delivery Ticket v2 revision, selected Ticket membership, optional Deterministic Operations, the approved immutable package, the package-linked Run, the immutable ExecutionAssignment, runtime execution evidence, the audit packet and decision, and immutable remediation evidence when revision is required. The approved Delivery Ticket v2 is the complete semantic implementation authority; Deterministic Operations add optional exact-execution data and never replace the Ticket. The package binds the exact repository-instruction basis: every `AGENTS.md` applicable to the selected Ticket's inspected source paths, resolved from the exact selected source closure and carried by identity through the immutable ExecutionAssignment. Package approval derives the immutable ExecutionAssignment for the package-linked Run; the Orchestrator executes that assignment, which embeds the approved Delivery Ticket, verified authority layers, bound repository-instruction identities, and required validation commands. Adaptive execution stores structured validation results in attempt-owned `execution_evidence`; deterministic-complete execution has no adaptive attempt and represents assigned validations as `not_run` in audit evidence because no adaptive Executor attempt was dispatched. Package approval is the only active Run-creation path. Ticket Design Briefs, authored Execution Spec submission, standalone Run creation, and Plan/pass-associated Run creation are retired.

Projects retain repository associations, notes, Feature Workspaces, Delivery Tickets, and historical records. Plans and passes remain readable historical records only: no active Plan or pass creation or mutation path exists.

### Execution modes

Each package has one effective mode:

- `adaptive_no_operations`: no deterministic writes; the Orchestrator executes the immutable ExecutionAssignment containing the approved Delivery Ticket v2.
- `adaptive_preflight_failed`: no deterministic writes; the Orchestrator executes the assignment with verified preflight failure evidence.
- `adaptive_after_partial_application`: deterministic writes occurred; the Orchestrator executes the assignment with exact applied/residual evidence.
- `deterministic_complete`: deterministic writes completed the package; no adaptive Orchestrator dispatch launches.

There is at most one adaptive Executor attempt. Relay does not perform multi-candidate or multi-agent execution.

In adaptive modes, required validation commands originate in the immutable ExecutionAssignment, propagate through execution admission and adaptive launch, and produce structured attempt-owned `execution_evidence`. In deterministic-complete execution, no adaptive launch occurs and assigned validations appear as `not_run` audit evidence because no adaptive Executor attempt was dispatched. There is no parallel validation-artifact family.

### Audit and remediation

An audit packet binds the approved package authority, exact Ticket revision, execution evidence, retained authority, repository evidence, and audited commit. Packet readback rechecks stored bytes, ownership, digest, current execution evidence, and repository evidence; stale or superseded packets cannot receive decisions.

The Auditor role app records `accepted` or `needs_revision` decisions. Findings may be attributed to `implementation`, `governing_package`, or `both`. A `needs_revision` decision produces immutable remediation evidence for a fresh-context Planner revision; the prior Executor transcript is not supplied. Remediation returns through the normal Ticket revision, approval, package, and Run lifecycle.

## MCP

Relay exposes exactly three public HTTP MCP role apps:

| Role app | Endpoint |
| --- | --- |
| Wayfinder | `POST /mcp/wayfinder` |
| Planner | `POST /mcp/planner` |
| Auditor | `POST /mcp/auditor` |

Each app compiles its fixed internal routes and generated tool catalog. The seven `/mcp/v1/...` values are internal route identities, not connector URLs; request data cannot select another role or internal route. Private ingress listeners are also role-specific. The `*:all` tunnel commands supervise registrations for all three role apps; they do not create an MCP endpoint. See [docs/mcp.md](docs/mcp.md) and [docs/chatgpt-mcp-local.md](docs/chatgpt-mcp-local.md).

## Web application

The TanStack Start application is normally available at `http://localhost:3000`; the Go API normally listens at `http://localhost:18080`.

| Route | Surface |
| --- | --- |
| `/projects` | Projects, repositories, Feature Workspaces, Tickets, and retained history |
| `/plans` | Read-only historical Plans and passes |
| `/runs` | Run registry and package-linked Run guidance |
| `/runs/{runId}/execute` | Execution attempts and evidence |
| `/runs/{runId}/audit` | Audit packet status and preparation |

## Setup

```bash
npm --prefix apps/web install
sqlc generate
templ generate
npm run build
go build -o bin/relay.exe ./cmd/relay
go run ./cmd/relay
npm run dev:web
```

Use the three-role tunnel supervision commands for local ChatGPT registrations:

```bash
npm run chatgpt-mcp:init:all
npm run chatgpt-mcp:doctor:all
npm run chatgpt-mcp:start:all
npm run chatgpt-mcp:status:all
npm run chatgpt-mcp:stop:all
```

## Documentation

- [Operator guide](docs/operator-guide.md)
- [Package-native workflow](docs/package-workflow.md)
- [Package workflow examples](docs/examples/package-workflow/)
- [MCP contract](docs/mcp.md)
- [Local ChatGPT tunnel](docs/chatgpt-mcp-local.md)
- [Frontend API contract](docs/api/frontend-api-contract.md)
