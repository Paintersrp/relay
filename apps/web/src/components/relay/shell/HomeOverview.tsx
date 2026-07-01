// ============================================================
// Relay Shell — Home_Overview landing surface
// ============================================================
//
// The attention-oriented triage landing rendered at the application root
// (Requirement 3). It is a presentation surface only: it composes two
// independently loaded sections from the existing Shell_Data_Composition_Layer
// (`useShellData`) and renders navigable links into existing detail/workbench
// routes. It sources data exclusively through the API_Contract-backed queries
// already used by the runs/plans/projects features.
//
//   - Attention section     → `selectAttentionRuns` (capped at 50 + overflow
//                             indicator), backed by the runs list query (Req 3.2).
//   - Recent-activity section → `selectRecentActivity` (10 most recent Runs /
//                             Plans / Projects), backed by the runs + plans +
//                             projects list queries (Req 3.3).
//
// Boundary (Requirements 3.9, 3.10):
//   This surface is limited to exactly the two sections above plus navigation
//   into existing routes. It is NOT a customizable dashboard, an analytics
//   surface, a reporting system, a metrics backend, a daemon health monitor, or
//   a new status aggregation service, and it introduces NO background polling or
//   subscription behavior beyond the per-section list queries `useShellData`
//   already composes.

import * as React from "react";
import { Link } from "@tanstack/react-router";
import { ChevronRight } from "lucide-react";

import { AppPageFrame } from "@/components/relay/AppPageFrame";
import { RelayStateSurface } from "@/components/relay/RelayStateSurface";
import { StatusBadge } from "@/components/relay/StatusBadge";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import {
  formatRunDate,
  formatRunDateRelative,
} from "@/features/relay-runs";
import type { AttentionRunItem } from "@/features/relay-navigation/attention";
import type { RecentActivityItem } from "@/features/relay-navigation/types";
import { useShellData } from "@/features/relay-navigation/useShellData";
import type { ShellDataQueryState } from "@/features/relay-navigation/useShellData";
import { cn } from "@/lib/utils";

// ------------------------------------------------------------
// Per-section state derivation
// ------------------------------------------------------------

export type SectionStatus = "loading" | "ready" | "empty" | "error";

/**
 * Derive a single section's state from its backing query state(s) and the count
 * of selected items. Error takes precedence over loading so a failed source
 * shows the retryable error state rather than a stuck spinner; a section is
 * `empty` only once it has successfully loaded with no items (Req 3.5–3.7).
 */
export function deriveSectionStatus(
  queries: readonly ShellDataQueryState[],
  itemCount: number,
): SectionStatus {
  if (queries.some((query) => query.isError)) return "error";
  if (queries.some((query) => query.isLoading)) return "loading";
  if (itemCount === 0) return "empty";
  return "ready";
}

// ------------------------------------------------------------
// Navigable rows (route by item type)
// ------------------------------------------------------------

const RECENT_TYPE_LABEL: Record<RecentActivityItem["type"], string> = {
  run: "Run",
  plan: "Plan",
  project: "Project",
};

const rowClassName =
  "flex items-center gap-3 rounded border border-[var(--relay-row-border)] " +
  "bg-[var(--relay-panel-bg)] px-3 py-2.5 transition-colors " +
  "hover:bg-[var(--relay-row-hover-bg)] focus-visible:outline-none " +
  "focus-visible:ring-1 focus-visible:ring-[var(--relay-accent)] group";

/**
 * Render an attention Run as a navigable row linking to its workbench route
 * (Req 3.4). The canonical `status` is rendered through the shared
 * {@link StatusBadge} so status color stays consistent across the shell.
 */
function AttentionRow({ item }: { item: AttentionRunItem }) {
  return (
    <Link
      to="/runs/$runId"
      params={{ runId: item.id }}
      aria-label={`Open workbench for ${item.label}`}
      className={rowClassName}
    >
      <div className="min-w-0 flex-1">
        <p className="truncate text-sm font-medium leading-snug text-foreground">
          {item.label}
        </p>
        <p
          className="mt-0.5 truncate text-[11px] text-muted-foreground/60"
          title={formatRunDate(item.updatedAt)}
        >
          Updated {formatRunDateRelative(item.updatedAt)}
        </p>
      </div>
      <StatusBadge status={item.status} />
      <ChevronRight className="size-4 shrink-0 text-muted-foreground/30 transition-colors group-hover:text-muted-foreground" />
    </Link>
  );
}

/**
 * Render a recent-activity item as a navigable row linking to the item's detail
 * or workbench route corresponding to its type (Req 3.4). Each entity type maps
 * to its own typed route so navigation stays type-safe.
 */
function RecentActivityRow({ item }: { item: RecentActivityItem }) {
  const meta = (
    <>
      <div className="min-w-0 flex-1">
        <p className="truncate text-sm font-medium leading-snug text-foreground">
          {item.label}
        </p>
        <p
          className="mt-0.5 truncate text-[11px] text-muted-foreground/60"
          title={formatRunDate(item.updatedAt)}
        >
          Updated {formatRunDateRelative(item.updatedAt)}
        </p>
      </div>
      <span className="shrink-0 rounded-sm border border-[var(--relay-row-border)] px-2 py-0.5 text-[10px] font-medium uppercase tracking-wide text-muted-foreground">
        {RECENT_TYPE_LABEL[item.type]}
      </span>
      <ChevronRight className="size-4 shrink-0 text-muted-foreground/30 transition-colors group-hover:text-muted-foreground" />
    </>
  );

  const label = `Open ${RECENT_TYPE_LABEL[item.type].toLowerCase()} ${item.label}`;

  // Route by type with a type-safe `to`/`params` pair per entity (Req 3.4).
  switch (item.type) {
    case "run":
      return (
        <Link to="/runs/$runId" params={{ runId: item.id }} aria-label={label} className={rowClassName}>
          {meta}
        </Link>
      );
    case "plan":
      return (
        <Link to="/plans/$planId" params={{ planId: item.id }} aria-label={label} className={rowClassName}>
          {meta}
        </Link>
      );
    case "project":
      return (
        <Link
          to="/projects/$projectId"
          params={{ projectId: item.id }}
          aria-label={label}
          className={rowClassName}
        >
          {meta}
        </Link>
      );
    default:
      return null;
  }
}

// ------------------------------------------------------------
// Section shell (loading / error / empty / ready)
// ------------------------------------------------------------

function SectionLoading() {
  return (
    <div className="flex flex-col gap-2" data-testid="home-section-loading">
      {Array.from({ length: 3 }).map((_, index) => (
        <div
          key={`home-loading-row-${index}`}
          className="rounded border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)] px-3 py-2.5"
        >
          <div className="flex items-center gap-3">
            <div className="min-w-0 flex-1 space-y-2">
              <Skeleton className="h-4 w-48" />
              <Skeleton className="h-3 w-28" />
            </div>
            <Skeleton className="h-6 w-20" />
          </div>
        </div>
      ))}
    </div>
  );
}

interface HomeSectionProps {
  title: string;
  description: string;
  /** Right-aligned header accessory (e.g. attention overflow indicator). */
  accessory?: React.ReactNode;
  status: SectionStatus;
  emptyTitle: string;
  emptyDescription: string;
  errorTitle: string;
  errorDescription: string;
  onRetry: () => void;
  children: React.ReactNode;
  className?: string;
}

/**
 * A single Home_Overview section. Renders exactly one of loading / error /
 * empty / ready. The error state is visually distinct from the empty state and
 * exposes a retry affordance that re-runs the section's backing query (Req 3.5,
 * 3.6, 3.7). Sections render independently, so one section's error never blocks
 * the other.
 */
function HomeSection({
  title,
  description,
  accessory,
  status,
  emptyTitle,
  emptyDescription,
  errorTitle,
  errorDescription,
  onRetry,
  children,
  className,
}: HomeSectionProps) {
  return (
    <section
      className={cn(
        "flex min-w-0 flex-col rounded-lg border border-[var(--relay-row-border)] bg-[var(--relay-content-bg)]",
        className,
      )}
      aria-label={title}
    >
      <header className="flex items-start justify-between gap-3 border-b border-[var(--relay-row-border)] px-4 py-3">
        <div className="min-w-0">
          <h2 className="text-sm font-semibold text-foreground">{title}</h2>
          <p className="mt-0.5 text-xs text-muted-foreground">{description}</p>
        </div>
        {accessory ? <div className="shrink-0">{accessory}</div> : null}
      </header>

      <div className="min-h-0 flex-1 p-4">
        {status === "loading" ? <SectionLoading /> : null}

        {status === "error" ? (
          <RelayStateSurface
            tone="danger"
            title={errorTitle}
            description={errorDescription}
            action={
              <Button variant="outline" size="sm" onClick={onRetry}>
                Retry
              </Button>
            }
          />
        ) : null}

        {status === "empty" ? (
          <RelayStateSurface tone="empty" title={emptyTitle} description={emptyDescription} />
        ) : null}

        {status === "ready" ? <div className="flex flex-col gap-2">{children}</div> : null}
      </div>
    </section>
  );
}

// ------------------------------------------------------------
// HomeOverview
// ------------------------------------------------------------

/**
 * The Home_Overview landing surface (Requirement 3). Composes an attention
 * section and a recent-activity section from {@link useShellData}, each loaded
 * by its own backing query so the sections load and error independently.
 */
export function HomeOverview() {
  const { attention, recentActivity, runsQuery, plansQuery, projectsQuery } = useShellData();

  // Attention is derived from Runs alone, so it tracks the runs list query.
  const attentionStatus = deriveSectionStatus([runsQuery], attention.items.length);

  // Recent activity mixes Runs, Plans, and Projects, so it tracks all three.
  const recentQueries = React.useMemo(
    () => [runsQuery, plansQuery, projectsQuery],
    [runsQuery, plansQuery, projectsQuery],
  );
  const recentStatus = deriveSectionStatus(recentQueries, recentActivity.length);

  const retryRecent = React.useCallback(() => {
    runsQuery.refetch();
    plansQuery.refetch();
    projectsQuery.refetch();
  }, [runsQuery, plansQuery, projectsQuery]);

  const hasOverflow = attention.totalCount > attention.items.length;

  return (
    <AppPageFrame
      title="Home"
      description="Triage what needs attention and jump back into recent work."
    >
      <div className="grid min-h-0 grid-cols-1 gap-6 lg:grid-cols-2">
        <HomeSection
          title="Needs attention"
          description="Runs that are blocked or awaiting review."
          accessory={
            hasOverflow ? (
              <span
                className="rounded-sm border border-[var(--relay-row-border)] bg-[var(--surface-inset)] px-2 py-0.5 font-mono text-[11px] text-muted-foreground"
                data-testid="attention-overflow"
              >
                Showing {attention.items.length} of {attention.totalCount}
              </span>
            ) : null
          }
          status={attentionStatus}
          emptyTitle="Nothing needs attention"
          emptyDescription="No runs are currently blocked or awaiting review."
          errorTitle="Attention items failed to load"
          errorDescription="Relay could not load runs needing attention. Check the API process and retry."
          onRetry={runsQuery.refetch}
        >
          {attention.items.map((item) => (
            <AttentionRow key={item.id} item={item} />
          ))}
        </HomeSection>

        <HomeSection
          title="Recent activity"
          description="The most recently updated runs, plans, and projects."
          status={recentStatus}
          emptyTitle="No recent activity"
          emptyDescription="Recently updated runs, plans, and projects will appear here."
          errorTitle="Recent activity failed to load"
          errorDescription="Relay could not load recent activity. Check the API process and retry."
          onRetry={retryRecent}
        >
          {recentActivity.map((item) => (
            <RecentActivityRow key={`${item.type}-${item.id}`} item={item} />
          ))}
        </HomeSection>
      </div>
    </AppPageFrame>
  );
}
