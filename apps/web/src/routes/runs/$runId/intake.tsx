import { createFileRoute } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import {
  type RelayArtifact,
  type RelayRun,
  type RelayRunEvent,
  runDetailQueryOptions,
  runArtifactsQueryOptions,
  runEventsQueryOptions,
} from "@/features/relay-runs";
import {
  RunIntakeReviewPanel,
  RunIntakeStageActions,
  useRunIntakeReviewController,
} from "@/components/relay/RunIntakeReviewPanel";
import { RunWorkbenchLayout } from "@/components/relay/RunWorkbenchLayout";
import {
  RunWorkbenchLoadFailedState,
  RunWorkbenchLoadingState,
} from "@/components/relay/RunWorkbenchStates";
import { ValidationPanel } from "@/components/relay/ValidationPanel";
import { LogPreviewPanel } from "@/components/relay/LogPreviewPanel";
import { RunEvidenceBrowser } from "@/components/relay/RunEvidenceBrowser";

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
  const { data: events, isLoading: isLoadingEvents } = useQuery(
    runEventsQueryOptions(runId),
  );

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
    <IntakeWorkbench
      run={run}
      artifacts={artifacts || []}
      events={events || []}
    />
  );
}

function IntakeWorkbench({
  run,
  artifacts,
  events,
}: {
  run: RelayRun;
  artifacts: RelayArtifact[];
  events: RelayRunEvent[];
}) {

  // Format events as log preview lines
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
  const intakeReview = useRunIntakeReviewController({
    run,
    artifacts: artifacts || [],
  });

  return (
    <RunWorkbenchLayout
      run={{
        ...run,
        artifacts: artifacts || [],
        latestEvents: events || [],
        logPreview,
      }}
      currentStep="intake"
      stageActions={<RunIntakeStageActions controller={intakeReview} />}
      mainContent={<RunIntakeReviewPanel controller={intakeReview} />}
      inspectorPanels={{
        logs: <LogPreviewPanel logPreview={logPreview} />,
        artifacts: (
          <RunEvidenceBrowser
            runId={run.id}
            artifacts={artifacts || []}
            events={events || []}
          />
        ),
        validation: <ValidationPanel summary={run.validationSummary} />,
      }}
    />
  );
}
