import type { ComponentProps } from "react";

import { Badge } from "@/components/ui/badge";
import { cn } from "@/lib/utils";
import { AlertTriangle } from "lucide-react";

import {
  getRelayAttentionLabel,
  type RelayAttentionReason,
} from "./relayVisualState";

interface RelayAttentionBadgeProps extends ComponentProps<"span"> {
  reason: RelayAttentionReason;
  count?: number;
  compact?: boolean;
}

function getAttentionVariant(reason: RelayAttentionReason) {
  switch (reason) {
    case "executor-blocked":
    case "validation-failed":
      return "destructive" as const;
    case "audit-required":
    case "intake-review":
      return "warning" as const;
    default:
      return "outline" as const;
  }
}

function getCompactLabel(reason: RelayAttentionReason) {
  switch (reason) {
    case "executor-blocked":
      return "Blocked";
    case "validation-failed":
      return "Validation";
    case "audit-required":
      return "Audit";
    case "intake-review":
      return "Review";
    default:
      return "";
  }
}

export function RelayAttentionBadge({
  reason,
  count,
  compact = false,
  className,
  ...props
}: RelayAttentionBadgeProps) {
  if (reason === "none") {
    return null;
  }

  const label = compact ? getCompactLabel(reason) : getRelayAttentionLabel(reason);

  return (
    <Badge
      variant={getAttentionVariant(reason)}
      className={cn("gap-1 text-[11px] font-medium", className)}
      {...props}
    >
      <AlertTriangle className="size-3" />
      {typeof count === "number" ? <span>{count}</span> : null}
      <span>{label}</span>
    </Badge>
  );
}
