#!/usr/bin/env node
import fs from "node:fs";
import path from "node:path";
import process from "node:process";

const root = process.cwd();
const mode = process.argv[2] ?? "--check";
if (!new Set(["--apply", "--check"]).has(mode)) {
  console.error("usage: node scripts/final-surface.mjs [--apply|--check]");
  process.exit(2);
}

// These directories are wholly retired. None contains a canonical workflow
// package. Parent directories that contain active workflow subpackages are
// intentionally absent from this list.
const legacyDirectories = [
  "apps/web/src/features/relay-refactors",
  "apps/web/src/features/relay-runs/components",
  "cmd/plan-api-smoke",
  "cmd/plan-seed-smoke",
  "internal/app/drift",
  "internal/app/intake",
  "internal/auditor",
  "internal/closeout",
  "internal/compiler",
  "internal/contextpackets",
  "internal/db/migrations",
  "internal/db/queries",
  "internal/events",
  "internal/handlers",
  "internal/httpx",
  "internal/intake",
  "internal/packets",
  "internal/projectmemory",
  "internal/refactors",
  "internal/refs",
  "internal/renderer",
  "internal/repairer",
  "internal/settings",
  "internal/smoke",
  "internal/sources",
  "internal/store/generated",
  "internal/store/sqlc",
  "internal/validation",
  "internal/validationrunner",
  "internal/views",
  "web",
];

const legacyFiles = [
  "apps/web/src/routes/projects/$projectId.refactor-backlog.tsx",
  "cmd/relay-closeout/main.go",
  "internal/server/routes.go",
  "docs/backend-code-surface-map.md",
  "docs/project-planning-backlog-plan-seeds.md",
  "docs/refactor-backlog.md",
  "docs/frontend-pivot.md",
];

// Direct-file pruning never traverses child directories. In particular,
// internal/artifacts/workflow and internal/repos/workflow are preserved.
const allowedDirectGoFiles = new Map([
  ["internal/api", new Set([])],
  ["internal/api/artifacts", new Set(["workflow.go", "workflow_test.go"])],
  ["internal/api/audits", new Set(["routes.go", "workflow.go", "workflow_test.go"])],
  ["internal/api/plans", new Set(["workflow.go", "workflow_test.go"])],
  ["internal/api/projects", new Set(["workflow.go", "workflow_test.go"])],
  ["internal/api/repositories", new Set(["workflow.go", "workflow_test.go"])],
  ["internal/api/runs", new Set(["workflow.go", "workflow_test.go", "workflow_execution.go", "workflow_execution_test.go"])],
  ["internal/app", new Set([])],
  ["internal/app/audits", new Set(["doc.go", "release_workflow_test.go", "workflow_packet.go", "workflow_service.go", "workflow_service_test.go", "workflow_types.go"])],
  ["internal/app/plans", new Set([])],
  ["internal/app/projects", new Set([])],
  ["internal/app/runs", new Set([])],
  ["internal/artifacts", new Set([])],
  ["internal/repos", new Set([])],
  ["internal/store", new Set([])],
]);

const legacyMCPFiles = new Set([
  "context_broker_git_tools.go",
  "context_broker_git_tools_test.go",
  "context_broker_tools.go",
  "context_broker_tools_test.go",
  "context_packet_workflow_test.go",
  "local_audit_tools.go",
  "local_audit_tools_test.go",
  "mcp_test.go",
  "orchestrator_work_tools.go",
  "orchestrator_work_tools_test.go",
  "plan_attempt_tools.go",
  "plan_attempt_tools_test.go",
  "plan_seed_tools.go",
  "plan_seed_tools_test.go",
  "plan_tools.go",
  "plan_tools_test.go",
  "project_context_memory_tools.go",
  "project_context_memory_tools_test.go",
  "refactor_backlog_tools.go",
  "refactor_backlog_tools_test.go",
  "tool_create_run.go",
  "tool_create_run_test.go",
  "tool_get_run_status.go",
  "tool_list_runs.go",
  "tool_submit_audit.go",
  "tool_test_audit_packet.go",
  "tool_validate_planner_handoff.go",
  "tool_surface_test.go",
]);

const legacyFrontendFiles = [
  "apps/web/src/features/relay-plans/api.ts",
  "apps/web/src/features/relay-plans/api.test.ts",
  "apps/web/src/features/relay-plans/queries.ts",
  "apps/web/src/features/relay-plans/types.ts",
  "apps/web/src/features/relay-projects/api.ts",
  "apps/web/src/features/relay-projects/api.test.ts",
  "apps/web/src/features/relay-projects/queries.ts",
  "apps/web/src/features/relay-projects/types.ts",
  "apps/web/src/features/relay-runs/api.ts",
  "apps/web/src/features/relay-runs/api.test.ts",
  "apps/web/src/features/relay-runs/api.audit-status.test.ts",
  "apps/web/src/features/relay-runs/mock-data.ts",
  "apps/web/src/features/relay-runs/queries.ts",
  "apps/web/src/features/relay-runs/queries.test.ts",
  "apps/web/src/features/relay-runs/types.ts",
  "apps/web/src/features/relay-runs/derivation.ts",
  "apps/web/src/features/relay-runs/derivation.test.ts",
  "apps/web/src/features/relay-runs/progression.ts",
  "apps/web/src/features/relay-runs/progression.test.ts",
  "apps/web/src/features/relay-runs/run-step.ts",
  "apps/web/src/features/relay-runs/run-step.test.ts",
  "apps/web/src/features/relay-runs/validationGate.ts",
  "apps/web/src/features/relay-runs/validationGate.test.ts",
  "apps/web/src/components/relay/RelayPlanDetail.tsx",
  "apps/web/src/components/relay/RelayPlanPassDetail.tsx",
  "apps/web/src/components/relay/RelayPlanPassTimeline.tsx",
  "apps/web/src/components/relay/RelayPlanPassTimeline.test.tsx",
  "apps/web/src/components/relay/RelayPlanWorkflowPanel.tsx",
  "apps/web/src/components/relay/RelayPlansRegistry.tsx",
  "apps/web/src/components/relay/RelayProjectPlanSeedsPanel.tsx",
  "apps/web/src/components/relay/RelayProjectRepositoryForm.tsx",
  "apps/web/src/components/relay/RelayRefactorBacklog.tsx",
  "apps/web/src/components/relay/RelayRefactorCandidateDetail.tsx",
  "apps/web/src/components/relay/RelayRefactorCandidates.tsx",
  "apps/web/src/components/relay/RelayRefactorDiscoveryTaskDetail.tsx",
  "apps/web/src/components/relay/RelayRefactorDiscoveryTasks.tsx",
  "apps/web/src/components/relay/RelayRunsRegistry.tsx",
  "apps/web/src/components/relay/RelayRunsRegistryRows.tsx",
  "apps/web/src/components/relay/RunEvidenceBrowser.tsx",
  "apps/web/src/components/relay/RunIntakeReviewPanel.tsx",
  "apps/web/src/components/relay/RunPlanContext.tsx",
  "apps/web/src/components/relay/RunSourceContextPanel.tsx",
  "apps/web/src/components/relay/RunStagePrimitives.tsx",
  "apps/web/src/components/relay/RunStatusTrackerLayout.tsx",
  "apps/web/src/components/relay/RunStatusTrackerLayout.test.tsx",
  "apps/web/src/components/relay/RunStepActionBar.tsx",
  "apps/web/src/components/relay/RunStepActionBar.test.tsx",
  "apps/web/src/components/relay/RunStepEvidence.tsx",
  "apps/web/src/components/relay/RunStepEvidence.test.tsx",
  "apps/web/src/components/relay/RunStepper.tsx",
  "apps/web/src/components/relay/RunStepper.test.tsx",
  "apps/web/src/components/relay/RunSummaryHeader.tsx",
  "apps/web/src/components/relay/RunWorkbenchStates.tsx",
  "apps/web/src/components/relay/runIntakeVisualState.ts",
  "apps/web/src/components/relay/runIntakeVisualState.test.ts",
  "apps/web/src/components/relay/runStageVisualState.ts",
  "apps/web/src/components/relay/runStageVisualState.test.ts",
];

const requiredPaths = [
  "internal/artifacts/workflow/store.go",
  "internal/repos/workflow/registry.go",
  "internal/store/workflow/store.go",
  "internal/app/audits/workflow_packet.go",
  "internal/app/audits/workflow_service.go",
  "internal/mcp/artifact_readback_tools.go",
  "apps/web/src/routes/runs/$runId.tsx",
  "apps/web/src/routes/runs/$runId/index.tsx",
  "apps/web/src/routes/runs/$runId/index.test.tsx",
  "apps/web/src/routes/runs/$runId.navigation.test.tsx",
  "apps/web/src/routeTree.gen.ts",
];

function abs(relative) {
  return path.join(root, relative);
}

function removePath(relative) {
  const target = abs(relative);
  if (fs.existsSync(target)) fs.rmSync(target, { recursive: true, force: true });
}

function directGoPrunes() {
  const result = [];
  for (const [directory, allowed] of allowedDirectGoFiles) {
    const target = abs(directory);
    if (!fs.existsSync(target)) continue;
    for (const entry of fs.readdirSync(target, { withFileTypes: true })) {
      if (entry.isFile() && entry.name.endsWith(".go") && !allowed.has(entry.name)) {
        result.push(path.posix.join(directory, entry.name));
      }
    }
  }
  return result;
}

function executorPrunes() {
  const target = abs("internal/executor");
  if (!fs.existsSync(target)) return [];
  const oldRootImport = /"relay\/internal\/(?:app\/plans|artifacts|events|store)"/;
  return fs.readdirSync(target, { withFileTypes: true })
    .filter((entry) => entry.isFile() && entry.name.endsWith(".go") && entry.name !== "executor.go")
    .map((entry) => path.posix.join("internal/executor", entry.name))
    .filter((relative) => oldRootImport.test(fs.readFileSync(abs(relative), "utf8")));
}

function mcpPrunes() {
  return [...legacyMCPFiles]
    .map((name) => path.posix.join("internal/mcp", name))
    .filter((relative) => fs.existsSync(abs(relative)));
}

function computedLegacyPaths() {
  return [...directGoPrunes(), ...executorPrunes(), ...mcpPrunes()].sort();
}

function existingLegacyPaths() {
  return [
    ...legacyDirectories,
    ...legacyFiles,
    ...legacyFrontendFiles,
    ...computedLegacyPaths(),
  ].filter((relative) => fs.existsSync(abs(relative)));
}

if (mode === "--apply") {
  for (const relative of [...legacyDirectories, ...legacyFiles, ...legacyFrontendFiles]) {
    removePath(relative);
  }
  for (const relative of computedLegacyPaths()) removePath(relative);
}

const remaining = existingLegacyPaths();
if (remaining.length > 0) {
  console.error("legacy surface remains:\n" + remaining.map((value) => `- ${value}`).join("\n"));
  process.exit(1);
}

for (const relative of requiredPaths) {
  if (!fs.existsSync(abs(relative))) {
    console.error(`required canonical path is missing: ${relative}`);
    process.exit(1);
  }
}
if (!fs.statSync(abs("internal/artifacts/workflow")).isDirectory()) {
  console.error("canonical workflow artifact package is not a directory");
  process.exit(1);
}
if (!fs.statSync(abs("internal/repos/workflow")).isDirectory()) {
  console.error("canonical workflow repository package is not a directory");
  process.exit(1);
}

const requiredText = [
  ["package.json", '"release:smoke": "bash scripts/release-smoke.sh"'],
  ["package.json", '"build:web": "npm --prefix apps/web run build"'],
  ["scripts/release-smoke.sh", "go run ./cmd/mcp-smoke"],
  ["scripts/release-smoke.sh", "go vet ./..."],
  ["internal/app/audits/workflow_types.go", "ChangedFiles"],
  ["internal/app/audits/workflow_types.go", "Artifacts"],
  ["internal/app/audits/workflow_packet.go", 'ArtifactReference: "unified_diff"'],
  ["internal/app/audits/workflow_service.go", 'batch.Stage("unified_diff"'],
  ["internal/app/audits/workflow_service.go", "resolvePacketArtifact"],
  ["internal/mcp/artifact_readback_tools.go", "artifact_reference"],
  ["internal/store/workflow/store.go", 'workflowartifacts "relay/internal/artifacts/workflow"'],
  ["internal/app/audits/workflow_packet.go", 'workflowrepos "relay/internal/repos/workflow"'],
  ["apps/web/src/routes/runs/$runId/index.tsx", 'createFileRoute("/runs/$runId/")'],
  ["apps/web/src/routes/runs/$runId/index.tsx", "workflowRunStageRoute(query.data.run.stage)"],
  ["apps/web/src/routeTree.gen.ts", "./routes/runs/$runId/index"],
  ["apps/web/src/routes/runs/$runId.navigation.test.tsx", "browser history"],
];
for (const [relative, expected] of requiredText) {
  const text = fs.readFileSync(abs(relative), "utf8");
  if (!text.toLowerCase().includes(expected.toLowerCase())) {
    console.error(`required canonical content is missing from ${relative}: ${expected}`);
    process.exit(1);
  }
}

const forbiddenChecks = [
  ["package.json", /build:css|build:js|plan-seed-smoke|htmx\.org|alpinejs|make validate|make mcp-smoke/],
  ["scripts/release-smoke.sh", /plan-seed-smoke|internal\/refactors|make mcp-smoke|npm run smoke|make validate/],
  ["internal/app/audits/workflow_types.go", /AuditPacketID\s+string\s*`|SelectedPass\s+\*WorkflowAuditPassAuthority|ValidationEvidence|Commit\s+WorkflowAuditCommitAuthority|Blockers\s+\[\]string/],
  ["internal/app/audits/workflow_service.go", /packet\.ValidationEvidence/],
  ["internal/mcp/server.go", /planner_handoff|plan_attempt|plan_seed|context_packet|context_memory|refactor/i],
  ["internal/mcp/deps.go", /relay\/internal\/store"|ContextBroker|Drift/i],
  ["apps/web/src/routeTree.gen.ts", /refactor-backlog|\/intake|\/prepare/],
];
for (const [relative, pattern] of forbiddenChecks) {
  if (!fs.existsSync(abs(relative))) continue;
  const text = fs.readFileSync(abs(relative), "utf8");
  if (pattern.test(text)) {
    console.error(`forbidden legacy concept remains in ${relative}: ${pattern}`);
    process.exit(1);
  }
}

function walk(relative) {
  const target = abs(relative);
  if (!fs.existsSync(target)) return [];
  const result = [];
  for (const entry of fs.readdirSync(target, { withFileTypes: true })) {
    const child = path.posix.join(relative, entry.name);
    if (entry.isDirectory()) result.push(...walk(child));
    else result.push(child);
  }
  return result;
}
const forbiddenExactRootImport = /"relay\/internal\/(?:app\/(?:drift|intake|plans|projects|runs)|artifacts|contextpackets|events|refactors|repos|store)"/;
for (const relative of walk("internal").filter((value) => value.endsWith(".go"))) {
  if (forbiddenExactRootImport.test(fs.readFileSync(abs(relative), "utf8"))) {
    console.error(`legacy root-package import remains in ${relative}`);
    process.exit(1);
  }
}

console.log("final canonical surface check passed");
