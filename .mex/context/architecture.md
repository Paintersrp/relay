---
name: architecture
description: High-level Relay architecture and workflow from Planner handoff to run audit.
triggers:
  - architecture
  - workflow
  - run pipeline
  - managed plans
edges:
  - target: context/managed-plans.md
    condition: when plan/pass/run association details matter
  - target: context/relay-api.md
    condition: when tracing backend handlers, store methods, or migrations
  - target: context/relay-web.md
    condition: when tracing workbench routes and components
  - target: context/pipeline-contracts.md
    condition: when contract authority or artifact shape matters
last_updated: 2026-06-23
---

# Architecture

## System Overview

Planner handoff or managed plan artifact -> Relay intake/API -> local SQLite store metadata -> filesystem-backed artifacts -> compile/prepare prompt and canonical packet -> execute via configured agent adapter -> collect validation/output evidence -> generate audit packet and closeout support.

Managed plans add an optional planning layer: a plan has ordered passes, and runs can be standalone or associated to a plan/pass. Association supplies UI context and read-side traceability without making plans mandatory for run creation.

The backend is a local Go daemon using `net/http`, `chi`, `database/sql`, SQLite, migrations, sqlc-generated query wrappers, and services under `internal/*`. The primary UI is the React/TanStack Start workbench under `apps/web`; root `web/` and templ assets remain for legacy/utility surfaces.

## Key Components

- **Go API (`internal/api`, `cmd/relay`)** - exposes run, plan, project, artifact, validation, repair, and smoke endpoints through `chi`.
- **Store and DB (`internal/store`, `internal/db`)** - owns SQLite access, migrations, sqlc query sources, and generated data models.
- **Plan services (`internal/plans`, `internal/intake`)** - validate planner pass plans, persist plan/pass rows, compute lifecycle readiness, and resolve optional run association.
- **Pipeline services (`internal/compiler`, `internal/pipeline`, `internal/executor`, `internal/auditor`, `internal/validation*`)** - transform handoffs into executable artifacts, run agents/validation, and collect audit evidence.
- **Workbench (`apps/web/src`)** - React Query plus TanStack Router UI for plans, passes, runs, execution, validation, artifacts, and audit state.
- **Contracts (`relay-contracts/`)** - source-controlled planner/pipeline contracts, schemas, policies, templates, and examples.

## External Dependencies

- **SQLite via `modernc.org/sqlite`** - local-first runtime database; migrations run through Go auto-migration and goose-compatible files.
- **Filesystem artifacts** - large run outputs and packets are written to disk while metadata remains in SQLite.
- **Repo source services** - local git/rg/filesystem discovery feeds source snapshots and run context.
- **Agent adapters** - Codex, OpenCode, Antigravity, and related executor paths are represented in `internal/executor` and pipeline code.

## What Does NOT Exist Here

- `.mex` is not normative pipeline law and must not duplicate entire relay-contracts files.
- Runs do not have to belong to a managed plan.
- The React workbench does not make this a SPA-only repo; Go/templ/htmx files still exist and should not be casually removed.
- Generated route trees and sqlc Go output are not hand-edited source.
