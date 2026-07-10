# Latest Relay Validation Report (tier)

- status: passed
- validation_tier: fast
- validation_scope: tier
- base_commit: d714627d25ad7681650b1ff4383839b593b101d4
- validated_source_snapshot: f949f61782c1cc96f5b494df35c32fcf3410ead5209d275865082e8c9d0dc9f0
- worktree_dirty: false
- created_at: 2026-07-08T12:05:53Z

## Validated source changes

No source changes relative to base commit.

## Commands

| Step | Name | Command | Exit | Status |
|---:|---|---|---:|---|
| 1 | `go-fmt-agentrefs-executor` | `go fmt ./cmd/agentrefs ./internal/agentrefs ./internal/executor` | 0 | passed |
| 2 | `go-test-agentrefs` | `go test ./internal/agentrefs/... ./cmd/agentrefs/...` | 0 | passed |
| 3 | `go-test-executor` | `go test ./internal/executor/...` | 0 | passed |

## Failure output tails

No command failures captured.

