import * as React from "react";

import type { ProgressionEntry, Tone } from "@/features/relay-runs/runStatusTrackerViews";
import { formatRunDate, formatRunDateRelative } from "@/features/relay-runs";
import { cn } from "@/lib/utils";

import { ProgressiveDisclosure } from "./ProgressiveDisclosure";

// ============================================================
// Run Status Tracker Redesign — ProgressionRail (Requirement 4)
// ============================================================
//
// Chronological, most-recent-first log of past run events. Collapsed by
// default to a short recent tail (`collapsedCount`, default 3) rendered
// inline and visually subordinate to `CurrentStatusBlock` (smaller type,
// muted color, no competing card border). The remaining history is
// revealed via the existing `ProgressiveDisclosure` primitive, unchanged,
// behind a "Show full history (N)" affordance.
//
// Expansion state is keyed by `runId` (via `ProgressiveDisclosure`'s
// `resetKey`), not by Active_Route_Step, so navigating between step
// sub-routes for the *same* run preserves whether the Operator already
// expanded the rail (Requirement 4.10). Only navigating to a genuinely
// different run resets it back to collapsed.

const TONE_DOT_CLASS: Record<Tone, string> = {
  neutral: "bg-muted-foreground/50",
  info: "bg-[var(--info)]",
  success: "bg-[var(--success)]",
  warning: "bg-[var(--warning)]",
  danger: "bg-[var(--destructive)]",
};

export interface ProgressionRailProps {
  /**
   * The run this rail belongs to. Used solely to key the "Show full
   * history" expansion state (see module doc above) — never used to
   * re-derive or re-order `entries`.
   */
  runId: string;
  /** Progression entries. Re-sorted defensively most-recent-first here so
   * this component satisfies ordering (Requirement 4.2) even if a caller
   * passes entries in a different order. */
  entries: ProgressionEntry[];
  /** Number of most-recent entries shown inline before expansion. */
  collapsedCount?: number;
  /**
   * True when the underlying run events query failed to load. Takes
   * precedence over the empty-entries case — even a locally cached empty
   * `entries` array renders "History unavailable" while this is true
   * (Requirement 4.9).
   */
  eventsLoadFailed?: boolean;
  className?: string;
}

function sortMostRecentFirst(entries: ProgressionEntry[]): ProgressionEntry[] {
  return [...entries].sort(
    (a, b) => new Date(b.timestamp).getTime() - new Date(a.timestamp).getTime(),
  );
}

function ProgressionRailRow({ entry }: { entry: ProgressionEntry }) {
  return (
    <div
      className="flex items-start gap-2 py-1 text-xs leading-snug text-muted-foreground"
      data-testid="progression-entry"
      data-tone={entry.tone}
    >
      <span
        aria-hidden="true"
        className={cn("mt-1 size-1.5 shrink-0 rounded-full", TONE_DOT_CLASS[entry.tone])}
      />
      <span className="min-w-0 flex-1 break-words">{entry.label}</span>
      <span
        className="shrink-0 whitespace-nowrap text-[11px] text-muted-foreground/70"
        title={formatRunDate(entry.timestamp)}
      >
        {formatRunDateRelative(entry.timestamp)}
      </span>
    </div>
  );
}

export function ProgressionRail({
  runId,
  entries,
  collapsedCount = 3,
  eventsLoadFailed = false,
  className,
}: ProgressionRailProps) {
  const sorted = React.useMemo(() => sortMostRecentFirst(entries), [entries]);

  // Requirement 4.9: events failed to load — render "History unavailable",
  // regardless of whether the (locally cached) entries array is empty, and
  // without affecting sibling regions (this component renders nothing else).
  if (eventsLoadFailed) {
    return (
      <section
        className={cn("min-w-0", className)}
        data-testid="progression-rail"
        data-state="unavailable"
      >
        <p className="text-xs text-muted-foreground">History unavailable</p>
      </section>
    );
  }

  // Requirement 4.8: events loaded successfully but there is no history yet.
  if (sorted.length === 0) {
    return (
      <section
        className={cn("min-w-0", className)}
        data-testid="progression-rail"
        data-state="empty"
      >
        <p className="text-xs text-muted-foreground">No history yet</p>
      </section>
    );
  }

  const visible = sorted.slice(0, collapsedCount);
  const rest = sorted.slice(collapsedCount);
  const hasMore = rest.length > 0;

  return (
    <section
      className={cn("min-w-0", className)}
      data-testid="progression-rail"
      data-state="populated"
    >
      <div className="flex flex-col">
        {visible.map((entry) => (
          <ProgressionRailRow key={entry.id} entry={entry} />
        ))}
      </div>

      {hasMore ? (
        <ProgressiveDisclosure
          resetKey={runId}
          label={(expanded) =>
            expanded ? "Hide full history" : `Show full history (${sorted.length})`
          }
          className="mt-1"
          triggerClassName="text-[11px]"
        >
          <div className="flex flex-col">
            {rest.map((entry) => (
              <ProgressionRailRow key={entry.id} entry={entry} />
            ))}
          </div>
        </ProgressiveDisclosure>
      ) : null}
    </section>
  );
}
