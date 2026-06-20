import type { ComponentProps } from "react";

import type { RelayRunStatus } from "@/features/relay-runs";
import { cn } from "@/lib/utils";

import { RelayStatusDot } from "./RelayStatusDot";
import { getRelayStatusConfig } from "./relayVisualState";

interface RelayStatusIndicatorProps extends ComponentProps<"span"> {
  status: RelayRunStatus | string;
  showDot?: boolean;
  showLabel?: boolean;
}

const ROLE_TEXT_CLASS = {
  running: "text-running",
  blocked: "text-destructive",
  complete: "text-success",
  audit: "text-warning",
  validation: "text-destructive",
  neutral: "text-muted-foreground",
} as const;

export function RelayStatusIndicator({
  status,
  showDot = true,
  showLabel = true,
  className,
  ...props
}: RelayStatusIndicatorProps) {
  const config = getRelayStatusConfig(status);

  return (
    <span
      className={cn("inline-flex items-center gap-1.5", className)}
      {...props}
    >
      {showDot ? <RelayStatusDot role={config.role} pulse={config.role === "running"} /> : null}
      {showLabel ? (
        <span className={cn("font-mono text-xs", ROLE_TEXT_CLASS[config.role])}>
          {config.label}
        </span>
      ) : null}
    </span>
  );
}
