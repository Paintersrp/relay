# Agent Instructions

## Project

Relay is a local-first handoff/run orchestration workbench.

Relay accepts reviewed Planner handoffs and managed Plan/Pass artifacts, stores run metadata and filesystem-backed artifacts, prepares execution prompts, captures run outputs, and generates validation/audit evidence for review and closeout.

## Authority Order

When instructions conflict, use this order:

1. Current user/task instructions
2. The selected Planner handoff or canonical packet for the run, when provided
3. Checked-out source code and tests
4. The canonical relay-contracts GitHub repository for Planner/pipeline behavior
5. Older repo notes, prior chat, or stale instructions

Do not treat repo-local notes as more authoritative than:

- checked-out source code
- Planner handoffs
- canonical packets
- Relay DB state
- run artifacts
- audit evidence
- relay-contracts source files

## Planner / Pipeline Contract Source

For Planner handoffs, pass plans, canonical packets, validation reports, executor briefs, audit packets, policy behavior, and schema behavior, use the canonical relay-contracts source.

## Stack

Relay is a hybrid repo.

Backend and root/runtime stack:

- Go `net/http`
- `chi`
- `encoding/json`
- `log/slog`
- `database/sql`
- SQLite through `modernc.org/sqlite`
- `sqlc`
- `goose`
- `templ`
- htmx
- Alpine
- Tailwind CSS
- TypeScript browser bundle
- filesystem-backed run artifacts

The root Go/templ/htmx UI remains present as a legacy/utility surface.

The primary modern workbench is under `apps/web` and uses React/TanStack Start with related TanStack libraries.

Do not remove legacy root `web/`, templ views, root npm scripts, or generated templ output unless the current task explicitly decommissions them.

## Generated Files

Do not edit generated files directly.

In particular:

- Do not hand-edit `apps/web/src/routeTree.gen.ts`.
- Do not hand-edit `internal/store/generated/*`.
- For sqlc changes, update SQL/query sources and regenerate.
- For route tree changes, update route files and regenerate through normal frontend tooling.
- Do not hand-edit generated `*_templ.go` files without changing source templates and regenerating.

## Run / Plan Behavior

Runs may be standalone.

Managed plan/pass association is optional and should remain nullable-compatible unless the selected handoff explicitly changes that behavior.

Do not require every run to belong to a plan or pass.

A managed plan stores a Planner pass plan JSON submission as a `plans` row plus ordered `plan_passes` rows. A run may be associated to a plan and optionally one pass through nullable `runs.plan_row_id` and `runs.plan_pass_row_id`.

## Repo Reference

For deeper repo orientation, see `docs/agent-reference.md`.

`docs/agent-reference.md` remains compact human orientation and does not override generated references, source code, tests, selected handoffs, canonical packets, Relay DB state, run artifacts, audit evidence, or relay-contracts.

For backend/API/MCP/storage/workflow/contract navigation, use the generated project-level agent references as the default source-backed navigation entry point:

- `docs/generated/agent-references/index.json` is the machine-readable generated reference index.
- `docs/generated/agent-references/index.md` is the human-readable generated reference index.

`docs/backend-code-surface-map.md` is a retired compatibility pointer and not the default source-backed navigation map.

## RTK Shell Command Rule

Use RTK for noisy shell commands when available.

Prefer this order:

1. `rtk.exe`
2. `rtk`
3. raw command without RTK

Check availability with:

```bash
rtk.exe --version || rtk --version
```

Use RTK-wrapped commands for noisy inspection, search, diff, generation, migration, build, and test output.

Examples:

```bash
rtk.exe git status
rtk.exe git diff
rtk.exe grep "<pattern>" .
rtk.exe find "*.go" .
rtk.exe test "templ generate"
rtk.exe test "sqlc generate"
rtk.exe test "go test ./..."
rtk.exe test "go vet ./..."
rtk.exe test "npm run build"
```

If `rtk.exe` is unavailable, use the same commands with `rtk`.

If neither `rtk.exe` nor `rtk` is available, run the normal command directly.

## Validation Commands

Use the narrowest relevant validation first, then broader validation when risk warrants.

Common commands:

```bash
go test ./...
make validate
make plan-api-smoke
cd apps/web && npm run typecheck
cd apps/web && npm run test
cd apps/web && npm run build
```

For managed plan API/store changes, prefer:

```bash
make plan-api-smoke
go test ./...
```

For `apps/web` changes, prefer:

```bash
cd apps/web && npm run typecheck
cd apps/web && npm run test
```

## Scope Discipline

Keep implementation changes bounded to the current task or selected Planner handoff.

Do not introduce future-pass work, unrelated cleanup, framework changes, route rewrites, schema changes, lifecycle behavior changes, or generated-file churn unless explicitly requested.

If repo instructions are stale, update only the stale instruction text needed for the current task.
