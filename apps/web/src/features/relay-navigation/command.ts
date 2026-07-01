// ============================================================
// Relay Navigation — Command palette entry registry + filtering
// ============================================================
//
// Pure, React-free logic for the Command_Palette. This module owns:
//   - the primary-domain navigation entries (Projects, Plans, Runs),
//   - the recents builder (5 newest per entity group by `updatedAt` desc),
//   - the CLOSED action-entry set (exactly New Run and New Plan), and
//   - `filterCommandEntries(query, entries)` (case-insensitive substring on
//     the visible label).
//
// The action-entry set is frozen to exactly "new-run" and "new-plan"
// (Requirement 4.3). This module does NOT and MUST NOT expose any
// lifecycle-mutating Run action entry — approve intake, prepare/compile,
// render brief, approve brief, dispatch executor, cancel executor, validate,
// repair validation, generate audit, approve audit, request revision, close
// run, or delete/mutate an artifact (Requirement 4.10). Those actions remain
// owned by existing run-scoped workflow surfaces and the Go_Daemon API and are
// out of scope here. The `CommandEntry` action variant has no variant capable
// of expressing them.

import type { CommandEntry, PrimaryDomain } from "./types";

// ------------------------------------------------------------
// Primary-domain navigation entries (Req 4.2)
// ------------------------------------------------------------

/**
 * The three primary-domain navigation entries, in Projects → Plans → Runs
 * order. These are always present in the command palette (Requirement 4.2).
 */
export const PRIMARY_DOMAIN_NAV_ENTRIES: readonly CommandEntry[] = [
  { kind: "nav-domain", id: "projects", label: "Projects", to: "/projects" },
  { kind: "nav-domain", id: "plans", label: "Plans", to: "/plans" },
  { kind: "nav-domain", id: "runs", label: "Runs", to: "/runs" },
] as const;

// ------------------------------------------------------------
// Recents builder (Req 4.2)
// ------------------------------------------------------------

/**
 * Minimal input shape for a recent entity. Kept intentionally small — only the
 * fields the recents builder needs (identifier, visible label, and the
 * `updatedAt` timestamp used for ordering).
 */
export interface RecentEntityInput {
  id: string;
  label: string;
  updatedAt: string; // ISO-8601
}

/** Corpora of the three recent entity groups the palette draws recents from. */
export interface RecentEntityCorpora {
  runs: RecentEntityInput[];
  plans: RecentEntityInput[];
  projects: RecentEntityInput[];
}

/** Maximum number of recent entries surfaced per entity group (Req 4.2). */
export const RECENTS_PER_GROUP = 5;

type RecentEntity = "run" | "plan" | "project";

/**
 * Returns the numeric timestamp for an `updatedAt` value, or `-Infinity` when
 * it cannot be parsed so that unparseable entries sort to the end.
 */
function updatedAtMillis(updatedAt: string): number {
  const millis = new Date(updatedAt).getTime();
  return Number.isNaN(millis) ? Number.NEGATIVE_INFINITY : millis;
}

/**
 * Returns the `RecentEntityInput` items sorted by `updatedAt` descending,
 * limited to the `RECENTS_PER_GROUP` most recently updated. The sort is
 * stable, so items sharing an `updatedAt` retain their input order.
 */
function newestPerGroup(items: RecentEntityInput[]): RecentEntityInput[] {
  return [...items]
    .sort((a, b) => updatedAtMillis(b.updatedAt) - updatedAtMillis(a.updatedAt))
    .slice(0, RECENTS_PER_GROUP);
}

function toRecentEntry(entity: RecentEntity, item: RecentEntityInput): CommandEntry {
  switch (entity) {
    case "run":
      return {
        kind: "nav-recent",
        entity,
        id: item.id,
        label: item.label,
        to: "/runs/$runId",
        params: { runId: item.id },
      };
    case "plan":
      return {
        kind: "nav-recent",
        entity,
        id: item.id,
        label: item.label,
        to: "/plans/$planId",
        params: { planId: item.id },
      };
    case "project":
      return {
        kind: "nav-recent",
        entity,
        id: item.id,
        label: item.label,
        to: "/projects/$projectId",
        params: { projectId: item.id },
      };
  }
}

/**
 * Builds the recent-navigation entries for the command palette: at most
 * `RECENTS_PER_GROUP` (5) entries per entity group, each drawn from the 5 most
 * recently updated items of that group by `updatedAt` descending (Req 4.2).
 * Groups are emitted in Runs → Plans → Projects order.
 */
export function buildRecentEntries(corpora: RecentEntityCorpora): CommandEntry[] {
  return [
    ...newestPerGroup(corpora.runs).map((item) => toRecentEntry("run", item)),
    ...newestPerGroup(corpora.plans).map((item) => toRecentEntry("plan", item)),
    ...newestPerGroup(corpora.projects).map((item) => toRecentEntry("project", item)),
  ];
}

// ------------------------------------------------------------
// Action entries — CLOSED set (Req 4.3, 4.10)
// ------------------------------------------------------------

/**
 * Callbacks wired by the palette component to execute each action entry. The
 * action set is closed to exactly New Run and New Plan; there is no handler for
 * any lifecycle-mutating Run action (Requirement 4.3, 4.10).
 */
export interface CommandActionHandlers {
  onNewRun: () => void;
  onNewPlan: () => void;
}

/**
 * Builds the CLOSED action-entry set: exactly New Run (`new-run`) and New Plan
 * (`new-plan`), in that order, and no others (Requirement 4.3). No
 * lifecycle-mutating Run action entry is or may be added here (Req 4.10).
 */
export function buildActionEntries(handlers: CommandActionHandlers): CommandEntry[] {
  return [
    { kind: "action", id: "new-run", label: "New Run", run: handlers.onNewRun },
    { kind: "action", id: "new-plan", label: "New Plan", run: handlers.onNewPlan },
  ];
}

// ------------------------------------------------------------
// Full entry registry
// ------------------------------------------------------------

/**
 * Assembles the full command-entry registry in stable order: the three
 * primary-domain navigation entries (always present), then the per-group
 * recents (Req 4.2), then the closed New Run / New Plan action set (Req 4.3).
 */
export function buildCommandEntries(
  corpora: RecentEntityCorpora,
  handlers: CommandActionHandlers,
): CommandEntry[] {
  return [
    ...PRIMARY_DOMAIN_NAV_ENTRIES,
    ...buildRecentEntries(corpora),
    ...buildActionEntries(handlers),
  ];
}

// ------------------------------------------------------------
// Filtering (Req 4.4)
// ------------------------------------------------------------

/**
 * Returns exactly the entries whose visible `label` contains `query` as a
 * case-insensitive substring (Requirement 4.4). No qualifying entry is omitted
 * and no non-qualifying entry is included. An empty query matches every entry.
 */
export function filterCommandEntries(query: string, entries: CommandEntry[]): CommandEntry[] {
  const needle = query.toLowerCase();
  return entries.filter((entry) => entry.label.toLowerCase().includes(needle));
}

// Re-export the primary-domain identifier type for convenience at call sites
// that build or inspect command entries.
export type { PrimaryDomain };
