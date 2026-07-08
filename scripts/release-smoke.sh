#!/usr/bin/env bash
set -euo pipefail

node scripts/final-surface.mjs --check
sqlc generate
go test ./internal/speccompiler ./internal/store/workflow ./internal/repos/workflow ./internal/artifacts/workflow ./internal/app/canonical ./internal/app/projects/workflow ./internal/app/plans/workflow ./internal/app/runs/workflow ./internal/app/audits ./internal/executor ./internal/api/... ./internal/server ./internal/mcp ./cmd/mcp-smoke ./cmd/agentrefs
go run ./cmd/mcp-smoke
go run ./cmd/agentrefs check
npm --prefix apps/web run typecheck
npm --prefix apps/web run test -- --run
npm --prefix apps/web run build
go test ./...
go vet ./...
git diff --check
node scripts/final-surface.mjs --check

echo "canonical release smoke passed"
