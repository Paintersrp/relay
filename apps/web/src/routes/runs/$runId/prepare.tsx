import { createFileRoute, Link } from '@tanstack/react-router'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import {
  runDetailQueryOptions,
  runArtifactsQueryOptions,
  runEventsQueryOptions,
  prepareRun,
  renderBrief,
  approveBrief,
  RelayApiError,
} from '@/features/relay-runs'
import { RunWorkbenchLayout } from '@/components/relay/RunWorkbenchLayout'
import { ValidationPanel } from '@/components/relay/ValidationPanel'
import { LogPreviewPanel } from '@/components/relay/LogPreviewPanel'
import { ArtifactPreviewCard } from '@/components/relay/ArtifactPreviewCard'
import { ArtifactInspectorDialog } from '@/components/relay/ArtifactInspectorDialog'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import { Badge } from '@/components/ui/badge'
import { Separator } from '@/components/ui/separator'
import { Button } from '@/components/ui/button'
import { Textarea } from '@/components/ui/textarea'
import { Label } from '@/components/ui/label'
import {
  CheckCircle2,
  AlertTriangle,
  AlertCircle,
  Clock,
  ArrowLeft,
  ArrowRight,
  ShieldCheck,
  FileText,
  Play,
  RefreshCw,
  Wrench,
  Loader2,
} from 'lucide-react'

export const Route = createFileRoute('/runs/$runId/prepare')({
  component: PreparePage,
})

function PreparePage() {
  const { runId } = Route.useParams()
  const { data: run, isLoading: isLoadingRun, error: errorRun } = useQuery(runDetailQueryOptions(runId))
  const { data: artifacts, isLoading: isLoadingArtifacts } = useQuery(runArtifactsQueryOptions(runId))
  const { data: events, isLoading: isLoadingEvents } = useQuery(runEventsQueryOptions(runId))

  if (isLoadingRun || isLoadingArtifacts || isLoadingEvents) {
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
        <PrepareMainContent
          run={run}
          artifacts={artifacts || []}
        />
      }
      sideContent={
        <>
          <ValidationPanel summary={run.validationSummary} />
          {artifacts && artifacts.map((a) => (
            <ArtifactPreviewCard key={a.id} artifact={a} runId={run.id} />
          ))}
          <LogPreviewPanel logPreview={logPreview} />
        </>
      }
    />
  )
}

function PrepareMainContent({
  run,
  artifacts,
}: {
  run: any
  artifacts: any[]
}) {
  const queryClient = useQueryClient()
  const [approvalNotes, setApprovalNotes] = useState('')
  const [mutationError, setMutationError] = useState<string | null>(null)
  const [showValidationInspector, setShowValidationInspector] = useState(false)

  // Find relevant artifacts
  const canonicalPacketArt = artifacts.find((a) => a.filename === 'canonical_packet.json')
  const packetValidationArt = artifacts.find((a) => a.filename === 'packet_validation_report.json')
  const executorBriefArt = artifacts.find((a) => a.filename === 'executor_brief.md')
  const briefValidationArt = artifacts.find((a) => a.filename === 'brief_validation_report.json')

  // Determine action availability based on backend lifecycle
  const status = run.status as string
  const isApprovedForPrepare = status === 'approved_for_prepare'
  const isPacketValidationFailed = status === 'packet_validation_failed'
  const isPacketValidated = status === 'packet_validated' || status === 'repair_validated'
  const isBriefReadyForReview = status === 'brief_ready_for_review'
  const isApprovedForExecutor = status === 'approved_for_executor'

  const canCompile = isApprovedForPrepare
  const canRetryCompile = isPacketValidationFailed
  const canRenderBrief = isPacketValidated
  const canApproveBrief = isBriefReadyForReview

  // Check if previous compile has been attempted (canonical packet exists)
  const compileAttempted = !!canonicalPacketArt

  // Mutation: Compile (prepareRun)
  const compileMutation = useMutation({
    mutationFn: () => prepareRun(run.id),
    onSuccess: () => {
      setMutationError(null)
      void queryClient.invalidateQueries({ queryKey: ['relay-runs'] })
    },
    onError: (err: any) => {
      // Refresh state even on expected API error response (422, 409)
      void queryClient.invalidateQueries({ queryKey: ['relay-runs'] })

      if (err instanceof RelayApiError) {
        if (err.status === 422) {
          setMutationError('Compile failed packet validation. Review Packet Validation Report below.')
        } else if (err.status === 409) {
          const currentStatus = err.errorShape?.currentStatus || run.status
          setMutationError(`Compile cannot run from status "${currentStatus}". Return to the required step or refresh the run.`)
        } else {
          setMutationError(err.message || 'Compile failed.')
        }
      } else {
        setMutationError(err.message || 'Compile failed.')
      }
    },
  })

  // Mutation: Render Brief
  const renderBriefMutation = useMutation({
    mutationFn: () => renderBrief(run.id),
    onSuccess: () => {
      setMutationError(null)
      void queryClient.invalidateQueries({ queryKey: ['relay-runs'] })
    },
    onError: (err: any) => {
      setMutationError(err.message || 'Render brief failed.')
    },
  })

  // Mutation: Approve Brief
  const approveMutation = useMutation({
    mutationFn: () =>
      approveBrief(run.id, {
        action: 'approve',
        notes: approvalNotes.trim() || undefined,
      }),
    onSuccess: () => {
      setMutationError(null)
      setApprovalNotes('')
      void queryClient.invalidateQueries({ queryKey: ['relay-runs'] })
    },
    onError: (err: any) => {
      setMutationError(err.message || 'Failed to approve brief.')
    },
  })

  // Parse validation reports if available
  let packetValidationReport: any = null
  if (packetValidationArt?.preview) {
    try {
      packetValidationReport = JSON.parse(packetValidationArt.preview)
    } catch { /* ignore */ }
  }

  let briefValidationReport: any = null
  if (briefValidationArt?.preview) {
    try {
      briefValidationReport = JSON.parse(briefValidationArt.preview)
    } catch { /* ignore */ }
  }

  // Determine repair eligibility from packet validation report
  const repairEligible = packetValidationReport?.repair_eligible ?? packetValidationReport?.RepairEligible ?? false

  const isPending = compileMutation.isPending || renderBriefMutation.isPending || approveMutation.isPending

  const handleCompile = () => {
    setMutationError(null)
    compileMutation.mutate()
  }

  const handleRetryCompile = () => {
    setMutationError(null)
    compileMutation.mutate()
  }

  const handleRenderBrief = () => {
    setMutationError(null)
    renderBriefMutation.mutate()
  }

  const handleApproveBrief = () => {
    setMutationError(null)
    approveMutation.mutate()
  }

  return (
    <div className="flex flex-col gap-4">
      {/* Mutation Error Banner */}
      {mutationError && (
        <div className="flex items-start gap-1.5 text-xs text-red-400 bg-red-950/20 border border-red-900/30 rounded p-2.5">
          <AlertCircle className="w-3.5 h-3.5 shrink-0 mt-0.5" />
          <span>{mutationError}</span>
        </div>
      )}

      {/* Section 1: Compiler Result */}
      <Section title="Compiler Result" icon={<CheckCircle2 className="w-4 h-4 text-emerald-400" />}>
        {(!compileAttempted && !isPacketValidationFailed) ? (
          <div className="flex flex-col gap-2">
            <div className="flex items-center gap-2 text-xs bg-muted/30 border border-dashed rounded p-3 text-muted-foreground">
              <Clock className="w-4 h-4 shrink-0" />
              <span className="italic">Compile has not been run yet.</span>
            </div>
            {canCompile && (
              <Button
                variant="default"
                size="sm"
                onClick={handleCompile}
                disabled={isPending}
                className="w-fit gap-1.5"
              >
                {compileMutation.isPending ? (
                  <Loader2 className="w-3.5 h-3.5 animate-spin" />
                ) : (
                  <Play className="w-3.5 h-3.5" />
                )}
                Run Compile
              </Button>
            )}
            {!canCompile && (
              <p className="text-xs text-muted-foreground italic">
                Compile requires status &quot;approved_for_prepare&quot;. Current status: {status}.
              </p>
            )}
          </div>
        ) : (
          <div className="flex flex-col gap-2">
            <div className="flex items-center gap-2">
              <Badge
                variant={isPacketValidationFailed ? 'destructive' : 'success'}
                className="text-xs"
              >
                {isPacketValidationFailed ? 'Compile Failed' : 'Compiled'}
              </Badge>
              <span className="text-xs text-muted-foreground">
                {canonicalPacketArt?.filename && (
                  <code className="font-mono">{canonicalPacketArt.filename}</code>
                )}
              </span>
            </div>
            {canonicalPacketArt && (
              <div className="text-xs text-muted-foreground">
                Output: <code className="font-mono">{canonicalPacketArt.path}</code>
                {canonicalPacketArt.sizeHint && (
                  <span className="ml-2 opacity-70">{canonicalPacketArt.sizeHint}</span>
                )}
              </div>
            )}
            {isPacketValidationFailed && (
              <p className="text-xs text-yellow-500/90 italic">
                Retry compile is available because this run is in packet_validation_failed.
              </p>
            )}
            {canRetryCompile && (
              <Button
                variant="outline"
                size="sm"
                onClick={handleRetryCompile}
                disabled={isPending}
                className="w-fit gap-1.5"
              >
                {compileMutation.isPending ? (
                  <Loader2 className="w-3.5 h-3.5 animate-spin" />
                ) : (
                  <RefreshCw className="w-3.5 h-3.5" />
                )}
                Retry Compile
              </Button>
            )}
          </div>
        )}
      </Section>

      <Separator />

      {/* Section 2: Packet Validation */}
      <Section title="Packet Validation" icon={<ShieldCheck className="w-4 h-4" />}>
        {packetValidationArt ? (
          <div className="flex flex-col gap-2">
            <div className="flex items-center gap-4 text-xs">
              <span className="flex items-center gap-1">
                {packetValidationReport?.valid === true ? (
                  <CheckCircle2 className="w-3.5 h-3.5 text-emerald-400" />
                ) : (
                  <AlertCircle className="w-3.5 h-3.5 text-red-400" />
                )}
                <span className={packetValidationReport?.valid === true ? 'text-emerald-400' : 'text-red-400'}>
                  {packetValidationReport?.valid === true ? 'Valid' : 'Invalid'}
                </span>
              </span>
              {packetValidationReport?.errors && (
                <span className="flex items-center gap-1">
                  <AlertTriangle className="w-3.5 h-3.5 text-red-400" />
                  <span className="text-red-400">{packetValidationReport.errors.length}</span>
                  <span className="text-muted-foreground">errors</span>
                </span>
              )}
            </div>
            <div className="flex items-center gap-2">
              <div className="text-xs text-muted-foreground truncate">
                Report: <code className="font-mono">{packetValidationArt.path}</code>
                {packetValidationArt.sizeHint && (
                  <span className="ml-2 opacity-70">{packetValidationArt.sizeHint}</span>
                )}
              </div>
              <Button
                variant="link"
                size="sm"
                className="h-auto p-0 text-xs text-purple-400 hover:text-purple-300"
                onClick={() => setShowValidationInspector(true)}
              >
                Inspect Report
              </Button>
            </div>
            {packetValidationReport?.errors && packetValidationReport.errors.length > 0 && (
              <div className="flex flex-col gap-1.5 mt-1 border border-border/40 rounded bg-muted/20 p-2 max-h-36 overflow-y-auto">
                {packetValidationReport.errors.map((err: any, idx: number) => (
                  <div key={idx} className="flex items-start gap-1.5 text-xs text-foreground/80 leading-normal">
                    <span className="text-red-400 font-bold shrink-0">[ERROR]</span>
                    <span>{err.message || JSON.stringify(err)}</span>
                  </div>
                ))}
              </div>
            )}
          </div>
        ) : isPacketValidationFailed ? (
          <div className="flex flex-col gap-2">
            <div className="flex items-center gap-2 text-xs bg-red-950/20 border border-red-900/30 rounded p-3 text-red-400">
              <AlertCircle className="w-4 h-4 shrink-0" />
              <span className="italic">Compile failed packet validation. Review Validation Report or Logs below.</span>
            </div>
          </div>
        ) : (
          <div className="flex flex-col gap-2">
            <div className="flex items-center gap-2 text-xs bg-muted/30 border border-dashed rounded p-3 text-muted-foreground">
              <Clock className="w-4 h-4 shrink-0" />
              <span className="italic">Packet validation report not available. Compile must be run first.</span>
            </div>
          </div>
        )}
        {packetValidationArt && (
          <ArtifactInspectorDialog
            runId={run.id}
            artifact={packetValidationArt}
            open={showValidationInspector}
            onOpenChange={setShowValidationInspector}
          />
        )}
      </Section>

      <Separator />

      {/* Section 3: Repair Attempts */}
      <Section title="Repair Attempts" icon={<Wrench className="w-4 h-4 text-yellow-400" />}>
        <div className="flex flex-col gap-2">
          <p className="text-xs text-muted-foreground">
            Repair attempts fix structural or formatting issues detected during packet validation.
            This is available when the validation report marks issues as repair-eligible.
          </p>
          {packetValidationArt ? (
            <div className="flex items-center gap-2 text-xs">
              <Badge
                variant={repairEligible ? 'warning' : 'secondary'}
                className="text-xs"
              >
                {repairEligible ? 'Repair Eligible' : 'Not Eligible'}
              </Badge>
              <span className="text-xs text-muted-foreground">
                {repairEligible
                  ? 'Packet validation found repair-eligible issues.'
                  : 'No repair-eligible issues detected.'}
              </span>
            </div>
          ) : (
            <div className="flex items-center gap-2 text-xs bg-muted/30 border border-dashed rounded p-3 text-muted-foreground">
              <Clock className="w-4 h-4 shrink-0" />
              <span className="italic">Not yet available. Run compile first.</span>
            </div>
          )}
          <div className="flex items-center gap-2 text-xs text-muted-foreground/60 italic">
            <AlertTriangle className="w-3 h-3" />
            <span>Repair behavior is not yet implemented in this pass.</span>
          </div>
        </div>
      </Section>

      <Separator />

      {/* Section 4: Rendered Executor Brief */}
      <Section title="Rendered Executor Brief" icon={<FileText className="w-4 h-4 text-blue-400" />}>
        <div className="flex flex-col gap-2">
          <p className="text-xs text-muted-foreground">
            The executor brief is the rendered agent prompt sent to the configured executor.
            It is generated from the compiled canonical packet.
          </p>
          {isPacketValidated && !executorBriefArt ? (
            <div className="flex flex-col gap-2">
              <div className="flex items-center gap-2 text-xs bg-muted/30 border border-dashed rounded p-3 text-muted-foreground">
                <Clock className="w-4 h-4 shrink-0" />
                <span className="italic">Executor brief not yet rendered.</span>
              </div>
              <Button
                variant="default"
                size="sm"
                onClick={handleRenderBrief}
                disabled={isPending || !canRenderBrief}
                className="w-fit gap-1.5"
              >
                {renderBriefMutation.isPending ? (
                  <Loader2 className="w-3.5 h-3.5 animate-spin" />
                ) : (
                  <FileText className="w-3.5 h-3.5" />
                )}
                Render Brief
              </Button>
            </div>
          ) : executorBriefArt ? (
            <div className="flex flex-col gap-2">
              <div className="flex items-center gap-2">
                <Badge variant="success" className="text-xs">Rendered</Badge>
                <span className="text-xs text-muted-foreground">
                  {executorBriefArt.filename}
                </span>
              </div>
              <div className="flex items-center justify-between p-2 bg-muted/30 rounded text-xs font-mono border border-border/50">
                <span className="text-muted-foreground truncate">{executorBriefArt.path}</span>
                {executorBriefArt.sizeHint && (
                  <span className="text-muted-foreground shrink-0 ml-2">{executorBriefArt.sizeHint}</span>
                )}
              </div>
              {/* Bounded safe plain text preview (CR5) */}
              {executorBriefArt.preview ? (
                <pre className="text-[11px] font-mono bg-muted/40 p-2.5 rounded border border-border/40 max-h-48 overflow-y-auto whitespace-pre-wrap text-foreground">
                  {executorBriefArt.preview}
                </pre>
              ) : (
                <div className="text-xs bg-muted/30 border border-dashed rounded p-3 text-muted-foreground">
                  <span className="italic">Brief content preview not available.</span>
                </div>
              )}
              {isPacketValidated && (
                <Button
                  variant="outline"
                  size="sm"
                  onClick={handleRenderBrief}
                  disabled={isPending}
                  className="w-fit gap-1.5"
                >
                  {renderBriefMutation.isPending ? (
                    <Loader2 className="w-3.5 h-3.5 animate-spin" />
                  ) : (
                    <RefreshCw className="w-3.5 h-3.5" />
                  )}
                  Re-render Brief
                </Button>
              )}
            </div>
          ) : (
            <div className="flex items-center gap-2 text-xs bg-muted/30 border border-dashed rounded p-3 text-muted-foreground">
              <Clock className="w-4 h-4 shrink-0" />
              <span className="italic">
                {isPacketValidationFailed
                  ? 'Cannot render: compile failed. Fix compile errors first.'
                  : 'Not yet available. Status must be packet_validated.'}
              </span>
            </div>
          )}
        </div>
      </Section>

      <Separator />

      {/* Section 5: Brief Validation */}
      <Section title="Brief Validation" icon={<ShieldCheck className="w-4 h-4 text-emerald-400" />}>
        {briefValidationArt ? (
          <div className="flex flex-col gap-2">
            <div className="flex items-center gap-2">
              <Badge
                variant={briefValidationReport?.status === 'passed' ? 'success' : 'destructive'}
                className="text-xs"
              >
                {briefValidationReport?.status === 'passed' ? 'Brief Valid' : 'Validation Failed'}
              </Badge>
              {briefValidationReport?.status === 'passed' && (
                <CheckCircle2 className="w-3.5 h-3.5 text-emerald-400" />
              )}
            </div>
            <div className="text-xs text-muted-foreground">
              Report: <code className="font-mono">{briefValidationArt.path}</code>
              {briefValidationArt.sizeHint && (
                <span className="ml-2 opacity-70">{briefValidationArt.sizeHint}</span>
              )}
            </div>
            {briefValidationReport?.issues && briefValidationReport.issues.length > 0 ? (
              <div className="flex flex-col gap-1.5 mt-1 border border-border/40 rounded bg-muted/20 p-2 max-h-36 overflow-y-auto">
                {briefValidationReport.issues.map((issue: any, idx: number) => (
                  <div key={idx} className="flex items-start gap-1.5 text-xs text-foreground/80 leading-normal">
                    <span
                      className={
                        issue.severity === 'error'
                          ? 'text-red-400 font-bold shrink-0'
                          : 'text-yellow-400 font-bold shrink-0'
                      }
                    >
                      [{issue.severity?.toUpperCase() || 'ERROR'}]
                    </span>
                    <span>{issue.message}</span>
                  </div>
                ))}
              </div>
            ) : briefValidationReport?.status === 'passed' ? (
              <p className="text-xs text-muted-foreground italic">No validation issues found.</p>
            ) : null}
          </div>
        ) : (
          <div className="flex items-center gap-2 text-xs bg-muted/30 border border-dashed rounded p-3 text-muted-foreground">
            <Clock className="w-4 h-4 shrink-0" />
            <span className="italic">
              {executorBriefArt
                ? 'Brief validation report not yet available.'
                : 'Not yet available. Render the brief first.'}
            </span>
          </div>
        )}
      </Section>

      <Separator />

      {/* Section 6: Approve for Agent */}
      <Section title="Approve for Agent" icon={<ShieldCheck className="w-4 h-4 text-primary" />}>
        <div className="flex flex-col gap-3 mt-1">
          {isApprovedForExecutor ? (
            <div className="flex flex-col gap-2 p-3 rounded bg-emerald-950/20 border border-emerald-950/40 text-xs text-foreground">
              <div className="flex items-center gap-2 text-emerald-400 font-medium">
                <CheckCircle2 className="w-4 h-4 shrink-0" />
                <span>Brief Approved Successfully!</span>
              </div>
              <p className="text-muted-foreground leading-normal">
                This run is approved for executor dispatch.
              </p>
              <Button size="sm" asChild className="w-full mt-1.5 gap-1.5 bg-emerald-600 hover:bg-emerald-700">
                <Link to="/runs/$runId/execute" params={{ runId: run.id }}>
                  Proceed to Execute
                  <ArrowRight className="w-3.5 h-3.5" />
                </Link>
              </Button>
            </div>
          ) : canApproveBrief ? (
            <div className="flex flex-col gap-2">
              <div className="flex items-center gap-2 text-xs text-muted-foreground">
                <CheckCircle2 className="w-3.5 h-3.5 text-emerald-400" />
                <span>Brief is ready for review and approval.</span>
              </div>
              <Label htmlFor="approval-notes" className="text-xs text-muted-foreground">
                Approval Notes (Optional)
              </Label>
              <Textarea
                id="approval-notes"
                value={approvalNotes}
                onChange={(e) => setApprovalNotes(e.target.value)}
                placeholder="Optional notes for the approval decision..."
                className="h-16 text-xs bg-background/50 resize-none"
                disabled={isPending}
              />
              {mutationError && (
                <div className="flex items-start gap-1.5 text-xs text-red-400 bg-red-950/20 border border-red-900/30 rounded p-2">
                  <AlertCircle className="w-3.5 h-3.5 shrink-0 mt-0.5" />
                  <span>{mutationError}</span>
                </div>
              )}
              <Button
                variant="default"
                size="sm"
                onClick={handleApproveBrief}
                disabled={isPending}
                className="w-fit gap-1.5 bg-emerald-600 hover:bg-emerald-700 text-white"
              >
                {approveMutation.isPending ? (
                  <Loader2 className="w-3.5 h-3.5 animate-spin" />
                ) : (
                  <ShieldCheck className="w-3.5 h-3.5" />
                )}
                Approve for Executor
              </Button>
            </div>
          ) : (
            <div className="flex flex-col gap-2">
              <div className="flex items-center gap-2 p-2.5 rounded bg-muted/40 border border-border/50 text-xs text-muted-foreground leading-normal">
                <Clock className="w-4 h-4 shrink-0" />
                <span>
                  Approval is not available. Current status: <strong>{run.state || status}</strong>.
                  Complete compile and render steps first.
                </span>
              </div>
              {compileAttempted && !executorBriefArt && (
                <p className="text-xs text-muted-foreground italic">
                  Render the executor brief first.
                </p>
              )}
            </div>
          )}

          {/* Navigation hint to execute when approved */}
          {isApprovedForExecutor && (
            <div className="flex items-center gap-2 text-xs text-muted-foreground mt-1">
              <ArrowRight className="w-3 h-3" />
              <Link
                to="/runs/$runId/execute"
                params={{ runId: run.id }}
                className="text-emerald-400 hover:underline"
              >
                Go to Execute step
              </Link>
            </div>
          )}
        </div>
      </Section>
    </div>
  )
}

function Section({
  title,
  icon,
  children,
}: {
  title: string
  icon?: React.ReactNode
  children: React.ReactNode
}) {
  return (
    <Card className="border-border/60 bg-card/20">
      <CardHeader className="p-3 pb-2">
        <CardTitle className="text-sm font-medium flex items-center gap-2">
          {icon}
          {title}
        </CardTitle>
      </CardHeader>
      <CardContent className="p-3 pt-0 flex flex-col gap-1.5">{children}</CardContent>
    </Card>
  )
}
