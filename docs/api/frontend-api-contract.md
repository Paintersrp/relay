# Relay Frontend API Contract

## Runtime boundary

The Go daemon is the only backend authority for Projects, repository targets, Plans, Runs, execution attempts, artifacts, audit packets, audit decisions, and lifecycle transitions.

The React workbench uses this JSON API only. The backend exposes no handoff intake, prepare, brief-approval, project-scoped planning, source/context, seed, Plan Attempt, refactor-backlog, or legacy closeout routes.

Default development addresses:

- Backend: `http://localhost:8080`
- Frontend: `http://localhost:3000`
- Frontend backend configuration: `VITE_RELAY_API_BASE_URL`
- Backend frontend configuration: `RELAY_WEB_BASE_URL`

All IDs in paths are Relay string identities such as `project-*`, `note-*`, `plan-*`, `pass-*`, `run-*`, `attempt-*`, and `artifact-*`. Numeric legacy IDs are not accepted.

## Workflow stages

Every Run exposes its exact durable `status` and one derived `stage`.

| Run status | Stage |
| --- | --- |
| `created` | `specification` |
| `setup_ready` | `specification` |
| `executing` | `execute` |
| `execution_failed` | `execute` |
| `cancelled` | `execute` |
| `validating` | `audit` |
| `validation_failed` | `audit` |
| `audit_ready` | `audit` |
| `needs_revision` | `audit` |
| `completed` | `audit` |

Unknown or legacy statuses are errors. They do not fall back to another stage.

The non-API route `GET /runs/{runId}` redirects to:

```text
${RELAY_WEB_BASE_URL}/runs/{runId}/{stage}
```

A newly created Run therefore opens at `/specification`.

## List bounds

- Project, Plan, and Run lists default to 50 items.
- Project, Plan, and Run lists are capped at 100 items.
- A Run detail exposes at most the 50 most recent execution attempts.
- Artifact content defaults to 64 KiB and is capped at 64 KiB per request.
- Artifact bodies are never embedded in Plan, Run, execution-attempt, or audit metadata responses.

## Global repository targets

### `GET /api/repositories`

Returns:

```json
{
  "items": [
    {
      "repoTarget": "relay",
      "localPath": "D:\\Code\\relay",
      "createdAt": "2026-07-06T00:00:00Z",
      "updatedAt": "2026-07-06T00:00:00Z"
    }
  ],
  "count": 1
}
```

### `POST /api/repositories`

Request:

```json
{
  "repoTarget": "relay",
  "localPath": "D:\\Code\\relay"
}
```

Repository targets are globally unique case-insensitive keys. The local path must resolve to an existing directory.

### `GET /api/repositories/{repoTarget}`

Returns one repository target or `404 NOT_FOUND`.

Project repository routes create or remove non-owning references to these global targets; they never copy repository configuration.


## Projects

Projects are lightweight organizational records. They contain attached Plan references, non-owning repository references, and bounded Notes.

### `GET /api/projects`

Optional query parameters:

- `status=active|archived`
- `limit=1..100`

Returns compact Projects with repository and note counts.

### `POST /api/projects`

Request:

```json
{
  "name": "Relay",
  "description": "Primary Relay workflow work."
}
```

Returns `201 Created` with the created active Project.

### `GET /api/projects/{projectId}`

Returns one Project with repository references and bounded Notes.

### `PATCH /api/projects/{projectId}`

Updates Project name, description, or status. Archived Projects remain readable.

### `POST /api/projects/{projectId}/repositories`

Request:

```json
{
  "repoTarget": "relay"
}
```

Attaches an existing global repository target to the Project as a non-owning case-insensitive reference.

### `DELETE /api/projects/{projectId}/repositories/{repoTarget}`

Removes the non-owning Project repository reference.

### `POST /api/projects/{projectId}/notes`

Request:

```json
{
  "title": "Future cleanup",
  "body": "Review remaining legacy cleanup."
}
```

Creates an open Project Note.

### `PATCH /api/projects/{projectId}/notes/{noteId}`

Updates title, body, or `status=open|done`.

## Canonical browser validation

### `POST /api/canonical-artifacts/validate`

Request:

```json
{
  "fileName": "feature.plan.json",
  "canonicalContent": "{...}\n"
}
```

Returns the computed hash, artifact kind, bounded compiler diagnostics, and notices without creating database rows or artifact files. Validation does not accept an expected hash. Canonical basenames and mutation `expectedSha256` values are validated exactly and are not whitespace-normalized.

## Plans

### `GET /api/plans`

Optional query parameters:

- `status=active|completed`
- `projectId=project-*`
- `limit=1..100`

Returns:

```json
{
  "items": [
    {
      "planId": "plan-*",
      "project": {"projectId": "project-*", "name": "Relay", "status": "active"},
      "featureSlug": "feature",
      "status": "active",
      "canonicalSha256": "64 lowercase hex characters",
      "createdAt": "ISO-8601",
      "updatedAt": "ISO-8601",
      "passCount": 3,
      "completedPassCount": 1,
      "inProgressPassCount": 1,
      "plannedPassCount": 1,
      "currentPassId": "pass-*"
    }
  ],
  "count": 1
}
```

### `POST /api/plans`

Request:

```json
{
  "projectId": "project-*",
  "fileName": "feature.plan.json",
  "canonicalContent": "{...}\n",
  "expectedSha256": "64 lowercase hex characters"
}
```

The exact UTF-8 bytes of `canonicalContent` are hash-checked and compiled through the same application service used by MCP. The destination Project must be active. Project metadata is stored separately from canonical Plan JSON.

### `GET /api/plans/{planId}`

Returns:

- bounded Plan summary;
- ordered repository targets;
- ordered passes;
- dependency pass IDs;
- associated Run summaries;
- Plan artifact metadata with explicit `contentUrl`.

Canonical Plan JSON and rendered Plan Markdown are retrieved only through the artifact content endpoint.

### `PATCH /api/plans/{planId}/project`

Request:

```json
{
  "projectId": "project-*"
}
```

Moves the Plan atomically to an active Project without changing canonical artifacts, passes, Runs, or audit evidence.

### `GET /api/plans/{planId}/passes/{passId}`

Returns one pass with dependency IDs and associated Run summaries.

No Project review settings, Plan Attempt, legacy Plan Seed orchestration, next-pass-work, or next-audit-work HTTP route exists.

## Runs

### `GET /api/runs`

Optional query parameters:

- `status=<exact workflow Run status>`
- `planId=plan-*`
- `passId=pass-*` only when `planId` is also supplied
- `limit=1..100`

Returns:

```json
{
  "items": [
    {
      "runId": "run-*",
      "featureSlug": "feature",
      "repoTarget": "relay",
      "status": "setup_ready",
      "stage": "specification",
      "branch": "main",
      "baseCommit": "40 lowercase hex characters",
      "canonicalSha256": "64 lowercase hex characters",
      "planId": "plan-*",
      "passId": "pass-*",
      "passNumber": 1,
      "project": {"projectId": "project-*", "name": "Relay", "status": "active"},
      "remediatesRunId": "run-*",
      "createdAt": "ISO-8601",
      "updatedAt": "ISO-8601",
      "latestAttempt": {},
      "currentPacket": {},
      "latestDecision": {}
    }
  ],
  "count": 1
}
```

Optional properties are omitted when not applicable.

### `POST /api/runs`

Request:

```json
{
  "fileName": "feature.pass-1.execution-spec.json",
  "canonicalContent": "{...}\n",
  "expectedSha256": "64 lowercase hex characters",
  "planId": "plan-*",
  "passNumber": 1,
  "remediatesRunId": "run-*"
}
```

`planId` and `passNumber` are supplied together for a Managed Run, whose `fileName` must end in the matching `.pass-<number>.execution-spec.json` qualifier. A Standalone Run omits both association fields and must use the unqualified `feature.execution-spec.json` form. Missing, malformed, mismatched, or Standalone pass qualifiers block before persistence. Run creation never accepts or stores a direct Project association.

### `GET /api/runs/{runId}`

Returns:

```json
{
  "run": {},
  "attempts": [],
  "artifacts": []
}
```

Run-owned artifacts and attempt-owned evidence are metadata only. Every artifact includes an explicit `/api/artifacts/{artifactId}/content` URL.

### `GET /api/runs/{runId}/specification`

Returns the Specification review projection:

```json
{
  "run": {},
  "executionSpec": {
    "artifactId": "artifact-*",
    "kind": "execution_spec",
    "contentUrl": "/api/artifacts/artifact-*/content"
  },
  "executorBrief": {
    "artifactId": "artifact-*",
    "kind": "executor_brief",
    "contentUrl": "/api/artifacts/artifact-*/content"
  },
  "plan": {
    "planId": "plan-*",
    "featureSlug": "feature",
    "status": "active"
  },
  "pass": {
    "passId": "pass-*",
    "number": 1,
    "name": "Pass name",
    "repoTarget": "relay",
    "status": "in_progress"
  },
  "remediatesRunId": "run-*"
}
```

Plan, pass, and remediation properties are omitted for unmanaged ordinary Runs.

## Execution attempts

### `POST /api/runs/{runId}/attempts`

Request:

```json
{
  "adapter": "codex",
  "model": "model-id"
}
```

Performs read-only repository and executor preflight before creating the immutable attempt. Returns `202 Accepted`.

### `GET /api/runs/{runId}/attempts`

Returns:

```json
{
  "items": [],
  "count": 0
}
```

At most 50 attempts are returned.

### `GET /api/runs/{runId}/attempts/{attemptId}`

Returns the bounded attempt projection, artifact metadata, and bounded live stdout/stderr tails. Runtime owner IDs, process identities, and command previews are excluded.

### `POST /api/runs/{runId}/attempts/{attemptId}/cancel`

Requests cancellation for the matching Run and attempt.

### `POST /api/runs/{runId}/attempts/{attemptId}/reconcile`

Reopens durable process ownership and resolves cleanup-pending execution state.

The former `/api/workflow/runs/...` prefix does not exist.

## Artifacts

### `GET /api/artifacts/{artifactId}`

Returns metadata only:

```json
{
  "artifactId": "artifact-*",
  "ownerType": "run",
  "kind": "executor_brief",
  "mediaType": "text/markdown",
  "sha256": "64 lowercase hex characters",
  "sizeBytes": 123,
  "createdAt": "ISO-8601",
  "contentUrl": "/api/artifacts/artifact-*/content"
}
```

Filesystem paths are not returned.

### `GET /api/artifacts/{artifactId}/content`

Optional query parameters:

- `offset`, default `0`
- `limit`, default and maximum `65536`

The daemon verifies the artifact's exact size and SHA-256 before returning content.

Response:

```json
{
  "artifact": {},
  "offset": 0,
  "byteCount": 65536,
  "encoding": "utf-8",
  "content": "bounded content",
  "truncated": true,
  "nextOffset": 65536
}
```

`encoding` is `utf-8` when the returned bytes are valid UTF-8 and `base64` otherwise. `nextOffset` is omitted when no further bytes remain.

## Audit

### `POST /api/runs/{runId}/audit/prepare`

Request:

```json
{
  "auditedCommit": "40 lowercase hex characters"
}
```

Validates the exact clean local commit range and creates or returns the current immutable audit packet.

### `GET /api/runs/{runId}/audit/status`

Returns current packet, latest packet, and recorded decision metadata using camel-case fields. Packet bodies remain available only through their artifact `contentUrl`.

Audit decisions are recorded through the canonical Auditor MCP tool and require exact packet identity plus explicit operator confirmation.

The former local-audit, project-audit, audit submit, approve, request-revision, commit-message, and close routes do not exist.

## MCP

`POST /mcp` remains the canonical ChatGPT-facing JSON-RPC endpoint.

Canonical tool inventories remain profile-specific:

- Planner: `validate_artifact`, `list_projects`, `submit_plan`, `get_plan`, `create_run`
- Auditor: `validate_artifact`, `create_run`, `get_audit_packet`, `record_audit_decision`
- Local operator: the union of Planner and Auditor tools, including `list_projects`

`submit_plan` requires external `project_id`. `validate_artifact` and `create_run` remain Project-independent. `get_plan` returns compact Project metadata.

Successful `create_run` output includes:

```json
{
  "review_url": "http://localhost:3000/runs/run-*/specification"
}
```

The URL uses `RELAY_WEB_BASE_URL` when configured.

## Removed routes

The daemon returns `404 NOT_FOUND` for every obsolete workflow route, including:

- `/api/runs/{legacyNumericId}/approve-intake`
- `/api/runs/{legacyNumericId}/prepare`
- `/api/runs/{legacyNumericId}/render-brief`
- `/api/runs/{legacyNumericId}/approve-brief`
- `/api/runs/{legacyNumericId}/execute`
- `/api/runs/{legacyNumericId}/validate`
- `/api/audits/local...`
- `/api/workflow/runs...`
- `/handoffs`
- `/handoffs/new`
- `/settings/repos...`

No removed route redirects or translates into the new workflow.
