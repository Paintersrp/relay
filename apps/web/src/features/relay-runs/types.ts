import type {
  WorkflowArtifactReference,
  WorkflowCanonicalValidation,
  WorkflowProjectReference,
  WorkflowRunStage,
} from "@/features/relay-plans/types";

export type {
  WorkflowArtifactReference,
  WorkflowProjectReference,
  WorkflowRunStage,
} from "@/features/relay-plans/types";

export type WorkflowRunStatus =
  | "created"
  | "setup_ready"
  | "executing"
  | "execution_failed"
  | "cancelled"
  | "validating"
  | "validation_failed"
  | "audit_ready"
  | "needs_revision"
  | "completed";

export type WorkflowExecutionAttemptStatus =
  | "pending"
  | "running"
  | "succeeded"
  | "failed"
  | "cancelled"
  | "timed_out";

export type WorkflowTerminalExecutionAttemptStatus = Exclude<
  WorkflowExecutionAttemptStatus,
  "pending" | "running"
>;

export type WorkflowImplementationActorKind = "applier" | "executor" | "hybrid";

export interface WorkflowAuditApplierEvidence {
  outcome: string;
  implementationResultArtifactReference: string;
  ledgerArtifactReference: string;
  changedFiles: string[];
  residualOperationIds: string[];
  failureClass?: string;
  failureReason?: string;
}

export interface WorkflowAuditExecutorEvidence {
  attemptId: string;
  attemptNumber: number;
  adapter: string;
  model: string;
  status: WorkflowExecutionAttemptStatus;
  result: WorkflowExecutionAttemptResult;
  startedAt?: string;
  finishedAt?: string;
}

export interface WorkflowAuditExecutionEvidence {
  actorKind: WorkflowImplementationActorKind;
  status: string;
  committedSha: string;
  completionSummary: string;
  blockersOrIncompleteWork: string[];
  reportedChangedFiles: string[];
  applier?: WorkflowAuditApplierEvidence;
  executor?: WorkflowAuditExecutorEvidence;
}

export interface WorkflowExecutionArtifact {
  artifactId: string;
  kind: string;
  mediaType: string;
  sha256: string;
  sizeBytes: number;
  createdAt: string;
}

export interface WorkflowExecutionAttemptSummary {
  attemptId: string;
  attemptNumber: number;
  adapter: string;
  model: string;
  status: WorkflowExecutionAttemptStatus;
  createdAt: string;
  startedAt?: string;
  finishedAt?: string;
  cancellationRequestedAt?: string;
  artifacts: WorkflowArtifactReference[];
}

export interface WorkflowExecutionAttemptResult
  extends Record<string, unknown> {
  cleanup_pending?: boolean;
  pending_terminal_status?: WorkflowTerminalExecutionAttemptStatus;
  termination_verified?: boolean;
}

export interface WorkflowExecutionAttempt
  extends Omit<WorkflowExecutionAttemptSummary, "artifacts"> {
  runId: string;
  result: WorkflowExecutionAttemptResult;
  artifacts: WorkflowExecutionArtifact[];
  liveStdout: string;
  liveStderr: string;
  liveStdoutTruncated: boolean;
  liveStderrTruncated: boolean;
  liveStdoutBytes: number;
  liveStderrBytes: number;
}

export interface WorkflowAuditPacket {
	auditPacketId: string;
	implementationActorKind: WorkflowImplementationActorKind;
  auditedCommit: string;
  packetSha256: string;
  status: string;
  staleReason?: string;
  createdAt: string;
  supersededAt?: string;
}

export interface WorkflowAuditDecision {
  auditDecisionId: string;
  auditedCommit: string;
  packetSha256: string;
  decision: string;
  rationale: string;
  createdAt: string;
}

export interface WorkflowAuditMaterialFinding {
  source: "executor_implementation" | "execution_spec" | "both";
  summary: string;
  evidence: string;
  requiredRemediation: string;
}

export interface WorkflowAuditTicketPackage {
  package: {
    packageId: string;
    packageSha256: string;
    workspaceId: string;
    featureSlug: string;
    selectionId: string;
    selectionState: string;
    authorityRevisionId: string;
    authoritySha256: string;
    sourceClosureId: string;
    sourceCommit: string;
  };
  tickets: Array<{
    sequence: number;
    ticketId: string;
    revisionRowId: number;
    revisionNumber: number;
    memberSha256: string;
    approvalId: string;
    approvalBasisSha256: string;
    authorityRevisionRowId: number;
    sourceClosureRowId: number;
    designBrief: { artifactReference: string; sha256: string };
  }>;
  mutationLeases: Array<{
    leaseId: string;
    state: string;
    certainty: string;
    reconciliationState: string;
    releasedAt: string;
  }>;
  bundleIntegration: {
    runId: string;
    executionPackageId: string;
    selectionId: string;
    selectionState: string;
    approvedRunStatus: string;
  };
}

export interface WorkflowAuditReadback {
  runId: string;
  runStatus: WorkflowRunStatus;
  packet: WorkflowAuditPacket;
  document: unknown;
  ticketPackage?: WorkflowAuditTicketPackage;
}

export interface RecordWorkflowAuditDecisionRequest {
  auditPacketId: string;
  packetSha256: string;
  auditedCommit: string;
  decision: "accepted" | "needs_revision";
  rationale: string;
  materialFindings: WorkflowAuditMaterialFinding[];
  observations: string[];
  operatorConfirmed: boolean;
}

export interface RecordWorkflowAuditDecisionResponse {
  runId: string;
  runStatus: WorkflowRunStatus;
  packet: WorkflowAuditPacket;
  decision: WorkflowAuditDecision;
  effects: {
    ticketRevisionDecisions: Array<{ auditTicketRevisionDecisionRowId: number; auditPacketTicketObligationRowId: number }>;
    ticketSatisfactions: Array<{ deliveryTicketRevisionRowId: number; auditTicketRevisionDecisionRowId: number }>;
    remediationSeeds: Array<{ remediationSeedId: string; auditPacketRowId: number; executionPackageRowId: number; auditedCommit: string }>;
  };
}

export interface WorkflowRunSummary {
  runId: string;
  featureSlug: string;
  repoTarget: string;
  status: WorkflowRunStatus;
  stage: WorkflowRunStage;
  branch: string;
  baseCommit: string;
  canonicalSha256: string;
  planId?: string;
  passId?: string;
  passNumber?: number;
  project?: WorkflowProjectReference;
  remediatesRunId?: string;
  createdAt: string;
  updatedAt: string;
  completedAt?: string;
  latestAttempt?: WorkflowExecutionAttemptSummary;
  currentPacket?: WorkflowAuditPacket;
  latestDecision?: WorkflowAuditDecision;
}

export interface WorkflowRunDetail {
  run: WorkflowRunSummary;
  attempts: WorkflowExecutionAttemptSummary[];
  artifacts: WorkflowArtifactReference[];
}

export interface WorkflowRunListFilters {
  status?: WorkflowRunStatus;
  planId?: string;
  passId?: string;
  limit?: number;
}

export interface WorkflowRunListResponse {
  count: number;
  runs: WorkflowRunSummary[];
}

export interface WorkflowSpecificationReview {
  run: WorkflowRunSummary;
  executionSpec: WorkflowArtifactReference;
  executorBrief: WorkflowArtifactReference;
  plan?: {
    planId: string;
    featureSlug: string;
    status: string;
  };
  pass?: {
    passId: string;
    number: number;
    name: string;
    repoTarget: string;
    status: string;
  };
  remediatesRunId?: string;
}

export interface CreateWorkflowRunRequest {
  fileName: string;
  canonicalContent: string;
  expectedSha256: string;
  planId?: string;
  passNumber?: number;
  remediatesRunId?: string;
}

export interface CreateWorkflowRunResponse {
  run: {
    runId: string;
    featureSlug: string;
    repoTarget: string;
    status: WorkflowRunStatus;
    branch: string;
    baseCommit: string;
    canonicalSha256: string;
    createdAt: string;
    updatedAt: string;
    reviewUrl: string;
  };
  artifacts: WorkflowArtifactReference[];
}

export interface WorkflowAuditStatus {
  runId: string;
  runStatus: WorkflowRunStatus;
  currentPacket?: WorkflowAuditPacket;
  latestPacket?: WorkflowAuditPacket;
  decision?: WorkflowAuditDecision;
}

export interface PrepareWorkflowAuditResponse {
  success: boolean;
  runId: string;
  runStatus: WorkflowRunStatus;
  packet: WorkflowAuditPacket;
  artifact: {
    artifactId: string;
    kind: string;
    sha256: string;
    sizeBytes: number;
    contentUrl: string;
  };
}

export interface WorkflowArtifactContent {
  artifact: WorkflowArtifactReference;
  offset: number;
  byteCount: number;
  encoding: "utf-8" | "base64";
  content: string;
  truncated: boolean;
  nextOffset?: number;
}

export type WorkflowExecutionSpecValidation = WorkflowCanonicalValidation;

export function workflowRunStageRoute(
  stage: WorkflowRunStage,
):
  | "/runs/$runId/specification"
  | "/runs/$runId/execute"
  | "/runs/$runId/audit" {
  switch (stage) {
    case "specification":
      return "/runs/$runId/specification";
    case "execute":
      return "/runs/$runId/execute";
    case "audit":
      return "/runs/$runId/audit";
    default:
      throw new Error(`Unsupported workflow Run stage: ${String(stage)}`);
  }
}
