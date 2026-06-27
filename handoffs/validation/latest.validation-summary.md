# Latest Relay Validation Report

- status: passed
- revision: PASS-004 executor-routing/storage/validation corrections v2
- base_commit: 18ba130
- worktree_dirty: true
- created_at: 2026-06-27T18:00:00Z

## Validated source changes

- M relay-contracts/schema/canonical_packet.schema.json
- M relay-contracts/contracts/planner_to_compiler_contract.md
- M internal/executor/adapter.go
- M internal/app/intake/service.go
- M internal/app/intake/types.go
- M internal/app/intake/helpers_test.go
- M internal/agentrefs/agentrefs_test.go
- M docs/generated/agent-references/storage-surface.json
- M docs/generated/agent-references/storage-surface.md
- M docs/generated/agent-references/backend-surface.json
- M docs/generated/agent-references/backend-surface.md
- M docs/generated/agent-references/workflow-surfaces.json
- M docs/generated/agent-references/workflow-surfaces.md
- M docs/generated/agent-references/index.json
- M docs/generated/agent-references/index.md
- M handoffs/validation/latest.validation-summary.md

## Commands

| Step | Name | Exit | Status |
|---:|---|---:|---|
| V1 | `go test ./internal/app/intake/... ./internal/agentrefs/... ./cmd/agentrefs/...` | 0 | passed |
| V2 | `make agentrefs-check` | 0 | passed |
| V3 | `go test ./...` | 0 | passed |
| V4 | `cd apps/web && npm run typecheck` | 0 | passed |
| V5 | `cd apps/web && npm run test` | 0 | passed |
| V6 | `cd apps/web && npm run build` | 0 | passed |
| V7 | `test ! -e agentrefs.exe` | 0 | passed |

## Failure output tails

No command failures captured.
