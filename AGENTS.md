# AGENTS.md

# Agent Instructions

## Project

Relay is a local-first handoff orchestration web app.

Relay accepts surgical implementation handoffs, stores run metadata/artifacts, prepares prompts for repo agents, captures run outputs, and generates audit packets for review.

## Stack

Use:

- Go `net/http`
- `chi`
- `templ`
- `encoding/json`
- `log/slog`
- `database/sql`
- SQLite
- `sqlc`
- `goose`
- `htmx`
- Alpine
- Tailwind CSS
- TypeScript browser bundle
- filesystem-backed run artifacts

## RTK shell command rule

Use RTK for noisy shell commands when available.

Prefer this order:

1. `rtk.exe`
2. `rtk`
3. raw command without RTK

Check availability with:

```bash
rtk.exe --version || rtk --version
```

Use RTK-wrapped commands for noisy inspection, search, diff, build, and test output.

Examples:

```bash
rtk.exe git status
rtk.exe git diff
rtk.exe grep "<pattern>" .
rtk.exe find "*.go" .
rtk.exe test "go test ./..."
rtk.exe test "go vet ./..."
rtk.exe test "npm run build"
```

If `rtk.exe` is unavailable, use the same commands with `rtk`:

```bash
rtk git status
rtk git diff
rtk test "go test ./..."
rtk test "npm run build"
```

If neither `rtk.exe` nor `rtk` is available, run the normal command directly.

Preserve full error detail when a build, typecheck, generation, migration, or test command fails. If RTK output is too compact to diagnose a failure, inspect the relevant source file or rerun the narrow failing command without RTK.

## Repo hygiene rules

- Work from source files, not generated output.
- Do not edit generated assets, dependency output, coverage output, or build output unless explicitly requested.
- Do not edit `node_modules/`, `coverage/`, `bin/`, `tmp/`, generated frontend assets, generated sqlc output, or local data artifacts unless the task explicitly requires it.
- Make focused, surgical changes.
- Preserve existing behavior unless the task explicitly asks to change it.
- Avoid unrelated formatting churn.
- Prefer boring, readable code over clever abstractions.

## Working rules

- Follow the current handoff exactly.
- Keep changes scoped to the requested task.
- Do not add unrelated architecture or cleanup.
- Do not implement future pipeline stages unless explicitly requested.
- Use server-rendered HTML. Do not introduce a SPA framework.
- Do not introduce React, Vue, Svelte, TanStack Start, Echo, Gin, Fiber, or templ unless explicitly requested.
- Keep large artifacts on disk and metadata in SQLite.
- Use `html/template` for server-rendered views.
- Use htmx for server-driven interactions.
- Use Alpine only for local UI state such as tabs, collapsible panels, dropdowns, and small confirmation toggles.
- Do not store run lifecycle state in Alpine.
- Use Tailwind for styling.
- Use TypeScript for browser-side behavior.
- Run the requested validation commands when available.
- Report blockers instead of guessing around missing tools, missing commands, or unclear requirements.

## Validation expectations

When relevant, prioritize:

```bash
go fmt ./...
go test ./...
go vet ./...
sqlc generate
goose -dir internal/db/migrations sqlite3 data/relay.sqlite up
npm run build
```

If the project uses different script names, run the documented equivalents.

If a validation command cannot be run, report the exact command and reason.

## Completion response format

When the task is complete, reply with only:

```text
DONE or BLOCKED
Build status
Test status
Count of LOC changed
Blocker/error only if BLOCKED
```

Keep output minimal.
