# Relay Frontend Pivot

> **Pass 1 — Additive scaffold only. No backend migration. No execution movement.**

## Overview

`apps/web` is a **new, additive** React frontend for the Relay run workbench. It lives alongside the existing Go backend and templ/htmx UI. Nothing in the current backend or old UI has been changed.

## Runtime Split

| Runtime                | Location    | Port                   | Responsibility                                              |
| ---------------------- | ----------- | ---------------------- | ----------------------------------------------------------- |
| Go daemon (existing)   | `cmd/relay` | `:8080`                | Orchestration, SQLite, artifact storage, run lifecycle      |
| TanStack Start (React) | `apps/web`  | `:3000`                | Run workbench UI, read-only display, future action surfaces |
| Old templ/htmx UI      | `web/`      | `:8080` (served by Go) | Existing server-rendered UI — untouched until later pass    |

The `VITE_RELAY_API_BASE_URL=http://localhost:8080` environment variable documents where the React frontend will send API requests. **Pass 1 does not make any API calls** — all data is mock-only.

## Pass Boundaries

### Pass 1 (current) — Additive scaffold

- `apps/web` created with official TanStack Start React scaffold using npm.
- React Query configured at app root.
- Tailwind CSS v4 + shadcn/ui initialized inside `apps/web`.
- Relay-specific workbench routes (`/runs`, `/runs/new`, `/runs/$runId/intake`, `/runs/$runId/prepare`, `/runs/$runId/execute`, `/runs/$runId/audit`) using mock data only.
- All action buttons (approve, submit, close) are visually disabled to prevent confusion with real mutations.
- **No real Go API calls. No real SSE. No mutations.**

### Pass 2 (next) — Frontend API contract

- Define the JSON API shape Relay's Go backend will expose.
- Document request/response schemas for each workbench step.
- No backend implementation yet.

### Pass 3 — Read-only backend JSON endpoints

- Go backend adds new JSON-only endpoints alongside existing templ/htmx routes.
- React frontend queries these endpoints via React Query.
- Old templ/htmx UI remains intact.
- Real validation results, artifact content, log lines, and run metadata become available.

### Pass 4 — Action wiring

- Approval gate actions (approve, reject) wired to real backend endpoints.
- New Run intake submission wired to backend.
- Close Run wired to backend.
- Mock data replaced with real React Query data.

### Later passes — Decommission (TBD)

- The old templ/htmx UI may be decommissioned in a future pass after the React frontend reaches feature parity.
- This is explicitly not planned for Pass 1–4.

## Hard Constraints

- **Pipeline execution must NOT move into TanStack Start server functions.** The Go daemon owns all execution, validation, and artifact lifecycle.
- **Do not convert the repo into a monorepo workspace.** Root `package.json` and its scripts remain for the old build.
- **Do not delete or migrate the old templ/htmx UI** until a later pass explicitly permits it.
- All new frontend package changes belong to `apps/web/package.json` and `apps/web/package-lock.json` only.

## Tech Stack (apps/web)

- **Framework**: TanStack Start (React, Vite, file-based routing)
- **Routing**: TanStack Router
- **Data fetching**: TanStack React Query (mock data in Pass 1)
- **Styling**: Tailwind CSS v4 + shadcn/ui components
- **Icons**: lucide-react
- **Package manager**: npm (apps/web only)

## Development

```bash
# Start the Go backend (existing):
make dev
# or:
go run cmd/relay/main.go

# Start the React frontend (new, separate terminal):
cd apps/web
cp .env.example .env
npm install
npm run dev
# → http://localhost:3000
```

The two processes are independent. The Go backend does not depend on the React frontend, and in Pass 1, the React frontend does not depend on the Go backend.
