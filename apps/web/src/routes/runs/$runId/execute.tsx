import { createFileRoute } from "@tanstack/react-router";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { useState, useMemo } from "react";
import {
  runDetailQueryOptions,
  runArtifactsQueryOptions,
  runEventsQueryOptions,
  executeRun,
  cancelRun,
  recoverRun,
  validateRun,
  evaluateExecuteValidationAction,
} from "@/features/relay-runs";
import type { RelayArtifact, RelayExecutorPhase, RelayRun } from "@/features/relay-runs";
import { RunWorkbenchLayout } from "@/components/relay/RunWorkbenchLayout";
import {
  RelayStateBanner,
} from "@/components/relay/RelayStateSurface";
import {
  RunWorkbenchLoadFailedState,
  RunWorkbenchLoadingState,
} from "@/components/relay/RunWorkbenchStates";
import { ValidationPanel } from "@/components/relay/ValidationPanel";
import { LogPreviewPanel } from "@/components/relay/LogPreviewPanel";
import { RunEvidenceBrowser } from "@/components/relay/RunEvidenceBrowser";
import {
  RunStageInspectorSection,
  RunStageKeyValueRow,
  RunStagePipeline,
  RunStageStateCard,
  RunStageSummaryCard,
  RunStageSummaryChip,
  RunStageContentSection,
  RunStageEvidenceRow,
  RunStageEvidenceList,
  RunStageMainStack,
} from "@/components/relay/RunStagePrimitives";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { ScrollArea } from "@/components/ui/scroll-area";
import {
  Loader2,
  CheckCircle2,
  FileCode,
  Terminal,
  AlertTriangle,
  Play,
  XCircle,
  RefreshCw,
  StopCircle,
  Clock,
} from "lucide-react";
import {
  EXECUTE_PIPELINE_STEPS,
  getExecuteDisplayState,
  getExecutePipelineStatuses,
  getExecuteStateCardCopy,
} from "./runExecuteVisualState";

export const Route = createFileRoute("/runs/$runId/execute")({
  component: ExecutePage,
});

function ExecutePage() {
  const { runId } = Route.useParams();
  const {
    data: run,
    isLoading: isLoadingRun,
    error: errorRun,
  } = useQuery(runDetailQueryOptions(runId));
  const { data: artifacts, isLoading: isLoadingArtifacts } = useQuery(
    runArtifactsQueryOptions(runId),
  );
  const { data: events, isLoading: isLoadingEvents } = useQuery(
    runEventsQueryOptions(runId),
  );

  if (isLoadingRun || isLoadingArtifacts || isLoadingEvents) {
    return <RunWorkbenchLoadingState label="Loading run" />;
  }

  if (errorRun || !run) {
    return (
      <RunWorkbenchLoadFailedState
        title="Run failed to load"
        description="Relay could not load this run. Return to the runs registry and reopen the workbench."
        backToRuns
      />
    );
  }

  const formattedLogs = events
    ? events.map((e) => {
        const timeStr = new Date(e.createdAt).toLocaleTimeString("en-US", {
          hour12: false,
          hour: "2-digit",
          minute: "2-digit",
          second: "2-digit",
        });
        return `[${timeStr}] ${e.message}`;
      })
    : [];

  const logPreview = {
    lines: formattedLogs.slice(-50),
    truncated: formattedLogs.length > 50,
  };
  const resolvedArtifacts = artifacts || [];
  const resolvedEvents = events || [];

  return (
    <RunWorkbenchLayout
      run={{
        ...run,
        artifacts: resolvedArtifacts,
        latestEvents: resolvedEvents,
        logPreview,
      }}
      currentStep="execute"
      mainContent={<ExecuteMainContent run={run} artifacts={resolvedArtifacts} />}
      initialInspectorTab="details"
      inspectorTabs={[
        { key: "details", label: "Details" },
        { key: "artifacts", label: "Artifacts" },
        { key: "validation", label: "Validation" },
        { key: "logs", label: "Logs" },
      ]}
      inspectorPanels={{
        details: <ExecuteDetailsPanel run={run} artifacts={resolvedArtifacts} />,
        logs: <LogPreviewPanel logPreview={logPreview} />,
        artifacts: (
          <RunEvidenceBrowser
            runId={run.id}
            artifacts={resolvedArtifacts}
            events={resolvedEvents}
          />
        ),
        validation: <ValidationPanel summary={run.validationSummary} />,
      }}
    />
  );
}

function deriveExecutorPhase(
  runStatus: string,
  lifecycleState: string,
): RelayExecutorPhase {
  if (lifecycleState === "failed" || runStatus === "blocked") return "blocked";
  if (runStatus === "executor_dispatched" || runStatus === "executor_running")
    return "running";
  if (runStatus === "executor_done" || runStatus === "agent_done")
    return "done";
  if (runStatus === "executor_blocked" || runStatus === "agent_blocked")
    return "failed";
  if (runStatus === "agent_result_needs_review") return "done";
  if (runStatus === "approved_for_executor") return "idle";
  if (lifecycleState === "execute") return "idle";
  return "unavailable";
}

function artifactIdentity(a: any): string {
  return [
    a.storageKind,
    a.kind,
    a.filename,
    a.label,
  ]
    .filter(Boolean)
    .join(" ")
    .toLowerCase();
}

function artifactHas(a: any, ...tokens: string[]): boolean {
  const id = artifactIdentity(a);
  return tokens.some((token) => id.includes(token.toLowerCase()));
}

function artifactPreviewHas(a: any, token: string): boolean {
  return String(a.preview || "").toLowerCase().includes(token.toLowerCase());
}

function isResultArtifact(a: any): boolean {
  return (
    a.kind === "result" ||
    artifactHas(
      a,
      "executor_result",
      "agent_result_raw",
      "executor_stdout",
      "executor_stderr",
      "command_log",
      "codex_last_message",
    )
  );
}

function isExecutorResultArtifact(a: any): boolean {
  return artifactHas(a, "executor_result");
}

function isCommandLogArtifact(a: any): boolean {
  return artifactHas(a, "command_log");
}

function isExecutorLogArtifact(a: any): boolean {
  return artifactHas(a, "executor_stdout", "executor_stderr", "command_log");
}

function isDiffArtifact(a: any): boolean {
  return a.kind === "diff" || artifactHas(a, "git_diff", "git_status");
}

function isValidationArtifact(a: any): boolean {
  return (
    a.kind === "validation" ||
    artifactHas(
      a,
      "validation_run_json",
      "validation_progress_json",
      "validation_stdout",
      "validation_stderr",
      "handoff_validation_json",
      "packet_validation_report",
      "brief_validation_report",
      "intake_validation_report",
    )
  );
}

function ExecuteMainContent({
  run,
  artifacts,
}: {
  run: RelayRun;
  artifacts: any[];
}) {
  const queryClient = useQueryClient();
  const [mutationError, setMutationError] = useState<string | null>(null);

  const runStatus = (run.status || "") as string;
  const runLifecycle = (run.lifecycleState || "") as string;
  const executorPhase = deriveExecutorPhase(runStatus, runLifecycle);

  const canRunValidation = evaluateExecuteValidationAction(
    artifacts,
    runStatus,
  );
  const localValidationIsRunning = runStatus === "local_validation_running";

  const actionAvailability = useMemo(() => {
    const isApproved = runStatus === "approved_for_executor";
    const isExecuting =
      runStatus === "executor_dispatched" || runStatus === "executor_running";
    const isBlocked = executorPhase === "blocked" || executorPhase === "failed";
    return {
      canStart: isApproved || (isBlocked && runLifecycle === "execute"),
      canCancel: isExecuting,
      canRecover: isBlocked && runLifecycle === "execute",
      startUnavailableReason:
        !isApproved && !isBlocked ? `Current status: ${runStatus}` : undefined,
      cancelUnavailableReason:
        "Cancellation is not yet implemented in the backend.",
      recoverUnavailableReason:
        "Recovery is not yet implemented in the backend.",
    };
  }, [runStatus, runLifecycle, executorPhase]);

  // Find relevant artifacts for Step 3 display
  const resultArtifacts = artifacts.filter(isResultArtifact);
  const diffArtifacts = artifacts.filter(isDiffArtifact);
  const validationArtifacts = artifacts.filter(isValidationArtifact);

  // Find specific result candidates
  const commandLogArt = resultArtifacts.find(isCommandLogArtifact);
  const preflightResultArt = resultArtifacts.find(
    (a: any) =>
      isExecutorResultArtifact(a) &&
      artifactPreviewHas(a, "executor preflight failed"),
  );
  const preflightCommandLogArt = resultArtifacts.find(
    (a: any) =>
      isCommandLogArtifact(a) &&
      artifactPreviewHas(a, "Preflight: BLOCKED"),
  );
  const preflightBlocked =
    (executorPhase === "blocked" || executorPhase === "failed") &&
    Boolean(preflightResultArt || preflightCommandLogArt);

  const executorResultArt = resultArtifacts.find(
    (a: any) =>
      a.filename?.includes("executor_result") ||
      a.label?.includes("Executor Result"),
  );
  const agentResultRawArt = resultArtifacts.find(
    (a: any) =>
      a.filename?.includes("agent_result_raw") ||
      a.label?.includes("Agent Result"),
  );
  const executorStdoutArt = resultArtifacts.find(
    (a: any) =>
      a.filename?.includes("executor_stdout") ||
      a.label?.includes("Executor Stdout"),
  );

  // Choose the best result artifact for display
  const primaryResultArt =
    executorResultArt ||
    agentResultRawArt ||
    executorStdoutArt ||
    resultArtifacts[0];

  // Find diff artifacts for changed files
  const gitDiffPatch = diffArtifacts.find(
    (a: any) =>
      a.filename?.includes("git_diff_patch") ||
      a.label?.includes("Git Diff Patch"),
  );
  const gitDiffNameStatus = diffArtifacts.find(
    (a: any) =>
      a.filename?.includes("git_diff_name_status") ||
      a.label?.includes("Git Diff Name Status"),
  );
  const gitStatus = diffArtifacts.find(
    (a: any) =>
      a.filename?.includes("git_status") || a.label?.includes("Git Status"),
  );
  const primaryDiffArt =
    gitDiffPatch || gitDiffNameStatus || gitStatus || diffArtifacts[0];

  // Find validation artifact candidates
  const validationStdoutArt = validationArtifacts.find(
    (a: any) =>
      a.filename?.includes("validation_stdout") ||
      a.label?.includes("Validation Output"),
  );
  const validationReportArt = validationArtifacts.find(
    (a: any) =>
      a.filename?.includes("validation_run") ||
      a.label?.includes("Validation Report"),
  );
  const primaryValidationArt =
    validationStdoutArt || validationReportArt || validationArtifacts[0];

  // Mutation: Start Executor
  const startMutation = useMutation({
    mutationFn: () => executeRun(run.id),
    onSuccess: () => {
      setMutationError(null);
      void queryClient.invalidateQueries({ queryKey: ["relay-runs"] });
    },
    onError: (err: any) => {
      setMutationError(err.message || "Failed to start executor.");
    },
  });

  // Mutation: Cancel Executor
  const cancelMutation = useMutation({
    mutationFn: () => cancelRun(run.id),
    onSuccess: () => {
      setMutationError(null);
      void queryClient.invalidateQueries({ queryKey: ["relay-runs"] });
    },
    onError: (err: any) => {
      setMutationError(err.message || "Failed to cancel executor.");
    },
  });

  // Mutation: Recover Executor
  const recoverMutation = useMutation({
    mutationFn: () => recoverRun(run.id),
    onSuccess: () => {
      setMutationError(null);
      void queryClient.invalidateQueries({ queryKey: ["relay-runs"] });
    },
    onError: (err: any) => {
      setMutationError(err.message || "Failed to recover executor.");
    },
  });

  // Mutation: Run Validation
  const validateMutation = useMutation({
    mutationFn: () => validateRun(run.id),
    onSuccess: () => {
      setMutationError(null);
      void queryClient.invalidateQueries({ queryKey: ["relay-runs"] });
    },
    onError: (err: any) => {
      setMutationError(err.message || "Failed to run validation.");
    },
  });

  const activeMutation =
    startMutation.isPending ||
    cancelMutation.isPending ||
    recoverMutation.isPending ||
    validateMutation.isPending;
  const executeVisualStateInput = {
    run,
    executorPhase,
    preflightBlocked,
    executePending: startMutation.isPending,
    cancelPending: cancelMutation.isPending,
    recoverPending: recoverMutation.isPending,
    validatePending: validateMutation.isPending,
    hasResultArtifacts: resultArtifacts.length > 0,
    hasDiffArtifacts: diffArtifacts.length > 0,
    hasValidationArtifacts: validationArtifacts.length > 0,
  };
  const executeDisplayState = getExecuteDisplayState(executeVisualStateInput);
  const executePipelineStatuses = getExecutePipelineStatuses(
    executeVisualStateInput,
  );
  const executeStateCardCopy = getExecuteStateCardCopy(executeDisplayState);

  const handleStart = () => {
    setMutationError(null);
    startMutation.mutate();
  };

  const handleCancel = () => {
    setMutationError(null);
    cancelMutation.mutate();
  };

  const handleRecover = () => {
    setMutationError(null);
    recoverMutation.mutate();
  };

  const handleValidate = () => {
    setMutationError(null);
    validateMutation.mutate();
  };

  const formatPhaseLabel = (phase: RelayExecutorPhase): string => {
    const labels: Record<RelayExecutorPhase, string> = {
      idle: "Awaiting Start",
      dispatched: "Dispatching…",
      running: "Executing",
      done: "Completed",
      blocked: "Blocked",
      failed: "Failed",
      unavailable: "Unavailable",
    };
    return labels[phase];
  };

  const formatPhaseBadgeVariant = (phase: RelayExecutorPhase): string => {
    const variants: Record<RelayExecutorPhase, string> = {
      idle: "secondary",
      dispatched: "running",
      running: "running",
      done: "success",
      blocked: "destructive",
      failed: "destructive",
      unavailable: "outline",
    };
    return variants[phase];
  };

  return (
    <RunStageMainStack>
      {mutationError && (
        <RelayStateBanner
          tone="danger"
          title="Executor action failed"
          description={mutationError}
        />
      )}

      <RunStageStateCard
        tone={executeStateCardCopy.tone}
        eyebrow={executeStateCardCopy.eyebrow}
        title={executeStateCardCopy.title}
        message={executeStateCardCopy.message}
        action={
          <div className="flex items-center gap-2">
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
                {runStatus === "approved_for_executor"
                  ? "Start Executor"
                  : "Restart Executor"}
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
        }
      >
        <div className="flex items-center gap-2 mt-2">
          <Badge
            variant={formatPhaseBadgeVariant(executorPhase) as any}
            className="text-xs"
          >
            {formatPhaseLabel(executorPhase)}
          </Badge>
          <span className="text-xs text-muted-foreground flex items-center gap-1.5 font-mono">
            <span>{run.executorAdapter || run.executor}</span>
            <span className="opacity-50">/</span>
            <span>{run.model}</span>
          </span>
        </div>

        {preflightBlocked && (
          <RelayStateBanner
            tone="blocked"
            title="Executor blocked"
            description="Relay did not start the executor CLI. Fix local daemon readiness, binary path, workdir, or prompt-file availability, then retry."
            metadata={`Evidence: ${(preflightCommandLogArt || commandLogArt)?.filename || "command_log"} / ${(preflightResultArt || executorResultArt)?.filename || "executor_result"}`}
            className="mt-3"
          />
        )}
        {!preflightBlocked &&
          (executorPhase === "blocked" || executorPhase === "failed") && (
            <RelayStateBanner
              tone="blocked"
              title="Executor blocked"
              description={
                executorPhase === "failed"
                  ? "Executor encountered a failure. Review error artifacts and consider recovery options."
                  : "Executor reported a blocking issue. Review result artifacts for details."
              }
              metadata={`Current status: ${runStatus}`}
              className="mt-3"
            />
          )}

        {actionAvailability.canCancel &&
          actionAvailability.cancelUnavailableReason && (
            <p className="text-xs text-muted-foreground/60 italic mt-1 flex items-center gap-1">
              <AlertTriangle className="w-3 h-3 text-yellow-400" />
              {actionAvailability.cancelUnavailableReason}
            </p>
          )}
        {actionAvailability.canRecover &&
          actionAvailability.recoverUnavailableReason && (
            <p className="text-xs text-muted-foreground/60 italic mt-1 flex items-center gap-1">
              <AlertTriangle className="w-3 h-3 text-yellow-400" />
              {actionAvailability.recoverUnavailableReason}
            </p>
          )}
      </RunStageStateCard>

      <RunStageSummaryCard
        eyebrow="Execute Pipeline"
        title="Executor progression"
        description="Brief approval, dispatch, execution, result capture, and audit readiness."
      >
        <div className="mb-3 flex flex-wrap gap-2">
          <RunStageSummaryChip label="Status" value={runStatus} mono />
          <RunStageSummaryChip
            label="Executor"
            value={formatPhaseLabel(executorPhase)}
            tone={getExecutorPhaseTone(executorPhase)}
          />
          <RunStageSummaryChip
            label="Result"
            value={getArtifactSummaryLabel(resultArtifacts.length)}
            tone={resultArtifacts.length > 0 ? "success" : "default"}
          />
          <RunStageSummaryChip
            label="Validation"
            value={getValidationSummaryLabel(
              localValidationIsRunning,
              validationArtifacts.length,
            )}
            tone={
              localValidationIsRunning
                ? "info"
                : validationArtifacts.length > 0
                  ? "success"
                  : "default"
            }
          />
        </div>
        <RunStagePipeline
          steps={EXECUTE_PIPELINE_STEPS}
          statuses={executePipelineStatuses}
        />
      </RunStageSummaryCard>

      <RunStageContentSection
        eyebrow="Logs"
        title="Recent Activity & Live Logs"
        description="Live execution events and output captured from the running executor."
      >
        {run.logPreview.lines.length > 0 ? (
          <ScrollArea className="h-48 w-full rounded-md border border-[var(--relay-row-border)] bg-[var(--relay-code-bg)]">
            <div className="min-w-0 p-3 font-mono text-xs">
              <div className="overflow-x-auto">
                <div className="min-w-max space-y-0.5">
                  {run.logPreview.lines.map((line: string, i: number) => (
                    <div
                      key={i}
                      className="text-emerald-300/80 leading-relaxed whitespace-pre"
                    >
                      {line}
                    </div>
                  ))}
                  {run.logPreview.truncated && (
                    <div className="text-muted-foreground/50 italic">
                      … output truncated. Full log available via raw artifact content endpoint.
                    </div>
                  )}
                </div>
              </div>
            </div>
          </ScrollArea>
        ) : (
          <div className="flex items-center gap-2 text-xs bg-[var(--surface-inset)]/30 border border-dashed rounded p-3 text-muted-foreground">
            <Terminal className="w-3.5 h-3.5 shrink-0" />
            <span className="italic">
              {executorPhase === "idle"
                ? "No logs yet. Start the executor to see output."
                : "No event logs recorded for this phase."}
            </span>
          </div>
        )}

        {resultArtifacts.filter(isExecutorLogArtifact).length > 0 && (
          <div className="flex flex-col gap-1 mt-2">
            <p className="text-[11px] text-muted-foreground/60 italic">
              Executor log artifacts on disk:
            </p>
            <RunStageEvidenceList>
              {resultArtifacts
                .filter(isExecutorLogArtifact)
                .slice(0, 3)
                .map((a: any) => (
                  <RunStageEvidenceRow
                    key={a.id}
                    label={a.filename}
                    value={a.sizeHint || ""}
                  />
                ))}
            </RunStageEvidenceList>
          </div>
        )}
      </RunStageContentSection>

      <RunStageContentSection
        eyebrow="Validation"
        title="Validation Commands"
        description="Validation commands run after executor completion. Results are captured as artifacts."
        actions={
          canRunValidation ? (
            <Button
              variant="outline"
              size="sm"
              onClick={handleValidate}
              disabled={activeMutation}
              className="gap-1.5"
            >
              {validateMutation.isPending ? (
                <Loader2 className="w-3.5 h-3.5 animate-spin" />
              ) : (
                <Play className="w-3.5 h-3.5" />
              )}
              Run Validation
            </Button>
          ) : undefined
        }
      >
        <div className="flex flex-col gap-3">
          {validationArtifacts.length > 0 ? (
            <RunStageEvidenceList>
              {validationArtifacts.slice(0, 5).map((a: any) => (
                <RunStageEvidenceRow
                  key={a.id}
                  label={a.filename || a.label}
                  value={
                    a.storageKind === "validation_run_json"
                      ? "Validation Result"
                      : a.storageKind === "validation_progress_json"
                        ? "Validation Progress"
                        : a.status === "ready"
                          ? "Captured"
                          : a.status
                  }
                  status={a.sizeHint ? <span className="text-muted-foreground/60 text-xs">{a.sizeHint}</span> : null}
                />
              ))}
            </RunStageEvidenceList>
          ) : (
            <div className="flex items-center gap-2 text-xs bg-[var(--surface-inset)]/30 border border-dashed rounded p-3 text-muted-foreground">
              <Clock className="w-3.5 h-3.5 shrink-0" />
              <span className="italic">
                {localValidationIsRunning
                  ? "Local validation is running..."
                  : executorPhase === "idle"
                    ? "Validation not yet available. Start the executor first."
                    : executorPhase === "running"
                      ? "Validation runs after executor completes."
                      : "No validation artifacts found for this run."}
              </span>
            </div>
          )}

          {localValidationIsRunning && (
            <div className="flex items-center gap-2 text-xs text-muted-foreground mt-1">
              <Loader2 className="w-3.5 h-3.5 animate-spin" />
              <span>Local validation is executing...</span>
            </div>
          )}

          {(run.validationSummary?.errors > 0 ||
            run.validationSummary?.warnings > 0 ||
            run.validationSummary?.passed > 0) && (
            <div className="flex items-center gap-3 text-xs text-muted-foreground mt-1">
              {run.validationSummary.errors > 0 && (
                <span className="flex items-center gap-1">
                  <XCircle className="w-3 h-3 text-red-400" />
                  <span className="text-red-400">
                    {run.validationSummary.errors}
                  </span>{" "}
                  errors
                </span>
              )}
              {run.validationSummary.warnings > 0 && (
                <span className="flex items-center gap-1">
                  <AlertTriangle className="w-3 h-3 text-yellow-400" />
                  <span className="text-yellow-400">
                    {run.validationSummary.warnings}
                  </span>{" "}
                  warnings
                </span>
              )}
              {run.validationSummary.passed > 0 && (
                <span className="flex items-center gap-1">
                  <CheckCircle2 className="w-3 h-3 text-emerald-400" />
                  <span className="text-emerald-400">
                    {run.validationSummary.passed}
                  </span>{" "}
                  passed
                </span>
              )}
            </div>
          )}

          {primaryValidationArt?.preview && (
            <div className="mt-2">
              <pre className="max-w-full overflow-x-auto text-[11px] font-mono bg-[var(--relay-code-bg)] p-2.5 rounded border border-[var(--relay-row-border)] max-h-32 overflow-y-auto whitespace-pre-wrap text-foreground">
                {primaryValidationArt.preview}
              </pre>
            </div>
          )}
        </div>
      </RunStageContentSection>

      <RunStageContentSection
        eyebrow="Diff"
        title="Changed Files"
        description="Changes made to the target repository workspace by the executor."
      >
        {diffArtifacts.length > 0 ? (
          <div className="flex flex-col gap-2">
            <RunStageEvidenceList>
              {diffArtifacts.slice(0, 5).map((a: any) => (
                <RunStageEvidenceRow
                  key={a.id}
                  label={a.filename || a.label}
                  value={a.sizeHint || ""}
                />
              ))}
            </RunStageEvidenceList>

            {primaryDiffArt?.preview && (
              <pre className="max-w-full overflow-x-auto text-[11px] font-mono bg-[var(--relay-code-bg)] p-2.5 rounded border border-[var(--relay-row-border)] max-h-48 overflow-y-auto whitespace-pre-wrap text-foreground">
                {primaryDiffArt.preview}
              </pre>
            )}

            <p className="text-xs text-muted-foreground/60 italic">
              Full diff content available via raw artifact endpoint.
            </p>
          </div>
        ) : (
          <div className="flex items-center gap-2 text-xs bg-[var(--surface-inset)]/30 border border-dashed rounded p-3 text-muted-foreground">
            <FileCode className="w-3.5 h-3.5 shrink-0" />
            <span className="italic">
              {executorPhase === "idle"
                ? "Execution has not started — diff not yet available."
                : executorPhase === "running"
                  ? "Execution in progress — diff not yet available."
                  : "No diff artifacts found for this run."}
            </span>
          </div>
        )}
      </RunStageContentSection>

      <RunStageContentSection
        eyebrow="Result"
        title="Executor Result"
        description="Final captured result output and terminal code of the running executor."
      >
        {primaryResultArt ? (
          <div className="flex flex-col gap-2">
            <div className="flex items-center gap-2">
              <Badge variant={executorPhase === "done" ? "default" : "secondary"}>
                {preflightBlocked
                  ? "Preflight Blocked"
                  : executorPhase === "done"
                    ? "Completed"
                    : executorPhase === "blocked" || executorPhase === "failed"
                      ? "Failed"
                      : "Captured"}
              </Badge>
              <span className="text-xs font-mono text-muted-foreground truncate">
                {primaryResultArt.filename}
              </span>
              {primaryResultArt.sizeHint && (
                <span className="text-xs text-muted-foreground/60">
                  {primaryResultArt.sizeHint}
                </span>
              )}
            </div>

            {primaryResultArt.preview ? (
              <pre className="max-w-full overflow-x-auto text-[11px] font-mono bg-[var(--relay-code-bg)] p-2.5 rounded border border-[var(--relay-row-border)] max-h-48 overflow-y-auto whitespace-pre-wrap text-foreground">
                {primaryResultArt.preview}
              </pre>
            ) : (
              <div className="text-xs bg-[var(--surface-inset)]/30 border border-dashed rounded p-3 text-muted-foreground">
                <span className="italic">Result content preview not available.</span>
              </div>
            )}

            {resultArtifacts.length > 1 && (
              <div className="flex flex-col gap-1 mt-2">
                <p className="text-[11px] text-muted-foreground/60 italic">
                  Additional result artifacts:
                </p>
                <RunStageEvidenceList>
                  {resultArtifacts
                    .filter((a: any) => a.id !== primaryResultArt.id)
                    .slice(0, 3)
                    .map((a: any) => (
                      <RunStageEvidenceRow
                        key={a.id}
                        label={a.filename}
                        value={a.sizeHint || ""}
                      />
                    ))}
                </RunStageEvidenceList>
              </div>
            )}
          </div>
        ) : (
          <div className="flex items-center gap-2 text-xs bg-[var(--surface-inset)]/30 border border-dashed rounded p-3 text-muted-foreground">
            <Clock className="w-3.5 h-3.5 shrink-0" />
            <span className="italic">
              {executorPhase === "idle"
                ? "Execution has not started — result pending."
                : executorPhase === "running"
                  ? "Execution in progress — result pending."
                  : "No result artifact found for this run."}
            </span>
          </div>
        )}
      </RunStageContentSection>
    </RunStageMainStack>
  );
}

function ExecuteDetailsPanel({
  run,
  artifacts,
}: {
  run: RelayRun;
  artifacts: RelayArtifact[];
}) {
  const runStatus = (run.status || "") as string;
  const runLifecycle = (run.lifecycleState || "") as string;
  const executorPhase = deriveExecutorPhase(runStatus, runLifecycle);
  const resultArtifacts = artifacts.filter(isResultArtifact);
  const diffArtifacts = artifacts.filter(isDiffArtifact);
  const validationArtifacts = artifacts.filter(isValidationArtifact);
  const commandLogArt = resultArtifacts.find(isCommandLogArtifact);
  const preflightResultArt = resultArtifacts.find(
    (artifact) =>
      isExecutorResultArtifact(artifact) &&
      artifactPreviewHas(artifact, "executor preflight failed"),
  );
  const preflightCommandLogArt = resultArtifacts.find(
    (artifact) =>
      isCommandLogArtifact(artifact) &&
      artifactPreviewHas(artifact, "Preflight: BLOCKED"),
  );
  const preflightBlocked =
    (executorPhase === "blocked" || executorPhase === "failed") &&
    Boolean(preflightResultArt || preflightCommandLogArt);
  const primaryResultArt =
    resultArtifacts.find(
      (artifact) =>
        artifact.filename?.includes("executor_result") ||
        artifact.label?.includes("Executor Result"),
    ) || resultArtifacts[0];
  const primaryDiffArt =
    diffArtifacts.find(
      (artifact) =>
        artifact.filename?.includes("git_diff_patch") ||
        artifact.label?.includes("Git Diff Patch"),
    ) || diffArtifacts[0];
  const primaryValidationArt =
    validationArtifacts.find(
      (artifact) =>
        artifact.filename?.includes("validation_run") ||
        artifact.label?.includes("Validation Report"),
    ) || validationArtifacts[0];

  return (
    <div className="flex flex-col gap-3">
      <RunStageInspectorSection title="Run State">
        <RunStageKeyValueRow label="Status" value={runStatus} mono />
        <RunStageKeyValueRow label="Lifecycle" value={runLifecycle} mono />
        <RunStageKeyValueRow label="Active step" value="Execute" />
      </RunStageInspectorSection>

      <RunStageInspectorSection title="Executor">
        <RunStageKeyValueRow
          label="Phase"
          value={formatExecutorPhaseLabel(executorPhase)}
        />
        <RunStageKeyValueRow
          label="Adapter"
          value={run.executorAdapter || run.executor || "-"}
          mono
        />
        <RunStageKeyValueRow label="Model" value={run.model || "-"} mono />
      </RunStageInspectorSection>

      <RunStageInspectorSection title="Dispatch">
        <RunStageKeyValueRow
          label="Start"
          value={
            runStatus === "approved_for_executor"
              ? "Available"
              : `Unavailable from ${runStatus}`
          }
        />
        <RunStageKeyValueRow
          label="Preflight"
          value={preflightBlocked ? "Blocked" : "No blocker detected"}
        />
        <RunStageKeyValueRow
          label="Command log"
          value={formatArtifactLocation(preflightCommandLogArt || commandLogArt)}
          mono
        />
      </RunStageInspectorSection>

      <RunStageInspectorSection title="Result">
        <RunStageKeyValueRow
          label="Result"
          value={formatArtifactLocation(primaryResultArt)}
          mono
        />
        <RunStageKeyValueRow
          label="Changed files"
          value={formatArtifactCount(diffArtifacts.length)}
        />
        <RunStageKeyValueRow
          label="Diff"
          value={formatArtifactLocation(primaryDiffArt)}
          mono
        />
      </RunStageInspectorSection>

      <RunStageInspectorSection title="Validation">
        <RunStageKeyValueRow
          label="State"
          value={getValidationSummaryLabel(
            runStatus === "local_validation_running",
            validationArtifacts.length,
          )}
        />
        <RunStageKeyValueRow
          label="Evidence"
          value={formatArtifactCount(validationArtifacts.length)}
        />
        <RunStageKeyValueRow
          label="Report"
          value={formatArtifactLocation(primaryValidationArt)}
          mono
        />
      </RunStageInspectorSection>
    </div>
  );
}

function formatExecutorPhaseLabel(phase: RelayExecutorPhase): string {
  const labels: Record<RelayExecutorPhase, string> = {
    idle: "Awaiting Start",
    dispatched: "Dispatching",
    running: "Executing",
    done: "Completed",
    blocked: "Blocked",
    failed: "Failed",
    unavailable: "Unavailable",
  };
  return labels[phase];
}

function getExecutorPhaseTone(
  phase: RelayExecutorPhase,
): "default" | "success" | "warning" | "danger" | "info" {
  if (phase === "done") return "success";
  if (phase === "running" || phase === "dispatched") return "info";
  if (phase === "blocked" || phase === "failed") return "danger";
  if (phase === "idle") return "warning";
  return "default";
}

function getArtifactSummaryLabel(count: number): string {
  return count > 0 ? formatArtifactCount(count) : "Pending";
}

function getValidationSummaryLabel(
  isRunning: boolean,
  artifactCount: number,
): string {
  if (isRunning) return "Running";
  if (artifactCount > 0) return formatArtifactCount(artifactCount);
  return "Pending";
}

function formatArtifactCount(count: number): string {
  if (count === 1) return "1 artifact";
  return `${count} artifacts`;
}

function formatArtifactLocation(artifact?: RelayArtifact): string {
  return artifact?.path || artifact?.filename || "-";
}

