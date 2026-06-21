import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import type { RelayRun } from '@/features/relay-runs'
import { CheckCircle2, XCircle, AlertTriangle } from 'lucide-react'
import {
  RelayInlineState,
  RelayStateBanner,
} from '@/components/relay/RelayStateSurface'
import { cn } from '@/lib/utils'

interface ValidationPanelProps {
  summary: RelayRun['validationSummary']
  className?: string
}

export function ValidationPanel({ summary, className }: ValidationPanelProps) {
  const resolvedSummary = summary ?? { errors: 0, warnings: 0, passed: 0 }
  const { errors, warnings, passed } = resolvedSummary
  const hasErrors = errors > 0
  const hasWarnings = warnings > 0
  const hasNoResults = errors === 0 && warnings === 0 && passed === 0
  const statusLabel = hasErrors
    ? `${errors} error${errors !== 1 ? 's' : ''}`
    : hasNoResults
      ? 'Pending'
      : 'Clean'

  return (
    <Card className={cn('border-border/60', className)}>
      <CardHeader className="p-3 pb-2">
        <CardTitle className="text-sm font-medium flex items-center gap-2">
          Validation Results
          {hasErrors ? (
            <Badge variant="destructive" className="text-xs">{statusLabel}</Badge>
          ) : hasNoResults ? (
            <Badge variant="secondary" className="text-xs">{statusLabel}</Badge>
          ) : (
            <Badge variant="success" className="text-xs">{statusLabel}</Badge>
          )}
        </CardTitle>
      </CardHeader>
      <CardContent className="p-3 pt-0">
        <div className="flex items-center gap-4">
          <div className="flex items-center gap-1.5">
            <XCircle className={cn('w-4 h-4', hasErrors ? 'text-[var(--destructive)]' : 'text-muted-foreground/40')} />
            <span className={cn('text-sm font-medium tabular-nums', hasErrors ? 'text-[var(--destructive)]' : 'text-muted-foreground')}>
              {errors}
            </span>
            <span className="text-xs text-muted-foreground">errors</span>
          </div>
          <div className="flex items-center gap-1.5">
            <AlertTriangle className={cn('w-4 h-4', hasWarnings ? 'text-[var(--warning)]' : 'text-muted-foreground/40')} />
            <span className={cn('text-sm font-medium tabular-nums', hasWarnings ? 'text-[var(--warning)]' : 'text-muted-foreground')}>
              {warnings}
            </span>
            <span className="text-xs text-muted-foreground">warnings</span>
          </div>
          <div className="flex items-center gap-1.5">
            <CheckCircle2 className={cn('w-4 h-4', passed > 0 ? 'text-[var(--success)]' : 'text-muted-foreground/40')} />
            <span className={cn('text-sm font-medium tabular-nums', passed > 0 ? 'text-[var(--success)]' : 'text-muted-foreground')}>
              {passed}
            </span>
            <span className="text-xs text-muted-foreground">passed</span>
          </div>
        </div>
        {hasErrors ? (
          <RelayStateBanner
            tone="danger"
            title="Validation failed"
            description={`${errors} error${errors !== 1 ? 's' : ''}${hasWarnings ? ` and ${warnings} warning${warnings !== 1 ? 's' : ''}` : ''} captured for this step.`}
            className="mt-3"
          />
        ) : null}
        {!hasErrors && hasWarnings ? (
          <RelayInlineState
            tone="warning"
            title="Validation warnings captured"
            description={`${warnings} warning${warnings !== 1 ? 's' : ''} captured for this step.`}
            className="mt-3"
          />
        ) : null}
        {hasNoResults ? (
          <RelayInlineState
            tone="empty"
            title="Validation not run"
            description="Relay has not captured validation output for this step yet."
            className="mt-3"
          />
        ) : null}
      </CardContent>
    </Card>
  )
}
