---
name: relay-api
description: Go API, store, SQL/sqlc, migration, and smoke-test conventions.
triggers:
  - API
  - store
  - sqlc
  - migration
  - smoke
edges:
  - target: context/managed-plans.md
    condition: when API changes touch managed plans or run association
  - target: context/pipeline-contracts.md
    condition: when API changes touch handoff or pipeline artifact contracts
  - target: patterns/add-sqlc-query.md
    condition: when adding or changing SQL queries
  - target: patterns/add-plan-api-read-field.md
    condition: when adding or changing plan read fields
last_updated: 2026-06-23
---

# Relay API

## Handler and Service Shape

- `cmd/relay/main.go` starts the local app; `internal/server` wires routes.
- `internal/api/api.go` owns HTTP request parsing, response structs, JSON mapping, and plan/run endpoint behavior.
- Domain logic should live in services such as `internal/plans`, `internal/intake`, `internal/pipeline`, `internal/auditor`, and `internal/validationrunner`.
- `internal/store/db.go` wraps sqlc-generated methods with repo-specific defaults and helper semantics.

## Store and SQL

- SQL migrations live in `internal/db/migrations`; query source lives in `internal/db/queries`.
- `sqlc.yaml` generates package `generated` into `internal/store/generated` with JSON tags and empty slices.
- Store aliases generated models (`type Plan = generated.Plan`) and exposes higher-level methods such as `CreateRunWithAssociation`.
- Do not hand-edit `internal/store/generated/*`; modify SQL/migrations and run `sqlc generate`.

## Plan/Run API Touchpoints

- Plan submission validation starts in `APIHandler.ValidatePlan` and `APIHandler.SubmitPlan`, then delegates to `plans.Service`.
- Plan read APIs map store rows through `mapPlanToAPI`, `mapPlanPassToAPI`, `mapRunToPlanAPIRunSummary`, and `buildPlanAPIReadPlan`.
- Associated runs are collected with `ListRunsByPlan` or `ListRunsByPlanPass`; summaries include workbench paths derived from run status.
- Run association resolution for intake is in `internal/intake/association.go`.

## Smoke and Tests

- `cmd/plan-api-smoke` creates an isolated temp SQLite store, registers plan routes, submits a synthetic plan, creates an associated run, and verifies read-side fields.
- Prefer `make plan-api-smoke` after managed plan API/store changes.
- Use `go test ./...` for broader Go safety, and add targeted tests near service/API code when behavior changes.
