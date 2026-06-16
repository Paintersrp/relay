import * as React from 'react'
import type { RelayRun } from '@/features/relay-runs'
import { RunSummaryHeader } from './RunSummaryHeader'
import { cn } from '@/lib/utils'

interface RunWorkbenchLayoutProps {
  run: RelayRun
  /** Left/main content area (step-specific sections) */
  mainContent: React.ReactNode
  /** Right sidebar (validation, approval, artifacts, logs) */
  sideContent?: React.ReactNode
  className?: string
}

export function RunWorkbenchLayout({
  run,
  mainContent,
  sideContent,
  className,
}: RunWorkbenchLayoutProps) {
  return (
    <div className={cn('flex flex-col flex-1 overflow-hidden', className)}>
      <RunSummaryHeader run={run} />

      <div className="flex flex-1 overflow-hidden">
        {/* Main content — step-specific sections */}
        <div className="flex-1 overflow-y-auto p-4">
          {mainContent}
        </div>

        {/* Side panel — shared panels */}
        {sideContent && (
          <div className="w-80 border-l border-border/60 overflow-y-auto p-4 flex flex-col gap-3 shrink-0">
            {sideContent}
          </div>
        )}
      </div>
    </div>
  )
}
