import { createFileRoute } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'
import { runDetailQueryOptions } from '@/features/relay-runs'
import { RunWorkbenchLayout } from '@/components/relay/RunWorkbenchLayout'
import { ValidationPanel } from '@/components/relay/ValidationPanel'
import { ApprovalCard } from '@/components/relay/ApprovalCard'
import { ArtifactPreviewCard } from '@/components/relay/ArtifactPreviewCard'
import { LogPreviewPanel } from '@/components/relay/LogPreviewPanel'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import { Separator } from '@/components/ui/separator'
import { Badge } from '@/components/ui/badge'
import { CheckCircle2, FileText, ShieldCheck, AlertTriangle } from 'lucide-react'

export const Route = createFileRoute('/runs/$runId/prepare')({
  component: PreparePage,
})

function PreparePage() {
  const { runId } = Route.useParams()
  const { data: run, isLoading } = useQuery(runDetailQueryOptions(runId))

  if (isLoading || !run) return <Skeleton className="m-6 h-64" />

  return (
    <RunWorkbenchLayout
      run={run}
      mainContent={<PrepareMainContent runId={runId} />}
      sideContent={
        <>
          <ValidationPanel summary={run.validationSummary} />
          <ApprovalCard gate={run.approvalGate} />
          {run.artifacts.map((a) => (
            <ArtifactPreviewCard key={a.path} artifact={a} />
          ))}
          <LogPreviewPanel logPreview={run.logPreview} />
        </>
      }
    />
  )
}

function PrepareMainContent({ runId }: { runId: string }) {
  const { data: run } = useQuery(runDetailQueryOptions(runId))
  if (!run) return null

  const briefArtifact = run.artifacts.find((a) => a.kind === 'prompt')
  const validationArtifact = run.artifacts.find((a) => a.kind === 'validation')

  return (
    <div className="flex flex-col gap-4">
      {/* Compiler Result */}
      <Section title="Compiler Result" icon={<CheckCircle2 className="w-4 h-4 text-emerald-400" />}>
        <div className="flex items-center gap-2">
          <Badge variant="success" className="text-xs">Compiled</Badge>
          <span className="text-xs text-muted-foreground">
            Handoff compiled to executor brief
          </span>
        </div>
        {briefArtifact && (
          <div className="text-xs text-muted-foreground mt-1">
            Output: <code className="font-mono">{briefArtifact.path}</code>
          </div>
        )}
        <p className="text-xs text-muted-foreground/60 mt-1 italic">
          Compiled brief content viewable in Pass 3.
        </p>
      </Section>

      <Separator />

      {/* Packet Validation */}
      <Section title="Packet Validation" icon={<ShieldCheck className="w-4 h-4" />}>
        <div className="flex items-center gap-4 text-xs">
          <span className="flex items-center gap-1">
            <CheckCircle2 className="w-3.5 h-3.5 text-emerald-400" />
            <span className="text-emerald-400">{run.validationSummary.passed}</span>
            <span className="text-muted-foreground">checks passed</span>
          </span>
          {run.validationSummary.warnings > 0 && (
            <span className="flex items-center gap-1">
              <AlertTriangle className="w-3.5 h-3.5 text-yellow-400" />
              <span className="text-yellow-400">{run.validationSummary.warnings}</span>
              <span className="text-muted-foreground">warnings</span>
            </span>
          )}
        </div>
        {validationArtifact && (
          <div className="text-xs text-muted-foreground mt-1">
            Report: <code className="font-mono">{validationArtifact.path}</code>
          </div>
        )}
      </Section>

      <Separator />

      {/* Rendered Executor Brief */}
      <Section title="Rendered Executor Brief" icon={<FileText className="w-4 h-4 text-blue-400" />}>
        <p className="text-xs text-muted-foreground">
          The executor brief is the transformed agent prompt sent to the configured executor.
          Sensitive handoff content not appropriate for the agent is excluded.
        </p>
        {briefArtifact && (
          <div className="flex items-center justify-between mt-2 p-2 bg-muted/30 rounded text-xs font-mono border border-border/50">
            <span className="text-muted-foreground truncate">{briefArtifact.path}</span>
            <span className="text-muted-foreground shrink-0 ml-2">{briefArtifact.sizeHint}</span>
          </div>
        )}
        <p className="text-xs text-muted-foreground/60 mt-1 italic">
          Brief content rendering is Pass 3.
        </p>
      </Section>

      <Separator />

      {/* Brief Validation */}
      <Section title="Brief Validation">
        <p className="text-xs text-muted-foreground">
          Validates the rendered brief against the handoff contract — scope, validation commands,
          non-goal constraints, and final output format.
        </p>
        <div className="flex items-center gap-2 mt-1">
          <Badge variant={run.validationSummary.errors === 0 ? 'success' : 'destructive'} className="text-xs">
            {run.validationSummary.errors === 0 ? 'Brief Valid' : `${run.validationSummary.errors} errors`}
          </Badge>
        </div>
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
