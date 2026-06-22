import type { RelayExecutorPhase } from "@/features/relay-runs";
import type {
  RunStageStepDefinition,
  RunStageStepStatusMap,
} from "@/components/relay/runStageVisualState";
import type { RunStageTone } from "@/components/relay/RunStagePrimitives";

type ExecuteRunState = {
  status: string;
  lifecycleState?: string;
};

export const EXECUTE_PIPELINE_STEPS: RunStageStepDefinition[] = [
  {
    id: "brief-approved",
    label: "Brief approved",
    helperText: "Brief approval is required before executor dispatch.",
  },
  {
    id: "dispatched",
    label: "Executor dispatched",
    helperText: "Dispatch the selected executor to begin execution.",
  },
  {
    id: "running",
    label: "Execution running",
    helperText: "Relay is waiting for executor output.",
  },
  { id: "result-captured", label: "Result captured" },
  {
    id: "audit-ready",
    label: "Audit ready",
    helperText: "Result evidence is captured and ready for audit review.",
  },
];

export type ExecuteDisplayState =
  | "blocked"
  | "ready"
  | "running"
  | "validating"
  | "complete"
  | "failed";

export interface ExecuteVisualStateInput {
  run: ExecuteRunState;
  executorPhase?: RelayExecutorPhase;
  preflightBlocked?: boolean;
  executePending?: boolean;
  cancelPending?: boolean;
  recoverPending?: boolean;
  validatePending?: boolean;
  hasResultArtifacts?: boolean;
  hasDiffArtifacts?: boolean;
  hasValidationArtifacts?: boolean;
}

const BASE_PIPELINE_STATUSES: RunStageStepStatusMap = {
  "brief-approved": "waiting",
  dispatched: "waiting",
  running: "waiting",
  "result-captured": "waiting",
  "audit-ready": "waiting",
};

export function getExecuteDisplayState(
  input: ExecuteVisualStateInput,
): ExecuteDisplayState {
  const status = input.run.status;

  if (input.validatePending || status === "local_validation_running") {
    return "validating";
  }

  if (input.executePending || input.cancelPending || input.recoverPending) {
    return "running";
  }

  if (
    status === "executor_blocked" ||
    status === "agent_blocked" ||
    input.preflightBlocked ||
    input.executorPhase === "failed"
  ) {
    return "failed";
  }

  if (
    status === "executor_dispatched" ||
    status === "executor_running" ||
    input.executorPhase === "dispatched" ||
    input.executorPhase === "running"
  ) {
    return "running";
  }

  if (
    status === "executor_done" ||
    status === "agent_done" ||
    status === "agent_result_needs_review" ||
    status === "validation_passed"
  ) {
    return "complete";
  }

  if (status === "approved_for_executor") {
    return "ready";
  }

  return "blocked";
}

export function getExecutePipelineStatuses(
  input: ExecuteVisualStateInput,
): RunStageStepStatusMap {
  const state = getExecuteDisplayState(input);

  switch (state) {
    case "ready":
      return {
        ...BASE_PIPELINE_STATUSES,
        "brief-approved": "success",
        dispatched: "active",
      };
    case "running":
      return {
        ...BASE_PIPELINE_STATUSES,
        "brief-approved": "success",
        dispatched: "success",
        running: "running",
      };
    case "validating":
      return {
        ...BASE_PIPELINE_STATUSES,
        "brief-approved": "success",
        dispatched: "success",
        running: "success",
        "result-captured": input.hasResultArtifacts ? "success" : "waiting",
        "audit-ready": "running",
      };
    case "complete":
      return {
        ...BASE_PIPELINE_STATUSES,
        "brief-approved": "success",
        dispatched: "success",
        running: "success",
        "result-captured": "success",
        "audit-ready":
          input.run.status === "validation_passed" ||
          input.hasValidationArtifacts ||
          input.hasDiffArtifacts
            ? "success"
            : "active",
      };
    case "failed":
      return {
        ...BASE_PIPELINE_STATUSES,
        "brief-approved": "success",
        dispatched: "success",
        running: "failed",
        "result-captured": input.hasResultArtifacts ? "success" : "waiting",
      };
    case "blocked":
      return {
        ...BASE_PIPELINE_STATUSES,
        "brief-approved": "blocked",
      };
  }
}

export function getExecuteStateCardCopy(state: ExecuteDisplayState): {
  tone: RunStageTone;
  eyebrow: string;
  title: string;
  message: string;
} {
  switch (state) {
    case "ready":
      return {
        tone: "info",
        eyebrow: "READY TO DISPATCH",
        title: "Executor can start",
        message:
          "The executor brief is approved. Start the selected executor when you are ready.",
      };
    case "running":
      return {
        tone: "info",
        eyebrow: "EXECUTION RUNNING",
        title: "Executor is in progress",
        message:
          "Relay is waiting for executor output. Review recent logs and captured artifacts as they appear.",
      };
    case "validating":
      return {
        tone: "info",
        eyebrow: "VALIDATION RUNNING",
        title: "Local validation is running",
        message:
          "Executor output is captured and Relay is collecting validation evidence.",
      };
    case "complete":
      return {
        tone: "success",
        eyebrow: "EXECUTION COMPLETE",
        title: "Result evidence is captured",
        message:
          "Review the result, changed files, and validation evidence before moving into audit.",
      };
    case "failed":
      return {
        tone: "danger",
        eyebrow: "EXECUTION BLOCKED",
        title: "Executor needs attention",
        message:
          "Review blocker evidence, command logs, and result artifacts before retrying or recovering.",
      };
    case "blocked":
      return {
        tone: "warning",
        eyebrow: "EXECUTION UNAVAILABLE",
        title: "Execute is blocked",
        message:
          "This run has not reached the executor stage. Complete the required upstream steps first.",
      };
  }
}
