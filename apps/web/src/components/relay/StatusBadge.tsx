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
      <Badge variant="outline" className={cn('font-mono text-xs', className)}>
        {status}
      </Badge>
    )
  }
  return (
    <Badge variant={config.badgeVariant as BadgeProps['variant']} className={cn('font-mono text-xs', className)}>
      {config.label}
    </Badge>
  )
}
