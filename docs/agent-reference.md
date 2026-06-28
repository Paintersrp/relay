# Relay Agent Reference

This file is a compact repo-orientation reference for coding agents. It is not an authority layer.

Authority order remains:

1. Current user/task instructions
2. Selected Planner handoff or canonical packet, when provided
3. Checked-out source code and tests
4. Canonical relay-contracts GitHub repository for Planner/pipeline behavior
5. This reference and other older repo notes

## System Overview

Planner handoff or managed plan artifact flows into Relay intake/API, then local SQLite metadata and filesystem-backed artifacts. Relay compiles/prepares prompts and canonical packets, executes through configured agent adapters, collects validation/output evidence, and generates audit packets for closeout.

Managed plans add an optional planning layer: a plan has ordered passes, and runs can be standalone or associated to a plan/pass. Association supplies UI context and read-side traceability without making plans mandatory for run creation.

The backend is a local Go daemon using `net/http`, `chi`, `database/sql`, SQLite, migrations, sqlc-generated query wrappers, and services under `internal/*`.

The primary UI is the React/TanStack Start workbench under `apps/web`; root `web/` and templ assets remain for legacy/utility surfaces.

`docs/backend-code-surface-map.md` is retained only as a retired compatibility pointer; it should not be expanded with new manual routing tables.

## Generated Agent References

The default source-backed navigation entry point is `docs/generated/agent-references/index.json`. The readable companion is `docs/generated/agent-references/index.md`.

Agents should use the generated index to locate backend, workflow, storage, MCP, HTTP API, and frontend/backend contract references.

Generated reference outputs:

- `docs/generated/agent-references/backend-surface.json` — Generated backend package, service, handler, symbol, import-edge, and adjacent-test surface reference.
- `docs/generated/agent-references/frontend-backend-contract.json` — Generated frontend/backend contract reference: frontend API clients, query keys, TypeScript contracts, backend HTTP route matches, and backend Go DTO alignment.
- `docs/generated/agent-references/http-api-surface.json` — Generated HTTP/API route surface reference: method, path, handler, source file, and route group from route source files.
- `docs/generated/agent-references/mcp-surface.json` — Generated MCP action registry reference: tool definitions, dispatch handlers, profile gating, mutating vs retrieval-only behavior, and forbidden side effects.
- `docs/generated/agent-references/storage-surface.json` — Generated storage, migration, SQL query, sqlc-boundary, and store-wrapper surface reference.
- `docs/generated/agent-references/workflow-surfaces.json` — Generated Plan v2 workflow, intent packet, drift review, refactor backlog, and work-packet lifecycle surface reference.

Generated references do not override checked-out source code, tests, selected Planner handoffs, canonical packets, Relay DB state, run artifacts, audit evidence, or relay-contracts.

## Key Components

- `cmd/relay` starts the local app.
- `internal/server` wires routes.
- `internal/api/api.go` owns HTTP request parsing, response structs, JSON mapping, and plan/run endpoint behavior.
- `internal/store` and `internal/db` own SQLite access, migrations, sqlc query sources, generated data models, and store wrappers.
- `internal/plans` validates and persists planner pass plans.
- `internal/intake` resolves optional run plan/pass association.
- `internal/compiler`, `internal/pipeline`, `internal/executor`, `internal/auditor`, and `internal/validation*` transform handoffs into executable artifacts, run agents/validation, and collect audit evidence.
- `apps/web/src` provides the React Query plus TanStack Router workbench for plans, passes, runs, execution, validation, artifacts, and audit state.

## Stack

| Area | Current stack |
|---|---|
| Backend runtime | Go module `relay`; HTTP routing through `net/http` and `github.com/go-chi/chi/v5`. |
| Local storage | SQLite through `modernc.org/sqlite`; the store opens the DB with WAL and foreign keys. |
| Data access generation | `sqlc.yaml` config reads query sources from `internal/db/queries`, migrations from `internal/db/migrations`, and generates Go code in `internal/store/generated`. |
| Server-rendered utility surface | `templ` views live under `internal/views`; generated `_templ.go` files are output. |
| Root frontend bundle | `web/src` builds legacy/utility assets into `web/static` using Tailwind, esbuild, htmx, Alpine, TypeScript, and concurrently-run dev scripts. |
| Primary workbench | `apps/web` is the React/TanStack Start workbench using Vite/Vitest and TanStack Router/Query/Table/Virtual/Form. |

## Manifest-Backed Libraries

| Manifest area | Libraries and tools to expect |
|---|---|
| Go module | `github.com/a-h/templ`, `github.com/go-chi/chi/v5`, `modernc.org/sqlite`, and migration tooling including `github.com/pressly/goose/v3`. |
| Root package | `tailwindcss`, `@tailwindcss/cli`, `esbuild`, `typescript`, `htmx.org`, `alpinejs`, and `concurrently`. |
| `apps/web` package | React, React DOM, TanStack Start/Router/Query/Table/Virtual/Form, Vite, Vitest, Radix UI, lucide-react, shadcn-style components, zod, and Tailwind. |
| Validation scripts | Root `npm run build` builds legacy CSS/JS; `npm run build:web` delegates to `apps/web`; `make validate` runs `scripts/validate.sh`. |

## Planner and Pipeline Contracts

Canonical contract repository: `Paintersrp/relay-contracts`.

Fetch current files from that repository when producing Planner handoffs, auditing contract behavior, or discussing current Planner/pipeline rules.

Important contract paths include:

- `agents/knowledge/planner_github_knowledge_manifest.json`
- `agents/instructions/planner_agent_instructions.md`
- `contracts/planner_to_compiler_contract.md`
- `contracts/planner_mcp_run_submission_contract.md`
- `contracts/planner_mcp_plan_submission_contract.md`
- `contracts/planner_pass_plan_contract.md`
- `contracts/pipeline_artifact_model.md`
- `templates/planner_handoff_template.md`
- `schema/planner_handoff_manifest.schema.json`
- `schema/canonical_packet.schema.json`
- `schema/planner_pass_plan.schema.json`
- `policies/artifact_naming_policy.md`
- `policies/pipeline_lifecycle_policy.md`
- `policies/security_redaction_policy.md`
- `policies/human_approval_gate_policy.md`

Do not assume these files exist inside `Paintersrp/relay` unless intentionally vendored or mounted.

Implementation implications:

- Planner handoffs must stay scoped to the selected pass and must not implement future-pass work.
- Plan/pass JSON validation should use contract schemas and preserve contract field names.
- Canonical packets, executor briefs/results, validation reports, repair prompts, and audit packets should follow relay-contracts templates and schemas.
- Large artifact contents remain on disk; Relay stores artifact metadata and paths in SQLite.

## Managed Plans

Concepts:

- A managed plan stores a Planner pass plan JSON submission as a `plans` row plus ordered `plan_passes` rows.
- A run may be standalone or associated to a plan and optionally one pass through nullable `runs.plan_row_id` and `runs.plan_pass_row_id`.
- `pass_id` requires `plan_id` during association resolution; omitting both preserves standalone run intake.
- Plan completion readiness is computed from pass statuses by `internal/plans` lifecycle code and surfaced as `completionReady`.

Current read fields:

`PlanAPIPass` includes:

- `id`, `planRowId`, `passId`, `sequence`, `name`, `goal`
- `intendedExecutionScope`, `nonGoals`, `dependencies`, `status`
- `associatedRunIds`
- `associatedRuns` with `id`, `title`, `status`, `lifecycleState`, `activeStep`, `workbenchPath`, `createdAt`, `updatedAt`
- `createdAt`, `updatedAt`

`PlanAPIReadPlan` embeds the base plan and currently exposes `passCount` and `completionReady`; frontend type fields for optional counts/current/next may be future-facing unless backed by API code.

UI surfaces:

- `/plans` — plan registry via `RelayPlansRegistry`.
- `/plans/new` — plan submission workbench via `RelayPlanSubmissionWorkbench`.
- `/plans/$planId` — plan detail/timeline via `RelayPlanDetail` and `RelayPlanPassTimeline`.
- `/plans/$planId/passes/$passId` — pass detail via `RelayPlanPassDetail`.
- `/runs/new` and `/runs/$runId/*` — run creation and workbench routes can show plan context through run data/components such as `RunPlanContext`.

Backend touchpoints:

- `internal/plans/types.go` defines submitted planner pass plan input shape and validation issue codes.
- `internal/plans/service.go` validates and persists plans/passes transactionally.
- `internal/intake/association.go` resolves optional `plan_id`/`pass_id` into nullable row IDs.
- `internal/api/api.go` maps plan/pass/store rows to JSON responses and exposes plan endpoints.
- `internal/store/db.go` exposes store wrappers over sqlc-generated queries.
- `internal/db/migrations/00006_create_plans_and_plan_passes.sql` and `00007_add_run_plan_pass_association.sql` define plan/pass/run tables and association fields.
- `cmd/plan-api-smoke` exercises validate, submit, list, detail, pass detail, associated runs, and completion readiness without touching production data.

## Relay API Notes

Handler and service shape:

- `cmd/relay/main.go` starts the local app; `internal/server` wires routes.
- `internal/api/api.go` owns HTTP request parsing, response structs, JSON mapping, and plan/run endpoint behavior.
- Domain logic should live in services such as `internal/plans`, `internal/intake`, `internal/pipeline`, `internal/auditor`, and `internal/validationrunner`.
- `internal/store/db.go` wraps sqlc-generated methods with repo-specific defaults and helper semantics.

Store and SQL:

- SQL migrations live in `internal/db/migrations`; query source lives in `internal/db/queries`.
- `sqlc.yaml` generates package `generated` into `internal/store/generated` with JSON tags and empty slices.
- Store aliases generated models, for example `type Plan = generated.Plan`, and exposes higher-level methods such as `CreateRunWithAssociation`.
- Do not hand-edit `internal/store/generated/*`; modify SQL/migrations and run `sqlc generate`.

Plan/run API touchpoints:

- Plan submission validation starts in `APIHandler.ValidatePlan` and `APIHandler.SubmitPlan`, then delegates to `plans.Service`.
- Plan read APIs map store rows through `mapPlanToAPI`, `mapPlanPassToAPI`, `mapRunToPlanAPIRunSummary`, and `buildPlanAPIReadPlan`.
- Associated runs are collected with `ListRunsByPlan` or `ListRunsByPlanPass`; summaries include workbench paths derived from run status.
- Run association resolution for intake is in `internal/intake/association.go`.

Smoke and tests:

- `cmd/plan-api-smoke` creates an isolated temp SQLite store, registers plan routes, submits a synthetic plan, creates an associated run, and verifies read-side fields.
- Prefer `make plan-api-smoke` after managed plan API/store changes.
- Use `go test ./...` for broader Go safety, and add targeted tests near service/API code when behavior changes.

## Relay Web Notes

Route map:

- `apps/web/src/routes/__root.tsx` — app shell, React Query provider, TanStack devtools, metadata.
- `apps/web/src/routes/index.tsx` — workbench landing/default route.
- `apps/web/src/routes/plans/index.tsx` — `/plans` registry.
- `apps/web/src/routes/plans/new.tsx` — `/plans/new` submission.
- `apps/web/src/routes/plans/$planId.tsx` — plan detail.
- `apps/web/src/routes/plans/$planId.passes.$passId.tsx` — pass detail route for `/plans/$planId/passes/$passId`.
- `apps/web/src/routes/runs/*` — run registry, creation, run layout, and stage routes: intake, prepare, execute, audit.

Feature areas:

- `apps/web/src/features/relay-plans` — plan API client, query options, TypeScript contracts, API tests.
- `apps/web/src/features/relay-runs` — run API client, query options, types, validation gate logic, tests.
- `apps/web/src/components/relay` — shared workbench layout, plan components, run components, stage primitives, state surfaces, status helpers.
- `apps/web/src/components/ui` — shadcn/Radix primitives and base UI components.

Plan components:

- `RelayPlansRegistry` renders the plan list.
- `RelayPlanSubmissionWorkbench` handles plan validation/submission UX.
- `RelayPlanDetail` and `RelayPlanPassTimeline` render plan detail and pass sequencing.
- `RelayPlanPassDetail` renders one selected pass, including scope, dependencies, and associated run context.
- `relayPlanVisualState`, `relayPlanSubmissionState`, and `relayPlanPassDetailState` contain visual-state derivation with colocated tests.

Run workbench components:

- `RunWorkbenchLayout`, `RunSummaryHeader`, `RunStepper`, and `RunStagePrimitives` frame stage pages.
- `RunPlanContext` surfaces optional plan/pass context for runs.
- Route-specific helpers such as `runCompileRenderVisualState`, `runExecuteVisualState`, and `runAuditVisualState` keep display state testable.

Conventions:

- Keep API types in `features/*/types.ts` aligned with Go JSON response structs.
- Add or update Vitest coverage for visual-state helpers and API client behavior when field contracts change.
- Do not edit `routeTree.gen.ts`; change route files and let tooling regenerate.
- Use React Query query options from feature packages rather than ad hoc fetches in components.

## Repo Conventions

Naming:

- Backend packages are short lower-case domain names under `internal/`, such as `plans`, `intake`, `pipeline`, `auditor`, and `validationrunner`.
- Database tables/columns use snake_case; JSON API fields use camelCase on read models and contract-prescribed snake_case inside planner plan JSON.
- Managed plan route params use `planId` and `passId`; frontend API types mirror backend response names such as `PlanAPIPass.associatedRunIds`.
- React components in `apps/web/src/components/relay` use PascalCase; visual-state helpers use lower camel case plus `.test.ts`.
- SQL query files live in `internal/db/queries/*.sql`; migrations are numbered `internal/db/migrations/000NN_*.sql`.

Structure:

- Source SQL changes go in `internal/db/queries` and migrations; generated sqlc output in `internal/store/generated` follows regeneration only.
- Plan validation/submission behavior belongs in `internal/plans`; request/response mapping belongs in `internal/api`; optional run association lookup belongs in `internal/intake`.
- `apps/web/src/routes` owns route files; reusable UI lives in `apps/web/src/components/relay`; API clients/types/query options live in `apps/web/src/features/*`.
- Root `web/src` scripts are for the legacy/utility bundle; do not mix them with `apps/web` React code.
- relay-contracts files should be referenced, not copied wholesale into implementation docs.

Patterns:

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

Verify checklist:

- No direct edits to generated `routeTree.gen.ts`, `*_templ.go`, or `internal/store/generated/*` without changing source and regenerating.
- Standalone runs still work when plan/pass IDs are omitted.
- Planner/pass scope is not broadened beyond the selected pass.
- Backend API response structs, mappers, frontend TypeScript types, and UI tests stay aligned.
- Relevant Go, web, and smoke checks are run or explicitly reported as not run.

## Setup

Prerequisites:

- Go matching `go.mod`.
- Node/npm for root scripts and `apps/web`.
- `templ`, `sqlc`, and `goose` when regenerating views, queries, or migrations.
- `make` for documented targets such as `make validate` and `make plan-api-smoke`.
- Optional RTK wrapper for noisy shell commands: prefer `rtk.exe`, then `rtk`, then raw command.

First-time setup:

1. `npm install`
2. `cd apps/web && npm install`
3. `go test ./...`
4. `make plan-api-smoke`
5. `cd apps/web && npm run typecheck`
6. `cd apps/web && npm run test`

Environment variables:

- `RELAY_DEV_RELOAD` optional; used by root dev targets for reload behavior.
- `RELAY_DEV_SMOKE` optional; enables dev smoke endpoint in `internal/api/smoke.go`.
- `RELAY_MCP_URL` and `RELAY_MCP_AUTH_TOKEN` required only for `make mcp-http-smoke`.

Common commands:

- `npm run dev` — root concurrent dev flow for legacy CSS/JS, templ generation, and Go server reload.
- `npm run build` — root legacy CSS/JS bundle from `web/src` into `web/static`.
- `npm run dev:web` or `cd apps/web && npm run dev` — React/TanStack workbench dev server.
- `go test ./...` — Go test suite.
- `make plan-api-smoke` — isolated smoke harness for managed plan HTTP API behavior.
- `make validate` — documented full validation target via `scripts/validate.sh`.
- `cd apps/web && npm run typecheck` — TypeScript check for the workbench.
- `cd apps/web && npm run test` — Vitest tests for the workbench.

Common issues:

- `make mcp-http-smoke` fails unless `RELAY_MCP_URL` and `RELAY_MCP_AUTH_TOKEN` are set.
- `sqlc generate` changes generated Go; review source SQL/migrations first and commit generated output only after regeneration.
- `apps/web/src/routeTree.gen.ts` is generated by TanStack Router tooling; update route files instead of editing it directly.

## Durable Decisions

relay-contracts is authoritative for Planner contracts.

- Planner handoffs, pass plans, canonical packets, policies, templates, schemas, and audit packet contracts are governed by relay-contracts, not repo-local notes.
- Copying full contracts into repo-local notes is rejected because it creates a stale second source of truth.

Managed plans are optional.

- Runs may be standalone or associated to a plan/pass through nullable DB fields.
- API, store, and UI work must preserve omitted plan/pass behavior.

Plan/pass association is read/UI context, not mandatory lifecycle state.

- Plan/pass/run association supports traceability and UI context.
- The backend store remains authoritative for run lifecycle and audit evidence.
- Surface associated runs in read APIs while keeping run state transitions server-backed.

Preserve root templ/htmx stack until explicit decommission.

- The root Go/templ/htmx/Tailwind assets remain part of the repo while `apps/web` is the primary React workbench.
- Do not casually delete `web/`, templ views, root npm scripts, or generated templ output.
