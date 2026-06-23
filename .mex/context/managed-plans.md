---
name: managed-plans
description: Managed plan/pass/run association concepts, fields, routes, and smoke-test touchpoints.
triggers:
  - managed plan
  - plan pass
  - associated runs
  - completionReady
edges:
  - target: context/relay-api.md
    condition: when changing backend plan APIs or store methods
  - target: context/relay-web.md
    condition: when changing plan registry/detail/pass detail UI
  - target: patterns/add-plan-api-read-field.md
    condition: when adding or changing plan API read fields
  - target: patterns/update-plan-pass-ui.md
    condition: when changing managed plan UI surfaces
last_updated: 2026-06-23
---

# Managed Plans

## Concepts

- A managed plan stores a Planner pass plan JSON submission as a `plans` row plus ordered `plan_passes` rows.
- A run may be standalone or associated to a plan and optionally one pass through nullable `runs.plan_row_id` and `runs.plan_pass_row_id`.
- `pass_id` requires `plan_id` during association resolution; omitting both preserves standalone run intake.
- Plan completion readiness is computed from pass statuses by `internal/plans` lifecycle code and surfaced as `completionReady`.

## Current Read Fields

`PlanAPIPass` includes:
- `id`, `planRowId`, `passId`, `sequence`, `name`, `goal`
- `intendedExecutionScope`, `nonGoals`, `dependencies`, `status`
- `associatedRunIds`
- `associatedRuns` with `id`, `title`, `status`, `lifecycleState`, `activeStep`, `workbenchPath`, `createdAt`, `updatedAt`
- `createdAt`, `updatedAt`

`PlanAPIReadPlan` embeds the base plan and currently exposes `passCount` and `completionReady`; the frontend type also has optional count/current/next fields that may be future-facing unless backed by API code.

## UI Surfaces

- `/plans` - plan registry via `RelayPlansRegistry`.
- `/plans/new` - plan submission workbench via `RelayPlanSubmissionWorkbench`.
- `/plans/$planId` - plan detail/timeline via `RelayPlanDetail` and `RelayPlanPassTimeline`.
- `/plans/$planId/passes/$passId` - pass detail via `RelayPlanPassDetail`.
- `/runs/new` and `/runs/$runId/*` - run creation and workbench routes can show plan context through relay run data/components such as `RunPlanContext`.

## Backend Touchpoints

- `internal/plans/types.go` defines submitted planner pass plan input shape and validation issue codes.
- `internal/plans/service.go` validates and persists plans/passes transactionally.
- `internal/intake/association.go` resolves optional `plan_id`/`pass_id` into nullable row IDs.
- `internal/api/api.go` maps plan/pass/store rows to JSON responses and exposes plan endpoints.
- `internal/store/db.go` exposes store wrappers over sqlc-generated queries.
- `internal/db/migrations/00006_create_plans_and_plan_passes.sql` and `00007_add_run_plan_pass_association.sql` define plan/pass/run tables and association fields.
- `cmd/plan-api-smoke` exercises validate, submit, list, detail, pass detail, associated runs, and completion readiness without touching production data.
