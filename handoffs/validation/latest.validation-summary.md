# Latest Relay Validation Report (tier)

- status: failed
- validation_tier: full
- validation_scope: tier
- base_commit: a224decf7780ffe268a5b4fdde8338f263aac15c
- validated_source_snapshot: 986ec9211ac7bbc8b6033cf6cc13bd5f437488d9198af67b1b6406286511d993
- worktree_dirty: true
- created_at: 2026-07-11T09:37:01Z

## Validated source changes

- D docs/agent-reference.md
- M go.mod
- M go.sum
- M internal/app/workflow/seed_contract_test.go
- M internal/architecture/boundary_test.go
- M internal/executor/executor.go
- M Makefile
- M README.md
- D schema/project_agent_reference.schema.json

## Commands

| Step | Name | Command | Exit | Status |
|---:|---|---|---:|---|
| 1 | `go-test-executor` | `go test ./internal/executor/...` | 0 | passed |
| 2 | `go-test-all` | `go test ./...` | 1 | failed |
| 3 | `web-typecheck` | `cd apps/web && npm run typecheck` | 0 | passed |
| 4 | `web-test` | `cd apps/web && npm run test` | 0 | passed |
| 5 | `web-build` | `cd apps/web && npm run build` | 0 | passed |

## Failure output tails

### go-test-all

```text
        ## Completion Criteria

        - The fixture compiles without errors.
        - The rendered brief matches the golden file.

        ## Execution Instructions

        - Treat this Executor Brief as the implementation authority for the assigned execution.
        - Complete the stated goal, implementation work, completion criteria, and validation.
        - Make any repository changes necessary to complete the specification.
        - Keep changes relevant to the specification and avoid unrelated cleanup or refactoring.
        - Preserve unrelated local changes. Do not reset, discard, or overwrite them.
        - Run the specified validation and report the results.
        - Report validation results, any incomplete work, and any technical blocker that prevents completion.
--- FAIL: TestCompileQualifiedExecutionSpecMatchesGolden (0.00s)
    compiler_test.go:35: qualified rendered brief does not match golden
--- FAIL: TestCompilePlanMatchesGolden (0.00s)
    compiler_test.go:48: rendered plan does not match golden
--- FAIL: TestPlanCrossFieldValidation (0.00s)
    compiler_test.go:217: failure returned partial output: {OutputFilename:0x1bce202f3810 Markdown:0x1bce202f3800 Errors:[] Notices:[]}
--- FAIL: TestMissingValidationCommandBlocksRendering (0.00s)
    compiler_test.go:367: failure returned partial output: {OutputFilename:0x1bce201bd9d0 Markdown:0x1bce201bd9c0 Errors:[] Notices:[]}
--- FAIL: TestSchemaVersionFallbackForms (0.00s)
    contract_test.go:35: expected exactly one replacement target "  \"schema_version\": \"1.0\",\n", found 0
--- FAIL: TestCanonicalOrderAcrossObjectShapes (0.00s)
    contract_test.go:83: expected exactly one replacement target "  \"goal\": \"Compile a representative Execution Spec fixture.\",\n  \"context\": \"Representative context with `inline code`.\\n\\n```go\\npackage example\\n```\",\n", found 0
--- FAIL: TestPlanStructuralDiagnostics (0.00s)
    contract_test.go:334: expected exactly one replacement target "    }\n  ],\n  \"passes\":", found 0
--- FAIL: TestEmbeddedSchemasMatchPinnedRelaySpecsBlobs (0.00s)
    --- FAIL: TestEmbeddedSchemasMatchPinnedRelaySpecsBlobs/schemas/plan.schema.json (0.00s)
        contract_test.go:431: embedded schema blob mismatch: got 3cb1f23dfcec4acd445f3e61161a9cc5eb238eec want 2a2fb55b39d6be8d79ab1de124c017d85ea1d872
    --- FAIL: TestEmbeddedSchemasMatchPinnedRelaySpecsBlobs/schemas/execution-spec.schema.json (0.00s)
        contract_test.go:431: embedded schema blob mismatch: got e659c0efd292acdd498ec5f1e36d6330cd49e48c want af6a5f0d8f546b5434dfe104c7b7f4159ff40cbe
FAIL
FAIL	relay/internal/speccompiler	2.251s
ok  	relay/internal/store/workflow	(cached)
?   	relay/internal/store/workflowgenerated	[no test files]
FAIL
exit_code: 1

```
