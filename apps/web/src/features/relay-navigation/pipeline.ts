// ============================================================
// Relay Navigation — Run_Pipeline stage derivation
// ============================================================
//
// Pure, presentation-only derivations for the Run_Workbench stage rail.
//
// `derivePipelineStages(status)` produces the four Run_Pipeline_Stages
// (Intake -> Compile/Render -> Execute -> Audit) in pipeline order, marking
// exactly one stage as the current position and surfacing an attention
// indicator on the affected stage when the canonical `status` is in the closed
// blocked / awaiting-review classification.
//
// `adjacentStage(current, direction)` returns the immediately adjacent stage in
// pipeline order, clamping at the boundaries (previous from Intake stays Intake,
// next from Audit stays Audit).
//
// Design boundary (Requirements 6.3, 6.7, 6.8):
//   - The current stage and every stage's status are derived SOLELY from the
//     canonical `status` field. This module never reads or gates on derived
//     display fields (`activeStep`, `lifecycleState`, `state`, `statusSeverity`).
//   - Attention classification uses ONLY the closed `BLOCKED_STATUSES` /
//     `AWAITING_REVIEW_STATUSES` sets (Requirement 3.11). The broader
//     `getRelayAttentionReason` helper is intentionally NOT used here for
//     classification; it may be used elsewhere only for a display label/icon
//     (Requirement 3.12).
//   - These are pure view derivations over the existing canonical `status`
//     enum; they introduce no new status values and produce no state transition.

import type { RelayRunStatus, RelayRunStep } from "@/features/relay-runs";
import { AWAITING_REVIEW_STATUSES, BLOCKED_STATUSES } from "./statusSets";
import type { PipelineStageStatus, PipelineStageView } from "./types";

// ------------------------------------------------------------
// Stage order, labels, and route templates
// ------------------------------------------------------------

/** Canonical Run_Pipeline stage order: Intake -> Compile/Render -> Execute -> Audit. */
export const PIPELINE_STAGE_ORDER: readonly RelayRunStep[] = [
  "intake",
  "prepare",
  "execute",
  "audit",
] as const;

/** Human-readable stage labels for the pipeline stage rail. */
export const PIPELINE_STAGE_LABELS: Record<RelayRunStep, string> = {
  intake: "Intake",
  prepare: "Compile / Render",
  execute: "Execute",
  audit: "Audit",
};

/**
 * Stage -> route template mapping. Templates use the `$runId` param and are
 * navigated with `{ params: { runId } }`, consistent with the existing
 * RunStepper / TanStack Router convention.
 */
export const PIPELINE_STAGE_ROUTES: Record<RelayRunStep, string> = {
  intake: "/runs/$runId/intake",
  prepare: "/runs/$runId/prepare",
  execute: "/runs/$runId/execute",
  audit: "/runs/$runId/audit",
};

// ------------------------------------------------------------
// Canonical status -> stage mapping (deterministic, status-only)
// ------------------------------------------------------------
//
// Maps every canonical `RelayRunStatus` to the pipeline stage that is the Run's
// current position when it holds that status. Declared as an exhaustive
// `Record<RelayRunStatus, RelayRunStep>` so the compiler enforces coverage of
// the full canonical status contract.

const STATUS_TO_STAGE: Record<RelayRunStatus, RelayRunStep> = {
  // Intake
  draft: "intake",
  needs_cleanup: "intake",
  intake_received: "intake",
  intake_needs_review: "intake",
  validated: "intake",
  // Compile / Render (prepare)
  approved_for_prepare: "prepare",
  packet_validated: "prepare",
  packet_validation_failed: "prepare",
  repair_validated: "prepare",
  brief_ready_for_review: "prepare",
  // Execute
  approved_for_executor: "execute",
  executor_dispatched: "execute",
  executor_running: "execute",
  executor_done: "execute",
  executor_blocked: "execute",
  agent_done: "execute",
  agent_blocked: "execute",
  agent_result_needs_review: "execute",
  blocked: "execute",
  // Audit
  audit_ready: "audit",
  audit_ready_for_review: "audit",
  revision_required: "audit",
  accepted: "audit",
  accepted_with_warnings: "audit",
  validation_passed: "audit",
  validation_failed_accepted: "audit",
  validation_failed: "audit",
  completed: "audit",
};

/**
 * Resolve the current pipeline stage from the canonical `status` alone. Unknown
 * / out-of-enum status strings resolve to the first stage (Intake) so the
 * derivation remains a total function that never crashes on unexpected input.
 */
function resolveCurrentStage(status: RelayRunStatus | string): RelayRunStep {
  // Guard with an own-property check so status strings that collide with
  // `Object.prototype` members (e.g. "valueOf", "toString", "constructor") do
  // not resolve to inherited prototype values; unknown inputs default to Intake.
  return Object.prototype.hasOwnProperty.call(STATUS_TO_STAGE, status)
    ? STATUS_TO_STAGE[status as RelayRunStatus]
    : "intake";
}

// ------------------------------------------------------------
// Attention classification (closed set only)
// ------------------------------------------------------------

const ATTENTION_STATUSES: ReadonlySet<string> = new Set<string>([
  ...BLOCKED_STATUSES,
  ...AWAITING_REVIEW_STATUSES,
]);

/**
 * True when the canonical `status` is in the closed blocked / awaiting-review
 * classification (Requirement 3.11). This is the SOLE authority for the
 * pipeline attention indicator (Requirement 6.4); it never consults broader
 * label/icon helpers.
 */
function isAttentionStatus(status: RelayRunStatus | string): boolean {
  return ATTENTION_STATUSES.has(status);
}

// ------------------------------------------------------------
// Pipeline stage derivation
// ------------------------------------------------------------

/**
 * Derive the four Run_Pipeline_Stage views from the canonical `status` alone.
 *
 * - Returns exactly four stages in pipeline order (Requirement 6.1).
 * - Stages before the current position are `completed`; stages after are
 *   `pending`.
 * - The current position is marked `current`, giving exactly one current stage
 *   (Requirement 6.2) — unless the status is in the closed attention set, in
 *   which case the affected (current) stage is marked `attention` to surface
 *   the blocked / awaiting-review indicator (Requirement 6.4).
 * - Deterministic over `status` only; no derived display field is consulted
 *   (Requirements 6.3, 6.7, 6.8).
 */
export function derivePipelineStages(status: RelayRunStatus | string): PipelineStageView[] {
  const currentStage = resolveCurrentStage(status);
  const currentIndex = PIPELINE_STAGE_ORDER.indexOf(currentStage);
  const needsAttention = isAttentionStatus(status);

  return PIPELINE_STAGE_ORDER.map((step, index) => {
    let stageStatus: PipelineStageStatus;
    if (index < currentIndex) {
      stageStatus = "completed";
    } else if (index > currentIndex) {
      stageStatus = "pending";
    } else {
      stageStatus = needsAttention ? "attention" : "current";
    }

    return {
      step,
      label: PIPELINE_STAGE_LABELS[step],
      status: stageStatus,
      to: PIPELINE_STAGE_ROUTES[step],
      // All four stages have a corresponding Run route (Requirement 6.5), so
      // every stage is navigable.
      navigable: true,
    } satisfies PipelineStageView;
  });
}

// ------------------------------------------------------------
// Stage adjacency (clamped)
// ------------------------------------------------------------

/**
 * Return the immediately adjacent stage in pipeline order, clamping at the
 * boundaries: previous from Intake stays Intake, next from Audit stays Audit
 * (Requirements 4.8, 4.9). The result is always one of the four valid stages.
 */
export function adjacentStage(
  current: RelayRunStep,
  direction: "next" | "previous",
): RelayRunStep {
  const index = PIPELINE_STAGE_ORDER.indexOf(current);
  // Defensive: an unrecognized stage resolves to the first stage.
  if (index === -1) {
    return PIPELINE_STAGE_ORDER[0];
  }

  const nextIndex = direction === "next" ? index + 1 : index - 1;
  const clampedIndex = Math.min(Math.max(nextIndex, 0), PIPELINE_STAGE_ORDER.length - 1);
  return PIPELINE_STAGE_ORDER[clampedIndex];
}
