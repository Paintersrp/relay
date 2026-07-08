import { useQuery } from "@tanstack/react-query";
import { createFileRoute, Navigate } from "@tanstack/react-router";

import { RelayStateSurface } from "@/components/relay/RelayStateSurface";
import { Button } from "@/components/ui/button";
import {
  workflowRunDetailQueryOptions,
  workflowRunStageRoute,
} from "@/features/relay-runs";

export const Route = createFileRoute("/runs/$runId/")({
  component: RunIndexPage,
});

export function RunIndexPage() {
  const { runId } = Route.useParams();
  const query = useQuery(workflowRunDetailQueryOptions(runId));

  if (query.isLoading) {
    return (
      <RelayStateSurface
        tone="loading"
        title="Loading Run"
        description="Resolving the canonical Run stage."
      />
    );
  }

  if (query.error || !query.data) {
    return (
      <div className="p-6">
        <RelayStateSurface
          tone="danger"
          title="Run failed to load"
          description={
            query.error instanceof Error
              ? query.error.message
              : "Relay could not load this Run."
          }
          action={
            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={() => void query.refetch()}
            >
              Retry Run
            </Button>
          }
        />
      </div>
    );
  }

  return (
    <Navigate
      to={workflowRunStageRoute(query.data.run.stage)}
      params={{ runId }}
      replace
    />
  );
}
