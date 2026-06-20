import type { BadgeProps } from "@/components/ui/badge";
import type { RelayArtifactKind, RelayRunStatus, RelayRunStep } from "@/features/relay-runs";

export type RelayVisualStatusRole =
  | "running"
  | "blocked"
  | "complete"
  | "audit"
  | "validation"
  | "neutral";

export type RelayAttentionReason =
  | "executor-blocked"
  | "validation-failed"
  | "audit-required"
  | "intake-review"
  | "none";

export interface RelayVisualStatusConfig {
  label: string;
  role: RelayVisualStatusRole;
  badgeVariant: BadgeProps["variant"];
}

export const RELAY_STAGE_LABELS: Record<RelayRunStep, string> = {
  intake: "Intake",
  prepare: "Compile / Render",
  execute: "Execute",
  audit: "Audit",
};

export const RELAY_ARTIFACT_KIND_LABELS: Partial<Record<RelayArtifactKind, string>> = {
  prompt: "Prompt",
  handoff: "Handoff",
  result: "Result",
  audit: "Audit Artifact",
  validation: "Validation Artifact",
  diff: "Diff",
  planner_handoff: "Planner Handoff",
  parsed_frontmatter: "Parsed Frontmatter",
  run_config: "Run Config",
  intake_validation_report: "Intake Validation Report",
  mcp_audit_handback: "MCP Audit Handback",
  executor_result: "Executor Result",
  executor_stdout: "Executor Stdout",
  executor_stderr: "Executor Stderr",
  command_log: "Command Log",
  codex_last_message: "Codex Last Message",
  validation_run_json: "Validation Run JSON",
  validation_progress_json: "Validation Progress JSON",
  validation_stdout: "Validation Stdout",
  validation_stderr: "Validation Stderr",
  git_status_text: "Git Status",
  git_diff_stat: "Git Diff Stat",
  git_diff_numstat: "Git Diff Numstat",
  git_diff_patch: "Git Diff Patch",
  git_diff_name_status: "Git Diff Name Status",
};

export const RELAY_STATUS_CONFIG: Partial<Record<RelayRunStatus, RelayVisualStatusConfig>> = {
  draft: { label: "Draft", role: "neutral", badgeVariant: "secondary" },
  needs_cleanup: { label: "Needs Cleanup", role: "audit", badgeVariant: "warning" },
  intake_received: { label: "Intake Received", role: "neutral", badgeVariant: "info" },
  intake_needs_review: { label: "Intake Review", role: "audit", badgeVariant: "warning" },
  validated: { label: "Validated", role: "neutral", badgeVariant: "info" },
  approved_for_prepare: {
    label: "Approved to Prepare",
    role: "complete",
    badgeVariant: "success",
  },
  packet_validated: { label: "Packet Validated", role: "neutral", badgeVariant: "info" },
  packet_validation_failed: {
    label: "Validation Failed",
    role: "validation",
    badgeVariant: "destructive",
  },
  repair_validated: { label: "Repair Validated", role: "neutral", badgeVariant: "info" },
  brief_ready_for_review: { label: "Brief Review", role: "audit", badgeVariant: "info" },
  approved_for_executor: {
    label: "Approved for Executor",
    role: "complete",
    badgeVariant: "success",
  },
  executor_dispatched: { label: "Dispatching", role: "running", badgeVariant: "running" },
  executor_running: { label: "Running", role: "running", badgeVariant: "running" },
  executor_done: { label: "Executor Done", role: "complete", badgeVariant: "success" },
  executor_blocked: {
    label: "Executor Blocked",
    role: "blocked",
    badgeVariant: "destructive",
  },
  agent_done: { label: "Agent Done", role: "complete", badgeVariant: "success" },
  agent_blocked: { label: "Agent Blocked", role: "blocked", badgeVariant: "destructive" },
  agent_result_needs_review: {
    label: "Result Needs Review",
    role: "audit",
    badgeVariant: "warning",
  },
  audit_ready: { label: "Audit Ready", role: "audit", badgeVariant: "warning" },
  audit_ready_for_review: { label: "Audit Review", role: "audit", badgeVariant: "warning" },
  revision_required: {
    label: "Revision Required",
    role: "audit",
    badgeVariant: "warning",
  },
  accepted: { label: "Accepted", role: "complete", badgeVariant: "success" },
  accepted_with_warnings: {
    label: "Accepted with Warnings",
    role: "audit",
    badgeVariant: "warning",
  },
  validation_passed: {
    label: "Validation Passed",
    role: "complete",
    badgeVariant: "success",
  },
  validation_failed_accepted: {
    label: "Failed (Accepted)",
    role: "audit",
    badgeVariant: "warning",
  },
  validation_failed: {
    label: "Validation Failed",
    role: "validation",
    badgeVariant: "destructive",
  },
  completed: { label: "Completed", role: "complete", badgeVariant: "success" },
  blocked: { label: "Blocked", role: "blocked", badgeVariant: "destructive" },
};

const RELAY_ATTENTION_LABELS: Record<RelayAttentionReason, string> = {
  "executor-blocked": "Executor Blocked",
  "validation-failed": "Validation Failed",
  "audit-required": "Audit Required",
  "intake-review": "Intake Review",
  none: "",
};

function asStatusKey(status: RelayRunStatus | string): RelayRunStatus | undefined {
  return status in RELAY_STATUS_CONFIG ? (status as RelayRunStatus) : undefined;
}

export function getRelayStatusConfig(status: RelayRunStatus | string): RelayVisualStatusConfig {
  const statusKey = asStatusKey(status);
  if (!statusKey) {
    return {
      label: status,
      role: "neutral",
      badgeVariant: "outline",
    };
  }

  return RELAY_STATUS_CONFIG[statusKey] ?? {
    label: status,
    role: "neutral",
    badgeVariant: "outline",
  };
}

export function getRelayStatusRole(status: RelayRunStatus | string): RelayVisualStatusRole {
  return getRelayStatusConfig(status).role;
}

export function getRelayStageLabel(step: RelayRunStep | string): string {
  return step in RELAY_STAGE_LABELS
    ? RELAY_STAGE_LABELS[step as RelayRunStep]
    : step;
}

export function getRelayAttentionReason(status: RelayRunStatus | string): RelayAttentionReason {
  switch (status) {
    case "executor_blocked":
    case "agent_blocked":
    case "blocked":
      return "executor-blocked";
    case "packet_validation_failed":
    case "validation_failed":
      return "validation-failed";
    case "audit_ready":
    case "audit_ready_for_review":
    case "agent_result_needs_review":
    case "revision_required":
      return "audit-required";
    case "intake_needs_review":
    case "needs_cleanup":
      return "intake-review";
    default:
      return "none";
  }
}

export function getRelayAttentionLabel(reason: RelayAttentionReason): string {
  return RELAY_ATTENTION_LABELS[reason];
}
