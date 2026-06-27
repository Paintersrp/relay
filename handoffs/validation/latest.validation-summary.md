# Latest Relay Validation Report

- status: passed
- base_commit: d8e5c26169f9169baf256e663c5dbdac9940b217
- validated_source_snapshot: a96936c6bf4bfcb8a61e7886d8c27c42d130340e4046ff45728645e0aa7064e4
- worktree_dirty: true
- created_at: 2026-06-27T23:02:00Z

## Validated source changes

- M apps/web/src/routes/runs/$runId/execute.test.tsx
- M apps/web/src/routes/runs/$runId/execute.tsx
- M cmd/agentrefs/main.go
- ?? docs/generated/agent-references/http-api-surface.json
- ?? docs/generated/agent-references/http-api-surface.md
- M docs/generated/agent-references/index.json
- M docs/generated/agent-references/index.md
- ?? internal/agentrefs/docs/generated/agent-references/http-api-surface.json
- ?? internal/agentrefs/docs/generated/agent-references/http-api-surface.md
- ?? internal/agentrefs/http_api_test.go
- ?? internal/agentrefs/http_api.go
- M internal/agentrefs/paths.go
- M internal/executor/executor.go
- ?? internal/executor/progress_parser_test.go
- ?? internal/executor/progress_parser.go

## Commands

| Step | Name | Exit | Status |
|---:|---|---:|---|
| 1 | `go-fmt-executor` | 0 | passed |
| 2 | `go-test-executor` | 0 | passed |
| 3 | `go-test-all` | 0 | passed |
| 4 | `web-typecheck` | 0 | passed |
| 5 | `web-build` | 0 | passed |

## Failure output tails

No command failures captured.

