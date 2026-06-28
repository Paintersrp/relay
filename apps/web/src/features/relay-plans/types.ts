export type PlanAPIStatus = "active" | "complete" | "abandoned";

export type PlanAPIPassStatus =
  | "planned"
  | "ready_for_planner"
  | "handoff_ready"
  | "run_created"
  | "in_progress"
  | "audit_ready"
  | "completed"
  | "revision_required"
  | "blocked"
  | "skipped";

export interface PlannerPassPlanMeta {
  plan_id: string;
  schema_version: string;
  created_at: string;
  title: string;
  goal: string;
  repo_target: string;
  branch_context: string;
  status: PlanAPIStatus;
  /** Optional project ID embedded in the plan meta for pre-filling the workbench. */
  project_id?: string;
  projectId?: string;
}

export interface PlannerPassPlanSourceIntent {
  summary: string;
}

export interface PlannerPassPlanPass {
  pass_id: string;
  sequence: number;
  name: string;
  goal: string;
  intended_execution_scope: string[];
  non_goals: string[];
  dependencies: string[];
  status: PlanAPIPassStatus;
}

export interface PlannerPassPlan {
  plan_meta: PlannerPassPlanMeta;
  source_intent: PlannerPassPlanSourceIntent;
  passes: PlannerPassPlanPass[];
}

export interface PlanAPIPlan {
  id: string;
  planId: string;
  schemaVersion: string;
  title: string;
  goal: string;
  repoTarget: string;
  branchContext: string;
  status: PlanAPIStatus;
  sourceIntentSummary: string;
  sourceArtifactPath?: string;
  createdAt: string;
  updatedAt: string;
  projectRowId?: string;
  projectId?: string;
}

export interface PlanAPIRunSummary {
  id: string;
  title: string;
  status: string;
  lifecycleState: string;
  activeStep: string;
  workbenchPath: string;
  createdAt: string;
  updatedAt: string;
}

export interface PlanAPIContextSearchTerm {
  repoId: string;
  query: string;
  purpose: string;
  required?: boolean;
}

export interface PlanAPIContextFileRead {
  repoId: string;
  path: string;
  purpose: string;
  required?: boolean;
}

export interface PlanAPIContextPlan {
  requiredRepositories: string[];
  seedSearchTerms: PlanAPIContextSearchTerm[];
  seedFilesToRead: PlanAPIContextFileRead[];
  contextCoverageExpectations: string[];
  blockedIfMissing: string[];
}

export interface PlanAPISourceSnapshotRequirements {
  requireGitStatus?: boolean;
  requireCommitSha?: boolean;
  allowDirtyWorktree?: boolean;
}

export interface PlanAPIContextBudget {
  maxFiles?: number;
  maxBytes?: number;
  maxSearchResults?: number;
  maxContextLines?: number;
}

export interface PlanAPIPass {
  id: string;
  planRowId: string;
  passId: string;
  sequence: number;
  name: string;
  goal: string;
  intendedExecutionScope: string[];
  nonGoals: string[];
  dependencies: string[];
  status: PlanAPIPassStatus;
  associatedRunIds: string[];
  associatedRuns: PlanAPIRunSummary[];
  createdAt: string;
  updatedAt: string;
  passType?: string;
  contextPlan?: PlanAPIContextPlan;
  sourceSnapshotRequirements?: PlanAPISourceSnapshotRequirements;
  handoffReadinessCriteria?: string[];
  riskLevel?: string;
  contextBudget?: PlanAPIContextBudget;
  contextParseWarnings?: string[];
}

export interface PlanAPIReadPlan extends PlanAPIPlan {
  passCount: number;
  completionReady: boolean;
  completedPassCount?: number;
  inProgressPassCount?: number;
  plannedPassCount?: number;
  skippedPassCount?: number;
  currentPassId?: string;
  currentPassName?: string;
  currentPassGoal?: string;
  nextPassId?: string;
  nextPassName?: string;
  nextPassGoal?: string;
}

export interface PlanValidationIssue {
  severity?: "error" | "warning" | "info" | string;
  code?: string;
  message: string;
  path?: string;
  details?: Record<string, unknown>;
}

export interface PlanValidationResult {
  valid: boolean;
  issues: PlanValidationIssue[];
}

export interface ValidatePlanRequest {
  plan: PlannerPassPlan;
  sourceArtifactPath?: string;
}

export interface ValidatePlanResponse {
  success: boolean;
  validation: PlanValidationResult;
}

export interface SubmitPlanRequest {
  plan: PlannerPassPlan;
  sourceArtifactPath?: string;
}

export interface SubmitPlanResponse {
  success: boolean;
  plan: PlanAPIPlan;
  passes: PlanAPIPass[];
  validation: PlanValidationResult;
}

export interface PlanListFilters {
  status?: PlanAPIStatus;
  limit?: number;
  projectId?: string;
}

export interface PlanListResponse {
  success: boolean;
  count: number;
  plans: PlanAPIReadPlan[];
}

export interface PlanDetailResponse {
  success: boolean;
  plan: PlanAPIReadPlan;
  passes: PlanAPIPass[];
  completionReady: boolean;
}

export interface PlanPassDetailResponse {
  success: boolean;
  plan: PlanAPIPlan;
  pass: PlanAPIPass;
  completionReady: boolean;
}

// Work-packet response types

export interface WorkBlocker {
  code: string;
  message: string;
  recoverable: boolean;
}

export interface WorkProjectSummary {
  projectId: string;
  name: string;
}

export interface WorkPlanSummary {
  planId: string;
  status: string;
  title?: string;
}

export interface WorkPassSummary {
  passId: string;
  sequence: number;
  name: string;
  status: PlanAPIPassStatus | string;
  goal?: string;
}

export interface WorkDependencyStatus {
  passId: string;
  status: string;
  satisfied: boolean;
}

export interface WorkRunSummary {
  runId: string;
  title?: string;
  status: string;
  lifecycleState: string;
  activeStep: string;
  workbenchPath?: string;
}

export interface WorkContextSummary {
  contextPlan: PlanAPIContextPlan;
  sourceSnapshotId?: string;
  sourceSnapshotStatus?: string;
  contextPacketId?: string;
  contextPacketStatus?: string;
  coverageReportPath?: string;
  contextReady: boolean;
}

export interface SuggestedRunSubmission {
  tool: "create_run_from_planner_handoff" | string;
  arguments: {
    planId: string;
    passId: string;
  };
}

export interface PlannerJumpstartBasisReport {
  snapshotId?: string;
  snapshotStatus?: string;
  packetId?: string;
  packetStatus?: string;
}

export interface ContextAcquisitionAction {
  tool: string;
  arguments: Record<string, unknown>;
}

export interface PlannerJumpstart {
  readinessState: "ready" | "needs_source_snapshot" | "needs_context_packet" | "blocked";
  selectedPassSummary?: WorkPassSummary;
  sourceRequirements?: PlanAPISourceSnapshotRequirements;
  contextRequirements: PlanAPIContextPlan;
  sourceBasisReport?: PlannerJumpstartBasisReport;
  suggestedContextAcquisitionActions?: ContextAcquisitionAction[];
  handoffPreflightChecklist?: string[];
}

export interface NextPassWorkResponse {
  ok: boolean;
  tool: "get_next_pass_work" | string;
  project?: WorkProjectSummary;
  plan?: WorkPlanSummary;
  selectedPass?: WorkPassSummary;
  dependencyStatus?: WorkDependencyStatus[];
  associatedRuns?: WorkRunSummary[];
  context?: WorkContextSummary;
  handoffReadinessCriteria?: string[];
  suggestedRunSubmission?: SuggestedRunSubmission;
  plannerJumpstart?: PlannerJumpstart;
  blockers: WorkBlocker[];
}

export interface WorkArtifactReference {
  kind: string;
  label: string;
  filename: string;
  contentUrl: string;
  status: string;
  createdAt?: string;
}

export interface AuditPriorPassContext {
  priorPasses: WorkPassSummary[];
}

export interface AuditDecisionRoute {
  method: string;
  path: string;
  bodyShape?: unknown;
  allowedDecisions?: string[];
  decision?: string;
}

export interface AuditDecisionPayloadGuidance {
  primaryRoute: AuditDecisionRoute;
  convenienceRoutes?: AuditDecisionRoute[];
}

export interface NextAuditWorkFilters {
  passId?: string;
  runId?: string;
}

export interface NextAuditWorkResponse {
  ok: boolean;
  tool: "get_next_audit_work" | string;
  project?: WorkProjectSummary;
  plan?: WorkPlanSummary;
  selectedPass?: WorkPassSummary;
  selectedRun?: WorkRunSummary;
  executorResultReferences?: WorkArtifactReference[];
  validationReportReferences?: WorkArtifactReference[];
  auditPacketReferences?: WorkArtifactReference[];
  diffEvidenceReferences?: WorkArtifactReference[];
  priorPassContext?: AuditPriorPassContext;
  allowedDecisions?: string[];
  submitDecisionPayloadGuidance?: AuditDecisionPayloadGuidance;
  blockers: WorkBlocker[];
}

// ─── Plan Attempt / Review Gate Types (PASS-006) ─────────────────────────────

export type DriftReviewMode = "disabled" | "manual" | "automatic" | "external";

export type ModelTier =
  | "economy"
  | "standard"
  | "high_assurance"
  | "auto_escalate";

export type PlanAttemptStatus =
  | "draft"
  | "approved"
  | "submitted"
  | "voided"
  | "superseded";

export type PlanAttemptReviewWorkflowState =
  | "review_not_required"
  | "manual_review_available"
  | "automatic_review_pending_or_failed"
  | "external_review_required"
  | "approval_ready"
  | "drift_acknowledgement_required"
  | "revision_required"
  | "drift_review_blocked"
  | "ready_for_submission"
  | "submitted"
  | "voided"
  | "superseded";

export interface PlanArtifactRefAPI {
  path: string;
  sha256: string;
  artifactKind:
    | "planner-pass-plan-json"
    | "planner-pass-plan-markdown"
    | string;
}

export interface RawPlanJSONAPI {
  content: unknown;
  contentHash?: string;
}

export interface IntentPacketInputAPI {
  summary: string;
  literalUserRequest: string;
  constraints: string[];
  source: {
    capturedFrom: "planner_chat" | "revision_notes" | "imported_request" | string;
    capturedBy: string;
    sourceArtifactPath: string;
  };
  redactionStatus:
    | "not_required"
    | "redacted"
    | "verified_no_secrets"
    | "blocked_sensitive_content"
    | string;
  contentHash?: string;
}

export interface CreatePlanAttemptWithIntentRequest {
  planAttemptId?: string;
  intentPacketId?: string;
  intentThreadId?: string;
  planArtifactRef: PlanArtifactRefAPI;
  optionalMarkdownRef?: PlanArtifactRefAPI;
  rawPlanJson: RawPlanJSONAPI;
  driftReviewMode?: DriftReviewMode | string;
  modelTier?: ModelTier | string;
  intentPacket: IntentPacketInputAPI;
}

export interface PlanReviewSettingsAPI {
  projectId: string;
  driftReviewMode: DriftReviewMode | string;
  modelTier: ModelTier | string;
  manualModelCallWarning: string;
  automaticReviewEnabled: boolean;
  externalReviewSupported: boolean;
  createdAt?: string;
  updatedAt?: string;
}

export interface PlanReviewSettingsAPIResponse {
  success: boolean;
  blockerCode?: string;
  message?: string;
  settings?: PlanReviewSettingsAPI;
}

export interface PlanAttemptAPI {
  id: string;
  planAttemptId: string;
  projectId: string;
  intentThreadId: string;
  rootIntentPacketId: string;
  currentIntentPacketId: string;
  supersedesPlanAttemptId?: string;
  replacementPlanAttemptId?: string;
  status: PlanAttemptStatus | string;
  reviewState: string;
  workflowState: string;
  driftReviewMode: DriftReviewMode | string;
  modelTier: ModelTier | string;
  planJsonArtifactPath: string;
  planJsonArtifactSha256: string;
  rawPlanJsonHash: string;
  planMarkdownArtifactPath?: string;
  planMarkdownArtifactSha256?: string;
  acceptedDriftReviewId?: string;
  submittedPlanId?: string;
  createdAt: string;
  updatedAt: string;
}

export interface IntentPacketAPI {
  id: string;
  intentPacketId: string;
  projectId: string;
  intentThreadId: string;
  rootIntentPacketId: string;
  parentIntentPacketId?: string;
  revisionOfPlanAttemptId?: string;
  kind: string;
  capturedFrom: string;
  capturedBy: string;
  sourceArtifactPath: string;
  summary: string;
  literalUserRequest: string;
  constraintsJson: string;
  redactionStatus: string;
  contentHash: string;
  createdAt: string;
}

export interface IntentDriftReviewAPI {
  id: string;
  intentDriftReviewId: string;
  projectId: string;
  planAttemptId: string;
  intentThreadId: string;
  rootIntentPacketId: string;
  reviewedIntentPacketId: string;
  reviewPacketHash: string;
  reviewSource: "external" | "internal" | string;
  submittedBy: string;
  sourceArtifactPath?: string;
  overallAlignment:
    | "aligned"
    | "minor_drift"
    | "major_drift"
    | "unclear"
    | string;
  confidence: number;
  findingsJson: string;
  recommendedAction: string;
  approvalGateStatus: string;
  inputHash: string;
  outputHash: string;
  createdAt: string;
}

export interface PlanAttemptBlockerAPI {
  code: string;
  message: string;
  recoverable: boolean;
}

export interface ExternalReviewInstructionsAPI {
  reviewPacketRoute: string;
  submitReviewRoute: string;
}

export interface PlanAttemptReviewGateAPI {
  workflowState: PlanAttemptReviewWorkflowState | string;
  driftReviewMode: DriftReviewMode | string;
  modelTier: ModelTier | string;
  reviewRequired: boolean;
  modelCallAllowed: boolean;
  modelCallWarning?: string;
  acceptedDriftReviewId?: string;
  latestReview?: IntentDriftReviewAPI;
  allowedActions: string[];
  blockers?: PlanAttemptBlockerAPI[];
  externalReviewInstructions?: ExternalReviewInstructionsAPI;
}

export interface PlanAttemptReviewGateAPIResponse {
  success: boolean;
  blockerCode?: string;
  message?: string;
  reviewGate?: PlanAttemptReviewGateAPI;
}

export interface RunPlanAttemptDriftReviewRequest {
  allowModelCall: boolean;
  requestedTier?: string;
  forceHighAssurance?: boolean;
}

export interface ApprovePlanAttemptRequest {
  approved: true;
  acceptedDriftReviewId?: string;
  driftAcknowledged: boolean;
  noDriftReviewAcknowledged: boolean;
}

export interface SubmitPlanAttemptRequest {
  submissionConfirmed: true;
  reviewedPlanJsonArtifactSha256: string;
  acceptedDriftReviewId?: string;
}

export interface RevisePlanAttemptRequest {
  planArtifactRef: PlanArtifactRefAPI;
  optionalMarkdownRef?: PlanArtifactRefAPI;
  rawPlanJson: RawPlanJSONAPI;
  intentPacket: IntentPacketInputAPI;
}

export interface PlanAttemptAPIResponse {
  success: boolean;
  blockerCode?: string;
  message?: string;
  intentPacket?: IntentPacketAPI;
  planAttempt?: PlanAttemptAPI;
  driftReview?: IntentDriftReviewAPI;
  plan?: PlanAPIPlan;
  passes?: PlanAPIPass[];
  reviewGate?: PlanAttemptReviewGateAPI;
  reviewAction?: {
    action: string;
    ok: boolean;
    failureCode?: string;
    message?: string;
    escalated?: boolean;
    escalationReason?: string;
    finalTier?: string;
  };
}
