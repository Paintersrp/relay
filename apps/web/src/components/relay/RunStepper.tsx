import { useRouter } from '@tanstack/react-router'
import { cn } from '@/lib/utils'
import type { RelayRunStep } from '@/features/relay-runs'
import { CheckCircle2, Circle, Loader2, Clock } from 'lucide-react'

const STEPS: { key: RelayRunStep; label: string; to: '/runs/$runId/intake' | '/runs/$runId/prepare' | '/runs/$runId/execute' | '/runs/$runId/audit' }[] = [
  { key: 'intake', label: 'Intake / Configure', to: '/runs/$runId/intake' },
  { key: 'prepare', label: 'Compile / Render', to: '/runs/$runId/prepare' },
  { key: 'execute', label: 'Execute', to: '/runs/$runId/execute' },
  { key: 'audit', label: 'Audit / Close', to: '/runs/$runId/audit' },
]

const STEP_ORDER: RelayRunStep[] = ['intake', 'prepare', 'execute', 'audit']

function getStepState(
  step: RelayRunStep,
  activeStep: RelayRunStep
): 'completed' | 'active' | 'pending' {
  const stepIdx = STEP_ORDER.indexOf(step)
  const activeIdx = STEP_ORDER.indexOf(activeStep)
  if (stepIdx < activeIdx) return 'completed'
  if (stepIdx === activeIdx) return 'active'
  return 'pending'
}

interface RunStepperProps {
  runId: string
  activeStep: RelayRunStep
  isRunning?: boolean
  className?: string
}

export function RunStepper({ runId, activeStep, isRunning = false, className }: RunStepperProps) {
  const router = useRouter()

  return (
    <nav aria-label="Run steps" className={cn('flex items-center gap-0', className)}>
      {STEPS.map((step, i) => {
        const state = getStepState(step.key, activeStep)
        const isActive = state === 'active'
        const isCompleted = state === 'completed'

        return (
          <div key={step.key} className="flex items-center">
            <button
              type="button"
              className={cn(
                'flex items-center gap-1.5 px-3 py-1.5 rounded text-xs font-medium transition-colors',
                isActive && 'bg-primary/10 text-primary border border-primary/30',
                isCompleted && 'text-emerald-400 hover:text-emerald-300',
                !isActive && !isCompleted && 'text-muted-foreground hover:text-foreground'
              )}
              aria-current={isActive ? 'step' : undefined}
              onClick={() => {
                void router.navigate({ to: step.to, params: { runId } })
              }}
            >
              {isCompleted ? (
                <CheckCircle2 className="w-3.5 h-3.5 text-emerald-400" />
              ) : isActive && isRunning ? (
                <Loader2 className="w-3.5 h-3.5 animate-spin text-primary" />
              ) : isActive ? (
                <Circle className="w-3.5 h-3.5 text-primary fill-primary/20" />
              ) : (
                <Clock className="w-3.5 h-3.5" />
              )}
              {step.label}
            </button>
            {i < STEPS.length - 1 && (
              <span className="text-border mx-1 text-xs select-none">›</span>
            )}
          </div>
        )
      })}
    </nav>
  )
}
