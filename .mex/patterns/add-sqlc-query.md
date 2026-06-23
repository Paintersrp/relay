---
name: add-sqlc-query
description: Add or change SQLite queries/migrations and regenerate sqlc Go output safely.
triggers:
  - sqlc
  - migration
  - query
  - generated store
edges:
  - target: context/relay-api.md
    condition: for store, query, migration, and generated-file conventions
  - target: context/conventions.md
    condition: for generated-file and validation rules
last_updated: 2026-06-23
---

# Add sqlc Query

## Context

SQL source is authoritative. Generated files under `internal/store/generated` are regeneration output.

## Steps

1. Add or modify migrations in `internal/db/migrations` if schema shape changes.
2. Add or modify named queries in `internal/db/queries/*.sql`.
3. Run `sqlc generate`.
4. Use generated methods through `internal/store/db.go` when a repo-level wrapper is useful.
5. Update API/service code to call store methods rather than embedding SQL in handlers.
6. Add or adjust Go tests that cover the query behavior.

## Gotchas

- Do not hand-edit generated sqlc Go files.
- Keep nullable plan/pass association fields nullable unless explicitly changing standalone run behavior.
- SQLite migrations should preserve local data and include indexes for new lookup paths.

## Verify

- `sqlc generate`
- `go test ./...`
- `goose -dir internal/db/migrations sqlite3 data/relay.sqlite up` when validating migrations against the local runtime DB is relevant.

## Update Scaffold

- [ ] Update `context/relay-api.md` if new query ownership or migration conventions emerge.
