# Relay MCP

Relay publishes three public role-app MCP surfaces:

| Role app | Public endpoint | Compiled internal routes |
| --- | --- | --- |
| Wayfinder | `POST /mcp/wayfinder` | 3 |
| Planner | `POST /mcp/planner` | 2 |
| Auditor | `POST /mcp/auditor` | 2 |

Each role app compiles its fixed internal routes, handlers, standing authority, and generated tool catalog. `BuildMCPAppSurfaceManifests` is the generated, tested catalog authority; documentation does not maintain a duplicate tool inventory.

The seven `/mcp/v1/...` values are internal route identities. They are not public connector URLs. A connector calls its role-app URL, and request data cannot select another role, an internal route, an internal tool name, or authority context. Private ingress listeners likewise accept only the fixed route for their role.

The role-specific private ingress mappings are:

| Role | Role-app route | Default listener |
| --- | --- | --- |
| Wayfinder | `/mcp/wayfinder` | `127.0.0.1:18101` |
| Planner | `/mcp/planner` | `127.0.0.1:18102` |
| Auditor | `/mcp/auditor` | `127.0.0.1:18103` |

HTTP MCP accepts POST JSON-RPC. When configured, role-app authentication uses the Relay MCP bearer token. A connector should use the generated public advertised names from that app's `tools/list` response.

## Role responsibilities

Wayfinder provides route-bound operation packets and packet-authorized retained-source investigation. Planner owns Ticket and package authoring in its fixed role surface. Auditor owns audit packet review and audit decisions in its fixed role surface.

The `*:all` local tunnel commands supervise all three role registrations together. This is multi-role registration orchestration, not a fourth MCP transport or connector URL.

## Workflow authority

The active lifecycle is Ticket revision, exact approval, selection, complete Ticket Design Brief, optional Deterministic Operations, package preparation, package approval, package-linked Run, execution and validation, audit, and optional remediation. Plans and passes are retained read-only history and have no active mutation or execution authority.

The Design Brief is the complete semantic authority. Deterministic Operations are optional exact-execution data. Required validation commands come from the approved assignment, flow into execution admission and launch, and remain in attempt-owned `execution_evidence`.

Audit packets bind package approval, the exact Ticket revision, execution evidence, retained authority, repository evidence, and audited commit. Readback rechecks stored bytes, ownership, digest, current execution evidence, and repository evidence. Stale or superseded packets cannot receive decisions. The Auditor may attribute findings to `implementation`, `governing_package`, or `both`; `needs_revision` creates immutable remediation evidence for fresh-context Planner work without the prior Executor transcript.
