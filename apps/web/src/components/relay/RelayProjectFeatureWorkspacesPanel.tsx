import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { ArrowRight, Plus } from "lucide-react";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { formatPlanDate } from "@/components/relay/relayPlanVisualState";
import { projectFeatureWorkspacesQueryOptions } from "@/features/relay-feature-workspaces";

interface RelayProjectFeatureWorkspacesPanelProps {
  projectId: string;
}

// Project-context discoverability for Feature workspaces (workflow state that
// is scoped to a Project, not a top-level primary-domain activity). Mirrors
// the read-only listing pattern used by RelayProjectPlansPanel, but adds a
// create action that carries the current Project identity via a typed route
// search param so the creation route does not require re-selecting the
// Project.
export function RelayProjectFeatureWorkspacesPanel({
  projectId,
}: RelayProjectFeatureWorkspacesPanelProps) {
  const query = useQuery(projectFeatureWorkspacesQueryOptions(projectId));

  return (
    <section
      aria-labelledby="project-feature-workspaces-heading"
      className="border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)]"
    >
      <div className="flex flex-wrap items-center justify-between gap-3 border-b border-[var(--relay-row-border)] px-5 py-3">
        <div>
          <h2
            id="project-feature-workspaces-heading"
            className="font-mono text-[10px] uppercase tracking-[0.18em] text-muted-foreground"
          >
            Feature Workspaces
          </h2>
          <p className="mt-1 text-xs text-muted-foreground">
            Bounded discovery and governing-authority workspaces organized by this Project.
          </p>
        </div>
        <Button asChild variant="outline" size="sm">
          <Link to="/feature-workspaces/new" search={{ projectId }}>
            <Plus className="size-3.5" />
            Create workspace
          </Link>
        </Button>
      </div>

      {query.isLoading ? (
        <div className="space-y-2 p-5">
          <Skeleton className="h-12 w-full rounded" />
          <Skeleton className="h-12 w-full rounded" />
        </div>
      ) : query.error ? (
        <div className="p-5 text-sm text-destructive" role="alert">
          Feature workspaces could not be loaded. The rest of this Project remains available.
        </div>
      ) : query.data && query.data.items.length === 0 ? (
        <div className="p-5 text-sm text-muted-foreground">
          No feature workspaces are attached to this Project yet.
        </div>
      ) : query.data ? (
        <div className="divide-y divide-[var(--relay-row-border)]">
          {query.data.items.map((workspace) => (
            <Link
              key={workspace.workspaceId}
              to="/feature-workspaces/$workspaceId"
              params={{ workspaceId: workspace.workspaceId }}
              className="flex flex-col gap-3 px-5 py-3 transition-colors hover:bg-[var(--relay-content-bg)] focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-inset focus-visible:ring-[var(--relay-accent)] sm:flex-row sm:items-center sm:justify-between"
            >
              <div className="min-w-0">
                <div className="flex flex-wrap items-center gap-2">
                  <span className="font-medium text-foreground">{workspace.featureSlug}</span>
                  <Badge variant={workspace.blocked ? "destructive" : workspace.state === "open" ? "running" : "outline"}>
                    {workspace.blocked ? "Needs recovery" : workspace.state}
                  </Badge>
                  <span className="text-xs text-muted-foreground">Version {workspace.version}</span>
                </div>
                <p className="mt-1 text-sm text-foreground/90">{workspace.progressionSummary}</p>
                <p className="mt-1 text-xs text-muted-foreground">
                  {workspace.resumeSummary}{workspace.blockedReason ? ` ${workspace.blockedReason}` : ""}
                </p>
                <p className="mt-1 text-xs text-muted-foreground">
                  Updated {formatPlanDate(workspace.updatedAt)}
                </p>
              </div>
              <span className="inline-flex items-center gap-1 text-xs text-muted-foreground">
                {workspace.blocked ? "Review recovery" : workspace.resumeSummary}
                <ArrowRight className="size-3.5" />
              </span>
            </Link>
          ))}
        </div>
      ) : null}
    </section>
  );
}
