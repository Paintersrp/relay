# Latest Relay Validation Report

- status: passed
- revision: PASS-004 storage/execute contract corrections
- base_commit: 18ba130
- worktree_dirty: true
- created_at: 2026-06-27T17:00:00Z

## Validated source changes

- M docs/generated/agent-references/storage-surface.json
- M docs/generated/agent-references/storage-surface.md
- M docs/generated/agent-references/backend-surface.json
- M docs/generated/agent-references/backend-surface.md
- M internal/agentrefs/agentrefs_test.go
- M internal/agentrefs/storage.go
- M internal/app/intake/helpers_test.go
- M internal/app/intake/service.go
- M relay-contracts/schema/canonical_packet.schema.json
- M relay-contracts/contracts/planner_to_compiler_contract.md

## Commands

| Step | Name | Exit | Status |
|---:|---|---:|---|
| 1 | `go test ./internal/agentrefs/... ./cmd/agentrefs/...` | 0 | passed |
| 2 | `make agentrefs-check` | 0 | passed |
| 3 | `go test ./...` | 0 | passed |
| 4 | `cd apps/web && npm run typecheck` | 0 | passed |
| 5 | `cd apps/web && npm run test` | 0 | passed |
| 6 | `cd apps/web && npm run build` | 0 | passed |
| 7 | `test ! -e agentrefs.exe` | 0 | passed |
| 8 | schema dynamic-route smoke | 0 | passed |

## Failure output tails

No command failures captured.
