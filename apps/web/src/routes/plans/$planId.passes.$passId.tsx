import { useQuery } from "@tanstack/react-query";
import { createFileRoute, Link } from "@tanstack/react-router";

import { RelayPlanPassDetail } from "@/components/relay/RelayPlanPassDetail";
import { RelayStateSurface } from "@/components/relay/RelayStateSurface";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import {
  planDetailQueryOptions,
  planPassDetailQueryOptions,
} from "@/features/relay-plans";

export const Route = createFileRoute("/plans/$planId/passes/$passId")({
  component: PlanPassDetailPage,
});

function PlanPassDetailPage() {
  const { planId, passId } = Route.useParams();
  const passDetailQuery = useQuery(planPassDetailQueryOptions(planId, passId));
  const planDetailQuery = useQuery(planDetailQueryOptions(planId));
  const isLoading = passDetailQuery.isLoading || planDetailQuery.isLoading;
  const error = passDetailQuery.error || planDetailQuery.error;
  const shellClassName =
    "mx-auto flex w-full max-w-6xl flex-col gap-4 px-4 py-4 sm:px-6 sm:py-5";

  return (
    <section className="min-h-0 flex-1 overflow-y-auto bg-[var(--relay-page-body-bg)]">
      {isLoading ? (
        <div className={shellClassName}>
          <div className="space-y-3 border-b border-[var(--relay-row-border)] pb-4">
            <Skeleton className="h-4 w-56" />
            <Skeleton className="h-7 w-96 max-w-full" />
            <Skeleton className="h-4 w-full max-w-4xl" />
          </div>
          <div className="grid gap-4 lg:grid-cols-[minmax(0,1fr)_20rem]">
            <div className="space-y-4">
              <Skeleton className="h-32 w-full rounded" />
              <Skeleton className="h-28 w-full rounded" />
              <Skeleton className="h-64 w-full rounded" />
              <Skeleton className="h-44 w-full rounded" />
            </div>
            <div className="space-y-4">
              <Skeleton className="h-72 w-full rounded" />
              <Skeleton className="h-36 w-full rounded" />
            </div>
          </div>
        </div>
      ) : null}

      {!isLoading && error ? (
        <div className={shellClassName}>
          <RelayStateSurface
            tone="danger"
            title="Pass failed to load"
            description="Relay could not load this managed plan pass detail. Check the API process and try again."
            metadata={`Plan ID: ${planId} / Pass ID: ${passId}`}
            action={
              <Button variant="outline" size="sm" asChild>
                <Link to="/plans/$planId" params={{ planId }}>
                  Back to Plan
                </Link>
              </Button>
            }
          />
        </div>
      ) : null}

      {!isLoading &&
      !error &&
      (!passDetailQuery.data?.plan ||
        !passDetailQuery.data.pass ||
        !planDetailQuery.data?.passes) ? (
        <div className={shellClassName}>
          <RelayStateSurface
            tone="empty"
            title="Pass not available"
            description="Relay returned an incomplete pass detail response for this plan."
            metadata={`Plan ID: ${planId} / Pass ID: ${passId}`}
            action={
              <Button variant="outline" size="sm" asChild>
                <Link to="/plans/$planId" params={{ planId }}>
                  Back to Plan
                </Link>
              </Button>
            }
          />
        </div>
      ) : null}

      {!isLoading &&
      !error &&
      passDetailQuery.data?.plan &&
      passDetailQuery.data.pass &&
      planDetailQuery.data?.passes ? (
        <RelayPlanPassDetail
          plan={passDetailQuery.data.plan}
          pass={passDetailQuery.data.pass}
          passes={planDetailQuery.data.passes}
          completionReady={passDetailQuery.data.completionReady}
        />
      ) : null}
    </section>
  );
}
