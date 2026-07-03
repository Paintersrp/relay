// ============================================================
// Run Workbench Refinement — Attention surfacing (Requirement 3)
// ============================================================
//
// Pure, presentation-only helper that derives the Active_Route_Step's
// Attention_Item list from existing `blockers`/`warnings`/
// `revisionRequirements` arrays and existing Visual_State_Module output.
// It reads only existing data and introduces no backend field.

import type { AttentionItem, StepAttentionInput } from "./runWorkbenchViews";

/**
 * Derives the ordered Attention_Item list for the Active_Route_Step.
 *
 * - Each `blockers` entry becomes a "blocker" item.
 * - Each `revisionRequirements` entry becomes a "revision requirement" item.
 * - Each `warnings` entry becomes a "warning" item.
 * - When the Active_Route_Step's Visual_State_Module reports a blocked or
 *   failed display state, its state-card copy becomes exactly one
 *   "blocking state" item.
 * - Items are ordered by the fixed category sequence blocker → blocking
 *   state → revision requirement → warning, preserving source array order
 *   within each category.
 * - Returns an empty list when no attention data is present and the
 *   display state is neither blocked nor failed.
 */
export function deriveStepAttention(input: StepAttentionInput): AttentionItem[] {
  const blockerItems: AttentionItem[] = (input.blockers ?? []).map((message) => ({
    category: "blocker",
    label: "blocker",
    message,
  }));

  const blockingStateItems: AttentionItem[] = input.visualStateIsBlockedOrFailed
    ? [
        {
          category: "blocking state",
          label: "blocking state",
          message: input.blockingStateCopy ?? "",
        },
      ]
    : [];

  const revisionRequirementItems: AttentionItem[] = (
    input.revisionRequirements ?? []
  ).map((message) => ({
    category: "revision requirement",
    label: "revision requirement",
    message,
  }));

  const warningItems: AttentionItem[] = (input.warnings ?? []).map((message) => ({
    category: "warning",
    label: "warning",
    message,
  }));

  return [
    ...blockerItems,
    ...blockingStateItems,
    ...revisionRequirementItems,
    ...warningItems,
  ];
}
