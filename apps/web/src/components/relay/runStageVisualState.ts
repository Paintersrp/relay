import type * as React from "react";

export type RunStageStepStatus =
  | "success"
  | "active"
  | "running"
  | "blocked"
  | "failed"
  | "accepted"
  | "warning"
  | "revision"
  | "waiting"
  | "na";

export interface RunStageStepDefinition {
  id: string;
  label: string;
  helperText?: React.ReactNode;
  naNote?: React.ReactNode;
}

export type RunStageStepStatusMap = Record<string, RunStageStepStatus>;

export function getRunStageStepStatus(
  statuses: RunStageStepStatusMap,
  stepId: string,
): RunStageStepStatus {
  return statuses[stepId] ?? "waiting";
}

export function isRunStageStepTerminal(
  status: RunStageStepStatus,
): boolean {
  return status === "success" || status === "accepted" || status === "warning";
}

export function isRunStageStepAttention(
  status: RunStageStepStatus,
): boolean {
  return (
    status === "active" ||
    status === "running" ||
    status === "blocked" ||
    status === "failed" ||
    status === "revision" ||
    status === "warning"
  );
}

export function getRunStageStepLabel(
  status: RunStageStepStatus,
): string | null {
  switch (status) {
    case "success":
      return "Complete";
    case "active":
      return "Ready";
    case "running":
      return "Running";
    case "blocked":
      return "Blocked";
    case "failed":
      return "Failed";
    case "accepted":
      return "Accepted";
    case "warning":
      return "Accepted w/ warnings";
    case "revision":
      return "Revision required";
    case "na":
      return "n/a";
    case "waiting":
      return null;
  }
}
