#!/usr/bin/env bash
set -euo pipefail

export PATH=$PATH:/mnt/c/Users/trist/go/bin:$HOME/go/bin

validation_tmpdir="$(mktemp -d)"
trap 'rm -rf "$validation_tmpdir"' EXIT

if command -v sqlc &> /dev/null; then
  sqlc generate
elif command -v sqlc.exe &> /dev/null; then
  sqlc.exe generate
else
  echo "sqlc not found"
  exit 1
fi

go test ./internal/speccompiler ./internal/store/workflow ./internal/repos/workflow ./internal/artifacts/workflow ./internal/app/submissions ./internal/app/projects/workflow ./internal/app/plans/workflow ./internal/app/runs/workflow ./internal/app/audits ./internal/executor ./internal/api/... ./internal/server ./internal/mcp ./cmd/mcp-smoke
npm run test:local-scripts

for profile in planner auditor local_operator; do
  RELAY_WORKFLOW_DB_PATH="$validation_tmpdir/$profile.sqlite" \
    RELAY_WORKFLOW_ARTIFACTS_DIR="$validation_tmpdir/$profile-artifacts" \
    RELAY_MCP_PROFILE="$profile" \
    node scripts/local/relay-mcp-stdio.mjs --self-test
done

go run ./cmd/mcp-smoke
npm --prefix apps/web run typecheck
npm --prefix apps/web run test -- --run
npm --prefix apps/web run build
go test ./...
go vet ./...
git diff --check

echo "workflow release smoke passed"
