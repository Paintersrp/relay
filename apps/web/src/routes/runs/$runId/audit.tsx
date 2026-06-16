import { createFileRoute } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'
import { runDetailQueryOptions, formatRunDate } from '@/features/relay-runs'
import { RunWorkbenchLayout } from '@/components/relay/RunWorkbenchLayout'
import { ValidationPanel } from '@/components/relay/ValidationPanel'
import { ApprovalCard } from '@/components/relay/ApprovalCard'
import { ArtifactPreviewCard } from '@/components/relay/ArtifactPreviewCard'
import { LogPreviewPanel } from '@/components/relay/LogPreviewPanel'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import { Separator } from '@/components/ui/separator'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { ShieldCheck, FileText, AlertTriangle, XSquare, CheckSquare } from 'lucide-react'

export const Route = createFileRoute('/runs/$runId/audit')({
  component: AuditPage,
})

function AuditPage() {
  const { runId } = Route.useParams()
  const { data: run, isLoading } = useQuery(runDetailQueryOptions(runId))

  if (isLoading || !run) return <Skeleton className="m-6 h-64" />

  return (
    <RunWorkbenchLayout
      run={run}
      mainContent={<AuditMainContent runId={runId} />}
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

function AuditMainContent({ runId }: { runId: string }) {
  const { data: run } = useQuery(runDetailQueryOptions(runId))
  if (!run) return null

  const auditArtifact = run.artifacts.find((a) => a.kind === 'audit')
  const resultArtifact = run.artifacts.find((a) => a.kind === 'result')
  const validationArtifact = run.artifacts.find((a) => a.kind === 'validation')
  const diffArtifact = run.artifacts.find((a) => a.kind === 'diff')

  return (
    <div className="flex flex-col gap-4">
      {/* Audit Input Summary */}
      <Section title="Audit Input Summary" icon={<FileText className="w-4 h-4" />}>
        <p className="text-xs text-muted-foreground">
          The audit packet is generated from: executor result, validation command output, and git diff evidence.
        </p>
        <div className="flex flex-col gap-1 mt-2">
          {[
            { label: 'Agent Result', artifact: resultArtifact },
            { label: 'Validation Report', artifact: validationArtifact },
            { label: 'Git Diff', artifact: diffArtifact },
          ].map(({ label, artifact }) => (
            <div key={label} className="flex items-center gap-2 text-xs">
              {artifact ? (
                <CheckSquare className="w-3.5 h-3.5 text-emerald-400" />
              ) : (
                <XSquare className="w-3.5 h-3.5 text-muted-foreground/50" />
              )}
              <span className={artifact ? 'text-foreground' : 'text-muted-foreground/50'}>{label}</span>
              {artifact && (
                <span className="font-mono text-muted-foreground ml-auto">{artifact.sizeHint}</span>
              )}
            </div>
          ))}
        </div>
        <KeyValueRow label="Run created" value={formatRunDate(run.createdAt)} />
        <KeyValueRow label="Last updated" value={formatRunDate(run.updatedAt)} />
      </Section>

      <Separator />

      {/* Audit Packet */}
      <Section title="Audit Packet" icon={<ShieldCheck className="w-4 h-4 text-yellow-400" />}>
        {auditArtifact ? (
          <>
            <div className="flex items-center gap-2">
              <Badge variant="warning" className="text-xs">Ready for Review</Badge>
              <span className="text-xs font-mono text-muted-foreground">{auditArtifact.path}</span>
            </div>
            <p className="text-xs text-muted-foreground mt-1">
              The audit packet contains validation evidence, git diff, and structured output for GPT audit review.
              Relay does not commit on your behalf.
            </p>
            <p className="text-xs text-muted-foreground/60 italic mt-1">
              Audit packet content rendering is Pass 3.
            </p>
          </>
        ) : (
          <p className="text-xs text-muted-foreground italic">Audit packet not yet generated.</p>
        )}
      </Section>

      <Separator />

      {/* Audit Decision */}
      <Section title="Audit Decision">
        <div className="flex items-center gap-2 flex-wrap">
          <Button
            variant="outline"
            size="sm"
            disabled
            className="gap-1.5 opacity-50 cursor-not-allowed"
            title="Audit approval is not implemented in Pass 1"
          >
            <CheckSquare className="w-3.5 h-3.5 text-emerald-400" />
            Approve Audit
          </Button>
          <Button
            variant="outline"
            size="sm"
            disabled
            className="gap-1.5 opacity-50 cursor-not-allowed text-destructive/60"
            title="Audit rejection is not implemented in Pass 1"
          >
            <XSquare className="w-3.5 h-3.5" />
            Request Revision
          </Button>
        </div>
        <p className="text-xs text-muted-foreground/60 italic mt-1">
          Audit decision actions are read-only in Pass 1. Real gate wiring is Pass 4.
        </p>
      </Section>

      <Separator />

      {/* Warnings / Revision Requirements */}
      {run.validationSummary.warnings > 0 && (
        <>
          <Section title="Warnings / Revision Requirements" icon={<AlertTriangle className="w-4 h-4 text-yellow-400" />}>
            <p className="text-xs text-muted-foreground">
              {run.validationSummary.warnings} warning{run.validationSummary.warnings !== 1 ? 's' : ''} found.
              Review before approving the audit.
            </p>
            <p className="text-xs text-muted-foreground/60 italic mt-1">
              Detailed warning list requires real validation data — Pass 3.
            </p>
          </Section>
          <Separator />
        </>
      )}

      {/* Close Run */}
      <Section title="Close Run">
        <p className="text-xs text-muted-foreground">
          Closing a run marks it as completed and commits the suggested commit message.
          Relay does not run <code className="font-mono">git commit</code> on your behalf.
        </p>
        <Button
          variant="outline"
          size="sm"
          disabled
          className="mt-2 opacity-50 cursor-not-allowed w-fit"
          title="Close run is not implemented in Pass 1"
        >
          Close Run — Pass 4
        </Button>
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

function KeyValueRow({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex items-baseline gap-2 text-xs">
      <span className="text-muted-foreground w-28 shrink-0">{label}</span>
      <span className="text-foreground">{value}</span>
    </div>
  )
}
