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

- Go 1.21+
- Node.js 18+ with npm
- `sqlc` CLI
- `goose` CLI
- `templ` CLI

Install CLI tools:

```bash
go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest
go install github.com/pressly/goose/v3/cmd/goose@latest
go install github.com/a-h/templ/cmd/templ@latest
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

## Run Actions

| Action | Status |
|--------|--------|
| `validate-handoff` | Implemented |
| `prepare-prompt` | Implemented |
| `mark-accepted` | Implemented |
| `mark-needs-cleanup` | Implemented |
| `run-agent` | Future |
| `run-validation` | Future |
| `inspect-diff` | Future |
| `generate-audit-packet` | Future |

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
