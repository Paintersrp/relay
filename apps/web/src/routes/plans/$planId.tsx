import { useQuery } from "@tanstack/react-query";
import { createFileRoute, Link } from "@tanstack/react-router";

import { AppPageFrame } from "@/components/relay/AppPageFrame";
import { RelayPlanDetail } from "@/components/relay/RelayPlanDetail";
import { RelayStateSurface } from "@/components/relay/RelayStateSurface";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { planDetailQueryOptions } from "@/features/relay-plans";

export const Route = createFileRoute("/plans/$planId")({
  component: PlanDetailPage,
});

function PlanDetailPage() {
  const { planId } = Route.useParams();
  const { data, isLoading, error } = useQuery(planDetailQueryOptions(planId));

  return (
    <AppPageFrame
      title="Plans"
      description="Managed multi-pass orchestration plans"
      bodyClassName="min-h-0 overflow-y-auto p-0"
    >
      {isLoading ? (
        <div className="space-y-6 p-6">
          <Skeleton className="h-6 w-40" />
          <Skeleton className="h-32 w-full rounded-2xl" />
          <div className="grid gap-6 xl:grid-cols-2">
            <Skeleton className="h-44 w-full rounded-2xl" />
            <Skeleton className="h-44 w-full rounded-2xl" />
          </div>
          <Skeleton className="h-56 w-full rounded-2xl" />
        </div>
      ) : null}

      {!isLoading && error ? (
        <div className="p-6">
          <RelayStateSurface
            tone="danger"
            title="Plan failed to load"
            description="Relay could not load this managed plan detail. Check the API process and try again."
            metadata={`Plan ID: ${planId}`}
            action={
              <Button variant="outline" size="sm" asChild>
                <Link to="/plans">Back to Plans</Link>
              </Button>
            }
          />
        </div>
      ) : null}

      {!isLoading && !error && (!data?.plan || !data.passes) ? (
        <div className="p-6">
          <RelayStateSurface
            tone="empty"
            title="Plan not available"
            description="Relay returned an incomplete plan detail response for this plan."
            metadata={`Plan ID: ${planId}`}
            action={
              <Button variant="outline" size="sm" asChild>
                <Link to="/plans">Back to Plans</Link>
              </Button>
            }
          />
        </div>
      ) : null}

      {!isLoading && !error && data?.plan && data.passes ? (
        <RelayPlanDetail
          plan={data.plan}
          passes={data.passes}
          completionReady={data.completionReady}
        />
      ) : null}
    </AppPageFrame>
  );
}
