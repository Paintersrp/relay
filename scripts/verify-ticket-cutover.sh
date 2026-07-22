#!/usr/bin/env bash
set -euo pipefail

echo "=== SQL generation and migration ==="
sqlc generate
git diff --check
go test ./internal/db/... ./internal/store/workflow/...

echo "=== Cutover configuration and lifecycle ==="
go test ./internal/app/cutover/... ./internal/app/operations/... ./internal/api/cutover/...

echo "=== Run boundary transaction ==="
go test ./internal/app/runs/workflow/...

echo "=== Aggregate closure and seven-route admission ==="
go test ./internal/server/... ./internal/mcp/... ./internal/transport/mcpingress/...

echo "=== Historical read regressions ==="
go test ./internal/app/audits/... ./internal/sourcegateway/... ./internal/sourcevault/...

echo "=== Gateway cutover verification complete ==="
