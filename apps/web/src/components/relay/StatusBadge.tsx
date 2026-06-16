import { Badge, type BadgeProps } from '@/components/ui/badge'
import type { RelayRunStatus } from '@/features/relay-runs'
import { cn } from '@/lib/utils'

const STATUS_CONFIG: Partial<Record<RelayRunStatus, { label: string; variant: BadgeProps['variant'] }>> = {
  draft: { label: 'Draft', variant: 'secondary' },
  needs_cleanup: { label: 'Needs Cleanup', variant: 'warning' },
  intake_received: { label: 'Intake Received', variant: 'info' },
  intake_needs_review: { label: 'Intake Review', variant: 'warning' },
  validated: { label: 'Validated', variant: 'info' },
  approved_for_prepare: { label: 'Approved to Prepare', variant: 'success' },
  packet_validated: { label: 'Packet Validated', variant: 'info' },
  packet_validation_failed: { label: 'Validation Failed', variant: 'destructive' },
  repair_validated: { label: 'Repair Validated', variant: 'info' },
  brief_ready_for_review: { label: 'Brief Review', variant: 'info' },
  approved_for_executor: { label: 'Approved for Executor', variant: 'success' },
  executor_dispatched: { label: 'Dispatching', variant: 'running' },
  executor_running: { label: 'Running', variant: 'running' },
  executor_done: { label: 'Executor Done', variant: 'success' },
  executor_blocked: { label: 'Executor Blocked', variant: 'destructive' },
  agent_done: { label: 'Agent Done', variant: 'success' },
  agent_blocked: { label: 'Agent Blocked', variant: 'destructive' },
  agent_result_needs_review: { label: 'Result Needs Review', variant: 'warning' },
  audit_ready: { label: 'Audit Ready', variant: 'warning' },
  audit_ready_for_review: { label: 'Audit Review', variant: 'warning' },
  accepted: { label: 'Accepted', variant: 'success' },
  accepted_with_warnings: { label: 'Accepted with Warnings', variant: 'warning' },
  validation_passed: { label: 'Validation Passed', variant: 'success' },
  validation_failed_accepted: { label: 'Failed (Accepted)', variant: 'warning' },
  validation_failed: { label: 'Validation Failed', variant: 'destructive' },
  completed: { label: 'Completed', variant: 'success' },
  blocked: { label: 'Blocked', variant: 'destructive' },
}

interface StatusBadgeProps {
  status: RelayRunStatus
  className?: string
}

export function StatusBadge({ status, className }: StatusBadgeProps) {
  const config = STATUS_CONFIG[status]
  if (!config) {
    return (
      <Badge variant="outline" className={cn('font-mono text-xs', className)}>
        {status}
      </Badge>
    )
  }
  return (
    <Badge variant={config.variant} className={cn('font-mono text-xs', className)}>
      {config.label}
    </Badge>
  )
}
