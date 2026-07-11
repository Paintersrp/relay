import * as React from "react";
import { Link, Navigate } from "@tanstack/react-router";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  AlertTriangle,
  ArrowLeft,
  CheckCircle2,
  ClipboardCheck,
  FileCode2,
  Loader2,
  Play,
  RotateCcw,
  Square,
} from "lucide-react";

import { RelayStateSurface } from "@/components/relay/RelayStateSurface";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  cancelWorkflowAttempt,
  deriveWorkflowAttemptControlState,
  getDefaultModelForAdapter,
  getModelOptionsForAdapter,
  isNonterminalWorkflowAttemptStatus,
  isTerminalWorkflowAttemptStatus,
  prepareWorkflowAudit,
  reconcileWorkflowAttempt,
  startWorkflowAttempt,
  workflowAttemptQueryOptions,
  workflowAuditStatusQueryOptions,
  workflowRunDetailQueryOptions,
  workflowRunKeys,
  workflowRunStageRoute,
  workflowSpecificationQueryOptions,
  workflowApiUrl,
  type WorkflowExecutionAttempt,
  type WorkflowExecutionAttemptStatus,
  type WorkflowExecutionAttemptSummary,
  type WorkflowRunStage,
  type WorkflowRunStatus,
} from "@/features/relay-runs";
import { EXECUTOR_ADAPTER_OPTIONS } from "@/features/relay-runs";
import { resolveWorkflowAvailableThroughStage } from "@/features/relay-navigation/pipeline";
import { cn } from "@/lib/utils";

interface RelayCanonicalRunWorkbenchProps {
  runId: string;
  stage: WorkflowRunStage;
}

const STAGES: Array<{
  stage: WorkflowRunStage;
  label: string;
  icon: React.ComponentType<{ className?: string }>;
}> = [
  { stage: "specification", label: "Specification", icon: FileCode2 },
  { stage: "execute", label: "Execute", icon: Play },
  { stage: "audit", label: "Audit", icon: ClipboardCheck },
];

function runErrorMessage(error: unknown): string {
  if (error instanceof Error) return error.message;
  return "Run operation failed.";
}

function stageIndex(stage: WorkflowRunStage): number {
  return STAGES.findIndex((entry) => entry.stage === stage);
}

function attemptOutput(attempt: WorkflowExecutionAttempt | WorkflowExecutionAttemptSummary): string {
  if ("liveStdout" in attempt || "liveStderr" in attempt) {
    const stdout = "liveStdout" in attempt ? attempt.liveStdout : "";
    const stderr = "liveStderr" in attempt ? attempt.liveStderr : "";
    return [stdout, stderr].filter(Boolean).join("\n");
  }
  return "";
}

function StageNavigation({
  runId,
  selectedStage,
  availableThroughStage,
}: {
  runId: string;
  selectedStage: WorkflowRunStage;
  availableThroughStage: WorkflowRunStage;
}) {
  const availableThroughIndex = stageIndex(availableThroughStage);
  return (
    <nav
      aria-label="Run stages"
      className="grid grid-cols-3 gap-1 rounded border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)] p-1"
    >
      {STAGES.map((entry, index) => {
        const Icon = entry.icon;
        const current = entry.stage === selectedStage;
        const available = index <= availableThroughIndex;
        return (
          <Link
            key={entry.stage}
            to={workflowRunStageRoute(entry.stage)}
            params={{ runId }}
            aria-current={current ? "step" : undefined}
            aria-disabled={!available}
            tabIndex={available ? 0 : -1}
            onClick={(event) => {
              if (!available) event.preventDefault();
            }}
            className={cn(
              "flex min-w-0 items-center justify-center gap-2 rounded px-2 py-2 text-xs font-medium focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--relay-accent)]",
              current
                ? "bg-[var(--relay-panel-hover-bg)] text-foreground"
                : available
                  ? "text-muted-foreground hover:text-foreground"
                  : "cursor-not-allowed text-muted-foreground/40",
            )}
          >
            <Icon className="size-3.5 shrink-0" />
            <span className="truncate">{entry.label}</span>
          </Link>
        );
      })}
    </nav>
  );
}

function SpecificationPanel({ runId }: { runId: string }) {
  const query = useQuery(workflowSpecificationQueryOptions(runId));
  if (query.isLoading) {
    return <RelayStateSurface tone="loading" title="Loading Specification" description="Loading canonical Execution Spec and Executor Brief." />;
  }
  if (query.error || !query.data) {
    return (
      <RelayStateSurface
        tone="danger"
        title="Specification failed to load"
        description={runErrorMessage(query.error)}
        action={<Button type="button" variant="outline" size="sm" onClick={() => void query.refetch()}>Retry Specification</Button>}
      />
    );
  }
  const review = query.data;
  return (
    <div className="space-y-4">
      <section className="rounded border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)] p-4">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div>
            <h2 className="text-sm font-semibold">Canonical execution inputs</h2>
            <p className="mt-1 text-xs text-muted-foreground">
              Review immutable Execution Spec and derived Executor Brief artifacts before execution.
            </p>
          </div>
          <Badge variant="outline">{review.run.status}</Badge>
        </div>
        <dl className="mt-4 grid gap-3 text-xs sm:grid-cols-2">
          <div><dt className="text-muted-foreground">Execution Spec</dt><dd className="mt-1 break-all font-mono">{review.executionSpec.sha256}</dd></div>
          <div><dt className="text-muted-foreground">Executor Brief</dt><dd className="mt-1 break-all font-mono">{review.executorBrief.sha256}</dd></div>
          {review.plan ? <div><dt className="text-muted-foreground">Plan</dt><dd className="mt-1 font-mono">{review.plan.planId}</dd></div> : null}
          {review.pass ? <div><dt className="text-muted-foreground">Pass</dt><dd className="mt-1">{review.pass.number}. {review.pass.name}</dd></div> : null}
          {review.remediatesRunId ? <div><dt className="text-muted-foreground">Remediates Run</dt><dd className="mt-1 font-mono">{review.remediatesRunId}</dd></div> : null}
        </dl>
      </section>
      <div className="grid gap-3 sm:grid-cols-2">
        {[review.executionSpec, review.executorBrief].map((artifact) => (
          <a
            key={artifact.artifactId}
            href={workflowApiUrl(artifact.contentUrl)}
            className="rounded border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)] p-4 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--relay-accent)]"
          >
            <p className="text-sm font-medium">{artifact.kind}</p>
            <p className="mt-1 break-all font-mono text-[10px] text-muted-foreground">{artifact.artifactId}</p>
            <p className="mt-3 text-xs text-muted-foreground">Open canonical artifact</p>
          </a>
        ))}
      </div>
    </div>
  );
}

export const ACTIVE_ATTEMPT_REFRESH_MS = 2_000;

function ExecutePanel({
  runId,
  runStatus,
  attempts,
}: {
  runId: string;
  runStatus: WorkflowRunStatus;
  attempts: WorkflowExecutionAttemptSummary[];
}) {
  const queryClient = useQueryClient();
  const [adapter, setAdapter] = React.useState(
    EXECUTOR_ADAPTER_OPTIONS[0]?.value ?? "opencode_go",
  );
  const [model, setModel] = React.useState(getDefaultModelForAdapter(adapter));
  const [selectedAttemptId, setSelectedAttemptId] = React.useState<string | null>(
    attempts[0]?.attemptId ?? null,
  );
  const [error, setError] = React.useState<string | null>(null);
  const modelOptions = getModelOptionsForAdapter(adapter, model);
  const selectedSummary =
    attempts.find((attempt) => attempt.attemptId === selectedAttemptId) ?? null;

  React.useEffect(() => {
    setModel(getDefaultModelForAdapter(adapter));
  }, [adapter]);

  React.useEffect(() => {
    if (!selectedAttemptId && attempts[0]) {
      setSelectedAttemptId(attempts[0].attemptId);
    }
  }, [attempts, selectedAttemptId]);

  const cachedAttempt = queryClient.getQueryData<WorkflowExecutionAttempt>(
    workflowRunKeys.attempt(runId, selectedAttemptId ?? ""),
  );
  const isCachedTerminal =
    cachedAttempt && isTerminalWorkflowAttemptStatus(cachedAttempt.status);

  const attemptQuery = useQuery({
    ...workflowAttemptQueryOptions(runId, selectedAttemptId ?? ""),
    enabled: selectedAttemptId !== null && !isCachedTerminal,
    refetchInterval: (query) => {
      const detailed = query.state.data as WorkflowExecutionAttempt | undefined;
      const status = detailed?.status ?? selectedSummary?.status;
      return isNonterminalWorkflowAttemptStatus(status)
        ? ACTIVE_ATTEMPT_REFRESH_MS
        : false;
    },
    refetchIntervalInBackground: true,
  });

  const refreshRun = React.useCallback(() => {
    void queryClient.invalidateQueries({
      queryKey: workflowRunKeys.detail(runId),
    });
  }, [queryClient, runId]);

  const attemptStatus = attemptQuery.data?.status;
  const [lastStatus, setLastStatus] = React.useState<WorkflowExecutionAttemptStatus | undefined>(undefined);

  React.useEffect(() => {
    if (attemptStatus) {
      if (
        lastStatus &&
        isNonterminalWorkflowAttemptStatus(lastStatus) &&
        !isNonterminalWorkflowAttemptStatus(attemptStatus)
      ) {
        refreshRun();
      }
      setLastStatus(attemptStatus);
    }
  }, [attemptStatus, lastStatus, refreshRun]);

  const retainDetailedAttempt = React.useCallback(
    (attempt: WorkflowExecutionAttempt) => {
      setSelectedAttemptId(attempt.attemptId);
      queryClient.setQueryData(
        workflowRunKeys.attempt(runId, attempt.attemptId),
        attempt,
      );
      setError(null);
      refreshRun();
    },
    [queryClient, refreshRun, runId],
  );

  const startMutation = useMutation({
    mutationFn: () => startWorkflowAttempt(runId, adapter, model),
    onSuccess: retainDetailedAttempt,
    onError: (value) => setError(runErrorMessage(value)),
  });
  const cancelMutation = useMutation({
    mutationFn: (attemptId: string) =>
      cancelWorkflowAttempt(runId, attemptId),
    onSuccess: retainDetailedAttempt,
    onError: (value) => setError(runErrorMessage(value)),
  });
  const reconcileMutation = useMutation({
    mutationFn: (attemptId: string) =>
      reconcileWorkflowAttempt(runId, attemptId),
    onSuccess: retainDetailedAttempt,
    onError: (value) => setError(runErrorMessage(value)),
  });

  const selectedAttempt = attemptQuery.data ?? selectedSummary;
  const output = attemptQuery.data ? attemptOutput(attemptQuery.data) : "";
  const selectedAttemptIsNonterminal = isNonterminalWorkflowAttemptStatus(
    selectedAttempt?.status,
  );
  const { canStart, canCancel, canReconcile } =
    deriveWorkflowAttemptControlState(
      runStatus,
      attempts,
      selectedAttempt,
    );
  const pending =
    startMutation.isPending ||
    cancelMutation.isPending ||
    reconcileMutation.isPending;

  return (
    <div
      className="grid grid-cols-1 gap-4 lg:grid-cols-[20rem_minmax(0,1fr)]"
      data-testid="execute-responsive-grid"
    >
      <section className="space-y-4 rounded border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)] p-4">
        <div>
          <h2 className="text-sm font-semibold">Execution attempt</h2>
          <p className="mt-1 text-xs text-muted-foreground">
            Adapter and model are selected only when starting an immutable attempt.
          </p>
        </div>
        {error ? (
          <div
            role="alert"
            className="rounded border border-destructive/30 bg-destructive/10 p-3 text-xs text-destructive"
          >
            {error}
          </div>
        ) : null}
        <div className="space-y-2">
          <Label htmlFor="workflow-adapter">Executor adapter</Label>
          <Select value={adapter} onValueChange={(val) => setAdapter(val as any)} disabled={pending || !canStart}>
            <SelectTrigger id="workflow-adapter" aria-label="Executor adapter">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {EXECUTOR_ADAPTER_OPTIONS.map((option) => (
                <SelectItem key={option.value} value={option.value}>
                  {option.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
        <div className="space-y-2">
          <Label htmlFor="workflow-model">Model</Label>
          <Select value={model} onValueChange={(val) => setModel(val as any)} disabled={pending || !canStart}>
            <SelectTrigger id="workflow-model" aria-label="Model">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {modelOptions.map((option) => (
                <SelectItem key={option.value} value={option.value}>
                  {option.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
        <Button
          type="button"
          className="w-full"
          disabled={pending || !canStart}
          onClick={() => startMutation.mutate()}
        >
          {startMutation.isPending ? (
            <Loader2 className="size-4 animate-spin" />
          ) : (
            <Play className="size-4" />
          )}
          Start attempt
        </Button>
        {selectedAttempt ? (
          <div className={cn("grid gap-2", canReconcile ? "grid-cols-2" : "grid-cols-1")}>
            <Button
              type="button"
              variant="outline"
              disabled={pending || !canCancel}
              onClick={() => cancelMutation.mutate(selectedAttempt.attemptId)}
            >
              <Square className="size-3.5" /> Cancel
            </Button>
            {canReconcile ? (
              <Button
                type="button"
                variant="outline"
                disabled={pending}
                onClick={() => reconcileMutation.mutate(selectedAttempt.attemptId)}
              >
                <RotateCcw className="size-3.5" /> Reconcile cleanup
              </Button>
            ) : null}
          </div>
        ) : null}
        {canReconcile ? (
          <p role="status" className="text-xs text-muted-foreground">
            Durable process cleanup is pending. Reconcile the owned process before retrying.
          </p>
        ) : null}
      </section>
      <section className="min-w-0 rounded border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)] p-4">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div>
            <h2 className="text-sm font-semibold">Attempt output</h2>
            <p className="mt-1 text-xs text-muted-foreground">
              {selectedAttempt
                ? `Attempt ${selectedAttempt.attemptNumber}`
                : "No attempt selected"}
            </p>
          </div>
          {selectedAttempt ? (
            <Badge variant="outline">{selectedAttempt.status}</Badge>
          ) : null}
        </div>
        {attemptQuery.error ? (
          <div
            role="alert"
            className="mt-4 rounded border border-destructive/30 bg-destructive/10 p-3 text-xs text-destructive"
          >
            <p>Attempt detail failed to load.</p>
            <Button
              type="button"
              variant="outline"
              size="sm"
              className="mt-2"
              onClick={() => void attemptQuery.refetch()}
            >
              Retry attempt detail
            </Button>
          </div>
        ) : null}
        <pre
          aria-label="Attempt output"
          aria-live="polite"
          className="mt-4 min-h-64 max-h-[32rem] overflow-auto whitespace-pre-wrap rounded border border-[var(--relay-row-border)] bg-[var(--relay-code-bg)] p-3 font-mono text-xs"
        >
          {attemptQuery.isLoading
            ? "Loading detailed attempt output."
            : output || "No captured output."}
        </pre>
        {attemptQuery.isFetching && selectedAttemptIsNonterminal ? (
          <p role="status" className="mt-2 text-[10px] text-muted-foreground">
            Refreshing active attempt output.
          </p>
        ) : null}
        {selectedAttempt && selectedAttempt.artifacts.length > 0 ? (
          <div className="mt-4 space-y-2">
            <h3 className="text-xs font-semibold">Attempt artifacts</h3>
            {selectedAttempt.artifacts.map((artifact) => (
              <div
                key={artifact.artifactId}
                className="rounded border border-[var(--relay-row-border)] p-2 text-xs"
              >
                <p>{artifact.kind}</p>
                <p className="mt-1 break-all font-mono text-[10px] text-muted-foreground">
                  {artifact.sha256}
                </p>
              </div>
            ))}
          </div>
        ) : null}
      </section>
    </div>
  );
}

function AuditPanel({
  runId,
  baseCommit,
}: {
  runId: string;
  baseCommit: string;
}) {
  const queryClient = useQueryClient();
  const statusQuery = useQuery(workflowAuditStatusQueryOptions(runId));
  const [auditedCommit, setAuditedCommit] = React.useState(baseCommit);
  const [error, setError] = React.useState<string | null>(null);
  const mutation = useMutation({
    mutationFn: () => prepareWorkflowAudit(runId, auditedCommit.trim()),
    onSuccess: () => {
      setError(null);
      void queryClient.invalidateQueries({ queryKey: workflowRunKeys.audit(runId) });
      void queryClient.invalidateQueries({ queryKey: workflowRunKeys.detail(runId) });
    },
    onError: (value) => setError(runErrorMessage(value)),
  });

  if (statusQuery.isLoading) {
    return <RelayStateSurface tone="loading" title="Loading audit status" description="Loading current packet and recorded decision metadata." />;
  }
  if (statusQuery.error || !statusQuery.data) {
    return (
      <RelayStateSurface
        tone="danger"
        title="Audit status failed to load"
        description={runErrorMessage(statusQuery.error)}
        action={<Button type="button" variant="outline" size="sm" onClick={() => void statusQuery.refetch()}>Retry Audit status</Button>}
      />
    );
  }
  const status = statusQuery.data;
  return (
    <div className="grid grid-cols-1 gap-4 lg:grid-cols-[20rem_minmax(0,1fr)]" data-testid="audit-responsive-grid">
      <section className="space-y-4 rounded border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)] p-4">
        <div>
          <h2 className="text-sm font-semibold">Prepare audit packet</h2>
          <p className="mt-1 text-xs text-muted-foreground">
            Browser audit is packet preparation and readback only. Final decisions remain with the Auditor tool.
          </p>
        </div>
        {error ? <div role="alert" className="rounded border border-destructive/30 bg-destructive/10 p-3 text-xs text-destructive">{error}</div> : null}
        <div className="space-y-2">
          <Label htmlFor="audited-commit">Audited commit</Label>
          <Input id="audited-commit" value={auditedCommit} onChange={(event) => setAuditedCommit(event.target.value)} />
        </div>
        <Button
          type="button"
          className="w-full"
          disabled={mutation.isPending || auditedCommit.trim().length !== 40}
          onClick={() => mutation.mutate()}
        >
          {mutation.isPending ? (
            <Loader2 className="size-4 animate-spin" />
          ) : (
            <ClipboardCheck className="size-4" />
          )}
          Prepare audit packet
        </Button>
      </section>
      <section
        className="space-y-4 rounded border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)] p-4"
        aria-live="polite"
      >
        <div>
          <h2 className="text-sm font-semibold">Audit state</h2>
          <p className="mt-1 text-xs text-muted-foreground">Current immutable packet and recorded decision.</p>
        </div>
        {status.currentPacket ? (
          <div className="rounded border border-[var(--relay-row-border)] p-3">
            <div className="flex items-center gap-2">
              <CheckCircle2 className="size-4 text-success" />
              <span className="text-sm font-medium">Current packet</span>
            </div>
            <p className="mt-2 break-all font-mono text-[10px]">{status.currentPacket.packetSha256}</p>
            <p className="mt-1 text-xs text-muted-foreground">{status.currentPacket.auditedCommit}</p>
          </div>
        ) : (
          <div className="rounded border border-dashed border-[var(--relay-row-border)] p-3 text-xs text-muted-foreground">
            No current audit packet.
          </div>
        )}
        {status.decision ? (
          <div className="rounded border border-success/30 bg-success/10 p-3">
            <p className="text-sm font-medium">{status.decision.decision}</p>
            <p className="mt-1 text-xs">{status.decision.rationale}</p>
          </div>
        ) : (
          <div className="flex items-start gap-2 rounded border border-info/30 bg-info/10 p-3 text-xs">
            <AlertTriangle className="mt-0.5 size-4 shrink-0" />
            <span>No decision is recorded. Use the canonical Auditor tool after reviewing the packet.</span>
          </div>
        )}
      </section>
    </div>
  );
}

export function RelayCanonicalRunWorkbench({
  runId,
  stage,
}: RelayCanonicalRunWorkbenchProps) {
  const query = useQuery(workflowRunDetailQueryOptions(runId));
  if (query.isLoading) {
    return <RelayStateSurface tone="loading" title="Loading Run" description="Loading canonical Run state." />;
  }
  if (query.error || !query.data) {
    return (
      <RelayStateSurface
        tone="danger"
        title="Run failed to load"
        description={runErrorMessage(query.error)}
        action={<Button type="button" variant="outline" size="sm" onClick={() => void query.refetch()}>Retry Run</Button>}
      />
    );
  }
  const detail = query.data;
  const run = detail.run;

  const availableThroughStage =
    resolveWorkflowAvailableThroughStage(run.status, run.stage) ?? run.stage;
  if (stageIndex(stage) > stageIndex(availableThroughStage)) {
    return (
      <Navigate
        to={workflowRunStageRoute(availableThroughStage)}
        params={{ runId }}
        replace
      />
    );
  }

  return (
    <section
      className="min-h-0 flex-1 overflow-y-auto bg-[var(--relay-page-body-bg)]"
      data-testid="run-workbench-frame"
    >
      <div className="mx-auto flex w-full max-w-7xl flex-col gap-4 px-4 py-4 sm:px-6 sm:py-5">
        <div className="flex flex-col gap-3 border-b border-[var(--relay-row-border)] pb-4 sm:flex-row sm:items-start sm:justify-between">
          <div className="min-w-0">
            <Link
              to="/runs"
              className="inline-flex items-center gap-1 rounded text-xs text-muted-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--relay-accent)]"
            >
              <ArrowLeft className="size-3.5" /> Back to Runs
            </Link>
            <h1 className="mt-2 truncate text-lg font-semibold">{run.featureSlug}</h1>
            <p className="mt-1 break-all font-mono text-[10px] text-muted-foreground">{run.runId}</p>
          </div>
          <Badge variant="outline">{run.status}</Badge>
        </div>
        <StageNavigation
          runId={runId}
          selectedStage={stage}
          availableThroughStage={availableThroughStage}
        />
        {stage === "specification" ? <SpecificationPanel runId={runId} /> : null}
        {stage === "execute" ? (
          <ExecutePanel
            runId={runId}
            runStatus={run.status}
            attempts={detail.attempts}
          />
        ) : null}
        {stage === "audit" ? <AuditPanel runId={runId} baseCommit={run.baseCommit} /> : null}
      </div>
    </section>
  );
}
