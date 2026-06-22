import type { RelayRunStatus } from '@/features/relay-runs'
import { cn } from '@/lib/utils'
import { getRelayStatusConfig } from './relayVisualState'

interface StatusBadgeProps {
  status: RelayRunStatus | string
  className?: string
}

export function StatusBadge({ status, className }: StatusBadgeProps) {
  const config = getRelayStatusConfig(status)
  
  let badgeCls = ""
  if (status === 'intake_needs_review' || status === 'needs_cleanup') {
    badgeCls = "border-warning/35 bg-warning/14 text-warning"
  } else if (config.role === 'validation' || config.role === 'blocked') {
    badgeCls = "border-destructive/30 bg-destructive/10 text-destructive"
  } else if (config.role === 'audit') {
    badgeCls = "border-info/30 bg-info/12 text-info"
  } else if (config.role === 'running') {
    badgeCls = "border-running/30 bg-running/12 text-running"
  } else if (status === 'complete' || status === 'completed') {
    badgeCls = "border-success/20 bg-success/8 text-success/70"
  } else if (config.role === 'complete') {
    badgeCls = "border-success/30 bg-success/12 text-success"
  } else {
    badgeCls = "border-border bg-muted/40 text-muted-foreground"
  }

  return (
    <span
      className={cn(
        "inline-flex items-center px-2 py-0.5 rounded-sm text-[10px] font-medium tracking-wide whitespace-nowrap border",
        badgeCls,
        className
      )}
      data-testid={`status-pill-${status}`}
    >
      {config.label}
    </span>
  )
}
