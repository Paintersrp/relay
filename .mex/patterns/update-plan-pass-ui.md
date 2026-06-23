---
name: update-plan-pass-ui
description: Update managed plan registry/detail/pass detail UI while keeping API contracts and visual-state helpers aligned.
triggers:
  - plan UI
  - pass detail
  - RelayPlan
  - associated runs UI
edges:
  - target: context/relay-web.md
    condition: for route and component map
  - target: context/managed-plans.md
    condition: for plan/pass/run association concepts and fields
  - target: patterns/add-plan-api-read-field.md
    condition: when the UI needs a backend field that does not exist yet
last_updated: 2026-06-23
---

# Update Plan Pass UI

## Context

Plan UI spans `apps/web/src/routes/plans/*`, `apps/web/src/components/relay`, and `apps/web/src/features/relay-plans`. Prefer existing relay components and visual-state helpers.

## Steps

1. Confirm required fields already exist in `features/relay-plans/types.ts` and backend JSON.
2. Update query usage through `plansListQueryOptions`, `planDetailQueryOptions`, or `planPassDetailQueryOptions`.
3. Put reusable rendering in `components/relay`; keep route files focused on data loading and state surfaces.
4. Add or update visual-state helper tests when display state or labels change.
5. Keep links route-safe using TanStack `Link` params for `/plans/$planId` and `/plans/$planId/passes/$passId`.

## Gotchas

- Do not edit `routeTree.gen.ts`.
- Do not render future-pass work as current scope.
- Associated runs are optional; empty arrays should render gracefully.

## Verify

- `cd apps/web && npm run typecheck`
- `cd apps/web && npm run test`
- `cd apps/web && npm run build` for broader UI changes.

## Update Scaffold

- [ ] Update `context/relay-web.md` if route/component ownership changes.
- [ ] Update `context/managed-plans.md` if durable UI surfaces or fields change.
