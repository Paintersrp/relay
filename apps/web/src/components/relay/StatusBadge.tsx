import type { WorkflowRunStatus } from "@/features/relay-runs";
import { cn } from "@/lib/utils";

interface StatusBadgeProps {
  status: WorkflowRunStatus | string;
  className?: string;
}

const STATUS_LABELS: Record<string, string> = {
  created: "Created",
  setup_ready: "Setup ready",
  executing: "Executing",
  execution_failed: "Execution failed",
  cancelled: "Cancelled",
  validating: "Validating",
  validation_failed: "Validation failed",
  audit_ready: "Audit ready",
  needs_revision: "Needs revision",
  completed: "Completed",
};

function toneClassName(status: WorkflowRunStatus | string): string {
  switch (status) {
    case "execution_failed":
    case "validation_failed":
    case "cancelled":
      return "border-destructive/30 bg-destructive/10 text-destructive";
    case "audit_ready":
    case "needs_revision":
      return "border-warning/35 bg-warning/14 text-warning";
    case "executing":
    case "validating":
      return "border-running/30 bg-running/12 text-running";
    case "completed":
      return "border-success/30 bg-success/12 text-success";
    default:
      return "border-border bg-muted/40 text-muted-foreground";
  }
}

export function StatusBadge({ status, className }: StatusBadgeProps) {
  const label = STATUS_LABELS[status] ?? status;
  return (
    <span
      className={cn(
        "inline-flex items-center px-2 py-0.5 rounded-sm text-[11px] font-medium tracking-wide whitespace-nowrap border",
        toneClassName(status),
        className,
      )}
      data-testid={`status-pill-${status}`}
    >
      {label}
    </span>
  );
}
