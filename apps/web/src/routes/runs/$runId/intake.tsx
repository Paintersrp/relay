import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import {
  approveIntake,
  type RelayArtifact,
  type RelayRun,
  type RelayRunEvent,
  runDetailQueryOptions,
  runArtifactsQueryOptions,
  runEventsQueryOptions,
} from "@/features/relay-runs";
import type { RelayApprovalAction } from "@/features/relay-runs/types";
import { useRunIntakeReviewController } from "@/components/relay/RunIntakeReviewPanel";
import { RunStatusTrackerLayout } from "@/components/relay/RunStatusTrackerLayout";
import {
  RunWorkbenchLoadFailedState,
  RunWorkbenchLoadingState,
} from "@/components/relay/RunWorkbenchStates";
import { ValidationPanel } from "@/components/relay/ValidationPanel";
import { ArtifactPreviewCard } from "@/components/relay/ArtifactPreviewCard";
import {
  deriveCurrentStatusText,
  type IntakeBlockedState,
} from "@/features/relay-runs/deriveCurrentStatusText";
import { deriveProgressionLog } from "@/features/relay-runs/deriveProgressionLog";
import { resolvePlanPassLink } from "@/features/relay-runs/planPassLink";
import type {
  ActionControlView,
  DetailSection,
  StepActionsView,
} from "@/features/relay-runs/runStatusTrackerViews";

export const Route = createFileRoute("/runs/$runId/intake")({
  component: IntakePage,
});

function IntakePage() {
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

  // Handle run details missing or load errors
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
    <IntakeTracker
      run={run}
      artifacts={artifacts || []}
      events={events || []}
      eventsLoadFailed={Boolean(errorEvents)}
    />
  );
}

// ------------------------------------------------------------
// Intake action-gating (task-5.7-style, per run-workbench-refinement)
// ------------------------------------------------------------
//
// Intake has no formally-declared `RelayIntakeActions` gating type in
// `types.ts` the way execute/audit do. The existing `isReviewable` boolean
// (unchanged — `run.status === "intake_needs_review" || "intake_received"`)
// already gates the same three review actions the prior
// `RunIntakeStageActions` rendered unconditionally when reviewable, and the
// existing `isApproved` boolean already gates the prior "Proceed to Compile
// / Render" link. This restates that existing gating as `can*`-shaped
// candidates and runs it through the same priority-order/primary-selection
// assembly `runStepActions.ts` uses for execute/audit, per task 5.7 — no
// new gating semantics are introduced.

interface IntakeActionCandidate {
  id: string;
  label: string;
  enabled: boolean;
  unavailableReason?: string;
}

export function buildIntakeActionsView({
  isReviewable,
  isApproved,
}: {
  isReviewable: boolean;
  isApproved: boolean;
}): StepActionsView {
  const reviewUnavailableReason = isReviewable
    ? undefined
    : "Intake is not currently awaiting review.";

  const candidates: IntakeActionCandidate[] = [
    {
      id: "approve",
      label: "Approve Intake",
      enabled: isReviewable,
      unavailableReason: reviewUnavailableReason,
    },
    {
      id: "needsRevision",
      label: "Needs Revision",
      enabled: isReviewable,
      unavailableReason: reviewUnavailableReason,
    },
    {
      id: "block",
      label: "Block Run",
      enabled: isReviewable,
      unavailableReason: reviewUnavailableReason,
    },
    {
      id: "proceedToPrepare",
      label: "Proceed to Compile / Render",
      enabled: isApproved,
      unavailableReason: isApproved
        ? undefined
        : "Approve intake before moving to Compile / Render.",
    },
  ];

  const nextSafeActionId = candidates.find((candidate) => candidate.enabled)?.id;

  const controls: ActionControlView[] = candidates.map((candidate) => {
    const isPrimary = candidate.id === nextSafeActionId;
    const hasReason =
      !candidate.enabled &&
      typeof candidate.unavailableReason === "string" &&
      candidate.unavailableReason.length > 0;

    return {
      id: candidate.id,
      label: candidate.label,
      enabled: candidate.enabled,
      isPrimary,
      ...(hasReason ? { unavailableReason: candidate.unavailableReason } : {}),
    };
  });

  return {
    controls,
    ...(nextSafeActionId ? { nextSafeActionId } : {}),
  };
}

const INTAKE_ACTION_TO_APPROVAL_ACTION: Record<string, RelayApprovalAction> = {
  approve: "approve",
  needsRevision: "needs_revision",
  block: "blocked",
};

function findArtifactByKindOrFilename(
  artifacts: RelayArtifact[],
  kind: string,
  filename: string,
): RelayArtifact | undefined {
  return artifacts.find(
    (artifact) => artifact.kind === kind || artifact.filename === filename,
  );
}

function IntakeTracker({
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
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [pendingActionId, setPendingActionId] = useState<string | null>(null);

  const intakeReview = useRunIntakeReviewController({
    run: {
      ...run,
      latestEvents: events,
    },
    artifacts,
  });

  // ------------------------------------------------------------
  // Run Status Tracker Redesign — Current_Status_Block (Requirement 2)
  // ------------------------------------------------------------
  //
  // Intake only distinguishes blocked vs not-blocked for the headline
  // (Requirements 2.3, 2.4) — restated from the existing, unchanged
  // `intakeDisplayState` the controller already computes.
  const blockedState: IntakeBlockedState =
    intakeReview.intakeDisplayState === "blocked" ? "blocked" : "not_blocked";
  const currentStatus = deriveCurrentStatusText("intake", blockedState, {
    updatedAt: run.updatedAt,
  });

  // ------------------------------------------------------------
  // Progression_Rail (Requirement 4)
  // ------------------------------------------------------------
  const progression = deriveProgressionLog(events);

  // ------------------------------------------------------------
  // Next_Action_Area (Requirement 3)
  // ------------------------------------------------------------
  const baseActionsView = buildIntakeActionsView({
    isReviewable: intakeReview.isReviewable,
    isApproved: intakeReview.isApproved,
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

  const onActionClick = (id: string) => {
    if (id === "proceedToPrepare") {
      void navigate({ to: "/runs/$runId/prepare", params: { runId: run.id } });
      return;
    }

    const action = INTAKE_ACTION_TO_APPROVAL_ACTION[id];
    if (!action) {
      return;
    }

    setPendingActionId(id);
    return approveIntake(run.id, { action, notes: "" })
      .then(() => {
        void queryClient.invalidateQueries({ queryKey: ["relay-runs"] });
      })
      .catch((err) => {
        void queryClient.invalidateQueries({ queryKey: ["relay-runs"] });
        throw err;
      })
      .finally(() => setPendingActionId(null));
  };

  // ------------------------------------------------------------
  // Detail_Disclosure (Requirement 5.9) — full parsed frontmatter,
  // run_config content, intake validation report detail, raw handoff
  // preview. Reuses the existing `ArtifactPreviewCard`/`ValidationPanel`
  // preview-rendering components — the same components the prior
  // inspector tabs used for this content — rather than inventing new
  // rendering. Lazy: only invoked once a section is opened.
  // ------------------------------------------------------------
  const parsedFrontmatterArtifact = findArtifactByKindOrFilename(
    artifacts,
    "parsed_frontmatter",
    "parsed_frontmatter.json",
  );
  const runConfigArtifact = findArtifactByKindOrFilename(
    artifacts,
    "run_config",
    "run_config.json",
  );
  const intakeValidationReportArtifact = findArtifactByKindOrFilename(
    artifacts,
    "intake_validation_report",
    "intake_validation_report.json",
  );
  const handoffArtifact = artifacts.find(
    (artifact) => artifact.kind === "handoff" || artifact.kind === "planner_handoff",
  );

  const detailSections: DetailSection[] = [
    {
      key: "frontmatter",
      label: "Parsed frontmatter",
      render: () =>
        parsedFrontmatterArtifact ? (
          <ArtifactPreviewCard runId={run.id} artifact={parsedFrontmatterArtifact} />
        ) : (
          <p className="text-xs text-muted-foreground italic">
            No parsed frontmatter artifact found for this run.
          </p>
        ),
    },
    {
      key: "run-config",
      label: "Run config",
      render: () =>
        runConfigArtifact ? (
          <ArtifactPreviewCard runId={run.id} artifact={runConfigArtifact} />
        ) : (
          <p className="text-xs text-muted-foreground italic">
            No run_config artifact found for this run.
          </p>
        ),
    },
    {
      key: "intake-validation",
      label: "Intake validation report",
      render: () => (
        <div className="flex flex-col gap-3">
          <ValidationPanel summary={run.validationSummary} />
          {intakeValidationReportArtifact ? (
            <ArtifactPreviewCard
              runId={run.id}
              artifact={intakeValidationReportArtifact}
            />
          ) : null}
        </div>
      ),
    },
    {
      key: "raw-handoff",
      label: "Raw handoff",
      render: () =>
        handoffArtifact ? (
          <ArtifactPreviewCard runId={run.id} artifact={handoffArtifact} />
        ) : (
          <p className="text-xs text-muted-foreground italic">
            No raw handoff artifact found for this run.
          </p>
        ),
    },
  ];

  const planPassLinkView = resolvePlanPassLink(run.planContext);

  return (
    <RunStatusTrackerLayout
      run={run}
      currentStep="intake"
      currentStatus={currentStatus}
      actionsView={actionsView}
      onActionClick={onActionClick}
      progression={progression}
      eventsLoadFailed={eventsLoadFailed}
      detailSections={detailSections}
      planPassLinkView={planPassLinkView}
    />
  );
}
