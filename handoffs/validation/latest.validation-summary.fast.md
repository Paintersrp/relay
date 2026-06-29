# Latest Relay Validation Report (fast)

- status: failed
- validation_tier: fast
- base_commit: 42dda0e5aa74a3b390ddf199bb72c7ba0e850ee4
- validated_source_snapshot: f925a44c682e323a1ded4f5d05e1e52105552d92e072c1e0007ffcb43288bb4c
- worktree_dirty: true
- created_at: 2026-06-29T02:03:33Z

## Validated source changes

- M internal/executor/executor_test.go
- M internal/executor/executor.go

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
2026/06/28 22:03:36 found 2 stale or missing output(s)
exit status 1
exit_code: 1

```

### go-test-executor

```text
2026/06/28 22:03:44 OK   00012_run_submission_provenance.sql (7.66ms)
2026/06/28 22:03:44 OK   00013_project_context_memory.sql (9.44ms)
2026/06/28 22:03:44 OK   00014_context_packets_schema_repair.go (6.93ms)
2026/06/28 22:03:44 OK   00015_local_audits.sql (8.99ms)
2026/06/28 22:03:44 OK   00016_project_owned_plans.sql (17.8ms)
2026/06/28 22:03:44 OK   00017_refactor_backlog.sql (12.7ms)
2026/06/28 22:03:44 OK   20260624000200_plan_attempts_intent_drift.sql (16.64ms)
2026/06/28 22:03:45 OK   20260626000500_plan_review_settings.sql (7.82ms)
2026/06/28 22:03:45 OK   20260627000100_plan_seeds.sql (9.89ms)
2026/06/28 22:03:45 goose: successfully migrated database to version: 20260627000100
time=2026-06-28T22:03:45.054-04:00 level=INFO msg="executor: dispatching from executor_brief.md" run_id=1 exec_id=1 model=""
2026/06/28 22:03:45 OK   00001_init.sql (7.66ms)
2026/06/28 22:03:45 OK   00002_repo_roots.sql (7.74ms)
2026/06/28 22:03:45 OK   00003_agent_executions.sql (7.19ms)
2026/06/28 22:03:45 OK   00004_validation_executions.sql (7.46ms)
2026/06/28 22:03:45 OK   00005_add_executor_adapter_to_runs.sql (8.07ms)
2026/06/28 22:03:45 OK   00006_create_plans_and_plan_passes.sql (8.03ms)
2026/06/28 22:03:45 OK   00007_add_run_plan_pass_association.sql (8.73ms)
2026/06/28 22:03:45 OK   00008_projects_registry.sql (8.25ms)
2026/06/28 22:03:45 OK   00009_source_snapshots.sql (9.5ms)
2026/06/28 22:03:45 OK   00010_context_packets.sql (12.98ms)
2026/06/28 22:03:45 OK   00011_plan_v2_fields.sql (16.63ms)
2026/06/28 22:03:45 OK   00012_run_submission_provenance.sql (7.67ms)
2026/06/28 22:03:45 OK   00013_project_context_memory.sql (10.34ms)
2026/06/28 22:03:45 OK   00014_context_packets_schema_repair.go (7.83ms)
2026/06/28 22:03:45 OK   00015_local_audits.sql (8.27ms)
2026/06/28 22:03:45 OK   00016_project_owned_plans.sql (18.17ms)
2026/06/28 22:03:45 OK   00017_refactor_backlog.sql (11.74ms)
2026/06/28 22:03:45 OK   20260624000200_plan_attempts_intent_drift.sql (17.71ms)
2026/06/28 22:03:45 OK   20260626000500_plan_review_settings.sql (7.52ms)
2026/06/28 22:03:45 OK   20260627000100_plan_seeds.sql (8.82ms)
2026/06/28 22:03:45 goose: successfully migrated database to version: 20260627000100
time=2026-06-28T22:03:45.416-04:00 level=INFO msg="executor: dispatching from executor_brief.md" run_id=1 exec_id=1 model=claude-sonnet-4.6
--- FAIL: TestParser_ANSI_PromptPrefix (0.00s)
    progress_parser_test.go:360: event message should not start with >: "> STATUS: DONE"
FAIL
FAIL	relay/internal/executor	7.177s
FAIL
exit_code: 1

```

