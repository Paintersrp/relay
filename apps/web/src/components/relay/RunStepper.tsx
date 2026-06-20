import { useRouter } from '@tanstack/react-router'
import { cn } from '@/lib/utils'
import type { RelayRunStep } from '@/features/relay-runs'
import { Loader2 } from 'lucide-react'

const STEPS: {
  key: RelayRunStep
  label: string
  to:
    | '/runs/$runId/intake'
    | '/runs/$runId/prepare'
    | '/runs/$runId/execute'
    | '/runs/$runId/audit'
}[] = [
  { key: 'intake', label: 'Intake', to: '/runs/$runId/intake' },
  { key: 'prepare', label: 'Compile / Render', to: '/runs/$runId/prepare' },
  { key: 'execute', label: 'Execute', to: '/runs/$runId/execute' },
  { key: 'audit', label: 'Audit', to: '/runs/$runId/audit' },
]

const STEP_ORDER: RelayRunStep[] = ['intake', 'prepare', 'execute', 'audit']

function getStepState(
  step: RelayRunStep,
  activeStep: RelayRunStep,
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

export function RunStepper({
  runId,
  activeStep,
  isRunning = false,
  className,
}: RunStepperProps) {
  const router = useRouter()

  return (
    <nav
      aria-label="Run steps"
      className={cn('flex h-10 items-stretch gap-0 overflow-x-auto', className)}
    >
      {STEPS.map((step) => {
        const state = getStepState(step.key, activeStep)
        const isActive = state === 'active'
        const isCompleted = state === 'completed'

        return (
          <button
            key={step.key}
            type="button"
            className={cn(
              'flex h-10 items-center gap-2 border-b-2 px-4 font-mono text-xs transition-colors whitespace-nowrap',
              isActive &&
                'border-[var(--relay-accent,hsl(var(--primary)))] text-foreground',
              isCompleted &&
                'border-transparent text-emerald-400 hover:text-foreground',
              !isActive &&
                !isCompleted &&
                'border-transparent text-muted-foreground hover:text-foreground',
            )}
            aria-current={isActive ? 'step' : undefined}
            onClick={() => {
              void router.navigate({ to: step.to, params: { runId } })
            }}
          >
            {isActive && isRunning ? (
              <Loader2 className="h-3.5 w-3.5 animate-spin" />
            ) : null}
            {step.label}
          </button>
        )
      })}
    </nav>
  )
}
