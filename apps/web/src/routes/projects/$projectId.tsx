import { useQuery } from "@tanstack/react-query";
import { createFileRoute, Link } from "@tanstack/react-router";

import { RelayProjectDetail } from "@/components/relay/RelayProjectDetail";
import { RelayStateSurface } from "@/components/relay/RelayStateSurface";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { projectDetailQueryOptions } from "@/features/relay-projects";

export const Route = createFileRoute("/projects/$projectId")({
  component: ProjectDetailPage,
});

function ProjectDetailPage() {
  const { projectId } = Route.useParams();
  const { data, isLoading, error } = useQuery(projectDetailQueryOptions(projectId));
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
            title="Project failed to load"
            description="Relay could not load this project configuration. Check the API process and try again."
            metadata={`Project ID: ${projectId}`}
            action={
              <Button variant="outline" size="sm" asChild>
                <Link to="/projects">Back to Projects</Link>
              </Button>
            }
          />
        </div>
      ) : null}

      {!isLoading && !error && !data?.project ? (
        <div className={shellClassName}>
          <RelayStateSurface
            tone="empty"
            title="Project not available"
            description="Relay returned an incomplete project response for this project identifier."
            metadata={`Project ID: ${projectId}`}
            action={
              <Button variant="outline" size="sm" asChild>
                <Link to="/projects">Back to Projects</Link>
              </Button>
            }
          />
        </div>
      ) : null}

      {!isLoading && !error && data?.project ? (
        <div className={shellClassName}>
          <RelayProjectDetail project={data.project} />
        </div>
      ) : null}
    </section>
  );
}
