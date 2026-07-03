import type {
  ActionControlView,
  StepActionsView,
} from "@/features/relay-runs/runWorkbenchViews";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";

// ============================================================
// Run Workbench Refinement — Next_Safe_Action bar (Requirement 4)
// ============================================================
//
// Thin presentational component: renders an already-derived
// `StepActionsView` (see `deriveExecuteActions` / `deriveAuditActions`).
// This component does not derive action data itself, and it does not know
// how to invoke the underlying action request — it only calls
// `onActionClick` with the control's `id` when the Operator clicks it. The
// route/component-level id -> handler map that wires that callback to the
// existing `api.ts` request functions is introduced separately in task 5.6.

export interface RunStepActionBarProps {
  /** Ordered action controls plus the designated Next_Safe_Action, for the Active_Route_Step. */
  view: StepActionsView;
  /** Called with a control's `id` when the Operator clicks it. Wiring the id to a concrete handler is a route/component-level concern (task 5.6), not this component's. */
  onActionClick?: (id: string) => void;
  className?: string;
}

/**
 * Renders `view.controls` split into two visually distinct areas:
 *
 * - The single control with `isPrimary === true` (if any) renders in a
 *   dedicated primary area with prominent (filled) button styling.
 * - Every other control — regardless of its own `enabled` state — renders
 *   in a secondary/disclosed action area with de-emphasized (outline)
 *   button styling. Nothing is ever fully hidden or removed.
 *
 * When no control is primary (all flags false, or actions unavailable),
 * all controls render disabled in the secondary area and no primary area
 * is shown.
 */
export function RunStepActionBar({
  view,
  onActionClick,
  className,
}: RunStepActionBarProps) {
  const primary = view.controls.find((control) => control.isPrimary);
  const secondaryControls = view.controls.filter(
    (control) => control.id !== primary?.id,
  );

  return (
    <div
      className={cn("flex min-w-0 flex-col gap-3", className)}
      data-slot="run-step-action-bar"
    >
      {primary ? (
        <div
          className="flex min-w-0 flex-col gap-1.5"
          data-slot="run-step-action-bar-primary"
        >
          <Button
            type="button"
            variant="default"
            disabled={!primary.enabled}
            onClick={() => onActionClick?.(primary.id)}
            className="self-start"
          >
            {primary.label}
          </Button>
          <RunStepActionUnavailableReason control={primary} />
        </div>
      ) : null}

      {secondaryControls.length > 0 ? (
        <div
          className="flex min-w-0 flex-wrap items-start gap-2 rounded border border-[var(--relay-row-border)] bg-[var(--surface-inset)]/40 p-2"
          data-slot="run-step-action-bar-secondary"
        >
          {secondaryControls.map((control) => (
            <RunStepActionBarSecondaryControl
              key={control.id}
              control={control}
              onActionClick={onActionClick}
            />
          ))}
        </div>
      ) : null}
    </div>
  );
}

interface RunStepActionBarSecondaryControlProps {
  control: ActionControlView;
  onActionClick?: (id: string) => void;
}

function RunStepActionBarSecondaryControl({
  control,
  onActionClick,
}: RunStepActionBarSecondaryControlProps) {
  return (
    <div className="flex min-w-0 flex-col gap-1.5">
      <Button
        type="button"
        variant="outline"
        size="sm"
        disabled={!control.enabled}
        onClick={() => onActionClick?.(control.id)}
      >
        {control.label}
      </Button>
      <RunStepActionUnavailableReason control={control} />
    </div>
  );
}

interface RunStepActionUnavailableReasonProps {
  control: ActionControlView;
}

function RunStepActionUnavailableReason({
  control,
}: RunStepActionUnavailableReasonProps) {
  if (control.enabled || !control.unavailableReason) {
    return null;
  }

  return (
    <p className="max-w-xs text-xs text-muted-foreground">
      {control.unavailableReason}
    </p>
  );
}
