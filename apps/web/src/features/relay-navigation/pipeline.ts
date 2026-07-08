// ============================================================
// Relay Navigation — Canonical Run_Pipeline stage derivation
// ============================================================
//
// Pure, presentation-only derivations for the Run_Workbench stage rail.
//
// `derivePipelineStages(durableStage, selectedRouteStage)` produces the three
// canonical Run_Pipeline_Stages (Specification -> Execute -> Audit) in pipeline
// order. Two independent inputs drive stage presentation:
//
//   1. `durableStage` (WorkflowRunStage from the canonical Run response) —
//      controls the maximum keyboard-reachable stage. Stages ahead of the
//      durable stage are `pending` and non-navigable; stages at or before
//      the durable stage are navigable.
//   2. `selectedRouteStage` (the currently selected route stage) — controls
//      `aria-current` (status "current") and the panel being reviewed.
//      Reviewing Specification or Execute does not reduce the operator's
//      ability to return by keyboard to a later durable stage.
//
// `adjacentStage(current, direction)` returns the immediately adjacent stage in
// pipeline order, clamping at the boundaries (previous from specification stays
// specification, next from audit stays audit).
//
// Design boundary (per brief Pass 4 context):
//   - The selected route stage and every stage's navigability are derived
//     SOLELY from the canonical `WorkflowRunStage` field. This module never
//     reads or gates on legacy `RelayRunStatus` derived display fields.
//   - Attention classification uses ONLY the closed `BLOCKED_STATUSES` /
//     `AWAITING_REVIEW_STATUSES` sets (statusSets.ts).
//   - These are pure view derivations; they introduce no new status values
//     and produce no state transition.

import type { WorkflowRunStage, WorkflowRunStatus } from "@/features/relay-runs";
import { AWAITING_REVIEW_STATUSES, BLOCKED_STATUSES } from "./statusSets";
import type { PipelineStageStatus, PipelineStageView } from "./types";

// ------------------------------------------------------------
// Stage order, labels, and route templates
// ------------------------------------------------------------

/** Canonical Run_Pipeline stage order: Specification -> Execute -> Audit. */
export const PIPELINE_STAGE_ORDER: readonly WorkflowRunStage[] = [
  "specification",
  "execute",
  "audit",
] as const;

/** Human-readable stage labels for the pipeline stage rail. */
export const PIPELINE_STAGE_LABELS: Record<WorkflowRunStage, string> = {
  specification: "Specification",
  execute: "Execute",
  audit: "Audit",
};

/**
 * Stage -> route template mapping. Templates use the `$runId` param and are
 * navigated with `{ params: { runId } }`, consistent with TanStack Router convention.
 */
export const PIPELINE_STAGE_ROUTES: Record<WorkflowRunStage, string> = {
  specification: "/runs/$runId/specification",
  execute: "/runs/$runId/execute",
  audit: "/runs/$runId/audit",
};

// ------------------------------------------------------------
// Canonical WorkflowRunStatus -> durable stage mapping
// ------------------------------------------------------------
//
// Maps every canonical `WorkflowRunStatus` to the pipeline stage that is the
// Run's durable stage when it holds that status.

const WORKFLOW_STATUS_TO_STAGE: Record<WorkflowRunStatus, WorkflowRunStage> = {
  // Specification (created, spec ready)
  created: "specification",
  setup_ready: "specification",
  // Execute (executing and all execute-related terminal/fail states)
  executing: "execute",
  execution_failed: "execute",
  cancelled: "execute",
  validating: "execute",
  validation_failed: "execute",
  // Audit
  audit_ready: "audit",
  needs_revision: "audit",
  completed: "audit",
};

/**
 * Resolve the durable pipeline stage from a canonical `WorkflowRunStatus`.
 * Returns `undefined` when `status` does not map to any canonical stage,
 * so the derivation stays a total function that never crashes on unexpected
 * input.
 */
export function resolveWorkflowStage(
  status: WorkflowRunStatus | string,
): WorkflowRunStage | undefined {
  return Object.prototype.hasOwnProperty.call(WORKFLOW_STATUS_TO_STAGE, status)
    ? WORKFLOW_STATUS_TO_STAGE[status as WorkflowRunStatus]
    : undefined;
}

// ------------------------------------------------------------
// Attention classification (closed set only)
// ------------------------------------------------------------

const ATTENTION_STATUSES: ReadonlySet<string> = new Set<string>([
  ...BLOCKED_STATUSES,
  ...AWAITING_REVIEW_STATUSES,
]);

/**
 * True when the canonical `WorkflowRunStatus` is in the closed
 * blocked / awaiting-review classification.
 */
function isAttentionStatus(status: WorkflowRunStatus | string): boolean {
  return ATTENTION_STATUSES.has(status);
}

// ------------------------------------------------------------
// Pipeline stage derivation
// ------------------------------------------------------------

/**
 * Derive the three canonical Run_Pipeline_Stage views.
 *
 * @param durableStage — The canonical Run's durable stage (from `run.stage`).
 *   Controls the maximum reachable stage: stages after the durable stage are
 *   `pending` and non-navigable.
 * @param selectedRouteStage — The currently selected route stage. Controls
 *   `aria-current` (`status === "current"`). Reviewing an earlier stage does
 *   not reduce keyboard reachability of the durable stage or later completed
 *   stages.
 * @param runStatus — Optional canonical Run status for attention classification.
 *   When the status is in the closed attention set AND the durable stage is the
 *   selected stage, that stage is marked "attention" instead of "current".
 *
 * Returns exactly three stages in pipeline order (Specification, Execute, Audit).
 * - Stages before the durable stage index: `completed` and navigable.
 * - The durable stage: `completed` or `current`/`attention` (if selected).
 * - Stages after the durable stage: `pending` and non-navigable.
 * - WHEN `durableStage` is undefined, all three stages fall back to `pending`
 *   and non-navigable (no canonical stage resolved from status).
 */
export function derivePipelineStages(
  durableStage: WorkflowRunStage | undefined,
  selectedRouteStage?: WorkflowRunStage | undefined,
  runStatus?: WorkflowRunStatus | string,
): PipelineStageView[] {
  const durableIndex =
    durableStage !== undefined ? PIPELINE_STAGE_ORDER.indexOf(durableStage) : -1;
  const needsAttention =
    runStatus !== undefined && durableIndex !== -1 && isAttentionStatus(runStatus);

  return PIPELINE_STAGE_ORDER.map((step, index) => {
    const isSelected = step === selectedRouteStage;
    const isAtOrBeforeDurable = durableIndex !== -1 && index <= durableIndex;

    let stageStatus: PipelineStageStatus;
    if (durableIndex === -1) {
      // No durable stage resolved: all stages are pending.
      stageStatus = "pending";
    } else if (isSelected) {
      // The currently viewed route stage: mark as current or attention.
      stageStatus = needsAttention && step === durableStage ? "attention" : "current";
    } else if (isAtOrBeforeDurable) {
      // Stages at or before the durable position that aren't selected: completed.
      stageStatus = "completed";
    } else {
      // Stages beyond the durable position: pending.
      stageStatus = "pending";
    }

    return {
      step,
      label: PIPELINE_STAGE_LABELS[step],
      status: stageStatus,
      to: PIPELINE_STAGE_ROUTES[step],
      // Stages at or before the durable stage are navigable; stages beyond
      // the durable stage remain unavailable per the brief requirement.
      navigable: isAtOrBeforeDurable,
    } satisfies PipelineStageView;
  });
}

// ------------------------------------------------------------
// Stage adjacency (clamped)
// ------------------------------------------------------------

/**
 * Return the immediately adjacent stage in pipeline order, clamping at the
 * boundaries: previous from specification stays specification, next from audit
 * stays audit. The result is always one of the three valid canonical stages.
 */
export function adjacentStage(
  current: WorkflowRunStage,
  direction: "next" | "previous",
): WorkflowRunStage {
  const index = PIPELINE_STAGE_ORDER.indexOf(current);
  // Defensive: an unrecognized stage resolves to the first stage.
  if (index === -1) {
    return PIPELINE_STAGE_ORDER[0];
  }

  const nextIndex = direction === "next" ? index + 1 : index - 1;
  const clampedIndex = Math.min(Math.max(nextIndex, 0), PIPELINE_STAGE_ORDER.length - 1);
  return PIPELINE_STAGE_ORDER[clampedIndex];
}
