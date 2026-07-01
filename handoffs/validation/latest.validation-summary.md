# Latest Relay Validation Report (tier)

- status: passed
- validation_tier: full
- validation_scope: tier
- base_commit: 9d8f662c7a976ccb8e0c8eb023a0f56767b5b789
- validated_source_snapshot: 79cfbc37ae26ca14c6211cd957f015b073b78e9091f156b55eb46918d5377241
- worktree_dirty: true
- created_at: 2026-07-01T00:47:41Z

## Validated source changes

- M AGENTS.md
- M cmd/relay-closeout/main.go
- M docs/generated/agent-references/backend-surface.json
- M docs/generated/agent-references/backend-surface.md
- M docs/generated/agent-references/index.json
- M docs/generated/agent-references/index.md
- M docs/generated/agent-references/mcp-surface.json
- M docs/generated/agent-references/mcp-surface.md
- ?? handoffs/closeout/2026-07-01_pass-005-closeout-remediation.closeout-evidence.json
- ?? handoffs/closeout/2026-07-01_pass-005-closeout-remediation.closeout-evidence.md
- M internal/artifacts/paths_closeout_test.go
- M internal/artifacts/paths.go
- M internal/closeout/closeout_test.go
- M internal/closeout/closeout.go
- M internal/instructions/assets/AGENTS.md
- M scripts/validate.sh

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

