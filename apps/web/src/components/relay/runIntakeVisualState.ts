import type { RelayRun } from "@/features/relay-runs";
import type {
  RunStageStepDefinition,
  RunStageStepStatusMap,
} from "./runStageVisualState";
import type { RunStageTone } from "./RunStagePrimitives";

type IntakeRunState = Pick<RelayRun, "status" | "activeStep">;

export const INTAKE_PIPELINE_STEPS: RunStageStepDefinition[] = [
  {
    id: "handoff-loaded",
    label: "Handoff loaded",
    helperText: "Awaiting handoff artifact from the Planner.",
  },
  {
    id: "config-reviewed",
    label: "Configuration reviewed",
    helperText: "Review repository, branch, execution profile, and target model.",
  },
  {
    id: "executor-selected",
    label: "Executor selected",
    helperText: "Select the executor adapter for this run.",
  },
  {
    id: "model-selected",
    label: "Model selected",
    helperText: "Select the target model for the executor.",
  },
  {
    id: "intake-approved",
    label: "Intake approved",
    helperText: "Approve Intake to begin Compile / Render.",
  },
];

export type IntakeDisplayState =
  | "review"
  | "received"
  | "approved"
  | "blocked"
  | "default";

export function getIntakeDisplayState(run: IntakeRunState): IntakeDisplayState {
  if (run.status === "approved_for_prepare" || run.activeStep === "prepare") {
    return "approved";
  }

  if (run.status === "intake_needs_review") {
    return "review";
  }

  if (run.status === "intake_received") {
    return "received";
  }

  if (run.status === "blocked") {
    return "blocked";
  }

  return "default";
}

export function getIntakePipelineStatuses({
  run,
  executorAdapter,
  model,
}: {
  run: IntakeRunState;
  repo?: string;
  branch?: string;
  executorAdapter?: string;
  model?: string;
}): RunStageStepStatusMap {
  const state = getIntakeDisplayState(run);

  if (state === "approved") {
    return {
      "handoff-loaded": "success",
      "config-reviewed": "success",
      "executor-selected": "success",
      "model-selected": "success",
      "intake-approved": "accepted",
    };
  }

  if (state === "blocked") {
    return {
      "handoff-loaded": "blocked",
      "config-reviewed": "waiting",
      "executor-selected": "waiting",
      "model-selected": "waiting",
      "intake-approved": "waiting",
    };
  }

  if (state === "review" || state === "received") {
    return {
      "handoff-loaded": "success",
      "config-reviewed": "active",
      "executor-selected": executorAdapter ? "success" : "waiting",
      "model-selected": model ? "success" : "waiting",
      "intake-approved": "waiting",
    };
  }

  return {
    "handoff-loaded": "success",
    "config-reviewed": "waiting",
    "executor-selected": executorAdapter ? "success" : "waiting",
    "model-selected": model ? "success" : "waiting",
    "intake-approved": "waiting",
  };
}

export function getIntakeStateCardCopy(
  state: IntakeDisplayState,
): {
  tone: RunStageTone;
  eyebrow: string;
  title: string;
  message: string;
} {
  switch (state) {
    case "review":
      return {
        tone: "warning",
        eyebrow: "INTAKE REVIEW",
        title: "Intake needs review",
        message: "Review the handoff and configuration before approving this run.",
      };
    case "received":
      return {
        tone: "info",
        eyebrow: "INTAKE RECEIVED",
        title: "Intake received",
        message:
          "Review the resolved handoff configuration before approving this run.",
      };
    case "approved":
      return {
        tone: "success",
        eyebrow: "INTAKE APPROVED",
        title: "Intake approved",
        message: "The run is ready for Compile / Render.",
      };
    case "blocked":
      return {
        tone: "danger",
        eyebrow: "INTAKE BLOCKED",
        title: "Intake is blocked",
        message: "Resolve blocking issues before this run can proceed.",
      };
    case "default":
      return {
        tone: "default",
        eyebrow: "INTAKE",
        title: "Intake status unavailable",
        message: "Relay has not exposed a reviewable intake state for this run.",
      };
  }
}
