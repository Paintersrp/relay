# Latest Relay Validation Report (touched)

- status: passed
- validation_tier: affected
- validation_scope: touched
- base_commit: fab408c7b3bfdfc223d848ab5bfeb54f6258a9f2
- validated_source_snapshot: f26d9e734c6ff914cf4818de4474d447c97ce3bc36fce834b91b7468cdb7a6f0
- worktree_dirty: true
- created_at: 2026-06-30T22:07:08Z

## Affected paths

- internal/artifacts/paths_test.go
- internal/artifacts/paths.go
- relay-contracts/contracts/pipeline_artifact_model.md
- relay-contracts/contracts/planner_to_compiler_contract.md
- relay-contracts/policies/artifact_naming_policy.md
- relay-contracts/schema/closeout_evidence.schema.json

Global escalation required: false

## Validated source changes

- M internal/artifacts/paths_test.go
- M internal/artifacts/paths.go
- M relay-contracts

## Commands

| Step | Name | Command | Exit | Status |
|---:|---|---|---:|---|
| 1 | `gofmt-touched-files` | `gofmt -w internal/artifacts/paths_test.go internal/artifacts/paths.go` | 0 | passed |
| 2 | `go-test-affected-packages` | `go test ./internal/artifacts` | 0 | passed |

## Failure output tails

No command failures captured.

