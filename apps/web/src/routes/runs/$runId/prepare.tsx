import * as React from "react";
import { createFileRoute, Link } from "@tanstack/react-router";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  approveBrief,
  evaluateRepairEligibility,
  prepareRun,
  RelayApiError,
  renderBrief,
  repairValidation,
  runArtifactsQueryOptions,
  runDetailQueryOptions,
  runEventsQueryOptions,
} from "@/features/relay-runs";
import type {
  RelayArtifact,
  RelayRun,
  RepairValidationResponse,
} from "@/features/relay-runs";
import { ArtifactInspectorDialog } from "@/components/relay/ArtifactInspectorDialog";
import { LogPreviewPanel } from "@/components/relay/LogPreviewPanel";
import {
  RelayInlineState,
  RelayStateBanner,
} from "@/components/relay/RelayStateSurface";
import { RunEvidenceBrowser } from "@/components/relay/RunEvidenceBrowser";
import { RunWorkbenchLayout } from "@/components/relay/RunWorkbenchLayout";
import {
  RunWorkbenchLoadFailedState,
  RunWorkbenchLoadingState,
} from "@/components/relay/RunWorkbenchStates";
import { ValidationPanel } from "@/components/relay/ValidationPanel";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import {
  ArrowRight,
  CheckCircle2,
  FileText,
  Loader2,
  Play,
  RefreshCw,
  ShieldCheck,
  Wrench,
} from "lucide-react";

type PacketValidationIssue = {
  code?: string;
  message?: string;
  repair_eligible?: boolean;
  RepairEligible?: boolean;
};

type PacketValidationReport = {
  valid?: boolean;
  repair_eligible?: boolean;
  RepairEligible?: boolean;
  errors?: PacketValidationIssue[];
};

type BriefValidationIssue = {
  severity?: string;
  message?: string;
};

type BriefValidationReport = {
  status?: string;
  issues?: BriefValidationIssue[];
};

type CompileRenderController = ReturnType<typeof useCompileRenderController>;

export const Route = createFileRoute("/runs/$runId/prepare")({
  component: PreparePage,
});

function PreparePage() {
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

  const resolvedArtifacts = artifacts || [];
  const resolvedEvents = events || [];
  const controller = useCompileRenderController({
    run,
    artifacts: resolvedArtifacts,
  });

  const formattedLogs = resolvedEvents.map((event) => {
    const timeStr = new Date(event.createdAt).toLocaleTimeString("en-US", {
      hour12: false,
      hour: "2-digit",
      minute: "2-digit",
      second: "2-digit",
    });
    return `[${timeStr}] ${event.message}`;
  });

  const logPreview = {
    lines: formattedLogs.slice(-50),
    truncated: formattedLogs.length > 50,
  };

  return (
    <RunWorkbenchLayout
      run={{
        ...run,
        artifacts: resolvedArtifacts,
        latestEvents: resolvedEvents,
        logPreview,
      }}
      currentStep="prepare"
      stageActions={<CompileRenderStageActions controller={controller} />}
      mainContent={<CompileRenderMainContent controller={controller} />}
      initialInspectorTab="details"
      inspectorTabs={[
        { key: "details", label: "Details" },
        { key: "artifacts", label: "Artifacts" },
        { key: "validation", label: "Validation" },
        { key: "logs", label: "Logs" },
      ]}
      inspectorPanels={{
        details: <CompileRenderDetailsPanel controller={controller} />,
        artifacts: (
          <RunEvidenceBrowser
            runId={run.id}
            artifacts={resolvedArtifacts}
            events={resolvedEvents}
          />
        ),
        validation: <ValidationPanel summary={run.validationSummary} />,
        logs: <LogPreviewPanel logPreview={logPreview} />,
      }}
    />
  );
}

function useCompileRenderController({
  run,
  artifacts,
}: {
  run: RelayRun;
  artifacts: RelayArtifact[];
}) {
  const queryClient = useQueryClient();
  const [approvalNotes, setApprovalNotes] = React.useState("");
  const [mutationError, setMutationError] = React.useState<string | null>(null);
  const [showValidationInspector, setShowValidationInspector] =
    React.useState(false);
  const [repairResult, setRepairResult] =
    React.useState<RepairValidationResponse | null>(null);

  const canonicalPacketArt = findArtifact(artifacts, "canonical_packet.json");
  const packetValidationArt = findArtifact(
    artifacts,
    "packet_validation_report.json",
  );
  const executorBriefArt = findArtifact(artifacts, "executor_brief.md");
  const briefValidationArt = findArtifact(
    artifacts,
    "brief_validation_report.json",
  );

  const status = run.status;
  const isApprovedForPrepare = status === "approved_for_prepare";
  const isPacketValidationFailed = status === "packet_validation_failed";
  const isPacketValidated =
    status === "packet_validated" || status === "repair_validated";
  const isBriefReadyForReview = status === "brief_ready_for_review";
  const isApprovedForExecutor = status === "approved_for_executor";

  const canCompile = isApprovedForPrepare;
  const canRetryCompile = isPacketValidationFailed;
  const canRenderBrief = isPacketValidated;
  const canApproveBrief = isBriefReadyForReview;
  const compileAttempted = Boolean(canonicalPacketArt);

  const compileMutation = useMutation({
    mutationFn: () => prepareRun(run.id),
    onSuccess: () => {
      setMutationError(null);
      void queryClient.invalidateQueries({ queryKey: ["relay-runs"] });
    },
    onError: (error: unknown) => {
      void queryClient.invalidateQueries({ queryKey: ["relay-runs"] });

      if (error instanceof RelayApiError) {
        if (error.status === 422) {
          setMutationError(
            "Compile failed packet validation. Review Packet Validation Report below.",
          );
          return;
        }

        if (error.status === 409) {
          const currentStatus = error.errorShape?.currentStatus || run.status;
          setMutationError(
            `Compile cannot run from status "${currentStatus}". Return to the required step or refresh the run.`,
          );
          return;
        }

        setMutationError(error.message || "Compile failed.");
        return;
      }

      setMutationError(
        error instanceof Error ? error.message : "Compile failed.",
      );
    },
  });

  const renderBriefMutation = useMutation({
    mutationFn: () => renderBrief(run.id),
    onSuccess: () => {
      setMutationError(null);
      void queryClient.invalidateQueries({ queryKey: ["relay-runs"] });
    },
    onError: (error: unknown) => {
      setMutationError(
        error instanceof Error ? error.message : "Render brief failed.",
      );
    },
  });

  const approveMutation = useMutation({
    mutationFn: () =>
      approveBrief(run.id, {
        action: "approve",
        notes: approvalNotes.trim() || undefined,
      }),
    onSuccess: () => {
      setMutationError(null);
      setApprovalNotes("");
      void queryClient.invalidateQueries({ queryKey: ["relay-runs"] });
    },
    onError: (error: unknown) => {
      setMutationError(
        error instanceof Error ? error.message : "Failed to approve brief.",
      );
    },
  });

  const repairMutation = useMutation({
    mutationFn: () => repairValidation(run.id),
    onSuccess: (data) => {
      setMutationError(null);
      setRepairResult(data);
      void queryClient.invalidateQueries({ queryKey: ["relay-runs"] });
    },
    onError: (error: unknown) => {
      void queryClient.invalidateQueries({ queryKey: ["relay-runs"] });

      if (error instanceof RelayApiError) {
        const shape = error.errorShape;
        if (shape?.error || shape?.message) {
          setMutationError(shape.error || shape.message);
          return;
        }

        setMutationError(error.message || "Repair failed.");
        return;
      }

      setMutationError(
        error instanceof Error ? error.message : "Repair failed.",
      );
    },
  });

  const packetValidationReport = parseArtifactPreview<PacketValidationReport>(
    packetValidationArt,
  );
  const briefValidationReport = parseArtifactPreview<BriefValidationReport>(
    briefValidationArt,
  );

  const repairEligibility = evaluateRepairEligibility(packetValidationReport);
  const repairEligible = repairEligibility.canOfferRepair;

  const isPending =
    compileMutation.isPending ||
    renderBriefMutation.isPending ||
    approveMutation.isPending ||
    repairMutation.isPending;

  const handleCompile = () => {
    setMutationError(null);
    compileMutation.mutate();
  };

  const handleRetryCompile = () => {
    setMutationError(null);
    compileMutation.mutate();
  };

  const handleRenderBrief = () => {
    setMutationError(null);
    renderBriefMutation.mutate();
  };

  const handleAttemptRepair = () => {
    setMutationError(null);
    setRepairResult(null);
    repairMutation.mutate();
  };

  const handleApproveBrief = () => {
    setMutationError(null);
    approveMutation.mutate();
  };

  return {
    run,
    artifacts,
    approvalNotes,
    setApprovalNotes,
    mutationError,
    setMutationError,
    showValidationInspector,
    setShowValidationInspector,
    canonicalPacketArt,
    packetValidationArt,
    executorBriefArt,
    briefValidationArt,
    packetValidationReport,
    briefValidationReport,
    repairEligibility,
    repairEligible,
    repairResult,
    status,
    isApprovedForPrepare,
    isPacketValidationFailed,
    isPacketValidated,
    isBriefReadyForReview,
    isApprovedForExecutor,
    canCompile,
    canRetryCompile,
    canRenderBrief,
    canApproveBrief,
    compileAttempted,
    isPending,
    compileMutation,
    renderBriefMutation,
    approveMutation,
    repairMutation,
    handleCompile,
    handleRetryCompile,
    handleRenderBrief,
    handleAttemptRepair,
    handleApproveBrief,
  };
}

function CompileRenderStageActions({
  controller,
}: {
  controller: CompileRenderController;
}) {
  const {
    run,
    canCompile,
    canRetryCompile,
    canApproveBrief,
    executorBriefArt,
    isApprovedForExecutor,
    isPacketValidated,
    isPacketValidationFailed,
    isPending,
    repairEligible,
    repairResult,
    compileMutation,
    renderBriefMutation,
    approveMutation,
    repairMutation,
    handleCompile,
    handleRetryCompile,
    handleRenderBrief,
    handleAttemptRepair,
    handleApproveBrief,
  } = controller;

  if (
    !canCompile &&
    !canRetryCompile &&
    !isPacketValidated &&
    !canApproveBrief &&
    !isApprovedForExecutor &&
    !(isPacketValidationFailed && repairEligible && repairResult === null)
  ) {
    return null;
  }

  return (
    <div className="flex flex-wrap justify-end gap-2 py-2">
      {canCompile ? (
        <Button size="sm" onClick={handleCompile} disabled={isPending}>
          {compileMutation.isPending ? (
            <Loader2 className="mr-1.5 h-3.5 w-3.5 animate-spin" />
          ) : (
            <Play className="mr-1.5 h-3.5 w-3.5" />
          )}
          Run Compile
        </Button>
      ) : null}

      {canRetryCompile ? (
        <Button
          variant="outline"
          size="sm"
          onClick={handleRetryCompile}
          disabled={isPending}
        >
          {compileMutation.isPending ? (
            <Loader2 className="mr-1.5 h-3.5 w-3.5 animate-spin" />
          ) : (
            <RefreshCw className="mr-1.5 h-3.5 w-3.5" />
          )}
          Retry Compile
        </Button>
      ) : null}

      {isPacketValidationFailed &&
      repairEligible &&
      repairResult === null ? (
        <Button
          size="sm"
          onClick={handleAttemptRepair}
          disabled={isPending}
        >
          {repairMutation.isPending ? (
            <Loader2 className="mr-1.5 h-3.5 w-3.5 animate-spin" />
          ) : (
            <Wrench className="mr-1.5 h-3.5 w-3.5" />
          )}
          Attempt Repair
        </Button>
      ) : null}

      {isPacketValidated ? (
        <Button
          size="sm"
          onClick={handleRenderBrief}
          disabled={isPending}
        >
          {renderBriefMutation.isPending ? (
            <Loader2 className="mr-1.5 h-3.5 w-3.5 animate-spin" />
          ) : executorBriefArt ? (
            <RefreshCw className="mr-1.5 h-3.5 w-3.5" />
          ) : (
            <FileText className="mr-1.5 h-3.5 w-3.5" />
          )}
          {executorBriefArt ? "Re-render Executor Brief" : "Render Executor Brief"}
        </Button>
      ) : null}

      {canApproveBrief ? (
        <Button size="sm" onClick={handleApproveBrief} disabled={isPending}>
          {approveMutation.isPending ? (
            <Loader2 className="mr-1.5 h-3.5 w-3.5 animate-spin" />
          ) : (
            <ShieldCheck className="mr-1.5 h-3.5 w-3.5" />
          )}
          Approve for Executor
        </Button>
      ) : null}

      {isApprovedForExecutor ? (
        <Button size="sm" asChild>
          <Link to="/runs/$runId/execute" params={{ runId: run.id }}>
            Proceed to Execute
            <ArrowRight className="ml-1.5 h-3.5 w-3.5" />
          </Link>
        </Button>
      ) : null}
    </div>
  );
}

function CompileRenderMainContent({
  controller,
}: {
  controller: CompileRenderController;
}) {
  const {
    run,
    approvalNotes,
    setApprovalNotes,
    mutationError,
    showValidationInspector,
    setShowValidationInspector,
    canonicalPacketArt,
    packetValidationArt,
    executorBriefArt,
    briefValidationArt,
    packetValidationReport,
    briefValidationReport,
    repairEligibility,
    repairResult,
    status,
    isApprovedForPrepare,
    isPacketValidationFailed,
    isPacketValidated,
    isBriefReadyForReview,
    isApprovedForExecutor,
    canApproveBrief,
    compileAttempted,
    isPending,
  } = controller;

  const packetValidationErrors = packetValidationReport?.errors || [];

  return (
    <div className="flex min-w-0 flex-col gap-4">
      {mutationError ? (
        <RelayStateBanner
          tone="danger"
          title="Compile / Render action failed"
          description={mutationError}
        />
      ) : null}

      <div className="grid gap-3 md:grid-cols-4">
        <SummaryTile
          label="Compiled Packet"
          value={getCompileSummaryLabel(controller)}
          tone={getCompileSummaryTone(controller)}
        />
        <SummaryTile
          label="Packet Validation"
          value={getPacketValidationStateLabel(controller)}
          tone={getPacketValidationStateTone(controller)}
        />
        <SummaryTile
          label="Executor Brief"
          value={getExecutorBriefSummaryLabel(controller)}
          tone={getExecutorBriefSummaryTone(controller)}
        />
        <SummaryTile
          label="Approval"
          value={getApprovalStateLabel(controller)}
          tone={getApprovalStateTone(controller)}
        />
      </div>

      <Section
        title="Compiled Packet"
        icon={<CheckCircle2 className="h-4 w-4 text-emerald-400" />}
      >
        <div className="flex flex-col gap-3">
          <div className="flex flex-wrap items-center gap-2">
            <Badge
              variant={
                isPacketValidationFailed
                  ? "destructive"
                  : compileAttempted
                    ? "success"
                    : "secondary"
              }
              className="text-xs"
            >
              {getCompileSummaryLabel(controller)}
            </Badge>
            <span className="text-xs text-muted-foreground">
              Current status: <code className="font-mono">{status}</code>
            </span>
          </div>

          {canonicalPacketArt ? (
            <ArtifactPathRow
              label="Canonical packet"
              artifact={canonicalPacketArt}
            />
          ) : (
            <RelayInlineState
              tone="empty"
              title="Canonical packet not created yet"
              description={
                isApprovedForPrepare
                  ? "Use the stage rail to run compile when you are ready."
                  : "Compile output will appear here after the prepare route produces a canonical packet."
              }
            />
          )}

          {isPacketValidationFailed ? (
            <>
              <RelayStateBanner
                tone="danger"
                title="Validation failed"
                description={`Compile failed packet validation with ${packetValidationErrors.length || 0} error${packetValidationErrors.length === 1 ? "" : "s"}. Review the report before retrying compile or attempting repair.`}
              />
              {packetValidationArt ? (
                <div className="flex flex-wrap items-center gap-3 text-xs text-muted-foreground">
                  <span>
                    Report:{" "}
                    <code className="font-mono">{packetValidationArt.path}</code>
                  </span>
                  <button
                    type="button"
                    onClick={() => setShowValidationInspector(true)}
                    className="font-medium text-foreground underline-offset-4 hover:underline"
                  >
                    Inspect report
                  </button>
                </div>
              ) : null}
              {packetValidationErrors.length > 0 ? (
                <div className="flex max-h-40 flex-col gap-1.5 overflow-y-auto rounded border border-border/40 bg-muted/20 p-3">
                  {packetValidationErrors.map((error, index) => (
                    <div
                      key={`${error.code || "issue"}-${index}`}
                      className="flex items-start gap-2 text-xs leading-normal text-foreground/85"
                    >
                      <span className="shrink-0 font-bold text-red-400">
                        [{error.code || "ERROR"}]
                      </span>
                      <span>{error.message || "Validation issue captured."}</span>
                    </div>
                  ))}
                </div>
              ) : null}
            </>
          ) : compileAttempted ? (
            <p className="text-sm text-muted-foreground">
              Compile output is present. Review packet validation, repair status,
              and executor brief readiness below.
            </p>
          ) : (
            <p className="text-sm text-muted-foreground">
              Compile has not run yet. The stage rail owns the compile action for
              this step.
            </p>
          )}
        </div>
      </Section>

      <Section
        title="Packet Validation & Repair"
        icon={<Wrench className="h-4 w-4 text-yellow-400" />}
      >
        <div className="grid gap-4 lg:grid-cols-[minmax(0,1.1fr)_minmax(0,0.9fr)]">
          <div className="flex min-w-0 flex-col gap-3">
            {packetValidationArt ? (
              <>
                <div className="flex flex-wrap items-center gap-2">
                  <Badge
                    variant={
                      packetValidationReport?.valid === true
                        ? "success"
                        : isPacketValidationFailed
                          ? "destructive"
                          : "secondary"
                    }
                    className="text-xs"
                  >
                    {getPacketValidationStateLabel(controller)}
                  </Badge>
                  <span className="text-xs text-muted-foreground">
                    Validation report captured for this compile pass.
                  </span>
                </div>
                <ArtifactPathRow
                  label="Validation report"
                  artifact={packetValidationArt}
                />
              </>
            ) : (
              <RelayInlineState
                tone="empty"
                title="Validation report unavailable"
                description={
                  compileAttempted
                    ? "Relay has not captured a packet validation report for the current compile result."
                    : "Compile must run before packet validation details are available."
                }
              />
            )}
          </div>

          <div className="flex min-w-0 flex-col gap-3 rounded border border-[var(--relay-row-border)] bg-[var(--surface-inset)]/40 p-3">
            <StatusLine
              label="Repair eligibility"
              value={getRepairEligibilityLabel(controller)}
            />
            <StatusLine
              label="Latest repair result"
              value={getRepairResultLabel(controller)}
            />
            <p className="text-xs text-muted-foreground">
              {getRepairGuidance(controller, repairEligibility.reason)}
            </p>

            {status === "repair_validated" || repairResult?.reValidationValid ? (
              <RelayInlineState
                tone="success"
                title="Repair validated"
                description="Repair passed validation. The packet can move forward to executor brief rendering."
              />
            ) : null}

            {repairResult?.blockedReason ? (
              <RelayStateBanner
                tone="blocked"
                title="Repair blocked"
                description={repairResult.blockedReason}
              />
            ) : null}

            {repairResult?.ineligibleReason ? (
              <RelayStateBanner
                tone="warning"
                title="Repair ineligible"
                description={repairResult.ineligibleReason}
              />
            ) : null}

            {repairResult &&
            !repairResult.blockedReason &&
            !repairResult.ineligibleReason &&
            repairResult.reValidationValid === false ? (
              <RelayStateBanner
                tone="danger"
                title="Repair attempted but validation still failed"
                description={
                  repairResult.reValidationError ||
                  "Repair did not produce a validation-passing packet."
                }
              />
            ) : null}
          </div>
        </div>
      </Section>

      <Section
        title="Executor Brief"
        icon={<FileText className="h-4 w-4 text-blue-400" />}
      >
        <div className="flex flex-col gap-3">
          <div className="flex flex-wrap items-center gap-2">
            <Badge
              variant={executorBriefArt ? "success" : "secondary"}
              className="text-xs"
            >
              {getExecutorBriefSummaryLabel(controller)}
            </Badge>
            <Badge
              variant={
                briefValidationReport?.status === "passed"
                  ? "success"
                  : briefValidationArt
                    ? "destructive"
                    : "secondary"
              }
              className="text-xs"
            >
              Brief validation: {getBriefValidationStateLabel(controller)}
            </Badge>
          </div>

          {executorBriefArt ? (
            <>
              <ArtifactPathRow
                label="Executor brief"
                artifact={executorBriefArt}
              />
              {executorBriefArt.preview ? (
                <pre className="max-h-48 overflow-y-auto rounded border border-border/40 bg-muted/30 p-3 font-mono text-[11px] whitespace-pre-wrap text-foreground">
                  {executorBriefArt.preview}
                </pre>
              ) : (
                <RelayInlineState
                  tone="empty"
                  title="Brief preview unavailable"
                  description="Relay captured the executor brief artifact but no preview text is available in this view."
                />
              )}
            </>
          ) : (
            <RelayInlineState
              tone="empty"
              title="Executor brief not rendered"
              description={
                isPacketValidated
                  ? "Use the stage rail to render the executor brief from the validated packet."
                  : "A validated or repaired packet is required before the executor brief can be rendered."
              }
            />
          )}

          {briefValidationArt ? (
            <ArtifactPathRow
              label="Brief validation report"
              artifact={briefValidationArt}
            />
          ) : null}

          {briefValidationReport?.issues?.length ? (
            <div className="flex max-h-40 flex-col gap-1.5 overflow-y-auto rounded border border-border/40 bg-muted/20 p-3">
              {briefValidationReport.issues.map((issue, index) => (
                <div
                  key={`${issue.severity || "issue"}-${index}`}
                  className="flex items-start gap-2 text-xs leading-normal text-foreground/85"
                >
                  <span
                    className={
                      issue.severity === "error"
                        ? "shrink-0 font-bold text-red-400"
                        : "shrink-0 font-bold text-yellow-400"
                    }
                  >
                    [{(issue.severity || "issue").toUpperCase()}]
                  </span>
                  <span>{issue.message || "Validation issue captured."}</span>
                </div>
              ))}
            </div>
          ) : briefValidationReport?.status === "passed" ? (
            <p className="text-sm text-muted-foreground">
              Brief validation passed with no reported issues.
            </p>
          ) : null}

          {!executorBriefArt && !isPacketValidated && !isBriefReadyForReview ? (
            <p className="text-sm text-muted-foreground">
              This area becomes active after compile validation succeeds or a
              repair pass validates the packet.
            </p>
          ) : null}
        </div>
      </Section>

      <Section
        title="Approval"
        icon={<ShieldCheck className="h-4 w-4 text-primary" />}
      >
        <div className="flex flex-col gap-3">
          {isApprovedForExecutor ? (
            <RelayInlineState
              tone="success"
              title="Approved for executor"
              description="Compile / Render is complete. Use the stage rail to continue into Execute."
            />
          ) : canApproveBrief ? (
            <>
              <RelayInlineState
                tone="info"
                title="Ready for approval"
                description="Review notes can be captured here before approving the executor brief from the stage rail."
              />
              <div className="flex flex-col gap-1.5">
                <Label
                  htmlFor="approval-notes"
                  className="text-xs text-muted-foreground"
                >
                  Approval Notes (Optional)
                </Label>
                <Textarea
                  id="approval-notes"
                  value={approvalNotes}
                  onChange={(event) => setApprovalNotes(event.target.value)}
                  placeholder="Optional notes for the approval decision..."
                  className="h-20 resize-none text-xs"
                  disabled={isPending}
                />
              </div>
            </>
          ) : (
            <RelayInlineState
              tone="empty"
              title="Approval not ready"
              description={
                isApprovedForPrepare
                  ? "Compile must run before approval can become available."
                  : "Compile, packet validation, and executor brief review must complete before approval is available."
              }
            />
          )}
        </div>
      </Section>

      {packetValidationArt ? (
        <ArtifactInspectorDialog
          runId={run.id}
          artifact={packetValidationArt}
          open={showValidationInspector}
          onOpenChange={setShowValidationInspector}
        />
      ) : null}
    </div>
  );
}

function CompileRenderDetailsPanel({
  controller,
}: {
  controller: CompileRenderController;
}) {
  const {
    run,
    canonicalPacketArt,
    packetValidationArt,
    executorBriefArt,
    briefValidationArt,
  } = controller;

  return (
    <div className="flex flex-col gap-3">
      <InspectorSection title="Run State">
        <InspectorField label="Status" value={controller.status} mono />
        <InspectorField label="Active step" value="Compile / Render" />
        <InspectorField
          label="Executor adapter"
          value={run.executorAdapter || run.executor || "—"}
          mono
        />
        <InspectorField label="Selected model" value={run.model || "—"} mono />
      </InspectorSection>

      <InspectorSection title="Compiled Packet">
        <InspectorField
          label="Canonical packet"
          value={formatArtifactLocation(canonicalPacketArt)}
          mono
        />
        <InspectorField
          label="Validation status"
          value={getPacketValidationStateLabel(controller)}
        />
        <InspectorField
          label="Validation report"
          value={formatArtifactLocation(packetValidationArt)}
          mono
        />
      </InspectorSection>

      <InspectorSection title="Repair">
        <InspectorField
          label="Eligibility"
          value={getRepairEligibilityLabel(controller)}
        />
        <InspectorField
          label="Latest repair result"
          value={getRepairResultLabel(controller)}
        />
      </InspectorSection>

      <InspectorSection title="Executor Brief">
        <InspectorField
          label="Brief artifact"
          value={formatArtifactLocation(executorBriefArt)}
          mono
        />
        <InspectorField
          label="Validation status"
          value={getBriefValidationStateLabel(controller)}
        />
        <InspectorField
          label="Validation report"
          value={formatArtifactLocation(briefValidationArt)}
          mono
        />
      </InspectorSection>

      <InspectorSection title="Approval">
        <InspectorField
          label="Approval state"
          value={getApprovalStateLabel(controller)}
        />
      </InspectorSection>
    </div>
  );
}

function SummaryTile({
  label,
  value,
  tone,
}: {
  label: string;
  value: string;
  tone: "default" | "success" | "warning" | "danger";
}) {
  const toneClasses: Record<typeof tone, string> = {
    default: "border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)]",
    success: "border-[var(--success)]/35 bg-[var(--success)]/10",
    warning: "border-[var(--warning)]/35 bg-[var(--warning)]/10",
    danger: "border-[var(--destructive)]/35 bg-[var(--destructive)]/10",
  };

  return (
    <div
      className={`rounded border px-3 py-2.5 ${toneClasses[tone]}`}
    >
      <p className="text-[10px] font-medium uppercase tracking-[0.08em] text-muted-foreground">
        {label}
      </p>
      <p className="mt-2 text-sm font-semibold text-foreground">{value}</p>
    </div>
  );
}

function ArtifactPathRow({
  label,
  artifact,
}: {
  label: string;
  artifact: RelayArtifact;
}) {
  return (
    <div className="rounded border border-[var(--relay-row-border)] bg-[var(--surface-inset)]/40 px-3 py-2.5">
      <p className="text-[10px] font-medium uppercase tracking-[0.06em] text-muted-foreground">
        {label}
      </p>
      <p className="mt-2 break-words font-mono text-[12px] text-foreground">
        {artifact.path || artifact.filename || "—"}
      </p>
      {artifact.sizeHint ? (
        <p className="mt-1 text-xs text-muted-foreground">{artifact.sizeHint}</p>
      ) : null}
    </div>
  );
}

function StatusLine({
  label,
  value,
}: {
  label: string;
  value: string;
}) {
  return (
    <div className="flex items-start justify-between gap-3">
      <span className="text-xs text-muted-foreground">{label}</span>
      <span className="text-right text-sm text-foreground">{value}</span>
    </div>
  );
}

function InspectorSection({
  title,
  children,
}: {
  title: string;
  children: React.ReactNode;
}) {
  return (
    <section className="rounded border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)] px-3 py-3">
      <p className="text-[10px] font-medium uppercase tracking-[0.08em] text-muted-foreground">
        {title}
      </p>
      <div className="mt-3 flex flex-col gap-2.5">{children}</div>
    </section>
  );
}

function InspectorField({
  label,
  value,
  mono = false,
}: {
  label: string;
  value: React.ReactNode;
  mono?: boolean;
}) {
  return (
    <div className="flex items-start justify-between gap-3">
      <span className="text-xs text-muted-foreground">{label}</span>
      <span
        className={[
          "max-w-[60%] text-right text-sm text-foreground",
          mono ? "font-mono text-[12px]" : "",
        ]
          .filter(Boolean)
          .join(" ")}
      >
        {value}
      </span>
    </div>
  );
}

function Section({
  title,
  icon,
  children,
}: {
  title: string;
  icon?: React.ReactNode;
  children: React.ReactNode;
}) {
  return (
    <Card className="min-w-0 border-border/60 bg-card/20">
      <CardHeader className="p-4 pb-3">
        <CardTitle className="flex items-center gap-2 text-sm font-medium">
          {icon}
          {title}
        </CardTitle>
      </CardHeader>
      <CardContent className="flex min-w-0 flex-col gap-3 p-4 pt-0">
        {children}
      </CardContent>
    </Card>
  );
}

function findArtifact(
  artifacts: RelayArtifact[],
  filename: string,
): RelayArtifact | undefined {
  return artifacts.find((artifact) => artifact.filename === filename);
}

function parseArtifactPreview<T>(artifact?: RelayArtifact): T | null {
  if (!artifact?.preview) {
    return null;
  }

  try {
    return JSON.parse(artifact.preview) as T;
  } catch {
    return null;
  }
}

function formatArtifactLocation(artifact?: RelayArtifact): string {
  return artifact?.path || artifact?.filename || "—";
}

function getCompileSummaryLabel(controller: CompileRenderController): string {
  if (controller.isPacketValidationFailed) {
    return "Compile failed";
  }
  if (controller.compileAttempted) {
    return "Compiled";
  }
  if (controller.isPending && controller.compileMutation.isPending) {
    return "Compiling";
  }
  return "Pending";
}

function getCompileSummaryTone(
  controller: CompileRenderController,
): "default" | "success" | "warning" | "danger" {
  if (controller.isPacketValidationFailed) {
    return "danger";
  }
  if (controller.compileAttempted) {
    return "success";
  }
  if (controller.canCompile) {
    return "warning";
  }
  return "default";
}

function getPacketValidationStateLabel(
  controller: CompileRenderController,
): string {
  if (controller.packetValidationReport?.valid === true) {
    return "Valid";
  }
  if (controller.isPacketValidationFailed) {
    return "Invalid";
  }
  if (controller.packetValidationArt) {
    return "Invalid";
  }
  return "Pending";
}

function getPacketValidationStateTone(
  controller: CompileRenderController,
): "default" | "success" | "warning" | "danger" {
  if (controller.packetValidationReport?.valid === true) {
    return "success";
  }
  if (controller.isPacketValidationFailed || controller.packetValidationArt) {
    return "danger";
  }
  if (controller.compileAttempted) {
    return "warning";
  }
  return "default";
}

function getRepairEligibilityLabel(controller: CompileRenderController): string {
  if (controller.isPacketValidationFailed) {
    return controller.repairEligible ? "Repair eligible" : "Not eligible";
  }
  if (
    controller.isPacketValidated ||
    controller.isBriefReadyForReview ||
    controller.isApprovedForExecutor
  ) {
    return "Not applicable";
  }
  return "Pending";
}

function getRepairResultLabel(controller: CompileRenderController): string {
  if (controller.repairMutation.isPending) {
    return "Pending";
  }
  if (controller.status === "repair_validated") {
    return "Validated";
  }
  if (controller.repairResult?.blockedReason) {
    return "Blocked";
  }
  if (controller.repairResult?.ineligibleReason) {
    return "Ineligible";
  }
  if (controller.repairResult?.reValidationValid === true) {
    return "Validated";
  }
  if (
    controller.repairResult?.reValidationValid === false ||
    controller.repairResult?.error
  ) {
    return "Failed";
  }
  return "Pending";
}

function getRepairGuidance(
  controller: CompileRenderController,
  fallbackReason: string,
): string {
  if (controller.isPacketValidationFailed) {
    if (controller.repairEligible) {
      return "Repair is available for this validation failure. Use the stage rail if you want Relay to attempt a constrained repair pass.";
    }
    return fallbackReason || "Repair is not available for the current validation report.";
  }

  if (controller.status === "repair_validated") {
    return "Repair already validated for this run. Continue with executor brief rendering.";
  }

  if (!controller.compileAttempted) {
    return "Repair becomes relevant only after a compile pass produces a validation report.";
  }

  return "Repair is only relevant when compile validation fails with repair-eligible issues.";
}

function getExecutorBriefSummaryLabel(
  controller: CompileRenderController,
): string {
  if (controller.executorBriefArt) {
    return "Rendered";
  }
  if (controller.canRenderBrief) {
    return "Ready to render";
  }
  return "Pending";
}

function getExecutorBriefSummaryTone(
  controller: CompileRenderController,
): "default" | "success" | "warning" | "danger" {
  if (controller.executorBriefArt) {
    return "success";
  }
  if (controller.canRenderBrief) {
    return "warning";
  }
  return "default";
}

function getBriefValidationStateLabel(
  controller: CompileRenderController,
): string {
  if (controller.briefValidationReport?.status === "passed") {
    return "Passed";
  }
  if (controller.briefValidationArt) {
    return "Failed";
  }
  return "Pending";
}

function getApprovalStateLabel(controller: CompileRenderController): string {
  if (controller.isApprovedForExecutor) {
    return "Approved for executor";
  }
  if (controller.canApproveBrief) {
    return "Ready for approval";
  }
  return "Not ready";
}

function getApprovalStateTone(
  controller: CompileRenderController,
): "default" | "success" | "warning" | "danger" {
  if (controller.isApprovedForExecutor) {
    return "success";
  }
  if (controller.canApproveBrief) {
    return "warning";
  }
  return "default";
}
