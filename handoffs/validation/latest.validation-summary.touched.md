# Latest Relay Validation Report (touched)

- status: failed
- validation_tier: affected
- validation_scope: touched
- base_commit: 212902fa0b95566cd523e2bca21ed282e5b13ad6
- validated_source_snapshot: 75e352884c49ff7f2e842e2a74047a282214ad0364865142fc70156d785e2659
- worktree_dirty: true
- created_at: 2026-06-30T23:53:14Z

## Affected paths

- cmd/relay-closeout/main.go
- internal/artifacts/paths_closeout_test.go
- internal/artifacts/paths.go
- internal/closeout/closeout_test.go
- internal/closeout/closeout.go
- Makefile

Global escalation required: true

## Validated source changes

- A cmd/relay-closeout/main.go
- A handoffs/closeout/2026-06-30_repo-owned-closeout-command-2.closeout-evidence.json
- A handoffs/closeout/2026-06-30_repo-owned-closeout-command-2.closeout-evidence.md
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
| 1 | `validate-script-syntax` | `bash -n scripts/validate.sh` | 0 | passed |
| 2 | `go-fmt-agentrefs-executor` | `go fmt ./cmd/agentrefs ./internal/agentrefs ./internal/executor` | 0 | passed |
| 3 | `go-test-agentrefs` | `go test ./internal/agentrefs/... ./cmd/agentrefs/...` | 0 | passed |
| 4 | `agentrefs-check` | `go run ./cmd/agentrefs check` | 1 | failed |
| 5 | `go-test-executor` | `go test ./internal/executor/...` | 0 | passed |
| 6 | `go-test-all` | `go test ./...` | 0 | passed |
| 7 | `web-typecheck` | `cd apps/web && npm run typecheck` | 0 | passed |
| 8 | `web-test` | `cd apps/web && npm run test` | 0 | passed |

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
2026/06/30 19:53:20 found 6 stale or missing output(s)
exit status 1
exit_code: 1

```

