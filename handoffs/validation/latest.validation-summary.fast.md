# Latest Relay Validation Report (fast)

- status: failed
- validation_tier: fast
- base_commit: 9e8bd864a3580681b733c8101b9e9388d5fefdeb
- validated_source_snapshot: 055d00d5fe64ae8176686d123c0ea2ff54a9aef2513666bfa87b1c4701bf27b9
- worktree_dirty: true
- created_at: 2026-06-28T23:47:17Z

## Validated source changes

- M .githooks/pre-commit
- M .githooks/pre-push
- M AGENTS.md
- M docs/generated/agent-references/backend-surface.json
- M docs/generated/agent-references/backend-surface.md
- M docs/generated/agent-references/frontend-backend-contract.json
- M docs/generated/agent-references/frontend-backend-contract.md
- M docs/generated/agent-references/index.json
- M docs/generated/agent-references/index.md
- M docs/generated/agent-references/workflow-surfaces.json
- M docs/generated/agent-references/workflow-surfaces.md
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
2026/06/28 19:47:22 found 2 stale or missing output(s)
exit status 1
exit_code: 1

```

### go-test-executor

```text
2026/06/28 19:47:31 OK   20260624000200_plan_attempts_intent_drift.sql (16.3ms)
2026/06/28 19:47:31 OK   20260626000500_plan_review_settings.sql (7.43ms)
2026/06/28 19:47:31 OK   20260627000100_plan_seeds.sql (7.32ms)
2026/06/28 19:47:31 goose: successfully migrated database to version: 20260627000100
time=2026-06-28T19:47:31.521-04:00 level=INFO msg="executor: dispatching from executor_brief.md" run_id=1 exec_id=1 model=claude-sonnet-4.6
time=2026-06-28T19:47:31.555-04:00 level=WARN msg="executor: failed to write kiro parse fixture" error="unknown artifact kind: kiro_parse_fixture_json"
--- FAIL: TestKiroParseFixture_EmittedOnNonzeroExit (0.31s)
    executor_test.go:2131: expected kiro_parse_fixture_json artifact on nonzero exit when flag enabled
2026/06/28 19:47:31 OK   00001_init.sql (8.18ms)
2026/06/28 19:47:31 OK   00002_repo_roots.sql (8.98ms)
2026/06/28 19:47:31 OK   00003_agent_executions.sql (7.77ms)
2026/06/28 19:47:31 OK   00004_validation_executions.sql (6.34ms)
2026/06/28 19:47:31 OK   00005_add_executor_adapter_to_runs.sql (6.27ms)
2026/06/28 19:47:31 OK   00006_create_plans_and_plan_passes.sql (7.49ms)
2026/06/28 19:47:31 OK   00007_add_run_plan_pass_association.sql (8.47ms)
2026/06/28 19:47:31 OK   00008_projects_registry.sql (7.87ms)
2026/06/28 19:47:31 OK   00009_source_snapshots.sql (11.6ms)
2026/06/28 19:47:31 OK   00010_context_packets.sql (8.46ms)
2026/06/28 19:47:31 OK   00011_plan_v2_fields.sql (14.11ms)
2026/06/28 19:47:31 OK   00012_run_submission_provenance.sql (7.14ms)
2026/06/28 19:47:31 OK   00013_project_context_memory.sql (8.12ms)
2026/06/28 19:47:31 OK   00014_context_packets_schema_repair.go (6.35ms)
2026/06/28 19:47:31 OK   00015_local_audits.sql (6.98ms)
2026/06/28 19:47:31 OK   00016_project_owned_plans.sql (16.9ms)
2026/06/28 19:47:31 OK   00017_refactor_backlog.sql (10.52ms)
2026/06/28 19:47:31 OK   20260624000200_plan_attempts_intent_drift.sql (15.57ms)
2026/06/28 19:47:31 OK   20260626000500_plan_review_settings.sql (8.31ms)
2026/06/28 19:47:31 OK   20260627000100_plan_seeds.sql (7.11ms)
2026/06/28 19:47:31 goose: successfully migrated database to version: 20260627000100
time=2026-06-28T19:47:31.825-04:00 level=INFO msg="executor: dispatching from executor_brief.md" run_id=1 exec_id=1 model=claude-sonnet-4.6
time=2026-06-28T19:47:31.859-04:00 level=WARN msg="executor: failed to write kiro parse fixture" error="unknown artifact kind: kiro_parse_fixture_json"
--- FAIL: TestKiroParseFixture_EmittedOnTimeout (0.28s)
    executor_test.go:2214: expected kiro_parse_fixture_json artifact on timeout when flag enabled
--- FAIL: TestParser_ANSI_PromptPrefix (0.00s)
    progress_parser_test.go:360: event message should not start with >: "> STATUS: DONE"
FAIL
FAIL	relay/internal/executor	8.126s
FAIL
exit_code: 1

```

