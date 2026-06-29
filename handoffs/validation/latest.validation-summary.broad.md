# Latest Relay Validation Report (broad)

- status: failed
- validation_tier: broad
- base_commit: fb08b7a0c3c77ae4699e76875e32bc417f4c66f2
- validated_source_snapshot: 880f4d74cb06734e20229f733c958d6b80bdd99a70bc1a15e4689e354f73ffa9
- worktree_dirty: false
- created_at: 2026-06-28T23:47:52Z

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
2026/06/28 19:47:53 found 2 stale or missing output(s)
exit status 1
exit_code: 1

```

### go-test-executor

```text
2026/06/28 19:48:02 OK   20260624000200_plan_attempts_intent_drift.sql (16.78ms)
2026/06/28 19:48:03 OK   20260626000500_plan_review_settings.sql (14.87ms)
2026/06/28 19:48:03 OK   20260627000100_plan_seeds.sql (9.79ms)
2026/06/28 19:48:03 goose: successfully migrated database to version: 20260627000100
time=2026-06-28T19:48:03.052-04:00 level=INFO msg="executor: dispatching from executor_brief.md" run_id=1 exec_id=1 model=claude-sonnet-4.6
time=2026-06-28T19:48:03.088-04:00 level=WARN msg="executor: failed to write kiro parse fixture" error="unknown artifact kind: kiro_parse_fixture_json"
--- FAIL: TestKiroParseFixture_EmittedOnNonzeroExit (0.35s)
    executor_test.go:2131: expected kiro_parse_fixture_json artifact on nonzero exit when flag enabled
2026/06/28 19:48:03 OK   00001_init.sql (8.76ms)
2026/06/28 19:48:03 OK   00002_repo_roots.sql (8.03ms)
2026/06/28 19:48:03 OK   00003_agent_executions.sql (6.42ms)
2026/06/28 19:48:03 OK   00004_validation_executions.sql (6.88ms)
2026/06/28 19:48:03 OK   00005_add_executor_adapter_to_runs.sql (6.29ms)
2026/06/28 19:48:03 OK   00006_create_plans_and_plan_passes.sql (11.6ms)
2026/06/28 19:48:03 OK   00007_add_run_plan_pass_association.sql (7.35ms)
2026/06/28 19:48:03 OK   00008_projects_registry.sql (7.42ms)
2026/06/28 19:48:03 OK   00009_source_snapshots.sql (8.51ms)
2026/06/28 19:48:03 OK   00010_context_packets.sql (9.02ms)
2026/06/28 19:48:03 OK   00011_plan_v2_fields.sql (15.94ms)
2026/06/28 19:48:03 OK   00012_run_submission_provenance.sql (12.38ms)
2026/06/28 19:48:03 OK   00013_project_context_memory.sql (8.3ms)
2026/06/28 19:48:03 OK   00014_context_packets_schema_repair.go (6.36ms)
2026/06/28 19:48:03 OK   00015_local_audits.sql (7.37ms)
2026/06/28 19:48:03 OK   00016_project_owned_plans.sql (15.31ms)
2026/06/28 19:48:03 OK   00017_refactor_backlog.sql (10.66ms)
2026/06/28 19:48:03 OK   20260624000200_plan_attempts_intent_drift.sql (15.55ms)
2026/06/28 19:48:03 OK   20260626000500_plan_review_settings.sql (11.59ms)
2026/06/28 19:48:03 OK   20260627000100_plan_seeds.sql (7.91ms)
2026/06/28 19:48:03 goose: successfully migrated database to version: 20260627000100
time=2026-06-28T19:48:03.358-04:00 level=INFO msg="executor: dispatching from executor_brief.md" run_id=1 exec_id=1 model=claude-sonnet-4.6
time=2026-06-28T19:48:03.391-04:00 level=WARN msg="executor: failed to write kiro parse fixture" error="unknown artifact kind: kiro_parse_fixture_json"
--- FAIL: TestKiroParseFixture_EmittedOnTimeout (0.30s)
    executor_test.go:2214: expected kiro_parse_fixture_json artifact on timeout when flag enabled
--- FAIL: TestParser_ANSI_PromptPrefix (0.00s)
    progress_parser_test.go:360: event message should not start with >: "> STATUS: DONE"
FAIL
FAIL	relay/internal/executor	8.635s
FAIL
exit_code: 1

```

### go-test-all

```text
2026/06/28 19:48:45 OK   00014_context_packets_schema_repair.go (13ms)
2026/06/28 19:48:45 OK   00015_local_audits.sql (32.37ms)
2026/06/28 19:48:45 OK   00016_project_owned_plans.sql (28.13ms)
2026/06/28 19:48:45 OK   00017_refactor_backlog.sql (23.13ms)
2026/06/28 19:48:45 OK   20260624000200_plan_attempts_intent_drift.sql (38.97ms)
2026/06/28 19:48:45 OK   20260626000500_plan_review_settings.sql (19.96ms)
2026/06/28 19:48:45 OK   20260627000100_plan_seeds.sql (17.63ms)
2026/06/28 19:48:45 goose: successfully migrated database to version: 20260627000100
time=2026-06-28T19:48:45.489-04:00 level=INFO msg="executor: dispatching from executor_brief.md" run_id=1 exec_id=1 model=claude-sonnet-4.6
time=2026-06-28T19:48:45.763-04:00 level=WARN msg="executor: failed to write kiro parse fixture" error="unknown artifact kind: kiro_parse_fixture_json"
--- FAIL: TestKiroParseFixture_EmittedOnTimeout (1.01s)
    executor_test.go:2214: expected kiro_parse_fixture_json artifact on timeout when flag enabled
--- FAIL: TestParser_ANSI_PromptPrefix (0.00s)
    progress_parser_test.go:360: event message should not start with >: "> STATUS: DONE"
FAIL
FAIL	relay/internal/executor	32.484s
ok  	relay/internal/handlers	21.727s
--- FAIL: TestRootAGENTSMDMatchesCanonical (0.00s)
    instructions_test.go:74: root AGENTS.md does not match canonical assets/AGENTS.md
FAIL
FAIL	relay/internal/instructions	0.531s
ok  	relay/internal/intake	(cached)
ok  	relay/internal/mcp	53.549s
ok  	relay/internal/pipeline	3.692s
ok  	relay/internal/projectmemory	(cached)
ok  	relay/internal/refactors	(cached)
ok  	relay/internal/renderer	(cached)
ok  	relay/internal/repairer	4.842s
ok  	relay/internal/repos	23.117s
ok  	relay/internal/server	(cached)
ok  	relay/internal/smoke	12.970s
ok  	relay/internal/sources	33.356s
ok  	relay/internal/store	(cached)
?   	relay/internal/store/generated	[no test files]
ok  	relay/internal/validation	(cached)
ok  	relay/internal/validationrunner	1.785s
ok  	relay/internal/views	(cached)
FAIL
exit_code: 1

```

