# Backend API/App Feature Architecture

## Purpose

This document records the package boundaries created by the backend API/app feature organization pass plan. It is a maintainer and audit reference for the completed API/app/store package structure, not a new runtime contract.

## Package Ownership

| Layer | Path | Owns | Must Not Own |
|---|---|---|---|
| Transport adapters | `internal/api/<feature>` | HTTP routes, DTOs, mappers, presenters, request decoding, response writing | business workflows, persistence queries |
| Shared transport helpers | `internal/api/shared` | JSON response helpers, request helpers, CORS/time formatting helpers | feature business behavior, persistence |
| App services/use cases | `internal/app/<feature>` | business workflow, validation orchestration, lifecycle/use-case coordination | HTTP request/response DTOs, route mounting |
| Store/persistence | `internal/store`, `internal/store/generated` | database access, generated SQL query wrappers, storage-facing row types | HTTP, API DTOs, app orchestration |
| Legacy root API adapter | `internal/api` | temporary refactor backlog routes and development smoke setup route | migrated feature handlers or new business behavior |

## Import Direction Rules

- `internal/api/<feature>` may import `internal/app/<feature>` and `internal/api/shared`.
- `internal/api/<feature>` must not import root `internal/api`.
- `internal/api/<feature>` must not import `internal/store` or `internal/store/generated` directly.
- `internal/app/<feature>` must not import `internal/api` or `internal/api/<feature>`.
- `internal/app/<feature>` may import `internal/store`, infrastructure packages, and other app packages only when needed for an existing workflow.
- `internal/store` must not import `internal/api` or `internal/app`.
- New business behavior must not be added to root `internal/api.APIHandler`.
- `internal/domain` does not exist in this architecture and must not be introduced without a separate approved pass.

## Current Feature Packages

| Feature | API Package | App Package |
|---|---|---|
| Projects | `internal/api/projects` | `internal/app/projects` |
| Plans | `internal/api/plans` | `internal/app/plans` |
| Runs | `internal/api/runs` | `internal/app/runs` |
| Intake | `internal/api/intake` | `internal/app/intake` |
| Artifacts | `internal/api/artifacts` | Uses run app services from `internal/app/runs` |
| Audits | `internal/api/audits` | `internal/app/audits` |

## Legacy Root API Boundary

The root `internal/api.APIHandler` is a bounded compatibility adapter. It currently owns only:

- project-scoped refactor backlog routes mounted by `mountProjectRefactorRoutes`
- `/api/dev/setup-smoke-validation-failure`

The legacy root API adapter is not the normal place for new handlers. New or migrated feature behavior should live behind a feature transport adapter in `internal/api/<feature>` and delegate workflow behavior to `internal/app/<feature>`.

## Architecture Tests

`internal/architecture/boundary_test.go` enforces:

- app must not import API
- store must not import app/API
- feature API packages must not import root API
- feature API packages must not import store directly
- server route composition must mount feature handlers, not `apiH`, for migrated feature routes

## Validation

```bash
go test ./internal/architecture
go test ./internal/api/... ./internal/app/... ./internal/server/...
go test ./...
```
