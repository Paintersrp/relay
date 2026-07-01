# Latest Relay Validation Report (tier)

- status: passed
- validation_tier: full
- validation_scope: tier
- base_commit: c9165cb93b09c284b5e67ddb6a4b627e5116816d
- validated_source_snapshot: 181da1f519e5e92736d020da3c581fe71e1445eb3cf7e952383561ae108b8d30
- worktree_dirty: true
- created_at: 2026-07-01T01:22:58Z

## Validated source changes

- M cmd/relay-closeout/main.go
- M internal/artifacts/paths_closeout_test.go
- M internal/artifacts/paths.go
- M internal/closeout/closeout_test.go
- M internal/closeout/closeout.go

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

