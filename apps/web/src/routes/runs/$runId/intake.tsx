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
import { Badge } from '@/components/ui/badge'
import { Separator } from '@/components/ui/separator'
import { CheckCircle2, AlertTriangle, Server, FolderGit2 } from 'lucide-react'

export const Route = createFileRoute('/runs/$runId/intake')({
  component: IntakePage,
})

function IntakePage() {
  const { runId } = Route.useParams()
  const { data: run, isLoading } = useQuery(runDetailQueryOptions(runId))

  if (isLoading || !run) return <Skeleton className="m-6 h-64" />

  return (
    <RunWorkbenchLayout
      run={run}
      mainContent={<IntakeMainContent runId={runId} />}
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

function IntakeMainContent({ runId }: { runId: string }) {
  const { data: run } = useQuery(runDetailQueryOptions(runId))
  if (!run) return null

  return (
    <div className="flex flex-col gap-4">
      {/* Section: Incoming Handoff */}
      <Section title="Incoming Handoff" icon={<FolderGit2 className="w-4 h-4" />}>
        <KeyValueRow label="Packet ID" value={run.packetId ?? '—'} mono />
        <KeyValueRow label="Title" value={run.title} />
        <KeyValueRow label="Status" value={run.status} mono />
        <p className="text-xs text-muted-foreground/60 mt-2 italic">
          Raw handoff content view available in Pass 3.
        </p>
      </Section>

      <Separator />

      {/* Section: Parsed Metadata */}
      <Section title="Parsed Metadata" icon={<CheckCircle2 className="w-4 h-4 text-emerald-400" />}>
        <KeyValueRow label="Repo" value={run.repo} mono />
        <KeyValueRow label="Branch" value={run.branch} mono />
        <KeyValueRow label="Worktree" value={run.worktree ?? '—'} mono />
        <KeyValueRow label="Executor" value={run.executor} />
        <KeyValueRow label="Model" value={run.model} mono />
      </Section>

      <Separator />

      {/* Section: Run Configuration */}
      <Section title="Run Configuration" icon={<Server className="w-4 h-4" />}>
        <p className="text-xs text-muted-foreground italic">
          Configuration overrides UI is Pass 4. Current values are read from mock metadata.
        </p>
        <KeyValueRow label="Active Step" value={run.activeStep} mono />
        <KeyValueRow label="Executor" value={run.executor} />
      </Section>

      <Separator />

      {/* Section: Repo Workspace Preflight */}
      <Section title="Repo Workspace Preflight">
        <div className="flex flex-col gap-1">
          {[
            { label: 'Repo reachable', pass: true },
            { label: 'Branch exists', pass: true },
            { label: 'No uncommitted changes', pass: run.status !== 'intake_needs_review' },
            { label: 'Validation commands extractable', pass: run.validationSummary.errors === 0 },
          ].map((check) => (
            <div key={check.label} className="flex items-center gap-2 text-xs">
              {check.pass ? (
                <CheckCircle2 className="w-3.5 h-3.5 text-emerald-400" />
              ) : (
                <AlertTriangle className="w-3.5 h-3.5 text-yellow-400" />
              )}
              <span className={check.pass ? 'text-foreground' : 'text-yellow-400'}>{check.label}</span>
              <Badge variant={check.pass ? 'success' : 'warning'} className="ml-auto text-xs">
                {check.pass ? 'OK' : 'Review'}
              </Badge>
            </div>
          ))}
        </div>
        <p className="text-xs text-muted-foreground/60 mt-2 italic">
          Real preflight checks run against the local repo in Pass 3.
        </p>
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

function KeyValueRow({ label, value, mono = false }: { label: string; value: string; mono?: boolean }) {
  return (
    <div className="flex items-baseline gap-2 text-xs">
      <span className="text-muted-foreground w-32 shrink-0">{label}</span>
      <span className={mono ? 'font-mono text-foreground' : 'text-foreground'}>{value}</span>
    </div>
  )
}
