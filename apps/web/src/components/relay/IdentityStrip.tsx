// ============================================================
// Run Status Tracker Redesign — IdentityStrip (Requirement 1)
// ============================================================
//
// Compact region: run identity (title/id, repo, branch) plus the
// canonical 3-step position overview (Specification → Execute → Audit).
// The position overview uses the durable stage from the Run for
// completed/navigable classification and uses the selected route stage
// for the `aria-current="location"` indicator.

import { useNavigate } from '@tanstack/react-router'
import type { WorkflowRunStage } from '@/features/relay-runs'
import {
  derivePipelineStages,
  resolveWorkflowAvailableThroughStage,
  resolveWorkflowStage,
} from '@/features/relay-navigation/pipeline'
import { resolveStatusColorToken } from '@/features/relay-navigation/statusColor'
import { cn } from '@/lib/utils'
import { GitBranch, GitFork, CheckCircle2, AlertTriangle } from 'lucide-react'

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

interface IdentityStripProps {
  run: any
  selectedStage?: WorkflowRunStage
  currentStep?: any
  className?: string
}

export function IdentityStrip({ run, selectedStage, className }: IdentityStripProps) {
  const navigate = useNavigate()
  const durableStage = resolveWorkflowStage(run.status)
  const availableThroughStage = resolveWorkflowAvailableThroughStage(
    run.status,
    durableStage,
  )
  const stages = derivePipelineStages(
    durableStage,
    selectedStage,
    run.status,
    availableThroughStage,
  )
  const attentionTokenColor = `var(${resolveStatusColorToken(run.status)})`

  return (
    <div className={cn('flex flex-col gap-3 px-4 py-3 border-b bg-muted/20', className)}>
      {/* Identity row */}
      <div className="flex flex-col gap-1 min-w-0">
        <span className="font-mono text-xs text-muted-foreground truncate">{run.runId}</span>
        <h1 className="text-base font-semibold leading-tight text-foreground truncate">
          {run.featureSlug}
        </h1>
      </div>

      {/* Meta row: repo, branch */}
      <div className="flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-muted-foreground">
        <span className="flex items-center gap-1">
          <GitFork className="w-3 h-3" aria-hidden="true" />
          {run.repoTarget}
        </span>
        {run.branch ? (
          <span className="flex items-center gap-1">
            <GitBranch className="w-3 h-3" aria-hidden="true" />
            {run.branch}
          </span>
        ) : null}
      </div>

      {/* Compact 3-step position overview */}
      <div role="group" aria-label="Pipeline position" className="flex items-center gap-1.5">
        {stages.map((stage) => {
          const isAttention = stage.status === 'attention'
          const isCurrent = stage.status === 'current' || isAttention
          const isCompleted = stage.status === 'completed'
          const isActiveRoute = stage.step === selectedStage

          return (
            <button
              key={stage.step}
              type="button"
              data-stage-status={stage.status}
              aria-current={isActiveRoute ? 'location' : isCurrent ? 'step' : undefined}
              className={cn(
                "flex flex-1 flex-col items-center gap-0.5 min-w-0 bg-transparent border-0 p-0",
                stage.navigable ? "cursor-pointer" : "cursor-default opacity-40",
              )}
              onClick={() => {
                if (!stage.navigable) return
                void navigate({ to: STAGE_ROUTES[stage.step], params: { runId: run.runId } })
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
