# Latest Relay Validation Report (tier)

- status: passed
- validation_tier: full
- validation_scope: tier
- base_commit: 274128577dc18856ecddbb0bff10dd37b7043a7e
- validated_source_snapshot: 202ef3d0cf49d575d9dbc67a8845f12ee18b28c61218180bb0b55be938058f98
- worktree_dirty: true
- created_at: 2026-07-02T23:32:48Z

## Validated source changes

- M internal/intake/association.go
- M internal/intake/service_test.go
- M internal/intake/service.go
- M internal/mcp/blocker_envelope_test.go
- M internal/mcp/tool_create_run_test.go
- M internal/mcp/tool_create_run.go
- M internal/mcp/tool_validate_planner_handoff.go
- ?? internal/pathsafety/path_safety_test.go
- M internal/pathsafety/path_safety.go

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

