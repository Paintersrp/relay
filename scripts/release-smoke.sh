#!/usr/bin/env bash
# deterministic local-only release check for Relay.
# Fails fast on any compilation, test, or typechecking error.

set -euo pipefail

node - <<'NODE'
const { spawnSync } = require('child_process');

const commands = [
  // PASS-008: explicit refactor backlog hardening slice (backend service,
  // orchestrator/audit mapping, and MCP local-operator tools) runs first so a
  // refactor backlog regression fails fast before the broader suite.
  'go test ./internal/refactors ./internal/plans ./internal/mcp',
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
