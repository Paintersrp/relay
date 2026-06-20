import * as React from "react";
import { Link } from "@tanstack/react-router";
import {
  ArrowRight,
  Bot,
  GitBranch,
  GitFork,
  Package,
  TimerReset,
} from "lucide-react";

import { RelayAttentionBadge } from "@/components/relay/RelayAttentionBadge";
import { RelayFilterTabs, type RelayFilterTabItem } from "@/components/relay/RelayFilterTabs";
import { RelayMetaItem, RelayMetaRow, RelayMonoText } from "@/components/relay/RelayMeta";
import { RelayStageLabel } from "@/components/relay/RelayStageLabel";
import { StatusBadge } from "@/components/relay/StatusBadge";
import {
  getRelayAttentionReason,
  type RelayAttentionReason,
} from "@/components/relay/relayVisualState";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import {
  formatRunDate,
  formatRunDateRelative,
  getActiveStepRoute,
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

export function RelayRunsRegistry({
  runs,
  isLoading = false,
  className,
}: RelayRunsRegistryProps) {
  const [filter, setFilter] = React.useState<RunsRegistryFilter>("all");
  const rows = runs ?? [];
  const sortedRows = [...rows].sort(compareRunsByUpdatedAtDesc);
  const filteredRuns = sortedRows.filter((run) => getFilterMatch(run, filter));
  const attentionCount = rows.filter(
    (run) => getRunAttentionReason(run) !== "none",
  ).length;

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
        "flex min-h-0 flex-col overflow-hidden rounded-lg border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)]",
        className,
      )}
    >
      <div className="flex flex-wrap items-center justify-between gap-3 border-b border-[var(--relay-row-border)] px-4 py-3">
        <div>
          <p className="text-sm font-medium text-foreground">
            {rows.length} run{rows.length === 1 ? "" : "s"} in registry
          </p>
          <p className="text-xs text-muted-foreground">
            {attentionCount} needing attention
          </p>
        </div>
        <div className="inline-flex items-center gap-1.5 text-xs text-muted-foreground">
          <TimerReset className="size-3.5" />
          Real-time operator view
        </div>
      </div>

      <div className="px-4 pt-2">
        <RelayFilterTabs
          value={filter}
          items={filterItems}
          onValueChange={(value) => setFilter(value as RunsRegistryFilter)}
        />
      </div>

      <div className="min-h-0 flex-1 overflow-auto px-4 pb-4">
        <Table className="min-w-[980px]">
          <TableHeader>
            <TableRow className="border-[var(--relay-row-border)] hover:bg-transparent">
              <TableHead className="w-[38%]">Run</TableHead>
              <TableHead>Status</TableHead>
              <TableHead>Stage</TableHead>
              <TableHead>Executor</TableHead>
              <TableHead>Updated</TableHead>
              <TableHead>Attention</TableHead>
              <TableHead className="w-[1%]" />
            </TableRow>
          </TableHeader>
          <TableBody>
            {isLoading
              ? Array.from({ length: 5 }).map((_, index) => (
                  <TableRow
                    key={`loading-row-${index}`}
                    className="border-[var(--relay-row-border)]"
                  >
                    <TableCell className="py-3">
                      <div className="space-y-2">
                        <Skeleton className="h-4 w-56" />
                        <Skeleton className="h-3 w-72" />
                        <Skeleton className="h-3 w-80" />
                      </div>
                    </TableCell>
                    <TableCell>
                      <Skeleton className="h-6 w-28" />
                    </TableCell>
                    <TableCell>
                      <Skeleton className="h-4 w-20" />
                    </TableCell>
                    <TableCell>
                      <Skeleton className="h-4 w-24" />
                    </TableCell>
                    <TableCell>
                      <Skeleton className="h-4 w-20" />
                    </TableCell>
                    <TableCell>
                      <Skeleton className="h-6 w-24" />
                    </TableCell>
                    <TableCell>
                      <Skeleton className="ml-auto h-8 w-28" />
                    </TableCell>
                  </TableRow>
                ))
              : null}

            {!isLoading && filteredRuns.length === 0 ? (
              <TableRow className="border-[var(--relay-row-border)]">
                <TableCell
                  colSpan={7}
                  className="py-12 text-center text-sm text-muted-foreground"
                >
                  No runs match this filter.
                </TableCell>
              </TableRow>
            ) : null}

            {!isLoading
              ? filteredRuns.map((run) => {
                  const attentionReason = getRunAttentionReason(run);
                  const attentionCountValue =
                    attentionReason === "validation-failed" &&
                    run.validationSummary.errors > 0
                      ? run.validationSummary.errors
                      : undefined;

                  return (
                    <TableRow
                      key={run.id}
                      className="border-[var(--relay-row-border)]"
                    >
                      <TableCell className="py-3 align-top">
                        <div className="space-y-2">
                          <div className="min-w-0">
                            <p className="truncate text-sm font-medium text-foreground">
                              {run.title}
                            </p>
                            <div className="mt-1 flex flex-wrap items-center gap-x-3 gap-y-1">
                              <RelayMonoText className="text-[11px] text-muted-foreground">
                                {run.id}
                              </RelayMonoText>
                              {run.packetId ? (
                                <div className="inline-flex min-w-0 items-center gap-1.5">
                                  <Package className="size-3 shrink-0 text-muted-foreground" />
                                  <RelayMonoText className="truncate text-[11px] text-muted-foreground">
                                    {run.packetId}
                                  </RelayMonoText>
                                </div>
                              ) : null}
                            </div>
                          </div>

                          <RelayMetaRow>
                            <RelayMetaItem
                              icon={<GitFork className="size-3" />}
                            >
                              {run.repo}
                            </RelayMetaItem>
                            <RelayMetaItem
                              icon={<GitBranch className="size-3" />}
                            >
                              {run.branch}
                            </RelayMetaItem>
                            {run.worktree ? (
                              <RelayMetaItem
                                icon={<Package className="size-3" />}
                                mono
                              >
                                {run.worktree}
                              </RelayMetaItem>
                            ) : null}
                          </RelayMetaRow>
                        </div>
                      </TableCell>

                      <TableCell className="align-top">
                        <StatusBadge status={run.status} />
                      </TableCell>

                      <TableCell className="align-top">
                        <RelayStageLabel step={run.activeStep} />
                      </TableCell>

                      <TableCell className="align-top">
                        <RelayMetaItem icon={<Bot className="size-3" />}>
                          <RelayMonoText>{run.executor}</RelayMonoText>
                        </RelayMetaItem>
                      </TableCell>

                      <TableCell className="align-top">
                        <span
                          className="text-xs text-muted-foreground"
                          title={formatRunDate(run.updatedAt)}
                        >
                          {formatRunDateRelative(run.updatedAt)}
                        </span>
                      </TableCell>

                      <TableCell className="align-top">
                        <RelayAttentionBadge
                          reason={attentionReason}
                          compact
                          count={attentionCountValue}
                        />
                      </TableCell>

                      <TableCell className="align-top">
                        <div className="flex justify-end">
                          <Button
                            variant="outline"
                            size="sm"
                            asChild
                            className="gap-1.5"
                          >
                            <Link to={getActiveStepRoute(run)}>
                              Open Workbench
                              <ArrowRight className="size-3.5" />
                            </Link>
                          </Button>
                        </div>
                      </TableCell>
                    </TableRow>
                  );
                })
              : null}
          </TableBody>
        </Table>
      </div>

      <div className="flex items-center justify-between gap-3 border-t border-[var(--relay-row-border)] px-4 py-3 text-xs text-muted-foreground">
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
