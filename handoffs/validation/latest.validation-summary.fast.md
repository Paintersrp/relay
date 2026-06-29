# Latest Relay Validation Report (fast)

- status: passed
- validation_tier: fast
- base_commit: 7aeec7a4ade0f49d828ef56a5343e31c6f93ddcf
- validated_source_snapshot: c00cc9b046e9c28b5fcb135a08ade90b7fad013f2f8805dd9f76a987ec05b4a6
- worktree_dirty: true
- created_at: 2026-06-29T02:25:11Z

## Validated source changes

- M docs/generated/agent-references/index.json
- M docs/generated/agent-references/index.md
- M handoffs/validation/latest.validation-report.broad.json
- M handoffs/validation/latest.validation-summary.broad.md
- M internal/compiler/compiler_test.go
- M internal/compiler/testdata/current_template_compiler_input_handoff.md
- M internal/executor/progress_parser.go
- M internal/renderer/renderer_test.go
- M internal/renderer/renderer.go
- M relay-contracts

## Commands

| Step | Name | Exit | Status |
|---:|---|---:|---|
| 1 | `go-fmt-agentrefs-executor` | 0 | passed |
| 2 | `go-test-agentrefs` | 0 | passed |
| 3 | `agentrefs-check` | 0 | passed |
| 4 | `go-test-executor` | 0 | passed |

## Failure output tails

No command failures captured.

