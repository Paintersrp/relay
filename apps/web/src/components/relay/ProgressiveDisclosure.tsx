import * as React from "react";
import { ChevronDown, ChevronRight } from "lucide-react";

import { cn } from "@/lib/utils";

export interface ProgressiveDisclosureProps {
  /**
   * Label rendered next to the toggle affordance. May be a plain string or
   * a render function that receives the current expanded state (useful for
   * swapping copy like "Show details" / "Hide details").
   */
  label: React.ReactNode | ((expanded: boolean) => React.ReactNode);
  /**
   * The Active_Route_Step (or any other value representing the current
   * disclosure scope) this instance is keyed to. When this value changes,
   * the affordance resets to collapsed via an internal effect, independent
   * of whether the parent also remounts this component with `key={currentStep}`.
   */
  resetKey?: string | number | null;
  /** Content rendered only while expanded. */
  children: React.ReactNode;
  /** Defaults to collapsed (`false`) on mount. */
  defaultExpanded?: boolean;
  className?: string;
  triggerClassName?: string;
  contentClassName?: string;
}

/**
 * Small reusable progressive-disclosure primitive: a toggle affordance that
 * shows/hides `children`, defaulting to collapsed. When `resetKey` changes
 * (e.g. the Active_Route_Step), the affordance resets back to collapsed.
 */
export function ProgressiveDisclosure({
  label,
  resetKey,
  children,
  defaultExpanded = false,
  className,
  triggerClassName,
  contentClassName,
}: ProgressiveDisclosureProps) {
  const [expanded, setExpanded] = React.useState(defaultExpanded);
  const previousResetKeyRef = React.useRef(resetKey);

  React.useEffect(() => {
    if (previousResetKeyRef.current !== resetKey) {
      previousResetKeyRef.current = resetKey;
      setExpanded(false);
    }
  }, [resetKey]);

  const resolvedLabel = typeof label === "function" ? label(expanded) : label;

  return (
    <div className={cn("min-w-0", className)}>
      <button
        type="button"
        onClick={() => setExpanded((value) => !value)}
        aria-expanded={expanded}
        className={cn(
          "flex items-center gap-1.5 text-[11px] font-medium text-muted-foreground hover:text-foreground",
          triggerClassName,
        )}
      >
        {expanded ? (
          <ChevronDown className="size-3.5 shrink-0" />
        ) : (
          <ChevronRight className="size-3.5 shrink-0" />
        )}
        {resolvedLabel}
      </button>
      {expanded ? (
        <div className={cn("mt-2 min-w-0", contentClassName)}>{children}</div>
      ) : null}
    </div>
  );
}
