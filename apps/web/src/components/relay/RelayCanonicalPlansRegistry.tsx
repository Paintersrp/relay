import * as React from "react";
import { Link } from "@tanstack/react-router";
import { ChevronRight } from "lucide-react";

import { RelayStateSurface } from "@/components/relay/RelayStateSurface";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import type { WorkflowProject } from "@/features/relay-projects";
import type {
  WorkflowPlanStatus,
  WorkflowPlanSummary,
} from "@/features/relay-plans";

interface RelayCanonicalPlansRegistryProps {
  plans?: WorkflowPlanSummary[];
  projects?: WorkflowProject[];
  isLoading?: boolean;
  error?: unknown;
}

type StatusFilter = WorkflowPlanStatus | "all";

export function RelayCanonicalPlansRegistry({
  plans = [],
  projects = [],
  isLoading = false,
  error,
}: RelayCanonicalPlansRegistryProps) {
  const [status, setStatus] = React.useState<StatusFilter>("active");
  const [projectId, setProjectId] = React.useState("all");
  const rows = React.useMemo(
    () =>
      [...plans]
        .filter((plan) => status === "all" || plan.status === status)
        .filter(
          (plan) =>
            projectId === "all" || plan.project.projectId === projectId,
        )
        .sort((a, b) => Date.parse(b.updatedAt) - Date.parse(a.updatedAt)),
    [plans, projectId, status],
  );

  if (isLoading) {
    return (
      <div className="space-y-3 p-4">
        {Array.from({ length: 5 }).map((_, index) => (
          <Skeleton key={index} className="h-20 w-full rounded" />
        ))}
      </div>
    );
  }

  if (error) {
    return (
      <div className="p-4">
        <RelayStateSurface
          tone="danger"
          title="Plans failed to load"
          description="Relay could not load the canonical Plan registry."
        />
      </div>
    );
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col overflow-hidden">
      <div className="flex flex-wrap gap-3 border-b border-[var(--relay-row-border)] px-4 py-3">
        <label className="flex items-center gap-2 text-xs text-muted-foreground">
          Status
          <select
            aria-label="Plan status"
            value={status}
            onChange={(event) =>
              setStatus(event.target.value as StatusFilter)
            }
            className="h-8 rounded border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)] px-2 text-xs"
          >
            <option value="active">Active</option>
            <option value="completed">Completed</option>
            <option value="all">All</option>
          </select>
        </label>
        <label className="flex items-center gap-2 text-xs text-muted-foreground">
          Project
          <select
            aria-label="Plan Project"
            value={projectId}
            onChange={(event) => setProjectId(event.target.value)}
            className="h-8 min-w-48 rounded border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)] px-2 text-xs"
          >
            <option value="all">All Projects</option>
            {projects.map((project) => (
              <option key={project.projectId} value={project.projectId}>
                {project.name} ({project.status})
              </option>
            ))}
          </select>
        </label>
        <span className="ml-auto self-center font-mono text-[10px] text-muted-foreground">
          {rows.length} shown / {plans.length} total
        </span>
      </div>

      {rows.length === 0 ? (
        <div className="p-4">
          <RelayStateSurface
            tone="empty"
            title="No Plans match"
            description="Choose another Project or status filter, or submit a canonical Plan."
            action={
              <Button asChild size="sm" variant="outline">
                <Link to="/plans/new">Submit Plan</Link>
              </Button>
            }
          />
        </div>
      ) : (
        <div className="min-h-0 flex-1 divide-y divide-[var(--relay-row-border)] overflow-y-auto">
          {rows.map((plan) => (
            <Link
              key={plan.planId}
              to="/plans/$planId"
              params={{ planId: plan.planId }}
              className="flex flex-col gap-3 px-5 py-4 transition-colors hover:bg-[var(--relay-panel-hover-bg)] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-[var(--relay-accent)] sm:flex-row sm:items-center sm:justify-between"
            >
              <div className="min-w-0">
                <div className="flex flex-wrap items-center gap-2">
                  <span className="font-medium">{plan.featureSlug}</span>
                  <Badge
                    variant={
                      plan.status === "completed" ? "success" : "running"
                    }
                  >
                    {plan.status}
                  </Badge>
                  <Badge variant="outline">{plan.project.name}</Badge>
                  {plan.project.status === "archived" ? (
                    <Badge variant="secondary">Project archived</Badge>
                  ) : null}
                </div>
                <p className="mt-1 break-all font-mono text-[10px] text-muted-foreground">
                  {plan.planId}
                </p>
                <p className="mt-2 text-xs text-muted-foreground">
                  {plan.completedPassCount}/{plan.passCount} passes completed
                </p>
              </div>
              <div className="flex items-center gap-2 text-xs text-muted-foreground">
                {new Date(plan.updatedAt).toLocaleString()}
                <ChevronRight className="size-4" />
              </div>
            </Link>
          ))}
        </div>
      )}
    </div>
  );
}
