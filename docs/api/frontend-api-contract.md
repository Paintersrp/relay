# Frontend API contract

The Go daemon is the runtime authority for Feature Workspaces, repository targets, Delivery Tickets and revisions, selections, execution packages and approvals, package-linked Runs, execution attempts, audit decisions, remediation evidence, and retained historical records.

Plans and passes remain presentation-only historical endpoints. There is no active Plan or pass creation or mutation path. `POST /api/runs` is retired: package approval is the only active Run-creation path.

## Active workflow routes

| Route | Purpose |
| --- | --- |
| `GET /api/feature-workspaces/{workspaceID}` | Feature Workspace detail |
| `POST /api/feature-workspaces/{workspaceID}/tickets/{ticketID}/revisions` | Publish a Ticket revision |
| `POST /api/delivery-tickets/{ticketID}/approvals` | Approve an exact Ticket revision |
| `POST /api/feature-workspaces/{workspaceID}/tickets/selection` | Create a selection |
| `POST /api/execution-packages` | Prepare a package |
| `GET /api/execution-packages/{packageID}` | Read package guidance |
| `POST /api/execution-packages/{packageID}/approvals` | Approve a package and create its Run |
| `GET /api/runs` | List Runs |
| `GET /api/runs/{runID}` | Run detail |
| `GET /api/runs/{runID}/specification` | Historical specification review surface |
| `POST /api/runs/{runID}/attempts` | Start package execution |
| `GET /api/runs/{runID}/attempts` | List attempts |
| `GET /api/runs/{runID}/attempts/{attemptID}` | Attempt detail |
| `POST /api/runs/{runID}/attempts/{attemptID}/cancel` | Cancel attempt |
| `POST /api/runs/{runID}/attempts/{attemptID}/reconcile` | Reconcile attempt |
| `GET /api/runs/{runID}/audit/status` | Audit status |
| `POST /api/runs/{runID}/audit/prepare` | Prepare audit packet |
| `GET /api/runs/{runID}/audit/packet` | Read current audit packet |
| `POST /api/runs/{runID}/audit/decision` | Record an audit decision from the active web Run workbench's manual fallback |

The Auditor role app is the normal handoff for recording audit decisions. The active web Run workbench also provides this HTTP route as an exceptional manual fallback; both paths reach the same audit decision domain owner. The web client does not implement a separate decision authority.

## Run response DTOs

`GET /api/runs` returns `{ "items": [...], "count": number }`. Each item uses these JSON properties:

```json
{
  "runId": "...",
  "featureSlug": "...",
  "repoTarget": "...",
  "status": "...",
  "stage": "...",
  "branch": "...",
  "baseCommit": "...",
  "canonicalSha256": "...",
  "createdAt": "...",
  "updatedAt": "..."
}
```

Optional summary properties are `planId`, `passId`, `passNumber`, `project`, `remediatesRunId`, `completedAt`, `latestAttempt`, `currentPacket`, and `latestDecision`. Plan and pass references are historical compatibility data, not active execution authority.

`GET /api/runs/{runID}` returns `run`, `attempts`, and `artifacts`. An attempt has `attemptId`, `attemptNumber`, `adapter`, `model`, `status`, `createdAt`, `startedAt`, `finishedAt`, `cancellationRequestedAt`, and `artifacts`; detailed attempt responses also contain `runId`, `result`, `liveStdout`, `liveStderr`, `liveStdoutTruncated`, `liveStderrTruncated`, `liveStdoutBytes`, and `liveStderrBytes`.

`GET /api/runs/{runID}/specification` is a retained historical-review transport name. It currently returns `run`, `executionSpec`, and `executorBrief`, with optional `plan`, `pass`, and `remediatesRunId`. These response property names and identifiers are compatibility-only DTO data. They do not mean an active package-linked Run has authored Execution Spec or Executor Brief authority, or Plan/pass authority; no active workflow creates or depends on a newly authored Execution Spec or Ticket Design Brief.

Execution admission returns `success` and `preflight`; it may return `run` or `attempt` according to the derived execution mode. Required validation commands come from the immutable ExecutionAssignment. Under adaptive execution, structured results remain in the adaptive attempt's `execution_evidence`; under deterministic-complete execution, no adaptive attempt or attempt-owned evidence artifact exists and assigned commands are represented as `not_run` in audit evidence because no adaptive Orchestrator dispatch was required.

## Audit

Audit status returns `runId`, `runStatus`, and where available `currentPacket`, `latestPacket`, and `decision`. Packet metadata uses `auditPacketId`, `implementationActorKind`, `auditedCommit`, `packetSha256`, `status`, `staleReason`, `createdAt`, and `supersededAt`.

Packet preparation binds the approved package authority, exact Ticket revision, execution evidence, retained authority, repository evidence, and audited commit. Packet readback rechecks stored bytes, ownership, digest, current execution evidence, and repository evidence. Stale or superseded packets cannot receive decisions. `needs_revision` produces immutable remediation evidence; remediation returns through the standard Ticket revision, approval, package, and Run lifecycle.

## MCP boundary

The public connector routes are `POST /mcp/wayfinder`, `POST /mcp/planner`, and `POST /mcp/auditor`. Each role app exposes only its fixed compiled generated catalog. The `/mcp/v1/...` values are internal route identities, not public URLs; request data cannot select another role or internal route.
