# Relay

Relay is a local-first workflow application for turning approved execution packages into tracked implementation Runs. It provides a React operator workbench, a Go API and execution service, role-app MCP tools, SQLite workflow state, and filesystem-backed evidence artifacts.

## Active workflow

The active lifecycle is:

```text
Delivery Ticket revision → exact revision approval → selection → complete Ticket Design Brief
→ zero or one Deterministic Operations artifact → package preparation → package approval
→ package-linked Run → execution and validation → audit → optional remediation
```

The Ticket Design Brief is the complete semantic implementation authority. Deterministic Operations are optional exact-execution data. Authored Execution Spec submission, standalone Run creation, and Plan/pass-associated Run creation are retired; package approval is the only active Run-creation path.

Projects retain repository associations, notes, Feature Workspaces, Delivery Tickets, and historical records. Plans and passes remain readable historical records only: no active Plan or pass creation or mutation path exists.

### Execution modes

Each package has one effective mode:

- `adaptive_no_operations`: the adaptive Executor receives the complete Brief.
- `adaptive_preflight_failed`: no deterministic writes occur; the adaptive Executor receives the complete Brief and verified preflight result.
- `adaptive_after_partial_application`: the adaptive Executor receives the complete Brief and exact applied/residual deterministic evidence.
- `deterministic_complete`: deterministic application is complete and no adaptive Executor attempt launches.

There is at most one adaptive Executor attempt. Relay does not perform multi-candidate or multi-agent execution.

Required validation commands originate in the approved execution assignment, propagate through execution admission and launch, and produce structured attempt-owned `execution_evidence`. There is no parallel validation-artifact family.

### Audit and remediation

An audit packet binds the approved package authority, exact Ticket revision, execution evidence, retained authority, repository evidence, and audited commit. Packet readback rechecks stored bytes, ownership, digest, current execution evidence, and repository evidence; stale or superseded packets cannot receive decisions.

The Auditor role app records `accepted` or `needs_revision` decisions. Findings may be attributed to `implementation`, `governing_package`, or `both`. A `needs_revision` decision produces immutable remediation evidence for a fresh-context Planner pass; the prior Executor transcript is not supplied. Remediation returns through the normal Ticket revision, approval, package, and Run lifecycle.

## MCP

Relay exposes exactly three public HTTP MCP role apps:

| Role app | Endpoint |
| --- | --- |
| Wayfinder | `POST /mcp/wayfinder` |
| Planner | `POST /mcp/planner` |
| Auditor | `POST /mcp/auditor` |

Each app compiles its fixed internal routes and generated tool catalog. The seven `/mcp/v1/...` values are internal route identities, not connector URLs; request data cannot select another role or internal route. Private ingress listeners are also role-specific. The `*:all` tunnel commands supervise registrations for all three role apps; they do not create an MCP endpoint. See [docs/mcp.md](docs/mcp.md) and [docs/chatgpt-mcp-local.md](docs/chatgpt-mcp-local.md).

## Web application

The TanStack Start application is normally available at `http://localhost:3000`; the Go API normally listens at `http://localhost:8080`.

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
- [MCP contract](docs/mcp.md)
- [Local ChatGPT tunnel](docs/chatgpt-mcp-local.md)
- [Frontend API contract](docs/api/frontend-api-contract.md)
