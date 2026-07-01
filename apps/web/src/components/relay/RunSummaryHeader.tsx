import { Link } from '@tanstack/react-router'
import type { RelayRun } from '@/features/relay-runs'
import { formatRunDateRelative, formatRunDate } from '@/features/relay-runs'
import { StatusBadge } from './StatusBadge'
import { RunStepper } from './RunStepper'
import { Separator } from '@/components/ui/separator'
import { GitBranch, GitFork, Bot, Calendar } from 'lucide-react'

interface RunSummaryHeaderProps {
  run: RelayRun
}

export function RunSummaryHeader({ run }: RunSummaryHeaderProps) {
  const isRunning = run.status === 'executor_dispatched' || run.status === 'executor_running'

  return (
    <div className="flex flex-col gap-3 px-4 py-3 border-b bg-muted/20">
      {/* Title row */}
      <div className="flex items-start justify-between gap-4">
        <div className="flex flex-col gap-1 min-w-0">
          <div className="flex items-center gap-2">
            <Link
              to="/runs"
              className="text-muted-foreground hover:text-foreground text-xs transition-colors"
              aria-label="Back to runs list"
            >
              ← Runs
            </Link>
            <span className="text-muted-foreground text-xs">/</span>
            <span className="font-mono text-xs text-muted-foreground">{run.id}</span>
          </div>
          <h1 className="text-base font-semibold leading-tight text-foreground truncate">
            {run.title}
          </h1>
          {run.packetId && (
            <p className="font-mono text-xs text-muted-foreground truncate">{run.packetId}</p>
          )}
        </div>
        <StatusBadge status={run.status} className="shrink-0 mt-1" />
      </div>

      {/* Meta row */}
      <div className="flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-muted-foreground">
        <span className="flex items-center gap-1">
          <GitFork className="w-3 h-3" />
          {run.repo}
        </span>
        <span className="flex items-center gap-1">
          <GitBranch className="w-3 h-3" />
          {run.branch}
          {run.worktree && <span className="text-muted-foreground/60">({run.worktree})</span>}
        </span>
        <span className="flex items-center gap-1">
          <Bot className="w-3 h-3" />
          {run.executor} / {run.model}
        </span>
        <span className="flex items-center gap-1" title={formatRunDate(run.updatedAt)}>
          <Calendar className="w-3 h-3" />
          {formatRunDateRelative(run.updatedAt)}
        </span>
      </div>

      <Separator className="my-0.5" />

      {/* Stepper */}
      <RunStepper
        runId={run.id}
        status={run.status}
        isRunning={isRunning}
      />
    </div>
  )
}
