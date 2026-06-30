# Latest Relay Validation Report (tier)

- status: failed
- validation_tier: full
- validation_scope: tier
- base_commit: 212902fa0b95566cd523e2bca21ed282e5b13ad6
- validated_source_snapshot: 38008ccea1b324b42eeb8caeb191285506986279b5b10412fc5994a47c47b116
- worktree_dirty: true
- created_at: 2026-06-30T23:51:12Z

## Validated source changes

- A cmd/relay-closeout/main.go
- A handoffs/closeout/2026-06-30_repo-owned-closeout-command.closeout-evidence.json
- A handoffs/closeout/2026-06-30_repo-owned-closeout-command.closeout-evidence.md
- A internal/artifacts/paths_closeout_test.go
- M internal/artifacts/paths.go
- A internal/closeout/closeout_test.go
- A internal/closeout/closeout.go
- M Makefile
- M scripts/validate.sh

## Commands

| Step | Name | Command | Exit | Status |
|---:|---|---|---:|---|
| 1 | `go-fmt-agentrefs-executor` | `go fmt ./cmd/agentrefs ./internal/agentrefs ./internal/executor` | 0 | passed |
| 2 | `go-test-agentrefs` | `go test ./internal/agentrefs/... ./cmd/agentrefs/...` | 0 | passed |
| 3 | `agentrefs-check` | `go run ./cmd/agentrefs check` | 1 | failed |
| 4 | `go-test-executor` | `go test ./internal/executor/...` | 0 | passed |
| 5 | `go-test-all` | `go test ./...` | 0 | passed |
| 6 | `web-typecheck` | `cd apps/web && npm run typecheck` | 0 | passed |
| 7 | `web-test` | `cd apps/web && npm run test` | 0 | passed |
| 8 | `web-build` | `cd apps/web && npm run build` | 0 | passed |
| 9 | `no-root-agentrefs-exe` | `test ! -e agentrefs.exe` | 0 | passed |

## Failure output tails

### agentrefs-check

```text
$ go run ./cmd/agentrefs check
docs/generated/agent-references/index.json: stale
docs/generated/agent-references/index.md: stale
docs/generated/agent-references/backend-surface.json: stale
docs/generated/agent-references/backend-surface.md: stale
docs/generated/agent-references/mcp-surface.json: stale
docs/generated/agent-references/mcp-surface.md: stale
2026/06/30 19:51:17 found 6 stale or missing output(s)
exit status 1
exit_code: 1

```

