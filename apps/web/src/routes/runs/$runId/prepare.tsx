import * as React from "react";
import { createFileRoute } from "@tanstack/react-router";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  approveBrief,
  evaluateRepairEligibility,
  prepareRun,
  renderBrief,
  repairValidation,
  runArtifactsQueryOptions,
  runDetailQueryOptions,
  runEventsQueryOptions,
} from "@/features/relay-runs";
import type {
  RelayArtifact,
  RelayRun,
  RelayRunEvent,
  RepairValidationResponse,
} from "@/features/relay-runs";
import { ArtifactPreviewCard } from "@/components/relay/ArtifactPreviewCard";
import { RunStatusTrackerLayout } from "@/components/relay/RunStatusTrackerLayout";
import {
  RunWorkbenchLoadFailedState,
  RunWorkbenchLoadingState,
} from "@/components/relay/RunWorkbenchStates";
import {
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
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { Loader2 } from "lucide-react";
import { getCompileRenderDisplayState } from "./runCompileRenderVisualState";
import { deriveCurrentStatusText } from "@/features/relay-runs/deriveCurrentStatusText";
import { deriveProgressionLog } from "@/features/relay-runs/deriveProgressionLog";
import { resolvePlanPassLink } from "@/features/relay-runs/planPassLink";
import type {
  ActionControlView,
  DetailSection,
  StepActionsView,
} from "@/features/relay-runs/runStatusTrackerViews";

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
  const {
    data: events,
    isLoading: isLoadingEvents,
    error: errorEvents,
  } = useQuery(runEventsQueryOptions(runId));

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

  return (
    <PrepareTracker
      run={run}
      artifacts={artifacts || []}
      events={events || []}
      eventsLoadFailed={Boolean(errorEvents)}
    />
  );
}

// ------------------------------------------------------------
// Prepare action-gating (task-5.7-style, per run-workbench-refinement)
// ------------------------------------------------------------
//
// Prepare has no formally-declared `RelayPrepareActions` gating type in
// `types.ts` the way execute/audit do. The existing `canCompile` /
// `canRetryCompile` / `canRenderBrief` / `canApproveBrief` booleans (plus
// the repair-attempt candidate, gated the same way the prior
// `CompileRenderStageActions` gated its "Attempt Repair" button) already
// carry the same action-gating information; this restates them as
// `can*`-shaped candidates and runs them through the same
// priority-order/primary-selection assembly `runStepActions.ts` uses for
// execute/audit, per task 5.7 — no new gating semantics are introduced.
// `ActionControlView.unavailableReason` is optional, so omitting reason
// text (prepare's booleans have no companion `*UnavailableReason` strings)
// does not require inventing any.

interface PrepareActionCandidate {
  id: string;
  label: string;
  enabled: boolean;
}

export function buildPrepareActionsView({
  canCompile,
  canRetryCompile,
  canAttemptRepair,
  canRenderBrief,
  canApproveBrief,
}: {
  canCompile: boolean;
  canRetryCompile: boolean;
  canAttemptRepair: boolean;
  canRenderBrief: boolean;
  canApproveBrief: boolean;
}): StepActionsView {
  const candidates: PrepareActionCandidate[] = [
    { id: "compile", label: "Run Compile", enabled: canCompile },
    { id: "retryCompile", label: "Retry Compile", enabled: canRetryCompile },
    {
      id: "attemptRepair",
      label: "Attempt Repair",
      enabled: canAttemptRepair,
    },
    { id: "renderBrief", label: "Render Executor Brief", enabled: canRenderBrief },
    { id: "approveBrief", label: "Approve for Executor", enabled: canApproveBrief },
  ];

  const nextSafeActionId = candidates.find((candidate) => candidate.enabled)?.id;

  const controls: ActionControlView[] = candidates.map((candidate) => ({
    id: candidate.id,
    label: candidate.label,
    enabled: candidate.enabled,
    isPrimary: candidate.id === nextSafeActionId,
  }));

  return {
    controls,
    ...(nextSafeActionId ? { nextSafeActionId } : {}),
  };
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

function PrepareTracker({
  run,
  artifacts,
  events,
  eventsLoadFailed,
}: {
  run: RelayRun;
  artifacts: RelayArtifact[];
  events: RelayRunEvent[];
  eventsLoadFailed: boolean;
}) {
  const queryClient = useQueryClient();
  const [approvalNotes, setApprovalNotes] = React.useState("");
  const [showApproveForm, setShowApproveForm] = React.useState(false);
  const [repairResult, setRepairResult] =
    React.useState<RepairValidationResponse | null>(null);
  const [pendingActionId, setPendingActionId] = React.useState<string | null>(
    null,
  );

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

  const canCompile = isApprovedForPrepare;
  const canRetryCompile = isPacketValidationFailed;
  const canRenderBrief = isPacketValidated;
  const canApproveBrief = isBriefReadyForReview;

  const packetValidationReport = parseArtifactPreview<PacketValidationReport>(
    packetValidationArt,
  );
  const briefValidationReport = parseArtifactPreview<BriefValidationReport>(
    briefValidationArt,
  );

  const repairEligibility = evaluateRepairEligibility(packetValidationReport);
  const canAttemptRepair =
    isPacketValidationFailed &&
    repairEligibility.canOfferRepair &&
    repairResult === null;

  const invalidate = () => {
    void queryClient.invalidateQueries({ queryKey: ["relay-runs"] });
  };

  const compileMutation = useMutation({
    mutationFn: () => prepareRun(run.id),
    onSuccess: invalidate,
    onError: invalidate,
  });

  const renderBriefMutation = useMutation({
    mutationFn: () => renderBrief(run.id),
    onSuccess: invalidate,
  });

  const approveMutation = useMutation({
    mutationFn: () =>
      approveBrief(run.id, {
        action: "approve",
        notes: approvalNotes.trim() || undefined,
      }),
    onSuccess: () => {
      setShowApproveForm(false);
      setApprovalNotes("");
      invalidate();
    },
  });

  const repairMutation = useMutation({
    mutationFn: () => repairValidation(run.id),
    onSuccess: (data) => {
      setRepairResult(data);
      invalidate();
    },
    onError: invalidate,
  });

  const compileRenderVisualStateInput = {
    run,
    repairEligible: repairEligibility.canOfferRepair,
    repairResult,
    compilePending: compileMutation.isPending,
    repairPending: repairMutation.isPending,
    renderBriefPending: renderBriefMutation.isPending,
    approvePending: approveMutation.isPending,
    hasPassingBriefValidationReport:
      briefValidationReport?.status === "passed",
    hasFailingBriefValidationReport: Boolean(
      briefValidationArt && briefValidationReport?.status !== "passed",
    ),
  };
  const compileRenderDisplayState = getCompileRenderDisplayState(
    compileRenderVisualStateInput,
  );

  const currentStatus = deriveCurrentStatusText(
    "prepare",
    compileRenderDisplayState,
    { updatedAt: run.updatedAt },
  );
  const progression = deriveProgressionLog(events);

  const baseActionsView = buildPrepareActionsView({
    canCompile,
    canRetryCompile,
    canAttemptRepair,
    canRenderBrief,
    canApproveBrief,
  });
  const actionsView: StepActionsView = pendingActionId
    ? {
        ...baseActionsView,
        controls: baseActionsView.controls.map((control) =>
          control.id === pendingActionId
            ? { ...control, enabled: false }
            : control,
        ),
      }
    : baseActionsView;

  // approveBrief requires Operator-entered notes, so it opens a dialog
  // rather than invoking a mutation directly from `onActionClick`. Every
  // other id maps directly onto the existing mutation for this route; the
  // returned Promise is passed straight back so `RunStatusTrackerLayout`'s
  // built-in tone-escalation (Requirement 6.6) handles the failure banner
  // — no separate manual error-banner state.
  const onActionClick = (id: string) => {
    if (id === "approveBrief") {
      setShowApproveForm(true);
      return;
    }

    let mutationPromise: Promise<unknown> | undefined;
    if (id === "compile" || id === "retryCompile") {
      mutationPromise = compileMutation.mutateAsync();
    } else if (id === "attemptRepair") {
      setRepairResult(null);
      mutationPromise = repairMutation.mutateAsync();
    } else if (id === "renderBrief") {
      mutationPromise = renderBriefMutation.mutateAsync();
    }

    if (!mutationPromise) {
      return;
    }

    setPendingActionId(id);
    return mutationPromise.finally(() => setPendingActionId(null));
  };

  const planPassLinkView = resolvePlanPassLink(run.planContext);

  // ------------------------------------------------------------
  // Detail_Disclosure (Requirement 5.8) — canonical packet content, packet
  // validation report detail, full executor brief text, brief validation
  // issue list, repair result detail. Reuses the existing
  // `ArtifactPreviewCard`/`ValidationPanel` preview-rendering components —
  // the same components the prior inspector tabs used for this content —
  // rather than inventing new rendering. Lazy: only invoked once a section
  // is opened.
  // ------------------------------------------------------------
  const packetValidationErrors = packetValidationReport?.errors || [];
  const briefValidationIssues = briefValidationReport?.issues || [];

  const detailSections: DetailSection[] = [
    {
      key: "packet-content",
      label: "Canonical packet",
      render: () =>
        canonicalPacketArt ? (
          <ArtifactPreviewCard runId={run.id} artifact={canonicalPacketArt} />
        ) : (
          <p className="text-xs text-muted-foreground italic">
            No canonical packet artifact found for this run.
          </p>
        ),
    },
    {
      key: "packet-validation",
      label: "Packet validation report",
      render: () => (
        <div className="flex flex-col gap-3">
          <RunStageKeyValueRow
            label="Status"
            value={
              packetValidationReport?.valid === true ? "Valid" : "Invalid"
            }
          />
          {packetValidationErrors.length > 0 ? (
            <RunStageFindingList>
              {packetValidationErrors.map((error, index) => (
                <RunStageFindingRow
                  key={`${error.code || "issue"}-${index}`}
                  severity="error"
                  code={error.code}
                  message={error.message || "Validation issue captured."}
                />
              ))}
            </RunStageFindingList>
          ) : null}
          {packetValidationArt ? (
            <ArtifactPreviewCard runId={run.id} artifact={packetValidationArt} />
          ) : (
            <p className="text-xs text-muted-foreground italic">
              No packet validation report artifact found for this run.
            </p>
          )}
        </div>
      ),
    },
    {
      key: "executor-brief",
      label: "Executor brief",
      render: () =>
        executorBriefArt ? (
          <div className="flex flex-col gap-3">
            {executorBriefArt.preview ? (
              <pre className="max-h-96 overflow-y-auto rounded border border-border/40 bg-[var(--relay-code-bg)] p-3 font-mono text-[11px] whitespace-pre-wrap text-foreground">
                {executorBriefArt.preview}
              </pre>
            ) : null}
            <ArtifactPreviewCard runId={run.id} artifact={executorBriefArt} />
          </div>
        ) : (
          <p className="text-xs text-muted-foreground italic">
            No executor brief artifact found for this run.
          </p>
        ),
    },
    {
      key: "brief-validation",
      label: "Brief validation issues",
      render: () => (
        <div className="flex flex-col gap-3">
          <RunStageKeyValueRow
            label="Status"
            value={
              briefValidationReport?.status === "passed" ? "Passed" : "Failed"
            }
          />
          {briefValidationIssues.length > 0 ? (
            <RunStageFindingList>
              {briefValidationIssues.map((issue, index) => (
                <RunStageFindingRow
                  key={`${issue.severity || "issue"}-${index}`}
                  severity={issue.severity === "error" ? "error" : "warning"}
                  message={issue.message || "Validation issue captured."}
                />
              ))}
            </RunStageFindingList>
          ) : (
            <p className="text-xs text-muted-foreground italic">
              No brief validation issues reported.
            </p>
          )}
          {briefValidationArt ? (
            <ArtifactPreviewCard runId={run.id} artifact={briefValidationArt} />
          ) : null}
        </div>
      ),
    },
    {
      key: "repair-result",
      label: "Repair result",
      render: () => (
        <div className="flex flex-col gap-3">
          <RunStageKeyValueRow
            label="Eligibility"
            value={
              repairEligibility.canOfferRepair
                ? "Repair eligible"
                : repairEligibility.reason || "Not eligible"
            }
          />
          {repairResult?.blockedReason ? (
            <RunStageKeyValueRow
              label="Blocked"
              value={repairResult.blockedReason}
            />
          ) : null}
          {repairResult?.ineligibleReason ? (
            <RunStageKeyValueRow
              label="Ineligible"
              value={repairResult.ineligibleReason}
            />
          ) : null}
          {repairResult?.reValidationValid !== undefined ? (
            <RunStageKeyValueRow
              label="Re-validation"
              value={repairResult.reValidationValid ? "Passed" : "Failed"}
            />
          ) : null}
          {repairResult?.reValidationError ? (
            <RunStageKeyValueRow
              label="Re-validation error"
              value={repairResult.reValidationError}
            />
          ) : null}
          {!repairResult ? (
            <p className="text-xs text-muted-foreground italic">
              Repair has not been attempted for this run.
            </p>
          ) : null}
        </div>
      ),
    },
  ];

  return (
    <>
      <RunStatusTrackerLayout
        run={run}
        currentStep="prepare"
        currentStatus={currentStatus}
        actionsView={actionsView}
        onActionClick={onActionClick}
        progression={progression}
        eventsLoadFailed={eventsLoadFailed}
        detailSections={detailSections}
        planPassLinkView={planPassLinkView}
      />

      <ApproveBriefDialog
        open={showApproveForm}
        onOpenChange={setShowApproveForm}
        notes={approvalNotes}
        setNotes={setApprovalNotes}
        mutation={approveMutation}
      />
    </>
  );
}

// ------------------------------------------------------------
// Action dialog — approveBrief requires an optional Operator-entered notes
// field that cannot be derived from just a run id, so `onActionClick` opens
// this dialog instead of invoking a mutation directly. Rendered outside
// `RunStatusTrackerLayout` (Requirement 6.1's single main column describes
// the tracker regions, not modal overlays).
// ------------------------------------------------------------

function ApproveBriefDialog({
  open,
  onOpenChange,
  notes,
  setNotes,
  mutation,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  notes: string;
  setNotes: (value: string) => void;
  mutation: ReturnType<typeof useMutation<unknown, unknown, void>>;
}) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Approve for Executor</DialogTitle>
          <DialogDescription>
            Approve the compiled executor brief to advance to the execution
            stage.
          </DialogDescription>
        </DialogHeader>
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="approval-notes" className="text-xs text-muted-foreground">
            Approval Notes (Optional)
          </Label>
          <Textarea
            id="approval-notes"
            value={notes}
            onChange={(event) => setNotes(event.target.value)}
            placeholder="Optional notes for the approval decision..."
            className="h-20 resize-none text-xs"
            disabled={mutation.isPending}
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
