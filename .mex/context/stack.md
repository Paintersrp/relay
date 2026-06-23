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

| Area | Current stack |
|---|---|
| Backend runtime | Go module `relay`; HTTP routing through `net/http` and `github.com/go-chi/chi/v5`. |
| Local storage | SQLite through `modernc.org/sqlite`; the store opens the DB with WAL and foreign keys. |
| Data access generation | `sqlc.yaml` config reads query sources from `internal/db/queries`, migrations from `internal/db/migrations`, and generates Go code in `internal/store/generated`. |
| Server-rendered utility surface | `templ` views live under `internal/views`; generated `_templ.go` files are output. |
| Root frontend bundle | `web/src` builds legacy/utility assets into `web/static` using Tailwind, esbuild, htmx, Alpine, TypeScript, and concurrently-run dev scripts. |
| Primary workbench | `apps/web` is the React/TanStack Start workbench using Vite/Vitest and TanStack Router/Query/Table/Virtual/Form. |

## Manifest-Backed Libraries

| Manifest area | Libraries and tools to expect |
|---|---|
| Go module | `github.com/a-h/templ`, `github.com/go-chi/chi/v5`, `modernc.org/sqlite`, and migration tooling including `github.com/pressly/goose/v3`. |
| Root package | `tailwindcss`, `@tailwindcss/cli`, `esbuild`, `typescript`, `htmx.org`, `alpinejs`, and `concurrently`. |
| `apps/web` package | React, React DOM, TanStack Start/Router/Query/Table/Virtual/Form, Vite, Vitest, Radix UI, lucide-react, shadcn-style components, zod, and Tailwind. |
| Validation scripts | Root `npm run build` builds legacy CSS/JS; `npm run build:web` delegates to `apps/web`; `make validate` runs `scripts/validate.sh`. |

## What We Deliberately Do NOT Use

- Do not introduce Echo, Gin, Fiber, or another Go web framework for normal API work.
- Do not introduce a new SPA framework; `apps/web` is already React/TanStack Start, and root UI remains templ/htmx/Alpine.
- Do not bypass sqlc by hand-writing generated Go query wrappers.
- Do not store large run artifacts in SQLite when filesystem-backed artifact storage is already the pattern.

## Version Constraints

- `apps/web` package versions are mostly `"latest"`; avoid making assumptions from memory. Inspect lockfiles/current install if version-specific behavior matters.
- Root and `apps/web` have separate `package.json` files and separate build/test scripts.
