// ============================================================
// Run Status Tracker Redesign — IdentityStrip (Requirement 1)
// ============================================================
//
// Replaces `RunSummaryHeader` + the full-width `RunStepper` pairing with a
// single compact region: run identity (title/id, repo, branch, model) plus a
// compact four-step position overview. This component intentionally renders
// no `StatusBadge` and no current-status prose — that content lives solely
// in `CurrentStatusBlock` (Requirement 1.3). The position overview is a
// compact element within this component, not an independent full-width
// stepper region (Requirement 1.4).

import { useNavigate } from '@tanstack/react-router'
import type { RelayRun, RelayRunStep } from '@/features/relay-runs'
import { deriveRunIdentity } from '@/features/relay-runs/runIdentity'
import { derivePipelineStages } from '@/features/relay-navigation/pipeline'
import { resolveStatusColorToken } from '@/features/relay-navigation/statusColor'
import { cn } from '@/lib/utils'
import { GitBranch, GitFork, Bot, CheckCircle2, AlertTriangle } from 'lucide-react'

// Typed route templates keyed by pipeline stage (mirrors the prior
// `RunStepper`'s `STAGE_ROUTES` map). `derivePipelineStages` returns the same
// route templates as opaque strings; this map re-associates them with the
// TanStack Router-typed `to` union so navigation stays type-checked.
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

interface IdentityStripProps {
  run: RelayRun
  /**
   * Active_Route_Step — the step sub-route the Operator is currently
   * viewing. Used only to highlight which segment of the compact position
   * overview corresponds to the currently viewed route; it never overrides
   * or re-derives the canonical position, which comes solely from
   * `derivePipelineStages(run.status)` (Requirement 1.2).
   */
  currentStep: RelayRunStep
  className?: string
}

export function IdentityStrip({ run, currentStep, className }: IdentityStripProps) {
  const navigate = useNavigate()
  const identity = deriveRunIdentity(run)
  const stages = derivePipelineStages(run.status)
  const attentionTokenColor = `var(${resolveStatusColorToken(run.status)})`

  return (
    <div className={cn('flex flex-col gap-3 px-4 py-3 border-b bg-muted/20', className)}>
      {/* Identity row: title/id (Requirement 1.1) */}
      <div className="flex flex-col gap-1 min-w-0">
        <span className="font-mono text-xs text-muted-foreground truncate">{identity.runId}</span>
        <h1 className="text-base font-semibold leading-tight text-foreground truncate">
          {identity.primaryText}
        </h1>
      </div>

      {/* Meta row: repo, branch, model (Requirement 1.1) */}
      <div className="flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-muted-foreground">
        <span className="flex items-center gap-1">
          <GitFork className="w-3 h-3" aria-hidden="true" />
          {identity.repo}
        </span>
        {identity.showBranch ? (
          <span className="flex items-center gap-1">
            <GitBranch className="w-3 h-3" aria-hidden="true" />
            {identity.branch}
          </span>
        ) : null}
        {identity.showModel ? (
          <span className="flex items-center gap-1">
            <Bot className="w-3 h-3" aria-hidden="true" />
            {identity.model}
          </span>
        ) : null}
      </div>

      {/* Compact four-step position overview (Requirement 1.2, 1.4), and also
          the Operator's step-to-step navigation control: every stage is
          clickable and navigates to that stage's run route (restores the
          cross-step navigation the prior `RunStepper` provided), in either
          direction — forward, backward, or to any step directly. Still no
          status prose, no StatusBadge here (Requirement 1.3). */}
      <div role="group" aria-label="Pipeline position" className="flex items-center gap-1.5">
        {stages.map((stage) => {
          const isAttention = stage.status === 'attention'
          const isCurrent = stage.status === 'current' || isAttention
          const isCompleted = stage.status === 'completed'
          const isActiveRoute = stage.step === currentStep

          return (
            <button
              key={stage.step}
              type="button"
              data-stage-status={stage.status}
              aria-current={isActiveRoute ? 'location' : isCurrent ? 'step' : undefined}
              className="flex flex-1 flex-col items-center gap-0.5 min-w-0 cursor-pointer bg-transparent border-0 p-0"
              onClick={() => {
                void navigate({ to: STAGE_ROUTES[stage.step], params: { runId: run.id } })
              }}
            >
              <div
                className={cn(
                  'h-1.5 w-full rounded-full transition-colors',
                  isCompleted && 'bg-[var(--relay-status-complete)]',
                  isCurrent && !isAttention && 'bg-[var(--relay-accent,hsl(var(--primary)))]',
                  !isCompleted && !isCurrent && 'bg-muted',
                  isActiveRoute && 'ring-2 ring-offset-1 ring-[var(--relay-accent,hsl(var(--primary)))]',
                )}
                style={isAttention ? { backgroundColor: attentionTokenColor } : undefined}
              />
              <span
                className={cn(
                  'flex items-center gap-0.5 text-[10px] leading-none truncate',
                  isCurrent ? 'text-foreground font-medium' : 'text-muted-foreground',
                )}
              >
                {isCompleted ? (
                  <CheckCircle2
                    className="h-2.5 w-2.5 shrink-0 text-[var(--relay-status-complete)]"
                    aria-hidden="true"
                  />
                ) : null}
                {isAttention ? (
                  <AlertTriangle
                    className="h-2.5 w-2.5 shrink-0"
                    style={{ color: attentionTokenColor }}
                    aria-hidden="true"
                  />
                ) : null}
                {stage.label}
              </span>
            </button>
          )
        })}
      </div>
    </div>
  )
}
