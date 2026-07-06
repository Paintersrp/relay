# Latest Relay Validation Report (tier)

- status: failed
- validation_tier: full
- validation_scope: tier
- base_commit: cc21cc0d6dbd32946f05634b732ab3a9824eeeec
- validated_source_snapshot: 61a393d05f5958c6a7ea76dbdd35eedb54542f53772d979ccb2cf407d5ae174d
- worktree_dirty: true
- created_at: 2026-07-06T00:08:21Z

## Validated source changes

- M .aider.chat.history.md
- M agents/knowledge/backend_code_surface_map.md
- M apps/web/src/components/relay/PlanPassContextPanel.test.ts
- M apps/web/src/components/relay/RelayProjectForm.tsx
- M apps/web/src/features/relay-plans/api.test.ts
- M docs/agent-reference.md
- M docs/backend-code-surface-map.md
- M docs/generated/agent-references/backend-surface.json
- M docs/generated/agent-references/backend-surface.md
- M docs/generated/agent-references/frontend-backend-contract.json
- M docs/generated/agent-references/frontend-backend-contract.md
- M docs/generated/agent-references/http-api-surface.json
- M docs/generated/agent-references/http-api-surface.md
- M docs/generated/agent-references/index.json
- M docs/generated/agent-references/index.md
- M docs/generated/agent-references/mcp-surface.json
- M docs/generated/agent-references/mcp-surface.md
- M docs/generated/agent-references/README.md
- M docs/generated/agent-references/storage-surface.json
- M docs/generated/agent-references/storage-surface.md
- M docs/generated/agent-references/workflow-surfaces.json
- M docs/generated/agent-references/workflow-surfaces.md
- M docs/mcp.md
- M docs/refactor-backlog.md
- M internal/agentrefs/agentrefs_test.go
- M internal/agentrefs/docs_integration_test.go
- M internal/agentrefs/mcp.go
- M internal/agentrefs/workflow.go
- M internal/api/next_pass_work_test.go
- M internal/app/plans/service_test.go
- M internal/app/plans/types.go
- M internal/app/plans/work_packets_test.go
- M internal/app/plans/work_packets.go
- M internal/app/projects/service_test.go
- M internal/auditor/auditor_test.go
- M internal/auditor/service_test.go
- M internal/closeout/closeout_test.go
- M internal/closeout/closeout.go
- M internal/compiler/compiler.go
- M internal/instructions/assets/AGENTS.md
- M internal/mcp/plan_attempt_tools_test.go
- M internal/refactors/promotion.go
- M internal/renderer/renderer.go
- M internal/repairer/aider_test.go
- M internal/repairer/aider.go
- M internal/sources/repository_resolver_test.go
- M internal/validation/validation_test.go
- M internal/validation/validation.go
- D relay-contracts

## Commands

| Step | Name | Command | Exit | Status |
|---:|---|---|---:|---|
| 1 | `go-fmt-agentrefs-executor` | `go fmt ./cmd/agentrefs ./internal/agentrefs ./internal/executor` | 0 | passed |
| 2 | `go-test-agentrefs` | `go test ./internal/agentrefs/... ./cmd/agentrefs/...` | 0 | passed |
| 3 | `go-test-executor` | `go test ./internal/executor/...` | 0 | passed |
| 4 | `go-test-all` | `go test ./...` | 1 | failed |
| 5 | `web-typecheck` | `cd apps/web && npm run typecheck` | 0 | passed |
| 6 | `web-test` | `cd apps/web && npm run test` | 1 | failed |
| 7 | `web-build` | `cd apps/web && npm run build` | 0 | passed |
| 8 | `no-root-agentrefs-exe` | `test ! -e agentrefs.exe` | 0 | passed |

## Failure output tails

### go-test-all

```text
ok  	relay/internal/store	(cached)
?   	relay/internal/store/generated	[no test files]
2026/07/05 20:08:59 OK   00001_workflow.sql (62.95ms)
2026/07/05 20:08:59 OK   00002_execution_attempt_retries.sql (70.89ms)
2026/07/05 20:09:00 OK   00003_audit_packets.sql (129.83ms)
2026/07/05 20:09:00 OK   00014_context_packets_schema_repair.go (35.02ms)
2026/07/05 20:09:00 goose: successfully migrated database to version: 14
--- FAIL: [REDACTED_SECRET] (0.44s)
    store_test.go:49: unexpected fresh workflow tables
        got:  [artifacts audit_decisions audit_packets execution_attempts goose_db_version plan_pass_dependencies plan_passes plan_repository_targets plans repository_targets runs]
        want: [artifacts audit_decisions execution_attempts goose_db_version plan_pass_dependencies plan_passes plan_repository_targets plans repository_targets runs]
2026/07/05 20:09:00 OK   00001_workflow.sql (24.25ms)
2026/07/05 20:09:00 OK   00002_execution_attempt_retries.sql (12.02ms)
2026/07/05 20:09:00 OK   00003_audit_packets.sql (13.7ms)
2026/07/05 20:09:00 OK   00014_context_packets_schema_repair.go (10.89ms)
2026/07/05 20:09:00 goose: successfully migrated database to version: 14
2026/07/05 20:09:00 OK   00001_workflow.sql (75.65ms)
2026/07/05 20:09:00 OK   00002_execution_attempt_retries.sql (44.98ms)
2026/07/05 20:09:00 OK   00003_audit_packets.sql (44.21ms)
2026/07/05 20:09:00 OK   00014_context_packets_schema_repair.go (46.97ms)
2026/07/05 20:09:00 goose: successfully migrated database to version: 14
2026/07/05 20:09:00 OK   00001_workflow.sql (36.12ms)
2026/07/05 20:09:00 OK   00002_execution_attempt_retries.sql (48.36ms)
2026/07/05 20:09:00 OK   00003_audit_packets.sql (32.78ms)
2026/07/05 20:09:01 OK   00014_context_packets_schema_repair.go (21.77ms)
2026/07/05 20:09:01 goose: successfully migrated database to version: 14
2026/07/05 20:09:01 OK   00001_workflow.sql (61.67ms)
2026/07/05 20:09:01 OK   00002_execution_attempt_retries.sql (29.2ms)
2026/07/05 20:09:01 OK   00003_audit_packets.sql (43.74ms)
2026/07/05 20:09:01 OK   00014_context_packets_schema_repair.go (97.11ms)
2026/07/05 20:09:01 goose: successfully migrated database to version: 14
FAIL
FAIL	relay/internal/store/workflow	3.553s
?   	relay/internal/store/workflowgenerated	[no test files]
ok  	relay/internal/validation	3.466s
ok  	relay/internal/validationrunner	(cached)
ok  	relay/internal/views	(cached)
FAIL
exit_code: 1

```

### web-test

```text
Error: Test timed out in 5000ms.
If this is a long-running test, pass a timeout value as the last argument or configure it globally with "testTimeout".
 ❯ src/routes/runs/$runId/execute.test.tsx:298:3
    296|
    297| describe("Execute route — default composition (Req 6.4)", () => {
    298|   it("renders identity, status, action, and a collapsed progression ra…
       |   ^
    299|     cleanups.push(installFetchStub(makeRunFixture()));
    300|

⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯[2/4]⎯

 FAIL  src/routes/runs/$runId/intake.test.tsx > Intake route - default composition (Req 6.4) > renders identity, current status, next action, and collapsed progression without expanding anything
Error: Test timed out in 5000ms.
If this is a long-running test, pass a timeout value as the last argument or configure it globally with "testTimeout".
 ❯ src/routes/runs/$runId/intake.test.tsx:216:3
    214|
    215| describe("Intake route - default composition (Req 6.4)", () => {
    216|   it("renders identity, current status, next action, and collapsed pro…
       |   ^
    217|     await renderIntakeRoute();
    218|

⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯[3/4]⎯

 FAIL  src/routes/runs/$runId/prepare.test.tsx > Prepare route — default composition (Req 6.4) > renders identity, status, action, and a collapsed progression rail without expanding any Detail_Disclosure section
Error: Test timed out in 5000ms.
If this is a long-running test, pass a timeout value as the last argument or configure it globally with "testTimeout".
 ❯ src/routes/runs/$runId/prepare.test.tsx:272:3
    270|
    271| describe("Prepare route — default composition (Req 6.4)", () => {
    272|   it("renders identity, status, action, and a collapsed progression ra…
       |   ^
    273|     cleanups.push(installFetchStub(makeRunFixture()));
    274|

⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯[4/4]⎯

exit_code: 1

```

