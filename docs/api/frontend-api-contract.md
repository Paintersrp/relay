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

## Shared Models

### RelayRun

```json
{
  "id": "string",
  "name": "string",
  "repo": "string",
  "branch": "string",
  "activeStep": "intake | prepare | execute | audit",
  "status": "intake_needs_review | brief_ready_for_review | executor_running | audit_ready_for_review | completed | blocked",
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
  "latestEvents": []
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
    "status": "intake_needs_review",
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
    "status": "brief_ready_for_review | intake_needs_review | blocked",
    "lifecycleState": "prepare | intake",
    "updatedAt": "string (ISO-8601)"
  }
  ```
- **Fallback Policy**: **Strictly Forbidden**. Never return mock success.
- **Expected Error Behavior**: Throws a typed `RelayApiError` on missing endpoint, daemon offline, non-2xx status, or invalid response.

### 7. POST `/api/runs/{id}/prepare`
- **Purpose**: Trigger compilation of the instruction brief and git environment preparation.
- **Note**: This endpoint is not implemented in Pass 6.
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

### 8. POST `/api/runs/{id}/approve-brief`
- **Purpose**: Approve the compiled brief and authorize execution.
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
    "status": "executor_running",
    "lifecycleState": "execute",
    "updatedAt": "string (ISO-8601)"
  }
  ```
- **Fallback Policy**: **Strictly Forbidden**. Never return mock success.
- **Expected Error Behavior**: Throws a typed `RelayApiError` on missing endpoint, daemon offline, non-2xx status, or invalid response.

### 9. POST `/api/runs/{id}/execute`
- **Purpose**: Start the repository agent execution loop.
- **Request Body**: None
- **Response Body**:
  ```json
  {
    "success": true,
    "runId": "string",
    "status": "executor_running",
    "lifecycleState": "execute",
    "updatedAt": "string (ISO-8601)"
  }
  ```
- **Fallback Policy**: **Strictly Forbidden**. Never return mock success.
- **Expected Error Behavior**: Throws a typed `RelayApiError` on missing endpoint, daemon offline, non-2xx status, or invalid response.

### 10. POST `/api/runs/{id}/audit`
- **Purpose**: Request generation of the final audit packet and validation check execution.
- **Request Body**: None
- **Response Body**:
  ```json
  {
    "success": true,
    "runId": "string",
    "status": "audit_ready_for_review",
    "lifecycleState": "audit",
    "updatedAt": "string (ISO-8601)"
  }
  ```
- **Fallback Policy**: **Strictly Forbidden**. Never return mock success.
- **Expected Error Behavior**: Throws a typed `RelayApiError` on missing endpoint, daemon offline, non-2xx status, or invalid response.

### 11. POST `/api/runs/{id}/approve-closeout`
- **Purpose**: Accept the audit results and commit/close the run.
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
