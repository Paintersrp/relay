# Latest Relay Validation Report

- status: passed
- base_commit: 01695bd8c8ce2c0d6ec88321b84b94b77ab2e7fe
- validated_source_snapshot: d5362b2ffeab99c3cd2f18bacfc70dd5dbbd0d288da3285217a399215663308f
- worktree_dirty: false
- created_at: 2026-06-27T23:14:44Z

## Validated source changes

- M apps/web/src/routes/runs/$runId/execute.test.tsx
- M apps/web/src/routes/runs/$runId/execute.tsx
- M cmd/agentrefs/main.go
- A docs/generated/agent-references/http-api-surface.json
- A docs/generated/agent-references/http-api-surface.md
- M docs/generated/agent-references/index.json
- M docs/generated/agent-references/index.md
- A internal/agentrefs/http_api.go
- A internal/agentrefs/http_api_test.go
- M internal/agentrefs/paths.go
- M internal/executor/executor.go
- A internal/executor/progress_parser.go
- A internal/executor/progress_parser_test.go

## Commands

| Step | Name | Exit | Status |
|---:|---|---:|---|
| 1 | `go-test-agentrefs` | 0 | passed |
| 2 | `make-agentrefs-check` | 0 | passed |
| 3 | `go-test-executor` | 0 | passed |
| 4 | `go-test-all` | 0 | passed |
| 5 | `web-typecheck` | 0 | passed |
| 6 | `web-test` | 0 | passed |
| 7 | `web-build` | 0 | passed |
| 8 | `check-agentrefs-binary` | 0 | passed |

## Failure output tails

No command failures captured.
