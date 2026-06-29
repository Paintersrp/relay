# Latest Relay Validation Report (broad)

- status: failed
- validation_tier: broad
- base_commit: b3a4ff4bca6c50969b3d44da848577dc3d9a86a7
- validated_source_snapshot: fc015b3d4c15bfac5022c70b8a4d02474fd404cbad6cdecad2fde58c5f39660e
- worktree_dirty: false
- created_at: 2026-06-29T02:03:50Z

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
2026/06/28 22:03:52 found 2 stale or missing output(s)
exit status 1
exit_code: 1

```

### go-test-executor

```text
2026/06/28 22:04:01 OK   00012_run_submission_provenance.sql (7.64ms)
2026/06/28 22:04:01 OK   00013_project_context_memory.sql (8.3ms)
2026/06/28 22:04:01 OK   00014_context_packets_schema_repair.go (7.37ms)
2026/06/28 22:04:01 OK   00015_local_audits.sql (8.36ms)
2026/06/28 22:04:01 OK   00016_project_owned_plans.sql (18.64ms)
2026/06/28 22:04:01 OK   00017_refactor_backlog.sql (11.53ms)
2026/06/28 22:04:01 OK   20260624000200_plan_attempts_intent_drift.sql (17.26ms)
2026/06/28 22:04:01 OK   20260626000500_plan_review_settings.sql (7.98ms)
2026/06/28 22:04:01 OK   20260627000100_plan_seeds.sql (7.89ms)
2026/06/28 22:04:01 goose: successfully migrated database to version: 20260627000100
time=2026-06-28T22:04:01.469-04:00 level=INFO msg="executor: dispatching from executor_brief.md" run_id=1 exec_id=1 model=""
2026/06/28 22:04:01 OK   00001_init.sql (8.52ms)
2026/06/28 22:04:01 OK   00002_repo_roots.sql (7.88ms)
2026/06/28 22:04:01 OK   00003_agent_executions.sql (6.91ms)
2026/06/28 22:04:01 OK   00004_validation_executions.sql (6.86ms)
2026/06/28 22:04:01 OK   00005_add_executor_adapter_to_runs.sql (6.71ms)
2026/06/28 22:04:01 OK   00006_create_plans_and_plan_passes.sql (7.95ms)
2026/06/28 22:04:01 OK   00007_add_run_plan_pass_association.sql (8.92ms)
2026/06/28 22:04:01 OK   00008_projects_registry.sql (8.04ms)
2026/06/28 22:04:01 OK   00009_source_snapshots.sql (9.62ms)
2026/06/28 22:04:01 OK   00010_context_packets.sql (10.2ms)
2026/06/28 22:04:01 OK   00011_plan_v2_fields.sql (14.92ms)
2026/06/28 22:04:01 OK   00012_run_submission_provenance.sql (7.17ms)
2026/06/28 22:04:01 OK   00013_project_context_memory.sql (10.47ms)
2026/06/28 22:04:01 OK   00014_context_packets_schema_repair.go (7.27ms)
2026/06/28 22:04:01 OK   00015_local_audits.sql (9.07ms)
2026/06/28 22:04:01 OK   00016_project_owned_plans.sql (16.8ms)
2026/06/28 22:04:01 OK   00017_refactor_backlog.sql (11.27ms)
2026/06/28 22:04:01 OK   20260624000200_plan_attempts_intent_drift.sql (16.38ms)
2026/06/28 22:04:01 OK   20260626000500_plan_review_settings.sql (8.72ms)
2026/06/28 22:04:01 OK   20260627000100_plan_seeds.sql (7.4ms)
2026/06/28 22:04:01 goose: successfully migrated database to version: 20260627000100
time=2026-06-28T22:04:01.803-04:00 level=INFO msg="executor: dispatching from executor_brief.md" run_id=1 exec_id=1 model=claude-sonnet-4.6
--- FAIL: TestParser_ANSI_PromptPrefix (0.00s)
    progress_parser_test.go:360: event message should not start with >: "> STATUS: DONE"
FAIL
FAIL	relay/internal/executor	7.526s
FAIL
exit_code: 1

```

### go-test-all

```text
2026/06/28 22:04:43 OK   00008_projects_registry.sql (18.04ms)
2026/06/28 22:04:43 OK   00009_source_snapshots.sql (19.27ms)
2026/06/28 22:04:43 OK   00010_context_packets.sql (22.67ms)
2026/06/28 22:04:43 OK   00011_plan_v2_fields.sql (32.85ms)
2026/06/28 22:04:43 OK   00012_run_submission_provenance.sql (16.43ms)
2026/06/28 22:04:43 OK   00013_project_context_memory.sql (21.6ms)
2026/06/28 22:04:43 OK   00014_context_packets_schema_repair.go (19.73ms)
2026/06/28 22:04:43 OK   00015_local_audits.sql (18.45ms)
2026/06/28 22:04:43 OK   00016_project_owned_plans.sql (29.28ms)
2026/06/28 22:04:43 OK   00017_refactor_backlog.sql (39ms)
2026/06/28 22:04:43 OK   20260624000200_plan_attempts_intent_drift.sql (39.34ms)
2026/06/28 22:04:43 OK   20260626000500_plan_review_settings.sql (19ms)
2026/06/28 22:04:43 OK   20260627000100_plan_seeds.sql (20.59ms)
2026/06/28 22:04:43 goose: successfully migrated database to version: 20260627000100
time=2026-06-28T22:04:43.596-04:00 level=INFO msg="executor: dispatching from executor_brief.md" run_id=1 exec_id=1 model=claude-sonnet-4.6
--- FAIL: TestParser_ANSI_PromptPrefix (0.00s)
    progress_parser_test.go:360: event message should not start with >: "> STATUS: DONE"
FAIL
FAIL	relay/internal/executor	25.338s
ok  	relay/internal/handlers	19.414s
ok  	relay/internal/instructions	1.159s
ok  	relay/internal/intake	9.940s
ok  	relay/internal/mcp	56.432s
ok  	relay/internal/pipeline	(cached)
ok  	relay/internal/projectmemory	(cached)
ok  	relay/internal/refactors	38.470s
ok  	relay/internal/renderer	4.814s
ok  	relay/internal/repairer	4.838s
ok  	relay/internal/repos	(cached)
ok  	relay/internal/server	6.924s
ok  	relay/internal/smoke	9.207s
ok  	relay/internal/sources	(cached)
ok  	relay/internal/store	(cached)
?   	relay/internal/store/generated	[no test files]
ok  	relay/internal/validation	(cached)
ok  	relay/internal/validationrunner	2.111s
ok  	relay/internal/views	(cached)
FAIL
exit_code: 1

```

