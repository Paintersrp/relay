import { createFileRoute } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'
import { runDetailQueryOptions } from '@/features/relay-runs'
import { RunWorkbenchLayout } from '@/components/relay/RunWorkbenchLayout'
import { LogPreviewPanel } from '@/components/relay/LogPreviewPanel'
import { ValidationPanel } from '@/components/relay/ValidationPanel'
import { ArtifactPreviewCard } from '@/components/relay/ArtifactPreviewCard'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import { Separator } from '@/components/ui/separator'
import { Badge } from '@/components/ui/badge'
import { Loader2, CheckCircle2, FileCode, Terminal } from 'lucide-react'

export const Route = createFileRoute('/runs/$runId/execute')({
  component: ExecutePage,
})

function ExecutePage() {
  const { runId } = Route.useParams()
  const { data: run, isLoading } = useQuery(runDetailQueryOptions(runId))

  if (isLoading || !run) return <Skeleton className="m-6 h-64" />

  return (
    <RunWorkbenchLayout
      run={run}
      mainContent={<ExecuteMainContent runId={runId} />}
      sideContent={
        <>
          <LogPreviewPanel logPreview={run.logPreview} />
          <ValidationPanel summary={run.validationSummary} />
          {run.artifacts.map((a) => (
            <ArtifactPreviewCard key={a.path} artifact={a} />
          ))}
        </>
      }
    />
  )
}

function ExecuteMainContent({ runId }: { runId: string }) {
  const { data: run } = useQuery(runDetailQueryOptions(runId))
  if (!run) return null

  const isRunning = run.status === 'executor_running'
  const resultArtifact = run.artifacts.find((a) => a.kind === 'result')
  const diffArtifact = run.artifacts.find((a) => a.kind === 'diff')

  return (
    <div className="flex flex-col gap-4">
      {/* Agent Status */}
      <Section
        title="Agent Status"
        icon={isRunning ? <Loader2 className="w-4 h-4 text-violet-400 animate-spin" /> : <CheckCircle2 className="w-4 h-4 text-emerald-400" />}
      >
        <div className="flex items-center gap-2">
          <Badge variant={isRunning ? 'running' : 'success'} className="text-xs">
            {isRunning ? 'Executor Running' : 'Execution Complete'}
          </Badge>
          <span className="text-xs text-muted-foreground">
            {run.executor} / {run.model}
          </span>
        </div>
        {isRunning && (
          <p className="text-xs text-muted-foreground/70 italic mt-1">
            Live execution monitoring is not available in Pass 1. Real SSE log streaming is Pass 3.
          </p>
        )}
      </Section>

      <Separator />

      {/* Log Preview */}
      <Section title="Log Preview" icon={<Terminal className="w-4 h-4" />}>
        <div className="font-mono text-xs bg-black/30 border border-border/50 rounded p-3 space-y-0.5 max-h-40 overflow-y-auto">
          {run.logPreview.lines.map((line, i) => (
            <div key={i} className="text-emerald-300/80 leading-relaxed">{line}</div>
          ))}
          {run.logPreview.truncated && (
            <div className="text-muted-foreground/50 italic">… truncated</div>
          )}
        </div>
      </Section>

      <Separator />

      {/* Validation Commands */}
      <Section title="Validation Commands">
        <p className="text-xs text-muted-foreground">
          Validation commands are extracted from the handoff and run locally after executor
          completion. Results are captured to artifacts.
        </p>
        <div className="flex flex-col gap-1 mt-1">
          {[
            { cmd: 'go fmt ./...', status: isRunning ? 'pending' : 'pass' },
            { cmd: 'go vet ./...', status: isRunning ? 'pending' : 'pass' },
            { cmd: 'go test ./...', status: isRunning ? 'running' : 'pass' },
            { cmd: 'npm run build', status: isRunning ? 'pending' : 'pass' },
          ].map(({ cmd, status }) => (
            <div key={cmd} className="flex items-center gap-2 text-xs font-mono p-1.5 bg-muted/20 rounded border border-border/40">
              <code className="flex-1 text-muted-foreground">{cmd}</code>
              <Badge
                variant={status === 'pass' ? 'success' : status === 'running' ? 'running' : 'secondary'}
                className="text-xs shrink-0"
              >
                {status === 'pass' ? 'PASS' : status === 'running' ? 'Running…' : 'Pending'}
              </Badge>
            </div>
          ))}
        </div>
        <p className="text-xs text-muted-foreground/60 mt-1 italic">
          Real validation command execution is Pass 3.
        </p>
      </Section>

      <Separator />

      {/* Changed Files */}
      <Section title="Changed Files" icon={<FileCode className="w-4 h-4" />}>
        {diffArtifact ? (
          <>
            <div className="flex items-center justify-between p-2 bg-muted/20 rounded border border-border/40 text-xs font-mono">
              <span className="text-muted-foreground">{diffArtifact.path}</span>
              <span className="text-muted-foreground">{diffArtifact.sizeHint}</span>
            </div>
            <p className="text-xs text-muted-foreground/60 italic mt-1">
              Git diff viewing and changed file listing require real backend — Pass 3.
            </p>
          </>
        ) : (
          <p className="text-xs text-muted-foreground italic">
            {isRunning ? 'Execution in progress — diff not yet available.' : 'No diff artifact found for this run.'}
          </p>
        )}
      </Section>

      <Separator />

      {/* Executor Result */}
      <Section title="Executor Result" icon={<CheckCircle2 className="w-4 h-4 text-muted-foreground" />}>
        {resultArtifact ? (
          <>
            <div className="flex items-center gap-2">
              <Badge variant="success" className="text-xs">Result Captured</Badge>
              <span className="text-xs font-mono text-muted-foreground">{resultArtifact.path}</span>
            </div>
            <p className="text-xs text-muted-foreground/60 italic mt-1">
              Result content preview is Pass 3.
            </p>
          </>
        ) : (
          <p className="text-xs text-muted-foreground italic">
            {isRunning ? 'Execution in progress — result pending.' : 'No result artifact found.'}
          </p>
        )}
      </Section>
    </div>
  )
}

function Section({ title, icon, children }: { title: string; icon?: React.ReactNode; children: React.ReactNode }) {
  return (
    <Card className="border-border/60 bg-card/20">
      <CardHeader className="p-3 pb-2">
        <CardTitle className="text-sm font-medium flex items-center gap-2">
          {icon}
          {title}
        </CardTitle>
      </CardHeader>
      <CardContent className="p-3 pt-0 flex flex-col gap-1.5">
        {children}
      </CardContent>
    </Card>
  )
}
