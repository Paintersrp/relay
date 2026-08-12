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
| `POST /api/feature-workspaces/{workspaceID}/program-members` | Prepare a Program Dispatch member |
| `GET /api/feature-workspaces/{workspaceID}/program-members` | List prepared Program Dispatch members |
| `POST /api/feature-workspaces/{workspaceID}/program-members/{memberID}/cancel` | Cancel a prepared member |
| `POST /api/feature-workspaces/{workspaceID}/program-dispatches` | Create the immutable Program dispatch |
| `GET /api/feature-workspaces/{workspaceID}/program-dispatches/{dispatchID}` | Read one immutable Program dispatch |
| `GET /api/feature-workspaces/{workspaceID}/program-dispatches/{dispatchID}/handoff` | Read the canonical Program Orchestrator handoff |
| `POST /api/feature-workspaces/{workspaceID}/program-dispatches/{dispatchID}/result` | Record terminal member results of a dispatched member set |
| `POST /api/feature-workspaces/{workspaceID}/program-dispatches/{dispatchID}/integration-assignments` | Generate the one immutable Integration Assignment for an exact subset |
| `GET /api/feature-workspaces/{workspaceID}/program-dispatches/{dispatchID}/integration-assignments/{assignmentID}` | Read one immutable Integration Assignment |
| `POST /api/feature-workspaces/{workspaceID}/program-dispatches/{dispatchID}/integration-assignments/{assignmentID}/merge-results` | Admit the external Merge result for an Assignment |
| `GET /api/feature-workspaces/{workspaceID}/program-dispatches/{dispatchID}/integration-assignments/{assignmentID}/merge-results` | Read the admitted Merge result |
| `POST /api/feature-workspaces/{workspaceID}/program-dispatches/{dispatchID}/integration-assignments/{assignmentID}/verification` | Run Relay verification of the admitted Merge result |
| `GET /api/feature-workspaces/{workspaceID}/program-dispatches/{dispatchID}/integration-assignments/{assignmentID}/verification` | Read the recorded Relay verification |
| `GET /api/feature-workspaces/{workspaceID}/program-dispatches/{dispatchID}/integration-assignments/{assignmentID}/failure` | Read the immutable failed-verification evidence |

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

## Program dispatch and integration

The program routes above are the Feature Workspace program section surface: prepare one eligible approved current package at a time, create the immutable dispatch of a common-repository member set, copy the canonical Program Orchestrator handoff, record terminal member results, then integrate the accepted constituents through exact immutable Integration Assignments.

### Program dispatch

`POST /program-members` takes `{ "packageId", "expectedVersion" }` and returns the prepared member (`ID`, `PackageID`, `RunID`, `AssignmentArtifactID`, `RepoTarget`, `Branch`, `BaseCommit`, `State`, `TicketRevisionRowID`). `POST /program-dispatches` takes `{ "expectedVersion", "memberIds" }` (at least two members, one common repository/branch/base) and returns the immutable dispatch with its members. `POST .../result` takes `{ "expectedVersion", "members", "laterIntegrationRisks" }`; each member result is either `done` with a result branch and 40-hex branch head SHA or `blocked` with a blocker reason. Recording results transitions the dispatch to `reported`. The handoff endpoint returns the exact raw canonical handoff payload the browser copies verbatim; it is never reconstructed client-side.

### Standalone vs Program audit outcome

A standalone accepted isolated audit records the ordinary completion of the audited Ticket revision through the audit decision. A Program-bound accepted isolated audit is **eligibility only**: when every exact recorded fact holds (current approved Ticket revision, accepted commit and pushed branch matching the recorded dispatch result, selected package identity, executed authority lineage, exact dispatch repository basis), a durable integration-eligibility record is created. It never completes the Ticket revision, satisfies a dependency, or advances workspace completion on its own. The ordinary completed outcome of a Program-bound constituent's Ticket revision is recorded only by Relay verification of the integrated result.

### Integration Assignments

`POST .../integration-assignments` takes `{ "expectedVersion", "memberIds" }` and generates the one immutable Assignment for an exact nonempty subset of the dispatch's eligible constituents. Subset admission requires every selected constituent to be integration-eligible with exact recorded facts still current, and the dependency closure of both selected and omitted members must not require a missing Program member (Ticket-carried Shared Design constraints included). A missing, stale, or mismatched fact blocks the whole Assignment; no partial subset is emitted. The response is the Assignment envelope (`AssignmentID`, `DispatchID`, `WorkspaceID`, `RepoTarget`, `Branch`, `BaseCommit`, `Status`, `ContentSHA256`) with the exact transport `Document`: `schema_version`, `assignment`, `constituents` (each binding `member_id`, `ticket_id`, `ticket_revision`, `accepted_commit`, `pushed_branch`, `package_id`, `run_id`, `execution_assignment`, `audit_decision_id`, `eligibility_id`, `shared_design`, `validation_commands`, `required_evidence`), `combined_validation`, and `required_evidence`. Assignment statuses are `generated`, `admitted`, `verified`, and `failed`.

### Merge-owned combined validation

The Assignment's `combined_validation` and `required_evidence` are the exact bound commands and obligations the external Merge must execute against the integrated result. `POST .../merge-results` admits the one external Merge result: `{ "expectedVersion", "integratedCommit", "preservationIdentity", "conflictResolution", "conflictEvidence", "validations", "evidence" }`. `conflictResolution` is factual runtime evidence: `clean` requires empty conflict evidence, `mechanically_resolved` requires `mechanically_resolved:<integratedCommit>`, and `material_conflict` remains blocked. The admitted outcomes must be exactly the bound combined validation commands and required evidence — same count, same order, and identical `command`/`expected` and `kind`/`obligation` identities — with a `passed` or `failed` status and outcome evidence per item. The admitted result is immutable evidence.

### Relay evidence verification, no rerun

`POST .../verification` runs Relay's post-Merge verification of the admitted result. It never reruns the combined validation and never re-audits an accepted constituent: it re-verifies the exact bound authority facts and confirms every recorded validation and evidence outcome passed. A successful pass records the ordinary completed outcome of each bound constituent whose Ticket revision is still current, through the existing satisfaction mechanism; omitted constituents never advance and stale bound constituents are skipped. A failed verification records immutable failure evidence and creates no completion.

### Retry, failure, and completion

A failed Assignment is never patched or reused; retry always generates a fresh Assignment from the same recorded facts (the failed Assignment's constituents become eligible again). `GET .../failure` returns the recorded failure evidence (`VerificationID`, `AssignmentID`, `DispatchID`, `FailureReason`); a passed verification or the absence of verification is not a failure and returns not found. The operator sees the ordinary completed outcome only after Relay verification passes.

## MCP boundary

The public connector routes are `POST /mcp/wayfinder`, `POST /mcp/planner`, and `POST /mcp/auditor`. Each role app exposes only its fixed compiled generated catalog. The `/mcp/v1/...` values are internal route identities, not public URLs; request data cannot select another role or internal route.
