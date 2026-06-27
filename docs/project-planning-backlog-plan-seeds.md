# Project Planning Backlog and Plan Seeds

## Purpose

This document describes the current runtime behavior of the Project Planning Backlog and Plan Seeds in Relay. It covers the field schema, status model, HTTP routes, MCP actions, capture boundaries, and linkage semantics.

> [!IMPORTANT]
> **Scope**: The Plan Seed runtime is implemented. It supports project-scoped capture, lifecycle actions, read-only planning context, and draft attempt registration. It does not automatically submit managed plans or create runs from seed bridge actions.

---

## Product framing

The Project Planning Backlog introduces a lightweight intake stage to capture planning ideas before they are expanded into detailed, multi-pass structures:

```text
Project Planning Backlog → Plan Seeds → Draft Plan Attempt → Managed Plan
```

By separating quick capturing from intent expansion and final submission, Relay ensures that:
1. Ideas can be quickly stored without needing to run context retrieval.
2. Draft plans can be generated and reviewed inside draft attempts without polluting the active plans table.
3. The operator remains the gatekeeper, explicitly confirming transitions.

---

## Plan Seed definition

A Plan Seed is a project-scoped future plan-level todo with quick context. It functions as a lightweight backlog record to capture potential project milestones, tasks, or features. Plan Seeds store operator input and metadata until they are selected for plan expansion.

---

## Field contract

Plan Seed records persisted in the database use the following schema. All database operations and API/MCP interactions are project-scoped.

| Field Name | Type | Description |
|---|---|---|
| `seed_id` | UUID/String | Stable unique identifier for the Plan Seed. |
| `project_id` | UUID/String | The ID of the owning Relay project. |
| `title` | String | A short, human-readable title describing the backlog idea. |
| `quick_context` | String | Bounded operator-supplied context for future planning. Must not store unbounded chat transcripts or secrets. |
| `constraints_json` | JSON | Structured list of constraints for future plan authoring. |
| `non_goals_json` | JSON | Structured list of non-goals for future plan authoring. |
| `tags_json` | JSON | Structured tags/labels for filtering and organization. |
| `priority` | String | Operator-controlled priority label (e.g., `normal`, `high`). Defaults to `normal`. |
| `status` | String | Persisted status of the seed. Must be exactly one of `captured`, `planned`, `deferred`, `rejected`. |
| `source_type` | String | Creation origin. Must map to exactly one of `manual`, `chat`, `mcp`. |
| `source_label` | String | Optional bounded label indicating provenance (e.g., chat session ID, operator name). |
| `source_ref_id` | String | Optional bounded reference ID to a safe external reference or caller context. |
| `plan_attempt_id` | UUID/String | Set only when a draft plan attempt is created from this seed. Nullable. |
| `managed_plan_id` | UUID/String | Set by internal service linkage when the draft plan attempt is submitted and becomes a managed plan. Nullable. |
| `planned_at` | Timestamp | Timestamp indicating when the draft plan attempt was successfully registered. Nullable. |
| `defer_reason` | String | Bounded reason for deferring the seed. Nullable. |
| `reject_reason` | String | Bounded reason for rejecting the seed. Nullable. |
| `created_at` | Timestamp | Timestamp of seed creation. |
| `updated_at` | Timestamp | Timestamp of the last seed update. |

---

## Status model

The Plan Seed model consists of exactly four statuses.
Legacy planning and ready\_for\_planning are not valid v1 states.

| Status | Meaning |
|---|---|
| `captured` | Stored project-level future planning idea with quick context. |
| `planned` | A draft plan attempt was created from the seed; this is the planned/done state. |
| `deferred` | Valid but postponed. |
| `rejected` | Closed as not worth planning. |

### Detailed State Contract Table

| Stored Backend Status | API Workflow State | Display State/Label | Frontend Step Derivation | Allowed Actions | Next States |
|---|---|---|---|---|---|
| `captured` | Stored seed exists and is available for future planning | Captured | Show as active backlog idea | read, update mutable capture fields, defer, reject, later create one draft plan attempt | `planned`, `deferred`, `rejected` |
| `deferred` | Valid seed is postponed | Deferred | Show as postponed backlog idea with defer reason | read, reject | `rejected` |
| `planned` | A draft plan attempt was created from the seed | Planned / Done | Show linkage to `plan_attempt_id` and optionally `managed_plan_id` | read, internal managed-plan linkage update only | terminal except linkage updates |
| `rejected` | Seed closed as not worth planning | Rejected | Show as closed/rejected with reject reason | read only | terminal unless later pass adds reopening |

The `deferred → captured` relaunch transition is supported by the backend service (`RelaunchDeferredPlanSeed`) but is not exposed through current HTTP/MCP/UI surfaces.

---

## Status transitions

Plan Seed state transitions are strictly restricted to the following paths:

- `captured → planned` (Transitions on successful creation of a draft plan attempt)
- `captured → deferred` (Transitions on defer action)
- `captured → rejected` (Transitions on reject action)
- `deferred → captured` (Relaunched/restored to the active planning backlog; internal service only in this pass)
- `deferred → rejected` (Rejected from a deferred state)
- `planned` is terminal except for internal linkage updates (e.g. setting `managed_plan_id`).
- `rejected` is terminal unless a later implementation pass explicitly adds reopening capability.

---

## Creation sources

Plan Seeds can be created from three distinct sources in v1:
- **Manual UI capture**: An operator types a seed title and details into the React workbench. This maps to `source_type = manual`.
- **Chat creation**: An agent or chat client registers a seed from conversation text. This maps to `source_type = chat`.
- **MCP/API creation**: An external service or MCP client invokes the creation tool. This maps to `source_type = mcp`.

---

## Capture boundaries

Plan Seed capture is designed to be lightweight backlog storage. The capture process has strict side-effect boundaries:
- Plan Seed capture does not create intent packets.
- Plan Seed capture does not create managed plan or pass records.
- Plan Seed capture does not create runs.
- Plan Seed capture does not create source snapshots or context packets.

Furthermore, audit, drift-review, and refactor outputs do not create Plan Seeds in v1.

---

## HTTP route contract

All HTTP endpoints are project-scoped. The Go backend enforces project validation before delegating to the handler.

| HTTP Method & Route | Target Backend Handler | Frontend Client/Fetch Path | Request/Response Contract |
|---|---|---|---|
| `GET /api/projects/{projectId}/plan-seeds` | `internal/api/projects` list handler | `apps/web/src/features/relay-projects/api.ts::getPlanSeeds` | Lists project-scoped seeds; supports `status` and `limit` filters; includes linkage fields when present. |
| `POST /api/projects/{projectId}/plan-seeds` | `internal/api/projects` create handler | `apps/web/src/features/relay-projects/api.ts::createPlanSeed` | Creates one seed with required `title`, `quick_context`; optional `priority`, `tags`, `constraints`, `non_goals`, `source_label`; status starts `captured`. |
| `GET /api/projects/{projectId}/plan-seeds/{seedId}` | `internal/api/projects` get handler | `apps/web/src/features/relay-projects/api.ts::getPlanSeed` | Reads exactly one seed by project and seed ID; rejects unknown project/seed or cross-project mismatch. |
| `POST /api/projects/{projectId}/plan-seeds/{seedId}/update` | `internal/api/projects` update handler | `apps/web/src/features/relay-projects/api.ts::updatePlanSeed` | Updates mutable capture fields only; does not change status. |
| `POST /api/projects/{projectId}/plan-seeds/{seedId}/defer` | `internal/api/projects` defer handler | `apps/web/src/features/relay-projects/api.ts::deferPlanSeed` | Sets status `deferred`; optional `defer_reason`. |
| `POST /api/projects/{projectId}/plan-seeds/{seedId}/reject` | `internal/api/projects` reject handler | `apps/web/src/features/relay-projects/api.ts::rejectPlanSeed` | Sets status `rejected`; optional `reject_reason`; does not delete seed. |
| `GET /api/projects/{projectId}/plan-seeds/{seedId}/planning-context` | `internal/api/projects` planning context handler | `apps/web/src/features/relay-projects/api.ts::getPlanSeedPlanningContext` | Retrieval-only; returns bounded planner-facing context; no mutation or generation side effects. |
| `POST /api/projects/{projectId}/plan-seeds/{seedId}/plan-attempts` | `internal/api/projects` attempt bridge handler | `apps/web/src/features/relay-projects/api.ts::createPlanAttemptFromSeed` | Registers exactly one draft Plan of Passes attempt from a reviewed/generated artifact; does not submit managed plan or create runs. |

---

## MCP action contract

All MCP actions are defined under the local-operator profile. They require strict schema validation (`additionalProperties: false`) and project scope verification.

| MCP Action | Purpose | Required Inputs | Optional Inputs | Side Effects | Forbidden Side Effects |
|---|---|---|---|---|---|
| `create_plan_seed` | Create one project-scoped Plan Seed from chat/MCP input | `project_id`, `title`, `quick_context` | `priority`, `tags`, `constraints`, `non_goals`, `source_label` | Creates seed with `status=captured` | No intent packet, no plan attempt, no managed plan, no run |
| `list_plan_seeds` | List Plan Seeds for one project | `project_id` | `status`, `limit` | None | Must not mutate seed state |
| `get_plan_seed` | Read one seed | `project_id`, `seed_id` | none | None | Must not mutate seed state |
| `update_plan_seed` | Update mutable capture fields before planned/rejected terminal behavior | `project_id`, `seed_id` | `title`, `quick_context`, `priority`, `tags`, `constraints`, `non_goals` | Updates mutable fields when status allows | Must not change status |
| `defer_plan_seed` | Mark a valid seed as postponed | `project_id`, `seed_id` | `defer_reason` | Sets `status=deferred` | Must not create plan attempts |
| `reject_plan_seed` | Close a seed as not worth planning | `project_id`, `seed_id` | `reject_reason` | Sets `status=rejected` | Must not delete seed |
| `get_plan_seed_planning_context` | Return bounded planner-facing context for one seed | `project_id`, `seed_id` | none | None | No plan generation, mutation, intent packet, plan attempt, managed plan submission, or run creation |
| `create_plan_attempt_from_seed` | Register exactly one draft Plan of Passes attempt from reviewed/generated plan artifact | `project_id`, `seed_id`, `planner_pass_plan_json`, `source_artifact_path` | `drift_review_mode`, `model_tier` | Creates draft plan attempt; creates intent packet during attempt creation; links `plan_attempt_id`; marks seed `planned` after success | No managed plan submission, pass/run records, executor dispatch, or public result-linking action |

> [!IMPORTANT]
> **No Public Link Action**: There is no public `link_plan_seed_result` MCP action in v1. Linkage of plans to seeds is managed internally.

---

## Planning-context and draft-attempt boundary

The operations bridging Plan Seeds to the planning loop are strictly separated:

1. **Retrieval**: `get_plan_seed_planning_context` is retrieval-only. It gathers project facts, inventory, and constraints to return a bounded planning context. It has no side effects, does not generate plans, and does not alter database state.
2. **Draft Attempt Registration**: `create_plan_attempt_from_seed` registers exactly one draft Plan of Passes attempt for one seed. If the draft plan attempt infrastructure is unavailable, it returns a structured blocker and leaves the seed status unchanged.
3. **No Auto-Submit**: Attempt creation does not submit managed plans, create runs, or dispatch executors. It simply stages a draft Plan Attempt for human review.

---

## Linkage semantics

Linkage connects Plan Seeds forward into the execution pipeline:
- A seed is linked to exactly one draft plan attempt via `plan_attempt_id` when the attempt is successfully registered. This updates `planned_at` and sets the seed status to `planned`.
- Plan Seed linkage to `plan_attempt_id` and `managed_plan_id` is internal service behavior after appropriate later-pass actions succeed. When the draft attempt is approved and submitted, the resulting managed plan ID is associated with `managed_plan_id` in the database.

---

## UI placement contract

The Plan Seeds user interface belongs in the Project Details page of the React workbench in v1:
- It is positioned near the Refactor Backlog entry point and the managed plans panel (`RelayProjectPlansPanel`).
- It does not reside in a standalone workspace.
- The UI panel shows the backlog items sorted by status (`captured`, `deferred`) and optional `priority` ordering.
- Linkages to `plan_attempt_id` and `managed_plan_id` are shown when present.

---

## Security and redaction expectations

- **Secret Blocking**: Obvious secrets, bearer tokens, or sensitive credentials within `title`, `quick_context`, or metadata fields block seed creation/update or are rejected outright if they cannot be safely redacted.
- **Context Boundedness**: `quick_context` must contain only bounded, high-level developer prompt descriptions. Storing unbounded chat transcripts or complete environment listings is prohibited.

---

## Explicit v1 non-goals

The following are explicitly out of scope for the v1 Plan Seed implementation:
- Audit, drift-review, and refactor outputs do not create Plan Seeds.
- No automatic generation of plans or draft attempts at seed capture time.
- No public `link_plan_seed_result` MCP action.
- No managed plan submission or run creation from seed bridge actions.

---

## Validation checklist

The current release-hardening checklist for Plan Seeds:
- [x] Database schema matches the field contract, with correct types and nullability.
- [x] Status updates validate against the strict state transition paths.
- [x] Route paths and handler logic scope everything by `projectId` and validate projects before mutations.
- [x] MCP actions register under the local-operator profile with strict schema validation.
- [x] Secret detection triggers and blocks creation/updates when credentials appear in input fields.
- [x] `get_plan_seed_planning_context` is read-only and does not mutate intent/plan/run tables.
- [x] `create_plan_attempt_from_seed` creates exactly one intent packet and one draft plan attempt, zero managed plans/passes/runs.
- [x] Duplicate attempt creation is blocked without creating extra rows.
- [x] Deferred and rejected seeds cannot create draft attempts.
- [x] `make plan-seed-smoke` / `go run ./cmd/plan-seed-smoke` passes in an isolated temp store.
- [x] `scripts/release-smoke.sh` runs focused Plan Seed checks before the broader suite.
