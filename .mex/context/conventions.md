---
name: conventions
description: Repo-specific naming, generated-file, API/store, UI, and validation conventions.
triggers:
  - convention
  - naming
  - generated
  - validation
  - pattern
edges:
  - target: context/relay-api.md
    condition: when backend or database conventions apply
  - target: context/relay-web.md
    condition: when frontend route/component conventions apply
  - target: context/pipeline-contracts.md
    condition: when handoff or artifact conventions apply
last_updated: 2026-06-23
---

# Conventions

## Naming

- Backend packages are short lower-case domain names under `internal/` (`plans`, `intake`, `pipeline`, `auditor`, `validationrunner`).
- Database tables/columns use snake_case; JSON API fields use camelCase on read models and contract-prescribed snake_case inside planner plan JSON.
- Managed plan route params use `planId` and `passId`; frontend API types mirror backend response names such as `PlanAPIPass.associatedRunIds`.
- React components in `apps/web/src/components/relay` use PascalCase; visual-state helpers use lower camel case plus `.test.ts`.
- SQL query files live in `internal/db/queries/*.sql`; migrations are numbered `internal/db/migrations/000NN_*.sql`.

## Structure

- Source SQL changes go in `internal/db/queries` and migrations; generated sqlc output in `internal/store/generated` follows regeneration only.
- Plan validation/submission behavior belongs in `internal/plans`; request/response mapping belongs in `internal/api`; optional run association lookup belongs in `internal/intake`.
- `apps/web/src/routes` owns route files; reusable UI lives in `apps/web/src/components/relay`; API clients/types/query options live in `apps/web/src/features/*`.
- Root `web/src` scripts are for the legacy/utility bundle; do not mix them with `apps/web` React code.
- `relay-contracts/` files should be referenced, not copied wholesale into `.mex` or implementation docs.

## Patterns

Add read-side plan fields through every layer:
```text
DB/query/store if needed -> internal/api response struct + mapper -> apps/web feature type/API tests -> component render/tests.
```

Change SQL through source and regeneration:
```text
internal/db/migrations/*.sql or internal/db/queries/*.sql -> sqlc generate -> use generated methods from store/API code.
```

For UI state labels, prefer tested helper functions:
```text
relayPlanVisualState.ts / runStageVisualState.ts / route-specific visual-state helpers -> component renders the derived state.
```

## Verify Checklist

- [ ] No direct edits to generated `routeTree.gen.ts`, `*_templ.go`, or `internal/store/generated/*` without changing source and regenerating.
- [ ] Standalone runs still work when plan/pass IDs are omitted.
- [ ] Planner/pass scope is not broadened beyond the selected pass.
- [ ] Backend API response structs, mappers, frontend TypeScript types, and UI tests stay aligned.
- [ ] Relevant Go, web, smoke, and mex checks are run or explicitly reported as not run.
