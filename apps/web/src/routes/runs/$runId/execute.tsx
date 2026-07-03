import { createFileRoute } from "@tanstack/react-router";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import {
  executeActiveRunDetailQueryOptions,
  executeActiveRunArtifactsQueryOptions,
  executeActiveRunEventsQueryOptions,
} from "@/features/relay-runs";
import type {
  RelayArtifact,
  RelayExecutorPhase,
  RelayRunEvent,
} from "@/features/relay-runs";
import { RunStatusTrackerLayout } from "@/components/relay/RunStatusTrackerLayout";
import {
  RunWorkbenchLoadFailedState,
  RunWorkbenchLoadingState,
} from "@/components/relay/RunWorkbenchStates";
import { ValidationPanel } from "@/components/relay/ValidationPanel";
import { LogPreviewPanel } from "@/components/relay/LogPreviewPanel";
import { RunEvidenceBrowser } from "@/components/relay/RunEvidenceBrowser";
import { Clock } from "lucide-react";
import { getExecuteDisplayState } from "./runExecuteVisualState";
import { deriveCurrentStatusText } from "@/features/relay-runs/deriveCurrentStatusText";
import { deriveProgressionLog } from "@/features/relay-runs/deriveProgressionLog";
import { deriveExecuteActions } from "@/features/relay-runs/runStepActions";
import { resolvePlanPassLink } from "@/features/relay-runs/planPassLink";
import { EXECUTE_ACTION_HANDLERS } from "@/features/relay-runs/runStepActionHandlers";
import type { DetailSection } from "@/features/relay-runs/runStatusTrackerViews";

export const Route = createFileRoute("/runs/$runId/execute")({
  component: ExecutePage,
});

function ExecutePage() {
  const { runId } = Route.useParams();
  const queryClient = useQueryClient();
  const [pendingActionId, setPendingActionId] = useState<string | null>(null);
  const {
    data: run,
    isLoading: isLoadingRun,
    error: errorRun,
  } = useQuery(executeActiveRunDetailQueryOptions(runId));
  const { data: artifacts, isLoading: isLoadingArtifacts } = useQuery(
    executeActiveRunArtifactsQueryOptions(runId, run?.status),
  );
  const {
    data: events,
    isLoading: isLoadingEvents,
    error: errorEvents,
  } = useQuery(executeActiveRunEventsQueryOptions(runId, run?.status));

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

  // ------------------------------------------------------------
  // Run Status Tracker Redesign — the Visual_State_Module display-state
  // derivation stays here (single source of truth, unchanged) and now
  // feeds `deriveCurrentStatusText` instead of a state card + badge +
  // pipeline chips trio. `RunStatusTrackerLayout` owns rendering the five
  // tracker regions; nothing below duplicates status, position, or action
  // buttons.
  // ------------------------------------------------------------
  const runStatus = (run.status || "") as string;
  const runLifecycle = (run.lifecycleState || "") as string;
  const executorPhase = deriveExecutorPhase(runStatus, runLifecycle);

  const resultArtifacts = resolvedArtifacts.filter(isResultArtifact);
  const diffArtifacts = resolvedArtifacts.filter(isDiffArtifact);
  const validationArtifacts = resolvedArtifacts.filter(isValidationArtifact);

  const preflightResultArt = resultArtifacts.find(
    (a: any) =>
      isExecutorResultArtifact(a) &&
      artifactPreviewHas(a, "executor preflight failed"),
  );
  const preflightCommandLogArt = resultArtifacts.find(
    (a: any) =>
      isCommandLogArtifact(a) && artifactPreviewHas(a, "Preflight: BLOCKED"),
  );
  const preflightBlocked =
    (executorPhase === "blocked" || executorPhase === "failed") &&
    Boolean(preflightResultArt || preflightCommandLogArt);

  const actionAvailability = {
    canStart: runStatus === "approved_for_executor",
    canCancel:
      runStatus === "executor_dispatched" || runStatus === "executor_running",
    canRecover: false,
    startUnavailableReason:
      runStatus !== "approved_for_executor"
        ? executorPhase === "blocked" || executorPhase === "failed"
          ? `Start is unavailable while blocked (status: ${runStatus})`
          : `Current status: ${runStatus}`
        : undefined,
    cancelUnavailableReason:
      "Cancellation is not yet implemented in the backend.",
    recoverUnavailableReason:
      "Recovery is not yet implemented in the backend.",
  };

  const executeDisplayState = getExecuteDisplayState({
    run,
    executorPhase,
    preflightBlocked,
    hasResultArtifacts: resultArtifacts.length > 0,
    hasDiffArtifacts: diffArtifacts.length > 0,
    hasValidationArtifacts: validationArtifacts.length > 0,
  });

  const currentStatus = deriveCurrentStatusText("execute", executeDisplayState, {
    updatedAt: run.updatedAt,
  });
  const progression = deriveProgressionLog(resolvedEvents);

  const baseStepActionsView = deriveExecuteActions(actionAvailability);
  // `RunStepActionBar` has no built-in pending state; force-disable the
  // in-flight control locally rather than expanding that shared component.
  const stepActionsView = pendingActionId
    ? {
        ...baseStepActionsView,
        controls: baseStepActionsView.controls.map((control) =>
          control.id === pendingActionId
            ? { ...control, enabled: false }
            : control,
        ),
      }
    : baseStepActionsView;
  // Invoke the id->handler map directly and return the resulting Promise so
  // `RunStatusTrackerLayout`'s own action-failure escalation (Requirement
  // 6.6) can catch a rejection and escalate `currentStatus.tone` to
  // "danger" — no separate `mutationError`/banner state is needed here.
  // Cache invalidation still runs on both the success and failure paths.
  const onActionClick = (id: string) => {
    const handler = EXECUTE_ACTION_HANDLERS[id];
    if (!handler) return;
    setPendingActionId(id);
    return handler(run.id)
      .then(() => {
        void queryClient.invalidateQueries({ queryKey: ["relay-runs"] });
      })
      .catch((err: any) => {
        void queryClient.invalidateQueries({ queryKey: ["relay-runs"] });
        throw err;
      })
      .finally(() => setPendingActionId(null));
  };
  const planPassLinkView = resolvePlanPassLink(run.planContext);

  const detailSections: DetailSection[] = [
    {
      key: "logs",
      label: "Full logs",
      render: () => <LogPreviewPanel logPreview={logPreview} />,
    },
    {
      key: "result",
      label: "Executor result",
      render: () => (
        <ExecuteResultDetail
          resultArtifacts={resultArtifacts}
          executorPhase={executorPhase}
        />
      ),
    },
    {
      key: "diff",
      label: "Changed files",
      render: () => (
        <RunEvidenceBrowser
          runId={run.id}
          artifacts={diffArtifacts}
          events={resolvedEvents}
        />
      ),
    },
    {
      key: "validation",
      label: "Validation report",
      render: () => <ValidationPanel summary={run.validationSummary} />,
    },
  ];

  return (
    <RunStatusTrackerLayout
      run={run}
      currentStep="execute"
      currentStatus={currentStatus}
      actionsView={stepActionsView}
      onActionClick={onActionClick}
      progression={progression}
      eventsLoadFailed={Boolean(errorEvents)}
      detailSections={detailSections}
      planPassLinkView={planPassLinkView}
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
  return [a.storageKind, a.kind, a.filename, a.label]
    .filter(Boolean)
    .join(" ")
    .toLowerCase();
}

function artifactHas(a: any, ...tokens: string[]): boolean {
  const id = artifactIdentity(a);
  return tokens.some((token) => id.includes(token.toLowerCase()));
}

function artifactPreviewHas(a: any, token: string): boolean {
  return String(a.preview || "")
    .toLowerCase()
    .includes(token.toLowerCase());
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

function findPrimaryResultArtifact(
  resultArtifacts: RelayArtifact[],
): RelayArtifact | undefined {
  return (
    resultArtifacts.find(
      (a) =>
        a.filename?.includes("executor_result") ||
        a.label?.includes("Executor Result"),
    ) ||
    resultArtifacts.find(
      (a) =>
        a.filename?.includes("agent_result_raw") ||
        a.label?.includes("Agent Result"),
    ) ||
    resultArtifacts.find(
      (a) =>
        a.filename?.includes("executor_stdout") ||
        a.label?.includes("Executor Stdout"),
    ) ||
    resultArtifacts[0]
  );
}

// Detail_Disclosure "Executor result" section (Requirement 5.6) — the full
// executor result output. Only invoked once the Operator opens this
// section (lazy `DetailSection.render()`).
function ExecuteResultDetail({
  resultArtifacts,
  executorPhase,
}: {
  resultArtifacts: RelayArtifact[];
  executorPhase: RelayExecutorPhase;
}) {
  const primaryResultArt = findPrimaryResultArtifact(resultArtifacts);

  if (!primaryResultArt) {
    return (
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
    );
  }

  if (!primaryResultArt.preview) {
    return (
      <div className="text-xs bg-[var(--surface-inset)]/30 border border-dashed rounded p-3 text-muted-foreground">
        <span className="italic">
          Result content preview not available for this artifact.
        </span>
      </div>
    );
  }

  return (
    <pre className="max-w-full overflow-x-auto text-[11px] font-mono bg-[var(--relay-code-bg)] p-2.5 rounded border border-[var(--relay-row-border)] max-h-64 overflow-y-auto whitespace-pre-wrap text-foreground">
      {primaryResultArt.preview}
    </pre>
  );
}

export function isExecuteLiveStatus(status?: string): boolean {
  return (
    status === "executor_dispatched" ||
    status === "executor_running" ||
    status === "local_validation_running"
  );
}

export function formatExecutorPacket(raw: string): string[] {
  const trimmed = raw.trim();
  if (!trimmed) return [];

  let parsed: any;
  try {
    parsed = JSON.parse(trimmed);
  } catch {
    const line = trimmed.length > 200 ? `${trimmed.slice(0, 200)}…` : trimmed;
    return [line];
  }

  if (!parsed || typeof parsed !== "object") {
    return [String(trimmed).slice(0, 200)];
  }

  const lines: string[] = [];

  const packetType = parsed.type || "packet";
  const part = parsed.part || parsed;
  const tool = part.tool || part.type || packetType;
  const state = part.state || parsed.state || {};
  const status = state.status || "";
  const input = state.input || {};
  const output = state.output;

  let target = "";
  if (input.filePath) {
    target = String(input.filePath).replace(/\\/g, "/");
    const repoMarker = "/relay/";
    const idx = target.lastIndexOf(repoMarker);
    if (idx >= 0) {
      target = target.slice(idx + repoMarker.length);
    }
  } else if (input.command) {
    target = String(input.command);
  }

  const displayType =
    tool === part.type || part.type === "tool" ? "tool" : packetType;
  let summary = `${displayType} ${tool}`;
  if (status) {
    summary += ` ${status}`;
  }
  if (target) {
    summary += ` — ${target}`;
  }
  lines.push(summary);

  if (output !== undefined && output !== null) {
    let outStr = "";
    if (typeof output === "string") {
      outStr = output;
    } else {
      try {
        outStr = JSON.stringify(output);
      } catch {
        outStr = String(output);
      }
    }
    if (outStr) {
      const preview = outStr.length > 80 ? `${outStr.slice(0, 80)}…` : outStr;
      lines.push(`  → ${preview}`);
    }
  }

  return lines;
}

export function deriveLiveExecutorProgress(
  events: RelayRunEvent[],
  _artifacts: RelayArtifact[],
): string[] {
  const rows: { at: number; line: string }[] = [];

  for (const e of events || []) {
    const time = e.createdAt ? new Date(e.createdAt).getTime() : 0;
    const timeStr = e.createdAt
      ? new Date(e.createdAt).toLocaleTimeString("en-US", {
          hour12: false,
          hour: "2-digit",
          minute: "2-digit",
          second: "2-digit",
        })
      : "--:--:--";
    const kind = e.kind || "log";
    rows.push({
      at: time,
      line: `[${timeStr}] [${kind}] ${e.message || ""}`,
    });
  }

  rows.sort((a, b) => a.at - b.at);
  return rows.slice(-100).map((r) => r.line);
}
