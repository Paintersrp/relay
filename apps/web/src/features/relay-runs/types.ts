// ============================================================
// Relay Run Types — frontend-only model.
// These types reflect what the UI needs to render Pass 1 mock data.
// They are NOT the backend API contract. Pass 2 will define that.
// ============================================================

export type RelayRunStatus =
  | 'intake_needs_review'
  | 'brief_ready_for_review'
  | 'executor_running'
  | 'audit_ready_for_review'
  | 'completed'
  | 'blocked'

export type RelayRunStep = 'intake' | 'prepare' | 'execute' | 'audit'

export interface RelayValidationIssue {
  severity: 'error' | 'warning' | 'info'
  code: string
  message: string
  path?: string
}

export interface RelayArtifactPreview {
  label: string
  path: string
  sizeHint?: string
  kind: 'prompt' | 'handoff' | 'result' | 'audit' | 'validation' | 'diff'
}

export interface RelayApprovalGate {
  label: string
  state: 'pending' | 'approved' | 'rejected' | 'skipped'
  /** NOTE: Pass 1 — approval actions are mock/disabled. Real submission is Pass 4. */
  note?: string
}

export interface RelayLogPreview {
  lines: string[]
  truncated: boolean
}

export interface RelayRun {
  id: string
  title: string
  packetId?: string
  repo: string
  branch: string
  worktree?: string
  executor: string
  model: string
  status: RelayRunStatus
  activeStep: RelayRunStep
  createdAt: string   // ISO-8601
  updatedAt: string   // ISO-8601
  validationSummary: {
    errors: number
    warnings: number
    passed: number
  }
  approvalGate: RelayApprovalGate
  artifacts: RelayArtifactPreview[]
  logPreview: RelayLogPreview
  /** Frontend display fields */
  stepLabels: Record<RelayRunStep, string>
}
