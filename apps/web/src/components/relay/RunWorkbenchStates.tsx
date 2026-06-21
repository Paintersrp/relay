import { Link } from '@tanstack/react-router'

import { RelayStateSurface } from '@/components/relay/RelayStateSurface'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { ArrowLeft } from 'lucide-react'

export function RunWorkbenchLoadingState({
  label = 'Loading run',
}: {
  label?: string
}) {
  return (
    <div className="mx-auto flex w-full max-w-4xl flex-col gap-4 p-6">
      <RelayStateSurface
        tone="loading"
        title={label}
        description="Relay is loading run metadata, artifacts, and recent events."
      >
        <div className="space-y-3">
          <Skeleton className="h-4 w-56" />
          <Skeleton className="h-4 w-80 max-w-full" />
          <Skeleton className="h-4 w-40" />
        </div>
      </RelayStateSurface>

      <div className="rounded border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)] p-4">
        <div className="space-y-3">
          <Skeleton className="h-5 w-40" />
          <Skeleton className="h-4 w-full" />
          <Skeleton className="h-4 w-11/12" />
          <Skeleton className="h-32 w-full" />
        </div>
      </div>
    </div>
  )
}

export function RunWorkbenchLoadFailedState({
  title = 'Run failed to load',
  description = 'Relay could not load this run. Return to the runs registry and reopen the workbench.',
  backToRuns = false,
}: {
  title?: string
  description?: string
  backToRuns?: boolean
}) {
  return (
    <div className="mx-auto flex w-full max-w-4xl flex-col gap-4 p-6">
      <RelayStateSurface
        tone="danger"
        title={title}
        description={description}
        action={
          backToRuns ? (
            <Button variant="outline" size="sm" asChild>
              <Link to="/runs">
                <ArrowLeft className="mr-1.5 h-3.5 w-3.5" />
                Back to Runs
              </Link>
            </Button>
          ) : null
        }
      />
    </div>
  )
}
