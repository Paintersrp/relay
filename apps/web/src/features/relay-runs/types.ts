// ============================================================
// Relay Run Types — Canonical frontend type contract
// ============================================================

// Required mock run IDs
export type RelayRunId =
  | "intake_needs_review"
  | "brief_ready_for_review"
  | "executor_running"
  | "audit_ready_for_review"
  | string;

// Required step values
export type RelayRunStep = "intake" | "prepare" | "execute" | "audit";

// Compatibility alias for Step Key
export type RelayRunStepKey = RelayRunStep;

// Canonical workflow states for action gating — all statuses the API can emit
export type RelayRunStatus =
  | "draft"
  | "needs_cleanup"
  | "intake_received"
  | "intake_needs_review"
  | "validated"
  | "approved_for_prepare"
  | "packet_validated"
  | "packet_validation_failed"
  | "repair_validated"
  | "brief_ready_for_review"
  | "approved_for_executor"
  | "executor_dispatched"
  | "executor_running"
  | "executor_done"
  | "executor_blocked"
  | "agent_done"
  | "agent_blocked"
  | "agent_result_needs_review"
  | "audit_ready"
  | "audit_ready_for_review"
  | "accepted"
  | "accepted_with_warnings"
  | "validation_passed"
  | "validation_failed_accepted"
  | "validation_failed"
  | "completed"
  | "blocked";

// Required lifecycle values
export type RelayRunLifecycleState =
  | "intake"
  | "prepare"
  | "execute"
  | "audit"
  | "completed"
  | "failed";

// Artifact kinds
export type RelayArtifactKind =
  | "prompt"
  | "handoff"
  | "result"
  | "audit"
  | "validation"
  | "diff";

// Canonical Relay Artifact type
export interface RelayArtifact {
  id: string;
  label: string;
  path: string;
  kind: RelayArtifactKind;
  sizeHint?: string;
  createdAt?: string;

  // Compatibility fields for legacy components
  status: string;
  filename: string;
  preview?: string;
}

// Compatibility alias for Artifact Preview
export type RelayArtifactPreview = RelayArtifact;

// Compatibility alias for old components that look for specific properties
export type RelayRunArtifactPreview = RelayArtifact;

// Validation types
export interface RelayValidationIssue {
  severity: "error" | "warning" | "info";
  code: string;
  message: string;
  path?: string;
}

export interface RelayValidationResult {
  errors: number;
  warnings: number;
  passed: number;
  issues?: RelayValidationIssue[];
}

// Compatibility alias for old validation panel item structure
export interface RelayRunValidationItem {
  label: string;
  message: string;
  status: string; // e.g. "error", "warning", "passed"
}

// Event kinds
export type RelayRunEventKind =
  | "log"
  | "status_change"
  | "artifact_created"
  | "validation_run"
  | "step_transition";

// Canonical Event structure
export interface RelayRunEvent {
  id: string;
  runId: string;
  kind: RelayRunEventKind;
  message: string;
  createdAt: string; // ISO-8601
  details?: Record<string, any>;
}

// Approval and Action models
export type RelayApprovalAction = "approve" | "needs_revision" | "blocked" | "reject" | "skip";

export interface RelayActionRequest {
  action: RelayApprovalAction | string;
  notes?: string;
  overrides?: {
    model?: string;
    repo?: string;
    branch?: string;
    worktree?: string;
    validationCommands?: string;
  };
}

export interface RelayActionResponse {
  success: boolean;
  runId: string;
  status: RelayRunStatus;
  lifecycleState: RelayRunLifecycleState;
  updatedAt: string;
}

export interface PlannerHandoffIntakeRequest {
  // Primary v2 fields
  planner_handoff_markdown?: string;
  repo_target?: string;
  branch_context?: string;
  run_id?: string;
  name?: string;

  // Compatibility aliases
  repo?: string;
  branch?: string;
  handoffPath?: string;
  packetId?: string;
  source?: string;
}

export interface PlannerHandoffIntakeResponse {
  success: boolean;
  runId: string;
  run_id?: string;
  status: RelayRunStatus;
  lifecycleState: RelayRunLifecycleState;
  createdAt: string;
  review_url?: string;
}

// Canonical API Error shape
export interface RelayApiErrorShape {
  error: string;
  message: string;
  code?: string;
  details?: Record<string, any>;
}

// Legacy structure compatibility support
export interface RelayApprovalGate {
  label: string;
  state: "pending" | "approved" | "rejected" | "skipped";
  note?: string;
}

export interface RelayLogPreview {
  lines: string[];
  truncated: boolean;
}

// Status severity for UI Badge
export type RelayRunStatusSeverity = "neutral" | "info" | "success" | "warning" | "danger";

// Run summary used by header component
export interface RelayRunSummary {
  id: string;
  title: string;
  repo: string;
  branch?: string;
  updatedAt: string;
  model?: string;
  statusSeverity: RelayRunStatusSeverity;
  state: string;
}

// Canonical RelayRun struct
export interface RelayRun {
  id: RelayRunId;
  name: string;
  repo: string;
  branch: string;
  activeStep: RelayRunStep;
  status: RelayRunStatus;
  lifecycleState: RelayRunLifecycleState;
  createdAt: string; // ISO-8601
  updatedAt: string; // ISO-8601
  summary: string;
  model: string;
  riskLevel: "low" | "medium" | "high" | "critical";
  validation: RelayValidationResult;
  artifacts: RelayArtifact[];
  latestEvents: RelayRunEvent[];
  statusSeverity: RelayRunStatusSeverity;
  state: string;

  // Legacy field support to prevent breaking current views
  title: string;
  packetId: string;
  worktree?: string;
  executor: string;
  validationSummary: RelayValidationResult;
  approvalGate: RelayApprovalGate;
  logPreview: RelayLogPreview;
  stepLabels: Record<RelayRunStep, string>;

  // Latest agent execution phase (separate from canonical status)
  latestExecutionStatus?: string;
}

// Run detail page workbench input structure
export interface RelayRunDetail extends RelayRun {
  validations: RelayRunValidationItem[];
  logs: string[];
}

// Step info structure
export interface RelayRunStepInfo {
  key: RelayRunStep;
  label: string;
  description: string;
}

// Step 4: Audit / Close-specific types

export type RelayAuditDecisionValue =
  | "accepted"
  | "accepted_with_warnings"
  | "revision_required"
  | "blocked"
  | "manual_review_required";

export const RELAY_AUDIT_DECISION_VALUES: RelayAuditDecisionValue[] = [
  "accepted",
  "accepted_with_warnings",
  "revision_required",
  "blocked",
  "manual_review_required",
];

export interface RelayAuditInputSummaryInfo {
  artifactId: string;
  artifactPath: string;
  available: boolean;
  generatedAt?: string;
  preview?: string;
  evidenceIncluded: string[];
  missingEvidence: string[];
}

export interface RelayAuditPacketInfo {
  artifactId: string;
  artifactPath: string;
  available: boolean;
  isManual: boolean;
  generatedAt?: string;
  decision?: RelayAuditDecisionValue;
  preview?: string;
  warnings: string[];
}

export interface RelayAuditDecisionStatus {
  currentDecision?: RelayAuditDecisionValue;
  source: "generated" | "manual" | "approved" | "none";
  approvedAt?: string;
  approvedBy?: string;
  notes?: string;
}

export interface RelayCommitSummary {
  changedFileArtifactIds: string[];
  commitMessageArtifactId?: string;
  commitMessagePreview?: string;
  commitMessageAvailable: boolean;
  validationSummary: string;
  auditDecisionSummary: string;
}

export interface RelayAuditActions {
  canGenerateAudit: boolean;
  canSubmitManual: boolean;
  canApproveAudit: boolean;
  canRequestRevision: boolean;
  canPrepareCommitMessage: boolean;
  canCloseRun: boolean;
  generateAuditUnavailableReason?: string;
  submitManualUnavailableReason?: string;
  approveAuditUnavailableReason?: string;
  requestRevisionUnavailableReason?: string;
  prepareCommitMessageUnavailableReason?: string;
  closeRunUnavailableReason?: string;
}

export interface RelayAuditPageData {
  runId: string;
  inputSummary: RelayAuditInputSummaryInfo;
  generatedPacket: RelayAuditPacketInfo;
  manualPacket?: RelayAuditPacketInfo;
  decision: RelayAuditDecisionStatus;
  commitSummary: RelayCommitSummary;
  actions: RelayAuditActions;
  warnings: string[];
  revisionRequirements: string[];
  blockers: string[];
}

// Step 3: Execute-specific types

export type RelayExecutorPhase =
  | "idle"
  | "dispatched"
  | "running"
  | "done"
  | "blocked"
  | "failed"
  | "unavailable";

export interface RelayChangedFile {
  path: string;
  status: string;
}

export interface RelayValidationCommand {
  command: string;
  status: string;
  output?: string;
}

export interface RelayExecuteActions {
  canStart: boolean;
  canCancel: boolean;
  canRecover: boolean;
  startUnavailableReason?: string;
  cancelUnavailableReason?: string;
  recoverUnavailableReason?: string;
}

// Exported standard steps array
export const RELAY_RUN_STEPS: RelayRunStepInfo[] = [
  {
    key: "intake",
    label: "Intake / Configure",
    description: "Submit handoff packet and review intake metadata."
  },
  {
    key: "prepare",
    label: "Compile / Render",
    description: "Compile implementation instructions and preview prompt brief."
  },
  {
    key: "execute",
    label: "Execute",
    description: "Run repository agent and stream execution feedback."
  },
  {
    key: "audit",
    label: "Audit / Close",
    description: "Generate audit packet, verify checks, and close work session."
  }
];
