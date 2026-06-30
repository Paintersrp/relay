# Latest Relay Validation Report (touched)

- status: passed
- validation_tier: affected
- validation_scope: touched
- base_commit: 4354dfbd0edc327088bb59582bf278f1e0823660
- validated_source_snapshot: 0cad98189af7e6efda5e415585a7585b8454210951f9385a8c1cb4db8907e37d
- worktree_dirty: true
- created_at: 2026-06-30T10:03:49Z

## Affected paths

- apps/web/package.json
- Makefile
- scripts/validate.sh

Global escalation required: true

## Validated source changes

- M docs/generated/agent-references/backend-surface.json
- M docs/generated/agent-references/backend-surface.md
- M docs/generated/agent-references/index.json
- M docs/generated/agent-references/index.md
- M docs/generated/agent-references/mcp-surface.json
- M docs/generated/agent-references/mcp-surface.md
- M docs/generated/agent-references/workflow-surfaces.json
- M docs/generated/agent-references/workflow-surfaces.md
- M scripts/validate.sh

## Commands

| Step | Name | Command | Exit | Status |
|---:|---|---|---:|---|
| 1 | `validate-script-syntax` | `bash -n scripts/validate.sh` | 0 | passed |
| 2 | `go-fmt-agentrefs-executor` | `go fmt ./cmd/agentrefs ./internal/agentrefs ./internal/executor` | 0 | passed |
| 3 | `go-test-agentrefs` | `go test ./internal/agentrefs/... ./cmd/agentrefs/...` | 0 | passed |
| 4 | `agentrefs-check` | `go run ./cmd/agentrefs check` | 0 | passed |
| 5 | `go-test-executor` | `go test ./internal/executor/...` | 0 | passed |
| 6 | `go-test-all` | `go test ./...` | 0 | passed |
| 7 | `web-typecheck` | `cd apps/web && npm run typecheck` | 0 | passed |
| 8 | `web-test` | `cd apps/web && npm run test` | 0 | passed |

## Failure output tails

No command failures captured.

