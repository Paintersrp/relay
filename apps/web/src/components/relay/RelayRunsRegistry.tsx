import * as React from "react";
import { Link } from "@tanstack/react-router";
import { useVirtualizer } from "@tanstack/react-virtual";

import {
  RelayFilterTabs,
  type RelayFilterTabItem,
} from "@/components/relay/RelayFilterTabs";
import {
  RelayRunCompactRow,
  RelayRunTableRow,
} from "@/components/relay/RelayRunsRegistryRows";
import {
  getRelayAttentionReason,
  type RelayAttentionReason,
} from "@/components/relay/relayVisualState";
import { RelayStateSurface } from "@/components/relay/RelayStateSurface";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import {
  type RelayRun,
  type RelayRunStatus,
} from "@/features/relay-runs";
import { cn } from "@/lib/utils";

type RunsRegistryFilter =
  | "all"
  | "attention"
  | "running"
  | "blocked"
  | "audit"
  | "complete";

interface RelayRunsRegistryProps {
  runs?: RelayRun[];
  isLoading?: boolean;
  error?: unknown;
  className?: string;
}

function getRunAttentionReason(run: RelayRun): RelayAttentionReason {
  if (run.validationSummary?.errors > 0) {
    return "validation-failed";
  }

  return getRelayAttentionReason(run.status);
}

function isRunningStatus(status: RelayRunStatus): boolean {
  return status === "executor_dispatched" || status === "executor_running";
}

function isCompleteStatus(status: RelayRunStatus): boolean {
  return (
    status === "accepted" ||
    status === "completed" ||
    status === "executor_done" ||
    status === "agent_done"
  );
}

function getFilterMatch(run: RelayRun, filter: RunsRegistryFilter): boolean {
  const attention = getRunAttentionReason(run);

  switch (filter) {
    case "all":
      return true;
    case "attention":
      return attention !== "none";
    case "running":
      return isRunningStatus(run.status);
    case "blocked":
      return attention === "executor-blocked";
    case "audit":
      return attention === "audit-required";
    case "complete":
      return isCompleteStatus(run.status);
    default:
      return true;
  }
}

function compareRunsByUpdatedAtDesc(a: RelayRun, b: RelayRun): number {
  return Date.parse(b.updatedAt) - Date.parse(a.updatedAt);
}

const registryColumns =
  "minmax(20rem,2.4fr) minmax(10rem,1fr) minmax(9rem,0.8fr) minmax(12rem,1fr) minmax(8rem,0.8fr) minmax(10rem,0.9fr) 2.5rem";

export function RelayRunsRegistry({
  runs,
  isLoading = false,
  error,
  className,
}: RelayRunsRegistryProps) {
  const [filter, setFilter] = React.useState<RunsRegistryFilter>("all");
  const rows = runs ?? [];
  const sortedRows = [...rows].sort(compareRunsByUpdatedAtDesc);
  const filteredRuns = sortedRows.filter((run) => getFilterMatch(run, filter));
  const attentionCount = rows.filter(
    (run) => getRunAttentionReason(run) !== "none",
  ).length;
  const scrollParentRef = React.useRef<HTMLDivElement>(null);
  const rowVirtualizer = useVirtualizer({
    count: filteredRuns.length,
    getScrollElement: () => scrollParentRef.current,
    estimateSize: () => 60,
    overscan: 8,
  });

  const filterItems: RelayFilterTabItem[] = [
    { value: "all", label: "All Runs", count: rows.length },
    { value: "attention", label: "Needs Attention", count: attentionCount },
    {
      value: "running",
      label: "Running",
      count: rows.filter((run) => isRunningStatus(run.status)).length,
    },
    {
      value: "blocked",
      label: "Executor Blocked",
      count: rows.filter(
        (run) => getRunAttentionReason(run) === "executor-blocked",
      ).length,
    },
    {
      value: "audit",
      label: "Audit Required",
      count: rows.filter(
        (run) => getRunAttentionReason(run) === "audit-required",
      ).length,
    },
    {
      value: "complete",
      label: "Complete",
      count: rows.filter((run) => isCompleteStatus(run.status)).length,
    },
  ];

  return (
    <div
      className={cn(
        "flex min-h-0 flex-1 flex-col overflow-hidden border-t border-[var(--relay-row-border)] bg-[var(--relay-content-bg)]",
        className,
      )}
    >
      <div className="flex flex-wrap items-center justify-between gap-3 border-b border-[var(--relay-row-border)] px-4 py-3">
        <div className="flex min-w-0 items-center gap-2">
          <h2 className="text-sm font-semibold text-foreground">Runs</h2>
          <span className="font-mono text-[11px] text-muted-foreground">
            {rows.length}
          </span>
          {attentionCount > 0 ? (
            <span className="font-mono text-[11px] text-[var(--warning)]">
              {attentionCount} need attention
            </span>
          ) : null}
        </div>
        <span className="font-mono text-xs text-muted-foreground">Updated</span>
      </div>

      <div className="pt-2">
        <RelayFilterTabs
          value={filter}
          items={filterItems}
          onValueChange={(value) => setFilter(value as RunsRegistryFilter)}
        />
      </div>

      <div className="min-h-0 flex-1">
        {isLoading ? (
          <div className="min-h-0 flex h-full flex-col">
            <div className="flex min-h-0 flex-1 flex-col gap-3 overflow-y-auto p-3 lg:hidden">
              {Array.from({ length: 5 }).map((_, index) => (
                <div
                  key={`compact-loading-row-${index}`}
                  className="rounded border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)] p-3"
                >
                  <div className="space-y-2">
                    <Skeleton className="h-4 w-40" />
                    <Skeleton className="h-3 w-56" />
                    <div className="flex flex-wrap gap-2 pt-1">
                      <Skeleton className="h-6 w-24" />
                      <Skeleton className="h-6 w-20" />
                      <Skeleton className="h-6 w-28" />
                    </div>
                  </div>
                </div>
              ))}
            </div>

            <div className="hidden min-h-0 flex-1 overflow-x-auto overflow-y-hidden lg:flex">
              <div className="flex h-full min-h-0 min-w-[980px] flex-1 flex-col">
                <div
                  className="grid shrink-0 border-b border-[var(--relay-row-border)] py-2 text-xs font-semibold text-foreground"
                  style={{ gridTemplateColumns: registryColumns }}
                >
                  <div className="px-4">Run</div>
                  <div className="px-4">Status</div>
                  <div className="px-4">Stage</div>
                  <div className="px-4">Executor</div>
                  <div className="px-4">Updated</div>
                  <div className="px-4">Attention</div>
                  <div className="pr-2" />
                </div>

                <div className="min-h-0 flex-1 overflow-y-auto">
                  {Array.from({ length: 5 }).map((_, index) => (
                    <div
                      key={`loading-row-${index}`}
                      className="grid border-b border-[var(--relay-row-border)]"
                      style={{ gridTemplateColumns: registryColumns }}
                    >
                      <div className="px-4 py-3">
                        <div className="space-y-2">
                          <Skeleton className="h-4 w-56" />
                          <Skeleton className="h-3 w-40" />
                        </div>
                      </div>
                      <div className="px-4 py-3">
                        <Skeleton className="h-6 w-28" />
                      </div>
                      <div className="px-4 py-3">
                        <Skeleton className="h-4 w-20" />
                      </div>
                      <div className="px-4 py-3">
                        <Skeleton className="h-4 w-24" />
                      </div>
                      <div className="px-4 py-3">
                        <Skeleton className="h-4 w-20" />
                      </div>
                      <div className="px-4 py-3">
                        <Skeleton className="h-6 w-24" />
                      </div>
                      <div className="px-4 py-3">
                        <Skeleton className="ml-auto h-4 w-4" />
                      </div>
                    </div>
                  ))}
                </div>
              </div>
            </div>
          </div>
        ) : null}

        {!isLoading && error ? (
          <div className="min-h-0 flex-1 overflow-y-auto p-4">
            <RelayStateSurface
              tone="danger"
              title="Runs failed to load"
              description="Relay could not load the runs registry. Check the API process and try again."
            />
          </div>
        ) : null}

        {!isLoading && !error && rows.length === 0 ? (
          <div className="min-h-0 flex-1 overflow-y-auto p-4">
            <RelayStateSurface
              tone="empty"
              title="No runs yet"
              description="Create a run from a Planner handoff to start Relay orchestration."
              action={
                <Button size="sm" asChild>
                  <Link to="/runs/new">New Run</Link>
                </Button>
              }
            />
          </div>
        ) : null}

        {!isLoading && !error && rows.length > 0 && filteredRuns.length === 0 ? (
          <div className="min-h-0 flex-1 overflow-y-auto">
            <div className="p-4">
              <RelayStateSurface
                tone="empty"
                title="No runs match this filter"
                description="Switch filters to view the rest of the run registry."
                action={
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => setFilter("all")}
                  >
                    Show all runs
                  </Button>
                }
              />
            </div>
          </div>
        ) : null}

        {!isLoading && !error && filteredRuns.length > 0 ? (
          <div className="min-h-0 flex h-full flex-col">
            <div className="min-h-0 flex-1 overflow-y-auto p-3 lg:hidden">
              <div className="flex flex-col gap-3">
                {filteredRuns.map((run) => {
                  const attentionReason = getRunAttentionReason(run);
                  const attentionCountValue =
                    attentionReason === "validation-failed" &&
                    run.validationSummary.errors > 0
                      ? run.validationSummary.errors
                      : undefined;

                  return (
                    <RelayRunCompactRow
                      key={run.id}
                      run={run}
                      attentionReason={attentionReason}
                      attentionCountValue={attentionCountValue}
                    />
                  );
                })}
              </div>
            </div>

            <div className="hidden min-h-0 flex-1 overflow-x-auto overflow-y-hidden lg:flex">
              <div className="flex h-full min-h-0 min-w-[980px] flex-1 flex-col">
                <div
                  className="grid shrink-0 border-b border-[var(--relay-row-border)] py-2 text-xs font-semibold text-foreground"
                  style={{ gridTemplateColumns: registryColumns }}
                >
                  <div className="px-4">Run</div>
                  <div className="px-4">Status</div>
                  <div className="px-4">Stage</div>
                  <div className="px-4">Executor</div>
                  <div className="px-4">Updated</div>
                  <div className="px-4">Attention</div>
                  <div className="pr-2" />
                </div>

                <div
                  ref={scrollParentRef}
                  className="min-h-0 flex-1 overflow-y-auto"
                >
                  <div
                    className="relative"
                    style={{ height: `${rowVirtualizer.getTotalSize()}px` }}
                  >
                    {rowVirtualizer.getVirtualItems().map((virtualRow) => {
                      const run = filteredRuns[virtualRow.index];
                      const attentionReason = getRunAttentionReason(run);
                      const attentionCountValue =
                        attentionReason === "validation-failed" &&
                        run.validationSummary.errors > 0
                          ? run.validationSummary.errors
                          : undefined;

                      return (
                        <RelayRunTableRow
                          key={run.id}
                          run={run}
                          attentionReason={attentionReason}
                          attentionCountValue={attentionCountValue}
                          columns={registryColumns}
                          style={{
                            height: `${virtualRow.size}px`,
                            transform: `translateY(${virtualRow.start}px)`,
                          }}
                        />
                      );
                    })}
                  </div>
                </div>
              </div>
            </div>
          </div>
        ) : null}
      </div>

      <div className="flex shrink-0 items-center justify-between border-t border-[var(--relay-row-border)] px-4 py-2 font-mono text-[11px] text-muted-foreground">
        <span>
          {filteredRuns.length} run{filteredRuns.length === 1 ? "" : "s"}
        </span>
        <span>
          {filter === "all"
            ? "Showing all runs"
            : `Filtered from ${rows.length} total`}
        </span>
      </div>
    </div>
  );
}
