import { useQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";

import { AppPageFrame } from "@/components/relay/AppPageFrame";
import { RelayCanonicalPlansRegistry } from "@/components/relay/RelayCanonicalPlansRegistry";
import { workflowPlansListQueryOptions } from "@/features/relay-plans";
import { workflowProjectsListQueryOptions } from "@/features/relay-projects";

export const Route = createFileRoute("/plans/")({
  component: PlansListPage,
});

function PlansListPage() {
  const plansQuery = useQuery(workflowPlansListQueryOptions({ limit: 100 }));
  const projectsQuery = useQuery(
    workflowProjectsListQueryOptions({ limit: 100 }),
  );

  return (
    <AppPageFrame
      title="Plans"
      description="Historical canonical Plans organized by Relay Projects."
      bodyClassName="flex min-h-0 flex-col overflow-hidden p-0"
    >
      <RelayCanonicalPlansRegistry
        plans={plansQuery.data?.plans}
        projects={projectsQuery.data?.projects}
        isLoading={plansQuery.isLoading || projectsQuery.isLoading}
        error={plansQuery.error || projectsQuery.error}
      />
    </AppPageFrame>
  );
}
