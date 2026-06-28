# Latest Relay Validation Report

- status: passed
- base_commit: 01695bd8c8ce2c0d6ec88321b84b94b77ab2e7fe
- validated_source_snapshot: 629d8417943fb2a93018ed26b38671ef086e618dfb85e9c7a974256bd1e7bf7f
- worktree_dirty: true
- created_at: 2026-06-27T23:56:58Z

## Validated source changes

- A cmd/agentrefs/main_test.go
- M cmd/agentrefs/main.go
- A docs/generated/agent-references/frontend-backend-contract.json
- A docs/generated/agent-references/frontend-backend-contract.md
- M docs/generated/agent-references/index.json
- M docs/generated/agent-references/index.md
- M internal/agentrefs/agentrefs_test.go
- A internal/agentrefs/frontend_backend_contract.go
- M internal/agentrefs/http_api.go
- M internal/agentrefs/mcp.go
- M internal/agentrefs/paths.go
- M internal/agentrefs/storage.go
- M internal/agentrefs/types.go
- M package.json
- M scripts/validate.sh

## Commands

| Step | Name | Exit | Status |
|---:|---|---:|---|
| 1 | `go-fmt-agentrefs-executor` | 0 | passed |
| 2 | `go-test-agentrefs` | 0 | passed |
| 3 | `agentrefs-check` | 0 | passed |
| 4 | `go-test-executor` | 0 | passed |
| 5 | `go-test-all` | 0 | passed |
| 6 | `web-typecheck` | 0 | passed |
| 7 | `web-test` | 0 | passed |
| 8 | `web-build` | 0 | passed |
| 9 | `no-root-agentrefs-exe` | 0 | passed |

## Failure output tails

No command failures captured.

