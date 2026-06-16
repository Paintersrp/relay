// ============================================================
// Relay Mock Data — Pass 1 frontend-only mock runs.
// All four required status combinations are represented.
// This data does NOT claim to be the backend API contract.
// Pass 2 defines the actual JSON API shape.
// ============================================================

import type { RelayRun } from './types'

const STEP_LABELS = {
  intake: 'Intake / Configure',
  prepare: 'Compile / Render',
  execute: 'Execute',
  audit: 'Audit / Close',
} as const

export const MOCK_RUNS: RelayRun[] = [
  {
    id: 'intake_needs_review',
    title: 'Add run workbench shell — Pass 1 scaffold',
    packetId: 'packet-2026-06-16-relay-pass-1-tanstack-start-workbench-shell',
    repo: 'Paintersrp/relay',
    branch: 'feature/tanstack-frontend',
    worktree: 'relay-frontend-wt',
    executor: 'deepseek-v4-flash',
    model: 'deepseek/deepseek-chat-v3-0324:free',
    status: 'intake_needs_review',
    activeStep: 'intake',
    createdAt: '2026-06-16T14:00:00.000Z',
    updatedAt: '2026-06-16T14:02:18.000Z',
    stepLabels: STEP_LABELS,
    validationSummary: { errors: 2, warnings: 5, passed: 18 },
    approvalGate: {
      label: 'Intake Review',
      state: 'pending',
      note: 'Pass 1 — approval gates are mock/read-only. Real gate wiring is Pass 4.',
    },
    artifacts: [
      { label: 'Original Handoff', path: 'runs/intake_needs_review/handoff.md', kind: 'handoff', sizeHint: '12 KB' },
      { label: 'Parsed Metadata', path: 'runs/intake_needs_review/metadata.json', kind: 'validation', sizeHint: '2 KB' },
    ],
    logPreview: {
      lines: [
        '[14:02:00] Relay: Received handoff packet',
        '[14:02:01] Relay: Parsing metadata...',
        '[14:02:02] Relay: Found 2 validation errors',
        '[14:02:02] Relay: Awaiting intake review',
      ],
      truncated: false,
    },
  },
  {
    id: 'brief_ready_for_review',
    title: 'Define frontend API contract — Pass 2 schema',
    packetId: 'packet-2026-06-16-relay-pass-2-frontend-api-contract',
    repo: 'Paintersrp/relay',
    branch: 'feature/api-contract',
    worktree: 'relay-api-contract-wt',
    executor: 'claude-sonnet-4-5',
    model: 'anthropic/claude-sonnet-4-5',
    status: 'brief_ready_for_review',
    activeStep: 'prepare',
    createdAt: '2026-06-16T10:00:00.000Z',
    updatedAt: '2026-06-16T10:37:00.000Z',
    stepLabels: STEP_LABELS,
    validationSummary: { errors: 0, warnings: 1, passed: 24 },
    approvalGate: {
      label: 'Brief Review',
      state: 'pending',
      note: 'Pass 1 — approval gates are mock/read-only. Real gate wiring is Pass 4.',
    },
    artifacts: [
      { label: 'Compiled Brief', path: 'runs/brief_ready_for_review/brief.md', kind: 'prompt', sizeHint: '8 KB' },
      { label: 'Packet Validation', path: 'runs/brief_ready_for_review/packet-validation.json', kind: 'validation', sizeHint: '1 KB' },
      { label: 'Executor Brief', path: 'runs/brief_ready_for_review/executor-brief.md', kind: 'prompt', sizeHint: '5 KB' },
    ],
    logPreview: {
      lines: [
        '[10:00:00] Relay: Intake approved',
        '[10:00:01] Relay: Running compiler...',
        '[10:35:00] Relay: Brief compiled successfully',
        '[10:37:00] Relay: Brief ready for review',
      ],
      truncated: false,
    },
  },
  {
    id: 'executor_running',
    title: 'Add read-only Go backend JSON endpoints — Pass 3',
    packetId: 'packet-2026-06-16-relay-pass-3-backend-json-endpoints',
    repo: 'Paintersrp/relay',
    branch: 'feature/backend-json',
    worktree: 'relay-backend-json-wt',
    executor: 'opencode',
    model: 'openrouter/auto',
    status: 'executor_running',
    activeStep: 'execute',
    createdAt: '2026-06-15T20:00:00.000Z',
    updatedAt: '2026-06-15T21:15:00.000Z',
    stepLabels: STEP_LABELS,
    validationSummary: { errors: 0, warnings: 0, passed: 0 },
    approvalGate: {
      label: 'Execution Gate',
      state: 'skipped',
      note: 'Executor is actively running. Gate applies after execution completes.',
    },
    artifacts: [
      { label: 'Agent Brief', path: 'runs/executor_running/agent-brief.md', kind: 'prompt', sizeHint: '11 KB' },
    ],
    logPreview: {
      lines: [
        '[20:00:00] Relay: Brief approved — dispatching to executor',
        '[20:01:00] OpenCode: Starting agent run...',
        '[21:00:00] OpenCode: go vet ./... — PASS',
        '[21:10:00] OpenCode: go test ./... — running...',
        '[21:15:00] OpenCode: waiting for test output...',
      ],
      truncated: true,
    },
  },
  {
    id: 'audit_ready_for_review',
    title: 'Attach run audit packet generator — relay-audit-v1',
    packetId: 'packet-2026-06-15-relay-audit-generator-v1',
    repo: 'Paintersrp/relay',
    branch: 'feature/audit-generator',
    worktree: 'relay-audit-wt',
    executor: 'cline',
    model: 'anthropic/claude-3-5-haiku',
    status: 'audit_ready_for_review',
    activeStep: 'audit',
    createdAt: '2026-06-15T08:00:00.000Z',
    updatedAt: '2026-06-15T16:45:00.000Z',
    stepLabels: STEP_LABELS,
    validationSummary: { errors: 0, warnings: 2, passed: 31 },
    approvalGate: {
      label: 'Audit Decision',
      state: 'pending',
      note: 'Pass 1 — approval gates are mock/read-only. Real gate wiring is Pass 4.',
    },
    artifacts: [
      { label: 'Agent Result', path: 'runs/audit_ready_for_review/result.md', kind: 'result', sizeHint: '6 KB' },
      { label: 'Validation Report', path: 'runs/audit_ready_for_review/validation.json', kind: 'validation', sizeHint: '3 KB' },
      { label: 'Git Diff', path: 'runs/audit_ready_for_review/diff.patch', kind: 'diff', sizeHint: '18 KB' },
      { label: 'Audit Packet', path: 'runs/audit_ready_for_review/audit.md', kind: 'audit', sizeHint: '9 KB' },
    ],
    logPreview: {
      lines: [
        '[16:30:00] Relay: Execution complete',
        '[16:31:00] Relay: Running go fmt ./...',
        '[16:32:00] Relay: Running go test ./... — PASS (31 tests)',
        '[16:40:00] Relay: Capturing git diff...',
        '[16:45:00] Relay: Audit packet generated — ready for review',
      ],
      truncated: false,
    },
  },
]

export function getMockRun(id: string): RelayRun | undefined {
  return MOCK_RUNS.find((r) => r.id === id)
}

export function getActiveStepRoute(run: RelayRun): string {
  return `/runs/${run.id}/${run.activeStep}`
}
