---
name: agents
description: Always-loaded project anchor. Contains project identity, non-negotiables, commands, and pointer to ROUTER.md.
edges:
  - target: ROUTER.md
    condition: always read after this file
  - target: context/decisions.md
    condition: when authority or scope is unclear
last_updated: 2026-06-23
---

# Relay

## What This Is
Relay is a local-first handoff/run orchestration workbench for turning reviewed Planner handoffs and managed Plan/Pass artifacts into runs, validation evidence, audit artifacts, and manual closeout support.

## Non-Negotiables
- `relay-contracts/` is authoritative for Planner and pipeline contracts; `.mex` is implementation memory only.
- Do not broaden Planner handoffs or implementation work beyond the selected pass.
- Preserve standalone runs; managed plan/pass association is optional.
- Do not edit generated `apps/web/src/routeTree.gen.ts` or `internal/store/generated/*` directly.
- Preserve the legacy root Go/templ/htmx assets until an explicit decommission pass removes them.

## Commands
- Go tests: `go test ./...`
- Plan smoke: `make plan-api-smoke`
- Full validate: `make validate`
- Web typecheck: `cd apps/web && npm run typecheck`
- Web tests: `cd apps/web && npm run test`
- Web build: `cd apps/web && npm run build`
- Mex check: `npx mex-agent check --json`

## Navigation
Read `ROUTER.md` next, then load task-routed context and patterns.
