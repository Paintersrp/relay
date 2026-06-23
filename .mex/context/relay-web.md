---
name: relay-web
description: apps/web route, component, feature API, and visual-state map.
triggers:
  - apps/web
  - React
  - TanStack
  - component
  - route
edges:
  - target: context/managed-plans.md
    condition: when UI touches plan/pass/run association
  - target: context/relay-api.md
    condition: when frontend types must align with backend JSON
  - target: patterns/update-plan-pass-ui.md
    condition: when modifying plan registry/detail/pass detail UI
last_updated: 2026-06-23
---

# Relay Web

## Route Map

- `apps/web/src/routes/__root.tsx` - app shell, React Query provider, TanStack devtools, metadata.
- `apps/web/src/routes/index.tsx` - workbench landing/default route.
- `apps/web/src/routes/plans/index.tsx` - `/plans` registry.
- `apps/web/src/routes/plans/new.tsx` - `/plans/new` submission.
- `apps/web/src/routes/plans/$planId.tsx` - plan detail.
- `apps/web/src/routes/plans/$planId.passes.$passId.tsx` - pass detail route for `/plans/$planId/passes/$passId`.
- `apps/web/src/routes/runs/*` - run registry, creation, run layout, and stage routes: intake, prepare, execute, audit.

## Feature Areas

- `apps/web/src/features/relay-plans` - plan API client, query options, TypeScript contracts, API tests.
- `apps/web/src/features/relay-runs` - run API client, query options, types, validation gate logic, tests.
- `apps/web/src/components/relay` - shared workbench layout, plan components, run components, stage primitives, state surfaces, status helpers.
- `apps/web/src/components/ui` - shadcn/Radix primitives and base UI components.

## Plan Components

- `RelayPlansRegistry` renders the plan list.
- `RelayPlanSubmissionWorkbench` handles plan validation/submission UX.
- `RelayPlanDetail` and `RelayPlanPassTimeline` render plan detail and pass sequencing.
- `RelayPlanPassDetail` renders one selected pass, including scope, dependencies, and associated run context.
- `relayPlanVisualState`, `relayPlanSubmissionState`, and `relayPlanPassDetailState` contain visual-state derivation with colocated tests.

## Run Workbench Components

- `RunWorkbenchLayout`, `RunSummaryHeader`, `RunStepper`, and `RunStagePrimitives` frame stage pages.
- `RunPlanContext` surfaces optional plan/pass context for runs.
- Route-specific helpers such as `runCompileRenderVisualState`, `runExecuteVisualState`, and `runAuditVisualState` keep display state testable.

## Conventions

- Keep API types in `features/*/types.ts` aligned with Go JSON response structs.
- Add or update Vitest coverage for visual-state helpers and API client behavior when field contracts change.
- Do not edit `routeTree.gen.ts`; change route files and let tooling regenerate.
- Use React Query query options from feature packages rather than ad hoc fetches in components.
