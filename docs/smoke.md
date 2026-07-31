# Relay Validation and Smoke Checks

Relay keeps focused checks for MCP boundaries and one integrated local release gate.

## Integrated release gate

Run:

```bash
npm run release:smoke
```

This delegates to `scripts/release-smoke.sh`, which performs:

1. `sqlc` generation and verifies that it does not change the worktree;
2. `npm run test:local-scripts`;
3. web typechecking, tests, and production build;
4. the complete Go test graph;
5. Go vet; and
6. diff whitespace validation.

## MCP checks

### Focused HTTP, route, and catalog tests

```bash
make mcp-http-test
```

Focused role-app route and generated-catalog tests also run directly from the
Go packages that own those contracts.

### Local-script guardrails

```bash
npm run test:local-scripts
```

These verify three-role tunnel configuration, private ingress, supervision,
credential redaction, protocol markers, file-parameter metadata, and retired
surface absence.

### Local tunnel diagnostics

```bash
npm run chatgpt-mcp:doctor:all
```

This diagnoses configured local tunnels for Wayfinder, Planner, and Auditor.
Use temporary `RELAY_WORKFLOW_DB_PATH` and `RELAY_WORKFLOW_ARTIFACTS_DIR`
values outside the release gate when isolation matters.

## Web checks

```bash
npm run typecheck:web
npm run test:web -- --run
npm run build:web
```

## Go and generation checks

```bash
sqlc generate
go test ./...
go vet ./...
```

Generated workflow query source is owned by `internal/db/workflow_migrations`,
`internal/db/workflow_queries`, and `sqlc.yaml`. Do not hand-edit
`internal/store/workflowgenerated` independently.

## Database checks

```bash
make workflow-db-status
make workflow-db-migrate
```

These inspect or apply the retained workflow migration chain against the
default local workflow database. Normal server startup applies embedded
migrations automatically.
