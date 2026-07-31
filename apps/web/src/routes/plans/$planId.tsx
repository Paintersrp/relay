import { useQuery } from "@tanstack/react-query";
import {
  createFileRoute,
  Outlet,
  useRouterState,
} from "@tanstack/react-router";
import { RefreshCw } from "lucide-react";

import { RelayCanonicalPlanDetail } from "@/components/relay/RelayCanonicalPlanDetail";
import { RelayStateSurface } from "@/components/relay/RelayStateSurface";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { workflowPlanDetailQueryOptions } from "@/features/relay-plans";

export const Route = createFileRoute("/plans/$planId")({
  component: PlanDetailPage,
});

export function PlanDetailPage() {
  const { planId } = Route.useParams();
  const pathname = useRouterState({
    select: (state) => state.location.pathname,
  });
  const isPassDetailRoute = pathname.startsWith(`/plans/${planId}/passes/`);
  const planQuery = useQuery(workflowPlanDetailQueryOptions(planId));
  const shellClassName =
    "mx-auto flex w-full max-w-5xl flex-col gap-4 px-4 py-4 sm:px-6 sm:py-5";

  if (isPassDetailRoute) return <Outlet />;

  return (
    <section className="min-h-0 flex-1 overflow-y-auto bg-[var(--relay-page-body-bg)]">
      {planQuery.isLoading ? (
        <div className={shellClassName}>
          <Skeleton className="h-40 w-full rounded" />
          <Skeleton className="h-72 w-full rounded" />
        </div>
      ) : null}

      {!planQuery.isLoading && planQuery.error ? (
        <div className={shellClassName}>
          <RelayStateSurface
            tone="danger"
            title="Plan failed to load"
            description="Relay could not load this canonical Plan."
            metadata={`Plan ID: ${planId}`}
            action={
              <Button
                type="button"
                variant="outline"
                size="sm"
                onClick={() => void planQuery.refetch()}
              >
                <RefreshCw className="size-3.5" />
                Retry Plan
              </Button>
            }
          />
        </div>
      ) : null}

      {!planQuery.isLoading && !planQuery.error && planQuery.data ? (
        <div className={shellClassName}>
          <RelayCanonicalPlanDetail detail={planQuery.data} />
        </div>
      ) : null}
    </section>
  );
}
