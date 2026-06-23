# Relay Release Readiness Report

This report documents the final release-hardening verification for Relay, ensuring compatibility, local safety, and clean documentation across all components.

## Release Verification Checklist

- **[x] Migrations Compatible**: Runtime auto-migrations execute successfully on fresh/stale databases. Enforced foreign keys (`PRAGMA foreign_keys = ON`) and WAL mode at connection level via native SQLite pragmas.
- **[x] Standalone Run Works**: Run creation operates without required plan or pass associations.
- **[x] Managed Plan/Pass Run Works**: Plan submission (`submit_planner_pass_plan`) registers plans and passes. Run creation associates runs to passes safely, handling state transitions.
- **[x] Source/Context Packet Path Works**: Read-only source snapshots and context packet generation are verified.
- **[x] Local Audit Works**: Bounded audit generation and decision validation run locally without external PR/CI/Action requirements.
- **[x] Project Context Memory Works**: Context memory records can be searched, listed, created, and superseded through MCP tools.
- **[x] MCP Profile Behavior Clear**: The default tool profile (`local-operator`) and `restricted` profile correctly restrict/expose tools.
- **[x] Docs Links Portable**: No machine-specific absolute file paths remain in documentation.
- **[x] No `.mex` Edits**: Checked and confirmed that no `.mex` files were created or modified.
- **[x] No Hidden External Dependency**: Tunnel doctor diagnostics and local scripts operate purely locally without requiring online endpoints.

## Verification Command Output

Verification was executed via the unified release smoke verification runner:

```bash
bash scripts/release-smoke.sh
```

All 7 execution steps passed cleanly:
1. `go test ./...` — **PASSED**
2. `npm run test:local-scripts` — **PASSED**
3. `npm --prefix apps/web run typecheck` — **PASSED**
4. `npm --prefix apps/web test` — **PASSED**
5. `npm --prefix apps/web run build` — **PASSED**
6. `npm run smoke` — **PASSED**
7. `make validate` — **PASSED**
