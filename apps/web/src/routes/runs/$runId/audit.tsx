import { createFileRoute } from "@tanstack/react-router";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import {
  runDetailQueryOptions,
  runArtifactsQueryOptions,
  runEventsQueryOptions,
  auditStatusQueryOptions,
  validateRun,
  submitManualAuditPacket,
  approveAudit,
  requestAuditRevision,
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
import { RunStatusTrackerLayout } from "@/components/relay/RunStatusTrackerLayout";
import {
  RunWorkbenchLoadFailedState,
  RunWorkbenchLoadingState,
} from "@/components/relay/RunWorkbenchStates";
import { ValidationPanel } from "@/components/relay/ValidationPanel";
import { ArtifactPreviewCard } from "@/components/relay/ArtifactPreviewCard";
import {
  RunStageEvidenceRow,
  RunStageEvidenceList,
  RunStageFindingRow,
  RunStageFindingList,
  RunStageKeyValueRow,
} from "@/components/relay/RunStagePrimitives";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import { AlertTriangle, Loader2, RefreshCw } from "lucide-react";
import { getAuditDisplayState } from "./runAuditVisualState";
import { deriveCurrentStatusText } from "@/features/relay-runs/deriveCurrentStatusText";
import { deriveProgressionLog } from "@/features/relay-runs/deriveProgressionLog";
import { deriveAuditActions } from "@/features/relay-runs/runStepActions";
import { resolvePlanPassLink } from "@/features/relay-runs/planPassLink";
import { AUDIT_ACTION_HANDLERS } from "@/features/relay-runs/runStepActionHandlers";
import type { DetailSection } from "@/features/relay-runs/runStatusTrackerViews";

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
  const {
    data: events,
    isLoading: isLoadingEvents,
    error: errorEvents,
  } = useQuery(runEventsQueryOptions(runId));
  const { data: auditStatus } = useQuery(auditStatusQueryOptions(runId));

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

  return (
    <AuditPageBody
      run={run}
      artifacts={resolvedArtifacts}
      events={resolvedEvents}
      auditStatus={auditStatus}
      eventsLoadFailed={Boolean(errorEvents)}
    />
  );
}

function AuditPageBody({
  run,
  artifacts,
  events,
  auditStatus,
  eventsLoadFailed,
}: {
  run: RelayRun;
  artifacts: RelayArtifact[];
  events: RelayRunEvent[];
  auditStatus?: RelayAuditStatus;
  eventsLoadFailed: boolean;
}) {
  // ------------------------------------------------------------
  // Run Status Tracker Redesign — the Visual_State_Module display-state
  // derivation stays here (single source of truth, unchanged) and now
  // feeds `deriveCurrentStatusText` instead of a state card + badge trio.
  // `RunStatusTrackerLayout` owns rendering the five tracker regions;
  // nothing below duplicates status, position, or action buttons.
  // ------------------------------------------------------------
  const queryClient = useQueryClient();
  const { runId } = Route.useParams();
  const [pendingActionId, setPendingActionId] = useState<string | null>(null);

  // Approve / Request Revision / Submit Manual all require Operator-entered
  // form data (decision/notes/reason/markdown) that cannot be derived from
  // just a run id, so they open a dialog rather than firing a mutation
  // directly from `onActionClick`.
  const [showApproveForm, setShowApproveForm] = useState(false);
  const [showRevisionForm, setShowRevisionForm] = useState(false);
  const [showManualSubmit, setShowManualSubmit] = useState(false);

  // Local-validation ("Run Validation" / "Accept Failed Validation") is not
  // part of `RelayAuditActions`'s Action_Gating_Flag set, so it is not
  // surfaced through Next_Action_Area — it lives inside the "Validation
  // report" Detail_Section instead (see `ValidationReportDetail` below).
  const [showAcceptanceForm, setShowAcceptanceForm] = useState(false);
  const [acceptanceReason, setAcceptanceReason] = useState("");

  const [manualDecision, setManualDecision] = useState<string>("");
  const [manualPacketMarkdown, setManualPacketMarkdown] = useState("");
  const [manualNotes, setManualNotes] = useState("");
  const [approveDecision, setApproveDecision] = useState<
    "accepted" | "accepted_with_warnings"
  >("accepted");
  const [approveNotes, setApproveNotes] = useState("");
  const [revisionReason, setRevisionReason] = useState("");

  const auditData = deriveAuditData(run, artifacts, events, auditStatus);

  const runStatus = (run.status || "") as string;
  const { hasFinalValidationEvidence, validationAllowsAudit } =
    evaluateValidationGate(
      artifacts as Array<{ storageKind: string }>,
      runStatus,
    );
  const isAuditReadyStatus =
    runStatus === "audit_ready" || runStatus === "audit_ready_for_review";
  const isAccepted =
    runStatus === "accepted" || runStatus === "accepted_with_warnings";
  const isCompleted =
    runStatus === "completed" || run.lifecycleState === "completed";
  const isRevisionRequired = runStatus === "revision_required";

  const invalidate = () => {
    void queryClient.invalidateQueries({ queryKey: ["relay-runs"] });
  };

  const validateMutation = useMutation({
    mutationFn: () => validateRun(runId),
    onSuccess: invalidate,
  });

  const approveMutation = useMutation({
    mutationFn: () =>
      approveAudit(runId, { decision: approveDecision, notes: approveNotes }),
    onSuccess: () => {
      setShowApproveForm(false);
      setApproveNotes("");
      invalidate();
    },
  });

  const revisionMutation = useMutation({
    mutationFn: () => requestAuditRevision(runId, { reason: revisionReason }),
    onSuccess: () => {
      setShowRevisionForm(false);
      setRevisionReason("");
      invalidate();
    },
  });

  const submitManualMutation = useMutation({
    mutationFn: () =>
      submitManualAuditPacket(runId, {
        audit_packet_markdown: manualPacketMarkdown,
        decision: manualDecision as RelayAuditDecisionValue,
        notes: manualNotes,
      }),
    onSuccess: () => {
      setShowManualSubmit(false);
      setManualPacketMarkdown("");
      setManualDecision("");
      setManualNotes("");
      invalidate();
    },
  });

  const acceptFailureMutation = useMutation({
    mutationFn: (reason: string) => acceptFailedValidation(runId, reason),
    onSuccess: () => {
      setShowAcceptanceForm(false);
      setAcceptanceReason("");
      invalidate();
    },
  });

  const auditVisualStateInput = {
    run,
    hasFinalValidationEvidence,
    validationAllowsAudit,
    hasAuditPacket:
      auditData.generatedPacket.available || Boolean(auditData.manualPacket),
    hasInputSummary: auditData.inputSummary.available,
    hasWarnings:
      auditData.warnings.length > 0 ||
      auditData.generatedPacket.warnings.length > 0,
    generatePending: pendingActionId === "generateAudit",
    validatePending: validateMutation.isPending,
    manualSubmitPending: submitManualMutation.isPending,
    approvePending: approveMutation.isPending,
    revisionPending: revisionMutation.isPending,
    commitMessagePending: pendingActionId === "prepareCommitMessage",
    closePending: pendingActionId === "closeRun",
    acceptFailurePending: acceptFailureMutation.isPending,
    isAuditCandidate: isAuditCandidateStatus(runStatus),
    isAuditReady: isAuditReadyStatus,
    isAccepted,
    isCompleted,
    isRevisionRequired,
    hasRevisionRequirements: auditData.revisionRequirements.length > 0,
    hasBlockers: auditData.blockers.length > 0,
  };
  const auditDisplayState = getAuditDisplayState(auditVisualStateInput);

  const currentStatus = deriveCurrentStatusText("audit", auditDisplayState, {
    updatedAt: run.updatedAt,
    blockerCount: auditData.blockers.length,
    revisionRequirementCount: auditData.revisionRequirements.length,
    warningCount: auditData.warnings.length,
  });
  const progression = deriveProgressionLog(events);
  const stepActionsView = deriveAuditActions(auditData.actions);

  // approveAudit/requestRevision/submitManual open a form dialog instead of
  // invoking a mutation directly (they require Operator-entered data).
  // Every other id maps onto `AUDIT_ACTION_HANDLERS` and is invoked with
  // just the run id; the returned Promise is passed straight back so
  // `RunStatusTrackerLayout`'s built-in tone-escalation (Requirement 6.6)
  // handles the failure banner — no separate manual error-banner state.
  const onActionClick = (id: string) => {
    if (id === "approveAudit") {
      setShowApproveForm(true);
      return;
    }
    if (id === "requestRevision") {
      setShowRevisionForm(true);
      return;
    }
    if (id === "submitManual") {
      setShowManualSubmit(true);
      return;
    }
    const handler = AUDIT_ACTION_HANDLERS[id];
    if (!handler) return;
    setPendingActionId(id);
    return handler(run.id)
      .then(() => {
        invalidate();
      })
      .catch((err: unknown) => {
        invalidate();
        throw err;
      })
      .finally(() => setPendingActionId(null));
  };

  const planPassLinkView = resolvePlanPassLink(run.planContext);

  const detailSections: DetailSection[] = [
    {
      key: "packet-preview",
      label: "Audit packet preview",
      render: () => <AuditPacketPreviewDetail auditData={auditData} />,
    },
    {
      key: "commit-message",
      label: "Commit message preview",
      render: () => <CommitMessagePreviewDetail auditData={auditData} />,
    },
    {
      key: "input-summary",
      label: "Input summary",
      render: () => (
        <InputSummaryDetail auditData={auditData} runId={run.id} />
      ),
    },
    {
      key: "validation",
      label: "Validation report",
      render: () => (
        <ValidationReportDetail
          run={run}
          artifacts={artifacts}
          runId={run.id}
          hasFinalValidationEvidence={hasFinalValidationEvidence}
          validateMutation={validateMutation}
          acceptFailureMutation={acceptFailureMutation}
          showAcceptanceForm={showAcceptanceForm}
          setShowAcceptanceForm={setShowAcceptanceForm}
          acceptanceReason={acceptanceReason}
          setAcceptanceReason={setAcceptanceReason}
        />
      ),
    },
    {
      key: "revision-requirements",
      label: "Revision requirements",
      render: () => <RevisionRequirementsDetail auditData={auditData} />,
    },
  ];

  return (
    <>
      <RunStatusTrackerLayout
        run={run}
        currentStep="audit"
        currentStatus={currentStatus}
        actionsView={stepActionsView}
        onActionClick={onActionClick}
        progression={progression}
        eventsLoadFailed={eventsLoadFailed}
        detailSections={detailSections}
        planPassLinkView={planPassLinkView}
      />

      <ApproveAuditDialog
        open={showApproveForm}
        onOpenChange={setShowApproveForm}
        decision={approveDecision}
        setDecision={setApproveDecision}
        notes={approveNotes}
        setNotes={setApproveNotes}
        mutation={approveMutation}
      />
      <RequestRevisionDialog
        open={showRevisionForm}
        onOpenChange={setShowRevisionForm}
        reason={revisionReason}
        setReason={setRevisionReason}
        mutation={revisionMutation}
      />
      <SubmitManualAuditDialog
        open={showManualSubmit}
        onOpenChange={setShowManualSubmit}
        decision={manualDecision}
        setDecision={setManualDecision}
        markdown={manualPacketMarkdown}
        setMarkdown={setManualPacketMarkdown}
        notes={manualNotes}
        setNotes={setManualNotes}
        mutation={submitManualMutation}
      />
    </>
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

// ------------------------------------------------------------
// Detail_Disclosure sections (Requirement 5.7) — audit packet preview,
// commit-message preview, input-summary detail, full validation report,
// revision-requirement detail list. Each is only invoked once the Operator
// opens Detail_Disclosure and then that specific section (lazy
// `DetailSection.render()`). None of these sections render an action
// button that duplicates a Next_Action_Area control — action controls
// (generate/approve/request revision/submit manual/prepare commit
// message/close) live solely in Next_Action_Area.
// ------------------------------------------------------------

function AuditPacketPreviewDetail({
  auditData,
}: {
  auditData: ReturnType<typeof deriveAuditData>;
}) {
  return (
    <div className="flex flex-col gap-3">
      {auditData.generatedPacket.available ? (
        <div className="flex flex-col gap-2">
          <div className="flex items-center gap-2">
            <Badge
              variant="default"
              className="text-xs bg-emerald-600 hover:bg-emerald-700 text-black"
            >
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
            <pre className="max-w-full overflow-x-auto text-[11px] font-mono bg-muted/40 p-2.5 rounded border border-[var(--relay-row-border)] max-h-64 overflow-y-auto whitespace-pre-wrap text-foreground">
              {auditData.generatedPacket.preview}
            </pre>
          )}
          {auditData.generatedPacket.warnings.length > 0 && (
            <div className="flex flex-col gap-1 text-xs text-warning">
              {auditData.generatedPacket.warnings.map(
                (w: string, i: number) => (
                  <span key={i} className="flex items-center gap-1 text-[11px]">
                    <AlertTriangle className="w-3 h-3" />
                    {w}
                  </span>
                ),
              )}
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
            <pre className="max-w-full overflow-x-auto text-[11px] font-mono bg-muted/40 p-2.5 rounded border border-[var(--relay-row-border)] max-h-64 overflow-y-auto whitespace-pre-wrap text-foreground">
              {auditData.manualPacket.preview}
            </pre>
          )}
        </div>
      )}

      {auditData.decision.currentDecision && (
        <div className="flex items-center gap-2 text-xs mt-1">
          <Badge variant="secondary" className="text-xs">
            Decision: {auditData.decision.currentDecision}
          </Badge>
          {auditData.decision.notes && (
            <span className="text-muted-foreground">
              {auditData.decision.notes}
            </span>
          )}
        </div>
      )}

      {!auditData.generatedPacket.available && !auditData.manualPacket && (
        <p className="text-xs text-muted-foreground/60 italic">
          No audit packet available yet.
        </p>
      )}
    </div>
  );
}

function CommitMessagePreviewDetail({
  auditData,
}: {
  auditData: ReturnType<typeof deriveAuditData>;
}) {
  return (
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

      {auditData.commitSummary.commitMessagePreview ? (
        <pre className="max-w-full overflow-x-auto text-[11px] font-mono bg-muted/40 p-2.5 rounded border border-[var(--relay-row-border)] max-h-64 overflow-y-auto whitespace-pre-wrap text-foreground">
          {auditData.commitSummary.commitMessagePreview}
        </pre>
      ) : (
        <p className="text-xs text-muted-foreground italic">
          No commit message prepared yet.
        </p>
      )}
    </div>
  );
}

function InputSummaryDetail({
  auditData,
  runId,
}: {
  auditData: ReturnType<typeof deriveAuditData>;
  runId: string;
}) {
  return (
    <div className="flex flex-col gap-3">
      {auditData.inputSummary.available ? (
        <>
          <div className="flex items-center gap-2">
            <span className="text-xs font-mono text-muted-foreground">
              {auditData.inputSummary.artifactPath}
            </span>
            {auditData.inputSummary.generatedAt && (
              <span className="text-[11px] text-muted-foreground/60">
                {new Date(auditData.inputSummary.generatedAt).toLocaleString()}
              </span>
            )}
          </div>
          {auditData.inputSummary.preview && (
            <pre className="max-w-full overflow-x-auto text-[11px] font-mono bg-muted/40 p-2.5 rounded border border-[var(--relay-row-border)] max-h-64 overflow-y-auto whitespace-pre-wrap text-foreground">
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
                    <span key={i} className="text-[11px]">
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
              <span className="text-[11px] font-medium">Missing evidence:</span>
              {auditData.inputSummary.missingEvidence.map(
                (w: string, i: number) => (
                  <span key={i} className="flex items-center gap-1 text-[11px]">
                    <AlertTriangle className="w-3 h-3" />
                    {w}
                  </span>
                ),
              )}
            </div>
          )}
        </>
      ) : (
        <p className="text-xs text-muted-foreground italic">
          No audit input summary generated yet.
        </p>
      )}

      {auditData.evidenceManifestArtifact && (
        <div className="flex flex-col gap-1.5 pt-2 border-t border-[var(--relay-row-border)]">
          <span className="text-[11px] font-medium text-muted-foreground">
            Evidence manifest:
          </span>
          <ArtifactPreviewCard
            artifact={auditData.evidenceManifestArtifact}
            runId={runId}
            className="max-w-full"
          />
        </div>
      )}
    </div>
  );
}

function ValidationReportDetail({
  run,
  artifacts,
  runId,
  hasFinalValidationEvidence,
  validateMutation,
  acceptFailureMutation,
  showAcceptanceForm,
  setShowAcceptanceForm,
  acceptanceReason,
  setAcceptanceReason,
}: {
  run: RelayRun;
  artifacts: RelayArtifact[];
  runId: string;
  hasFinalValidationEvidence: boolean;
  validateMutation: ReturnType<typeof useMutation<unknown, unknown, void>>;
  acceptFailureMutation: ReturnType<
    typeof useMutation<unknown, unknown, string>
  >;
  showAcceptanceForm: boolean;
  setShowAcceptanceForm: (value: boolean) => void;
  acceptanceReason: string;
  setAcceptanceReason: (value: string) => void;
}) {
  const runStatus = (run.status || "") as string;
  const localValidationIsRunning = runStatus === "local_validation_running";
  const localValidationPassed = runStatus === "validation_passed";
  const localValidationFailed = runStatus === "validation_failed";
  const localValidationAccepted = runStatus === "validation_failed_accepted";
  const activeMutation =
    validateMutation.isPending || acceptFailureMutation.isPending;

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

  const handleValidate = () => validateMutation.mutate();
  const handleAcceptFailure = () => {
    if (!acceptanceReason.trim()) return;
    acceptFailureMutation.mutate(acceptanceReason);
  };

  return (
    <div className="flex flex-col gap-3">
      <ValidationPanel summary={run.validationSummary} />

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

      {(validationRunJsonArt ||
        validationFailureAcceptanceJsonArt ||
        validationProgressJsonArt ||
        validationStdoutArt ||
        validationStderrArt) && (
        <RunStageEvidenceList className="mt-1 border-t border-[var(--relay-row-border)] pt-3">
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
        <div className="flex flex-col gap-2 mt-1 border-t border-[var(--relay-row-border)] pt-3">
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
  );
}

function RevisionRequirementsDetail({
  auditData,
}: {
  auditData: ReturnType<typeof deriveAuditData>;
}) {
  const hasAny =
    auditData.revisionRequirements.length > 0 ||
    auditData.warnings.length > 0 ||
    auditData.blockers.length > 0;

  if (!hasAny) {
    return (
      <p className="text-xs text-muted-foreground italic">
        No outstanding revision requirements, warnings, or blockers.
      </p>
    );
  }

  return (
    <RunStageFindingList>
      {auditData.blockers.map((b: string, i: number) => (
        <RunStageFindingRow key={`blocker-${i}`} severity="error" message={b} />
      ))}
      {auditData.revisionRequirements.map((r: string, i: number) => (
        <RunStageFindingRow key={`rev-${i}`} severity="warning" message={r} />
      ))}
      {auditData.warnings.map((w: string, i: number) => (
        <RunStageFindingRow key={`warn-${i}`} severity="info" message={w} />
      ))}
    </RunStageFindingList>
  );
}

// ------------------------------------------------------------
// Action dialogs — approveAudit/requestRevision/submitManual require
// Operator-entered form data that cannot be derived from just a run id, so
// `onActionClick` opens one of these dialogs instead of invoking a mutation
// directly. Rendered outside `RunStatusTrackerLayout` (Requirement 6.1's
// single main column describes the tracker regions, not modal overlays).
// ------------------------------------------------------------

function ApproveAuditDialog({
  open,
  onOpenChange,
  decision,
  setDecision,
  notes,
  setNotes,
  mutation,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  decision: "accepted" | "accepted_with_warnings";
  setDecision: (value: "accepted" | "accepted_with_warnings") => void;
  notes: string;
  setNotes: (value: string) => void;
  mutation: ReturnType<typeof useMutation<unknown, unknown, void>>;
}) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Approve Audit</DialogTitle>
          <DialogDescription>
            Record the audit decision. This updates Relay state only.
          </DialogDescription>
        </DialogHeader>
        <div className="flex flex-col gap-2">
          <div className="flex flex-col gap-1">
            <label className="text-[11px] text-muted-foreground">Decision</label>
            <select
              className="text-xs bg-background border border-[var(--relay-row-border)] rounded px-2 py-1.5 text-foreground"
              value={decision}
              onChange={(e) =>
                setDecision(
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
            value={notes}
            onChange={(e) => setNotes(e.target.value)}
          />
        </div>
        <DialogFooter>
          <Button
            variant="ghost"
            size="sm"
            onClick={() => onOpenChange(false)}
            disabled={mutation.isPending}
          >
            Cancel
          </Button>
          <Button
            variant="default"
            size="sm"
            onClick={() => mutation.mutate()}
            disabled={mutation.isPending}
            className="gap-1.5"
          >
            {mutation.isPending ? (
              <Loader2 className="w-3.5 h-3.5 animate-spin" />
            ) : null}
            Confirm Approval
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function RequestRevisionDialog({
  open,
  onOpenChange,
  reason,
  setReason,
  mutation,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  reason: string;
  setReason: (value: string) => void;
  mutation: ReturnType<typeof useMutation<unknown, unknown, void>>;
}) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Request Revision</DialogTitle>
          <DialogDescription>
            Describe what needs revision before the audit packet is
            regenerated.
          </DialogDescription>
        </DialogHeader>
        <Textarea
          className="text-xs min-h-[60px]"
          placeholder="Describe what needs revision..."
          value={reason}
          onChange={(e) => setReason(e.target.value)}
        />
        <DialogFooter>
          <Button
            variant="ghost"
            size="sm"
            onClick={() => onOpenChange(false)}
            disabled={mutation.isPending}
          >
            Cancel
          </Button>
          <Button
            variant="outline"
            size="sm"
            onClick={() => mutation.mutate()}
            disabled={mutation.isPending}
            className="gap-1.5 text-destructive/80"
          >
            {mutation.isPending ? (
              <Loader2 className="w-3.5 h-3.5 animate-spin" />
            ) : null}
            Submit Revision Request
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function SubmitManualAuditDialog({
  open,
  onOpenChange,
  decision,
  setDecision,
  markdown,
  setMarkdown,
  notes,
  setNotes,
  mutation,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  decision: string;
  setDecision: (value: string) => void;
  markdown: string;
  setMarkdown: (value: string) => void;
  notes: string;
  setNotes: (value: string) => void;
  mutation: ReturnType<typeof useMutation<unknown, unknown, void>>;
}) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Submit Manual Audit Decision</DialogTitle>
          <DialogDescription>
            Paste an audit packet and record a decision manually.
          </DialogDescription>
        </DialogHeader>
        <div className="flex flex-col gap-2">
          <Textarea
            className="text-xs font-mono min-h-[120px]"
            placeholder="Paste audit packet markdown content..."
            value={markdown}
            onChange={(e) => setMarkdown(e.target.value)}
          />
          <div className="flex flex-col gap-1">
            <label className="text-[11px] text-muted-foreground">Decision</label>
            <select
              className="text-xs bg-background border border-[var(--relay-row-border)] rounded px-2 py-1.5 text-foreground"
              value={decision}
              onChange={(e) => setDecision(e.target.value)}
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
            value={notes}
            onChange={(e) => setNotes(e.target.value)}
          />
        </div>
        <DialogFooter>
          <Button
            variant="ghost"
            size="sm"
            onClick={() => onOpenChange(false)}
            disabled={mutation.isPending}
          >
            Cancel
          </Button>
          <Button
            variant="default"
            size="sm"
            onClick={() => mutation.mutate()}
            disabled={mutation.isPending || !decision || !markdown.trim()}
            className="gap-1.5"
          >
            {mutation.isPending ? (
              <Loader2 className="w-3.5 h-3.5 animate-spin" />
            ) : null}
            Submit
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
