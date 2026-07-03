// ============================================================
// Run Workbench Refinement — Plan/Pass jump link resolution
// ============================================================
//
// Pure, presentation-only derivation of the Plan_Pass_Link (Requirement 6).
// Reuses the existing route-selection logic in `getRunPlanContextHrefs`
// (RunPlanContext.tsx) rather than reimplementing plan/pass route
// selection, and adds display-label/truncation behavior on top of it.

import { getRunPlanContextHrefs } from "@/components/relay/RunPlanContext";

import type { RelayRunPlanContext } from "./types";
import type { PlanPassLinkView } from "./runWorkbenchViews";

const DEFAULT_LABEL = "View Plan/Pass Details";
const MAX_DISPLAY_LABEL_LENGTH = 120;

function truncateForDisplay(value: string): string {
  if (value.length <= MAX_DISPLAY_LABEL_LENGTH) {
    return value;
  }

  return `${value.slice(0, MAX_DISPLAY_LABEL_LENGTH)}\u2026`;
}

/**
 * Resolves the Plan_Pass_Link view for the Run_Workbench from an optional
 * `RelayRunPlanContext`.
 *
 * - No context / no `planId` -> `present: false`, fixed default label.
 * - `planId` only -> link to `/plans/$planId`.
 * - `planId` + `passId` -> link to `/plans/$planId/passes/$passId` (no
 *   separate plan-only link is added).
 * - Label: built from `planTitle`/`passName` only when both are present and
 *   non-empty; the displayed label is truncated to 120 characters with a
 *   trailing ellipsis when longer, while `accessibleName` always carries the
 *   full, untruncated value. Otherwise a fixed default label is used for
 *   both `displayLabel` and `accessibleName`.
 */
export function resolvePlanPassLink(
  context?: RelayRunPlanContext | null,
): PlanPassLinkView {
  const hrefs = getRunPlanContextHrefs(context);

  if (!hrefs.planTo || !hrefs.planParams) {
    return {
      present: false,
      displayLabel: DEFAULT_LABEL,
      accessibleName: DEFAULT_LABEL,
    };
  }

  const planTitle = context?.planTitle?.trim();
  const passName = context?.passName?.trim();

  let fullLabel = DEFAULT_LABEL;
  if (planTitle && passName) {
    fullLabel = `${planTitle} / ${passName}`;
  }

  const view: PlanPassLinkView = {
    present: true,
    displayLabel: truncateForDisplay(fullLabel),
    accessibleName: fullLabel,
  };

  if (hrefs.passTo && hrefs.passParams) {
    view.to = hrefs.passTo;
    view.params = hrefs.passParams;
  } else {
    view.to = hrefs.planTo;
    view.params = hrefs.planParams;
  }

  return view;
}
