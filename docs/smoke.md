# Relay Validation and Smoke Checks

Relay keeps focused checks for individual boundaries and one integrated local release gate.

## Integrated release gate

Run:

```bash
npm run release:smoke
```

This delegates to `scripts/release-smoke.sh`, which performs:

1. `sqlc` generation;
2. focused workflow, API, server, executor, and MCP tests;
3. root local-script guardrails;
4. real stdio MCP self-tests for `planner`, `auditor`, and `local_operator` against isolated temporary workflow stores;
5. canonical `local_operator` MCP smoke;
6. web typechecking, tests, and production build;
7. the complete Go test graph;
8. Go vet;
9. diff whitespace validation.

The release script removes its temporary launcher stores on success or failure. It does not create validation reports, generated inventories, closeout evidence, or durable workflow records.

## MCP checks

### Focused Go tests

```bash
make mcp-test
make mcp-http-test
```

These cover canonical profiles, strict schemas, registration and dispatch, transport behavior, authentication, and server integration.

### Stdio smoke

```bash
make mcp-smoke
```

This builds the retained stdio server and exercises the canonical eight-action `local_operator` workflow.

### Real launcher self-test

```bash
RELAY_MCP_PROFILE=planner node scripts/local/relay-mcp-stdio.mjs --self-test
RELAY_MCP_PROFILE=auditor node scripts/local/relay-mcp-stdio.mjs --self-test
RELAY_MCP_PROFILE=local_operator node scripts/local/relay-mcp-stdio.mjs --self-test
```

Use temporary `RELAY_WORKFLOW_DB_PATH` and `RELAY_WORKFLOW_ARTIFACTS_DIR` values when running these outside the release gate and isolation matters.

### Local-script guardrails

```bash
npm run test:local-scripts
```

These verify package-wrapper ownership, help output, exact ordered inventories, canonical profile and transport names, default URLs and health listener, Planner fallback, credential redaction, protocol markers, file-parameter metadata, and representative retired-action absence.

### Authenticated HTTP smoke

Start the Relay daemon with a token, then run:

```bash
make mcp-http-smoke RELAY_MCP_URL=http://localhost:8080/mcp RELAY_MCP_AUTH_TOKEN=dev-token
```

This check requires a separately running HTTP daemon. The local stdio and package-script tests do not require credentials or network tunnel access.

## Web checks

```bash
npm run typecheck:web
npm run test:web -- --run
npm run build:web
```

The integrated release gate executes equivalent workspace commands directly.

## Go and generation checks

```bash
sqlc generate
go test ./...
go vet ./...
```

Generated workflow query source is owned by `internal/db/workflow_migrations`, `internal/db/workflow_queries`, and `sqlc.yaml`. Do not hand-edit `internal/store/workflowgenerated` independently.

## Database checks

```bash
make workflow-db-status
make workflow-db-migrate
```

These inspect or apply the retained workflow migration chain against the default local workflow database. Normal server startup applies embedded migrations automatically.

## What no longer exists

There is no root `smoke` package script, Plan Seed smoke, agent-reference generation, validation-report generation, closeout target, or compatibility MCP smoke. Use only the commands documented above and the current Makefile/package manifest.
