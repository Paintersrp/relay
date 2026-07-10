import * as React from "react";
import { Link } from "@tanstack/react-router";
import { ChevronRight, Plus } from "lucide-react";

import {
  RelayFilterTabs,
  type RelayFilterTabItem,
} from "@/components/relay/RelayFilterTabs";
import { RelayStateSurface } from "@/components/relay/RelayStateSurface";
import { formatPlanDate, formatPlanDateRelative } from "@/components/relay/relayPlanVisualState";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import type { WorkflowProject } from "@/features/relay-projects";
import { cn } from "@/lib/utils";

interface RelayProjectsRegistryProps {
  projects?: WorkflowProject[];
  isLoading?: boolean;
  error?: unknown;
  className?: string;
}

type ProjectFilter = "active" | "archived" | "all";

function compareProjectsByUpdatedAtDesc(a: WorkflowProject, b: WorkflowProject): number {
  return Date.parse(b.updatedAt) - Date.parse(a.updatedAt);
}

function ProjectStatusPill({ status }: { status: WorkflowProject["status"] }) {
  return (
    <Badge variant={status === "archived" ? "secondary" : "success"}>
      {status === "archived" ? "Archived" : "Active"}
    </Badge>
  );
}

function ProjectMetadata({ project }: { project: WorkflowProject }) {
  return (
    <div className="mt-0.5 flex flex-wrap items-center gap-x-2 gap-y-0.5 font-mono text-[10px] leading-4 text-muted-foreground">
      <span className="break-all">{project.projectId}</span>
      <span title={formatPlanDate(project.updatedAt)}>
        Updated {formatPlanDateRelative(project.updatedAt)}
      </span>
    </div>
  );
}

function ProjectCompactRow({ project }: { project: WorkflowProject }) {
  const projectParams = { projectId: project.projectId };
  return (
    <Link
      to="/projects/$projectId"
      params={projectParams}
      aria-label={`Open project ${project.name}`}
      className="rounded border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)] p-3 transition-colors hover:bg-[var(--relay-panel-hover-bg)] focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-[var(--relay-accent)]"
    >
      <div className="flex min-w-0 items-start gap-3">
        <div className="min-w-0 flex-1">
          <p className="truncate text-sm font-medium leading-snug text-foreground">
            {project.name}
          </p>
          <ProjectMetadata project={project} />
        </div>
        <ChevronRight className="mt-0.5 size-4 shrink-0 text-muted-foreground" />
      </div>
      {project.description ? (
        <p className="mt-2 line-clamp-2 text-xs text-muted-foreground">
          {project.description}
        </p>
      ) : null}
      <div className="mt-3">
        <ProjectStatusPill status={project.status} />
      </div>
    </Link>
  );
}

function ProjectTableRow({ project }: { project: WorkflowProject }) {
  const projectParams = { projectId: project.projectId };
  return (
    <tr className="border-b border-[var(--relay-row-border)] last:border-b-0 hover:bg-[var(--relay-panel-hover-bg)]">
      <td className="px-6 py-3.5 align-middle">
        <Link
          to="/projects/$projectId"
          params={projectParams}
          className="block rounded-sm focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-[var(--relay-accent)]"
        >
          <p className="truncate text-sm font-medium leading-snug text-foreground">
            {project.name}
          </p>
          <ProjectMetadata project={project} />
        </Link>
      </td>
      <td className="px-4 py-3.5 align-middle">
        <ProjectStatusPill status={project.status} />
      </td>
      <td className="px-4 py-3.5 align-middle text-xs text-muted-foreground">
        <span title={formatPlanDate(project.updatedAt)}>
          {formatPlanDateRelative(project.updatedAt)}
        </span>
      </td>
      <td className="px-3 py-3.5 text-right align-middle">
        <Link
          to="/projects/$projectId"
          params={projectParams}
          aria-label={`Open project ${project.name}`}
          className="inline-flex rounded-sm p-1 text-muted-foreground hover:text-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-[var(--relay-accent)]"
        >
          <ChevronRight className="size-4" />
        </Link>
      </td>
    </tr>
  );
}

function TableSkeleton() {
  return (
    <tbody>
      {Array.from({ length: 4 }).map((_, index) => (
        <tr key={`project-loading-${index}`} className="border-b border-[var(--relay-row-border)]">
          <td className="px-6 py-3.5">
            <Skeleton className="h-4 w-56" />
            <Skeleton className="mt-1.5 h-3 w-44" />
          </td>
          <td className="px-4 py-3.5">
            <Skeleton className="h-5 w-20 rounded-sm" />
          </td>
          <td className="px-4 py-3.5">
            <Skeleton className="h-4 w-20" />
          </td>
          <td className="px-3 py-3.5">
            <Skeleton className="ml-auto h-4 w-4" />
          </td>
        </tr>
      ))}
    </tbody>
  );
}

export function RelayProjectsRegistry({
  projects,
  isLoading = false,
  error,
  className,
}: RelayProjectsRegistryProps) {
  const [filter, setFilter] = React.useState<ProjectFilter>("active");
  const rows = React.useMemo(
    () => [...(projects ?? [])].sort(compareProjectsByUpdatedAtDesc),
    [projects],
  );
  const activeCount = rows.filter((project) => project.status === "active").length;
  const archivedCount = rows.filter((project) => project.status === "archived").length;
  const filteredProjects = rows.filter((project) => {
    if (filter === "all") return true;
    return project.status === filter;
  });
  const filterItems: RelayFilterTabItem[] = [
    { value: "active", label: "Active", count: activeCount },
    { value: "archived", label: "Archived", count: archivedCount },
    { value: "all", label: "All", count: rows.length },
  ];

  return (
    <div
      className={cn(
        "flex min-h-0 flex-1 flex-col overflow-hidden border-t border-[var(--relay-row-border)] bg-[var(--relay-content-bg)]",
        className,
      )}
    >
      <div className="flex flex-wrap items-center justify-between gap-3 border-b border-[var(--relay-row-border)] px-4 py-2.5 text-xs text-muted-foreground">
        <span>
          <span className="font-mono text-foreground">{activeCount}</span> active
        </span>
        <span>
          <span className="font-mono text-foreground">{archivedCount}</span> archived
        </span>
      </div>

      <RelayFilterTabs
        value={filter}
        items={filterItems}
        onValueChange={(value) => setFilter(value as ProjectFilter)}
        listClassName="gap-0 px-4 pb-0"
        triggerClassName="h-auto flex-none gap-1.5 rounded-none border-b-2 border-transparent px-3 py-2.5 text-[12px] font-medium text-muted-foreground after:bottom-[-1px] after:h-px after:bg-info hover:text-foreground data-active:border-info data-active:text-foreground"
        countClassName="rounded-sm bg-muted px-1.5 py-0.5 text-[9px] text-muted-foreground data-active:bg-info/12 data-active:text-info"
      />

      <div className="min-h-0 flex-1" aria-live="polite">
        {isLoading ? (
          <div className="flex h-full min-h-0 flex-col">
            <div className="flex min-h-0 flex-1 flex-col gap-3 overflow-y-auto p-3 lg:hidden">
              {Array.from({ length: 4 }).map((_, index) => (
                <div
                  key={`project-card-loading-${index}`}
                  className="rounded border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)] p-3"
                >
                  <Skeleton className="h-4 w-40" />
                  <Skeleton className="mt-2 h-3 w-52" />
                  <Skeleton className="mt-3 h-5 w-20" />
                </div>
              ))}
            </div>
            <div className="hidden min-h-0 flex-1 overflow-x-auto lg:block">
              <table className="w-full min-w-[720px] table-fixed border-collapse">
                <thead>
                  <tr className="border-b border-[var(--relay-row-border)]">
                    <th className="w-[56%] px-6 py-2 text-left text-[10px] font-medium uppercase tracking-[0.08em] text-muted-foreground">Project</th>
                    <th className="w-[16%] px-4 py-2 text-left text-[10px] font-medium uppercase tracking-[0.08em] text-muted-foreground">Status</th>
                    <th className="w-[22%] px-4 py-2 text-left text-[10px] font-medium uppercase tracking-[0.08em] text-muted-foreground">Updated</th>
                    <th className="w-[6%] px-3 py-2" />
                  </tr>
                </thead>
                <TableSkeleton />
              </table>
            </div>
          </div>
        ) : null}

        {!isLoading && error ? (
          <div className="p-4">
            <RelayStateSurface
              tone="danger"
              title="Projects failed to load"
              description="Relay could not load the Project registry. Check the API service and try again."
            />
          </div>
        ) : null}

        {!isLoading && !error && rows.length === 0 ? (
          <div className="p-4">
            <RelayStateSurface
              tone="empty"
              title="No Projects yet"
              description="Create a Project to organize Plans, repository references, and Project Notes."
              action={
                <Button asChild variant="outline" size="sm">
                  <Link to="/projects/new">
                    <Plus className="size-3.5" />
                    New Project
                  </Link>
                </Button>
              }
            />
          </div>
        ) : null}

        {!isLoading && !error && rows.length > 0 && filteredProjects.length === 0 ? (
          <div className="p-4">
            <RelayStateSurface
              tone="empty"
              title={`No ${filter} Projects`}
              description="Choose another filter to view the rest of the Project registry."
              action={
                <Button variant="outline" size="sm" onClick={() => setFilter("all")}>
                  Show all Projects
                </Button>
              }
            />
          </div>
        ) : null}

        {!isLoading && !error && filteredProjects.length > 0 ? (
          <div className="flex h-full min-h-0 flex-col">
            <div className="min-h-0 flex-1 overflow-y-auto p-3 lg:hidden">
              <div className="flex flex-col gap-3">
                {filteredProjects.map((project) => (
                  <ProjectCompactRow key={project.projectId} project={project} />
                ))}
              </div>
            </div>
            <div className="hidden min-h-0 flex-1 overflow-x-auto lg:block">
              <table className="w-full min-w-[720px] table-fixed border-collapse">
                <thead>
                  <tr className="border-b border-[var(--relay-row-border)]">
                    <th className="w-[56%] px-6 py-2 text-left text-[10px] font-medium uppercase tracking-[0.08em] text-muted-foreground">Project</th>
                    <th className="w-[16%] px-4 py-2 text-left text-[10px] font-medium uppercase tracking-[0.08em] text-muted-foreground">Status</th>
                    <th className="w-[22%] px-4 py-2 text-left text-[10px] font-medium uppercase tracking-[0.08em] text-muted-foreground">Updated</th>
                    <th className="w-[6%] px-3 py-2" />
                  </tr>
                </thead>
                <tbody>
                  {filteredProjects.map((project) => (
                    <ProjectTableRow key={project.projectId} project={project} />
                  ))}
                </tbody>
              </table>
            </div>
          </div>
        ) : null}
      </div>

      <div className="flex shrink-0 items-center justify-between border-t border-[var(--relay-row-border)] px-4 py-2 text-[10px] text-muted-foreground">
        <span className="font-mono">{rows.length} total</span>
        <span>{filteredProjects.length} shown</span>
      </div>
    </div>
  );
}
