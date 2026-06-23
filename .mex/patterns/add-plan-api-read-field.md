---
name: add-plan-api-read-field
description: Add a managed plan/pass read field through backend mapping, frontend types, and tests.
triggers:
  - plan API field
  - associatedRunIds
  - PlanAPIPass
  - PlanAPIReadPlan
edges:
  - target: context/managed-plans.md
    condition: to confirm plan/pass/run concepts and current fields
  - target: context/relay-api.md
    condition: for handler, mapper, store, and smoke-test touchpoints
  - target: context/relay-web.md
    condition: for TypeScript contract and UI consumers
last_updated: 2026-06-23
---

# Add Plan API Read Field

## Context

Read `context/managed-plans.md`, then inspect the exact Go response struct and frontend type before editing. Treat Go JSON output and `apps/web` types as a contract pair.

## Steps

1. Identify whether the field is derived from existing store rows or needs SQL/migration support.
2. If SQL is needed, follow `patterns/add-sqlc-query.md` first.
3. Update the relevant Go response struct in `internal/api/api.go`.
4. Update the mapper (`mapPlanToAPI`, `mapPlanPassToAPI`, `mapRunToPlanAPIRunSummary`, or `buildPlanAPIReadPlan`).
5. Update or add API tests/smoke checks; `cmd/plan-api-smoke` is the managed-plan read-side harness.
6. Update `apps/web/src/features/relay-plans/types.ts` and any API tests in `apps/web/src/features/relay-plans`.
7. Update consuming components only after the type/API layer is aligned.

## Gotchas

- Do not add fields only to the frontend type; confirm the backend actually returns them.
- Keep standalone runs valid when plan/pass association is absent.
- Associated run fields should summarize navigation context, not duplicate entire run payloads.

## Verify

- `go test ./...`
- `make plan-api-smoke`
- `cd apps/web && npm run typecheck`
- `cd apps/web && npm run test`

## Update Scaffold

- [ ] Update `context/managed-plans.md` if the durable read contract changes.
- [ ] Update this pattern if a new cross-layer step becomes required.
