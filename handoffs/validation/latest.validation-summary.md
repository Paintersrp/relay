# Latest Relay Validation Report

- status: passed
- base_commit: 63baa608c24458999fc5b3aae5ae331a47d35256
- validated_source_snapshot: 7ae1a7c750c70b4a9d408fc649d94e70acc8861e169f3c76c479e28a1e36753e
- worktree_dirty: true
- created_at: 2026-06-23T21:13:02Z

## Validated source changes

- M apps/web/.env.example
- M internal/db/db_test.go
- ?? internal/server/routes_compatibility_test.go
- M internal/store/db.go
- ?? internal/store/migration_compatibility_test.go
- ?? scripts/release-smoke.sh

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

