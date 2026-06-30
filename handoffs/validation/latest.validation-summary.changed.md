# Latest Relay Validation Report (changed)

- status: passed
- validation_tier: affected
- validation_scope: changed
- base_commit: 4354dfbd0edc327088bb59582bf278f1e0823660
- validated_source_snapshot: 29f7cac405b8bdbfb5494906fcc232b395ae21c4221af54da24e4deedb5675e5
- worktree_dirty: true
- created_at: 2026-06-30T10:04:35Z

## Affected paths

- docs/generated/agent-references/backend-surface.json
- docs/generated/agent-references/backend-surface.md
- docs/generated/agent-references/index.json
- docs/generated/agent-references/index.md
- docs/generated/agent-references/mcp-surface.json
- docs/generated/agent-references/mcp-surface.md
- docs/generated/agent-references/workflow-surfaces.json
- docs/generated/agent-references/workflow-surfaces.md
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

