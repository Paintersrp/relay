import { createFileRoute, Outlet, Link } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'
import { runDetailQueryOptions } from '@/features/relay-runs'
import { Skeleton } from '@/components/ui/skeleton'
import { Button } from '@/components/ui/button'
import { ArrowLeft } from 'lucide-react'

export const Route = createFileRoute('/runs/$runId')({
  component: RunLayout,
})

function RunLayout() {
  const { runId } = Route.useParams()
  const { data: run, isLoading } = useQuery(runDetailQueryOptions(runId))

  if (isLoading) {
    return (
      <div className="flex flex-col gap-3 p-6">
        <Skeleton className="h-8 w-64" />
        <Skeleton className="h-4 w-96" />
        <Skeleton className="h-4 w-48" />
      </div>
    )
  }

  if (!run) {
    return (
      <div className="flex flex-col items-center justify-center flex-1 gap-4 p-8 text-center">
        <div className="text-4xl">⚠️</div>
        <h1 className="text-lg font-semibold">Run not found</h1>
        <p className="text-sm text-muted-foreground max-w-sm">
          No relay run with ID <code className="font-mono text-xs bg-muted px-1 py-0.5 rounded">{runId}</code> was found in the Relay backend database.
        </p>
        <Button variant="outline" size="sm" asChild>
          <Link to="/runs">
            <ArrowLeft className="w-3.5 h-3.5 mr-1.5" />
            Back to Runs
          </Link>
        </Button>
      </div>
    )
  }

  return <Outlet />
}
