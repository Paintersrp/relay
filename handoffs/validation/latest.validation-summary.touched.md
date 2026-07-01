# Latest Relay Validation Report (touched)

- status: passed
- validation_tier: affected
- validation_scope: touched
- base_commit: 041c6f021eec3477484d881087d41b8ed5eec9fa
- validated_source_snapshot: ebbd38f2ccb809dae131e0126de9ce24cac9e7b8cb53ddeb4962bb134c31ce43
- worktree_dirty: true
- created_at: 2026-07-01T01:30:18Z

## Affected paths

- docs/operator-guide.md
- Makefile
- README.md

Global escalation required: true

## Validated source changes

- M docs/operator-guide.md
- M Makefile
- M README.md

## Commands

| Step | Name | Command | Exit | Status |
|---:|---|---|---:|---|
| 1 | `validate-script-syntax` | `bash -n scripts/validate.sh` | 0 | passed |
| 2 | `go-fmt-agentrefs-executor` | `go fmt ./cmd/agentrefs ./internal/agentrefs ./internal/executor` | 0 | passed |
| 3 | `go-test-agentrefs` | `go test ./internal/agentrefs/... ./cmd/agentrefs/...` | 0 | passed |
| 4 | `go-test-executor` | `go test ./internal/executor/...` | 0 | passed |
| 5 | `go-test-all` | `go test ./...` | 0 | passed |
| 6 | `web-typecheck` | `cd apps/web && npm run typecheck` | 0 | passed |
| 7 | `web-test` | `cd apps/web && npm run test` | 0 | passed |

## Failure output tails

No command failures captured.

