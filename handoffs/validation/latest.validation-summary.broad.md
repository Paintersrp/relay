# Latest Relay Validation Report (broad)

- status: failed
- validation_tier: broad
- base_commit: 7aeec7a4ade0f49d828ef56a5343e31c6f93ddcf
- validated_source_snapshot: c779c6ab867c6c09d0b9d7ee53a1021219de4590d449b52bc22e617d21743baf
- worktree_dirty: false
- created_at: 2026-06-29T02:06:25Z

## Validated source changes

No source changes relative to base commit.

## Commands

| Step | Name | Exit | Status |
|---:|---|---:|---|
| 1 | `go-fmt-agentrefs-executor` | 0 | passed |
| 2 | `go-test-agentrefs` | 0 | passed |
| 3 | `agentrefs-check` | 1 | failed |
| 4 | `go-test-executor` | 1 | failed |
| 5 | `go-test-all` | 1 | failed |
| 6 | `web-typecheck` | 0 | passed |
| 7 | `web-test` | 0 | passed |

## Failure output tails

### agentrefs-check

```text
$ go run ./cmd/agentrefs check
docs/generated/agent-references/index.json: stale
docs/generated/agent-references/index.md: stale
2026/06/28 22:06:27 found 2 stale or missing output(s)
exit status 1
exit_code: 1

```

### go-test-executor

```text
2026/06/28 22:06:37 OK   00012_run_submission_provenance.sql (8.6ms)
2026/06/28 22:06:37 OK   00013_project_context_memory.sql (10.25ms)
2026/06/28 22:06:37 OK   00014_context_packets_schema_repair.go (7.73ms)
2026/06/28 22:06:37 OK   00015_local_audits.sql (12.48ms)
2026/06/28 22:06:37 OK   00016_project_owned_plans.sql (21.93ms)
2026/06/28 22:06:37 OK   00017_refactor_backlog.sql (15.65ms)
2026/06/28 22:06:37 OK   20260624000200_plan_attempts_intent_drift.sql (19.67ms)
2026/06/28 22:06:37 OK   20260626000500_plan_review_settings.sql (12.55ms)
2026/06/28 22:06:37 OK   20260627000100_plan_seeds.sql (12.16ms)
2026/06/28 22:06:37 goose: successfully migrated database to version: 20260627000100
time=2026-06-28T22:06:37.550-04:00 level=INFO msg="executor: dispatching from executor_brief.md" run_id=1 exec_id=1 model=""
2026/06/28 22:06:38 OK   00001_init.sql (10ms)
2026/06/28 22:06:38 OK   00002_repo_roots.sql (9ms)
2026/06/28 22:06:38 OK   00003_agent_executions.sql (7ms)
2026/06/28 22:06:38 OK   00004_validation_executions.sql (8ms)
2026/06/28 22:06:38 OK   00005_add_executor_adapter_to_runs.sql (7ms)
2026/06/28 22:06:38 OK   00006_create_plans_and_plan_passes.sql (8.33ms)
2026/06/28 22:06:38 OK   00007_add_run_plan_pass_association.sql (9.61ms)
2026/06/28 22:06:38 OK   00008_projects_registry.sql (9.03ms)
2026/06/28 22:06:38 OK   00009_source_snapshots.sql (11.52ms)
2026/06/28 22:06:38 OK   00010_context_packets.sql (9.37ms)
2026/06/28 22:06:38 OK   00011_plan_v2_fields.sql (18.54ms)
2026/06/28 22:06:38 OK   00012_run_submission_provenance.sql (10.46ms)
2026/06/28 22:06:38 OK   00013_project_context_memory.sql (10.37ms)
2026/06/28 22:06:38 OK   00014_context_packets_schema_repair.go (9.57ms)
2026/06/28 22:06:38 OK   00015_local_audits.sql (14.08ms)
2026/06/28 22:06:38 OK   00016_project_owned_plans.sql (24.68ms)
2026/06/28 22:06:38 OK   00017_refactor_backlog.sql (15.87ms)
2026/06/28 22:06:38 OK   20260624000200_plan_attempts_intent_drift.sql (20.07ms)
2026/06/28 22:06:38 OK   20260626000500_plan_review_settings.sql (10.33ms)
2026/06/28 22:06:38 OK   20260627000100_plan_seeds.sql (9.77ms)
2026/06/28 22:06:38 goose: successfully migrated database to version: 20260627000100
time=2026-06-28T22:06:38.504-04:00 level=INFO msg="executor: dispatching from executor_brief.md" run_id=1 exec_id=1 model=claude-sonnet-4.6
--- FAIL: TestParser_ANSI_PromptPrefix (0.00s)
    progress_parser_test.go:360: event message should not start with >: "> STATUS: DONE"
FAIL
FAIL	relay/internal/executor	9.346s
FAIL
exit_code: 1

```

### go-test-all

```text
2026/06/28 22:06:59 OK   00008_projects_registry.sql (13.65ms)
2026/06/28 22:06:59 OK   00009_source_snapshots.sql (13.74ms)
2026/06/28 22:06:59 OK   00010_context_packets.sql (12.11ms)
2026/06/28 22:06:59 OK   00011_plan_v2_fields.sql (21.67ms)
2026/06/28 22:07:00 OK   00012_run_submission_provenance.sql (230.61ms)
2026/06/28 22:07:00 OK   00013_project_context_memory.sql (11.71ms)
2026/06/28 22:07:00 OK   00014_context_packets_schema_repair.go (9.58ms)
2026/06/28 22:07:00 OK   00015_local_audits.sql (9.17ms)
2026/06/28 22:07:00 OK   00016_project_owned_plans.sql (29.82ms)
2026/06/28 22:07:00 OK   00017_refactor_backlog.sql (16.25ms)
2026/06/28 22:07:00 OK   20260624000200_plan_attempts_intent_drift.sql (21.23ms)
2026/06/28 22:07:00 OK   20260626000500_plan_review_settings.sql (9.87ms)
2026/06/28 22:07:00 OK   20260627000100_plan_seeds.sql (11.09ms)
2026/06/28 22:07:00 goose: successfully migrated database to version: 20260627000100
time=2026-06-28T22:07:00.390-04:00 level=INFO msg="executor: dispatching from executor_brief.md" run_id=1 exec_id=1 model=claude-sonnet-4.6
--- FAIL: TestParser_ANSI_PromptPrefix (0.00s)
    progress_parser_test.go:360: event message should not start with >: "> STATUS: DONE"
FAIL
FAIL	relay/internal/executor	12.250s
ok  	relay/internal/handlers	9.569s
ok  	relay/internal/instructions	(cached)
ok  	relay/internal/intake	(cached)
ok  	relay/internal/mcp	(cached)
ok  	relay/internal/pipeline	(cached)
ok  	relay/internal/projectmemory	(cached)
ok  	relay/internal/refactors	(cached)
ok  	relay/internal/renderer	(cached)
ok  	relay/internal/repairer	3.122s
ok  	relay/internal/repos	(cached)
ok  	relay/internal/server	(cached)
ok  	relay/internal/smoke	(cached)
ok  	relay/internal/sources	(cached)
ok  	relay/internal/store	(cached)
?   	relay/internal/store/generated	[no test files]
ok  	relay/internal/validation	(cached)
ok  	relay/internal/validationrunner	(cached)
ok  	relay/internal/views	(cached)
FAIL
exit_code: 1

```

