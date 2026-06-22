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
  const shellClassName = "mx-auto flex w-full max-w-5xl flex-col gap-4 px-4 py-4 sm:px-6 sm:py-5";

  return (
    <section className="min-h-0 flex-1 overflow-y-auto bg-[var(--relay-page-body-bg)]">
      {isLoading ? (
        <div className={shellClassName}>
          <div className="space-y-3 border-b border-[var(--relay-row-border)] pb-4">
            <Skeleton className="h-4 w-40" />
            <Skeleton className="h-7 w-80 max-w-full" />
            <Skeleton className="h-4 w-full max-w-4xl" />
          </div>
          <Skeleton className="h-28 w-full rounded" />
          <Skeleton className="h-14 w-full rounded" />
          <Skeleton className="h-72 w-full rounded" />
          <Skeleton className="h-36 w-full rounded" />
        </div>
      ) : null}

      {!isLoading && error ? (
        <div className={shellClassName}>
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
        <div className={shellClassName}>
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
