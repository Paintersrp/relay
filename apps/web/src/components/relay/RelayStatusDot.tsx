import type { ComponentProps } from "react";

import { cn } from "@/lib/utils";

import type { RelayVisualStatusRole } from "./relayVisualState";

interface RelayStatusDotProps extends ComponentProps<"span"> {
  role?: RelayVisualStatusRole;
  pulse?: boolean;
}

export function RelayStatusDot({
  role = "neutral",
  pulse = true,
  className,
  ...props
}: RelayStatusDotProps) {
  return (
    <span
      aria-hidden="true"
      className={cn(
        "relay-status-dot",
        `relay-status-dot-${role}`,
        !pulse && "relay-status-dot-no-pulse",
        className
      )}
      {...props}
    />
  );
}
