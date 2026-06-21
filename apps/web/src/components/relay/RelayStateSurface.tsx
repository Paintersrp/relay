import * as React from 'react'
import {
  AlertCircle,
  AlertTriangle,
  Ban,
  CheckCircle2,
  CircleDashed,
  Info,
  Loader2,
} from 'lucide-react'

import { cn } from '@/lib/utils'

export type RelayStateTone =
  | 'info'
  | 'loading'
  | 'empty'
  | 'warning'
  | 'danger'
  | 'success'
  | 'blocked'

export type RelayStateDensity = 'default' | 'compact'

interface RelayStatePrimitiveProps {
  tone: RelayStateTone
  title: string
  description?: React.ReactNode
  metadata?: React.ReactNode
  action?: React.ReactNode
  className?: string
  children?: React.ReactNode
  icon?: React.ReactNode
  density?: RelayStateDensity
}

const TONE_STYLES: Record<
  RelayStateTone,
  {
    icon: React.ComponentType<{ className?: string }>
    accentClass: string
    borderClass: string
  }
> = {
  info: {
    icon: Info,
    accentClass: 'text-[var(--relay-accent)]',
    borderClass: 'border-l-[var(--relay-accent)]',
  },
  loading: {
    icon: Loader2,
    accentClass: 'text-[var(--relay-accent)]',
    borderClass: 'border-l-[var(--relay-accent)]',
  },
  empty: {
    icon: CircleDashed,
    accentClass: 'text-muted-foreground',
    borderClass: 'border-l-muted-foreground/40',
  },
  warning: {
    icon: AlertTriangle,
    accentClass: 'text-[var(--warning)]',
    borderClass: 'border-l-[var(--warning)]',
  },
  danger: {
    icon: AlertCircle,
    accentClass: 'text-[var(--destructive)]',
    borderClass: 'border-l-[var(--destructive)]',
  },
  success: {
    icon: CheckCircle2,
    accentClass: 'text-[var(--success)]',
    borderClass: 'border-l-[var(--success)]',
  },
  blocked: {
    icon: Ban,
    accentClass: 'text-orange-400',
    borderClass: 'border-l-orange-500/60',
  },
}

function RelayStateCopy({
  tone,
  title,
  description,
  metadata,
  action,
  children,
  icon,
  density = 'default',
}: Omit<RelayStatePrimitiveProps, 'className'>) {
  const toneStyle = TONE_STYLES[tone]
  const Icon = toneStyle.icon
  const compact = density === 'compact'

  return (
    <>
      <div className="flex items-start gap-3">
        <div
          className={cn(
            'flex shrink-0 items-center justify-center rounded border border-[var(--relay-row-border)] bg-[var(--relay-content-bg)]',
            compact ? 'h-6 w-6' : 'h-8 w-8',
            toneStyle.accentClass,
          )}
        >
          {icon ?? (
            <Icon
              className={cn(
                compact ? 'h-3.5 w-3.5' : 'h-4 w-4',
                tone === 'loading' && 'animate-spin',
              )}
            />
          )}
        </div>

        <div className="min-w-0 flex-1">
          <p className="text-sm font-semibold text-foreground">{title}</p>
          {description ? (
            <div className="mt-1 text-sm text-muted-foreground">
              {description}
            </div>
          ) : null}
          {metadata ? (
            <div className="mt-2 font-mono text-[11px] text-muted-foreground">
              {metadata}
            </div>
          ) : null}
        </div>
      </div>

      {children ? <div className="mt-3">{children}</div> : null}
      {action ? <div className="mt-3 flex flex-wrap gap-2">{action}</div> : null}
    </>
  )
}

export function RelayStateSurface({
  tone,
  title,
  description,
  metadata,
  action,
  className,
  children,
  icon,
  density = 'default',
}: RelayStatePrimitiveProps) {
  return (
    <div
      className={cn(
        'rounded border border-[var(--relay-row-border)] border-l-2 bg-[var(--relay-panel-bg)]',
        density === 'compact' ? 'px-3 py-2' : 'px-4 py-3',
        TONE_STYLES[tone].borderClass,
        className,
      )}
    >
      <RelayStateCopy
        tone={tone}
        title={title}
        description={description}
        metadata={metadata}
        action={action}
        icon={icon}
        density={density}
      >
        {children}
      </RelayStateCopy>
    </div>
  )
}

export function RelayInlineState({
  tone,
  title,
  description,
  metadata,
  action,
  className,
  children,
  icon,
  density = 'default',
}: RelayStatePrimitiveProps) {
  return (
    <div
      className={cn(
        'rounded border border-[var(--relay-row-border)] border-l-2 bg-[var(--relay-panel-hover-bg)]',
        density === 'compact' ? 'px-3 py-2' : 'px-3 py-2.5',
        TONE_STYLES[tone].borderClass,
        className,
      )}
    >
      <RelayStateCopy
        tone={tone}
        title={title}
        description={description}
        metadata={metadata}
        action={action}
        icon={icon}
        density={density}
      >
        {children}
      </RelayStateCopy>
    </div>
  )
}

export function RelayStateBanner({
  tone,
  title,
  description,
  metadata,
  action,
  className,
  children,
  icon,
  density = 'default',
}: RelayStatePrimitiveProps) {
  return (
    <div
      className={cn(
        'rounded border border-[var(--relay-row-border)] border-l-2 bg-[var(--relay-panel-hover-bg)]',
        density === 'compact' ? 'px-3 py-2' : 'px-3 py-2.5',
        TONE_STYLES[tone].borderClass,
        className,
      )}
      role="status"
    >
      <RelayStateCopy
        tone={tone}
        title={title}
        description={description}
        metadata={metadata}
        action={action}
        icon={icon}
        density={density}
      >
        {children}
      </RelayStateCopy>
    </div>
  )
}
