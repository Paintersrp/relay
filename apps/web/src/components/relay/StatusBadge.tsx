import { Badge, type BadgeProps } from '@/components/ui/badge'
import type { RelayRunStatus } from '@/features/relay-runs'
import { cn } from '@/lib/utils'
import { getRelayStatusConfig } from './relayVisualState'

interface StatusBadgeProps {
  status: RelayRunStatus
  className?: string
}

export function StatusBadge({ status, className }: StatusBadgeProps) {
  const config = getRelayStatusConfig(status)
  if (config.badgeVariant === 'outline') {
    return (
      <Badge variant="outline" className={cn('text-xs font-medium', className)}>
        {status}
      </Badge>
    )
  }
  return (
    <Badge variant={config.badgeVariant as BadgeProps['variant']} className={cn('text-xs font-medium', className)}>
      {config.label}
    </Badge>
  )
}
