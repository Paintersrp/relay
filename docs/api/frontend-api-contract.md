# Relay Frontend-Facing API Contract

This document specifies the typed JSON API contract between the TanStack Start React frontend and the Go orchestration backend.

## Purpose

The purpose of this contract is to define the exact JSON boundary between the frontend and the backend for the Relay workbench. This allows the frontend to run independently using standardized type models and mock data, while ensuring seamless integration once the Go backend implements the matching JSON routes.

## Runtime Model

Relay is partitioned into two runtime environments:

1. **Go Daemon (Backend)**: 
   - Runs on `http://localhost:8080` (or configured port).
   - Serves as the single source of truth for orchestration, SQLite storage, execution status, validation engines, and disk-based artifact storage.
   - Exposes JSON endpoints matching this contract.
2. **TanStack Start (React Frontend)**:
   - Runs on `http://localhost:3000` (development server).
   - Handles the React UI and coordinates state transitions by querying the Go daemon's API.
   - Configures the Go backend URL using the environment variable `VITE_RELAY_API_BASE_URL` (defaults to `http://localhost:8080`).

## Contract Rules

- **Go Daemon Orchestration**: Go remains the sole backend orchestration engine. Pipeline execution logic, validation runners, and git worktrees must not move into TanStack Start server functions.
- **No Templ/Htmx UI Calls**: The new React UI must interact only with the JSON API endpoints specified below. It must not call old HTML-rendering routes or templ/htmx form action handlers.
- **No Old Route Naming Schemes**: Do not reuse the old templ/htmx form action names (`/approve_intake`, `/execute_handoff`, etc.) or layout page names as API endpoint paths. The JSON API uses clean rest-style routes (e.g. `/api/runs/{id}/approve-intake`).

## Workflow Status Contract

`RelayRun.status` is the **canonical workflow state** used for action gating. It is set to the exact store status value and must not be collapsed into a broad display bucket. Display/helper fields are derived separately:

| Field | Type | Purpose |
|-------|------|---------|
| `status` | `RelayRunStatus` | Canonical workflow state for action gating |
| `activeStep` | `RelayRunStep` | Current pipeline step (`intake`, `prepare`, `execute`, `audit`) |
| `lifecycleState` | `RelayRunLifecycleState` | Lifecycle bucket (`intake`, `prepare`, `execute`, `audit`, `completed`, `failed`) |
| `state` | `string` | Human-readable display state label |
| `statusSeverity` | `RelayRunStatusSeverity` | UI badge severity (`neutral`, `info`, `success`, `warning`, `danger`) |
| `latestExecutionStatus` | `string` (optional) | Latest agent execution phase (`starting`, `running`, `completed`, `failed`, etc.) — separate from canonical status; `""` when no execution has been recorded |

### Canonical Status Values

The following canonical statuses are emitted by `GET /api/runs` and `GET /api/runs/{id}`:

- `draft` — Run created but not yet submitted
- `needs_cleanup` — Run has uncommitted or dirty state
- `intake_received` — Handoff received, no validation warnings
- `intake_needs_review` — Handoff received with warnings, awaiting review
- `validated` — Legacy: intake validated
- `approved_for_prepare` — Intake approved; compilation allowed
- `packet_validated` — Compilation succeeded, packet valid
- `packet_validation_failed` — Compilation failed validation
- `repair_validated` — Repair succeeded
- `brief_ready_for_review` — Executor brief rendered, awaiting approval
- `approved_for_executor` — Brief approved; executor dispatch allowed
- `executor_dispatched` — Executor dispatched and running
- `executor_done` — Executor completed successfully
- `executor_blocked` — Executor encountered a blocking error
- `audit_ready` — Audit packet generated, ready for review
- `audit_ready_for_review` — Legacy: audit ready (htmx fallback)
- `revision_required` — Revision requested, audit must be regenerated
- `accepted` — Audit approved
- `accepted_with_warnings` — Audit approved with warnings
- `completed` — Run closed
- `blocked` — Run blocked (intake or general)
- Legacy agent states: `agent_done`, `agent_blocked`, `agent_result_needs_review`, `validation_passed`, `validation_failed_accepted`, `validation_failed`

Display fields (`state`, `activeStep`, `lifecycleState`, `statusSeverity`) are derived from the canonical status and must not be used for action gating. Frontend action gating must use `status` only.

The canonical final state is `completed`. No mutating actions are available in this state.

## Shared Models

### RelayRun

```json
{
  "id": "string",
  "name": "string",
  "repo": "string",
  "branch": "string",
  "activeStep": "intake | prepare | execute | audit",
  "status": "<canonical workflow state>",
  "lifecycleState": "intake | prepare | execute | audit | completed | failed",
  "createdAt": "string (ISO-8601)",
  "updatedAt": "string (ISO-8601)",
  "summary": "string",
  "model": "string",
  "riskLevel": "low | medium | high | critical",
  "validation": {
    "errors": 0,
    "warnings": 0,
    "passed": 0,
    "issues": []
  },
  "artifacts": [],
  "latestEvents": [],
  "statusSeverity": "neutral | info | success | warning | danger",
  "state": "string",
  "latestExecutionStatus": "string (optional)"
}
```

### RelayArtifact

```json
{
  "id": "string",
  "label": "string",
  "path": "string",
  "kind": "prompt | handoff | result | audit | validation | diff",
  "sizeHint": "string (optional)",
  "createdAt": "string (ISO-8601, optional)"
}
```

### RelayRunEvent

```json
{
  "id": "string",
  "runId": "string",
  "kind": "log | status_change | artifact_created | validation_run | step_transition",
  "message": "string",
  "createdAt": "string (ISO-8601)"
}
```

### RelayApiErrorShape

```json
{
  "error": "string",
  "message": "string",
  "code": "string (optional)",
  "details": {}
}
```

---

### Audit State Machine

The audit lifecycle follows these state transitions:

```
executor_done / executor_blocked
  → POST /audit (GenerateAudit)
  → audit_ready
      → POST /audit/approve → accepted / accepted_with_warnings
          → POST /audit/prepare-commit-message (artifact only, no git)
          → POST /audit/close → completed (final state, read-only)
      → POST /audit/request-revision → revision_required
          → POST /audit (regenerate from executor terminal states) → audit_ready
```

**Rules:**
- `audit_ready` and `audit_ready_for_review` are equivalent for action gating.
- `revision_required` blocks all review actions until audit is regenerated.
- `completed` is the canonical final state. All actions are blocked in this state.
- ApproveAudit transitions to `accepted` or `accepted_with_warnings` only; never closes the run.
- PrepareCommitMessage is gated to `accepted`/`accepted_with_warnings` only.
- CloseRun is gated to `accepted`/`accepted_with_warnings` only; transitions to `completed`.
- No audit action performs git add, commit, push, merge, reset, checkout, or worktree mutation.

## Endpoints

### 1. GET `/api/runs`
- **Purpose**: Retrieve a list of all active and historic runs.
- **Request Body**: None
- **Response Body**: `RelayRun[]`
- **Fallback Policy**: Allowed. Falls back to static mock run lists if the endpoint is unavailable, unimplemented, or the daemon is offline.
- **Expected Error Behavior**: Throws a descriptive error if the response contains invalid or malformed JSON.

### 2. GET `/api/runs/{id}`
- **Purpose**: Fetch details of a single run by ID.
- **Request Body**: None
- **Expected status values for key pipeline states**:
  - `approved_for_prepare` — Intake approved, ready to compile (activeStep: `prepare`, lifecycleState: `prepare`)
  - `packet_validated` — Compilation succeeded (activeStep: `prepare`, lifecycleState: `prepare`)
  - `approved_for_executor` — Brief approved, ready to dispatch executor (activeStep: `execute`, lifecycleState: `execute`)
  - `executor_done` — Executor completed (activeStep: `execute`, lifecycleState: `execute`)
- **Response Body**: `RelayRun`
- **Fallback Policy**: Allowed. Falls back to the corresponding static mock run if the endpoint is 404, unimplemented, or the daemon is offline.
- **Expected Error Behavior**: Throws a descriptive error if the response contains invalid or malformed JSON.

### 3. GET `/api/runs/{id}/artifacts`
- **Purpose**: Get all artifacts associated with the specified run.
- **Request Body**: None
- **Response Body**: `RelayArtifact[]`
- **Fallback Policy**: Allowed. Falls back to mock artifacts if the endpoint is 404/501 or the daemon is offline.
- **Expected Error Behavior**: Throws a descriptive error if the response contains invalid or malformed JSON.

### 4. GET `/api/runs/{id}/events`
- **Purpose**: Stream or fetch all events and log messages for the specified run.
- **Request Body**: None
- **Response Body**: `RelayRunEvent[]`
- **Fallback Policy**: Allowed. Falls back to mock events if the endpoint is 404/501 or the daemon is offline.
- **Expected Error Behavior**: Throws a descriptive error if the response contains invalid or malformed JSON.

### 5. POST `/api/intake/planner-handoff`
- **Purpose**: Submit a new implementation handoff packet to begin the pipeline.
- **Request Body**:
  ```json
  {
    "repo": "string",
    "branch": "string",
    "handoffPath": "string",
    "packetId": "string (optional)",
    "name": "string (optional)"
  }
  ```
- **Response Body**:
  ```json
  {
    "success": true,
    "runId": "string",
    "status": "intake_received | intake_needs_review",
    "lifecycleState": "intake",
    "createdAt": "string (ISO-8601)"
  }
  ```
- **Fallback Policy**: **Strictly Forbidden**. Never return mock success.
- **Expected Error Behavior**: Throws a typed `RelayApiError` on missing endpoint, daemon offline, non-2xx status, or invalid response.

### 6. POST `/api/runs/{id}/approve-intake`
- **Purpose**: Approve, request revision, or block the parsed metadata and git preflights of the intake step.
- **Request Body**:
  ```json
  {
    "action": "approve | needs_revision | blocked",
    "notes": "string (optional)",
    "overrides": {
      "model": "string (optional)",
      "repo": "string (optional)",
      "branch": "string (optional)",
      "validationCommands": "string (optional)"
    }
  }
  ```
- **Response Body**:
  ```json
  {
    "success": true,
    "runId": "string",
    "status": "approved_for_prepare | intake_needs_review | blocked",
    "lifecycleState": "prepare | intake | failed",
    "updatedAt": "string (ISO-8601)"
  }
  ```
- **Fallback Policy**: **Strictly Forbidden**. Never return mock success.
- **Expected Error Behavior**: Throws a typed `RelayApiError` on missing endpoint, daemon offline, non-2xx status, or invalid response.

### 7. POST `/api/runs/{id}/prepare`
- **Purpose**: Trigger compilation of the instruction brief (calls existing Pass 6 compiler service).
- **Note**: Requires run status `approved_for_prepare`. Returns `422 Unprocessable Entity` with validation report on compile failure.
- **Request Body**: None
- **Response Body** (success):
  ```json
  {
    "success": true,
    "runId": "string",
    "packetId": "string",
    "status": "packet_validated | packet_validation_failed",
    "lifecycleState": "prepare",
    "validationReport": {}
  }
  ```
- **Response Body** (validation failure, 422):
  ```json
  {
    "success": false,
    "runId": "string",
    "packetId": "string",
    "issues": ["string"],
    "validationReport": {}
  }
  ```
- **Fallback Policy**: **Strictly Forbidden**. Never return mock success.
- **Expected Error Behavior**: Throws a typed `RelayApiError` on missing endpoint, daemon offline, non-2xx status, or invalid response.

### 8. POST `/api/runs/{id}/render-brief`
- **Purpose**: Render the executor brief from the compiled canonical packet (calls existing Pass 7 renderer service).
- **Note**: Requires run status `packet_validated` or `repair_validated`.
- **Request Body**: None
- **Response Body**:
  ```json
  {
    "success": true,
    "runId": "string",
    "status": "brief_ready_for_review",
    "lifecycleState": "prepare",
    "updatedAt": "string (ISO-8601)"
  }
  ```
- **Fallback Policy**: **Strictly Forbidden**. Never return mock success.
- **Expected Error Behavior**: Throws a typed `RelayApiError` on missing endpoint, daemon offline, non-2xx status, or invalid response.

### 9. POST `/api/runs/{id}/approve-brief`
- **Purpose**: Approve the compiled brief and authorize execution.
- **Note**: Requires run status `brief_ready_for_review` and a passing brief validation report. Advances to `approved_for_executor`.
- **Request Body**:
  ```json
  {
    "action": "approve | needs_revision",
    "notes": "string (optional)"
  }
  ```
- **Response Body**:
  ```json
  {
    "success": true,
    "runId": "string",
    "status": "approved_for_executor",
    "lifecycleState": "execute",
    "updatedAt": "string (ISO-8601)"
  }
  ```
- **Fallback Policy**: **Strictly Forbidden**. Never return mock success.
- **Expected Error Behavior**: Throws a typed `RelayApiError` on missing endpoint, daemon offline, non-2xx status, or invalid response.

### 10. POST `/api/runs/{id}/execute`
- **Purpose**: Start the repository agent execution loop. Dispatches only from `approved_for_executor` run status. Reads `executor_brief.md` artifact as the sole executor task instruction.
- **Request Body**:
  ```json
  {
    "action": "start | cancel | recover"
  }
  ```
- **Success Response Body** (start):
  ```json
  {
    "success": true,
    "runId": "string",
    "status": "executor_dispatched",
    "lifecycleState": "execute",
    "updatedAt": "string (ISO-8601)"
  }
  ```
- **Error Response Body** (dispatch blocked, 422):
  ```json
  {
    "success": false,
    "runId": "string",
    "error": "string describing why dispatch was blocked"
  }
  ```
- **Notes**:
  - `start` dispatches only when run status is `approved_for_executor`.
  - Dispatch reads `executor_brief.md` and sends only that rendered brief text to the executor.
  - Missing/empty brief, missing workspace config, missing selected model, or duplicate active session blocks dispatch.
  - Lifecycle states: `approved_for_executor` → `executor_dispatched` → `executor_done` | `executor_blocked`.
  - Output artifacts: `executor_stdout.txt`, `executor_stderr.txt`, `command_log.txt`, `executor_result.txt`.
  - `cancel` and `recover` return `501 Not Implemented` when unavailable.
- **Fallback Policy**: **Strictly Forbidden**. Never return mock success.
- **Expected Error Behavior**: Throws a typed `RelayApiError` on missing endpoint, daemon offline, non-2xx status, or invalid response.

### 11. POST `/api/runs/{id}/audit`
- **Purpose**: Generate the audit packet from executor evidence. Collects run artifacts (executor result, validation output, changed files, git diff) and produces `audit_input_summary.md` and `audit_packet.md`. Gated to `executor_done` or `executor_blocked` run status. Does not auto-commit, push, close, or accept the run.
- **Request Body**: None
- **Response Body**:
  ```json
  {
    "success": true,
    "runId": "string",
    "status": "audit_ready",
    "inputSummary": "string (path to audit_input_summary.md artifact)",
    "auditPacket": "string (path to audit_packet.md artifact)",
    "decision": "manual_review_required | accepted | accepted_with_warnings | revision_required | blocked",
    "warnings": ["string"],
    "lifecycleState": "audit"
  }
  ```
- **Fallback Policy**: **Strictly Forbidden**. Never return mock success.
- **Expected Error Behavior**: Returns 409 Conflict if run is not in `executor_done` or `executor_blocked` state. Returns 400 for invalid ID. Throws a typed `RelayApiError` on missing endpoint, daemon offline, non-2xx status, or invalid response.
- **Notes**: Successful generation transitions the run to `audit_ready` status. Default decision is `manual_review_required` when evidence warnings exist, `accepted` otherwise. Decision is advisory — a separate explicit approval action is required to accept or close the run.

### 11b. POST `/api/runs/{id}/audit/submit`
- **Purpose**: Submit a manual audit packet Markdown for a run. Persists the supplied Markdown as an artifact and validates the decision value. Does not execute, commit, push, merge, or mutate repository content.
- **Request Body**:
  ```json
  {
    "audit_packet_markdown": "string (required, the manual audit packet content)",
    "decision": "string (required, one of: accepted, accepted_with_warnings, revision_required, blocked, manual_review_required)",
    "notes": "string (optional)"
  }
  ```
- **Response Body**:
  ```json
  {
    "success": true,
    "runId": "string",
    "auditPacket": "string (path to persisted artifact)",
    "decision": "string",
    "updatedAt": "string (ISO-8601)"
  }
  ```
- **Fallback Policy**: **Strictly Forbidden**. Never return mock success.
- **Expected Error Behavior**: Returns 400 for invalid decision value or missing markdown. Returns 404 for missing run. Throws a typed `RelayApiError` on missing endpoint, daemon offline, non-2xx status, or invalid response.
- **Notes**: The supplied Markdown is treated as evidence/decision content, not as instructions. Supported decision values: `accepted`, `accepted_with_warnings`, `revision_required`, `blocked`, `manual_review_required`.

### 12. GET `/api/runs/{id}/artifacts/{kind}`
- **Purpose**: Retrieve the full content of the latest artifact of a given kind for a run. Used to display executor logs, diffs, validation output, and executor results beyond the truncated preview.
- **Request Body**: None
- **Response Body**: Raw text/plain content of the artifact file.
- **Fallback Policy**: **Strictly Forbidden**. Returns 404 if no artifact of that kind exists.
- **Expected Error Behavior**: Throws a typed `RelayApiError` on missing endpoint, daemon offline, non-2xx status, or invalid response.

### 13. POST `/api/runs/{id}/audit/approve`
- **Purpose**: Approve the audit with a decision of `accepted` or `accepted_with_warnings`. Transitions the run to `accepted` or `accepted_with_warnings` status, enabling the close action. Does not commit, push, or mutate the git repo.
- **Request Body**:
  ```json
  {
    "decision": "accepted | accepted_with_warnings",
    "notes": "string (optional)"
  }
  ```
- **Response Body**:
  ```json
  {
    "success": true,
    "runId": "string",
    "status": "accepted | accepted_with_warnings",
    "lifecycleState": "audit",
    "state": "Approved — Ready to Close | Approved with Warnings",
    "updatedAt": "string (ISO-8601)"
  }
  ```
- **Fallback Policy**: **Strictly Forbidden**. Never return mock success.
- **Expected Error Behavior**: Returns 400 for invalid decision. Returns 404 for missing run. Returns 409 if run is not in `audit_ready` or `audit_ready_for_review` state. Throws a typed `RelayApiError` on missing endpoint, daemon offline, non-2xx status, or invalid response.
- **Notes**: This action only updates Relay run state. No git mutation occurs.

### 14. POST `/api/runs/{id}/audit/request-revision`
- **Purpose**: Request revision for an audit. Records the revision request as an event artifact and transitions the run status to `revision_required`. Does not commit, push, or mutate repository content.
- **Request Body**:
  ```json
  {
    "notes": "string (optional)",
    "reason": "string (optional)"
  }
  ```
- **Response Body**:
  ```json
  {
    "success": true,
    "runId": "string",
    "status": "revision_required",
    "lifecycleState": "audit",
    "updatedAt": "string (ISO-8601)"
  }
  ```
- **Fallback Policy**: **Strictly Forbidden**. Never return mock success.
- **Expected Error Behavior**: Returns 404 for missing run. Returns 409 if run is not in `audit_ready` or `audit_ready_for_review` state. Throws a typed `RelayApiError` on missing endpoint, daemon offline, non-2xx status, or invalid response.
- **Notes**: Transitions the run to `revision_required`. An `audit_revision_request.md` artifact is persisted with revision details for durable evidence. After revision, audit must be regenerated from executor terminal states. No git mutation occurs.

### 15. POST `/api/runs/{id}/audit/prepare-commit-message`
- **Purpose**: Prepare a suggested commit message artifact for the run. Writes a `commit_message.txt` artifact with the run title and changed file summary. Gated to `accepted` or `accepted_with_warnings` status only. Does not commit, push, stage, or mutate any repo files.
- **Request Body**: None
- **Response Body**:
  ```json
  {
    "success": true,
    "runId": "string",
    "commitMessage": "string",
    "artifactPath": "string",
    "artifactKind": "commit_message_text"
  }
  ```
- **Fallback Policy**: **Strictly Forbidden**. Never return mock success.
- **Expected Error Behavior**: Returns 404 for missing run. Returns 409 if run is not in `accepted` or `accepted_with_warnings` state. Throws a typed `RelayApiError` on missing endpoint, daemon offline, non-2xx status, or invalid response.
- **Notes**: Only creates a suggested message artifact. No git commit, git push, git add, staging, merge, or repo mutation occurs.

### 16. POST `/api/runs/{id}/audit/close`
- **Purpose**: Close a run after audit approval. Transitions the run to `completed` status. Gated to `accepted` or `accepted_with_warnings` status only. Preserves all artifacts and evidence. Does not commit, push, or mutate the git repo.
- **Request Body**: None
- **Response Body**:
  ```json
  {
    "success": true,
    "runId": "string",
    "status": "completed",
    "lifecycleState": "completed",
    "updatedAt": "string (ISO-8601)"
  }
  ```
- **Fallback Policy**: **Strictly Forbidden**. Never return mock success.
- **Expected Error Behavior**: Returns 409 if run is not in `accepted` or `accepted_with_warnings` state. Returns 404 for missing run. Throws a typed `RelayApiError` on missing endpoint, daemon offline, non-2xx status, or invalid response.
- **Notes**: Closing updates Relay run state only. No git commit, push, or repo mutation occurs.

### 17. POST `/api/runs/{id}/approve-closeout`
- **Purpose**: (Legacy) Accept the audit results and commit/close the run.
- **Request Body**:
  ```json
  {
    "action": "approve | reject",
    "notes": "string (optional)"
  }
  ```
- **Response Body**:
  ```json
  {
    "success": true,
    "runId": "string",
    "status": "completed",
    "lifecycleState": "completed",
    "updatedAt": "string (ISO-8601)"
  }
  ```
- **Fallback Policy**: **Strictly Forbidden**. Never return mock success.
- **Expected Error Behavior**: Throws a typed `RelayApiError` on missing endpoint, daemon offline, non-2xx status, or invalid response.

---

## Read Fallback Policy

To support local-first operations and design-led prototyping:
- Read (GET) calls **MUST** check for endpoint availability.
- Fallback to mock data is **strictly restricted** to:
  1. The endpoint returning `404 Not Found` (meaning endpoint not yet implemented by Go daemon).
  2. The endpoint returning `501 Not Implemented`.
  3. The Go daemon process being offline or unreachable (`Connection Refused`/`TypeError: Fetch Failed`).
- If the Go daemon is reachable and returns a `200 OK` but the payload is malformed or invalid JSON, the client **must throw** an exception immediately to prevent debugging silence.

## Mutation Failure Policy

To ensure high-fidelity interactions:
- Mutation (POST) endpoints **must never return fake success**.
- If a mutation call is triggered and the daemon is offline, or the endpoint returns a non-2xx status (e.g. `404`, `500`, `400`), the client **must throw a descriptive error** that identifies both the HTTP method and endpoint.
- These failures must propagate to the UI to notify the user of real network or server issues.

## Future Backend Adapter Notes

When the Go daemon is extended with these endpoints:
1. It must support CORS headers allowing requests from `http://localhost:3000`.
2. It should handle standard JSON request payloads and return clean JSON error shapes matching `RelayApiErrorShape` when errors occur.
3. Long-running events should be exposed via Server-Sent Events (SSE) at `/api/runs/{id}/events/stream`.
