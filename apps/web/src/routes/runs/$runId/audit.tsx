import { createFileRoute } from "@tanstack/react-router";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { useState, useMemo } from "react";
import {
  runDetailQueryOptions,
  runArtifactsQueryOptions,
  runEventsQueryOptions,
  auditRun,
  validateRun,
  submitManualAuditPacket,
  approveAudit,
  requestAuditRevision,
  prepareCommitMessage,
  closeRun,
  evaluateValidationGate,
  isAuditCandidateStatus,
  acceptFailedValidation,
} from "@/features/relay-runs";
import type {
  RelayArtifact,
  RelayAuditDecisionValue,
  RelayAuditInputSummaryInfo,
  RelayAuditPacketInfo,
  RelayAuditDecisionStatus,
  RelayCommitSummary,
  RelayAuditActions,
  RelayRun,
  RelayRunEvent,
} from "@/features/relay-runs";
import { RELAY_AUDIT_DECISION_VALUES } from "@/features/relay-runs";
import { RunWorkbenchLayout } from "@/components/relay/RunWorkbenchLayout";
import { RelayStateBanner } from "@/components/relay/RelayStateSurface";
import {
  RunWorkbenchLoadFailedState,
  RunWorkbenchLoadingState,
} from "@/components/relay/RunWorkbenchStates";
import { ValidationPanel } from "@/components/relay/ValidationPanel";
import { ArtifactPreviewCard } from "@/components/relay/ArtifactPreviewCard";
import { RunEvidenceBrowser } from "@/components/relay/RunEvidenceBrowser";
import { LogPreviewPanel } from "@/components/relay/LogPreviewPanel";
import {
  RunStageInspectorSection,
  RunStageKeyValueRow,
  RunStagePipeline,
  RunStageStateCard,
  RunStageSummaryCard,
  RunStageSummaryChip,
} from "@/components/relay/RunStagePrimitives";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Separator } from "@/components/ui/separator";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
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
  RefreshCw,
  FileCode,
} from "lucide-react";
import {
  AUDIT_PIPELINE_STEPS,
  getAuditDisplayState,
  getAuditPipelineStatuses,
  getAuditStateCardCopy,
} from "./runAuditVisualState";

export const Route = createFileRoute("/runs/$runId/audit")({
  component: AuditPage,
});

function AuditPage() {
  const { runId } = Route.useParams();
  const {
    data: run,
    isLoading: isLoadingRun,
    error: errorRun,
  } = useQuery(runDetailQueryOptions(runId));
  const { data: artifacts, isLoading: isLoadingArtifacts } = useQuery(
    runArtifactsQueryOptions(runId),
  );
  const { data: events } = useQuery(runEventsQueryOptions(runId));

  if (isLoadingRun || isLoadingArtifacts) {
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
      currentStep="audit"
      mainContent={
        <AuditMainContent
          run={run}
          artifacts={resolvedArtifacts}
          events={resolvedEvents}
        />
      }
      initialInspectorTab="details"
      inspectorTabs={[
        { key: "details", label: "Details" },
        { key: "artifacts", label: "Artifacts" },
        { key: "validation", label: "Validation" },
        { key: "logs", label: "Logs" },
      ]}
      inspectorPanels={{
        details: (
          <AuditDetailsPanel
            run={run}
            artifacts={resolvedArtifacts}
            events={resolvedEvents}
          />
        ),
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

function deriveAuditData(
  run: RelayRun,
  artifacts: RelayArtifact[],
  events: RelayRunEvent[],
): {
  inputSummary: RelayAuditInputSummaryInfo;
  generatedPacket: RelayAuditPacketInfo;
  manualPacket?: RelayAuditPacketInfo;
  decision: RelayAuditDecisionStatus;
  commitSummary: RelayCommitSummary;
  actions: RelayAuditActions;
  warnings: string[];
  revisionRequirements: string[];
  blockers: string[];
} {
  const auditInputArts = artifacts
    .filter(
      (a: any) =>
        a.kind === "audit" &&
        (a.filename?.includes("audit_input_summary") ||
          a.path?.includes("audit_input_summary") ||
          a.label === "Audit Input Summary"),
    )
    .sort(
      (a: any, b: any) =>
        new Date(b.createdAt || 0).getTime() -
        new Date(a.createdAt || 0).getTime(),
    );
  const inputArtifact = auditInputArts[0];

  const packetArts = artifacts.filter(
    (a: any) =>
      a.kind === "audit" &&
      (a.filename?.includes("audit_packet") ||
        a.path?.includes("audit_packet") ||
        a.label === "Audit Packet"),
  );
  const genPacketArt = packetArts.find(
    (a: any) => !a.filename?.includes("manual") && !a.path?.includes("manual"),
  );
  const manualPacketArt = packetArts.find(
    (a: any) => a.filename?.includes("manual") || a.path?.includes("manual"),
  );

  const hasRevisionEvent = events.some((e: any) =>
    e.message?.includes("Audit revision requested"),
  );

  const runStatus = run.status || "";
  const isAuditCandidate = isAuditCandidateStatus(runStatus);
  const isAccepted =
    runStatus === "accepted" || runStatus === "accepted_with_warnings";
  const isAuditReady =
    runStatus === "audit_ready" || runStatus === "audit_ready_for_review";
  const isCompleted = runStatus === "completed";
  const isRevisionRequired = runStatus === "revision_required";
  const isBlocked = runStatus === "blocked";

  const { validationAllowsAudit } = evaluateValidationGate(
    artifacts as any,
    runStatus,
  );

  const commitMsgArts = artifacts.filter(
    (a: any) =>
      a.kind === "audit" &&
      (a.filename?.includes("commit_message") ||
        a.path?.includes("commit_message")),
  );
  const commitMsgArt = commitMsgArts[commitMsgArts.length - 1];
  const changedFileArts = artifacts.filter((a: any) => a.kind === "diff");

  const evidenceArtifacts = artifacts.filter(
    (a: any) =>
      a.kind === "result" || a.kind === "validation" || a.kind === "diff",
  );

  const hasGenerateEvent = events.some(
    (e: any) =>
      e.message?.includes("Audit packet generated") ||
      e.message?.includes("audit packet generated"),
  );
  const hasManualSubmitEvent = events.some((e: any) =>
    e.message?.includes("Manual audit packet submitted"),
  );

  const auditDecision = run.approvalGate?.state === "approved";
  const decisionValue: RelayAuditDecisionValue | undefined = isAccepted
    ? "accepted"
    : undefined;

  return {
    inputSummary: {
      artifactId: inputArtifact?.id || "",
      artifactPath: inputArtifact?.path || "",
      available: !!inputArtifact,
      generatedAt: inputArtifact?.createdAt,
      preview: inputArtifact?.preview,
      evidenceIncluded: evidenceArtifacts.map(
        (a: any) => a.label || a.filename,
      ),
      missingEvidence: [],
    },
    generatedPacket: {
      artifactId: genPacketArt?.id || "",
      artifactPath: genPacketArt?.path || "",
      available: !!genPacketArt,
      isManual: false,
      generatedAt: genPacketArt?.createdAt,
      preview: genPacketArt?.preview,
      warnings:
        run.validationSummary?.warnings > 0
          ? (run.validationSummary?.issues || [])
              .filter((i: any) => i.severity === "warning")
              .map((i: any) => i.message)
          : [],
    },
    manualPacket: manualPacketArt
      ? {
          artifactId: manualPacketArt.id,
          artifactPath: manualPacketArt.path,
          available: true,
          isManual: true,
          generatedAt: manualPacketArt.createdAt,
          preview: manualPacketArt.preview,
          warnings: [],
        }
      : undefined,
    decision: {
      currentDecision: decisionValue,
      source: isCompleted
        ? "approved"
        : hasManualSubmitEvent
          ? "manual"
          : hasGenerateEvent
            ? "generated"
            : "none",
      approvedAt: run.updatedAt,
      notes: run.approvalGate?.note,
    },
    commitSummary: {
      changedFileArtifactIds: changedFileArts.map((a: any) => a.id),
      commitMessageArtifactId: commitMsgArt?.id,
      commitMessagePreview: commitMsgArt?.preview,
      commitMessageAvailable: !!commitMsgArt,
      validationSummary: `${run.validationSummary?.passed || 0} passed, ${run.validationSummary?.errors || 0} errors, ${run.validationSummary?.warnings || 0} warnings`,
      auditDecisionSummary: decisionValue || "Pending review",
    },
    actions: {
      canGenerateAudit:
        isAuditCandidate && !isCompleted && validationAllowsAudit,
      canSubmitManual:
        isAuditReady && !isAccepted && !isCompleted && !isRevisionRequired,
      canApproveAudit:
        (hasGenerateEvent || hasManualSubmitEvent) &&
        isAuditReady &&
        !auditDecision &&
        !isCompleted &&
        !isRevisionRequired,
      canRequestRevision: isAuditReady && !isCompleted && !isRevisionRequired,
      canPrepareCommitMessage: isAccepted && !isCompleted,
      canCloseRun: isAccepted && !isCompleted,
      generateAuditUnavailableReason: !isAuditCandidate
        ? `Current status: ${runStatus}. Audit generation is not available for this lifecycle stage.`
        : isCompleted
          ? undefined
          : !validationAllowsAudit
            ? `Current status: ${runStatus}. Audit generation requires a passed or accepted-failed local validation result.`
            : undefined,
      submitManualUnavailableReason:
        !isAuditReady || isRevisionRequired
          ? `Current status: ${runStatus}`
          : undefined,
      approveAuditUnavailableReason:
        !isAuditReady || isRevisionRequired
          ? `Current status: ${runStatus}`
          : undefined,
      requestRevisionUnavailableReason:
        !isAuditReady || isRevisionRequired
          ? `Current status: ${runStatus}`
          : undefined,
      prepareCommitMessageUnavailableReason: !isAccepted
        ? `Run must be in accepted or accepted_with_warnings status. Current: ${runStatus}`
        : undefined,
      closeRunUnavailableReason: !isAccepted
        ? `Audit must be approved first (status must be accepted or accepted_with_warnings). Current: ${runStatus}`
        : undefined,
    },
    warnings:
      run.validationSummary?.warnings > 0
        ? (run.validationSummary?.issues || [])
            .filter((i: any) => i.severity === "warning")
            .map((i: any) => i.message)
        : [],
    revisionRequirements: hasRevisionEvent
      ? events
          .filter((e: any) => e.message?.includes("Audit revision requested"))
          .map((e: any) => e.message)
      : [],
    blockers: isBlocked
      ? ["Run is blocked and cannot proceed to close."]
      : isRevisionRequired
        ? ["Revision requested — audit must be regenerated."]
        : [],
  };
}

function AuditMainContent({
  run,
  artifacts,
  events,
}: {
  run: RelayRun;
  artifacts: RelayArtifact[];
  events: RelayRunEvent[];
}) {
  const queryClient = useQueryClient();
  const { runId } = Route.useParams();
  const [mutationError, setMutationError] = useState<string | null>(null);
  const [showManualSubmit, setShowManualSubmit] = useState(false);
  const [manualDecision, setManualDecision] = useState<string>("");
  const [manualPacketMarkdown, setManualPacketMarkdown] = useState("");
  const [manualNotes, setManualNotes] = useState("");
  const [approveDecision, setApproveDecision] = useState<
    "accepted" | "accepted_with_warnings"
  >("accepted");
  const [approveNotes, setApproveNotes] = useState("");
  const [showApproveForm, setShowApproveForm] = useState(false);
  const [revisionReason, setRevisionReason] = useState("");
  const [showRevisionForm, setShowRevisionForm] = useState(false);
  const [acceptanceReason, setAcceptanceReason] = useState("");
  const [showAcceptanceForm, setShowAcceptanceForm] = useState(false);

  const auditData = useMemo(
    () => deriveAuditData(run, artifacts, events),
    [run, artifacts, events],
  );

  const runStatus = (run.status || "") as string;
  const { hasFinalValidationEvidence, validationAllowsAudit } =
    evaluateValidationGate(
      artifacts as Array<{ storageKind: string }>,
      runStatus,
    );

  const localValidationIsRunning = runStatus === "local_validation_running";
  const localValidationPassed = runStatus === "validation_passed";
  const localValidationFailed = runStatus === "validation_failed";
  const localValidationAccepted = runStatus === "validation_failed_accepted";
  const isAuditReadyStatus =
    runStatus === "audit_ready" || runStatus === "audit_ready_for_review";
  const isAuditBlockedStatus =
    runStatus === "blocked" ||
    runStatus.includes("rejected") ||
    (runStatus.includes("failed") &&
      runStatus !== "validation_failed" &&
      runStatus !== "validation_failed_accepted");

  const validationRunJsonArt = artifacts.find(
    (a) => a.storageKind === "validation_run_json",
  );
  const validationFailureAcceptanceJsonArt = artifacts.find(
    (a) => a.storageKind === "validation_failure_acceptance_json",
  );
  const validationProgressJsonArt = artifacts.find(
    (a) => a.storageKind === "validation_progress_json",
  );
  const validationStdoutArt = artifacts.find(
    (a) => a.storageKind === "validation_stdout",
  );
  const validationStderrArt = artifacts.find(
    (a) => a.storageKind === "validation_stderr",
  );

  const invalidate = () => {
    void queryClient.invalidateQueries({ queryKey: ["relay-runs"] });
  };

  const generateMutation = useMutation({
    mutationFn: () => auditRun(runId),
    onSuccess: () => {
      setMutationError(null);
      invalidate();
    },
    onError: (err: any) =>
      setMutationError(err.message || "Failed to generate audit."),
  });

  const validateMutation = useMutation({
    mutationFn: () => validateRun(runId),
    onSuccess: () => {
      setMutationError(null);
      invalidate();
    },
    onError: (err: any) =>
      setMutationError(err.message || "Failed to run validation."),
  });

  const submitManualMutation = useMutation({
    mutationFn: () =>
      submitManualAuditPacket(runId, {
        audit_packet_markdown: manualPacketMarkdown,
        decision: manualDecision as RelayAuditDecisionValue,
        notes: manualNotes,
      }),
    onSuccess: () => {
      setMutationError(null);
      setShowManualSubmit(false);
      setManualPacketMarkdown("");
      invalidate();
    },
    onError: (err: any) =>
      setMutationError(err.message || "Failed to submit manual audit packet."),
  });

  const approveMutation = useMutation({
    mutationFn: () =>
      approveAudit(runId, { decision: approveDecision, notes: approveNotes }),
    onSuccess: () => {
      setMutationError(null);
      setShowApproveForm(false);
      setApproveNotes("");
      invalidate();
    },
    onError: (err: any) =>
      setMutationError(err.message || "Failed to approve audit."),
  });

  const revisionMutation = useMutation({
    mutationFn: () => requestAuditRevision(runId, { reason: revisionReason }),
    onSuccess: () => {
      setMutationError(null);
      setShowRevisionForm(false);
      setRevisionReason("");
      invalidate();
    },
    onError: (err: any) =>
      setMutationError(err.message || "Failed to request revision."),
  });

  const prepareCommitMutation = useMutation({
    mutationFn: () => prepareCommitMessage(runId),
    onSuccess: () => {
      setMutationError(null);
      invalidate();
    },
    onError: (err: any) =>
      setMutationError(err.message || "Failed to prepare commit message."),
  });

  const closeMutation = useMutation({
    mutationFn: () => closeRun(runId),
    onSuccess: () => {
      setMutationError(null);
      invalidate();
    },
    onError: (err: any) =>
      setMutationError(err.message || "Failed to close run."),
  });

  const acceptFailureMutation = useMutation({
    mutationFn: (reason: string) => acceptFailedValidation(runId, reason),
    onSuccess: () => {
      setMutationError(null);
      setShowAcceptanceForm(false);
      setAcceptanceReason("");
      invalidate();
    },
    onError: (err: any) =>
      setMutationError(err.message || "Failed to accept validation failure."),
  });

  const activeMutation =
    generateMutation.isPending ||
    validateMutation.isPending ||
    submitManualMutation.isPending ||
    approveMutation.isPending ||
    revisionMutation.isPending ||
    prepareCommitMutation.isPending ||
    closeMutation.isPending ||
    acceptFailureMutation.isPending;
  const auditVisualStateInput = {
    run,
    hasFinalValidationEvidence,
    validationAllowsAudit,
    hasAuditPacket:
      auditData.generatedPacket.available || Boolean(auditData.manualPacket),
    hasInputSummary: auditData.inputSummary.available,
    hasWarnings:
      auditData.warnings.length > 0 || auditData.generatedPacket.warnings.length > 0,
    generatePending: generateMutation.isPending,
    validatePending: validateMutation.isPending,
    manualSubmitPending: submitManualMutation.isPending,
    approvePending: approveMutation.isPending,
    revisionPending: revisionMutation.isPending,
    commitMessagePending: prepareCommitMutation.isPending,
    closePending: closeMutation.isPending,
    acceptFailurePending: acceptFailureMutation.isPending,
    isAuditCandidate: isAuditCandidateStatus(runStatus),
    isAuditReady: isAuditReadyStatus,
    isAccepted:
      runStatus === "accepted" || runStatus === "accepted_with_warnings",
    isCompleted:
      runStatus === "completed" || run.lifecycleState === "completed",
    isRevisionRequired: runStatus === "revision_required",
    hasRevisionRequirements: auditData.revisionRequirements.length > 0,
    hasBlockers: auditData.blockers.length > 0,
  };
  const auditDisplayState = getAuditDisplayState(auditVisualStateInput);
  const auditPipelineStatuses = getAuditPipelineStatuses(auditVisualStateInput);
  const auditStateCardCopy = getAuditStateCardCopy(auditDisplayState);

  const handleGenerateAudit = () => {
    if (!auditData.actions.canGenerateAudit) return;
    setMutationError(null);
    generateMutation.mutate();
  };
  const handleValidate = () => {
    setMutationError(null);
    validateMutation.mutate();
  };
  const handleSubmitManual = () => {
    setMutationError(null);
    submitManualMutation.mutate();
  };
  const handleApproveAudit = () => {
    setMutationError(null);
    approveMutation.mutate();
  };
  const handleRequestRevision = () => {
    setMutationError(null);
    revisionMutation.mutate();
  };
  const handlePrepareCommitMessage = () => {
    setMutationError(null);
    prepareCommitMutation.mutate();
  };
  const handleCloseRun = () => {
    setMutationError(null);
    closeMutation.mutate();
  };
  const handleAcceptFailure = () => {
    if (!acceptanceReason.trim()) return;
    setMutationError(null);
    acceptFailureMutation.mutate(acceptanceReason);
  };

  return (
    <div className="flex flex-col gap-4">
      {mutationError && (
        <RelayStateBanner
          tone="danger"
          title="Audit action failed"
          description={mutationError}
        />
      )}

      <RunStageStateCard
        tone={auditStateCardCopy.tone}
        eyebrow={auditStateCardCopy.eyebrow}
        title={auditStateCardCopy.title}
        message={auditStateCardCopy.message}
      />

      <RunStageSummaryCard
        eyebrow="Audit / Closeout Pipeline"
        title="Closeout progression"
        description="Executor evidence, validation review, audit packet preparation, approval, and explicit run closeout."
      >
        <div className="mb-3 flex flex-wrap gap-2">
          <RunStageSummaryChip label="Status" value={runStatus} mono />
          <RunStageSummaryChip
            label="Validation"
            value={getAuditValidationLabel(
              runStatus,
              hasFinalValidationEvidence,
              validationAllowsAudit,
            )}
            tone={getAuditValidationTone(
              runStatus,
              hasFinalValidationEvidence,
              validationAllowsAudit,
            )}
          />
          <RunStageSummaryChip
            label="Packet"
            value={getAuditPacketLabel(auditData)}
            tone={getAuditPacketTone(auditData)}
          />
          <RunStageSummaryChip
            label="Decision"
            value={getAuditDecisionLabel(runStatus, auditData)}
            tone={getAuditDecisionTone(runStatus, auditData)}
          />
        </div>
        <RunStagePipeline
          steps={AUDIT_PIPELINE_STEPS}
          statuses={auditPipelineStatuses}
        />
      </RunStageSummaryCard>

      {isAuditReadyStatus && (
        <RelayStateBanner
          tone="success"
          title="Audit ready"
          description="Relay has enough evidence to review the audit packet and finish this run."
        />
      )}
      {runStatus === "revision_required" && (
        <RelayStateBanner
          tone="warning"
          title="Revision required"
          description={
            auditData.revisionRequirements[0] ||
            "Audit revisions were requested. Update the run output and regenerate the audit packet."
          }
        />
      )}
      {!isAuditReadyStatus && isAuditBlockedStatus && (
        <RelayStateBanner
          tone="blocked"
          title={runStatus.includes("failed") ? "Audit failed" : "Audit blocked"}
          description={
            auditData.blockers[0] ||
            "Relay cannot close this run until the audit blockers are resolved."
          }
          metadata={`Current status: ${runStatus}`}
        />
      )}

      {/* Local Validation Required Panel */}
      <Section
        title="Local Validation Required"
        icon={<ShieldCheck className="w-4 h-4 text-purple-400" />}
      >
        <div className="flex flex-col gap-2">
          {!hasFinalValidationEvidence &&
            !localValidationIsRunning &&
            !localValidationFailed && (
              <p className="text-xs text-muted-foreground">
                Run local validation before generating the audit packet.
              </p>
            )}
          {localValidationIsRunning && (
            <p className="text-xs text-muted-foreground">
              Local validation is running. Audit generation is unavailable until
              validation finishes.
            </p>
          )}
          {localValidationFailed && (
            <p className="text-xs text-red-400">
              Local validation failed. Review validation artifacts before
              continuing.
            </p>
          )}
          {localValidationPassed && (
            <p className="text-xs text-emerald-400">Local validation passed.</p>
          )}
          {localValidationAccepted && (
            <p className="text-xs text-yellow-400">
              Local validation failed but was accepted.
            </p>
          )}

          {/* Validation Artifact Links */}
          {(validationRunJsonArt ||
            validationFailureAcceptanceJsonArt ||
            validationProgressJsonArt ||
            validationStdoutArt ||
            validationStderrArt) && (
            <div className="flex flex-col gap-1.5 mt-2 border-t border-border/40 pt-2">
              <span className="text-[11px] font-medium text-muted-foreground/70">
                Validation Evidence:
              </span>
              {validationRunJsonArt && (
                <div className="flex items-center gap-2 text-xs font-mono">
                  <span className="text-muted-foreground">
                    Validation Result:
                  </span>
                  <ArtifactPreviewCard
                    artifact={validationRunJsonArt}
                    runId={runId}
                    className="flex-1 max-w-xs"
                  />
                </div>
              )}
              {validationFailureAcceptanceJsonArt && (
                <div className="flex items-center gap-2 text-xs font-mono">
                  <span className="text-muted-foreground">
                    Failure Acceptance:
                  </span>
                  <ArtifactPreviewCard
                    artifact={validationFailureAcceptanceJsonArt}
                    runId={runId}
                    className="flex-1 max-w-xs"
                  />
                </div>
              )}
              {validationProgressJsonArt && (
                <div className="flex items-center gap-2 text-xs font-mono">
                  <span className="text-muted-foreground">
                    Validation Progress:
                  </span>
                  <ArtifactPreviewCard
                    artifact={validationProgressJsonArt}
                    runId={runId}
                    className="flex-1 max-w-xs"
                  />
                </div>
              )}
              {validationStdoutArt && (
                <div className="flex items-center gap-2 text-xs font-mono">
                  <span className="text-muted-foreground">
                    Validation Output:
                  </span>
                  <ArtifactPreviewCard
                    artifact={validationStdoutArt}
                    runId={runId}
                    className="flex-1 max-w-xs"
                  />
                </div>
              )}
              {validationStderrArt && (
                <div className="flex items-center gap-2 text-xs font-mono">
                  <span className="text-muted-foreground">
                    Validation Error Output:
                  </span>
                  <ArtifactPreviewCard
                    artifact={validationStderrArt}
                    runId={runId}
                    className="flex-1 max-w-xs"
                  />
                </div>
              )}
            </div>
          )}

          {/* Action Button */}
          {(!hasFinalValidationEvidence || localValidationFailed) &&
            !localValidationIsRunning && (
              <Button
                variant="outline"
                size="sm"
                onClick={handleValidate}
                disabled={activeMutation}
                className="w-fit gap-1.5 mt-1"
              >
                {validateMutation.isPending ? (
                  <Loader2 className="w-3.5 h-3.5 animate-spin" />
                ) : (
                  <RefreshCw className="w-3.5 h-3.5" />
                )}
                Run Validation
              </Button>
            )}
          {localValidationIsRunning && (
            <Button
              variant="outline"
              size="sm"
              disabled={true}
              className="w-fit gap-1.5 mt-1"
            >
              <Loader2 className="w-3.5 h-3.5 animate-spin" />
              Running Validation...
            </Button>
          )}

          {localValidationFailed && hasFinalValidationEvidence && (
            <div className="flex flex-col gap-2 mt-2 border-t border-border/40 pt-2">
              <span className="text-[11px] font-medium text-muted-foreground/70">
                Accept Failed Validation:
              </span>
              {!showAcceptanceForm ? (
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => setShowAcceptanceForm(true)}
                  disabled={activeMutation}
                  className="w-fit gap-1.5"
                >
                  <AlertTriangle className="w-3.5 h-3.5 text-yellow-400" />
                  Accept Failure...
                </Button>
              ) : (
                <div className="flex flex-col gap-2 p-3 bg-yellow-950/10 border border-yellow-900/30 rounded">
                  <span className="text-xs font-medium text-yellow-400/90">
                    Provide Acceptance Reason:
                  </span>
                  <Textarea
                    placeholder="Enter reason for accepting this validation failure..."
                    value={acceptanceReason}
                    onChange={(e) => setAcceptanceReason(e.target.value)}
                    className="text-xs h-16 bg-background border-yellow-900/50 focus-visible:ring-yellow-800"
                    disabled={activeMutation}
                  />
                  <div className="flex items-center gap-2">
                    <Button
                      variant="default"
                      size="sm"
                      onClick={handleAcceptFailure}
                      disabled={activeMutation || !acceptanceReason.trim()}
                      className="bg-yellow-600 hover:bg-yellow-700 text-black font-semibold h-7 text-xs"
                    >
                      {acceptFailureMutation.isPending ? (
                        <Loader2 className="w-3 h-3 animate-spin mr-1" />
                      ) : null}
                      Submit Acceptance
                    </Button>
                    <Button
                      variant="ghost"
                      size="sm"
                      onClick={() => {
                        setShowAcceptanceForm(false);
                        setAcceptanceReason("");
                      }}
                      disabled={activeMutation}
                      className="h-7 text-xs"
                    >
                      Cancel
                    </Button>
                  </div>
                </div>
              )}
            </div>
          )}
        </div>
      </Section>

      {/* Audit Input Summary */}
      <Section
        title="Audit Input Summary"
        icon={<FileText className="w-4 h-4" />}
      >
        {auditData.inputSummary.available ? (
          <div className="flex flex-col gap-1.5">
            <div className="flex items-center gap-2">
              <CheckCircle2 className="w-3.5 h-3.5 text-emerald-400" />
              <span className="text-xs font-mono text-muted-foreground">
                {auditData.inputSummary.artifactPath}
              </span>
              {auditData.inputSummary.generatedAt && (
                <span className="text-[11px] text-muted-foreground/60">
                  {new Date(
                    auditData.inputSummary.generatedAt,
                  ).toLocaleString()}
                </span>
              )}
            </div>
            {auditData.inputSummary.preview && (
              <pre className="max-w-full overflow-x-auto text-[11px] font-mono bg-muted/40 p-2.5 rounded border border-border/40 max-h-48 overflow-y-auto whitespace-pre-wrap text-foreground">
                {auditData.inputSummary.preview}
              </pre>
            )}
            <div className="flex flex-col gap-0.5 text-xs text-muted-foreground">
              <span className="text-[11px] font-medium text-muted-foreground/70">
                Evidence included:
              </span>
              {auditData.inputSummary.evidenceIncluded.length > 0 ? (
                auditData.inputSummary.evidenceIncluded
                  .slice(0, 10)
                  .map((e: string, i: number) => (
                    <span
                      key={i}
                      className="flex items-center gap-1 text-[11px]"
                    >
                      <CheckSquare className="w-3 h-3 text-emerald-400/70" />
                      {e}
                    </span>
                  ))
              ) : (
                <span className="italic text-[11px] text-muted-foreground/50">
                  No evidence artifacts found.
                </span>
              )}
            </div>
            {auditData.inputSummary.missingEvidence.length > 0 && (
              <div className="flex flex-col gap-0.5 text-xs text-yellow-400/80">
                <span className="text-[11px] font-medium">
                  Missing evidence:
                </span>
                {auditData.inputSummary.missingEvidence.map(
                  (w: string, i: number) => (
                    <span
                      key={i}
                      className="flex items-center gap-1 text-[11px]"
                    >
                      <AlertTriangle className="w-3 h-3" />
                      {w}
                    </span>
                  ),
                )}
              </div>
            )}
            {(auditData.actions.canGenerateAudit ||
              (isAuditCandidateStatus(runStatus) &&
                runStatus !== "completed")) && (
              <div className="flex flex-col gap-1.5 mt-1">
                <Button
                  variant="outline"
                  size="sm"
                  onClick={handleGenerateAudit}
                  disabled={
                    activeMutation || !auditData.actions.canGenerateAudit
                  }
                  className="w-fit gap-1.5"
                >
                  {generateMutation.isPending ? (
                    <Loader2 className="w-3.5 h-3.5 animate-spin" />
                  ) : (
                    <RefreshCw className="w-3.5 h-3.5" />
                  )}
                  Regenerate Audit Summary
                </Button>
                {!auditData.actions.canGenerateAudit && (
                  <p className="text-xs text-yellow-500/90 mt-1">
                    {auditData.actions.generateAuditUnavailableReason ||
                      "Audit generation is blocked by validation requirements."}
                  </p>
                )}
              </div>
            )}
          </div>
        ) : (
          <div className="flex flex-col gap-2">
            <p className="text-xs text-muted-foreground italic">
              No audit input summary generated yet.
            </p>
            {auditData.actions.canGenerateAudit ? (
              <div className="flex flex-col gap-1.5">
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
              </div>
            ) : isAuditCandidateStatus(runStatus) &&
              runStatus !== "completed" ? (
              <div className="flex flex-col gap-1.5">
                <Button
                  variant="default"
                  size="sm"
                  disabled={true}
                  className="w-fit gap-1.5"
                >
                  <ShieldCheck className="w-3.5 h-3.5" />
                  Generate Audit
                </Button>
                <p className="text-xs text-yellow-500/90 mt-1">
                  {auditData.actions.generateAuditUnavailableReason ||
                    "Audit generation is blocked by validation requirements."}
                </p>
              </div>
            ) : (
              <p className="text-xs text-muted-foreground/60 italic">
                {auditData.actions.generateAuditUnavailableReason ||
                  "Audit generation is not available for this run."}
              </p>
            )}
          </div>
        )}
      </Section>

      <Separator />

      {/* Audit Packet */}
      <Section
        title="Audit Packet"
        icon={<ShieldCheck className="w-4 h-4 text-yellow-400" />}
      >
        {auditData.generatedPacket.available ? (
          <div className="flex flex-col gap-1.5">
            <div className="flex items-center gap-2">
              <Badge variant="destructive" className="text-xs">
                Generated
              </Badge>
              <span className="text-xs font-mono text-muted-foreground truncate">
                {auditData.generatedPacket.artifactPath}
              </span>
              {auditData.generatedPacket.generatedAt && (
                <span className="text-[11px] text-muted-foreground/60">
                  {new Date(
                    auditData.generatedPacket.generatedAt,
                  ).toLocaleString()}
                </span>
              )}
            </div>
            {auditData.generatedPacket.preview && (
              <pre className="max-w-full overflow-x-auto text-[11px] font-mono bg-muted/40 p-2.5 rounded border border-border/40 max-h-48 overflow-y-auto whitespace-pre-wrap text-foreground">
                {auditData.generatedPacket.preview}
              </pre>
            )}
            {auditData.generatedPacket.warnings.length > 0 && (
              <div className="flex flex-col gap-0.5 text-xs text-yellow-400/80">
                {auditData.generatedPacket.warnings.map(
                  (w: string, i: number) => (
                    <span
                      key={i}
                      className="flex items-center gap-1 text-[11px]"
                    >
                      <AlertTriangle className="w-3 h-3" />
                      {w}
                    </span>
                  ),
                )}
              </div>
            )}
            {auditData.generatedPacket.decision && (
              <div className="flex items-center gap-2 text-xs">
                <Badge variant="secondary" className="text-xs">
                  Decision: {auditData.generatedPacket.decision}
                </Badge>
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
              <Badge variant="secondary" className="text-xs">
                Manual Submission
              </Badge>
              <span className="text-xs font-mono text-muted-foreground truncate">
                {auditData.manualPacket.artifactPath}
              </span>
            </div>
            {auditData.manualPacket.preview && (
              <pre className="max-w-full overflow-x-auto text-[11px] font-mono bg-muted/40 p-2.5 rounded border border-border/40 max-h-48 overflow-y-auto whitespace-pre-wrap text-foreground">
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
                <p className="text-xs font-medium text-muted-foreground">
                  Submit Manual Audit Packet
                </p>
                <Textarea
                  className="text-xs font-mono min-h-[120px]"
                  placeholder="Paste audit packet markdown content..."
                  value={manualPacketMarkdown}
                  onChange={(e) => setManualPacketMarkdown(e.target.value)}
                />
                <div className="flex flex-col gap-1">
                  <label className="text-[11px] text-muted-foreground">
                    Decision
                  </label>
                  <select
                    className="text-xs bg-background border border-border/60 rounded px-2 py-1.5"
                    value={manualDecision}
                    onChange={(e) => setManualDecision(e.target.value)}
                  >
                    <option value="">Select decision...</option>
                    {RELAY_AUDIT_DECISION_VALUES.map((d) => (
                      <option key={d} value={d}>
                        {d}
                      </option>
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
                    disabled={
                      activeMutation ||
                      !manualDecision ||
                      !manualPacketMarkdown.trim()
                    }
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
                    onClick={() => {
                      setShowManualSubmit(false);
                      setManualPacketMarkdown("");
                      setManualDecision("");
                      setManualNotes("");
                    }}
                    disabled={activeMutation}
                  >
                    Cancel
                  </Button>
                </div>
              </div>
            )}
          </div>
        )}

        {!auditData.generatedPacket.available &&
          !auditData.manualPacket &&
          !auditData.actions.canSubmitManual && (
            <p className="text-xs text-muted-foreground/60 italic mt-1">
              No audit packet available and manual submission is not enabled for
              this run.
            </p>
          )}
      </Section>

      <Separator />

      {/* Audit Decision */}
      <Section
        title="Audit Decision"
        icon={<CheckSquare className="w-4 h-4 text-emerald-400" />}
      >
        <div className="flex items-center gap-2 flex-wrap">
          <Badge
            variant={
              auditData.decision.source === "approved"
                ? "default"
                : auditData.decision.source === "manual"
                  ? "destructive"
                  : auditData.decision.source === "generated"
                    ? "secondary"
                    : "outline"
            }
            className="text-xs"
          >
            {auditData.decision.source === "approved"
              ? "Approved"
              : auditData.decision.source === "manual"
                ? "Manual Decision Submitted"
                : auditData.decision.source === "generated"
                  ? "Generated Recommendation"
                  : "No Decision"}
          </Badge>
          {auditData.decision.currentDecision && (
            <span className="text-xs font-mono text-muted-foreground">
              {auditData.decision.currentDecision}
            </span>
          )}
          {auditData.decision.approvedAt && (
            <span className="text-xs text-muted-foreground/60">
              {new Date(auditData.decision.approvedAt).toLocaleString()}
            </span>
          )}
        </div>
        {auditData.decision.notes && (
          <p className="text-xs text-muted-foreground mt-1">
            {auditData.decision.notes}
          </p>
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
            <p className="text-xs font-medium text-muted-foreground">
              Approve Audit
            </p>
            <div className="flex flex-col gap-1">
              <label className="text-[11px] text-muted-foreground">
                Decision
              </label>
              <select
                className="text-xs bg-background border border-border/60 rounded px-2 py-1.5"
                value={approveDecision}
                onChange={(e) =>
                  setApproveDecision(
                    e.target.value as "accepted" | "accepted_with_warnings",
                  )
                }
              >
                <option value="accepted">Accepted</option>
                <option value="accepted_with_warnings">
                  Accepted with Warnings
                </option>
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
                onClick={() => {
                  setShowApproveForm(false);
                  setApproveNotes("");
                }}
                disabled={activeMutation}
              >
                Cancel
              </Button>
            </div>
          </div>
        )}

        {showRevisionForm && (
          <div className="flex flex-col gap-2 p-3 bg-muted/20 rounded border border-border/40 mt-2">
            <p className="text-xs font-medium text-muted-foreground">
              Request Revision
            </p>
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
                onClick={() => {
                  setShowRevisionForm(false);
                  setRevisionReason("");
                }}
                disabled={activeMutation}
              >
                Cancel
              </Button>
            </div>
          </div>
        )}

        {!auditData.actions.canApproveAudit &&
          !auditData.actions.canRequestRevision &&
          auditData.decision.source === "none" && (
            <p className="text-xs text-muted-foreground/60 italic mt-1">
              Generate an audit packet or submit a manual audit to enable
              decision actions.
            </p>
          )}
      </Section>

      <Separator />

      {/* Warnings / Revision Requirements */}
      {(auditData.warnings.length > 0 ||
        auditData.revisionRequirements.length > 0 ||
        auditData.blockers.length > 0) && (
        <>
          <Section
            title="Warnings / Revision Requirements"
            icon={<AlertTriangle className="w-4 h-4 text-yellow-400" />}
          >
            {auditData.blockers.length > 0 && (
              <div className="flex flex-col gap-1">
                <p className="text-xs font-medium text-red-400">Blockers</p>
                {auditData.blockers.map((b: string, i: number) => (
                  <div
                    key={i}
                    className="flex items-start gap-1.5 text-xs text-red-400/80"
                  >
                    <AlertCircle className="w-3 h-3 shrink-0 mt-0.5" />
                    <span>{b}</span>
                  </div>
                ))}
              </div>
            )}
            {auditData.revisionRequirements.length > 0 && (
              <div className="flex flex-col gap-1 mt-2">
                <p className="text-xs font-medium text-yellow-400">
                  Revision Requirements
                </p>
                {auditData.revisionRequirements.map((r: string, i: number) => (
                  <div
                    key={i}
                    className="flex items-start gap-1.5 text-xs text-yellow-400/80"
                  >
                    <AlertTriangle className="w-3 h-3 shrink-0 mt-0.5" />
                    <span>{r}</span>
                  </div>
                ))}
              </div>
            )}
            {auditData.warnings.length > 0 && (
              <div className="flex flex-col gap-1 mt-2">
                <p className="text-xs font-medium text-muted-foreground">
                  Warnings
                </p>
                {auditData.warnings.map((w: string, i: number) => (
                  <div
                    key={i}
                    className="flex items-start gap-1.5 text-xs text-yellow-400/70"
                  >
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
            <Badge
              variant={
                auditData.commitSummary.commitMessageAvailable
                  ? "default"
                  : "secondary"
              }
              className="text-xs"
            >
              {auditData.commitSummary.commitMessageAvailable
                ? "Prepared"
                : "Not prepared"}
            </Badge>
            {auditData.commitSummary.commitMessageAvailable && (
              <span className="text-xs font-mono text-muted-foreground truncate">
                commit_message.txt
              </span>
            )}
          </div>

          <div className="grid grid-cols-1 gap-2 text-[11px] text-muted-foreground sm:grid-cols-3">
            <div>
              <span className="font-medium">Changed files:</span>{" "}
              {auditData.commitSummary.changedFileArtifactIds.length}
            </div>
            <div>
              <span className="font-medium">Validation:</span>{" "}
              {auditData.commitSummary.validationSummary}
            </div>
            <div>
              <span className="font-medium">Audit:</span>{" "}
              {auditData.commitSummary.auditDecisionSummary}
            </div>
          </div>

          {auditData.commitSummary.commitMessagePreview && (
            <pre className="max-w-full overflow-x-auto text-[11px] font-mono bg-muted/40 p-2.5 rounded border border-border/40 max-h-32 overflow-y-auto whitespace-pre-wrap text-foreground">
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
            Preparing a commit message only writes a suggested artifact. No git
            commit, push, or staging occurs.
          </p>
        </div>
      </Section>

      <Separator />

      {/* Close Run */}
      <Section title="Close Run" icon={<Terminal className="w-4 h-4" />}>
        <div className="flex flex-col gap-1.5">
          <div className="flex items-center gap-2">
            <Badge
              variant={
                run.lifecycleState === "completed"
                  ? "default"
                  : auditData.actions.canCloseRun
                    ? "destructive"
                    : "secondary"
              }
              className="text-xs"
            >
              {run.lifecycleState === "completed"
                ? "Closed"
                : auditData.actions.canCloseRun
                  ? "Ready to Close"
                  : auditData.decision.source === "approved"
                    ? "Approved"
                    : "Pending"}
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

          {auditData.actions.closeRunUnavailableReason &&
            run.lifecycleState !== "completed" && (
              <p className="text-xs text-muted-foreground/60 italic mt-1 flex items-center gap-1">
                <AlertTriangle className="w-3 h-3" />
                {auditData.actions.closeRunUnavailableReason}
              </p>
            )}

          {run.lifecycleState === "completed" && (
            <p className="text-xs text-emerald-400/70 mt-1">
              Run is closed. All artifacts and evidence are preserved.
            </p>
          )}

          <p className="text-[11px] text-muted-foreground/50 italic mt-1">
            Closing a run updates Relay run state only. No git commit, push, or
            repo mutation occurs.
          </p>
        </div>
      </Section>
    </div>
  );
}

function AuditDetailsPanel({
  run,
  artifacts,
  events,
}: {
  run: RelayRun;
  artifacts: RelayArtifact[];
  events: RelayRunEvent[];
}) {
  const auditData = deriveAuditData(run, artifacts, events);
  const runStatus = (run.status || "") as string;
  const { hasFinalValidationEvidence, validationAllowsAudit } =
    evaluateValidationGate(
      artifacts as Array<{ storageKind: string }>,
      runStatus,
    );
  const primaryValidationArtifact =
    artifacts.find((artifact) => artifact.storageKind === "validation_run_json") ||
    artifacts.find(
      (artifact) => artifact.storageKind === "validation_failure_acceptance_json",
    ) ||
    artifacts.find((artifact) => artifact.storageKind === "validation_stdout");
  const primaryEvidenceArtifact =
    artifacts.find((artifact) => artifact.kind === "result") ||
    artifacts.find((artifact) => artifact.kind === "diff") ||
    artifacts.find((artifact) => artifact.kind === "validation");
  const primaryAuditPacketArtifact =
    artifacts.find(
      (artifact) =>
        artifact.kind === "audit" &&
        (artifact.filename?.includes("audit_packet") ||
          artifact.path?.includes("audit_packet") ||
          artifact.label === "Audit Packet"),
    ) || artifacts.find((artifact) => artifact.id === auditData.manualPacket?.artifactId);
  const primaryCommitArtifact = artifacts.find(
    (artifact) =>
      artifact.kind === "audit" &&
      (artifact.filename?.includes("commit_message") ||
        artifact.path?.includes("commit_message")),
  );

  return (
    <div className="flex flex-col gap-3">
      <RunStageInspectorSection title="Run State">
        <RunStageKeyValueRow label="Status" value={runStatus} mono />
        <RunStageKeyValueRow
          label="Lifecycle"
          value={run.lifecycleState || "-"}
          mono
        />
        <RunStageKeyValueRow label="Active step" value="Audit / Closeout" />
      </RunStageInspectorSection>

      <RunStageInspectorSection title="Validation">
        <RunStageKeyValueRow
          label="Gate"
          value={getAuditValidationLabel(
            runStatus,
            hasFinalValidationEvidence,
            validationAllowsAudit,
          )}
        />
        <RunStageKeyValueRow
          label="Evidence"
          value={formatArtifactCount(
            artifacts.filter(
              (artifact) =>
                artifact.kind === "validation" ||
                String(artifact.storageKind || "").startsWith("validation_"),
            ).length,
          )}
        />
        <RunStageKeyValueRow
          label="Report"
          value={formatArtifactLocation(primaryValidationArtifact)}
          mono
        />
      </RunStageInspectorSection>

      <RunStageInspectorSection title="Evidence">
        <RunStageKeyValueRow
          label="Input summary"
          value={
            auditData.inputSummary.available
              ? auditData.inputSummary.artifactPath
              : "Pending"
          }
          mono
        />
        <RunStageKeyValueRow
          label="Captured"
          value={formatArtifactCount(auditData.inputSummary.evidenceIncluded.length)}
        />
        <RunStageKeyValueRow
          label="Primary"
          value={formatArtifactLocation(primaryEvidenceArtifact)}
          mono
        />
      </RunStageInspectorSection>

      <RunStageInspectorSection title="Audit Packet">
        <RunStageKeyValueRow
          label="State"
          value={getAuditPacketLabel(auditData)}
        />
        <RunStageKeyValueRow
          label="Source"
          value={
            auditData.manualPacket
              ? "Manual submission present"
              : auditData.generatedPacket.available
                ? "Generated packet"
                : "Not available"
          }
        />
        <RunStageKeyValueRow
          label="Artifact"
          value={
            auditData.manualPacket?.artifactPath ||
            auditData.generatedPacket.artifactPath ||
            formatArtifactLocation(primaryAuditPacketArtifact)
          }
          mono
        />
      </RunStageInspectorSection>

      <RunStageInspectorSection title="Decision">
        <RunStageKeyValueRow
          label="State"
          value={getAuditDecisionLabel(runStatus, auditData)}
        />
        <RunStageKeyValueRow
          label="Source"
          value={formatAuditDecisionSource(auditData.decision.source)}
        />
        <RunStageKeyValueRow
          label="Notes"
          value={auditData.decision.notes || "—"}
        />
      </RunStageInspectorSection>

      <RunStageInspectorSection title="Closeout">
        <RunStageKeyValueRow
          label="Commit msg"
          value={
            auditData.commitSummary.commitMessageAvailable ? "Prepared" : "Pending"
          }
        />
        <RunStageKeyValueRow
          label="Artifact"
          value={formatArtifactLocation(primaryCommitArtifact)}
          mono
        />
        <RunStageKeyValueRow
          label="Close run"
          value={run.lifecycleState === "completed" ? "Completed" : "Pending"}
        />
      </RunStageInspectorSection>
    </div>
  );
}

function getAuditValidationLabel(
  runStatus: string,
  hasFinalValidationEvidence: boolean,
  validationAllowsAudit: boolean,
): string {
  if (runStatus === "local_validation_running") return "Running";
  if (runStatus === "validation_failed") return "Failed";
  if (runStatus === "validation_failed_accepted") return "Accepted failure";
  if (runStatus === "validation_passed" || validationAllowsAudit) return "Passed";
  if (!hasFinalValidationEvidence) return "Required";
  return "Review required";
}

function getAuditValidationTone(
  runStatus: string,
  hasFinalValidationEvidence: boolean,
  validationAllowsAudit: boolean,
): "default" | "info" | "success" | "warning" | "danger" {
  if (runStatus === "local_validation_running") return "info";
  if (runStatus === "validation_failed") return "danger";
  if (runStatus === "validation_failed_accepted") return "warning";
  if (runStatus === "validation_passed" || validationAllowsAudit) return "success";
  if (!hasFinalValidationEvidence) return "warning";
  return "default";
}

function getAuditPacketLabel(
  auditData: ReturnType<typeof deriveAuditData>,
): string {
  if (auditData.manualPacket) return "Manual packet ready";
  if (auditData.generatedPacket.available) return "Generated packet ready";
  if (auditData.inputSummary.available) return "Ready to generate";
  return "Pending";
}

function getAuditPacketTone(
  auditData: ReturnType<typeof deriveAuditData>,
): "default" | "success" | "warning" {
  if (auditData.manualPacket) return "warning";
  if (auditData.generatedPacket.available) return "success";
  return "default";
}

function getAuditDecisionLabel(
  runStatus: string,
  auditData: ReturnType<typeof deriveAuditData>,
): string {
  if (runStatus === "completed") return "Run closed";
  if (runStatus === "accepted_with_warnings") return "Accepted with warnings";
  if (runStatus === "accepted") return "Accepted";
  if (runStatus === "revision_required") return "Revision required";
  if (auditData.decision.currentDecision) return auditData.decision.currentDecision;
  if (auditData.decision.source === "generated") return "Awaiting review";
  if (auditData.decision.source === "manual") return "Manual packet submitted";
  return "Pending";
}

function getAuditDecisionTone(
  runStatus: string,
  auditData: ReturnType<typeof deriveAuditData>,
): "default" | "success" | "warning" {
  if (runStatus === "accepted" || runStatus === "completed") return "success";
  if (
    runStatus === "accepted_with_warnings" ||
    runStatus === "revision_required" ||
    auditData.decision.source === "manual"
  ) {
    return "warning";
  }
  return "default";
}

function formatAuditDecisionSource(
  source: RelayAuditDecisionStatus["source"],
): string {
  switch (source) {
    case "approved":
      return "Approved in Relay";
    case "generated":
      return "Generated packet";
    case "manual":
      return "Manual packet";
    case "none":
      return "No decision";
  }
}

function formatArtifactCount(count: number): string {
  if (count === 1) return "1 artifact";
  return `${count} artifacts`;
}

function formatArtifactLocation(artifact?: RelayArtifact): string {
  return artifact?.path || artifact?.filename || "-";
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
      <CardHeader className="p-3 pb-2">
        <CardTitle className="text-sm font-medium flex items-center gap-2">
          {icon}
          {title}
        </CardTitle>
      </CardHeader>
      <CardContent className="min-w-0 p-3 pt-0 flex flex-col gap-1.5">
        {children}
      </CardContent>
    </Card>
  );
}
