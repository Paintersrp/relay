import { createFileRoute, Link } from '@tanstack/react-router'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { useState, useMemo } from 'react'
import {
  runDetailQueryOptions,
  runArtifactsQueryOptions,
  runEventsQueryOptions,
  executeRun,
  cancelRun,
  recoverRun,
  validateRun,
} from '@/features/relay-runs'
import type { RelayExecutorPhase } from '@/features/relay-runs'
import { RunWorkbenchLayout } from '@/components/relay/RunWorkbenchLayout'
import { ValidationPanel } from '@/components/relay/ValidationPanel'
import { LogPreviewPanel } from '@/components/relay/LogPreviewPanel'
import { ArtifactPreviewCard } from '@/components/relay/ArtifactPreviewCard'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import { Separator } from '@/components/ui/separator'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { ScrollArea } from '@/components/ui/scroll-area'
import {
  Loader2,
  CheckCircle2,
  FileCode,
  Terminal,
  AlertCircle,
  AlertTriangle,
  Play,
  XCircle,
  RefreshCw,
  StopCircle,
  Clock,
  ArrowLeft,
  FileText,
} from 'lucide-react'

export const Route = createFileRoute('/runs/$runId/execute')({
  component: ExecutePage,
})

function ExecutePage() {
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
        <ExecuteMainContent
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

function deriveExecutorPhase(runStatus: string, lifecycleState: string): RelayExecutorPhase {
  if (lifecycleState === 'failed' || runStatus === 'blocked') return 'blocked'
  if (runStatus === 'executor_dispatched' || runStatus === 'executor_running') return 'running'
  if (runStatus === 'executor_done' || runStatus === 'agent_done') return 'done'
  if (runStatus === 'executor_blocked' || runStatus === 'agent_blocked') return 'failed'
  if (runStatus === 'agent_result_needs_review') return 'done'
  if (runStatus === 'approved_for_executor') return 'idle'
  if (lifecycleState === 'execute') return 'idle'
  return 'unavailable'
}

function ExecuteMainContent({
  run,
  artifacts,
}: {
  run: any
  artifacts: any[]
}) {
  const queryClient = useQueryClient()
  const [mutationError, setMutationError] = useState<string | null>(null)

  const runStatus = (run.status || '') as string
  const runLifecycle = (run.lifecycleState || '') as string
  const executorPhase = deriveExecutorPhase(runStatus, runLifecycle)

  const hasStructuredValidationEvidence = artifacts.some(
    (a: any) => a.storageKind === 'validation_run_json' || a.storageKind === 'validation_progress_json'
  )
  const localValidationIsRunning = runStatus === 'local_validation_running'
  const canRunValidation = (runStatus === 'executor_done' || runStatus === 'executor_blocked' || runStatus === 'validation_passed' || runStatus === 'validation_failed') && !localValidationIsRunning && !hasStructuredValidationEvidence

  const actionAvailability = useMemo(() => {
    const isApproved = runStatus === 'approved_for_executor'
    const isExecuting = runStatus === 'executor_dispatched' || runStatus === 'executor_running'
    const isBlocked = executorPhase === 'blocked' || executorPhase === 'failed'
    return {
      canStart: isApproved || (isBlocked && runLifecycle === 'execute'),
      canCancel: isExecuting,
      canRecover: isBlocked && runLifecycle === 'execute',
      startUnavailableReason: !isApproved && !isBlocked ? `Current status: ${runStatus}` : undefined,
      cancelUnavailableReason: 'Cancellation is not yet implemented in the backend.',
      recoverUnavailableReason: 'Recovery is not yet implemented in the backend.',
    }
  }, [runStatus, runLifecycle, executorPhase])

  // Find relevant artifacts for Step 3 display
  const resultArtifacts = artifacts.filter((a: any) => a.kind === 'result')
  const diffArtifacts = artifacts.filter((a: any) => a.kind === 'diff')
  const validationArtifacts = artifacts.filter((a: any) => a.kind === 'validation')

  // Find specific result candidates
  const executorResultArt = resultArtifacts.find((a: any) =>
    a.filename?.includes('executor_result') || a.label?.includes('Executor Result')
  )
  const agentResultRawArt = resultArtifacts.find((a: any) =>
    a.filename?.includes('agent_result_raw') || a.label?.includes('Agent Result')
  )
  const executorStdoutArt = resultArtifacts.find((a: any) =>
    a.filename?.includes('executor_stdout') || a.label?.includes('Executor Stdout')
  )

  // Choose the best result artifact for display
  const primaryResultArt = executorResultArt || agentResultRawArt || executorStdoutArt || resultArtifacts[0]

  // Find diff artifacts for changed files
  const gitDiffPatch = diffArtifacts.find((a: any) =>
    a.filename?.includes('git_diff_patch') || a.label?.includes('Git Diff Patch')
  )
  const gitDiffNameStatus = diffArtifacts.find((a: any) =>
    a.filename?.includes('git_diff_name_status') || a.label?.includes('Git Diff Name Status')
  )
  const gitStatus = diffArtifacts.find((a: any) =>
    a.filename?.includes('git_status') || a.label?.includes('Git Status')
  )
  const primaryDiffArt = gitDiffPatch || gitDiffNameStatus || gitStatus || diffArtifacts[0]

  // Find validation artifact candidates
  const validationStdoutArt = validationArtifacts.find((a: any) =>
    a.filename?.includes('validation_stdout') || a.label?.includes('Validation Output')
  )
  const validationReportArt = validationArtifacts.find((a: any) =>
    a.filename?.includes('validation_run') || a.label?.includes('Validation Report')
  )
  const primaryValidationArt = validationStdoutArt || validationReportArt || validationArtifacts[0]

  // Mutation: Start Executor
  const startMutation = useMutation({
    mutationFn: () => executeRun(run.id),
    onSuccess: () => {
      setMutationError(null)
      void queryClient.invalidateQueries({ queryKey: ['relay-runs'] })
    },
    onError: (err: any) => {
      setMutationError(err.message || 'Failed to start executor.')
    },
  })

  // Mutation: Cancel Executor
  const cancelMutation = useMutation({
    mutationFn: () => cancelRun(run.id),
    onSuccess: () => {
      setMutationError(null)
      void queryClient.invalidateQueries({ queryKey: ['relay-runs'] })
    },
    onError: (err: any) => {
      setMutationError(err.message || 'Failed to cancel executor.')
    },
  })

  // Mutation: Recover Executor
  const recoverMutation = useMutation({
    mutationFn: () => recoverRun(run.id),
    onSuccess: () => {
      setMutationError(null)
      void queryClient.invalidateQueries({ queryKey: ['relay-runs'] })
    },
    onError: (err: any) => {
      setMutationError(err.message || 'Failed to recover executor.')
    },
  })

  // Mutation: Run Validation
  const validateMutation = useMutation({
    mutationFn: () => validateRun(run.id),
    onSuccess: () => {
      setMutationError(null)
      void queryClient.invalidateQueries({ queryKey: ['relay-runs'] })
    },
    onError: (err: any) => {
      setMutationError(err.message || 'Failed to run validation.')
    },
  })

  const activeMutation = startMutation.isPending || cancelMutation.isPending || recoverMutation.isPending || validateMutation.isPending

  const handleStart = () => {
    setMutationError(null)
    startMutation.mutate()
  }

  const handleCancel = () => {
    setMutationError(null)
    cancelMutation.mutate()
  }

  const handleRecover = () => {
    setMutationError(null)
    recoverMutation.mutate()
  }

  const handleValidate = () => {
    setMutationError(null)
    validateMutation.mutate()
  }

  const formatPhaseLabel = (phase: RelayExecutorPhase): string => {
    const labels: Record<RelayExecutorPhase, string> = {
      idle: 'Awaiting Start',
      dispatched: 'Dispatching…',
      running: 'Executing',
      done: 'Completed',
      blocked: 'Blocked',
      failed: 'Failed',
      unavailable: 'Unavailable',
    }
    return labels[phase]
  }

  const formatPhaseBadgeVariant = (phase: RelayExecutorPhase): string => {
    const variants: Record<RelayExecutorPhase, string> = {
      idle: 'secondary',
      dispatched: 'running',
      running: 'running',
      done: 'success',
      blocked: 'destructive',
      failed: 'destructive',
      unavailable: 'outline',
    }
    return variants[phase]
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

      {/* Agent Status */}
      <Section
        title="Agent Status"
        icon={
          executorPhase === 'running' || executorPhase === 'dispatched'
            ? <Loader2 className="w-4 h-4 text-violet-400 animate-spin" />
            : executorPhase === 'done'
              ? <CheckCircle2 className="w-4 h-4 text-emerald-400" />
              : executorPhase === 'blocked' || executorPhase === 'failed'
                ? <XCircle className="w-4 h-4 text-red-400" />
                : <Clock className="w-4 h-4 text-muted-foreground" />
        }
      >
        <div className="flex items-center gap-2">
          <Badge
            variant={formatPhaseBadgeVariant(executorPhase) as any}
            className="text-xs"
          >
            {formatPhaseLabel(executorPhase)}
          </Badge>
          <span className="text-xs text-muted-foreground">
            {run.executor} / {run.model}
          </span>
        </div>
        {executorPhase === 'running' && (
          <p className="text-xs text-muted-foreground/70 mt-1">
            Executor is actively running. Live log streaming via SSE is not yet available; refresh to see latest output.
          </p>
        )}
        {executorPhase === 'done' && (
          <p className="text-xs text-emerald-400/70 mt-1">
            Executor completed successfully. Review changed files and result below.
          </p>
        )}
        {executorPhase === 'blocked' && (
          <p className="text-xs text-red-400/70 mt-1">
            Executor reported a blocking issue. Review result artifacts for details.
          </p>
        )}
        {executorPhase === 'failed' && (
          <p className="text-xs text-red-400/70 mt-1">
            Executor encountered a failure. Review error artifacts and consider recovery options.
          </p>
        )}
        {executorPhase === 'idle' && (
          <p className="text-xs text-muted-foreground/70 mt-1">
            Ready to dispatch executor. Click Start to begin the agent run.
          </p>
        )}
        {executorPhase === 'unavailable' && (
          <p className="text-xs text-muted-foreground/70 mt-1">
            Execution is not available for this run. Current status: {runStatus}.
          </p>
        )}

        {/* Action Buttons */}
        <div className="flex items-center gap-2 mt-2">
          {actionAvailability.canStart && (
            <Button
              variant="default"
              size="sm"
              onClick={handleStart}
              disabled={activeMutation}
              className="w-fit gap-1.5"
            >
              {startMutation.isPending ? (
                <Loader2 className="w-3.5 h-3.5 animate-spin" />
              ) : (
                <Play className="w-3.5 h-3.5" />
              )}
              {runStatus === 'approved_for_executor' ? 'Start Executor' : 'Restart Executor'}
            </Button>
          )}

          {actionAvailability.canCancel && (
            <Button
              variant="outline"
              size="sm"
              onClick={handleCancel}
              disabled={activeMutation}
              className="w-fit gap-1.5"
              title={actionAvailability.cancelUnavailableReason}
            >
              {cancelMutation.isPending ? (
                <Loader2 className="w-3.5 h-3.5 animate-spin" />
              ) : (
                <StopCircle className="w-3.5 h-3.5" />
              )}
              Cancel
            </Button>
          )}

          {actionAvailability.canRecover && (
            <Button
              variant="outline"
              size="sm"
              onClick={handleRecover}
              disabled={activeMutation}
              className="w-fit gap-1.5"
              title={actionAvailability.recoverUnavailableReason}
            >
              {recoverMutation.isPending ? (
                <Loader2 className="w-3.5 h-3.5 animate-spin" />
              ) : (
                <RefreshCw className="w-3.5 h-3.5" />
              )}
              Recover
            </Button>
          )}
        </div>

        {/* Unsupported action hints */}
        {actionAvailability.canCancel && actionAvailability.cancelUnavailableReason && (
          <p className="text-xs text-muted-foreground/60 italic mt-1 flex items-center gap-1">
            <AlertTriangle className="w-3 h-3" />
            {actionAvailability.cancelUnavailableReason}
          </p>
        )}
        {actionAvailability.canRecover && actionAvailability.recoverUnavailableReason && (
          <p className="text-xs text-muted-foreground/60 italic mt-1 flex items-center gap-1">
            <AlertTriangle className="w-3 h-3" />
            {actionAvailability.recoverUnavailableReason}
          </p>
        )}
      </Section>

      <Separator />

      {/* Live Logs */}
      <Section title="Live Logs" icon={<Terminal className="w-4 h-4" />}>
        {run.logPreview.lines.length > 0 ? (
          <ScrollArea className="h-48 w-full rounded-md border border-border/50 bg-black/30">
            <div className="p-3 font-mono text-xs space-y-0.5">
              {run.logPreview.lines.map((line: string, i: number) => (
                <div key={i} className="text-emerald-300/80 leading-relaxed whitespace-pre-wrap break-all">
                  {line}
                </div>
              ))}
              {run.logPreview.truncated && (
                <div className="text-muted-foreground/50 italic">
                  … output truncated. Full log available via raw artifact content endpoint.
                </div>
              )}
            </div>
          </ScrollArea>
        ) : (
          <div className="flex items-center gap-2 text-xs bg-muted/30 border border-dashed rounded p-3 text-muted-foreground">
            <Terminal className="w-3.5 h-3.5 shrink-0" />
            <span className="italic">
              {executorPhase === 'idle'
                ? 'No logs yet. Start the executor to see output.'
                : 'No event logs recorded for this phase.'}
            </span>
          </div>
        )}

        {/* Executor log artifact links */}
        {resultArtifacts.filter((a: any) =>
          a.filename?.includes('executor_stdout') || a.filename?.includes('command_log') || a.filename?.includes('executor_stderr')
        ).length > 0 && (
          <div className="flex flex-col gap-1 mt-1">
            <p className="text-[11px] text-muted-foreground/60 italic">Executor log artifacts on disk:</p>
            {resultArtifacts
              .filter((a: any) =>
                a.filename?.includes('executor_stdout') || a.filename?.includes('command_log') || a.filename?.includes('executor_stderr')
              )
              .slice(0, 3)
              .map((a: any) => (
                <div key={a.id} className="flex items-center gap-2 text-[11px] font-mono text-muted-foreground/70">
                  <FileText className="w-3 h-3 shrink-0" />
                  <span className="truncate">{a.filename}</span>
                  {a.sizeHint && <span className="shrink-0">({a.sizeHint})</span>}
                </div>
              ))}
          </div>
        )}
      </Section>

      <Separator />

      {/* Validation Commands */}
      <Section title="Validation Commands" icon={<CheckCircle2 className="w-4 h-4" />}>
        <p className="text-xs text-muted-foreground">
          Validation commands are run after executor completion. Results are captured as artifacts.
        </p>
        <div className="flex flex-col gap-1 mt-1">
          {validationArtifacts.length > 0 ? (
            validationArtifacts.slice(0, 5).map((a: any) => (
              <div key={a.id} className="flex items-center gap-2 text-xs font-mono p-1.5 bg-muted/20 rounded border border-border/40">
                <Badge variant={
                  a.filename?.includes('validation_run_json') ? 'success' :
                  a.status === 'ready' ? 'success' : 'secondary'
                } className="text-xs shrink-0">
                  {a.status === 'ready' ? 'Captured' : a.status}
                </Badge>
                <code className="flex-1 text-muted-foreground truncate">{a.filename || a.label}</code>
                {a.sizeHint && <span className="text-muted-foreground/60">{a.sizeHint}</span>}
              </div>
            ))
          ) : (
            <div className="flex items-center gap-2 text-xs bg-muted/30 border border-dashed rounded p-3 text-muted-foreground">
              <Clock className="w-3.5 h-3.5 shrink-0" />
              <span className="italic">
                {localValidationIsRunning
                  ? 'Local validation is running...'
                  : executorPhase === 'idle'
                    ? 'Validation not yet available. Start the executor first.'
                    : executorPhase === 'running'
                      ? 'Validation runs after executor completes.'
                      : 'No validation artifacts found for this run.'}
              </span>
            </div>
          )}

          {localValidationIsRunning && (
            <div className="flex items-center gap-2 text-xs text-muted-foreground mt-1">
              <Loader2 className="w-3.5 h-3.5 animate-spin" />
              <span>Local validation is executing...</span>
            </div>
          )}

          {canRunValidation && (
            <Button
              variant="outline"
              size="sm"
              onClick={handleValidate}
              disabled={activeMutation}
              className="w-fit gap-1.5 mt-2"
            >
              {validateMutation.isPending ? (
                <Loader2 className="w-3.5 h-3.5 animate-spin" />
              ) : (
                <Play className="w-3.5 h-3.5" />
              )}
              Run Validation
            </Button>
          )}

          {/* Show summary counts from run validation */}
          {(run.validationSummary?.errors > 0 || run.validationSummary?.warnings > 0 || run.validationSummary?.passed > 0) && (
            <div className="flex items-center gap-3 text-xs text-muted-foreground mt-1">
              {run.validationSummary.errors > 0 && (
                <span className="flex items-center gap-1">
                  <XCircle className="w-3 h-3 text-red-400" />
                  <span className="text-red-400">{run.validationSummary.errors}</span> errors
                </span>
              )}
              {run.validationSummary.warnings > 0 && (
                <span className="flex items-center gap-1">
                  <AlertTriangle className="w-3 h-3 text-yellow-400" />
                  <span className="text-yellow-400">{run.validationSummary.warnings}</span> warnings
                </span>
              )}
              {run.validationSummary.passed > 0 && (
                <span className="flex items-center gap-1">
                  <CheckCircle2 className="w-3 h-3 text-emerald-400" />
                  <span className="text-emerald-400">{run.validationSummary.passed}</span> passed
                </span>
              )}
            </div>
          )}
        </div>

        {primaryValidationArt?.preview && (
          <div className="mt-2">
            <pre className="text-[11px] font-mono bg-muted/40 p-2.5 rounded border border-border/40 max-h-32 overflow-y-auto whitespace-pre-wrap text-foreground">
              {primaryValidationArt.preview}
            </pre>
          </div>
        )}
      </Section>

      <Separator />

      {/* Changed Files */}
      <Section title="Changed Files" icon={<FileCode className="w-4 h-4" />}>
        {diffArtifacts.length > 0 ? (
          <div className="flex flex-col gap-2">
            {/* Individual diff artifact entries */}
            {diffArtifacts.slice(0, 5).map((a: any) => (
              <div key={a.id} className="flex items-center justify-between p-2 bg-muted/20 rounded border border-border/40 text-xs font-mono">
                <div className="flex items-center gap-2 min-w-0">
                  <FileCode className="w-3.5 h-3.5 shrink-0 text-muted-foreground" />
                  <span className="text-muted-foreground truncate">{a.filename || a.label}</span>
                </div>
                {a.sizeHint && (
                  <span className="text-muted-foreground/60 shrink-0 ml-2">{a.sizeHint}</span>
                )}
              </div>
            ))}

            {/* Preview for primary diff artifact */}
            {primaryDiffArt?.preview && (
              <pre className="text-[11px] font-mono bg-muted/40 p-2.5 rounded border border-border/40 max-h-48 overflow-y-auto whitespace-pre-wrap text-foreground">
                {primaryDiffArt.preview}
              </pre>
            )}

            <p className="text-xs text-muted-foreground/60 italic">
              Full diff content available via raw artifact endpoint.
            </p>
          </div>
        ) : (
          <div className="flex items-center gap-2 text-xs bg-muted/30 border border-dashed rounded p-3 text-muted-foreground">
            <FileCode className="w-3.5 h-3.5 shrink-0" />
            <span className="italic">
              {executorPhase === 'idle'
                ? 'Execution has not started — diff not yet available.'
                : executorPhase === 'running'
                  ? 'Execution in progress — diff not yet available.'
                  : 'No diff artifacts found for this run.'}
            </span>
          </div>
        )}
      </Section>

      <Separator />

      {/* Executor Result */}
      <Section title="Executor Result" icon={<CheckCircle2 className="w-4 h-4 text-muted-foreground" />}>
        {primaryResultArt ? (
          <div className="flex flex-col gap-2">
            <div className="flex items-center gap-2">
              <Badge variant={executorPhase === 'done' ? 'success' : 'secondary'} className="text-xs">
                {executorPhase === 'done' ? 'Completed' : executorPhase === 'blocked' || executorPhase === 'failed' ? 'Failed' : 'Captured'}
              </Badge>
              <span className="text-xs font-mono text-muted-foreground truncate">{primaryResultArt.filename}</span>
              {primaryResultArt.sizeHint && (
                <span className="text-xs text-muted-foreground/60">{primaryResultArt.sizeHint}</span>
              )}
            </div>

            {/* Parsed result preview */}
            {primaryResultArt.preview ? (
              <pre className="text-[11px] font-mono bg-muted/40 p-2.5 rounded border border-border/40 max-h-48 overflow-y-auto whitespace-pre-wrap text-foreground">
                {primaryResultArt.preview}
              </pre>
            ) : (
              <div className="text-xs bg-muted/30 border border-dashed rounded p-3 text-muted-foreground">
                <span className="italic">Result content preview not available.</span>
              </div>
            )}

            {/* Show other result artifacts as references */}
            {resultArtifacts.length > 1 && (
              <div className="flex flex-col gap-1 mt-1">
                <p className="text-[11px] text-muted-foreground/60 italic">Additional result artifacts:</p>
                {resultArtifacts.filter((a: any) => a.id !== primaryResultArt.id).slice(0, 3).map((a: any) => (
                  <div key={a.id} className="flex items-center gap-2 text-[11px] font-mono text-muted-foreground/70">
                    <FileText className="w-3 h-3 shrink-0" />
                    <span className="truncate">{a.filename}</span>
                    {a.sizeHint && <span className="shrink-0">({a.sizeHint})</span>}
                  </div>
                ))}
              </div>
            )}
          </div>
        ) : (
          <div className="flex items-center gap-2 text-xs bg-muted/30 border border-dashed rounded p-3 text-muted-foreground">
            <Clock className="w-3.5 h-3.5 shrink-0" />
            <span className="italic">
              {executorPhase === 'idle'
                ? 'Execution has not started — result pending.'
                : executorPhase === 'running'
                  ? 'Execution in progress — result pending.'
                  : 'No result artifact found for this run.'}
            </span>
          </div>
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
