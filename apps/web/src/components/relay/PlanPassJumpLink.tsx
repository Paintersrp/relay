import * as React from "react";
import { Link } from "@tanstack/react-router";
import { ExternalLink } from "lucide-react";

import type { PlanPassLinkView } from "@/features/relay-runs/runWorkbenchViews";
import { Button } from "@/components/ui/button";

/**
 * Renders the Plan_Pass_Link (Requirement 6) from an already-resolved
 * `PlanPassLinkView`. This component is presentation-only — it does not
 * call `resolvePlanPassLink` itself; the consuming route/component is
 * responsible for resolving the view from `RelayRunPlanContext`.
 *
 * Renders nothing when `view.present` is false. Otherwise renders a single
 * TanStack Router `Link` to the resolved plan/pass route, showing
 * `view.displayLabel` as the visible (possibly truncated) text while using
 * `view.accessibleName` (always the full, untruncated value) as the link's
 * accessible name via `aria-label` and `title`.
 */
export function PlanPassJumpLink({
  view,
  className,
}: {
  view: PlanPassLinkView;
  className?: string;
}): React.JSX.Element | null {
  if (!view.present || !view.to || !view.params) {
    return null;
  }

  return (
    <Button
      variant="ghost"
      size="sm"
      asChild
      className={className ? `rounded-sm ${className}` : "rounded-sm"}
    >
      <Link
        to={view.to}
        params={view.params}
        aria-label={view.accessibleName}
        title={view.accessibleName}
        className="max-w-56 truncate font-medium text-[var(--relay-accent)]"
      >
        <span className="truncate">{view.displayLabel}</span>
        <ExternalLink className="size-3 shrink-0" />
      </Link>
    </Button>
  );
}
