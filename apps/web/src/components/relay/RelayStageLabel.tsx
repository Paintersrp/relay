import type { ComponentProps } from "react";

import type { RelayRunStep } from "@/features/relay-runs";
import { cn } from "@/lib/utils";

import { getRelayStageLabel } from "./relayVisualState";

interface RelayStageLabelProps extends ComponentProps<"span"> {
  step: RelayRunStep | string;
  state?: "active" | "complete" | "pending" | "blocked";
}

const STAGE_STATE_CLASS = {
  active: "text-primary",
  complete: "text-success",
  pending: "text-muted-foreground",
  blocked: "text-destructive",
} as const;

export function RelayStageLabel({
  step,
  state = "pending",
  className,
  ...props
}: RelayStageLabelProps) {
  return (
    <span
      className={cn(
        "inline-flex items-center text-xs font-medium uppercase tracking-[0.06em]",
        STAGE_STATE_CLASS[state],
        className
      )}
      {...props}
    >
      {getRelayStageLabel(step)}
    </span>
  );
}
