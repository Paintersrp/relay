// ============================================================
// Relay Mock Data — Pass 2 normalized mock runs.
// ============================================================

import type { RelayRun, RelayArtifact, RelayRunEvent } from "./types";

const STEP_LABELS = {
  intake: "Intake / Configure",
  prepare: "Compile / Render",
  execute: "Execute",
  audit: "Audit / Close",
} as const;

export const mockRelayRuns: RelayRun[] = [
  {
    id: "intake_needs_review",
    name: "Add run workbench shell — Pass 1 scaffold",
    title: "Add run workbench shell — Pass 1 scaffold",
    packetId: "packet-2026-06-16-relay-pass-1-tanstack-start-workbench-shell",
    repo: "Paintersrp/relay",
    branch: "feature/tanstack-frontend",
    worktree: "relay-frontend-wt",
    executorAdapter: "opencode_go",
    executor: "deepseek-v4-flash",
    model: "deepseek/deepseek-chat-v3-0324:free",
    status: "intake_needs_review",
    activeStep: "intake",
    lifecycleState: "intake",
    createdAt: "2026-06-16T14:00:00.000Z",
    updatedAt: "2026-06-16T14:02:18.000Z",
    stepLabels: STEP_LABELS,
    summary: "Scaffold a new React frontend inside apps/web without breaking existing Go htmx backend.",
    riskLevel: "medium",
    statusSeverity: "warning",
    state: "Intake Review",
    validation: {
      errors: 2,
      warnings: 5,
      passed: 18,
      issues: [
        { severity: "error", code: "V-101", message: "Branch name mismatch in git preflight checks", path: ".git/HEAD" },
        { severity: "error", code: "V-102", message: "Uncommitted changes detected in working directory", path: "apps/web/src" },
      ],
    },
    // compatibility field
    validationSummary: { errors: 2, warnings: 5, passed: 18 },
    approvalGate: {
      label: "Intake Review",
      state: "pending",
      note: "Pass 2 — approval gates are mock/read-only. Real gate wiring is Pass 4.",
    },
    artifacts: [
      {
        id: "art-intake-1",
        label: "Original Handoff",
        path: "runs/intake_needs_review/handoff.md",
        kind: "handoff",
        sizeHint: "12 KB",
        status: "ready",
        filename: "handoff.md",
      },
      {
        id: "art-intake-2",
        label: "Parsed Metadata",
        path: "runs/intake_needs_review/metadata.json",
        kind: "validation",
        sizeHint: "2 KB",
        status: "ready",
        filename: "metadata.json",
      },
    ],
    latestEvents: [
      {
        id: "ev-intake-1",
        runId: "intake_needs_review",
        kind: "status_change",
        message: "Relay: Received handoff packet",
        createdAt: "2026-06-16T14:02:00.000Z",
      },
      {
        id: "ev-intake-2",
        runId: "intake_needs_review",
        kind: "log",
        message: "Relay: Parsing metadata...",
        createdAt: "2026-06-16T14:02:01.000Z",
      },
      {
        id: "ev-intake-3",
        runId: "intake_needs_review",
        kind: "validation_run",
        message: "Relay: Found 2 validation errors",
        createdAt: "2026-06-16T14:02:02.000Z",
      },
    ],
    logPreview: {
      lines: [
        "[14:02:00] Relay: Received handoff packet",
        "[14:02:01] Relay: Parsing metadata...",
        "[14:02:02] Relay: Found 2 validation errors",
        "[14:02:02] Relay: Awaiting intake review",
      ],
      truncated: false,
    },
  },
  {
    id: "brief_ready_for_review",
    name: "Define frontend API contract — Pass 2 schema",
    title: "Define frontend API contract — Pass 2 schema",
    packetId: "packet-2026-06-16-relay-pass-2-frontend-api-contract",
    repo: "Paintersrp/relay",
    branch: "feature/api-contract",
    worktree: "relay-api-contract-wt",
    executorAdapter: "opencode_go",
    executor: "claude-sonnet-4-5",
    model: "anthropic/claude-sonnet-4-5",
    status: "brief_ready_for_review",
    activeStep: "prepare",
    lifecycleState: "prepare",
    createdAt: "2026-06-16T10:00:00.000Z",
    updatedAt: "2026-06-16T10:37:00.000Z",
    stepLabels: STEP_LABELS,
    summary: "Establish standard REST endpoints, query structures, and response shapes for Relay API client.",
    riskLevel: "low",
    statusSeverity: "info",
    state: "Brief Review",
    validation: {
      errors: 0,
      warnings: 1,
      passed: 24,
      issues: [
        { severity: "warning", code: "V-201", message: "Optional package version mismatch in devDependencies" }
      ],
    },
    // compatibility field
    validationSummary: { errors: 0, warnings: 1, passed: 24 },
    approvalGate: {
      label: "Brief Review",
      state: "pending",
      note: "Pass 2 — approval gates are mock/read-only. Real gate wiring is Pass 4.",
    },
    artifacts: [
      {
        id: "art-prep-1",
        label: "Compiled Brief",
        path: "runs/brief_ready_for_review/brief.md",
        kind: "prompt",
        sizeHint: "8 KB",
        status: "ready",
        filename: "brief.md",
      },
      {
        id: "art-prep-2",
        label: "Packet Validation",
        path: "runs/brief_ready_for_review/packet-validation.json",
        kind: "validation",
        sizeHint: "1 KB",
        status: "ready",
        filename: "packet-validation.json",
      },
      {
        id: "art-prep-3",
        label: "Executor Brief",
        path: "runs/brief_ready_for_review/executor-brief.md",
        kind: "prompt",
        sizeHint: "5 KB",
        status: "ready",
        filename: "executor-brief.md",
      },
    ],
    latestEvents: [
      {
        id: "ev-brief-1",
        runId: "brief_ready_for_review",
        kind: "status_change",
        message: "Relay: Intake approved",
        createdAt: "2026-06-16T10:00:00.000Z",
      },
      {
        id: "ev-brief-2",
        runId: "brief_ready_for_review",
        kind: "log",
        message: "Relay: Running compiler...",
        createdAt: "2026-06-16T10:00:01.000Z",
      },
      {
        id: "ev-brief-3",
        runId: "brief_ready_for_review",
        kind: "status_change",
        message: "Relay: Brief compiled successfully - ready for review",
        createdAt: "2026-06-16T10:37:00.000Z",
      },
    ],
    logPreview: {
      lines: [
        "[10:00:00] Relay: Intake approved",
        "[10:00:01] Relay: Running compiler...",
        "[10:35:00] Relay: Brief compiled successfully",
        "[10:37:00] Relay: Brief ready for review",
      ],
      truncated: false,
    },
  },
  {
    id: "executor_running",
    name: "Add read-only Go backend JSON endpoints — Pass 3",
    title: "Add read-only Go backend JSON endpoints — Pass 3",
    packetId: "packet-2026-06-16-relay-pass-3-backend-json-endpoints",
    repo: "Paintersrp/relay",
    branch: "feature/backend-json",
    worktree: "relay-backend-json-wt",
    executorAdapter: 'opencode_go',
    executor: "opencode",
    model: "openrouter/auto",
    status: "executor_running",
    activeStep: "execute",
    lifecycleState: "execute",
    createdAt: "2026-06-15T20:00:00.000Z",
    updatedAt: "2026-06-15T21:15:00.000Z",
    stepLabels: STEP_LABELS,
    summary: "Integrate read endpoints into the Go server, enabling real-time data flow for runs, events, and artifacts.",
    riskLevel: "high",
    statusSeverity: "info",
    state: "Running",
    validation: {
      errors: 0,
      warnings: 0,
      passed: 0,
    },
    // compatibility field
    validationSummary: { errors: 0, warnings: 0, passed: 0 },
    approvalGate: {
      label: "Execution Gate",
      state: "skipped",
      note: "Executor is actively running. Gate applies after execution completes.",
    },
    artifacts: [
      {
        id: "art-exec-1",
        label: "Agent Brief",
        path: "runs/executor_running/agent-brief.md",
        kind: "prompt",
        sizeHint: "11 KB",
        status: "ready",
        filename: "agent-brief.md",
      },
    ],
    latestEvents: [
      {
        id: "ev-exec-1",
        runId: "executor_running",
        kind: "status_change",
        message: "Relay: Brief approved — dispatching to executor",
        createdAt: "2026-06-15T20:00:00.000Z",
      },
      {
        id: "ev-exec-2",
        runId: "executor_running",
        kind: "log",
        message: "OpenCode: Starting agent run...",
        createdAt: "2026-06-15T20:01:00.000Z",
      },
      {
        id: "ev-exec-3",
        runId: "executor_running",
        kind: "log",
        message: "OpenCode: go vet ./... — PASS",
        createdAt: "2026-06-15T21:00:00.000Z",
      },
    ],
    logPreview: {
      lines: [
        "[20:00:00] Relay: Brief approved — dispatching to executor",
        "[20:01:00] OpenCode: Starting agent run...",
        "[21:00:00] OpenCode: go vet ./... — PASS",
        "[21:10:00] OpenCode: go test ./... — running...",
        "[21:15:00] OpenCode: waiting for test output...",
      ],
      truncated: true,
    },
  },
  {
    id: "audit_ready_for_review",
    name: "Attach run audit packet generator — relay-audit-v1",
    title: "Attach run audit packet generator — relay-audit-v1",
    packetId: "packet-2026-06-15-relay-audit-generator-v1",
    repo: "Paintersrp/relay",
    branch: "feature/audit-generator",
    worktree: "relay-audit-wt",
    executorAdapter: "opencode_go",
    executor: "cline",
    model: "anthropic/claude-3-5-haiku",
    status: "audit_ready_for_review",
    activeStep: "audit",
    lifecycleState: "audit",
    createdAt: "2026-06-15T08:00:00.000Z",
    updatedAt: "2026-06-15T16:45:00.000Z",
    stepLabels: STEP_LABELS,
    summary: "Establish run generators to package evidence, verification commands output, and git diffs for manual review.",
    riskLevel: "critical",
    statusSeverity: "warning",
    state: "Audit Review",
    validation: {
      errors: 0,
      warnings: 2,
      passed: 31,
      issues: [
        { severity: "warning", code: "V-401", message: "Code coverage dropped below target threshold of 80%" }
      ],
    },
    // compatibility field
    validationSummary: { errors: 0, warnings: 2, passed: 31 },
    approvalGate: {
      label: "Audit Decision",
      state: "pending",
      note: "Pass 2 — approval gates are mock/read-only. Real gate wiring is Pass 4.",
    },
    artifacts: [
      {
        id: "art-aud-1",
        label: "Agent Result",
        path: "runs/audit_ready_for_review/result.md",
        kind: "result",
        sizeHint: "6 KB",
        status: "ready",
        filename: "result.md",
      },
      {
        id: "art-aud-2",
        label: "Validation Report",
        path: "runs/audit_ready_for_review/validation.json",
        kind: "validation",
        sizeHint: "3 KB",
        status: "ready",
        filename: "validation.json",
      },
      {
        id: "art-aud-3",
        label: "Git Diff",
        path: "runs/audit_ready_for_review/diff.patch",
        kind: "diff",
        sizeHint: "18 KB",
        status: "ready",
        filename: "diff.patch",
      },
      {
        id: "art-aud-4",
        label: "Audit Packet",
        path: "runs/audit_ready_for_review/audit.md",
        kind: "audit",
        sizeHint: "9 KB",
        status: "ready",
        filename: "audit.md",
      },
    ],
    latestEvents: [
      {
        id: "ev-audit-1",
        runId: "audit_ready_for_review",
        kind: "status_change",
        message: "Relay: Execution complete",
        createdAt: "2026-06-15T16:30:00.000Z",
      },
      {
        id: "ev-audit-2",
        runId: "audit_ready_for_review",
        kind: "validation_run",
        message: "Relay: Running go test ./... — PASS (31 tests)",
        createdAt: "2026-06-15T16:32:00.000Z",
      },
      {
        id: "ev-audit-3",
        runId: "audit_ready_for_review",
        kind: "artifact_created",
        message: "Relay: Audit packet generated — ready for review",
        createdAt: "2026-06-15T16:45:00.000Z",
      },
    ],
    logPreview: {
      lines: [
        "[16:30:00] Relay: Execution complete",
        "[16:31:00] Relay: Running go fmt ./...",
        "[16:32:00] Relay: Running go test ./... — PASS (31 tests)",
        "[16:40:00] Relay: Capturing git diff...",
        "[16:45:00] Relay: Audit packet generated — ready for review",
      ],
      truncated: false,
    },
  },
];

// Compatibility exports
export const MOCK_RUNS = mockRelayRuns;

export function getMockRun(id: string): RelayRun | undefined {
  return getMockRelayRunById(id);
}

export function getActiveStepRoute(run: RelayRun): string {
  return `/runs/${run.id}/${run.activeStep}`;
}

// Pass 2 helpers
export function getMockRelayRuns(): RelayRun[] {
  return JSON.parse(JSON.stringify(mockRelayRuns));
}

export function getMockRelayRunById(id: string): RelayRun | undefined {
  const run = mockRelayRuns.find((r) => r.id === id);
  if (!run) return undefined;
  return JSON.parse(JSON.stringify(run));
}

export function getMockRelayRunArtifacts(runId: string): RelayArtifact[] {
  const run = getMockRelayRunById(runId);
  return run ? run.artifacts : [];
}

export function getMockRelayRunEvents(runId: string): RelayRunEvent[] {
  const run = getMockRelayRunById(runId);
  return run ? run.latestEvents : [];
}
