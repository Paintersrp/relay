// ============================================================
// Relay Navigation — Attention + recent-activity selection
// ============================================================
//
// Pure, React-free selectors backing the Home_Overview landing surface
// (Requirement 3). This module owns:
//   - `selectAttentionRuns(runs)`: the Runs whose canonical `status` is in the
//     CLOSED blocked / awaiting-review classification, capped at 50, plus a
//     `totalCount` equal to the full number of qualifying Runs (Req 3.2).
//   - `selectRecentActivity(items)`: the 10 most recently updated Runs, Plans,
//     or Projects ordered by `updatedAt` descending (Req 3.3).
//
// Attention classification boundary (Requirements 3.11, 3.12):
//   Attention membership is decided SOLELY against the closed, authoritative
//   `BLOCKED_STATUSES` / `AWAITING_REVIEW_STATUSES` sets from `statusSets.ts`.
//   This set is a UI/view-level classification only and MUST NOT be expanded
//   without an explicit update to Requirement 3.2. A broader helper such as
//   `getRelayAttentionReason` may be used elsewhere ONLY to produce a display
//   label or icon — it is intentionally NOT consulted here and never adds a Run
//   to the attention classification.

import type { WorkflowRunStatus } from "@/features/relay-runs";
import { AWAITING_REVIEW_STATUSES, BLOCKED_STATUSES } from "./statusSets";
import type { RecentActivityItem } from "./types";

// ------------------------------------------------------------
// Attention run selection (Req 3.2, 3.11, 3.12)
// ------------------------------------------------------------

/** Maximum number of attention Runs surfaced by {@link selectAttentionRuns} (Req 3.2). */
export const MAX_ATTENTION_RUNS = 50;

/**
 * Closed attention classification set (Requirement 3.11). Built once from the
 * authoritative `BLOCKED_STATUSES` / `AWAITING_REVIEW_STATUSES` constants. This
 * is the SOLE authority for which Runs appear in the Home_Overview attention
 * section; no broader label/icon helper is consulted.
 */
const ATTENTION_STATUSES: ReadonlySet<string> = new Set<string>([
  ...BLOCKED_STATUSES,
  ...AWAITING_REVIEW_STATUSES,
]);

/**
 * True when the canonical `status` is in the closed blocked / awaiting-review
 * classification (Requirement 3.11).
 */
function isAttentionStatus(status: WorkflowRunStatus | string): boolean {
  return ATTENTION_STATUSES.has(status);
}

/**
 * Minimal input shape for a Run considered for the attention section. Kept
 * intentionally small — only the fields the selector needs to classify the Run
 * and build a navigable output item.
 */
export interface AttentionRunInput {
  id: string;
  label: string;
  status: WorkflowRunStatus | string;
  updatedAt: string; // ISO-8601
}

/**
 * A Run surfaced in the Home_Overview attention section. Carries the canonical
 * `status` (for the status color/label the consumer renders) alongside the
 * navigation target for the Run's workbench route.
 */
export interface AttentionRunItem {
  type: "run";
  id: string;
  label: string;
  status: WorkflowRunStatus | string;
  updatedAt: string; // ISO-8601
  to: string;
  params: Record<string, string>;
}

/**
 * Result of {@link selectAttentionRuns}: the capped list of attention Runs plus
 * the full qualifying count so the consumer can render an overflow indicator
 * when `totalCount` exceeds `items.length` (Requirement 3.2).
 */
export interface AttentionRunSelection {
  items: AttentionRunItem[];
  totalCount: number;
}

/**
 * Select the Runs that need the operator's attention (Requirement 3.2).
 *
 * Returns only Runs whose canonical `status` is in the closed blocked
 * (`blocked`, `executor_blocked`) or awaiting-review (`intake_needs_review`,
 * `brief_ready_for_review`, `audit_ready`, `audit_ready_for_review`,
 * `revision_required`) set, capped at {@link MAX_ATTENTION_RUNS} items, plus a
 * `totalCount` equal to the full number of qualifying Runs in the input.
 *
 * Input order is preserved; the cap takes the first {@link MAX_ATTENTION_RUNS}
 * qualifying Runs. Classification uses ONLY the closed set (Req 3.11); no
 * broader helper is consulted (Req 3.12).
 */
export function selectAttentionRuns(runs: AttentionRunInput[]): AttentionRunSelection {
  const qualifying = runs.filter((run) => isAttentionStatus(run.status));

  const items: AttentionRunItem[] = qualifying
    .slice(0, MAX_ATTENTION_RUNS)
    .map((run) => ({
      type: "run",
      id: run.id,
      label: run.label,
      status: run.status,
      updatedAt: run.updatedAt,
      to: "/runs/$runId",
      params: { runId: run.id },
    }));

  return { items, totalCount: qualifying.length };
}

// ------------------------------------------------------------
// Recent-activity selection (Req 3.3)
// ------------------------------------------------------------

/** Maximum number of recent-activity items surfaced by {@link selectRecentActivity} (Req 3.3). */
export const MAX_RECENT_ACTIVITY = 10;

/**
 * Returns the numeric timestamp for an `updatedAt` value, or `-Infinity` when
 * it cannot be parsed so that unparseable entries sort to the end.
 */
function updatedAtMillis(updatedAt: string): number {
  const millis = new Date(updatedAt).getTime();
  return Number.isNaN(millis) ? Number.NEGATIVE_INFINITY : millis;
}

/**
 * Select the recent-activity items for the Home_Overview (Requirement 3.3).
 *
 * Takes a mixed list of Runs, Plans, and Projects and returns at most
 * {@link MAX_RECENT_ACTIVITY} items ordered by `updatedAt` descending — the 10
 * most recently updated. The sort is stable (via `Array.prototype.sort`), so
 * items sharing an `updatedAt` retain their input order, and the returned list
 * is exactly the prefix of the input sorted by `updatedAt` descending.
 */
export function selectRecentActivity(items: RecentActivityItem[]): RecentActivityItem[] {
  return [...items]
    .sort((a, b) => updatedAtMillis(b.updatedAt) - updatedAtMillis(a.updatedAt))
    .slice(0, MAX_RECENT_ACTIVITY);
}
