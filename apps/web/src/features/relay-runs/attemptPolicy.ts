import type {
  WorkflowExecutionAttempt,
  WorkflowExecutionAttemptStatus,
  WorkflowExecutionAttemptSummary,
  WorkflowRunStatus,
} from "./types";

export interface WorkflowAttemptControlState {
  canStart: boolean;
  canCancel: boolean;
  canReconcile: boolean;
}

const STARTABLE_RUN_STATUSES: ReadonlySet<WorkflowRunStatus> = new Set([
  "setup_ready",
  "execution_failed",
  "cancelled",
]);

const NONTERMINAL_ATTEMPT_STATUSES: ReadonlySet<WorkflowExecutionAttemptStatus> =
  new Set(["pending", "running"]);

export function isNonterminalWorkflowAttemptStatus(
  status: WorkflowExecutionAttemptStatus | undefined,
): boolean {
  return status !== undefined && NONTERMINAL_ATTEMPT_STATUSES.has(status);
}

export function isTerminalWorkflowAttemptStatus(
  status: WorkflowExecutionAttemptStatus | undefined,
): boolean {
  return status !== undefined && !isNonterminalWorkflowAttemptStatus(status);
}

function isCleanupPendingWorkflowAttempt(
  attempt:
    | WorkflowExecutionAttempt
    | WorkflowExecutionAttemptSummary
    | null
    | undefined,
): attempt is WorkflowExecutionAttempt {
  return (
    attempt !== null &&
    attempt !== undefined &&
    "result" in attempt &&
    isNonterminalWorkflowAttemptStatus(attempt.status) &&
    attempt.result.cleanup_pending === true
  );
}

export function deriveWorkflowAttemptControlState(
  runStatus: WorkflowRunStatus,
  attempts: readonly WorkflowExecutionAttemptSummary[],
  selectedAttempt:
    | WorkflowExecutionAttempt
    | WorkflowExecutionAttemptSummary
    | null
    | undefined,
): WorkflowAttemptControlState {
  const selectedAttemptIsNonterminal =
    isNonterminalWorkflowAttemptStatus(selectedAttempt?.status);
  const hasNonterminalAttempt =
    attempts.some((attempt) =>
      isNonterminalWorkflowAttemptStatus(attempt.status),
    ) || selectedAttemptIsNonterminal;

  return {
    canStart:
      STARTABLE_RUN_STATUSES.has(runStatus) && !hasNonterminalAttempt,
    canCancel:
      runStatus === "executing" &&
      selectedAttemptIsNonterminal &&
      selectedAttempt?.cancellationRequestedAt === undefined,
    canReconcile:
      runStatus === "executing" &&
      isCleanupPendingWorkflowAttempt(selectedAttempt),
  };
}
