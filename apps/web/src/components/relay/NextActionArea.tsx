import type { StepActionsView } from "@/features/relay-runs/runStatusTrackerViews";
import { RunStepActionBar } from "./RunStepActionBar";

// ============================================================
// Run Status Tracker Redesign — NextActionArea (Requirement 3)
// ============================================================
//
// Thin re-composition of the existing `RunStepActionBar`: renders the
// Active_Route_Step's action controls (zero, one, or many) using the
// existing `StepActionsView`/`ActionControlView` data, unchanged. This
// component introduces no new action-derivation or attention/findings
// logic — it only forwards `onActionClick`.
//
// Deliberately absent from this component:
//   - `RunStepAttentionPanel` or any other standing findings/blockers
//     panel (Requirement 3.5). A blocker/warning/revision-requirement is
//     folded into `CurrentStatusBlock` or into a control's
//     `unavailableReason` text upstream, not rendered here.
//   - An empty-state "no attention items" indicator when nothing is
//     wrong (Requirement 3.8). When `actionsView` has no controls, this
//     component renders nothing extra — not even an empty-state card.

export interface NextActionAreaProps {
  /** Ordered action controls plus the designated Next_Safe_Action, for the Active_Route_Step. Absent/no controls renders nothing. */
  actionsView?: StepActionsView;
  /** Called with a control's `id` when the Operator clicks it. */
  onActionClick?: (id: string) => void;
  className?: string;
}

export function NextActionArea({
  actionsView,
  onActionClick,
  className,
}: NextActionAreaProps) {
  if (!actionsView || actionsView.controls.length === 0) {
    return null;
  }

  return (
    <RunStepActionBar
      view={actionsView}
      onActionClick={onActionClick}
      className={className}
    />
  );
}
