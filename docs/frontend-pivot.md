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
- Step 4 Audit / Close route wired to real backend endpoints.
- Backend handlers added: `ApproveAudit`, `RequestAuditRevision`, `PrepareCommitMessage`, `CloseRun`.
- Frontend mutation methods: `submitManualAuditPacket`, `approveAudit`, `requestAuditRevision`, `prepareCommitMessage`, `closeRun`.
- Audit Input Summary, Audit Packet, Audit Decision, Warnings/Revision Requirements, Commit Summary, and Close Run sections render real backend data.
- Generate audit, manual audit packet submission, approve audit, request revision, prepare commit message, and mark done are real backend mutations.
- Prepare commit message writes only a suggested artifact — no git commit, push, staging, or repo mutation.
- Closeout gated by accepted or accepted_with_warnings audit state, updates Relay run state only.
- New Run intake submission wired to backend.
- Close Run wired to backend.
- Mock data replaced with real React Query data.

### Pass 5 — Intake Wiring
- Wired Step 1 Intake UI and Mutations, supporting real approval/reject/blocked decisions.
- Real Go backend parsed frontmatter, created runs, and saved intake artifacts.

### Pass 6 (current) — Compiler & Validation Service
- Implemented internal Go backend compiler and packet validation service.
- Compiles approved runs into canonical packets (`canonical_packet.json`) and runs schema/path/security checks, outputting validation reports (`packet_validation_report.json`) and transitioning run status to `packet_validated` or `packet_validation_failed`.

### Pass 11 (current) — Audit packet generator / bridge

- Backend auditor package (`internal/auditor`) added for evidence collection, `audit_input_summary.md` and `audit_packet.md` generation, and manual audit packet submission.
- `POST /api/runs/{id}/audit` generates audit artifacts from executor evidence (gated to `executor_done`/`executor_blocked`).
- `POST /api/runs/{id}/audit/submit` accepts manual audit packet Markdown with validated decision values.
- No Step 4 React UI wiring, MCP tool registration, commit, push, or auto-closeout behavior implemented.
- Old templ/htmx UI and existing Go backend routes remain intact.

### Later passes — Decommission (TBD)

- The old templ/htmx UI may be decommissioned in a future pass after the React frontend reaches feature parity.
- This is explicitly not planned for Pass 1–11.

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
