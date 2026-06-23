import * as React from "react";
import { Link, useRouter } from "@tanstack/react-router";
import { ChevronRight, Plus, FolderGit2 } from "lucide-react";

import {
  RelayFilterTabs,
  type RelayFilterTabItem,
} from "@/components/relay/RelayFilterTabs";
import { RelayStateSurface } from "@/components/relay/RelayStateSurface";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Skeleton } from "@/components/ui/skeleton";
import { formatPlanDate, formatPlanDateRelative } from "@/components/relay/relayPlanVisualState";
import type { RelayProject } from "@/features/relay-projects";
import { cn } from "@/lib/utils";

interface RelayProjectsRegistryProps {
  projects?: RelayProject[];
  isLoading?: boolean;
  error?: unknown;
  className?: string;
}

type ProjectFilter = "all" | "active" | "archived";

function compareProjectsByUpdatedAtDesc(a: RelayProject, b: RelayProject): number {
  return Date.parse(b.updatedAt) - Date.parse(a.updatedAt);
}

function getFilterMatch(project: RelayProject, filter: ProjectFilter): boolean {
  switch (filter) {
    case "all":
      return true;
    case "active":
      return project.status === "active";
    case "archived":
      return project.status === "archived";
    default:
      return true;
  }
}

function ProjectStatusPill({ status }: { status: string }) {
  const isArchived = status === "archived";
  return (
    <Badge variant={isArchived ? "secondary" : "success"}>
      {isArchived ? "Archived" : "Active"}
    </Badge>
  );
}

function ProjectMetadataLine({ project }: { project: RelayProject }) {
  return (
    <div className="mt-0.5 flex flex-wrap items-center gap-x-1 gap-y-0.5 font-mono text-[10px] leading-4 text-muted-foreground">
      <span className="break-all">{project.projectId}</span>
    </div>
  );
}

function RelayProjectCompactRow({ project }: { project: RelayProject }) {
  return (
    <Link
      to="/projects/$projectId"
      params={{ projectId: project.projectId }}
      aria-label={`Open project ${project.name}`}
      className="rounded border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)] p-3 transition-colors hover:bg-[var(--relay-panel-hover-bg)] focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-[var(--relay-accent)]"
    >
      <div className="flex min-w-0 items-start gap-3">
        <div className="min-w-0 flex-1">
          <p className="truncate text-sm font-medium leading-snug text-foreground">
            {project.name}
          </p>
          <ProjectMetadataLine project={project} />
        </div>
        <ChevronRight className="mt-0.5 size-4 shrink-0 text-muted-foreground" />
      </div>

      <div className="mt-3 flex flex-wrap items-center gap-2">
        <ProjectStatusPill status={project.status} />
        {project.defaultRepositoryId && (
          <span className="inline-flex items-center gap-1 rounded bg-muted px-2 py-0.5 font-mono text-[9px] text-muted-foreground">
            <FolderGit2 className="size-2.5" />
            {project.defaultRepositoryId}
          </span>
        )}
      </div>

      {project.description && (
        <p className="mt-2 line-clamp-2 text-xs text-muted-foreground">
          {project.description}
        </p>
      )}

      <div className="mt-3 flex items-center justify-between text-[10px] text-muted-foreground">
        <span>
          {project.repositories?.length ?? 0} repos
        </span>
        <span title={formatPlanDate(project.updatedAt)}>
          Updated {formatPlanDateRelative(project.updatedAt)}
        </span>
      </div>
    </Link>
  );
}

function RelayProjectTableRow({ project }: { project: RelayProject }) {
  const router = useRouter();

  const openProject = React.useCallback(() => {
    void router.navigate({
      to: "/projects/$projectId",
      params: { projectId: project.projectId },
    });
  }, [project.projectId, router]);

  return (
    <tr
      role="link"
      tabIndex={0}
      aria-label={`Open project ${project.name}`}
      className="group cursor-pointer border-b border-[var(--relay-row-border)] transition-colors hover:bg-[var(--relay-panel-hover-bg)] focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-[var(--relay-accent)]"
      onClick={openProject}
      onKeyDown={(event) => {
        if (event.key === "Enter" || event.key === " ") {
          event.preventDefault();
          openProject();
        }
      }}
    >
      <td className="px-6 py-3.5 pr-3 align-middle">
        <div className="min-w-0">
          <p className="truncate text-sm font-medium leading-snug text-foreground">
            {project.name}
          </p>
          <ProjectMetadataLine project={project} />
        </div>
      </td>

      <td className="px-4 py-3.5 align-middle">
        <ProjectStatusPill status={project.status} />
      </td>

      <td className="px-4 py-3.5 align-middle">
        {project.defaultRepositoryId ? (
          <span className="inline-flex items-center gap-1 rounded bg-muted px-2 py-0.5 font-mono text-[10px] text-muted-foreground">
            <FolderGit2 className="size-3" />
            {project.defaultRepositoryId}
          </span>
        ) : (
          <span className="text-muted-foreground/60 font-mono text-xs">—</span>
        )}
      </td>

      <td className="px-4 py-3.5 align-middle">
        <span className="font-mono text-xs text-foreground/80">
          {project.repositories?.length ?? 0}
        </span>
      </td>

      <td className="px-4 py-3.5 align-middle">
        <span
          className="whitespace-nowrap text-[11px] text-muted-foreground"
          title={formatPlanDate(project.updatedAt)}
        >
          {formatPlanDateRelative(project.updatedAt)}
        </span>
      </td>

      <td className="px-3 py-3.5 text-right align-middle">
        <ChevronRight className="inline-block size-[13px] text-muted-foreground transition-colors group-hover:text-foreground/60" />
      </td>
    </tr>
  );
}

function TableHeader() {
  return (
    <thead>
      <tr className="border-b border-[var(--relay-row-border)]">
        <th className="px-6 py-2 text-left text-[10px] font-medium uppercase tracking-[0.08em] text-muted-foreground">
          Project
        </th>
        <th className="px-4 py-2 text-left text-[10px] font-medium uppercase tracking-[0.08em] text-muted-foreground">
          Status
        </th>
        <th className="px-4 py-2 text-left text-[10px] font-medium uppercase tracking-[0.08em] text-muted-foreground">
          Default Repo
        </th>
        <th className="px-4 py-2 text-left text-[10px] font-medium uppercase tracking-[0.08em] text-muted-foreground">
          Repositories
        </th>
        <th className="px-4 py-2 text-left text-[10px] font-medium uppercase tracking-[0.08em] text-muted-foreground">
          Updated
        </th>
        <th className="px-3 py-2" />
      </tr>
    </thead>
  );
}

function TableSkeletonRows() {
  return (
    <tbody>
      {Array.from({ length: 4 }).map((_, index) => (
        <tr key={`project-table-loading-${index}`} className="border-b border-[var(--relay-row-border)]">
          <td className="px-6 py-3.5">
            <Skeleton className="h-4 w-56" />
            <Skeleton className="mt-1.5 h-3 w-48" />
          </td>
          <td className="px-4 py-3.5">
            <Skeleton className="h-5 w-20 rounded-sm" />
          </td>
          <td className="px-4 py-3.5">
            <Skeleton className="h-4 w-28" />
          </td>
          <td className="px-4 py-3.5">
            <Skeleton className="h-4 w-8" />
          </td>
          <td className="px-4 py-3.5">
            <Skeleton className="h-4 w-16" />
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
  const [filter, setFilter] = React.useState<ProjectFilter>("all");
  const rows = projects ?? [];
  const sortedRows = [...rows].sort(compareProjectsByUpdatedAtDesc);
  const filteredProjects = sortedRows.filter((project) => getFilterMatch(project, filter));

  const filterItems: RelayFilterTabItem[] = [
    { value: "all", label: "All", count: rows.length },
    {
      value: "active",
      label: "Active",
      count: rows.filter((project) => project.status === "active").length,
    },
    {
      value: "archived",
      label: "Archived",
      count: rows.filter((project) => project.status === "archived").length,
    },
  ];

  return (
    <div
      className={cn(
        "flex min-h-0 flex-1 flex-col overflow-hidden border-t border-[var(--relay-row-border)] bg-[var(--relay-content-bg)]",
        className,
      )}
    >
      <div className="flex flex-wrap items-center justify-between gap-3 border-b border-[var(--relay-row-border)] px-4 py-2.5">
        <div className="flex min-w-0 items-center gap-3 text-xs">
          <span className="text-muted-foreground">
            <span className="font-mono text-foreground">{rows.length}</span> projects
          </span>
        </div>
      </div>

      <RelayFilterTabs
        value={filter}
        items={filterItems}
        onValueChange={(value) => setFilter(value as ProjectFilter)}
        listClassName="gap-0 px-4 pb-0"
        triggerClassName="h-auto flex-none gap-1.5 rounded-none border-b-2 border-transparent px-3 py-2.5 text-[12px] font-medium text-muted-foreground after:bottom-[-1px] after:h-px after:bg-info hover:text-foreground data-active:border-info data-active:text-foreground"
        countClassName="rounded-sm bg-muted px-1.5 py-0.5 text-[9px] text-muted-foreground data-active:bg-info/12 data-active:text-info"
      />

      <div className="min-h-0 flex-1">
        {isLoading ? (
          <div className="flex h-full min-h-0 flex-col">
            <div className="flex min-h-0 flex-1 flex-col gap-3 overflow-y-auto p-3 lg:hidden">
              {Array.from({ length: 4 }).map((_, index) => (
                <div
                  key={`project-compact-loading-${index}`}
                  className="rounded border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)] p-3"
                >
                  <div className="space-y-2">
                    <Skeleton className="h-4 w-40" />
                    <Skeleton className="h-3 w-52" />
                    <Skeleton className="h-5 w-20 rounded-sm" />
                  </div>
                </div>
              ))}
            </div>

            <div className="hidden min-h-0 flex-1 overflow-x-auto overflow-y-hidden lg:flex">
              <div className="flex min-h-0 h-full min-w-[980px] flex-1 flex-col">
                <table className="w-full table-fixed border-collapse">
                  <colgroup>
                    <col style={{ width: "35%" }} />
                    <col style={{ width: "12%" }} />
                    <col style={{ width: "20%" }} />
                    <col style={{ width: "13%" }} />
                    <col style={{ width: "15%" }} />
                    <col style={{ width: "5%" }} />
                  </colgroup>
                  <TableHeader />
                  <TableSkeletonRows />
                </table>
              </div>
            </div>
          </div>
        ) : null}

        {!isLoading && error ? (
          <div className="min-h-0 flex-1 overflow-y-auto p-4">
            <RelayStateSurface
              tone="danger"
              title="Projects failed to load"
              description="Relay could not load the projects registry. Check the API service and try again."
            />
          </div>
        ) : null}

        {!isLoading && !error && rows.length === 0 ? (
          <div className="min-h-0 flex-1 overflow-y-auto p-4">
            <RelayStateSurface
              tone="empty"
              title="No projects configured yet"
              description="Projects allow configuring directories and branches for source-aware orchestrations."
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
          <div className="min-h-0 flex-1 overflow-y-auto p-4">
            <RelayStateSurface
              tone="empty"
              title="No projects match this filter"
              description="Switch filters to view the rest of the projects registry."
              action={
                <Button variant="outline" size="sm" onClick={() => setFilter("all")}>
                  Show all projects
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
                  <RelayProjectCompactRow key={project.projectId} project={project} />
                ))}
              </div>
            </div>

            <div className="hidden min-h-0 flex-1 overflow-x-auto overflow-y-hidden lg:flex">
              <div className="flex min-h-0 h-full min-w-[980px] flex-1 flex-col">
                <table className="w-full table-fixed border-collapse">
                  <colgroup>
                    <col style={{ width: "35%" }} />
                    <col style={{ width: "12%" }} />
                    <col style={{ width: "20%" }} />
                    <col style={{ width: "13%" }} />
                    <col style={{ width: "15%" }} />
                    <col style={{ width: "5%" }} />
                  </colgroup>
                  <TableHeader />
                  <tbody>
                    {filteredProjects.map((project) => (
                      <RelayProjectTableRow key={project.projectId} project={project} />
                    ))}
                  </tbody>
                </table>
              </div>
            </div>
          </div>
        ) : null}
      </div>

      <div className="flex shrink-0 items-center justify-between border-t border-[var(--relay-row-border)] px-4 py-2 text-[10px] text-muted-foreground">
        <span className="font-mono">
          {rows.length} project{rows.length === 1 ? "" : "s"}
        </span>
        <span>
          Showing {filteredProjects.length}
          {filter === "all" ? "" : ` of ${rows.length}`}
        </span>
      </div>
    </div>
  );
}
