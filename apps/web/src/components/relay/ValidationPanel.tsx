import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import type { RelayRun } from '@/features/relay-runs'
import { CheckCircle2, XCircle, AlertTriangle } from 'lucide-react'
import { cn } from '@/lib/utils'

interface ValidationPanelProps {
  summary: RelayRun['validationSummary']
  className?: string
}

export function ValidationPanel({ summary, className }: ValidationPanelProps) {
  const hasIssues = summary.errors > 0 || summary.warnings > 0

  return (
    <Card className={cn('border-border/60', className)}>
      <CardHeader className="p-3 pb-2">
        <CardTitle className="text-sm font-medium flex items-center gap-2">
          Validation Results
          {summary.errors > 0 ? (
            <Badge variant="destructive" className="text-xs">{summary.errors} error{summary.errors !== 1 ? 's' : ''}</Badge>
          ) : (
            <Badge variant="success" className="text-xs">Clean</Badge>
          )}
        </CardTitle>
      </CardHeader>
      <CardContent className="p-3 pt-0">
        <div className="flex items-center gap-4">
          <div className="flex items-center gap-1.5">
            <XCircle className={cn('w-4 h-4', summary.errors > 0 ? 'text-red-400' : 'text-muted-foreground/40')} />
            <span className={cn('text-sm font-medium tabular-nums', summary.errors > 0 ? 'text-red-400' : 'text-muted-foreground')}>
              {summary.errors}
            </span>
            <span className="text-xs text-muted-foreground">errors</span>
          </div>
          <div className="flex items-center gap-1.5">
            <AlertTriangle className={cn('w-4 h-4', summary.warnings > 0 ? 'text-yellow-400' : 'text-muted-foreground/40')} />
            <span className={cn('text-sm font-medium tabular-nums', summary.warnings > 0 ? 'text-yellow-400' : 'text-muted-foreground')}>
              {summary.warnings}
            </span>
            <span className="text-xs text-muted-foreground">warnings</span>
          </div>
          <div className="flex items-center gap-1.5">
            <CheckCircle2 className={cn('w-4 h-4', summary.passed > 0 ? 'text-emerald-400' : 'text-muted-foreground/40')} />
            <span className={cn('text-sm font-medium tabular-nums', summary.passed > 0 ? 'text-emerald-400' : 'text-muted-foreground')}>
              {summary.passed}
            </span>
            <span className="text-xs text-muted-foreground">passed</span>
          </div>
        </div>
        {hasIssues && (
          <p className="mt-2 text-xs text-muted-foreground/70 italic">
            Detailed issue list requires real validation data — available in Pass 3.
          </p>
        )}
        {summary.errors === 0 && summary.warnings === 0 && summary.passed === 0 && (
          <p className="mt-2 text-xs text-muted-foreground/70 italic">
            Validation not yet run for this step.
          </p>
        )}
      </CardContent>
    </Card>
  )
}
