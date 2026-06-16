import { Badge, type BadgeProps } from '@/components/ui/badge'
import type { RelayRunStatus } from '@/features/relay-runs'
import { cn } from '@/lib/utils'

const STATUS_CONFIG: Record<RelayRunStatus, { label: string; variant: BadgeProps['variant'] }> = {
  intake_needs_review: { label: 'Intake Review', variant: 'warning' },
  brief_ready_for_review: { label: 'Brief Review', variant: 'info' },
  executor_running: { label: 'Running', variant: 'running' },
  audit_ready_for_review: { label: 'Audit Review', variant: 'warning' },
  completed: { label: 'Completed', variant: 'success' },
  blocked: { label: 'Blocked', variant: 'destructive' },
}

interface StatusBadgeProps {
  status: RelayRunStatus
  className?: string
}

export function StatusBadge({ status, className }: StatusBadgeProps) {
  const config = STATUS_CONFIG[status]
  return (
    <Badge variant={config.variant} className={cn('font-mono text-xs', className)}>
      {config.label}
    </Badge>
  )
}
