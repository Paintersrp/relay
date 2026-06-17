# Relay Frontend Pivot

> **Pass 14R — Old templ/htmx workflow UI routes replaced with React redirects. React is now the primary workflow UI.**
> **Pass 15D — Verified end-to-end. Docs reconciled with implementation.**

## Current Architecture

`apps/web` is the **primary workflow UI** for the Relay run workbench. Superseded Go templ/htmx
workflow routes now redirect to the React workbench. The Go backend retains JSON API routes,
orchestration, and utility UI pages (instructions, settings, raw artifact viewer).

## Runtime Split

| Runtime                | Location    | Port                   | Responsibility                                                              |
| ---------------------- | ----------- | ---------------------- | --------------------------------------------------------------------------- |
| Go daemon              | `cmd/relay` | `:8080`                | JSON APIs, orchestration, SQLite, artifacts, utility UI (instructions, settings, artifact raw view) |
| TanStack Start (React) | `apps/web`  | `:3000`                | **Primary workflow UI** — run creation, intake, prepare, execute, audit     |
| Old templ/htmx UI      | `web/`      | `:8080` (served by Go) | Utility views only; workflow routes redirect to React                        |

## Canonical Workflow States

The API exposes canonical workflow states in the `status` field. Display/helper fields (`activeStep`, `lifecycleState`, `state`, `statusSeverity`) are derived from `status` and must not be used for action gating.

See `docs/api/frontend-api-contract.md` for the full status table and gating rules.

## Primary React Routes

| Path | Description |
|------|-------------|
| `/runs` | Run list with real API data |
| `/runs/new` | New run intake form (connects to `POST /api/intake/planner-handoff`) |
| `/runs/{id}/intake` | Step 1: Intake Review |
| `/runs/{id}/prepare` | Step 2/3: Compile / Render Brief |
| `/runs/{id}/execute` | Step 4: Execute |
| `/runs/{id}/audit` | Step 5: Audit / Close |

The frontend uses TanStack React Query to fetch from the Go backend JSON API (`VITE_RELAY_API_BASE_URL`). Mutations call real backend endpoints. Read endpoints fall back to mock data on 404/501 or backend unavailable.

## Preserved Go Utility Routes

All `/api/*` JSON routes remain the API surface.

Utility server-rendered pages remain:

- `GET /instructions`, `/instructions/{kind}`, `/instructions/{kind}/download` — instruction assets
- `GET /settings/repos`, `POST /settings/repos/*` — repository settings
- `GET /runs/{id}/artifacts/{kind}`, `/runs/{id}/artifacts/{kind}/download` — raw artifact viewer/download

## Removed / Redirected Old UI Routes

| Old Go route | New destination |
|---|---|
| `GET /` | React `/runs` |
| `GET /handoffs/new` | React `/runs/new` |
| `POST /handoffs` (success) | React `/runs/{id}/intake` |
| `GET /runs/{id}` | React `/runs/{id}/{step}` (status-resolved) |
| `GET /runs/{id}/agent-run-monitor` | React `/runs/{id}/execute` |

Removed routes (return 404 if accessed directly on Go server):

- `POST /runs/{id}/actions` — HTMX form action handler
- `GET /runs/{id}/events` — HTMX SSE partial page
- `GET /runs/{id}/artifacts/{kind}/preview` — templ preview page

JSON `/api/runs/{id}/events` (GET) and `/api/runs/{id}/artifacts/{kind}` (GET) remain available.

## Pass History

| Pass | Status | Summary |
|------|--------|---------|
| Pass 1 | Complete | TanStack Start scaffold with mock data |
| Pass 2 | Complete | API contract document created |
| Pass 3 | Complete | Read-only JSON endpoints added |
| Pass 4 | Complete | Action wiring (audit/close mutations) |
| Pass 5 | Complete | Intake wiring (real backend parsing) |
| Pass 6 | Complete | Compiler & validation service |
| Pass 7 | Complete | Executor brief renderer |
| Pass 11 | Complete | Audit packet generator |
| Pass 14R | Complete | Old templ/htmx workflow UI routes decommissioned |
| Pass 14R2 | Complete | Removed obsolete RunsHandler scaffolding, deleted files, cleaned layout navigation |
| Pass 15A | Complete | Workflow state contract repaired (canonical statuses exposed) |
| Pass 15B | Complete | Audit/close state gating repaired |
| Pass 15C | Complete | MCP 13A/B boundary documented |
| Pass 15D | In progress | E2E verification and docs reconciliation |

## Known Blocked Areas

- **Pass 13B MCP tools**: Blocked pending target-client feasibility confirmation. Only `submit_test_audit_packet` (Pass 13A) is registered.
- No shell/file/git mutation MCP tools are present.

## Hard Constraints

- **Pipeline execution must NOT move into TanStack Start server functions.** The Go daemon owns all execution, validation, and artifact lifecycle.
- **Do not convert the repo into a monorepo workspace.** Root `package.json` and its scripts remain for the old build.
- **Workflow routes removed are limited to superseded templ/htmx UI paths.** JSON API routes, artifact raw view/download, instructions, and settings remain on the Go backend.
- All new frontend package changes belong to `apps/web/package.json` and `apps/web/package-lock.json` only.

## Tech Stack (apps/web)

- **Framework**: TanStack Start (React, Vite, file-based routing)
- **Routing**: TanStack Router
- **Data fetching**: TanStack React Query (real API with mock fallback)
- **Styling**: Tailwind CSS v4 + shadcn/ui components
- **Icons**: lucide-react
- **Package manager**: npm (apps/web only)

## Development

```bash
# Start the Go backend:
go run ./cmd/relay
# → http://localhost:8080

# Start the React frontend (separate terminal):
cd apps/web
cp .env.example .env
npm install
npm run dev
# → http://localhost:3000
```

The two processes are independent. The Go backend owns all orchestration, storage, and API surface. The React frontend queries the Go backend via `VITE_RELAY_API_BASE_URL` (default `http://localhost:8080`).
