import type {
  RelayRun,
} from "@/features/relay-runs";
import { isAuditCandidateStatus } from "@/features/relay-runs";
import type {
  RunStageStepDefinition,
  RunStageStepStatusMap,
} from "@/components/relay/runStageVisualState";
import type { RunStageTone } from "@/components/relay/RunStagePrimitives";

type AuditRunState = Pick<RelayRun, "status" | "lifecycleState">;

export const AUDIT_PIPELINE_STEPS: RunStageStepDefinition[] = [
  { id: "result-captured", label: "Executor result captured" },
  { id: "validation-reviewed", label: "Validation reviewed" },
  { id: "scope-reviewed", label: "Scope reviewed" },
  { id: "evidence-reviewed", label: "Evidence reviewed" },
  { id: "audit-decision", label: "Audit decision" },
];

export type AuditDisplayState =
  | "blocked"
  | "validation_required"
  | "validation_running"
  | "validation_failed"
  | "validation_accepted"
  | "validation_passed"
  | "audit_candidate"
  | "audit_candidate_with_executor_blocker"
  | "generating_audit"
  | "audit_ready"
  | "submitting_manual"
  | "approving"
  | "accepted"
  | "accepted_with_warnings"
  | "revision_required"
  | "preparing_commit_message"
  | "closing"
  | "completed";

export interface AuditVisualStateInput {
  run: AuditRunState;
  hasFinalValidationEvidence: boolean;
  validationAllowsAudit: boolean;
  hasAuditPacket: boolean;
  hasInputSummary: boolean;
  hasWarnings: boolean;
  generatePending: boolean;
  validatePending: boolean;
  manualSubmitPending: boolean;
  approvePending: boolean;
  revisionPending: boolean;
  commitMessagePending: boolean;
  closePending: boolean;
  acceptFailurePending: boolean;
  isAuditCandidate?: boolean;
  isAuditReady?: boolean;
  isAccepted?: boolean;
  isCompleted?: boolean;
  isRevisionRequired?: boolean;
  hasRevisionRequirements?: boolean;
  hasBlockers?: boolean;
}

const BASE_PIPELINE_STATUSES: RunStageStepStatusMap = {
  "result-captured": "waiting",
  "validation-reviewed": "waiting",
  "scope-reviewed": "waiting",
  "evidence-reviewed": "waiting",
  "audit-decision": "waiting",
};

function getRunStatus(input: AuditVisualStateInput): string {
  return input.run.status as string;
}

function isAuditReadyStatus(status: string): boolean {
  return status === "audit_ready" || status === "audit_ready_for_review";
}

function isBlockedStatus(status: string, lifecycleState: string): boolean {
  return (
    lifecycleState === "failed" ||
    status === "blocked" ||
    status.includes("rejected") ||
    (status.includes("failed") &&
      status !== "validation_failed" &&
      status !== "validation_failed_accepted")
  );
}

function getResultStatus(input: AuditVisualStateInput) {
  return getRunStatus(input) === "executor_blocked" ? "warning" : "success";
}

function getCompletedValidationStatus(input: AuditVisualStateInput) {
  return getRunStatus(input) === "validation_failed_accepted"
    ? "warning"
    : "success";
}

export function getAuditDisplayState(
  input: AuditVisualStateInput,
): AuditDisplayState {
  const status = getRunStatus(input);
  const lifecycleState = input.run.lifecycleState as string;
  const isAuditCandidate =
    input.isAuditCandidate ?? isAuditCandidateStatus(status);

  if (input.closePending) {
    return "closing";
  }

  if (input.commitMessagePending) {
    return "preparing_commit_message";
  }

  if (input.approvePending) {
    return "approving";
  }

  if (input.manualSubmitPending) {
    return "submitting_manual";
  }

  if (input.generatePending) {
    return "generating_audit";
  }

  if (
    input.validatePending ||
    input.acceptFailurePending ||
    status === "local_validation_running"
  ) {
    return "validation_running";
  }

  if (
    input.isCompleted ||
    lifecycleState === "completed" ||
    status === "completed"
  ) {
    return "completed";
  }

  if (status === "accepted_with_warnings") {
    return "accepted_with_warnings";
  }

  if (input.isAccepted || status === "accepted") {
    return "accepted";
  }

  if (input.isRevisionRequired || status === "revision_required") {
    return "revision_required";
  }

  if (input.isAuditReady || isAuditReadyStatus(status)) {
    return "audit_ready";
  }

  if (status === "validation_failed") {
    return "validation_failed";
  }

  if (status === "validation_failed_accepted") {
    return "validation_accepted";
  }

  if (status === "validation_passed") {
    return "validation_passed";
  }

  if (isAuditCandidate && status === "executor_blocked") {
    return "audit_candidate_with_executor_blocker";
  }

  if (isAuditCandidate && input.validationAllowsAudit) {
    return "audit_candidate";
  }

  if (isAuditCandidate) {
    return "validation_required";
  }

  if (
    input.hasBlockers ||
    input.hasRevisionRequirements ||
    isBlockedStatus(status, lifecycleState)
  ) {
    return "blocked";
  }

  return "blocked";
}

export function getAuditPipelineStatuses(
  input: AuditVisualStateInput,
): RunStageStepStatusMap {
  const state = getAuditDisplayState(input);
  const resultStatus = getResultStatus(input);
  const validationStatus = getCompletedValidationStatus(input);
  const hasScopeEvidence = input.hasInputSummary || input.hasAuditPacket;

  switch (state) {
    case "blocked":
      return {
        ...BASE_PIPELINE_STATUSES,
        "result-captured": "blocked",
      };
    case "validation_required":
      return {
        ...BASE_PIPELINE_STATUSES,
        "result-captured": resultStatus,
        "validation-reviewed": "active",
      };
    case "validation_running":
      return {
        ...BASE_PIPELINE_STATUSES,
        "result-captured": resultStatus,
        "validation-reviewed": "running",
      };
    case "validation_failed":
      return {
        ...BASE_PIPELINE_STATUSES,
        "result-captured": resultStatus,
        "validation-reviewed": "failed",
        "audit-decision": "blocked",
      };
    case "validation_accepted":
      return {
        ...BASE_PIPELINE_STATUSES,
        "result-captured": resultStatus,
        "validation-reviewed": "warning",
        "scope-reviewed": input.hasInputSummary ? "success" : "active",
      };
    case "validation_passed":
      return {
        ...BASE_PIPELINE_STATUSES,
        "result-captured": resultStatus,
        "validation-reviewed": "success",
        "scope-reviewed": input.hasInputSummary ? "success" : "active",
      };
    case "audit_candidate":
      return {
        ...BASE_PIPELINE_STATUSES,
        "result-captured": resultStatus,
        "validation-reviewed": validationStatus,
        "scope-reviewed": input.hasInputSummary ? "success" : "active",
        "evidence-reviewed": input.hasAuditPacket ? "success" : "waiting",
        "audit-decision": input.hasAuditPacket ? "active" : "waiting",
      };
    case "audit_candidate_with_executor_blocker":
      return {
        ...BASE_PIPELINE_STATUSES,
        "result-captured": "warning",
        "validation-reviewed": input.validationAllowsAudit
          ? validationStatus
          : input.hasFinalValidationEvidence
            ? "warning"
            : "active",
        "scope-reviewed":
          input.validationAllowsAudit || input.hasInputSummary
            ? input.hasInputSummary
              ? "success"
              : "active"
            : "waiting",
        "evidence-reviewed": input.hasAuditPacket ? "success" : "waiting",
        "audit-decision": input.hasAuditPacket ? "active" : "waiting",
      };
    case "generating_audit":
      return {
        ...BASE_PIPELINE_STATUSES,
        "result-captured": resultStatus,
        "validation-reviewed": input.validationAllowsAudit
          ? validationStatus
          : "active",
        "scope-reviewed": input.hasInputSummary ? "success" : "running",
      };
    case "audit_ready":
      return {
        ...BASE_PIPELINE_STATUSES,
        "result-captured": resultStatus,
        "validation-reviewed": validationStatus,
        "scope-reviewed": hasScopeEvidence ? "success" : "active",
        "evidence-reviewed": input.hasAuditPacket ? "success" : "active",
        "audit-decision": "active",
      };
    case "submitting_manual":
      return {
        ...BASE_PIPELINE_STATUSES,
        "result-captured": resultStatus,
        "validation-reviewed": validationStatus,
        "scope-reviewed": hasScopeEvidence ? "success" : "active",
        "evidence-reviewed": "running",
      };
    case "approving":
      return {
        ...BASE_PIPELINE_STATUSES,
        "result-captured": resultStatus,
        "validation-reviewed": validationStatus,
        "scope-reviewed": "success",
        "evidence-reviewed": "success",
        "audit-decision": "running",
      };
    case "accepted":
    case "preparing_commit_message":
    case "closing":
      return {
        ...BASE_PIPELINE_STATUSES,
        "result-captured": resultStatus,
        "validation-reviewed": validationStatus,
        "scope-reviewed": "success",
        "evidence-reviewed": "success",
        "audit-decision": "accepted",
      };
    case "accepted_with_warnings":
      return {
        ...BASE_PIPELINE_STATUSES,
        "result-captured": resultStatus,
        "validation-reviewed": validationStatus,
        "scope-reviewed": "success",
        "evidence-reviewed": "success",
        "audit-decision": "warning",
      };
    case "revision_required":
      return {
        ...BASE_PIPELINE_STATUSES,
        "result-captured": resultStatus,
        "validation-reviewed": validationStatus,
        "scope-reviewed": hasScopeEvidence ? "success" : "active",
        "evidence-reviewed": input.hasAuditPacket ? "success" : "active",
        "audit-decision": "revision",
      };
    case "completed":
      return {
        ...BASE_PIPELINE_STATUSES,
        "result-captured": resultStatus,
        "validation-reviewed": validationStatus,
        "scope-reviewed": "success",
        "evidence-reviewed": "success",
        "audit-decision": input.hasWarnings ? "warning" : "accepted",
      };
  }
}

export function getAuditStateCardCopy(
  state: AuditDisplayState,
): {
  tone: RunStageTone;
  eyebrow: string;
  title: string;
  message: string;
} {
  switch (state) {
    case "validation_required":
      return {
        tone: "warning",
        eyebrow: "VALIDATION REQUIRED",
        title: "Run validation before audit generation",
        message:
          "Capture final validation evidence before Relay can generate or review the audit packet.",
      };
    case "validation_running":
      return {
        tone: "info",
        eyebrow: "VALIDATION RUNNING",
        title: "Validation is in progress",
        message:
          "Relay is collecting validation evidence. Audit generation stays locked until validation completes.",
      };
    case "validation_failed":
      return {
        tone: "danger",
        eyebrow: "VALIDATION FAILED",
        title: "Validation failed",
        message:
          "Review the validation artifacts, rerun validation, or explicitly accept the failure before continuing.",
      };
    case "validation_accepted":
      return {
        tone: "warning",
        eyebrow: "VALIDATION ACCEPTED",
        title: "Validation failure accepted",
        message:
          "Audit can proceed with an accepted validation failure, but the closeout should preserve that rationale.",
      };
    case "validation_passed":
      return {
        tone: "success",
        eyebrow: "VALIDATION PASSED",
        title: "Audit can be generated",
        message:
          "Relay has the validation evidence it needs. Generate the audit summary to begin review.",
      };
    case "audit_candidate":
      return {
        tone: "success",
        eyebrow: "AUDIT READY TO GENERATE",
        title: "Ready to prepare audit evidence",
        message:
          "Executor output and validation evidence are in place. Generate or refresh the audit artifacts for review.",
      };
    case "audit_candidate_with_executor_blocker":
      return {
        tone: "warning",
        eyebrow: "EXECUTOR BLOCKER RECORDED",
        title: "Audit can review blocked executor evidence",
        message:
          "The executor ended in a blocker state, but Relay can still preserve evidence and prepare the audit record.",
      };
    case "generating_audit":
      return {
        tone: "info",
        eyebrow: "GENERATING AUDIT",
        title: "Preparing audit artifacts",
        message:
          "Relay is generating the audit input summary and packet from the captured run evidence.",
      };
    case "audit_ready":
      return {
        tone: "success",
        eyebrow: "READY FOR DECISION",
        title: "Audit packet is ready for review",
        message:
          "Review the audit packet, supporting evidence, and validation context before approving or requesting revision.",
      };
    case "submitting_manual":
      return {
        tone: "info",
        eyebrow: "SUBMITTING MANUAL PACKET",
        title: "Manual audit packet is being recorded",
        message:
          "Relay is saving the manual audit packet so the decision stage can continue with the updated evidence.",
      };
    case "approving":
      return {
        tone: "info",
        eyebrow: "APPROVING AUDIT",
        title: "Recording the audit decision",
        message:
          "Relay is saving the approval outcome without performing any git staging, commit, or push operation.",
      };
    case "accepted":
      return {
        tone: "success",
        eyebrow: "AUDIT APPROVED",
        title: "Audit is approved",
        message:
          "Prepare the suggested commit message artifact when needed, then close the run when the review is complete.",
      };
    case "accepted_with_warnings":
      return {
        tone: "warning",
        eyebrow: "APPROVED WITH WARNINGS",
        title: "Audit is approved with warnings",
        message:
          "The run can move to closeout, but the warnings should remain visible in the final review packet.",
      };
    case "revision_required":
      return {
        tone: "warning",
        eyebrow: "REVISION REQUIRED",
        title: "Audit revision requested",
        message:
          "Update the run output, regenerate the audit artifacts, and return to review once the revision requirements are addressed.",
      };
    case "preparing_commit_message":
      return {
        tone: "info",
        eyebrow: "PREPARING CLOSEOUT",
        title: "Preparing the commit message artifact",
        message:
          "Relay is generating a suggested commit message artifact only. It does not stage, commit, or push repository changes.",
      };
    case "closing":
      return {
        tone: "info",
        eyebrow: "CLOSING RUN",
        title: "Closing the run",
        message:
          "Relay is updating run state to completed while preserving the audit packet and supporting evidence on disk.",
      };
    case "completed":
      return {
        tone: "success",
        eyebrow: "RUN CLOSED",
        title: "Audit closeout is complete",
        message:
          "The run is read-only now. Validation evidence, audit artifacts, and closeout context remain available for review.",
      };
    case "blocked":
      return {
        tone: "danger",
        eyebrow: "AUDIT BLOCKED",
        title: "Audit cannot advance",
        message:
          "This run is in a blocked or unsupported state for closeout. Resolve the blocker before continuing the audit stage.",
      };
  }
}
