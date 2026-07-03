import * as React from "react";

import type {
  CurrentStatusView,
  DetailSection,
  PlanPassLinkView,
  ProgressionEntry,
  RelayRun,
  RelayRunStep,
  StepActionsView,
} from "@/features/relay-runs/runStatusTrackerViews";
import { cn } from "@/lib/utils";

import { IdentityStrip } from "./IdentityStrip";
import { CurrentStatusBlock } from "./CurrentStatusBlock";
import { NextActionArea } from "./NextActionArea";
import { ProgressionRail } from "./ProgressionRail";
import { DetailDisclosure } from "./DetailDisclosure";
import { RunWorkbenchLoadFailedState } from "./RunWorkbenchStates";

// ============================================================
// Run Status Tracker Redesign — RunStatusTrackerLayout (Requirement 6)
// ============================================================
//
// Replaces `RunWorkbenchLayout` as the per-run shell. Composes the five
// tracker regions — IdentityStrip -> CurrentStatusBlock -> NextActionArea
// -> ProgressionRail -> DetailDisclosure — top to bottom in a single main
// column. Renders no resizable panel group and no side Inspector_Panel
// (Requirement 6.1, 6.2, 6.3).
//
// When the run detail query fails or returns no run, this component
// renders the existing load-failed state (link back to the runs registry)
// instead of the five regions (Requirement 6.5). When an action
// invocation from NextActionArea fails, this component escalates
// `currentStatus.tone` to "danger" with a short failure sentence and
// returns the failing control to enabled (Requirement 6.6) — `NextActionArea`
// itself never renders a "pending/disabled while in flight" state, so
// there is nothing to un-disable beyond re-rendering with the original
// `actionsView`.

export interface RunStatusTrackerLayoutProps {
  /** The run to render. When `null`/`undefined`, the load-failed state renders instead of the five regions. */
  run: RelayRun | null | undefined;
  /** Whether the run detail query failed. Also triggers the load-failed state, even if a stale `run` value is present. */
  loadFailed?: boolean;
  currentStep: RelayRunStep;
  /** Current_Status_Block content. May be locally escalated to tone "danger" when an action invocation fails (Requirement 6.6). */
  currentStatus: CurrentStatusView;
  /** Next_Action_Area content (existing type, reused). */
  actionsView?: StepActionsView;
  /**
   * Called with a control's `id` when the Operator clicks it. May return a
   * Promise; a thrown error or rejected Promise escalates `currentStatus`'s
   * rendered tone to "danger" with a short failure sentence (Requirement 6.6).
   */
  onActionClick?: (id: string) => void | Promise<unknown>;
  /** Progression_Rail content. */
  progression: ProgressionEntry[];
  /** Number of most-recent Progression_Rail entries shown inline before expansion. */
  progressionCollapsedCount?: number;
  /** True when the underlying run events query failed to load. */
  eventsLoadFailed?: boolean;
  /** Detail_Disclosure content (lazy, closed by default). */
  detailSections: DetailSection[];
  /** Existing type, reused — rendered inside Detail_Disclosure. */
  planPassLinkView?: PlanPassLinkView;
  className?: string;
}

const ACTION_FAILURE_DETAIL = "The last action didn't go through. Try again.";

export function RunStatusTrackerLayout({
  run,
  loadFailed = false,
  currentStep,
  currentStatus,
  actionsView,
  onActionClick,
  progression,
  progressionCollapsedCount,
  eventsLoadFailed,
  detailSections,
  planPassLinkView,
  className,
}: RunStatusTrackerLayoutProps) {
  // Requirement 6.6: escalate Current_Status_Block's tone to "danger" with
  // a short failure sentence when an action invocation fails. Local to this
  // component since RunStatusTrackerLayout is presentational and does not
  // itself own the action-invocation path. Cleared as soon as the Operator
  // retries an action (see handleActionClick) and also whenever the
  // upstream `currentStatus.updatedAt` moves forward, which signals that
  // the caller has re-derived status from genuinely fresher run data
  // (e.g. a poll/refresh) rather than just re-rendering with an
  // equivalent-but-newly-allocated view object.
  const [actionFailure, setActionFailure] = React.useState(false);

  React.useEffect(() => {
    setActionFailure(false);
  }, [currentStatus.updatedAt]);

  const handleActionClick = React.useMemo(() => {
    if (!onActionClick) {
      return undefined;
    }

    return (id: string) => {
      setActionFailure(false);
      try {
        const result = onActionClick(id);
        if (result && typeof (result as Promise<unknown>).then === "function") {
          (result as Promise<unknown>).catch(() => {
            setActionFailure(true);
          });
        }
      } catch {
        setActionFailure(true);
      }
    };
  }, [onActionClick]);

  // Requirement 6.5: run detail query failed or returned no run — render
  // the existing load-failed state instead of the five regions.
  if (loadFailed || !run) {
    return (
      <RunWorkbenchLoadFailedState
        title="Run failed to load"
        description="Relay could not load this run. Return to the runs registry and reopen the workbench."
        backToRuns
      />
    );
  }

  const resolvedStatus: CurrentStatusView = actionFailure
    ? { ...currentStatus, tone: "danger", detail: ACTION_FAILURE_DETAIL }
    : currentStatus;

  return (
    <div
      className={cn("flex min-h-0 flex-1 flex-col overflow-y-auto", className)}
      data-testid="run-status-tracker-layout"
    >
      <IdentityStrip run={run} currentStep={currentStep} />

      <div className="flex min-w-0 flex-col gap-4 px-4 py-4">
        <CurrentStatusBlock view={resolvedStatus} />

        <NextActionArea actionsView={actionsView} onActionClick={handleActionClick} />

        <ProgressionRail
          runId={run.id}
          entries={progression}
          collapsedCount={progressionCollapsedCount}
          eventsLoadFailed={eventsLoadFailed}
        />

        <DetailDisclosure
          sections={detailSections}
          planPassLinkView={planPassLinkView}
          currentStep={currentStep}
        />
      </div>
    </div>
  );
}
