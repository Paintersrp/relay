import { createFileRoute, Link } from '@tanstack/react-router'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { useState, useMemo } from 'react'
import {
  runDetailQueryOptions,
  runArtifactsQueryOptions,
  runEventsQueryOptions,
  runArtifactContentQueryOptions,
  auditRun,
  submitManualAuditPacket,
  approveAudit,
  requestAuditRevision,
  prepareCommitMessage,
  closeRun,
} from '@/features/relay-runs'
import type {
  RelayAuditDecisionValue,
  RelayAuditInputSummaryInfo,
  RelayAuditPacketInfo,
  RelayAuditDecisionStatus,
  RelayCommitSummary,
  RelayAuditActions,
} from '@/features/relay-runs'
import { RELAY_AUDIT_DECISION_VALUES } from '@/features/relay-runs'
import { RunWorkbenchLayout } from '@/components/relay/RunWorkbenchLayout'
import { ValidationPanel } from '@/components/relay/ValidationPanel'
import { ArtifactPreviewCard } from '@/components/relay/ArtifactPreviewCard'
import { LogPreviewPanel } from '@/components/relay/LogPreviewPanel'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import { Separator } from '@/components/ui/separator'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Textarea } from '@/components/ui/textarea'
import { ScrollArea } from '@/components/ui/scroll-area'
import {
  ShieldCheck,
  FileText,
  AlertTriangle,
  XSquare,
  CheckSquare,
  Loader2,
  AlertCircle,
  Terminal,
  CheckCircle2,
  Send,
  ArrowLeft,
  RefreshCw,
  FileCode,
  Clock,
} from 'lucide-react'

export const Route = createFileRoute('/runs/$runId/audit')({
  component: AuditPage,
})

function AuditPage() {
  const { runId } = Route.useParams()
  const queryClient = useQueryClient()
  const { data: run, isLoading: isLoadingRun, error: errorRun } = useQuery(runDetailQueryOptions(runId))
  const { data: artifacts, isLoading: isLoadingArtifacts } = useQuery(runArtifactsQueryOptions(runId))
  const { data: events } = useQuery(runEventsQueryOptions(runId))

  if (isLoadingRun || isLoadingArtifacts) {
    return (
      <div className="flex flex-col gap-3 p-6 max-w-4xl mx-auto">
        <Skeleton className="h-8 w-64" />
        <Skeleton className="h-4 w-96" />
        <Skeleton className="h-4 w-48" />
        <Skeleton className="h-64 w-full mt-6" />
      </div>
    )
  }

  if (errorRun || !run) {
    return (
      <div className="flex flex-col items-center justify-center flex-1 gap-4 p-8 text-center min-h-[50vh]">
        <div className="text-4xl">⚠️</div>
        <h1 className="text-lg font-semibold">Run not found or error loading</h1>
        <p className="text-sm text-muted-foreground max-w-sm">
          Failed to load run details from backend. Please verify the backend is running and the run ID is correct.
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

  const formattedLogs = events
    ? events.map((e) => {
        const timeStr = new Date(e.createdAt).toLocaleTimeString('en-US', {
          hour12: false,
          hour: '2-digit',
          minute: '2-digit',
          second: '2-digit',
        })
        return `[${timeStr}] ${e.message}`
      })
    : []

  const logPreview = {
    lines: formattedLogs.slice(-50),
    truncated: formattedLogs.length > 50,
  }

  return (
    <RunWorkbenchLayout
      run={{
        ...run,
        artifacts: artifacts || [],
        latestEvents: events || [],
        logPreview,
      }}
      mainContent={
        <AuditMainContent
          run={run}
          artifacts={artifacts || []}
          events={events || []}
        />
      }
      sideContent={
        <>
          <ValidationPanel summary={run.validationSummary} />
          {artifacts && artifacts.map((a) => (
            <ArtifactPreviewCard key={a.id} artifact={a} />
          ))}
          <LogPreviewPanel logPreview={logPreview} />
        </>
      }
    />
  )
}

function deriveAuditData(
  run: any,
  artifacts: any[],
  events: any[]
): {
  inputSummary: RelayAuditInputSummaryInfo
  generatedPacket: RelayAuditPacketInfo
  manualPacket?: RelayAuditPacketInfo
  decision: RelayAuditDecisionStatus
  commitSummary: RelayCommitSummary
  actions: RelayAuditActions
  warnings: string[]
  revisionRequirements: string[]
  blockers: string[]
} {
  const auditInputArts = artifacts.filter((a: any) =>
    a.kind === 'audit' && (a.filename?.includes('audit_input_summary') || a.path?.includes('audit_input_summary') || a.label === 'Audit Input Summary')
  ).sort((a: any, b: any) => new Date(b.createdAt || 0).getTime() - new Date(a.createdAt || 0).getTime())
  const inputArtifact = auditInputArts[0]

  const packetArts = artifacts.filter((a: any) =>
    a.kind === 'audit' && (a.filename?.includes('audit_packet') || a.path?.includes('audit_packet') || a.label === 'Audit Packet')
  )
  const genPacketArt = packetArts.find((a: any) =>
    !a.filename?.includes('manual') && !a.path?.includes('manual')
  )
  const manualPacketArt = packetArts.find((a: any) =>
    a.filename?.includes('manual') || a.path?.includes('manual')
  )

  const hasApprovedEvent = events.some((e: any) =>
    e.message?.includes('Audit approved') || e.message?.includes('Audit approved with warnings')
  )
  const hasRevisionEvent = events.some((e: any) =>
    e.message?.includes('Audit revision requested')
  )
  const hasCloseEvent = events.some((e: any) =>
    e.message?.includes('Run closed')
  )

  const runStatus = run.status || ''
  const lifecycleState = run.lifecycleState || ''
  const isAccepted = runStatus === 'audit_ready_for_review' && (run.state === 'Approved — Ready to Close' || run.state === 'Approved with Warnings')
  const isAuditReady = runStatus === 'audit_ready_for_review' && lifecycleState === 'audit'
  const isCompleted = runStatus === 'completed' && lifecycleState === 'completed'
  const isBlocked = runStatus === 'blocked'

  const commitMsgArts = artifacts.filter((a: any) =>
    a.kind === 'audit' && (a.filename?.includes('commit_message') || a.path?.includes('commit_message'))
  )
  const commitMsgArt = commitMsgArts[commitMsgArts.length - 1]
  const changedFileArts = artifacts.filter((a: any) => a.kind === 'diff')

  const evidenceArtifacts = artifacts.filter((a: any) =>
    a.kind === 'result' || a.kind === 'validation' || a.kind === 'diff'
  )

  const hasGenerateEvent = events.some((e: any) =>
    e.message?.includes('Audit packet generated') || e.message?.includes('audit packet generated')
  )
  const hasManualSubmitEvent = events.some((e: any) =>
    e.message?.includes('Manual audit packet submitted')
  )

  const auditDecision = run.approvalGate?.state === 'approved'
  const decisionValue: RelayAuditDecisionValue | undefined = isAccepted ? 'accepted' : undefined

  return {
    inputSummary: {
      artifactId: inputArtifact?.id || '',
      artifactPath: inputArtifact?.path || '',
      available: !!inputArtifact,
      generatedAt: inputArtifact?.createdAt,
      preview: inputArtifact?.preview,
      evidenceIncluded: evidenceArtifacts.map((a: any) => a.label || a.filename),
      missingEvidence: [],
    },
    generatedPacket: {
      artifactId: genPacketArt?.id || '',
      artifactPath: genPacketArt?.path || '',
      available: !!genPacketArt,
      isManual: false,
      generatedAt: genPacketArt?.createdAt,
      preview: genPacketArt?.preview,
      warnings: run.validationSummary?.warnings > 0
        ? (run.validationSummary?.issues || []).filter((i: any) => i.severity === 'warning').map((i: any) => i.message)
        : [],
    },
    manualPacket: manualPacketArt ? {
      artifactId: manualPacketArt.id,
      artifactPath: manualPacketArt.path,
      available: true,
      isManual: true,
      generatedAt: manualPacketArt.createdAt,
      preview: manualPacketArt.preview,
      warnings: [],
    } : undefined,
    decision: {
      currentDecision: decisionValue,
      source: isCompleted ? 'approved' : hasManualSubmitEvent ? 'manual' : hasGenerateEvent ? 'generated' : 'none',
      approvedAt: run.updatedAt,
      notes: run.approvalGate?.note,
    },
    commitSummary: {
      changedFileArtifactIds: changedFileArts.map((a: any) => a.id),
      commitMessageArtifactId: commitMsgArt?.id,
      commitMessagePreview: commitMsgArt?.preview,
      commitMessageAvailable: !!commitMsgArt,
      validationSummary: `${run.validationSummary?.passed || 0} passed, ${run.validationSummary?.errors || 0} errors, ${run.validationSummary?.warnings || 0} warnings`,
      auditDecisionSummary: decisionValue || 'Pending review',
    },
    actions: {
      canGenerateAudit: isAuditReady && (!hasGenerateEvent || !genPacketArt),
      canSubmitManual: isAuditReady && !isAccepted,
      canApproveAudit: (hasGenerateEvent || hasManualSubmitEvent) && isAuditReady && !auditDecision && !isCompleted,
      canRequestRevision: isAuditReady && !isCompleted,
      canPrepareCommitMessage: isAuditReady && (auditDecision || isAccepted) && !isCompleted,
      canCloseRun: isAccepted && !isCompleted,
      generateAuditUnavailableReason: !isAuditReady ? `Current status: ${runStatus}` : undefined,
      submitManualUnavailableReason: !isAuditReady ? `Current status: ${runStatus}` : undefined,
      approveAuditUnavailableReason: !isAuditReady ? `Current status: ${runStatus}` : undefined,
      requestRevisionUnavailableReason: !isAuditReady ? `Current status: ${runStatus}` : undefined,
      prepareCommitMessageUnavailableReason: !isAuditReady ? `Current status: ${runStatus}` : 'Audit must be approved first',
      closeRunUnavailableReason: !isAccepted ? `Audit must be approved first` : undefined,
    },
    warnings: run.validationSummary?.warnings > 0
      ? (run.validationSummary?.issues || []).filter((i: any) => i.severity === 'warning').map((i: any) => i.message)
      : [],
    revisionRequirements: hasRevisionEvent
      ? events.filter((e: any) => e.message?.includes('Audit revision requested')).map((e: any) => e.message)
      : [],
    blockers: isBlocked ? ['Run is blocked and cannot proceed to close.'] : [],
  }
}

function AuditMainContent({
  run,
  artifacts,
  events,
}: {
  run: any
  artifacts: any[]
  events: any[]
}) {
  const queryClient = useQueryClient()
  const { runId } = Route.useParams()
  const [mutationError, setMutationError] = useState<string | null>(null)
  const [showManualSubmit, setShowManualSubmit] = useState(false)
  const [manualDecision, setManualDecision] = useState<string>('')
  const [manualPacketMarkdown, setManualPacketMarkdown] = useState('')
  const [manualNotes, setManualNotes] = useState('')
  const [approveDecision, setApproveDecision] = useState<'accepted' | 'accepted_with_warnings'>('accepted')
  const [approveNotes, setApproveNotes] = useState('')
  const [showApproveForm, setShowApproveForm] = useState(false)
  const [revisionReason, setRevisionReason] = useState('')
  const [showRevisionForm, setShowRevisionForm] = useState(false)

  const auditData = useMemo(
    () => deriveAuditData(run, artifacts, events),
    [run, artifacts, events]
  )

  const invalidate = () => {
    void queryClient.invalidateQueries({ queryKey: ['relay-runs'] })
  }

  const generateMutation = useMutation({
    mutationFn: () => auditRun(runId),
    onSuccess: () => { setMutationError(null); invalidate() },
    onError: (err: any) => setMutationError(err.message || 'Failed to generate audit.'),
  })

  const submitManualMutation = useMutation({
    mutationFn: () => submitManualAuditPacket(runId, {
      audit_packet_markdown: manualPacketMarkdown,
      decision: manualDecision as RelayAuditDecisionValue,
      notes: manualNotes,
    }),
    onSuccess: () => { setMutationError(null); setShowManualSubmit(false); setManualPacketMarkdown(''); invalidate() },
    onError: (err: any) => setMutationError(err.message || 'Failed to submit manual audit packet.'),
  })

  const approveMutation = useMutation({
    mutationFn: () => approveAudit(runId, { decision: approveDecision, notes: approveNotes }),
    onSuccess: () => { setMutationError(null); setShowApproveForm(false); setApproveNotes(''); invalidate() },
    onError: (err: any) => setMutationError(err.message || 'Failed to approve audit.'),
  })

  const revisionMutation = useMutation({
    mutationFn: () => requestAuditRevision(runId, { reason: revisionReason }),
    onSuccess: () => { setMutationError(null); setShowRevisionForm(false); setRevisionReason(''); invalidate() },
    onError: (err: any) => setMutationError(err.message || 'Failed to request revision.'),
  })

  const prepareCommitMutation = useMutation({
    mutationFn: () => prepareCommitMessage(runId),
    onSuccess: () => { setMutationError(null); invalidate() },
    onError: (err: any) => setMutationError(err.message || 'Failed to prepare commit message.'),
  })

  const closeMutation = useMutation({
    mutationFn: () => closeRun(runId),
    onSuccess: () => { setMutationError(null); invalidate() },
    onError: (err: any) => setMutationError(err.message || 'Failed to close run.'),
  })

  const activeMutation = generateMutation.isPending
    || submitManualMutation.isPending
    || approveMutation.isPending
    || revisionMutation.isPending
    || prepareCommitMutation.isPending
    || closeMutation.isPending

  const handleGenerateAudit = () => { setMutationError(null); generateMutation.mutate() }
  const handleSubmitManual = () => { setMutationError(null); submitManualMutation.mutate() }
  const handleApproveAudit = () => { setMutationError(null); approveMutation.mutate() }
  const handleRequestRevision = () => { setMutationError(null); revisionMutation.mutate() }
  const handlePrepareCommitMessage = () => { setMutationError(null); prepareCommitMutation.mutate() }
  const handleCloseRun = () => { setMutationError(null); closeMutation.mutate() }

  return (
    <div className="flex flex-col gap-4">
      {mutationError && (
        <div className="flex items-start gap-1.5 text-xs text-red-400 bg-red-950/20 border border-red-900/30 rounded p-2.5">
          <AlertCircle className="w-3.5 h-3.5 shrink-0 mt-0.5" />
          <span>{mutationError}</span>
        </div>
      )}

      {/* Audit Input Summary */}
      <Section title="Audit Input Summary" icon={<FileText className="w-4 h-4" />}>
        {auditData.inputSummary.available ? (
          <div className="flex flex-col gap-1.5">
            <div className="flex items-center gap-2">
              <CheckCircle2 className="w-3.5 h-3.5 text-emerald-400" />
              <span className="text-xs font-mono text-muted-foreground">{auditData.inputSummary.artifactPath}</span>
              {auditData.inputSummary.generatedAt && (
                <span className="text-[11px] text-muted-foreground/60">{new Date(auditData.inputSummary.generatedAt).toLocaleString()}</span>
              )}
            </div>
            {auditData.inputSummary.preview && (
              <pre className="text-[11px] font-mono bg-muted/40 p-2.5 rounded border border-border/40 max-h-48 overflow-y-auto whitespace-pre-wrap text-foreground">
                {auditData.inputSummary.preview}
              </pre>
            )}
            <div className="flex flex-col gap-0.5 text-xs text-muted-foreground">
              <span className="text-[11px] font-medium text-muted-foreground/70">Evidence included:</span>
              {auditData.inputSummary.evidenceIncluded.length > 0 ? (
                auditData.inputSummary.evidenceIncluded.slice(0, 10).map((e: string, i: number) => (
                  <span key={i} className="flex items-center gap-1 text-[11px]">
                    <CheckSquare className="w-3 h-3 text-emerald-400/70" />
                    {e}
                  </span>
                ))
              ) : (
                <span className="italic text-[11px] text-muted-foreground/50">No evidence artifacts found.</span>
              )}
            </div>
            {auditData.inputSummary.missingEvidence.length > 0 && (
              <div className="flex flex-col gap-0.5 text-xs text-yellow-400/80">
                <span className="text-[11px] font-medium">Missing evidence:</span>
                {auditData.inputSummary.missingEvidence.map((w: string, i: number) => (
                  <span key={i} className="flex items-center gap-1 text-[11px]">
                    <AlertTriangle className="w-3 h-3" />
                    {w}
                  </span>
                ))}
              </div>
            )}
            {auditData.actions.canGenerateAudit && (
              <Button
                variant="outline"
                size="sm"
                onClick={handleGenerateAudit}
                disabled={activeMutation}
                className="w-fit gap-1.5 mt-1"
              >
                {generateMutation.isPending ? (
                  <Loader2 className="w-3.5 h-3.5 animate-spin" />
                ) : (
                  <RefreshCw className="w-3.5 h-3.5" />
                )}
                Regenerate Audit Summary
              </Button>
            )}
          </div>
        ) : (
          <div className="flex flex-col gap-2">
            <p className="text-xs text-muted-foreground italic">
              No audit input summary generated yet.
            </p>
            {auditData.actions.canGenerateAudit ? (
              <Button
                variant="default"
                size="sm"
                onClick={handleGenerateAudit}
                disabled={activeMutation}
                className="w-fit gap-1.5"
              >
                {generateMutation.isPending ? (
                  <Loader2 className="w-3.5 h-3.5 animate-spin" />
                ) : (
                  <ShieldCheck className="w-3.5 h-3.5" />
                )}
                Generate Audit
              </Button>
            ) : (
              <p className="text-xs text-muted-foreground/60 italic">
                {auditData.actions.generateAuditUnavailableReason || 'Audit generation is not available for this run.'}
              </p>
            )}
          </div>
        )}
      </Section>

      <Separator />

      {/* Audit Packet */}
      <Section title="Audit Packet" icon={<ShieldCheck className="w-4 h-4 text-yellow-400" />}>
        {auditData.generatedPacket.available ? (
          <div className="flex flex-col gap-1.5">
            <div className="flex items-center gap-2">
              <Badge variant="warning" className="text-xs">Generated</Badge>
              <span className="text-xs font-mono text-muted-foreground truncate">{auditData.generatedPacket.artifactPath}</span>
              {auditData.generatedPacket.generatedAt && (
                <span className="text-[11px] text-muted-foreground/60">{new Date(auditData.generatedPacket.generatedAt).toLocaleString()}</span>
              )}
            </div>
            {auditData.generatedPacket.preview && (
              <pre className="text-[11px] font-mono bg-muted/40 p-2.5 rounded border border-border/40 max-h-48 overflow-y-auto whitespace-pre-wrap text-foreground">
                {auditData.generatedPacket.preview}
              </pre>
            )}
            {auditData.generatedPacket.warnings.length > 0 && (
              <div className="flex flex-col gap-0.5 text-xs text-yellow-400/80">
                {auditData.generatedPacket.warnings.map((w: string, i: number) => (
                  <span key={i} className="flex items-center gap-1 text-[11px]">
                    <AlertTriangle className="w-3 h-3" />
                    {w}
                  </span>
                ))}
              </div>
            )}
            {auditData.generatedPacket.decision && (
              <div className="flex items-center gap-2 text-xs">
                <Badge variant="secondary" className="text-xs">Decision: {auditData.generatedPacket.decision}</Badge>
              </div>
            )}
          </div>
        ) : (
          <p className="text-xs text-muted-foreground italic">
            No generated audit packet available.
          </p>
        )}

        {auditData.manualPacket && (
          <div className="flex flex-col gap-1.5 mt-3 pt-3 border-t border-border/40">
            <div className="flex items-center gap-2">
              <Badge variant="secondary" className="text-xs">Manual Submission</Badge>
              <span className="text-xs font-mono text-muted-foreground truncate">{auditData.manualPacket.artifactPath}</span>
            </div>
            {auditData.manualPacket.preview && (
              <pre className="text-[11px] font-mono bg-muted/40 p-2.5 rounded border border-border/40 max-h-48 overflow-y-auto whitespace-pre-wrap text-foreground">
                {auditData.manualPacket.preview}
              </pre>
            )}
          </div>
        )}

        {auditData.actions.canSubmitManual && (
          <div className="mt-3">
            {!showManualSubmit ? (
              <Button
                variant="outline"
                size="sm"
                onClick={() => setShowManualSubmit(true)}
                disabled={activeMutation}
                className="w-fit gap-1.5"
              >
                <Send className="w-3.5 h-3.5" />
                Submit Manual Audit Packet
              </Button>
            ) : (
              <div className="flex flex-col gap-2 p-3 bg-muted/20 rounded border border-border/40">
                <p className="text-xs font-medium text-muted-foreground">Submit Manual Audit Packet</p>
                <Textarea
                  className="text-xs font-mono min-h-[120px]"
                  placeholder="Paste audit packet markdown content..."
                  value={manualPacketMarkdown}
                  onChange={(e) => setManualPacketMarkdown(e.target.value)}
                />
                <div className="flex flex-col gap-1">
                  <label className="text-[11px] text-muted-foreground">Decision</label>
                  <select
                    className="text-xs bg-background border border-border/60 rounded px-2 py-1.5"
                    value={manualDecision}
                    onChange={(e) => setManualDecision(e.target.value)}
                  >
                    <option value="">Select decision...</option>
                    {RELAY_AUDIT_DECISION_VALUES.map((d) => (
                      <option key={d} value={d}>{d}</option>
                    ))}
                  </select>
                </div>
                <Textarea
                  className="text-xs min-h-[60px]"
                  placeholder="Optional notes..."
                  value={manualNotes}
                  onChange={(e) => setManualNotes(e.target.value)}
                />
                <div className="flex items-center gap-2">
                  <Button
                    variant="default"
                    size="sm"
                    onClick={handleSubmitManual}
                    disabled={activeMutation || !manualDecision || !manualPacketMarkdown.trim()}
                    className="w-fit gap-1.5"
                  >
                    {submitManualMutation.isPending ? (
                      <Loader2 className="w-3.5 h-3.5 animate-spin" />
                    ) : (
                      <Send className="w-3.5 h-3.5" />
                    )}
                    Submit
                  </Button>
                  <Button
                    variant="ghost"
                    size="sm"
                    onClick={() => { setShowManualSubmit(false); setManualPacketMarkdown(''); setManualDecision(''); setManualNotes('') }}
                    disabled={activeMutation}
                  >
                    Cancel
                  </Button>
                </div>
              </div>
            )}
          </div>
        )}

        {!auditData.generatedPacket.available && !auditData.manualPacket && !auditData.actions.canSubmitManual && (
          <p className="text-xs text-muted-foreground/60 italic mt-1">
            No audit packet available and manual submission is not enabled for this run.
          </p>
        )}
      </Section>

      <Separator />

      {/* Audit Decision */}
      <Section title="Audit Decision" icon={<CheckSquare className="w-4 h-4 text-emerald-400" />}>
        <div className="flex items-center gap-2 flex-wrap">
          <Badge variant={
            auditData.decision.source === 'approved' ? 'success' :
            auditData.decision.source === 'manual' ? 'warning' :
            auditData.decision.source === 'generated' ? 'secondary' : 'outline'
          } className="text-xs">
            {auditData.decision.source === 'approved' ? 'Approved' :
             auditData.decision.source === 'manual' ? 'Manual Decision Submitted' :
             auditData.decision.source === 'generated' ? 'Generated Recommendation' : 'No Decision'}
          </Badge>
          {auditData.decision.currentDecision && (
            <span className="text-xs font-mono text-muted-foreground">{auditData.decision.currentDecision}</span>
          )}
          {auditData.decision.approvedAt && (
            <span className="text-xs text-muted-foreground/60">{new Date(auditData.decision.approvedAt).toLocaleString()}</span>
          )}
        </div>
        {auditData.decision.notes && (
          <p className="text-xs text-muted-foreground mt-1">{auditData.decision.notes}</p>
        )}

        <div className="flex items-center gap-2 flex-wrap mt-2">
          {auditData.actions.canApproveAudit && !showApproveForm && (
            <Button
              variant="default"
              size="sm"
              onClick={() => setShowApproveForm(true)}
              disabled={activeMutation}
              className="w-fit gap-1.5"
            >
              <CheckSquare className="w-3.5 h-3.5" />
              Approve Audit
            </Button>
          )}
          {auditData.actions.canRequestRevision && !showRevisionForm && (
            <Button
              variant="outline"
              size="sm"
              onClick={() => setShowRevisionForm(true)}
              disabled={activeMutation}
              className="w-fit gap-1.5 text-destructive/80"
            >
              <XSquare className="w-3.5 h-3.5" />
              Request Revision
            </Button>
          )}
        </div>

        {showApproveForm && (
          <div className="flex flex-col gap-2 p-3 bg-muted/20 rounded border border-border/40 mt-2">
            <p className="text-xs font-medium text-muted-foreground">Approve Audit</p>
            <div className="flex flex-col gap-1">
              <label className="text-[11px] text-muted-foreground">Decision</label>
              <select
                className="text-xs bg-background border border-border/60 rounded px-2 py-1.5"
                value={approveDecision}
                onChange={(e) => setApproveDecision(e.target.value as 'accepted' | 'accepted_with_warnings')}
              >
                <option value="accepted">Accepted</option>
                <option value="accepted_with_warnings">Accepted with Warnings</option>
              </select>
            </div>
            <Textarea
              className="text-xs min-h-[60px]"
              placeholder="Optional approval notes..."
              value={approveNotes}
              onChange={(e) => setApproveNotes(e.target.value)}
            />
            <div className="flex items-center gap-2">
              <Button
                variant="default"
                size="sm"
                onClick={handleApproveAudit}
                disabled={activeMutation}
                className="w-fit gap-1.5"
              >
                {approveMutation.isPending ? (
                  <Loader2 className="w-3.5 h-3.5 animate-spin" />
                ) : (
                  <CheckSquare className="w-3.5 h-3.5" />
                )}
                Confirm Approval
              </Button>
              <Button
                variant="ghost"
                size="sm"
                onClick={() => { setShowApproveForm(false); setApproveNotes('') }}
                disabled={activeMutation}
              >
                Cancel
              </Button>
            </div>
          </div>
        )}

        {showRevisionForm && (
          <div className="flex flex-col gap-2 p-3 bg-muted/20 rounded border border-border/40 mt-2">
            <p className="text-xs font-medium text-muted-foreground">Request Revision</p>
            <Textarea
              className="text-xs min-h-[60px]"
              placeholder="Describe what needs revision..."
              value={revisionReason}
              onChange={(e) => setRevisionReason(e.target.value)}
            />
            <div className="flex items-center gap-2">
              <Button
                variant="outline"
                size="sm"
                onClick={handleRequestRevision}
                disabled={activeMutation}
                className="w-fit gap-1.5 text-destructive/80"
              >
                {revisionMutation.isPending ? (
                  <Loader2 className="w-3.5 h-3.5 animate-spin" />
                ) : (
                  <XSquare className="w-3.5 h-3.5" />
                )}
                Submit Revision Request
              </Button>
              <Button
                variant="ghost"
                size="sm"
                onClick={() => { setShowRevisionForm(false); setRevisionReason('') }}
                disabled={activeMutation}
              >
                Cancel
              </Button>
            </div>
          </div>
        )}

        {!auditData.actions.canApproveAudit && !auditData.actions.canRequestRevision && auditData.decision.source === 'none' && (
          <p className="text-xs text-muted-foreground/60 italic mt-1">
            Generate an audit packet or submit a manual audit to enable decision actions.
          </p>
        )}
      </Section>

      <Separator />

      {/* Warnings / Revision Requirements */}
      {(auditData.warnings.length > 0 || auditData.revisionRequirements.length > 0 || auditData.blockers.length > 0) && (
        <>
          <Section title="Warnings / Revision Requirements" icon={<AlertTriangle className="w-4 h-4 text-yellow-400" />}>
            {auditData.blockers.length > 0 && (
              <div className="flex flex-col gap-1">
                <p className="text-xs font-medium text-red-400">Blockers</p>
                {auditData.blockers.map((b: string, i: number) => (
                  <div key={i} className="flex items-start gap-1.5 text-xs text-red-400/80">
                    <AlertCircle className="w-3 h-3 shrink-0 mt-0.5" />
                    <span>{b}</span>
                  </div>
                ))}
              </div>
            )}
            {auditData.revisionRequirements.length > 0 && (
              <div className="flex flex-col gap-1 mt-2">
                <p className="text-xs font-medium text-yellow-400">Revision Requirements</p>
                {auditData.revisionRequirements.map((r: string, i: number) => (
                  <div key={i} className="flex items-start gap-1.5 text-xs text-yellow-400/80">
                    <AlertTriangle className="w-3 h-3 shrink-0 mt-0.5" />
                    <span>{r}</span>
                  </div>
                ))}
              </div>
            )}
            {auditData.warnings.length > 0 && (
              <div className="flex flex-col gap-1 mt-2">
                <p className="text-xs font-medium text-muted-foreground">Warnings</p>
                {auditData.warnings.map((w: string, i: number) => (
                  <div key={i} className="flex items-start gap-1.5 text-xs text-yellow-400/70">
                    <AlertTriangle className="w-3 h-3 shrink-0 mt-0.5" />
                    <span>{w}</span>
                  </div>
                ))}
              </div>
            )}
          </Section>
          <Separator />
        </>
      )}

      {/* Commit Summary */}
      <Section title="Commit Summary" icon={<FileCode className="w-4 h-4" />}>
        <div className="flex flex-col gap-1.5">
          <div className="flex items-center gap-2">
            <Badge variant={auditData.commitSummary.commitMessageAvailable ? 'success' : 'secondary'} className="text-xs">
              {auditData.commitSummary.commitMessageAvailable ? 'Prepared' : 'Not prepared'}
            </Badge>
            {auditData.commitSummary.commitMessageAvailable && (
              <span className="text-xs font-mono text-muted-foreground truncate">
                commit_message.txt
              </span>
            )}
          </div>

          <div className="grid grid-cols-3 gap-2 text-[11px] text-muted-foreground">
            <div>
              <span className="font-medium">Changed files:</span> {auditData.commitSummary.changedFileArtifactIds.length}
            </div>
            <div>
              <span className="font-medium">Validation:</span> {auditData.commitSummary.validationSummary}
            </div>
            <div>
              <span className="font-medium">Audit:</span> {auditData.commitSummary.auditDecisionSummary}
            </div>
          </div>

          {auditData.commitSummary.commitMessagePreview && (
            <pre className="text-[11px] font-mono bg-muted/40 p-2.5 rounded border border-border/40 max-h-32 overflow-y-auto whitespace-pre-wrap text-foreground">
              {auditData.commitSummary.commitMessagePreview}
            </pre>
          )}

          {auditData.actions.canPrepareCommitMessage && (
            <Button
              variant="outline"
              size="sm"
              onClick={handlePrepareCommitMessage}
              disabled={activeMutation}
              className="w-fit gap-1.5 mt-1"
            >
              {prepareCommitMutation.isPending ? (
                <Loader2 className="w-3.5 h-3.5 animate-spin" />
              ) : (
                <FileText className="w-3.5 h-3.5" />
              )}
              Prepare Commit Message
            </Button>
          )}

          <p className="text-[11px] text-muted-foreground/50 italic">
            Preparing a commit message only writes a suggested artifact. No git commit, push, or staging occurs.
          </p>
        </div>
      </Section>

      <Separator />

      {/* Close Run */}
      <Section title="Close Run" icon={<Terminal className="w-4 h-4" />}>
        <div className="flex flex-col gap-1.5">
          <div className="flex items-center gap-2">
            <Badge variant={
              run.lifecycleState === 'completed' ? 'success' :
              auditData.actions.canCloseRun ? 'warning' : 'secondary'
            } className="text-xs">
              {run.lifecycleState === 'completed' ? 'Closed' :
               auditData.actions.canCloseRun ? 'Ready to Close' :
               auditData.decision.source === 'approved' ? 'Approved' : 'Pending'}
            </Badge>
            {auditData.decision.currentDecision && (
              <span className="text-xs text-muted-foreground">
                Final decision: {auditData.decision.currentDecision}
              </span>
            )}
          </div>

          {auditData.actions.canCloseRun && (
            <Button
              variant="default"
              size="sm"
              onClick={handleCloseRun}
              disabled={activeMutation}
              className="w-fit gap-1.5 mt-1"
            >
              {closeMutation.isPending ? (
                <Loader2 className="w-3.5 h-3.5 animate-spin" />
              ) : (
                <CheckCircle2 className="w-3.5 h-3.5" />
              )}
              Close Run
            </Button>
          )}

          {auditData.actions.closeRunUnavailableReason && !run.lifecycleState === 'completed' && (
            <p className="text-xs text-muted-foreground/60 italic mt-1 flex items-center gap-1">
              <AlertTriangle className="w-3 h-3" />
              {auditData.actions.closeRunUnavailableReason}
            </p>
          )}

          {run.lifecycleState === 'completed' && (
            <p className="text-xs text-emerald-400/70 mt-1">
              Run is closed. All artifacts and evidence are preserved.
            </p>
          )}

          <p className="text-[11px] text-muted-foreground/50 italic mt-1">
            Closing a run updates Relay run state only. No git commit, push, or repo mutation occurs.
          </p>
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
