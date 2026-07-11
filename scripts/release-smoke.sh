#!/usr/bin/env bash
set -euo pipefail

export PATH=$PATH:/mnt/c/Users/trist/go/bin:$HOME/go/bin

if command -v sqlc &> /dev/null; then
  sqlc generate
elif command -v sqlc.exe &> /dev/null; then
  sqlc.exe generate
else
  echo "sqlc not found"
  exit 1
fi

go test ./internal/speccompiler ./internal/store/workflow ./internal/repos/workflow ./internal/artifacts/workflow ./internal/app/submissions ./internal/app/projects/workflow ./internal/app/plans/workflow ./internal/app/runs/workflow ./internal/app/audits ./internal/executor ./internal/api/... ./internal/server ./internal/mcp ./cmd/mcp-smoke
go run ./cmd/mcp-smoke
npm --prefix apps/web run typecheck
npm --prefix apps/web run test -- --run
npm --prefix apps/web run build
go test ./...
go vet ./...
git diff --check

echo "workflow release smoke passed"
