import type { RelayRun, RepairValidationResponse } from "@/features/relay-runs";
import type {
  RunStageStepDefinition,
  RunStageStepStatusMap,
} from "@/components/relay/runStageVisualState";
import type { RunStageTone } from "@/components/relay/RunStagePrimitives";

type CompileRenderRunState = Pick<RelayRun, "status">;

export const COMPILE_RENDER_PIPELINE_STEPS: RunStageStepDefinition[] = [
  {
    id: "compile",
    label: "Compile packet",
    helperText: "Compile becomes available after Intake approval.",
  },
  { id: "packet-validation", label: "Packet validation" },
  {
    id: "repair",
    label: "Repair",
    naNote: "Becomes relevant only if validation fails.",
  },
  { id: "render-brief", label: "Render executor brief" },
  { id: "brief-validation", label: "Brief validation" },
  { id: "approval", label: "Approval" },
];

export type CompileRenderDisplayState =
  | "blocked"
  | "ready_to_compile"
  | "compiling"
  | "packet_invalid"
  | "repairing"
  | "repair_validated"
  | "packet_validated"
  | "rendering_brief"
  | "brief_ready"
  | "approving"
  | "approved";

export interface CompileRenderVisualStateInput {
  run: CompileRenderRunState;
  repairEligible?: boolean;
  repairResult?: RepairValidationResponse | null;
  compilePending?: boolean;
  repairPending?: boolean;
  renderBriefPending?: boolean;
  approvePending?: boolean;
  hasFailingBriefValidationReport?: boolean;
  hasPassingBriefValidationReport?: boolean;
}

const BASE_PIPELINE_STATUSES: RunStageStepStatusMap = {
  compile: "waiting",
  "packet-validation": "waiting",
  repair: "na",
  "render-brief": "waiting",
  "brief-validation": "waiting",
  approval: "waiting",
};

export function getCompileRenderDisplayState(
  input: CompileRenderVisualStateInput,
): CompileRenderDisplayState {
  if (input.compilePending) {
    return "compiling";
  }

  if (input.repairPending) {
    return "repairing";
  }

  if (input.renderBriefPending) {
    return "rendering_brief";
  }

  if (input.approvePending) {
    return "approving";
  }

  if (input.run.status === "approved_for_executor") {
    return "approved";
  }

  if (input.run.status === "brief_ready_for_review") {
    return "brief_ready";
  }

  if (
    input.run.status === "repair_validated" ||
    input.repairResult?.reValidationValid === true
  ) {
    return "repair_validated";
  }

  if (input.run.status === "packet_validated") {
    return "packet_validated";
  }

  if (input.run.status === "packet_validation_failed") {
    return "packet_invalid";
  }

  if (input.run.status === "approved_for_prepare") {
    return "ready_to_compile";
  }

  return "blocked";
}

export function getCompileRenderPipelineStatuses(
  input: CompileRenderVisualStateInput,
): RunStageStepStatusMap {
  const state = getCompileRenderDisplayState(input);
  const repairValidated =
    input.run.status === "repair_validated" ||
    input.repairResult?.reValidationValid === true;

  switch (state) {
    case "ready_to_compile":
      return {
        ...BASE_PIPELINE_STATUSES,
        compile: "active",
      };
    case "compiling":
      return {
        ...BASE_PIPELINE_STATUSES,
        compile: "running",
      };
    case "packet_invalid":
      return {
        ...BASE_PIPELINE_STATUSES,
        compile: "success",
        "packet-validation": "failed",
        repair: input.repairEligible ? "active" : "blocked",
      };
    case "repairing":
      return {
        ...BASE_PIPELINE_STATUSES,
        compile: "success",
        "packet-validation": "failed",
        repair: "running",
      };
    case "repair_validated":
      return {
        ...BASE_PIPELINE_STATUSES,
        compile: "success",
        "packet-validation": "success",
        repair: "success",
        "render-brief": "active",
      };
    case "packet_validated":
      return {
        ...BASE_PIPELINE_STATUSES,
        compile: "success",
        "packet-validation": "success",
        "render-brief": "active",
      };
    case "rendering_brief":
      return {
        ...BASE_PIPELINE_STATUSES,
        compile: "success",
        "packet-validation": "success",
        repair: repairValidated ? "success" : "na",
        "render-brief": "running",
      };
    case "brief_ready":
      return {
        ...BASE_PIPELINE_STATUSES,
        compile: "success",
        "packet-validation": "success",
        repair: repairValidated ? "success" : "na",
        "render-brief": "success",
        "brief-validation": input.hasPassingBriefValidationReport
          ? "success"
          : input.hasFailingBriefValidationReport
            ? "failed"
            : "waiting",
        approval: "active",
      };
    case "approving":
      return {
        ...BASE_PIPELINE_STATUSES,
        compile: "success",
        "packet-validation": "success",
        repair: repairValidated ? "success" : "na",
        "render-brief": "success",
        "brief-validation": input.hasFailingBriefValidationReport
          ? "failed"
          : "success",
        approval: "running",
      };
    case "approved":
      return {
        ...BASE_PIPELINE_STATUSES,
        compile: "success",
        "packet-validation": "success",
        repair: repairValidated ? "success" : "na",
        "render-brief": "success",
        "brief-validation": input.hasFailingBriefValidationReport
          ? "failed"
          : "success",
        approval: "accepted",
      };
    case "blocked":
      return {
        ...BASE_PIPELINE_STATUSES,
        compile: "blocked",
      };
  }
}

export function getCompileRenderStateCardCopy(
  state: CompileRenderDisplayState,
): {
  tone: RunStageTone;
  eyebrow: string;
  title: string;
  message: string;
} {
  switch (state) {
    case "ready_to_compile":
      return {
        tone: "info",
        eyebrow: "READY TO COMPILE",
        title: "Compile can run",
        message:
          "Intake is approved. Compile the canonical packet and validation report for this run.",
      };
    case "compiling":
      return {
        tone: "info",
        eyebrow: "COMPILING",
        title: "Compiling packet",
        message: "Relay is preparing the canonical packet and validation report.",
      };
    case "packet_invalid":
      return {
        tone: "danger",
        eyebrow: "VALIDATION FAILED",
        title: "Packet validation failed",
        message:
          "Review the validation report, retry compile, or run repair when the report is eligible.",
      };
    case "repairing":
      return {
        tone: "warning",
        eyebrow: "REPAIR RUNNING",
        title: "Repair is running",
        message: "Relay is attempting a constrained repair pass for the packet.",
      };
    case "repair_validated":
      return {
        tone: "success",
        eyebrow: "REPAIR VALIDATED",
        title: "Repair validated",
        message: "The repaired packet passed validation and is ready for brief rendering.",
      };
    case "packet_validated":
      return {
        tone: "success",
        eyebrow: "PACKET VALIDATED",
        title: "Packet validated",
        message: "The canonical packet passed validation and is ready for brief rendering.",
      };
    case "rendering_brief":
      return {
        tone: "info",
        eyebrow: "RENDERING BRIEF",
        title: "Rendering executor brief",
        message: "Relay is generating the executor brief from the validated packet.",
      };
    case "brief_ready":
      return {
        tone: "warning",
        eyebrow: "BRIEF READY",
        title: "Executor brief is ready for review",
        message: "Review the brief and validation evidence before approving execution.",
      };
    case "approving":
      return {
        tone: "info",
        eyebrow: "APPROVING",
        title: "Approving executor brief",
        message: "Relay is advancing the run to the executor stage.",
      };
    case "approved":
      return {
        tone: "success",
        eyebrow: "APPROVED",
        title: "Approved for executor",
        message: "Compile / Render is complete. Continue to Execute when ready.",
      };
    case "blocked":
      return {
        tone: "danger",
        eyebrow: "PREPARE BLOCKED",
        title: "Prepare is blocked",
        message: "Intake approval is required before Compile / Render can proceed.",
      };
  }
}
