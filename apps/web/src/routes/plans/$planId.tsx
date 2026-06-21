import { useQuery } from "@tanstack/react-query";
import { createFileRoute, Link } from "@tanstack/react-router";

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
    <section className="min-h-0 flex-1 overflow-y-auto bg-[var(--relay-page-body-bg)]">
      {isLoading ? (
        <div className="mx-auto flex w-full max-w-6xl flex-col gap-5 p-4 sm:p-6">
          <Skeleton className="h-5 w-64" />
          <div className="space-y-3">
            <Skeleton className="h-8 w-96 max-w-full" />
            <Skeleton className="h-4 w-full max-w-3xl" />
            <Skeleton className="h-4 w-full max-w-4xl" />
          </div>
          <Skeleton className="h-32 w-full rounded-xl" />
          <Skeleton className="h-24 w-full rounded-xl" />
          <Skeleton className="h-72 w-full rounded-xl" />
          <Skeleton className="h-40 w-full rounded-xl" />
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
    </section>
  );
}
