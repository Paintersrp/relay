import { useRouter } from '@tanstack/react-router'
import { cn } from '@/lib/utils'
import type { RelayRunStatus, RelayRunStep } from '@/features/relay-runs'
import { derivePipelineStages } from '@/features/relay-navigation/pipeline'
import { resolveStatusColorToken } from '@/features/relay-navigation/statusColor'
import { AlertTriangle, Loader2 } from 'lucide-react'

// Typed route templates keyed by pipeline stage. `derivePipelineStages` returns
// the same route templates as opaque strings; this map re-associates them with
// the TanStack Router-typed `to` union so navigation stays type-checked.
const STAGE_ROUTES: Record<
  RelayRunStep,
  | '/runs/$runId/intake'
  | '/runs/$runId/prepare'
  | '/runs/$runId/execute'
  | '/runs/$runId/audit'
> = {
  intake: '/runs/$runId/intake',
  prepare: '/runs/$runId/prepare',
  execute: '/runs/$runId/execute',
  audit: '/runs/$runId/audit',
}

interface RunStepperProps {
  runId: string
  /**
   * Canonical Run `status`. Stage derivation is driven SOLELY by this field via
   * `derivePipelineStages` (Requirements 6.3, 6.7); the stepper never gates on
   * `activeStep`, `lifecycleState`, `state`, or `statusSeverity`.
   */
  status: RelayRunStatus
  isRunning?: boolean
  className?: string
}

export function RunStepper({
  runId,
  status,
  isRunning = false,
  className,
}: RunStepperProps) {
  const router = useRouter()
  const stages = derivePipelineStages(status)
  const attentionTokenColor = `var(${resolveStatusColorToken(status)})`

  return (
    <nav
      aria-label="Run steps"
      className={cn('flex h-10 items-stretch gap-0 overflow-x-auto', className)}
    >
      {stages.map((stage) => {
        // The affected (current-position) stage is reported as "attention" when
        // the canonical status is in the closed blocked / awaiting-review set;
        // it is still the current position, so treat both as "current" visually
        // and add a distinct attention indicator on top (Req 6.2, 6.4).
        const isAttention = stage.status === 'attention'
        const isCurrent = stage.status === 'current' || isAttention
        const isCompleted = stage.status === 'completed'

        return (
          <button
            key={stage.step}
            type="button"
            className={cn(
              'flex h-10 items-center gap-2 border-b-2 px-4 text-xs font-medium transition-colors whitespace-nowrap',
              isCurrent &&
                !isAttention &&
                'border-[var(--relay-accent,hsl(var(--primary)))] text-foreground',
              isCurrent && isAttention && 'text-foreground',
              isCompleted &&
                'border-transparent text-[var(--relay-status-complete)] hover:text-foreground',
              !isCurrent &&
                !isCompleted &&
                'border-transparent text-muted-foreground hover:text-foreground',
              !stage.navigable && 'cursor-default',
            )}
            style={
              isAttention
                ? { borderBottomColor: attentionTokenColor }
                : undefined
            }
            aria-current={isCurrent ? 'step' : undefined}
            data-stage-status={stage.status}
            onClick={() => {
              // Non-navigable stages are a no-op and keep the current route
              // (Req 6.6). Navigable stages route to their run-scoped stage
              // route (Req 6.5).
              if (!stage.navigable) return
              void router.navigate({
                to: STAGE_ROUTES[stage.step],
                params: { runId },
              })
            }}
          >
            {isCurrent && isRunning && !isAttention ? (
              <Loader2 className="h-3.5 w-3.5 animate-spin" />
            ) : null}
            {isAttention ? (
              <AlertTriangle
                className="h-3.5 w-3.5 shrink-0"
                style={{ color: attentionTokenColor }}
                aria-hidden="true"
              />
            ) : null}
            {stage.label}
          </button>
        )
      })}
    </nav>
  )
}
