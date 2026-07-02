# Latest Relay Validation Report (tier)

- status: passed
- validation_tier: full
- validation_scope: tier
- base_commit: 6fc217fe221a076809304e6010c27a73d11aa6a6
- validated_source_snapshot: d469136f4650c44af8dd6a17a599e8da46a459f71ec1e92c7dfcec825504e25b
- worktree_dirty: true
- created_at: 2026-07-02T22:47:19Z

## Validated source changes

- M docs/mcp.md
- M internal/intake/service_test.go
- M internal/intake/service.go
- M internal/mcp/blocker_envelope_test.go
- M internal/mcp/blocker_envelope.go
- M internal/mcp/context_broker_tools_test.go
- M internal/mcp/mcp_test.go
- M internal/mcp/orchestrator_work_tools_test.go
- M internal/mcp/orchestrator_work_tools.go
- M internal/mcp/tool_create_run_test.go
- M internal/mcp/tool_create_run.go
- M internal/mcp/tool_validate_planner_handoff.go
- ?? internal/pathsafety/path_safety.go
- M relay-contracts

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

