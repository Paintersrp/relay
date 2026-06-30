# Latest Relay Validation Report (touched)

- status: passed
- validation_tier: affected
- validation_scope: touched
- base_commit: f426b410f8ec743720038382e45301b980379154
- validated_source_snapshot: 56f7d842ea2310077015a4f363cc00b4f2b02bce2ffadcbaf1ea49210bda2747
- worktree_dirty: true
- created_at: 2026-06-30T22:44:47Z

## Affected paths

- cmd/mcp-smoke/main.go
- docs/mcp.md
- internal/intake/service.go
- internal/mcp/mcp_test.go
- internal/mcp/server.go
- internal/mcp/tool_create_run.go
- relay-contracts/contracts/planner_mcp_context_broker_contract.md
- relay-contracts/contracts/planner_mcp_run_submission_contract.md

Global escalation required: false

## Validated source changes

- M cmd/mcp-smoke/main.go
- M docs/mcp.md
- M internal/intake/service.go
- M internal/mcp/mcp_test.go
- M internal/mcp/plan_attempt_tools_test.go
- M internal/mcp/server.go
- M internal/mcp/tool_create_run.go
- M relay-contracts

## Commands

| Step | Name | Command | Exit | Status |
|---:|---|---|---:|---|
| 1 | `gofmt-touched-files` | `gofmt -w cmd/mcp-smoke/main.go internal/intake/service.go internal/mcp/mcp_test.go internal/mcp/server.go internal/mcp/tool_create_run.go` | 0 | passed |
| 2 | `go-test-affected-packages` | `go test ./cmd/mcp-smoke ./internal/intake ./internal/mcp` | 0 | passed |

## Failure output tails

No command failures captured.

