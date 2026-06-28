# Latest Relay Validation Report (full)

- status: failed
- validation_tier: full
- base_commit: 9e8bd864a3580681b733c8101b9e9388d5fefdeb
- validated_source_snapshot: d033fc4cdf7ab429602024386e98c05647459eb87893f1e0a1586fc12ac7faba
- worktree_dirty: true
- created_at: 2026-06-28T23:28:05Z

## Validated source changes

- M docs/generated/agent-references/backend-surface.json
- M docs/generated/agent-references/backend-surface.md
- M docs/generated/agent-references/frontend-backend-contract.json
- M docs/generated/agent-references/frontend-backend-contract.md
- M docs/generated/agent-references/index.json
- M docs/generated/agent-references/index.md
- M docs/generated/agent-references/workflow-surfaces.json
- M docs/generated/agent-references/workflow-surfaces.md
- A handoffs/validation/latest.validation-report.fast.json
- A handoffs/validation/latest.validation-summary.fast.md
- M internal/executor/executor_test.go
- M internal/executor/executor.go

## Commands

| Step | Name | Exit | Status |
|---:|---|---:|---|
| 1 | `go-fmt-agentrefs-executor` | 0 | passed |
| 2 | `go-test-agentrefs` | 0 | passed |
| 3 | `agentrefs-check` | 0 | passed |
| 4 | `go-test-executor` | 1 | failed |
| 5 | `go-test-all` | 1 | failed |
| 6 | `web-typecheck` | 0 | passed |
| 7 | `web-test` | 0 | passed |
| 8 | `web-build` | 0 | passed |
| 9 | `no-root-agentrefs-exe` | 0 | passed |

## Failure output tails

### go-test-executor

```text
2026/06/28 19:28:16 OK   20260624000200_plan_attempts_intent_drift.sql (15.94ms)
2026/06/28 19:28:16 OK   20260626000500_plan_review_settings.sql (8.68ms)
2026/06/28 19:28:16 OK   20260627000100_plan_seeds.sql (8.01ms)
2026/06/28 19:28:16 goose: successfully migrated database to version: 20260627000100
time=2026-06-28T19:28:16.306-04:00 level=INFO msg="executor: dispatching from executor_brief.md" run_id=1 exec_id=1 model=claude-sonnet-4.6
time=2026-06-28T19:28:16.342-04:00 level=WARN msg="executor: failed to write kiro parse fixture" error="unknown artifact kind: kiro_parse_fixture_json"
--- FAIL: TestKiroParseFixture_EmittedOnNonzeroExit (0.31s)
    executor_test.go:2131: expected kiro_parse_fixture_json artifact on nonzero exit when flag enabled
2026/06/28 19:28:16 OK   00001_init.sql (7.68ms)
2026/06/28 19:28:16 OK   00002_repo_roots.sql (7.44ms)
2026/06/28 19:28:16 OK   00003_agent_executions.sql (7.71ms)
2026/06/28 19:28:16 OK   00004_validation_executions.sql (7.02ms)
2026/06/28 19:28:16 OK   00005_add_executor_adapter_to_runs.sql (7.52ms)
2026/06/28 19:28:16 OK   00006_create_plans_and_plan_passes.sql (8.74ms)
2026/06/28 19:28:16 OK   00007_add_run_plan_pass_association.sql (7.9ms)
2026/06/28 19:28:16 OK   00008_projects_registry.sql (9.21ms)
2026/06/28 19:28:16 OK   00009_source_snapshots.sql (8.67ms)
2026/06/28 19:28:16 OK   00010_context_packets.sql (8.77ms)
2026/06/28 19:28:16 OK   00011_plan_v2_fields.sql (16.18ms)
2026/06/28 19:28:16 OK   00012_run_submission_provenance.sql (9.14ms)
2026/06/28 19:28:16 OK   00013_project_context_memory.sql (9.54ms)
2026/06/28 19:28:16 OK   00014_context_packets_schema_repair.go (6.54ms)
2026/06/28 19:28:16 OK   00015_local_audits.sql (6.84ms)
2026/06/28 19:28:16 OK   00016_project_owned_plans.sql (16.43ms)
2026/06/28 19:28:16 OK   00017_refactor_backlog.sql (10.86ms)
2026/06/28 19:28:16 OK   20260624000200_plan_attempts_intent_drift.sql (18.78ms)
2026/06/28 19:28:16 OK   20260626000500_plan_review_settings.sql (7.54ms)
2026/06/28 19:28:16 OK   20260627000100_plan_seeds.sql (7.91ms)
2026/06/28 19:28:16 goose: successfully migrated database to version: 20260627000100
time=2026-06-28T19:28:16.610-04:00 level=INFO msg="executor: dispatching from executor_brief.md" run_id=1 exec_id=1 model=claude-sonnet-4.6
time=2026-06-28T19:28:16.643-04:00 level=WARN msg="executor: failed to write kiro parse fixture" error="unknown artifact kind: kiro_parse_fixture_json"
--- FAIL: TestKiroParseFixture_EmittedOnTimeout (0.29s)
    executor_test.go:2214: expected kiro_parse_fixture_json artifact on timeout when flag enabled
--- FAIL: TestParser_ANSI_PromptPrefix (0.00s)
    progress_parser_test.go:360: event message should not start with >: "> STATUS: DONE"
FAIL
FAIL	relay/internal/executor	8.376s
FAIL
exit_code: 1

```

### go-test-all

```text
2026/06/28 19:29:15 OK   00014_context_packets_schema_repair.go (54.27ms)
2026/06/28 19:29:15 OK   00015_local_audits.sql (39.49ms)
2026/06/28 19:29:15 OK   00016_project_owned_plans.sql (57.29ms)
2026/06/28 19:29:15 OK   00017_refactor_backlog.sql (62.44ms)
2026/06/28 19:29:15 OK   20260624000200_plan_attempts_intent_drift.sql (66.61ms)
2026/06/28 19:29:15 OK   20260626000500_plan_review_settings.sql (94.6ms)
2026/06/28 19:29:15 OK   20260627000100_plan_seeds.sql (52.81ms)
2026/06/28 19:29:15 goose: successfully migrated database to version: 20260627000100
time=2026-06-28T19:29:15.876-04:00 level=INFO msg="executor: dispatching from executor_brief.md" run_id=1 exec_id=1 model=claude-sonnet-4.6
time=2026-06-28T19:29:16.211-04:00 level=WARN msg="executor: failed to write kiro parse fixture" error="unknown artifact kind: kiro_parse_fixture_json"
--- FAIL: TestKiroParseFixture_EmittedOnTimeout (3.18s)
    executor_test.go:2214: expected kiro_parse_fixture_json artifact on timeout when flag enabled
--- FAIL: TestParser_ANSI_PromptPrefix (0.00s)
    progress_parser_test.go:360: event message should not start with >: "> STATUS: DONE"
FAIL
FAIL	relay/internal/executor	45.677s
ok  	relay/internal/handlers	23.247s
--- FAIL: TestRootAGENTSMDMatchesCanonical (0.00s)
    instructions_test.go:74: root AGENTS.md does not match canonical assets/AGENTS.md
FAIL
FAIL	relay/internal/instructions	0.656s
ok  	relay/internal/intake	7.578s
ok  	relay/internal/mcp	77.276s
ok  	relay/internal/pipeline	4.364s
ok  	relay/internal/projectmemory	(cached)
ok  	relay/internal/refactors	61.619s
ok  	relay/internal/renderer	4.851s
ok  	relay/internal/repairer	4.235s
ok  	relay/internal/repos	26.556s
ok  	relay/internal/server	4.400s
ok  	relay/internal/smoke	7.465s
ok  	relay/internal/sources	41.011s
ok  	relay/internal/store	(cached)
?   	relay/internal/store/generated	[no test files]
ok  	relay/internal/validation	(cached)
ok  	relay/internal/validationrunner	2.380s
ok  	relay/internal/views	(cached)
FAIL
exit_code: 1

```

