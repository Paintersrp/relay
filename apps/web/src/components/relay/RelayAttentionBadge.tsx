import type { ComponentProps } from "react";
import { AlertTriangle } from "lucide-react";
import { cn } from "@/lib/utils";
import type { RelayAttentionReason } from "./relayVisualState";

interface RelayAttentionBadgeProps extends ComponentProps<"span"> {
  reason: RelayAttentionReason;
  count?: number;
  compact?: boolean;
}

function getAttentionConfig(reason: RelayAttentionReason) {
  switch (reason) {
    case "executor-blocked":
      return {
        label: "Blocked",
        cls: "border-destructive/30 bg-destructive/10 text-destructive",
      };
    case "validation-failed":
      return {
        label: "Validation",
        cls: "border-warning/35 bg-warning/14 text-warning",
      };
    case "audit-required":
      return {
        label: "Audit",
        cls: "border-info/30 bg-info/12 text-info",
      };
    case "intake-review":
      return {
        label: "Review",
        cls: "border-warning/35 bg-warning/14 text-warning",
      };
    default:
      return {
        label: "",
        cls: "",
      };
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

  const cfg = getAttentionConfig(reason);
  if (!cfg.cls) return null;

  let displayLabel = cfg.label;
  if (reason === "validation-failed") {
    if (typeof count === "number") {
      displayLabel = count > 1 ? `${count} Validation` : `1 Validation`;
    } else {
      displayLabel = "Validation";
    }
  }

  return (
    <span
      className={cn(
        "inline-flex items-center gap-1 px-2 py-0.5 rounded-sm text-[10px] font-medium whitespace-nowrap border",
        cfg.cls,
        className
      )}
      {...props}
    >
      <AlertTriangle className="size-[9px]" />
      <span>{displayLabel}</span>
    </span>
  );
}
