# Latest Relay Validation Report (fast)

- status: failed
- validation_tier: fast
- base_commit: fb08b7a0c3c77ae4699e76875e32bc417f4c66f2
- validated_source_snapshot: 6a85f279a3dab51547f9fdbe66471ea28abb6ebbc51190d0ac3ca1e5c292e0ca
- worktree_dirty: true
- created_at: 2026-06-29T02:02:42Z

## Validated source changes

- A handoffs/validation/latest.validation-report.broad.json
- A handoffs/validation/latest.validation-summary.broad.md
- M internal/artifacts/paths.go
- M internal/executor/executor_test.go
- M internal/executor/executor.go
- M internal/instructions/assets/AGENTS.md

## Commands

| Step | Name | Exit | Status |
|---:|---|---:|---|
| 1 | `go-fmt-agentrefs-executor` | 0 | passed |
| 2 | `go-test-agentrefs` | 0 | passed |
| 3 | `agentrefs-check` | 1 | failed |
| 4 | `go-test-executor` | 1 | failed |

## Failure output tails

### agentrefs-check

```text
$ go run ./cmd/agentrefs check
docs/generated/agent-references/index.json: stale
docs/generated/agent-references/index.md: stale
2026/06/28 22:02:49 found 2 stale or missing output(s)
exit status 1
exit_code: 1

```

### go-test-executor

```text
2026/06/28 22:02:58 OK   00012_run_submission_provenance.sql (7.27ms)
2026/06/28 22:02:58 OK   00013_project_context_memory.sql (9.14ms)
2026/06/28 22:02:58 OK   00014_context_packets_schema_repair.go (6.09ms)
2026/06/28 22:02:58 OK   00015_local_audits.sql (8.38ms)
2026/06/28 22:02:58 OK   00016_project_owned_plans.sql (17.19ms)
2026/06/28 22:02:58 OK   00017_refactor_backlog.sql (10.77ms)
2026/06/28 22:02:58 OK   20260624000200_plan_attempts_intent_drift.sql (19.88ms)
2026/06/28 22:02:58 OK   20260626000500_plan_review_settings.sql (9.04ms)
2026/06/28 22:02:58 OK   20260627000100_plan_seeds.sql (7.72ms)
2026/06/28 22:02:58 goose: successfully migrated database to version: 20260627000100
time=2026-06-28T22:02:58.243-04:00 level=INFO msg="executor: dispatching from executor_brief.md" run_id=1 exec_id=1 model=""
2026/06/28 22:02:59 OK   00001_init.sql (10.84ms)
2026/06/28 22:02:59 OK   00002_repo_roots.sql (16.55ms)
2026/06/28 22:02:59 OK   00003_agent_executions.sql (10.6ms)
2026/06/28 22:02:59 OK   00004_validation_executions.sql (13.41ms)
2026/06/28 22:02:59 OK   00005_add_executor_adapter_to_runs.sql (23.19ms)
2026/06/28 22:02:59 OK   00006_create_plans_and_plan_passes.sql (10.61ms)
2026/06/28 22:02:59 OK   00007_add_run_plan_pass_association.sql (19.54ms)
2026/06/28 22:02:59 OK   00008_projects_registry.sql (10.53ms)
2026/06/28 22:02:59 OK   00009_source_snapshots.sql (11.17ms)
2026/06/28 22:03:00 OK   00010_context_packets.sql (11.31ms)
2026/06/28 22:03:00 OK   00011_plan_v2_fields.sql (20.18ms)
2026/06/28 22:03:00 OK   00012_run_submission_provenance.sql (8.09ms)
2026/06/28 22:03:00 OK   00013_project_context_memory.sql (8.66ms)
2026/06/28 22:03:00 OK   00014_context_packets_schema_repair.go (6.97ms)
2026/06/28 22:03:00 OK   00015_local_audits.sql (9.48ms)
2026/06/28 22:03:00 OK   00016_project_owned_plans.sql (16.52ms)
2026/06/28 22:03:00 OK   00017_refactor_backlog.sql (12.04ms)
2026/06/28 22:03:00 OK   20260624000200_plan_attempts_intent_drift.sql (16.18ms)
2026/06/28 22:03:00 OK   20260626000500_plan_review_settings.sql (8.51ms)
2026/06/28 22:03:00 OK   20260627000100_plan_seeds.sql (7.53ms)
2026/06/28 22:03:00 goose: successfully migrated database to version: 20260627000100
time=2026-06-28T22:03:00.163-04:00 level=INFO msg="executor: dispatching from executor_brief.md" run_id=1 exec_id=1 model=claude-sonnet-4.6
--- FAIL: TestParser_ANSI_PromptPrefix (0.00s)
    progress_parser_test.go:360: event message should not start with >: "> STATUS: DONE"
FAIL
FAIL	relay/internal/executor	8.386s
FAIL
exit_code: 1

```

