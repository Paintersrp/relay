---
name: router
description: Session bootstrap and navigation hub. Read after AGENTS.md before starting repo work.
edges:
  - target: context/architecture.md
    condition: always load first for implementation tasks
  - target: context/stack.md
    condition: when touching dependencies, build scripts, generated files, or runtime setup
  - target: context/conventions.md
    condition: when writing or reviewing code
  - target: context/managed-plans.md
    condition: when touching plan/pass/run association or plan UI/API behavior
  - target: context/relay-web.md
    condition: when touching apps/web routes, components, React Query, or visual-state helpers
  - target: context/relay-api.md
    condition: when touching Go API, store, SQL, migrations, or smoke harnesses
  - target: context/pipeline-contracts.md
    condition: when touching Planner handoffs, canonical packets, executor/audit artifacts, or contract-sensitive code
  - target: patterns/INDEX.md
    condition: before starting any task with a repeatable workflow
last_updated: 2026-06-23
---

# Session Bootstrap

Read `AGENTS.md`, then this file. Load `context/architecture.md` first for implementation work, then load the files or patterns named below.

## Current Project State

**Working:**
- Local Go daemon in `cmd/relay` with `chi` routes, SQLite store, migrations, sqlc queries, validation, executor, audit, source, MCP, and event services.
- React/TanStack Start workbench in `apps/web` with run registry/workbench routes and managed plan registry/detail/pass detail routes.
- Managed plan submission/read APIs exist for `/api/plans`, `/api/plans/{planId}`, and `/api/plans/{planId}/passes/{passId}`.
- Runs may be standalone or optionally associated to a plan and pass through nullable `runs.plan_row_id` and `runs.plan_pass_row_id`.
- Source-controlled contracts, schemas, policies, templates, and examples live under `relay-contracts/`.

**Not Built / Incomplete:**
- `.mex` is not a replacement for relay-contracts, database state, canonical packets, run artifacts, or audit evidence.
- Managed plan completion readiness is read-side support; plan status is not automatically closed by the smoke harness.
- Root templ/htmx UI remains present as legacy/utility surface while `apps/web` is the primary workbench.

**Known Drift Notes:**
- The installer brief names a legacy surgical implementation handoff instructions path under docs/instructions, but that file is not present in this checkout; current instruction assets are under `internal/instructions/` and contracts under `relay-contracts/`.
- The repo is currently hybrid: root Go/templ/htmx build scripts plus `apps/web` React/TanStack Start. Treat checked-out files as authoritative.

## Routing Table

| Task type | Load |
|-----------|------|
| System flow or boundaries | `context/architecture.md` |
| Stack, scripts, dependency choice | `context/stack.md` |
| Naming, generated files, validation habits | `context/conventions.md` |
| Authority, scope, or stale-memory questions | `context/decisions.md` |
| Local setup and validation commands | `context/setup.md` |
| Managed plans, passes, or run association | `context/managed-plans.md` |
| `apps/web` routes/components/types/tests | `context/relay-web.md` |
| Go API/store/sqlc/migrations/smoke tests | `context/relay-api.md` |
| Planner handoffs or pipeline artifact contracts | `context/pipeline-contracts.md` |
| Repeatable implementation task | `patterns/INDEX.md` |

## Behavioural Contract

1. **CONTEXT** - Load the routed context and matching pattern before editing.
2. **BUILD** - Make focused source changes only; do not edit generated files directly.
3. **VERIFY** - Run the narrow relevant checks first, then broader commands when risk warrants.
4. **DEBUG** - If a check fails, inspect the exact failing source or rerun the narrow command with full output.
5. **GROW** - Update `.mex` only when the task reveals durable repo memory or a stale context note.
