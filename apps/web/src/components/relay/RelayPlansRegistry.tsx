import * as React from "react";
import { Link } from "@tanstack/react-router";
import { AlertTriangle, ChevronRight, Plus } from "lucide-react";

import { RelayFilterTabs, type RelayFilterTabItem } from "@/components/relay/RelayFilterTabs";
import { RelayMonoText } from "@/components/relay/RelayMeta";
import { RelayStateSurface } from "@/components/relay/RelayStateSurface";
import {
  formatPlanDate,
  formatPlanDateRelative,
  getPlanAttention,
  getPlanAttentionLabel,
  getPlanAttentionVariant,
  getPlanStatusLabel,
  getPlanStatusVariant,
  type RelayPlanRegistryFilter,
} from "@/components/relay/relayPlanVisualState";
import { Badge } from "@/components/ui/badge";
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

const registryColumns =
  "minmax(19rem,2.1fr) minmax(8rem,0.9fr) minmax(10rem,1fr) minmax(12rem,1.2fr) minmax(8rem,0.8fr) minmax(10rem,0.9fr) 2.5rem";

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

function getRegistryProgress(plan: PlanAPIReadPlan) {
  const total = plan.passCount;
  const completed =
    typeof plan.completedPassCount === "number"
      ? plan.completedPassCount
      : plan.completionReady
        ? total
        : undefined;

  return {
    value: total > 0 && typeof completed === "number" ? (completed / total) * 100 : 0,
    label:
      total === 0
        ? "0 / 0"
        : typeof completed === "number"
          ? `${completed} / ${total}`
          : `- / ${total}`,
  };
}

function getRegistryPassDetailLabel(plan: PlanAPIReadPlan): string {
  const inProgress = plan.inProgressPassCount ?? 0;
  const planned = plan.plannedPassCount ?? 0;

  if (inProgress > 0) {
    return `${inProgress} pass${inProgress === 1 ? "" : "es"} in progress`;
  }

  if (planned > 0) {
    return `${planned} planned pass${planned === 1 ? "" : "es"}`;
  }

  return "Open plan for pass detail";
}

function PlanStatusBadge({ plan }: { plan: PlanAPIReadPlan }) {
  const primaryLabel = plan.completionReady ? "Completion Ready" : getPlanStatusLabel(plan.status);
  const primaryVariant = plan.completionReady ? "warning" : getPlanStatusVariant(plan.status);

  return (
    <Badge variant={primaryVariant} className="text-[11px] font-medium">
      {primaryLabel}
    </Badge>
  );
}

function PlanAttentionBadge({ plan }: { plan: PlanAPIReadPlan }) {
  const attention = getPlanAttention(plan);

  if (attention === "none") {
    return <span className="text-xs text-muted-foreground">None</span>;
  }

  return (
    <Badge
      variant={getPlanAttentionVariant(attention)}
      className="text-[11px] font-medium"
    >
      {getPlanAttentionLabel(attention)}
    </Badge>
  );
}

function RelayPlanCompactRow({ plan }: { plan: PlanAPIReadPlan }) {
  const progress = getRegistryProgress(plan);

  return (
    <Link
      to="/plans/$planId"
      params={{ planId: plan.planId }}
      aria-label={`Open plan ${plan.title}`}
      className="rounded border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)] p-3 transition-colors hover:bg-[var(--relay-panel-hover-bg)] focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-[var(--relay-accent)]"
    >
      <div className="flex min-w-0 items-start gap-3">
        <div className="min-w-0 flex-1">
          <p className="truncate text-sm font-medium text-foreground">{plan.title}</p>
          <div className="mt-1 flex flex-wrap items-center gap-x-2 gap-y-1">
            <RelayMonoText className="text-[11px] text-muted-foreground">
              {plan.planId}
            </RelayMonoText>
            <RelayMonoText className="min-w-0 break-words text-[11px] text-muted-foreground">
              {plan.repoTarget}
            </RelayMonoText>
            <RelayMonoText className="text-[11px] text-muted-foreground">
              {plan.branchContext}
            </RelayMonoText>
          </div>
        </div>
        <ChevronRight className="mt-0.5 size-4 shrink-0 text-muted-foreground" />
      </div>

      <div className="mt-3 flex flex-wrap items-center gap-2">
        <PlanStatusBadge plan={plan} />
        <PlanAttentionBadge plan={plan} />
      </div>

      <div className="mt-3 space-y-2">
        <div className="flex items-center justify-between gap-3">
          <span className="text-[10px] font-medium uppercase tracking-[0.06em] text-muted-foreground">
            Progress
          </span>
          <span className="text-xs text-muted-foreground">{progress.label}</span>
        </div>
        <div className="h-2 overflow-hidden rounded-full bg-[var(--relay-row-border)]">
          <div
            className="h-full rounded-full bg-[var(--relay-accent)] transition-[width]"
            style={{ width: `${progress.value}%` }}
          />
        </div>
      </div>

      <div className="mt-3 grid grid-cols-1 gap-2 sm:grid-cols-3">
        <div className="min-w-0">
          <p className="text-[10px] font-medium uppercase tracking-[0.06em] text-muted-foreground">
            Pass Detail
          </p>
          <span className="mt-1 block text-[11px] text-muted-foreground">
            {getRegistryPassDetailLabel(plan)}
          </span>
        </div>
        <div className="min-w-0">
          <p className="text-[10px] font-medium uppercase tracking-[0.06em] text-muted-foreground">
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
          <p className="text-[10px] font-medium uppercase tracking-[0.06em] text-muted-foreground">
            Attention
          </p>
          <span className="mt-1 block text-[11px] text-muted-foreground">
            {getPlanAttentionLabel(getPlanAttention(plan))}
          </span>
        </div>
      </div>
    </Link>
  );
}

function RelayPlanTableRow({ plan }: { plan: PlanAPIReadPlan }) {
  const progress = getRegistryProgress(plan);

  return (
    <Link
      to="/plans/$planId"
      params={{ planId: plan.planId }}
      aria-label={`Open plan ${plan.title}`}
      className="grid items-center border-b border-[var(--relay-row-border)] text-sm transition-colors hover:bg-[var(--relay-panel-hover-bg)] focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-[var(--relay-accent)]"
      style={{ gridTemplateColumns: registryColumns }}
    >
      <div className="min-w-0 px-4 py-3">
        <div className="space-y-1">
          <p className="truncate font-medium text-foreground">{plan.title}</p>
          <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
            <RelayMonoText className="text-[11px] text-muted-foreground">
              {plan.planId}
            </RelayMonoText>
            <span className="text-[11px] text-muted-foreground">/</span>
            <RelayMonoText className="truncate text-[11px] text-muted-foreground">
              {plan.repoTarget}
            </RelayMonoText>
            <RelayMonoText className="text-[11px] text-muted-foreground">
              {plan.branchContext}
            </RelayMonoText>
          </div>
        </div>
      </div>

      <div className="px-4 py-3">
        <PlanStatusBadge plan={plan} />
      </div>

      <div className="px-4 py-3">
        <div className="space-y-1">
          <span className="block text-xs text-foreground">{progress.label}</span>
          <div className="h-2 overflow-hidden rounded-full bg-[var(--relay-row-border)]">
            <div
              className="h-full rounded-full bg-[var(--relay-accent)] transition-[width]"
              style={{ width: `${progress.value}%` }}
            />
          </div>
        </div>
      </div>

      <div className="px-4 py-3">
        <span className="text-xs text-muted-foreground">
          {getRegistryPassDetailLabel(plan)}
        </span>
      </div>

      <div className="px-4 py-3">
        <span
          className="text-xs text-muted-foreground"
          title={formatPlanDate(plan.updatedAt)}
        >
          {formatPlanDateRelative(plan.updatedAt)}
        </span>
      </div>

      <div className="px-4 py-3">
        <PlanAttentionBadge plan={plan} />
      </div>

      <div className="flex justify-end px-4 py-3 text-muted-foreground">
        <ChevronRight className="size-4" />
      </div>
    </Link>
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
      <div className="flex flex-wrap items-center justify-between gap-3 border-b border-[var(--relay-row-border)] px-4 py-3">
        <div className="flex min-w-0 items-center gap-2">
          <span className="text-sm font-semibold text-foreground">
            {rows.length} plan{rows.length === 1 ? "" : "s"}
          </span>
          {attentionCount > 0 ? (
            <span className="inline-flex items-center gap-1 text-[11px] font-medium text-[var(--warning)]">
              <AlertTriangle className="size-3" />
              {attentionCount} need attention
            </span>
          ) : null}
        </div>
      </div>

      <div className="pt-2">
        <RelayFilterTabs
          value={filter}
          items={filterItems}
          onValueChange={(value) => setFilter(value as RelayPlanRegistryFilter)}
        />
      </div>

      <div className="min-h-0 flex-1">
        {isLoading ? (
          <div className="min-h-0 flex h-full flex-col">
            <div className="flex min-h-0 flex-1 flex-col gap-3 overflow-y-auto p-3 lg:hidden">
              {Array.from({ length: 4 }).map((_, index) => (
                <div
                  key={`plan-compact-loading-${index}`}
                  className="rounded border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)] p-3"
                >
                  <div className="space-y-2">
                    <Skeleton className="h-4 w-40" />
                    <Skeleton className="h-3 w-52" />
                    <Skeleton className="h-2 w-full" />
                    <div className="flex flex-wrap gap-2 pt-1">
                      <Skeleton className="h-6 w-24" />
                      <Skeleton className="h-6 w-28" />
                    </div>
                  </div>
                </div>
              ))}
            </div>

            <div className="hidden min-h-0 flex-1 overflow-x-auto overflow-y-hidden lg:flex">
              <div className="flex h-full min-h-0 min-w-[1120px] flex-1 flex-col">
                <div
                  className="grid shrink-0 border-b border-[var(--relay-row-border)] py-2 text-[10px] font-semibold uppercase tracking-[0.08em] text-muted-foreground"
                  style={{ gridTemplateColumns: registryColumns }}
                >
                  <div className="px-4">PLAN</div>
                  <div className="px-4">STATUS</div>
                  <div className="px-4">PROGRESS</div>
                  <div className="px-4">CURRENT / NEXT PASS</div>
                  <div className="px-4">UPDATED</div>
                  <div className="px-4">ATTENTION</div>
                  <div className="pr-2" />
                </div>

                <div className="min-h-0 flex-1 overflow-y-auto">
                  {Array.from({ length: 4 }).map((_, index) => (
                    <div
                      key={`plan-table-loading-${index}`}
                      className="grid border-b border-[var(--relay-row-border)]"
                      style={{ gridTemplateColumns: registryColumns }}
                    >
                      <div className="px-4 py-3">
                        <Skeleton className="h-4 w-56" />
                        <Skeleton className="mt-2 h-3 w-40" />
                      </div>
                      <div className="px-4 py-3">
                        <Skeleton className="h-6 w-24" />
                      </div>
                      <div className="px-4 py-3">
                        <Skeleton className="h-4 w-16" />
                        <Skeleton className="mt-2 h-2 w-full" />
                      </div>
                      <div className="px-4 py-3">
                        <Skeleton className="h-4 w-28" />
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
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  disabled
                  title="Plan submission arrives in UI-PLAN-03"
                >
                  <Plus className="size-3.5" />
                  New Plan arrives in UI-PLAN-03
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
          <div className="min-h-0 flex h-full flex-col">
            <div className="min-h-0 flex-1 overflow-y-auto p-3 lg:hidden">
              <div className="flex flex-col gap-3">
                {filteredPlans.map((plan) => (
                  <RelayPlanCompactRow key={plan.id} plan={plan} />
                ))}
              </div>
            </div>

            <div className="hidden min-h-0 flex-1 overflow-x-auto overflow-y-hidden lg:flex">
              <div className="flex h-full min-h-0 min-w-[1120px] flex-1 flex-col">
                <div
                  className="grid shrink-0 border-b border-[var(--relay-row-border)] py-2 text-[10px] font-semibold uppercase tracking-[0.08em] text-muted-foreground"
                  style={{ gridTemplateColumns: registryColumns }}
                >
                  <div className="px-4">PLAN</div>
                  <div className="px-4">STATUS</div>
                  <div className="px-4">PROGRESS</div>
                  <div className="px-4">CURRENT / NEXT PASS</div>
                  <div className="px-4">UPDATED</div>
                  <div className="px-4">ATTENTION</div>
                  <div className="pr-2" />
                </div>

                <div className="min-h-0 flex-1 overflow-y-auto">
                  {filteredPlans.map((plan) => (
                    <RelayPlanTableRow key={plan.id} plan={plan} />
                  ))}
                </div>
              </div>
            </div>
          </div>
        ) : null}
      </div>

      <div className="flex shrink-0 items-center justify-between border-t border-[var(--relay-row-border)] px-4 py-2 text-[11px] font-medium text-muted-foreground">
        <span>
          {filteredPlans.length} plan{filteredPlans.length === 1 ? "" : "s"}
        </span>
        <span>
          {filter === "all"
            ? "Showing all managed plans"
            : `Filtered from ${rows.length} total`}
        </span>
      </div>
    </div>
  );
}
