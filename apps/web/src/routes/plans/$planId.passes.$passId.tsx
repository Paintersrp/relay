import { useQuery } from "@tanstack/react-query";
import { createFileRoute, Link } from "@tanstack/react-router";

import { RelayCanonicalPlanPassDetail } from "@/components/relay/RelayCanonicalPlanPassDetail";
import { RelayStateSurface } from "@/components/relay/RelayStateSurface";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import {
  workflowPlanDetailQueryOptions,
  workflowPlanPassQueryOptions,
} from "@/features/relay-plans";

export const Route = createFileRoute("/plans/$planId/passes/$passId")({
  component: PlanPassDetailPage,
});

function PlanPassDetailPage() {
  const { planId, passId } = Route.useParams();
  const planQuery = useQuery(workflowPlanDetailQueryOptions(planId));
  const passQuery = useQuery(workflowPlanPassQueryOptions(planId, passId));
  const loading = planQuery.isLoading || passQuery.isLoading;
  const error = planQuery.error || passQuery.error;
  const shellClassName =
    "mx-auto flex w-full max-w-5xl flex-col gap-4 px-4 py-4 sm:px-6 sm:py-5";

  return (
    <section className="min-h-0 flex-1 overflow-y-auto bg-[var(--relay-page-body-bg)]">
      {loading ? (
        <div className={shellClassName}>
          <Skeleton className="h-40 w-full rounded" />
          <Skeleton className="h-60 w-full rounded" />
        </div>
      ) : null}
      {!loading && error ? (
        <div className={shellClassName}>
          <RelayStateSurface
            tone="danger"
            title="Pass failed to load"
            description="Relay could not load this canonical Plan pass."
            metadata={`Plan ID: ${planId} / Pass ID: ${passId}`}
            action={
              <Button asChild variant="outline" size="sm">
                <Link to="/plans/$planId" params={{ planId }}>
                  Back to Plan
                </Link>
              </Button>
            }
          />
        </div>
      ) : null}
      {!loading && !error && planQuery.data && passQuery.data ? (
        <div className={shellClassName}>
          <RelayCanonicalPlanPassDetail
            plan={planQuery.data.plan}
            pass={passQuery.data}
          />
        </div>
      ) : null}
    </section>
  );
}
