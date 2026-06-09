# Relay

Local-first handoff orchestration web app.

Relay accepts surgical implementation handoffs, stores run metadata and artifacts, validates handoff structure, generates ready prompts, and provides a run workbench for inspection.

## Stack

- Go `net/http` + `chi` router
- `templ` for server-rendered views
- SQLite via `database/sql`
- `sqlc` for typed queries
- `goose` for migrations
- `htmx` + Alpine for browser interactions
- Tailwind CSS v4
- TypeScript bundled with esbuild

## Prerequisites

- Go 1.25+
- Node.js 18+ with npm
- `sqlc` CLI
- `goose` CLI
- `templ` CLI
- `air` CLI for live-reload development

Install CLI tools:

```bash
go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest
go install github.com/pressly/goose/v3/cmd/goose@latest
go install github.com/a-h/templ/cmd/templ@latest
go install github.com/air-verse/air@latest
```

## Setup

```bash
# Install frontend dependencies
npm install

# Build frontend assets (CSS + JS)
npm run build

# Generate sqlc typed queries
sqlc generate

# Generate templ views
templ generate

# Run database migrations
goose -dir internal/db/migrations sqlite3 data/relay.sqlite up

# Build the server
go build -o bin/relay.exe ./cmd/relay

# Run the server (port 8080 by default)
go run ./cmd/relay
```

Or use the Makefile:

```bash
make install    # npm install
make assets     # build CSS + JS
make sqlc       # generate sqlc
make templ      # generate templ
make db-migrate # run migrations
make dev        # build assets + run server
make build      # full build
make test       # run tests
```

## Routes

| Method | Path | Description |
|--------|------|-------------|
| GET | `/` | Dashboard with recent runs |
| GET | `/handoffs/new` | New handoff form |
| POST | `/handoffs` | Create handoff run |
| GET | `/runs/{id}` | Run detail workbench |
| POST | `/runs/{id}/actions` | Execute run action |
| GET | `/runs/{id}/artifacts/{kind}` | View artifact |
| GET | `/runs/{id}/artifacts/{kind}/download` | Download artifact |
| GET | `/settings/repos` | Repository settings |
| POST | `/settings/repos/roots` | Add scan root |
| POST | `/settings/repos/scan` | Scan repos now |

## Run Actions

| Action | Status |
|--------|--------|
| `validate-handoff` | Implemented |
| `prepare-prompt` | Implemented |
| `mark-accepted` | Implemented |
| `mark-needs-cleanup` | Implemented |
| `generate-opencode-packet` | Implemented |
| `submit-agent-result` | Implemented |
| `run-agent` | Future |
| `run-validation` | Implemented |
| `inspect-diff` | Future |
| `generate-audit-packet` | Future |

## Development live reload

Relay supports a local dev live-reload workflow for server-rendered development.

Install frontend dependencies and required CLIs, then run:

```bash
make dev
```

This runs Tailwind watch, esbuild watch, templ watch, and an Air-managed Go server with `RELAY_DEV_RELOAD=1`.

If running directly on Windows PowerShell:

```powershell
$env:RELAY_DEV_RELOAD="1"
npm run dev
```

The browser reloads when built frontend assets change or when the Go server restarts.

## Intake review

Relay's first useful step is the intake review.

The intake review parses the pasted handoff and shows:

- detected model
- selected model
- selected repo
- branch/worktree
- scoped files
- scoped file existence checks
- validation commands
- final output contract
- suggested commit message
- blockers and warnings

Relay should warn before validation when the selected repo does not appear to match the handoff scope.

## Model selection

When creating a handoff, Relay parses a recommended model from the pasted handoff text. It supports:

- `## Execution model` / `Use:` section
- Labels like `Recommended Model:`, `Model:`, `Use model:`, `Suggested model:`

If no model is found and no override is selected, Relay defaults the selected model to DeepSeek V4 Flash.

The New Handoff form provides a model dropdown for overrides and a Custom option for provider-specific model IDs.

## Agent Prompt

The Agent Prompt artifact is currently the handoff text prepared for copying to an external agent. Relay does not execute OpenCode yet, and the OpenCode packet is metadata only until a future execution adapter consumes it.

## OpenCode handoff packet

Relay can generate an `opencode_handoff_packet.json` artifact after an agent prompt exists.

The packet includes the run id, local repo path, branch/worktree metadata, selected model, recommended model, agent prompt artifact path, run artifact directory, and an execution status of `not_implemented`.

Relay does not execute OpenCode yet. The packet is metadata only.

## Manual agent result intake

Relay can store the final output from an external repo agent after the user runs that agent outside Relay.

Expected final output shape:

```text
DONE or BLOCKED
Build status: ...
Test status: ...
Count of LOC changed: ...
Blocker/error only if BLOCKED: ...
```

Relay stores the raw pasted result, records parsed metadata as an agent result check, and updates the run status to `agent_done`, `agent_blocked`, or `agent_result_needs_review`.

Relay still does not execute OpenCode or other agents in this phase.

## Validation command runner

Relay can run validation commands for a run from the selected local repository path.

Commands are extracted from the original handoff's Tests / validation section. If no handoff commands are found, Relay falls back to the selected repo's default validation commands.

Validation command execution is user-triggered. Relay captures stdout, stderr, exit code, duration, and timeout state as run artifacts/checks. Relay does not execute OpenCode or inspect diffs in this phase.

## Local repository discovery

Relay discovers local Git repositories from configured scan roots.

The default scan root is:

```text
D:/Code
```

Open `/settings/repos` to:

- add additional roots
- enable or disable roots
- scan now
- review discovered repositories

The New Handoff page lets you select a discovered repo or use manual repo name/path entry.

## Project Structure

```
cmd/relay/          Entry point
internal/
  server/           HTTP server + chi routes
  handlers/         Request handlers
  store/            SQLite store + models
  pipeline/         Handoff validation + prompt prep
  artifacts/        Filesystem artifact storage
  views/            templ templates
  db/
    migrations/     goose SQL migrations
    queries/        sqlc query definitions
web/
  src/              TypeScript + Tailwind sources
  static/           Built assets (gitignored)
data/
  relay.sqlite      SQLite database (gitignored)
  artifacts/        Run artifact files (gitignored)
```
