# Latest Relay Validation Report

- status: passed
- base_commit: b97823d9fa53e8d2d37e9ad50545b60b0df6e6e9
- validated_source_snapshot: 8a8dfc53c28a2c73e0c8a1c326b3f0b5e1409933ec474ef94c7fc4877645b496
- worktree_dirty: true
- created_at: 2026-06-28T00:04:46Z

## Validated source changes

- M AGENTS.md
- M docs/agent-reference.md
- M docs/backend-code-surface-map.md
- M docs/generated/agent-references/backend-surface.json
- M docs/generated/agent-references/backend-surface.md
- M docs/generated/agent-references/index.json
- M docs/generated/agent-references/index.md
- ?? internal/agentrefs/docs_integration_test.go
- M internal/instructions/assets/AGENTS.md

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

