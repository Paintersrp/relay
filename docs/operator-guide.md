# Relay Operator Guide

This guide covers the current local-first workflow. Relay operates on repositories and branches that the operator prepares in advance. It does not perform Git delivery.

## Prerequisites

- Go 1.25.7 or compatible;
- Node.js and npm;
- Bash and Make;
- `sqlc`;
- `templ`;
- `goose` only for manual migration targets;
- any executor CLI you intend to use, installed and authenticated separately.

Install repository dependencies and generators:

```bash
npm --prefix apps/web install
go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest
go install github.com/pressly/goose/v3/cmd/goose@latest
go install github.com/a-h/templ/cmd/templ@latest
sqlc generate
templ generate
```

Copy selected values from `.env.example` into ignored `.env` or `.env.local`. Copy `apps/web/.env.example` to `apps/web/.env` when browser API configuration is needed.

## Start Relay

Run the API and web application in separate terminals:

```bash
go run ./cmd/relay
npm run dev:web
```

Defaults:

- web: `http://localhost:3000`;
- API and HTTP MCP: `http://localhost:8080`;
- workflow database: `data/workflow/relay-workflow.sqlite`;
- workflow artifacts: `data/workflow/artifacts`.

Override storage with `RELAY_WORKFLOW_DB_PATH` and `RELAY_WORKFLOW_ARTIFACTS_DIR`. Embedded migrations run automatically when Relay opens the store.

## Register repositories and Projects

1. Open `/projects`.
2. Register each local repository with a stable `repo_target`, local path, and branch information.
3. Create an active Project when the work should organize one or more Plans.
4. Attach the relevant registered repositories to the Project.

A Project is an organizational container. It does not alter canonical artifact bytes, approval identity, Run execution, or repository access.

## Submit a Plan

Use `/plans/new` or the Planner/`local_operator` MCP profile.

A canonical Plan:

- is named `<feature-slug>.plan.json`;
- references registered `repo_target` values;
- contains ordered passes and dependencies;
- is approved outside Relay before submission.

Submission validates the exact JSON, renders the Markdown Plan, persists Plan/pass records, and stores canonical and rendered artifacts. MCP submission additionally requires the exact approved SHA-256 and an externally selected active `project_id`.

Inspect the Plan at `/plans/{planId}` and a pass at `/plans/{planId}/passes/{passId}`.

## Create a Run

Use `/runs/new` or `create_run`.

- Managed Run: submit `<feature-slug>.pass-<number>.execution-spec.json` with matching `plan_id` and positive `pass_number`.
- Standalone Run: submit `<feature-slug>.execution-spec.json` without a Plan/pass association.
- Remediation Run: create a remediation Delivery Ticket revision through the ordinary Planner ticket route. The Auditor issues `needs_revision` decisions that produce immutable remediation seeds, not standalone remediation Execution Specs.

Relay verifies the exact SHA-256, validates the Execution Spec, renders the Executor Brief, and creates a setup-ready Run. Run creation does not start execution.

## Execute

Open `/runs/{runId}/execute`.

1. Select an executor adapter and model.
2. Start execution.
3. Monitor attempt state and captured stdout, stderr, command metadata, and artifacts.
4. Cancel an active attempt when necessary.
5. Retry only from an eligible failed or cancelled state.

Relay applies deterministic operations first. A complete deterministic application skips model execution. A blocked deterministic application moves the Run to revision. A partial application supplies bounded residual context to the selected executor.

The executor edits the existing worktree but does not stage, commit, push, create branches, create worktrees, or open pull requests.

## Record validation

The Execution Spec defines ordered validation commands. Relay supports validation state and evidence, but the current HTTP/web surface does not expose the validation-result transition. Operators must complete that transition through the currently available application path outside the workbench.

Do not mark validation successful unless every required command was actually executed successfully.

## Prepare and review an audit packet

When the Run is audit-ready:

1. Ensure the implementation is committed outside Relay.
2. Open `/runs/{runId}/audit`.
3. Prepare the packet against the full audited commit SHA.
4. Inspect packet identity, SHA-256, audited commit, selected attempt, diff, validation evidence, and declared artifact references.
5. Supply the packet and declared artifacts to the Auditor when direct MCP retrieval is unavailable.

`get_audit_packet` and `get_run_artifact` revalidate freshness, ownership, size, and SHA-256 on readback.

## Record an audit decision

Audit decision submission is MCP-only.

Use the `auditor` or `local_operator` profile and call `record_audit_decision` only after explicit operator confirmation. The request must bind to the exact current packet ID, packet SHA-256, audited commit, decision, and rationale.

- `accepted` completes the Run and managed pass.
- `needs_revision` returns the Run to revision.

The web audit view does not record the decision.

## Secure ChatGPT tunnel

Use the stable package interface:

```bash
npm run chatgpt-mcp:help
npm run chatgpt-mcp:init
npm run chatgpt-mcp:doctor
npm run chatgpt-mcp:start
```

Stdio is the default transport and launches the canonical MCP server locally. Keep the start process open while ChatGPT uses the connector. See [chatgpt-mcp-local.md](chatgpt-mcp-local.md) for environment values, profiles, advanced HTTP mode, and troubleshooting.

## Maintenance and validation

```bash
make workflow-db-status
make workflow-db-migrate
make mcp-test
make mcp-smoke
npm run test:local-scripts
npm run release:smoke
```

See [smoke.md](smoke.md) for focused checks.

## Current limitations

Relay does not currently:

- submit validation results through HTTP or the web UI;
- submit audit decisions through HTTP or the web UI;
- prepare repositories, branches, or worktrees;
- stage, commit, push, or open pull requests;
- run hosted CI;
- automatically start the next Plan pass;
- provide arbitrary MCP shell, filesystem, source-search, or Git tools;
- operate removed handoff, context, seed, refactor, local-audit, or intent-drift workflows.
