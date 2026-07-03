# Latest Relay Validation Report (tier)

- status: passed
- validation_tier: full
- validation_scope: tier
- base_commit: 86895da16bd982b5240c8ab773ad76fabe57663c
- validated_source_snapshot: beb92b78cb504c24d0d2228bd340d795a423b1c7022818bc229e97505ef89d2d
- worktree_dirty: true
- created_at: 2026-07-03T00:32:07Z

## Validated source changes

- M cmd/mcp-smoke/main.go
- M docs/generated/agent-references/backend-surface.json
- M docs/generated/agent-references/backend-surface.md
- M docs/generated/agent-references/index.json
- M docs/generated/agent-references/index.md
- M docs/generated/agent-references/mcp-surface.json
- M docs/generated/agent-references/mcp-surface.md
- M docs/generated/agent-references/workflow-surfaces.json
- M docs/generated/agent-references/workflow-surfaces.md
- M docs/mcp.md
- M docs/project-orchestrator-workflow.md
- M internal/mcp/mcp_test.go
- M Makefile
- M scripts/release-smoke.sh

## Commands

| Step | Name | Command | Exit | Status |
|---:|---|---|---:|---|
| 1 | `go-fmt-agentrefs-executor` | `go fmt ./cmd/agentrefs ./internal/agentrefs ./internal/executor` | 0 | passed |
| 2 | `go-test-agentrefs` | `go test ./internal/agentrefs/... ./cmd/agentrefs/...` | 0 | passed |
| 3 | `go-test-executor` | `go test ./internal/executor/...` | 0 | passed |
| 4 | `go-test-all` | `go test ./...` | 0 | passed |
| 5 | `web-typecheck` | `cd apps/web && npm run typecheck` | 0 | passed |
| 6 | `web-test` | `cd apps/web && npm run test` | 0 | passed |
| 7 | `web-build` | `cd apps/web && npm run build` | 0 | passed |
| 8 | `no-root-agentrefs-exe` | `test ! -e agentrefs.exe` | 0 | passed |

## Failure output tails

No command failures captured.

