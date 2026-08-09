import { useQuery } from "@tanstack/react-query";
import { createFileRoute, Link } from "@tanstack/react-router";
import { RelayFeatureWorkspaceDetail } from "@/components/relay/RelayFeatureWorkspaceDetail";
import { Button } from "@/components/ui/button";
import { featureWorkspaceGuidedQueryOptions } from "@/features/relay-feature-workspaces";

export const Route = createFileRoute("/feature-workspaces/$workspaceId")({
  component: FeatureWorkspacePage,
});
function FeatureWorkspacePage() {
  const { workspaceId } = Route.useParams();
  const query = useQuery(featureWorkspaceGuidedQueryOptions(workspaceId));
  return (
    <section
      data-testid="route-scroll-region"
      className="min-h-0 flex-1 overflow-y-auto bg-[var(--relay-page-body-bg)]"
    >
      {query.isLoading ? (
        <div className="mx-auto w-full max-w-5xl p-6">Loading workspace…</div>
      ) : query.error || !query.data ? (
        <div className="mx-auto w-full max-w-5xl space-y-3 p-6">
          <p role="alert">Workspace could not be loaded.</p>
          <Button asChild variant="outline">
            <Link to="/feature-workspaces/new">Create a workspace</Link>
          </Button>
        </div>
      ) : (
        <div className="mx-auto w-full max-w-5xl p-6">
          <RelayFeatureWorkspaceDetail detail={query.data} />
        </div>
      )}
    </section>
  );
}
