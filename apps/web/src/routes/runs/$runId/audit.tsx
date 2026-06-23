import { createFileRoute } from "@tanstack/react-router";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { useState, useMemo } from "react";
import {
  runDetailQueryOptions,
  runArtifactsQueryOptions,
  runEventsQueryOptions,
  auditStatusQueryOptions,
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
  RelayAuditStatus,
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
  RunStageSummaryChip,
  RunStageContentSection,
  RunStageEvidenceRow,
  RunStageEvidenceList,
  RunStageFindingRow,
  RunStageFindingList,
  RunStageMainStack,
} from "@/components/relay/RunStagePrimitives";
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
  CheckCircle2,
  Send,
  RefreshCw,
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
  const { data: auditStatus, error: auditStatusError } = useQuery(
    auditStatusQueryOptions(runId),
  );

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
          auditStatus={auditStatus}
          auditStatusError={auditStatusError ? String(auditStatusError) : null}
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
            auditStatus={auditStatus}
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
  auditStatus?: RelayAuditStatus,
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
  evidenceManifestArtifact?: RelayArtifact;
  decisionArtifact?: RelayArtifact;
  localOnly: boolean;
  auditState?: RelayAuditStatus["auditState"];
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
  const fallbackGeneratedPacket = packetArts.find(
    (a: any) => !a.filename?.includes("manual") && !a.path?.includes("manual"),
  );
  const fallbackManualPacket = packetArts.find(
    (a: any) => a.filename?.includes("manual") || a.path?.includes("manual"),
  );
  const genPacketArt =
    auditStatus?.generatedAuditPacketArtifact || fallbackGeneratedPacket;
  const manualPacketArt =
    auditStatus?.manualAuditPacketArtifact || fallbackManualPacket;
  const evidenceManifestArtifact = auditStatus?.evidenceManifestArtifact;
  const decisionArtifact = auditStatus?.decisionArtifact;

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

  const parsedDecision = parseDecisionArtifactPreview(decisionArtifact);
  const decisionValue: RelayAuditDecisionValue | undefined =
    parsedDecision.currentDecision ||
    (runStatus === "accepted_with_warnings"
      ? "accepted_with_warnings"
      : isAccepted
        ? "accepted"
        : undefined);
  const decisionSource: RelayAuditDecisionStatus["source"] = decisionArtifact
    ? "approved"
    : manualPacketArt
      ? "manual"
      : hasGenerateEvent || genPacketArt
        ? "generated"
        : "none";
  const warnings =
    auditStatus?.warnings ||
    (run.validationSummary?.warnings > 0
      ? (run.validationSummary?.issues || [])
          .filter((i: any) => i.severity === "warning")
          .map((i: any) => i.message)
      : []);
  const revisionRequirements =
    auditStatus?.revisionRequirements ||
    (hasRevisionEvent
      ? events
          .filter((e: any) => e.message?.includes("Audit revision requested"))
          .map((e: any) => e.message)
      : []);
  const blockers =
    auditStatus?.blockers ||
    (isBlocked
      ? ["Run is blocked and cannot proceed to close."]
      : isRevisionRequired
        ? ["Revision requested — audit must be regenerated."]
        : []);
  const canSubmitManual = auditStatus?.canSubmitDecision ?? (isAuditReady && !isAccepted && !isCompleted && !isRevisionRequired);
  const canApproveAudit = auditStatus?.canApprove ?? ((hasGenerateEvent || hasManualSubmitEvent) &&
    isAuditReady &&
    !isCompleted &&
    !isRevisionRequired);
  const canRequestRevision = auditStatus?.canRequestRevision ?? (isAuditReady && !isCompleted && !isRevisionRequired);
  const canCloseRun = auditStatus?.canCloseRun ?? (isAccepted && !isCompleted);
  const canGenerateAudit = auditStatus?.canGenerateAudit ?? (isAuditCandidate && !isCompleted && validationAllowsAudit);

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
      missingEvidence: blockers.filter((blocker) =>
        blocker.toLowerCase().includes("evidence"),
      ),
    },
    generatedPacket: {
      artifactId: genPacketArt?.id || "",
      artifactPath: genPacketArt?.path || "",
      available: !!genPacketArt,
      isManual: false,
      generatedAt: genPacketArt?.createdAt,
      preview: genPacketArt?.preview,
      warnings,
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
      source: decisionSource,
      approvedAt: decisionArtifact?.createdAt || run.updatedAt,
      notes: parsedDecision.notes || run.approvalGate?.note,
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
      canGenerateAudit,
      canSubmitManual,
      canApproveAudit,
      canRequestRevision,
      canPrepareCommitMessage: isAccepted && !isCompleted,
      canCloseRun,
      generateAuditUnavailableReason: !isAuditCandidate
        ? `Current status: ${runStatus}. Audit generation is not available for this lifecycle stage.`
        : isCompleted
          ? undefined
          : !canGenerateAudit
            ? `Current status: ${runStatus}. Audit generation requires a passed or accepted-failed local validation result.`
            : undefined,
      submitManualUnavailableReason:
        !canSubmitManual
          ? `Current status: ${runStatus}`
          : undefined,
      approveAuditUnavailableReason:
        !canApproveAudit
          ? `Current status: ${runStatus}`
          : undefined,
      requestRevisionUnavailableReason:
        !canRequestRevision
          ? `Current status: ${runStatus}`
          : undefined,
      prepareCommitMessageUnavailableReason: !isAccepted
        ? `Run must be in accepted or accepted_with_warnings status. Current: ${runStatus}`
        : undefined,
      closeRunUnavailableReason: !isAccepted
        ? `Audit must be approved first (status must be accepted or accepted_with_warnings). Current: ${runStatus}`
          : undefined,
    },
    warnings,
    revisionRequirements,
    blockers,
    evidenceManifestArtifact,
    decisionArtifact,
    localOnly: auditStatus?.localOnly ?? true,
    auditState: auditStatus?.auditState,
  };
}

function parseDecisionArtifactPreview(artifact?: RelayArtifact): {
  currentDecision?: RelayAuditDecisionValue;
  notes?: string;
} {
  if (!artifact?.preview) {
    return {};
  }
  try {
    const parsed = JSON.parse(artifact.preview);
    return {
      currentDecision: parsed?.decision as RelayAuditDecisionValue | undefined,
      notes: typeof parsed?.notes === "string" ? parsed.notes : undefined,
    };
  } catch {
    return {};
  }
}

function AuditMainContent({
  run,
  artifacts,
  events,
  auditStatus,
  auditStatusError,
}: {
  run: RelayRun;
  artifacts: RelayArtifact[];
  events: RelayRunEvent[];
  auditStatus?: RelayAuditStatus;
  auditStatusError: string | null;
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
    () => deriveAuditData(run, artifacts, events, auditStatus),
    [run, artifacts, events, auditStatus],
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
  };  return (
    <RunStageMainStack>
      {mutationError && (
        <RelayStateBanner
          tone="danger"
          title="Audit action failed"
          description={mutationError}
        />
      )}

      {auditStatusError && (
        <RelayStateBanner
          tone="warning"
          title="Structured audit status unavailable"
          description="Relay fell back to artifact-based audit details for this view."
          metadata={auditStatusError}
        />
      )}

      <RunStageStateCard
        tone={auditStateCardCopy.tone}
        eyebrow={auditStateCardCopy.eyebrow}
        title={auditStateCardCopy.title}
        message={auditStateCardCopy.message}
      >
        <div className="flex flex-wrap gap-2">
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
      </RunStageStateCard>

      <RunStageContentSection
        eyebrow="Audit / Closeout Pipeline"
        title="Closeout progression"
        description="Executor evidence, validation review, audit packet preparation, approval, and explicit run closeout."
      >
        <RunStagePipeline
          steps={AUDIT_PIPELINE_STEPS}
          statuses={auditPipelineStatuses}
        />
      </RunStageContentSection>

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
      <RunStageContentSection
        eyebrow="Validation"
        title="Local Validation Required"
        description="Run local validation before generating the audit packet."
      >
        <div className="flex flex-col gap-3">
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
            <p className="text-xs text-destructive">
              Local validation failed. Review validation artifacts before
              continuing.
            </p>
          )}
          {localValidationPassed && (
            <p className="text-xs text-success">Local validation passed.</p>
          )}
          {localValidationAccepted && (
            <p className="text-xs text-warning">
              Local validation failed but was accepted.
            </p>
          )}

          {/* Validation Artifact Links */}
          {(validationRunJsonArt ||
            validationFailureAcceptanceJsonArt ||
            validationProgressJsonArt ||
            validationStdoutArt ||
            validationStderrArt) && (
            <RunStageEvidenceList className="mt-2 border-t border-[var(--relay-row-border)] pt-3">
              <span className="text-[11px] font-medium text-muted-foreground">
                Validation Evidence:
              </span>
              {validationRunJsonArt && (
                <RunStageEvidenceRow
                  label="Validation Result"
                  value={
                    <ArtifactPreviewCard
                      artifact={validationRunJsonArt}
                      runId={runId}
                      className="max-w-xs"
                    />
                  }
                />
              )}
              {validationFailureAcceptanceJsonArt && (
                <RunStageEvidenceRow
                  label="Failure Acceptance"
                  value={
                    <ArtifactPreviewCard
                      artifact={validationFailureAcceptanceJsonArt}
                      runId={runId}
                      className="max-w-xs"
                    />
                  }
                />
              )}
              {validationProgressJsonArt && (
                <RunStageEvidenceRow
                  label="Validation Progress"
                  value={
                    <ArtifactPreviewCard
                      artifact={validationProgressJsonArt}
                      runId={runId}
                      className="max-w-xs"
                    />
                  }
                />
              )}
              {validationStdoutArt && (
                <RunStageEvidenceRow
                  label="Validation Output"
                  value={
                    <ArtifactPreviewCard
                      artifact={validationStdoutArt}
                      runId={runId}
                      className="max-w-xs"
                    />
                  }
                />
              )}
              {validationStderrArt && (
                <RunStageEvidenceRow
                  label="Validation Error Output"
                  value={
                    <ArtifactPreviewCard
                      artifact={validationStderrArt}
                      runId={runId}
                      className="max-w-xs"
                    />
                  }
                />
              )}
            </RunStageEvidenceList>
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
            <div className="flex flex-col gap-2 mt-2 border-t border-[var(--relay-row-border)] pt-3">
              <span className="text-[11px] font-medium text-muted-foreground">
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
                  <AlertTriangle className="w-3.5 h-3.5 text-warning" />
                  Accept Failure...
                </Button>
              ) : (
                <div className="flex flex-col gap-2 p-3 bg-warning/5 border border-warning/30 rounded">
                  <span className="text-xs font-medium text-warning">
                    Provide Acceptance Reason:
                  </span>
                  <Textarea
                    placeholder="Enter reason for accepting this validation failure..."
                    value={acceptanceReason}
                    onChange={(e) => setAcceptanceReason(e.target.value)}
                    className="text-xs h-16 bg-background border-[var(--relay-row-border)] focus-visible:ring-warning"
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
      </RunStageContentSection>

      {/* Audit Input Summary */}
      <RunStageContentSection
        eyebrow="Evidence"
        title="Audit Input Summary"
        description="Summary of executor outputs and evidence prepared for the audit packet."
      >
        {auditData.inputSummary.available ? (
          <div className="flex flex-col gap-3">
            <div className="flex items-center gap-2">
              <CheckCircle2 className="w-3.5 h-3.5 text-success" />
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
              <pre className="max-w-full overflow-x-auto text-[11px] font-mono bg-muted/40 p-2.5 rounded border border-[var(--relay-row-border)] max-h-48 overflow-y-auto whitespace-pre-wrap text-foreground">
                {auditData.inputSummary.preview}
              </pre>
            )}
            <div className="flex flex-col gap-1.5 text-xs text-muted-foreground">
              <span className="text-[11px] font-medium text-muted-foreground">
                Evidence included:
              </span>
              {auditData.inputSummary.evidenceIncluded.length > 0 ? (
                <div className="flex flex-col gap-1">
                  {auditData.inputSummary.evidenceIncluded
                    .slice(0, 10)
                    .map((e: string, i: number) => (
                      <span
                        key={i}
                        className="flex items-center gap-1 text-[11px]"
                      >
                        <ShieldCheck className="w-3 h-3 text-success" />
                        {e}
                      </span>
                    ))}
                </div>
              ) : (
                <span className="italic text-[11px] text-muted-foreground/50">
                  No evidence artifacts found.
                </span>
              )}
            </div>
            {auditData.inputSummary.missingEvidence.length > 0 && (
              <div className="flex flex-col gap-1 text-xs text-warning">
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
                  <p className="text-xs text-warning mt-1">
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
                <p className="text-xs text-warning mt-1">
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
      </RunStageContentSection>

      {/* Local Audit Evidence */}
      <RunStageContentSection
        eyebrow="Evidence"
        title="Local Audit Evidence"
        description="Audit evidence is derived from Relay artifacts only."
      >
        <div className="flex flex-col gap-3">
          <div className="flex items-center gap-2 flex-wrap">
            <Badge variant="default" className="text-xs">
              Local only
            </Badge>
            <span className="text-xs text-muted-foreground">
              GitHub PRs, CI, and Actions are not used.
            </span>
          </div>
          <div className="grid grid-cols-1 gap-4 text-xs text-muted-foreground sm:grid-cols-2">
            <RunStageKeyValueRow
              label="Workflow state"
              value={auditData.auditState || "fallback"}
            />
            <RunStageKeyValueRow
              label="Evidence manifest"
              value={auditData.evidenceManifestArtifact?.filename || "Pending"}
              mono
            />
          </div>
          {auditData.evidenceManifestArtifact && (
            <ArtifactPreviewCard
              artifact={auditData.evidenceManifestArtifact}
              runId={runId}
              className="max-w-full"
            />
          )}
        </div>
      </RunStageContentSection>

      {/* Audit Packet */}
      <RunStageContentSection
        eyebrow="Packet"
        title="Audit Packet"
        description="The generated or manual audit packet review."
      >
        <div className="flex flex-col gap-3">
          {auditData.generatedPacket.available ? (
            <div className="flex flex-col gap-2">
              <div className="flex items-center gap-2">
                <Badge variant="default" className="text-xs bg-emerald-600 hover:bg-emerald-700 text-black">
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
                <pre className="max-w-full overflow-x-auto text-[11px] font-mono bg-muted/40 p-2.5 rounded border border-[var(--relay-row-border)] max-h-48 overflow-y-auto whitespace-pre-wrap text-foreground">
                  {auditData.generatedPacket.preview}
                </pre>
              )}
              {auditData.generatedPacket.warnings.length > 0 && (
                <div className="flex flex-col gap-1 text-xs text-warning">
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
            <div className="flex flex-col gap-2 mt-3 pt-3 border-t border-[var(--relay-row-border)]">
              <div className="flex items-center gap-2">
                <Badge variant="secondary" className="text-xs">
                  Manual Submission
                </Badge>
                <span className="text-xs font-mono text-muted-foreground truncate">
                  {auditData.manualPacket.artifactPath}
                </span>
              </div>
              {auditData.manualPacket.preview && (
                <pre className="max-w-full overflow-x-auto text-[11px] font-mono bg-muted/40 p-2.5 rounded border border-[var(--relay-row-border)] max-h-48 overflow-y-auto whitespace-pre-wrap text-foreground">
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
                  Submit Manual Audit Decision
                </Button>
              ) : (
                <div className="flex flex-col gap-2 p-3 bg-muted/20 rounded border border-[var(--relay-row-border)]">
                  <p className="text-xs font-medium text-muted-foreground">
                    Submit Manual Audit Decision
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
                      className="text-xs bg-background border border-[var(--relay-row-border)] rounded px-2 py-1.5 text-foreground"
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
        </div>
      </RunStageContentSection>

      {/* Warnings / Revision Requirements */}
      {(auditData.warnings.length > 0 ||
        auditData.revisionRequirements.length > 0 ||
        auditData.blockers.length > 0) && (
        <RunStageContentSection
          eyebrow="Findings"
          title="Warnings & Revision Requirements"
          description="Outstanding blockers, revision requirements, or warnings that must be reviewed."
        >
          <RunStageFindingList>
            {auditData.blockers.map((b: string, i: number) => (
              <RunStageFindingRow
                key={`blocker-${i}`}
                severity="error"
                message={b}
              />
            ))}
            {auditData.revisionRequirements.map((r: string, i: number) => (
              <RunStageFindingRow
                key={`rev-${i}`}
                severity="warning"
                message={r}
              />
            ))}
            {auditData.warnings.map((w: string, i: number) => (
              <RunStageFindingRow
                key={`warn-${i}`}
                severity="info"
                message={w}
              />
            ))}
          </RunStageFindingList>
        </RunStageContentSection>
      )}

      {/* Audit Decision */}
      <RunStageContentSection
        eyebrow="Decision"
        title="Audit Decision Actions"
        description="Accept the audit candidate and record final decision notes, or request revision."
      >
        <div className="flex flex-col gap-3">
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

          <div className="flex items-center gap-2 flex-wrap mt-1">
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
            <div className="flex flex-col gap-2 p-3 bg-muted/20 rounded border border-[var(--relay-row-border)] mt-2">
              <p className="text-xs font-medium text-muted-foreground">
                Approve Audit
              </p>
              <div className="flex flex-col gap-1">
                <label className="text-[11px] text-muted-foreground">
                  Decision
                </label>
                <select
                  className="text-xs bg-background border border-[var(--relay-row-border)] rounded px-2 py-1.5 text-foreground"
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
            <div className="flex flex-col gap-2 p-3 bg-muted/20 rounded border border-[var(--relay-row-border)] mt-2">
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
        </div>
      </RunStageContentSection>

      {/* Commit Summary */}
      <RunStageContentSection
        eyebrow="Closeout"
        title="Post-Audit Closeout: Commit Summary"
        description="Prepare a suggested commit message artifact for review. No git operations occur."
      >
        <div className="flex flex-col gap-3">
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

          <div className="grid grid-cols-1 gap-4 text-xs text-muted-foreground sm:grid-cols-3">
            <RunStageKeyValueRow
              label="Changed files"
              value={String(auditData.commitSummary.changedFileArtifactIds.length)}
            />
            <RunStageKeyValueRow
              label="Validation"
              value={auditData.commitSummary.validationSummary}
            />
            <RunStageKeyValueRow
              label="Audit"
              value={auditData.commitSummary.auditDecisionSummary}
            />
          </div>

          {auditData.commitSummary.commitMessagePreview && (
            <pre className="max-w-full overflow-x-auto text-[11px] font-mono bg-muted/40 p-2.5 rounded border border-[var(--relay-row-border)] max-h-32 overflow-y-auto whitespace-pre-wrap text-foreground">
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
        </div>
      </RunStageContentSection>

      {/* Close Run */}
      <RunStageContentSection
        eyebrow="Closeout"
        title="Post-Audit Closeout: Close Run"
        description="Mark the run as complete. This updates Relay state only and preserves all evidence."
      >
        <div className="flex flex-col gap-3">
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
                <AlertTriangle className="w-3 h-3 text-warning" />
                {auditData.actions.closeRunUnavailableReason}
              </p>
            )}

          {run.lifecycleState === "completed" && (
            <p className="text-xs text-success mt-1">
              Run is closed. All artifacts and evidence are preserved.
            </p>
          )}
        </div>
      </RunStageContentSection>
    </RunStageMainStack>
  );
}

function AuditDetailsPanel({
  run,
  artifacts,
  events,
  auditStatus,
}: {
  run: RelayRun;
  artifacts: RelayArtifact[];
  events: RelayRunEvent[];
  auditStatus?: RelayAuditStatus;
}) {
  const auditData = deriveAuditData(run, artifacts, events, auditStatus);
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
          label="Manifest"
          value={formatArtifactLocation(auditData.evidenceManifestArtifact)}
          mono
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
          value={auditData.localOnly ? "Local-only artifacts" : "Not available"}
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
          label="Workflow"
          value={auditData.auditState || "-"}
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

