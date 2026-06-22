import * as React from "react";
import { Link, useRouter } from "@tanstack/react-router";
import { AlertTriangle, ChevronRight, Plus } from "lucide-react";

import {
  RelayFilterTabs,
  type RelayFilterTabItem,
} from "@/components/relay/RelayFilterTabs";
import { RelayStateSurface } from "@/components/relay/RelayStateSurface";
import {
  formatPlanDate,
  formatPlanDateRelative,
  getPlanAttention,
  getPlanAttentionLabel,
  getPlanProgressSummary,
  getPlanRegistryPassSummary,
  getPlanStatusLabel,
  type RelayPlanRegistryFilter,
} from "@/components/relay/relayPlanVisualState";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import type { PlanAPIReadPlan } from "@/features/relay-plans";
import { cn } from "@/lib/utils";

interface RelayPlansRegistryProps {
  plans?: PlanAPIReadPlan[];
  isLoading?: boolean;
  error?: unknown;
  className?: string;
}

const planPillBase =
  "inline-flex items-center gap-1 whitespace-nowrap rounded-sm border px-2 py-0.5 text-[10px] font-medium tracking-wide";

function comparePlansByUpdatedAtDesc(a: PlanAPIReadPlan, b: PlanAPIReadPlan): number {
  return Date.parse(b.updatedAt) - Date.parse(a.updatedAt);
}

function getFilterMatch(
  plan: PlanAPIReadPlan,
  filter: RelayPlanRegistryFilter,
): boolean {
  const attention = getPlanAttention(plan);

  switch (filter) {
    case "all":
      return true;
    case "active":
      return plan.status === "active";
    case "completion_ready":
      return plan.completionReady;
    case "needs_attention":
      return attention !== "none";
    case "complete":
      return plan.status === "complete";
    case "abandoned":
      return plan.status === "abandoned";
    default:
      return true;
  }
}

function getPlanStatusPillClassName(plan: PlanAPIReadPlan): string {
  if (plan.completionReady) {
    return "border-warning/35 bg-warning/14 text-warning";
  }

  switch (plan.status) {
    case "active":
      return "border-running/30 bg-running/12 text-running";
    case "complete":
      return "border-success/30 bg-success/12 text-success";
    case "abandoned":
      return "border-border bg-muted/40 text-muted-foreground";
  }
}

function getPlanAttentionPillClassName(attention: ReturnType<typeof getPlanAttention>): string {
  switch (attention) {
    case "completion-ready":
      return "border-warning/35 bg-warning/14 text-warning";
    case "in-progress":
      return "border-running/30 bg-running/12 text-running";
    case "next-pass-ready":
      return "border-info/30 bg-info/12 text-info";
    case "no-runnable-pass":
      return "border-destructive/30 bg-destructive/10 text-destructive";
    case "abandoned":
      return "border-border bg-muted/40 text-muted-foreground";
    case "none":
      return "";
  }
}

function PlanStatusPill({ plan }: { plan: PlanAPIReadPlan }) {
  const label = plan.completionReady ? "Completion Ready" : getPlanStatusLabel(plan.status);

  return (
    <span className={cn(planPillBase, getPlanStatusPillClassName(plan))}>
      {label}
    </span>
  );
}

function PlanAttentionPill({ plan }: { plan: PlanAPIReadPlan }) {
  const attention = getPlanAttention(plan);

  if (attention === "none") {
    return null;
  }

  return (
    <span className={cn(planPillBase, getPlanAttentionPillClassName(attention))}>
      <AlertTriangle className="h-[9px] w-[9px]" />
      {getPlanAttentionLabel(attention)}
    </span>
  );
}

function PlanMetadataLine({ plan }: { plan: PlanAPIReadPlan }) {
  const segments = [plan.planId, plan.repoTarget, plan.branchContext];

  return (
    <div className="mt-0.5 flex flex-wrap items-center gap-x-1 gap-y-0.5 font-mono text-[10px] leading-4 text-muted-foreground">
      {segments.map((value, index) => (
        <span key={`${plan.id}-${index}`} className="inline-flex max-w-full items-center gap-1">
          {index > 0 ? <span className="text-muted-foreground/55">/</span> : null}
          <span className="break-all">{value}</span>
        </span>
      ))}
    </div>
  );
}

function PlanProgressDots({ plan }: { plan: PlanAPIReadPlan }) {
  const progress = getPlanProgressSummary(plan);
  const visibleSegments = Math.min(progress.dotCount, 10);
  const filledSegments =
    progress.dotCount <= 10
      ? progress.filledDots
      : Math.min(
          visibleSegments,
          Math.max(
            0,
            Math.round(
              (progress.filledDots / Math.max(progress.dotCount, 1)) * visibleSegments,
            ),
          ),
        );

  return (
    <div className="flex items-center gap-2">
      <div className="flex gap-px" aria-hidden="true">
        {Array.from({ length: visibleSegments }).map((_, index) => (
          <span
            key={index}
            className={cn(
              "h-1.5 rounded-sm",
              index < filledSegments
                ? "bg-info/70"
                : "bg-[var(--relay-row-border)]",
            )}
            style={{ width: "9px" }}
          />
        ))}
      </div>
      <span className="font-mono text-[10px] tabular-nums whitespace-nowrap text-muted-foreground">
        {progress.label}
      </span>
    </div>
  );
}

function PlanProgressDotsSkeleton() {
  return (
    <div className="flex items-center gap-2">
      <div className="flex gap-px" aria-hidden="true">
        {Array.from({ length: 4 }).map((_, index) => (
          <Skeleton key={index} className="h-1.5 w-[9px] rounded-sm" />
        ))}
      </div>
      <Skeleton className="h-3 w-10" />
    </div>
  );
}

function PlanPassSummary({ plan }: { plan: PlanAPIReadPlan }) {
  const summary = getPlanRegistryPassSummary(plan);

  if (summary.kind === "fallback") {
    if (summary.title === "ALL COMPLETE") {
      return (
        <span className="font-mono text-[10px] tracking-[0.18em] text-success/70">
          {summary.title}
        </span>
      );
    }

    if (summary.title !== "—") {
      return (
        <div className="min-w-0">
          <div className="font-mono text-[10px] leading-4 tracking-[0.16em] text-warning">
            {summary.title}
          </div>
          {summary.subtitle ? (
            <div className="truncate text-[11px] leading-snug text-muted-foreground">
              {summary.subtitle}
            </div>
          ) : null}
        </div>
      );
    }

    return <span className="text-sm text-muted-foreground">—</span>;
  }

  const tooltip = summary.subtitle ? `${summary.title}\n${summary.subtitle}` : summary.title;
  const passId = summary.passId ?? (summary.kind === "current" ? "CURRENT" : "NEXT");

  return (
    <div className="flex max-w-[200px] items-start gap-1.5">
      <span
        className={cn(
          "mt-[5px] h-1.5 w-1.5 shrink-0 rounded-full",
          summary.kind === "current" ? "bg-info" : "bg-[var(--relay-row-border)]",
        )}
        aria-hidden="true"
      />
      <div className="min-w-0">
        <div className="truncate font-mono text-[10px] leading-4 text-muted-foreground">
          {passId}
        </div>
        <div className="truncate text-[11px] leading-snug text-foreground/85" title={tooltip}>
          {summary.title}
        </div>
      </div>
    </div>
  );
}

function RelayPlanCompactRow({ plan }: { plan: PlanAPIReadPlan }) {
  const attention = getPlanAttention(plan);
  const attentionLabel = getPlanAttentionLabel(attention);

  return (
    <Link
      to="/plans/$planId"
      params={{ planId: plan.planId }}
      aria-label={`Open plan ${plan.title}`}
      className="rounded border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)] p-3 transition-colors hover:bg-[var(--relay-panel-hover-bg)] focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-[var(--relay-accent)]"
    >
      <div className="flex min-w-0 items-start gap-3">
        <div className="min-w-0 flex-1">
          <p className="truncate text-sm font-medium leading-snug text-foreground">
            {plan.title}
          </p>
          <PlanMetadataLine plan={plan} />
        </div>
        <ChevronRight className="mt-0.5 size-4 shrink-0 text-muted-foreground" />
      </div>

      <div className="mt-3 flex flex-wrap items-center gap-2">
        <PlanStatusPill plan={plan} />
        <PlanAttentionPill plan={plan} />
      </div>

      <div className="mt-3 space-y-2">
        <span className="text-[10px] font-medium uppercase tracking-[0.08em] text-muted-foreground">
          Progress
        </span>
        <PlanProgressDots plan={plan} />
      </div>

      <div className="mt-3 grid grid-cols-1 gap-2 sm:grid-cols-3">
        <div className="min-w-0">
          <p className="text-[10px] font-medium uppercase tracking-[0.08em] text-muted-foreground">
            Current / Next
          </p>
          <div className="mt-1 min-w-0">
            <PlanPassSummary plan={plan} />
          </div>
        </div>
        <div className="min-w-0">
          <p className="text-[10px] font-medium uppercase tracking-[0.08em] text-muted-foreground">
            Updated
          </p>
          <span
            className="mt-1 block text-[11px] text-muted-foreground"
            title={formatPlanDate(plan.updatedAt)}
          >
            {formatPlanDateRelative(plan.updatedAt)}
          </span>
        </div>
        <div className="min-w-0">
          <p className="text-[10px] font-medium uppercase tracking-[0.08em] text-muted-foreground">
            Attention
          </p>
          <span className="mt-1 block text-[11px] text-muted-foreground">
            {attention === "none" ? "None" : attentionLabel}
          </span>
        </div>
      </div>
    </Link>
  );
}

function RelayPlanTableRow({ plan }: { plan: PlanAPIReadPlan }) {
  const router = useRouter();

  const openPlan = React.useCallback(() => {
    void router.navigate({ to: "/plans/$planId", params: { planId: plan.planId } });
  }, [plan.planId, router]);

  return (
    <tr
      role="link"
      tabIndex={0}
      aria-label={`Open plan ${plan.title}`}
      className="group cursor-pointer border-b border-[var(--relay-row-border)] transition-colors hover:bg-[var(--relay-panel-hover-bg)] focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-[var(--relay-accent)]"
      onClick={openPlan}
      onKeyDown={(event) => {
        if (event.key === "Enter" || event.key === " ") {
          event.preventDefault();
          openPlan();
        }
      }}
    >
      <td className="px-6 py-3.5 pr-3 align-middle">
        <div className="min-w-0">
          <p className="truncate text-sm font-medium leading-snug text-foreground">
            {plan.title}
          </p>
          <PlanMetadataLine plan={plan} />
        </div>
      </td>

      <td className="px-4 py-3.5 align-middle">
        <PlanStatusPill plan={plan} />
      </td>

      <td className="px-4 py-3.5 align-middle">
        <PlanProgressDots plan={plan} />
      </td>

      <td className="px-4 py-3.5 align-middle">
        <PlanPassSummary plan={plan} />
      </td>

      <td className="px-4 py-3.5 align-middle">
        <span className="whitespace-nowrap text-[11px] text-muted-foreground" title={formatPlanDate(plan.updatedAt)}>
          {formatPlanDateRelative(plan.updatedAt)}
        </span>
      </td>

      <td className="px-4 py-3.5 align-middle">
        <PlanAttentionPill plan={plan} />
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
          Plan
        </th>
        <th className="px-4 py-2 text-left text-[10px] font-medium uppercase tracking-[0.08em] text-muted-foreground">
          Status
        </th>
        <th className="px-4 py-2 text-left text-[10px] font-medium uppercase tracking-[0.08em] text-muted-foreground">
          Progress
        </th>
        <th className="px-4 py-2 text-left text-[10px] font-medium uppercase tracking-[0.08em] text-muted-foreground">
          Current / Next Pass
        </th>
        <th className="px-4 py-2 text-left text-[10px] font-medium uppercase tracking-[0.08em] text-muted-foreground">
          Updated
        </th>
        <th className="px-4 py-2 text-left text-[10px] font-medium uppercase tracking-[0.08em] text-muted-foreground">
          Attention
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
        <tr key={`plan-table-loading-${index}`} className="border-b border-[var(--relay-row-border)]">
          <td className="px-6 py-3.5">
            <Skeleton className="h-4 w-56" />
            <Skeleton className="mt-1.5 h-3 w-48" />
          </td>
          <td className="px-4 py-3.5">
            <Skeleton className="h-5 w-24 rounded-sm" />
          </td>
          <td className="px-4 py-3.5">
            <PlanProgressDotsSkeleton />
          </td>
          <td className="px-4 py-3.5">
            <Skeleton className="h-4 w-28" />
          </td>
          <td className="px-4 py-3.5">
            <Skeleton className="h-4 w-16" />
          </td>
          <td className="px-4 py-3.5">
            <Skeleton className="h-5 w-24 rounded-sm" />
          </td>
          <td className="px-3 py-3.5">
            <Skeleton className="ml-auto h-4 w-4" />
          </td>
        </tr>
      ))}
    </tbody>
  );
}

export function RelayPlansRegistry({
  plans,
  isLoading = false,
  error,
  className,
}: RelayPlansRegistryProps) {
  const [filter, setFilter] = React.useState<RelayPlanRegistryFilter>("all");
  const rows = plans ?? [];
  const sortedRows = [...rows].sort(comparePlansByUpdatedAtDesc);
  const filteredPlans = sortedRows.filter((plan) => getFilterMatch(plan, filter));
  const attentionCount = rows.filter((plan) => getPlanAttention(plan) !== "none").length;

  const filterItems: RelayFilterTabItem[] = [
    { value: "all", label: "All", count: rows.length },
    {
      value: "active",
      label: "Active",
      count: rows.filter((plan) => plan.status === "active").length,
    },
    {
      value: "completion_ready",
      label: "Completion Ready",
      count: rows.filter((plan) => plan.completionReady).length,
    },
    {
      value: "needs_attention",
      label: "Needs Attention",
      count: attentionCount,
    },
    {
      value: "complete",
      label: "Complete",
      count: rows.filter((plan) => plan.status === "complete").length,
    },
    {
      value: "abandoned",
      label: "Abandoned",
      count: rows.filter((plan) => plan.status === "abandoned").length,
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
            <span className="font-mono text-foreground">{rows.length}</span> plans
          </span>
          {attentionCount > 0 ? (
            <span className="inline-flex items-center gap-1 text-warning">
              <AlertTriangle className="size-3" />
              <span className="font-medium">{attentionCount}</span> need review
            </span>
          ) : null}
        </div>
      </div>

      <RelayFilterTabs
        value={filter}
        items={filterItems}
        onValueChange={(value) => setFilter(value as RelayPlanRegistryFilter)}
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
                  key={`plan-compact-loading-${index}`}
                  className="rounded border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)] p-3"
                >
                  <div className="space-y-2">
                    <Skeleton className="h-4 w-40" />
                    <Skeleton className="h-3 w-52" />
                    <PlanProgressDotsSkeleton />
                    <div className="flex flex-wrap gap-2 pt-1">
                      <Skeleton className="h-5 w-24 rounded-sm" />
                      <Skeleton className="h-5 w-28 rounded-sm" />
                    </div>
                  </div>
                </div>
              ))}
            </div>

            <div className="hidden min-h-0 flex-1 overflow-x-auto overflow-y-hidden lg:flex">
              <div className="flex min-h-0 h-full min-w-[980px] flex-1 flex-col">
                <table className="w-full table-fixed border-collapse">
                  <colgroup>
                    <col style={{ width: "34%" }} />
                    <col style={{ width: "11%" }} />
                    <col style={{ width: "12%" }} />
                    <col style={{ width: "23%" }} />
                    <col style={{ width: "8%" }} />
                    <col style={{ width: "8%" }} />
                    <col style={{ width: "4%" }} />
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
              title="Plans failed to load"
              description="Relay could not load the managed plans registry. Check the API process and try again."
            />
          </div>
        ) : null}

        {!isLoading && !error && rows.length === 0 ? (
          <div className="min-h-0 flex-1 overflow-y-auto p-4">
            <RelayStateSurface
              tone="empty"
              title="No managed plans yet"
              description="Plans will appear here once Relay receives validated multi-pass plan records."
              action={
                <Button asChild variant="outline" size="sm">
                  <Link to="/plans/new">
                    <Plus className="size-3.5" />
                    New Plan
                  </Link>
                </Button>
              }
            />
          </div>
        ) : null}

        {!isLoading && !error && rows.length > 0 && filteredPlans.length === 0 ? (
          <div className="min-h-0 flex-1 overflow-y-auto p-4">
            <RelayStateSurface
              tone="empty"
              title="No plans match this filter"
              description="Switch filters to view the rest of the plans registry."
              action={
                <Button variant="outline" size="sm" onClick={() => setFilter("all")}>
                  Show all plans
                </Button>
              }
            />
          </div>
        ) : null}

        {!isLoading && !error && filteredPlans.length > 0 ? (
          <div className="flex h-full min-h-0 flex-col">
            <div className="min-h-0 flex-1 overflow-y-auto p-3 lg:hidden">
              <div className="flex flex-col gap-3">
                {filteredPlans.map((plan) => (
                  <RelayPlanCompactRow key={plan.id} plan={plan} />
                ))}
              </div>
            </div>

            <div className="hidden min-h-0 flex-1 overflow-x-auto overflow-y-hidden lg:flex">
              <div className="flex min-h-0 h-full min-w-[980px] flex-1 flex-col">
                <table className="w-full table-fixed border-collapse">
                  <colgroup>
                    <col style={{ width: "34%" }} />
                    <col style={{ width: "11%" }} />
                    <col style={{ width: "12%" }} />
                    <col style={{ width: "23%" }} />
                    <col style={{ width: "8%" }} />
                    <col style={{ width: "8%" }} />
                    <col style={{ width: "4%" }} />
                  </colgroup>
                  <TableHeader />
                  <tbody>
                    {filteredPlans.map((plan) => (
                      <RelayPlanTableRow key={plan.id} plan={plan} />
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
          {rows.length} plan{rows.length === 1 ? "" : "s"}
        </span>
        <span>
          Showing {filteredPlans.length}
          {filter === "all" ? "" : ` of ${rows.length}`}
        </span>
      </div>
    </div>
  );
}
