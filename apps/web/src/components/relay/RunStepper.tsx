import { useRouter } from '@tanstack/react-router'
import { cn } from '@/lib/utils'
import type { WorkflowRunStage } from '@/features/relay-runs'
import {
  derivePipelineStages,
  resolveWorkflowStage,
} from '@/features/relay-navigation/pipeline'
import { resolveStatusColorToken } from '@/features/relay-navigation/statusColor'
import { AlertTriangle, Loader2 } from 'lucide-react'

// Typed route templates keyed by canonical pipeline stage.
const STAGE_ROUTES: Record<
  WorkflowRunStage,
  | '/runs/$runId/specification'
  | '/runs/$runId/execute'
  | '/runs/$runId/audit'
> = {
  specification: '/runs/$runId/specification',
  execute: '/runs/$runId/execute',
  audit: '/runs/$runId/audit',
}

interface RunStepperProps {
  runId: string
  /**
   * Canonical Run `status`. The durable stage is derived SOLELY from this
   * field via `resolveWorkflowStage` (brief requirement: durable-stage
   * navigation gating). The stepper never gates on legacy derived display
   * fields (`activeStep`, `lifecycleState`, `state`, `statusSeverity`).
   */
  status: any
  /**
   * The currently selected route stage. Controls `aria-current` and panel
   * highlighting. Reviewing an earlier stage does not reduce navigability
   * of the durable stage.
   */
  selectedStage?: WorkflowRunStage
  isRunning?: boolean
  className?: string
}

export function RunStepper({
  runId,
  status,
  selectedStage,
  isRunning = false,
  className,
}: RunStepperProps) {
  const router = useRouter()
  const durableStage = resolveWorkflowStage(status)
  const stages = derivePipelineStages(durableStage, selectedStage, status)
  const attentionTokenColor = `var(${resolveStatusColorToken(status as string)})`

  return (
    <nav
      aria-label="Run steps"
      className={cn('flex h-10 items-stretch gap-0 overflow-x-auto', className)}
    >
      {stages.map((stage) => {
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
              !stage.navigable && 'cursor-default opacity-40',
            )}
            style={
              isAttention
                ? { borderBottomColor: attentionTokenColor }
                : undefined
            }
            aria-current={isCurrent ? 'step' : undefined}
            data-stage-status={stage.status}
            onClick={() => {
              // Non-navigable stages (beyond the durable stage) are a no-op.
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
