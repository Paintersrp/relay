#!/usr/bin/env bash
# deterministic local-only release check for Relay.
# Fails fast on any compilation, test, or typechecking error.

set -euo pipefail

node - <<'NODE'
const { spawnSync } = require('child_process');

const commands = [
  // PASS-006: explicit Plan Seed release-hardening slice runs first so a
  // plan seed regression fails fast before the broader suite.
  'go test ./internal/app/projects ./internal/api/projects ./internal/mcp -run PlanSeed -count=1',
  'go run ./cmd/plan-seed-smoke',
  // PASS-008: explicit refactor backlog hardening slice (backend service,
  // orchestrator/audit mapping, and MCP local-operator tools) runs next so a
  // refactor backlog regression fails fast before the broader suite.
  'go test ./internal/refactors ./internal/app/plans ./internal/mcp',
  // Streamlined MCP context-gathering smoke — builds the MCP binary and
  // executes the deterministic end-to-end streamlined workflow smoke.
  'make mcp-smoke',
  'go test ./...',
  'npm run test:local-scripts',
  'npm --prefix apps/web run typecheck',
  'npm --prefix apps/web test -- --run',
  'npm --prefix apps/web run build',
  'npm run smoke',
  'make validate'
];

console.log("=========================================");
console.log("Starting Relay Release Smoke Verification");
console.log("=========================================");

for (const cmd of commands) {
  console.log(`\n>>> Executing: ${cmd}`);
  const result = spawnSync(cmd, { shell: true, stdio: 'inherit' });
  if (result.status !== 0) {
    console.error(`\n[FAIL] Command failed with exit code ${result.status}: ${cmd}`);
    process.exit(result.status ?? 1);
  }
}

console.log("\n=========================================");
console.log("SUCCESS: Relay release verification passed!");
console.log("=========================================");
NODE
