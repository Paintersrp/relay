# Latest Relay Validation Report (fast)

- status: failed
- validation_tier: fast
- base_commit: b3a4ff4bca6c50969b3d44da848577dc3d9a86a7
- validated_source_snapshot: 04f8359b200a162a967888e4d17ae25c03d4e8ea3562f0e34842348b8a68f519
- worktree_dirty: true
- created_at: 2026-06-29T02:06:01Z

## Validated source changes

- M handoffs/validation/latest.validation-report.broad.json
- M handoffs/validation/latest.validation-summary.broad.md

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
2026/06/28 22:06:06 found 2 stale or missing output(s)
exit status 1
exit_code: 1

```

### go-test-executor

```text
2026/06/28 22:06:14 OK   00012_run_submission_provenance.sql (7.93ms)
2026/06/28 22:06:14 OK   00013_project_context_memory.sql (7.92ms)
2026/06/28 22:06:14 OK   00014_context_packets_schema_repair.go (6.44ms)
2026/06/28 22:06:14 OK   00015_local_audits.sql (7.86ms)
2026/06/28 22:06:14 OK   00016_project_owned_plans.sql (18.14ms)
2026/06/28 22:06:14 OK   00017_refactor_backlog.sql (10.74ms)
2026/06/28 22:06:14 OK   20260624000200_plan_attempts_intent_drift.sql (15.59ms)
2026/06/28 22:06:14 OK   20260626000500_plan_review_settings.sql (7.99ms)
2026/06/28 22:06:14 OK   20260627000100_plan_seeds.sql (8.61ms)
2026/06/28 22:06:14 goose: successfully migrated database to version: 20260627000100
time=2026-06-28T22:06:14.508-04:00 level=INFO msg="executor: dispatching from executor_brief.md" run_id=1 exec_id=1 model=""
2026/06/28 22:06:14 OK   00001_init.sql (8.52ms)
2026/06/28 22:06:14 OK   00002_repo_roots.sql (10ms)
2026/06/28 22:06:14 OK   00003_agent_executions.sql (7.4ms)
2026/06/28 22:06:14 OK   00004_validation_executions.sql (7.37ms)
2026/06/28 22:06:14 OK   00005_add_executor_adapter_to_runs.sql (7.56ms)
2026/06/28 22:06:14 OK   00006_create_plans_and_plan_passes.sql (8.45ms)
2026/06/28 22:06:14 OK   00007_add_run_plan_pass_association.sql (8.49ms)
2026/06/28 22:06:14 OK   00008_projects_registry.sql (7.78ms)
2026/06/28 22:06:14 OK   00009_source_snapshots.sql (9.67ms)
2026/06/28 22:06:14 OK   00010_context_packets.sql (9.1ms)
2026/06/28 22:06:14 OK   00011_plan_v2_fields.sql (16.6ms)
2026/06/28 22:06:14 OK   00012_run_submission_provenance.sql (8.18ms)
2026/06/28 22:06:14 OK   00013_project_context_memory.sql (9ms)
2026/06/28 22:06:14 OK   00014_context_packets_schema_repair.go (6ms)
2026/06/28 22:06:14 OK   00015_local_audits.sql (6.99ms)
2026/06/28 22:06:14 OK   00016_project_owned_plans.sql (15.51ms)
2026/06/28 22:06:14 OK   00017_refactor_backlog.sql (10.92ms)
2026/06/28 22:06:14 OK   20260624000200_plan_attempts_intent_drift.sql (17.82ms)
2026/06/28 22:06:14 OK   20260626000500_plan_review_settings.sql (8.44ms)
2026/06/28 22:06:14 OK   20260627000100_plan_seeds.sql (8.48ms)
2026/06/28 22:06:14 goose: successfully migrated database to version: 20260627000100
time=2026-06-28T22:06:14.857-04:00 level=INFO msg="executor: dispatching from executor_brief.md" run_id=1 exec_id=1 model=claude-sonnet-4.6
--- FAIL: TestParser_ANSI_PromptPrefix (0.00s)
    progress_parser_test.go:360: event message should not start with >: "> STATUS: DONE"
FAIL
FAIL	relay/internal/executor	6.611s
FAIL
exit_code: 1

```

