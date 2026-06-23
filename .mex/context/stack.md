---
name: stack
description: Actual stack and scripts from go.mod, package.json, apps/web/package.json, Makefile, and sqlc.yaml.
triggers:
  - stack
  - dependency
  - build
  - scripts
  - sqlc
edges:
  - target: context/setup.md
    condition: when running or validating commands
  - target: context/conventions.md
    condition: when applying generated-file or framework conventions
  - target: context/relay-web.md
    condition: when working in apps/web
  - target: context/relay-api.md
    condition: when working in Go API/store code
last_updated: 2026-06-23
---

# Stack

## Core Technologies

- **Go 1.25.7** - primary backend/runtime module `relay`.
- **`net/http` + `github.com/go-chi/chi/v5`** - HTTP routing for API and server surfaces.
- **SQLite (`modernc.org/sqlite`)** - local database opened by `internal/store` with WAL and foreign keys.
- **sqlc v2 config** - query sources in `internal/db/queries`, migrations in `internal/db/migrations`, generated Go in `internal/store/generated`.
- **templ** - root/server-rendered views under `internal/views`; generated `_templ.go` files are output.
- **Root htmx/Alpine/Tailwind/esbuild** - legacy/utility UI bundle from `web/src` to `web/static`.
- **React/TanStack Start/Vite** - primary `apps/web` workbench.

## Key Libraries

- **Go:** `github.com/a-h/templ`, `github.com/go-chi/chi/v5`, `modernc.org/sqlite`, `github.com/pressly/goose/v3` (indirect but used by migration tooling).
- **Root frontend:** `tailwindcss`, `@tailwindcss/cli`, `esbuild`, `typescript`, `htmx.org`, `alpinejs`, `concurrently`.
- **apps/web:** React, React DOM, TanStack Start/Router/Query/Table/Virtual/Form, Vite, Vitest, Radix UI, lucide-react, shadcn, zod, Tailwind.
- **Dev scripts:** root `npm run build` builds legacy CSS/JS; `npm run build:web` delegates to `apps/web`; `make validate` runs `scripts/validate.sh`.

## What We Deliberately Do NOT Use

- Do not introduce Echo, Gin, Fiber, or another Go web framework for normal API work.
- Do not introduce a new SPA framework; `apps/web` is already React/TanStack Start, and root UI remains templ/htmx/Alpine.
- Do not bypass sqlc by hand-writing generated Go query wrappers.
- Do not store large run artifacts in SQLite when filesystem-backed artifact storage is already the pattern.

## Version Constraints

- `apps/web` package versions are mostly `"latest"`; avoid making assumptions from memory. Inspect lockfiles/current install if version-specific behavior matters.
- Root and `apps/web` have separate `package.json` files and separate build/test scripts.
