// ============================================================
// Run Status Tracker Redesign — Progression log derivation (Requirement 4)
// ============================================================
//
// Pure, presentation-only helpers that derive the Progression_Rail's
// `ProgressionEntry[]` from the existing `RelayRunEvent[]` for a run.
//
// `classifyEventTone` reuses the existing, closed `BLOCKED_STATUSES` /
// `AWAITING_REVIEW_STATUSES` sets from `relay-navigation/statusSets.ts`
// unchanged — no new status classification set is introduced here.
//
// `deriveProgressionLog` maps each input event 1:1 to a `ProgressionEntry`
// (never dropping or synthesizing an entry) and returns the result ordered
// most-recent-first by `timestamp`.

import {
  BLOCKED_STATUSES,
  AWAITING_REVIEW_STATUSES,
} from "../relay-navigation/statusSets";

import type { RelayRunEvent, RelayRunEventKind } from "./types";
import type { ProgressionEntry, Tone } from "./runStatusTrackerViews";

// ------------------------------------------------------------
// classifyEventTone
// ------------------------------------------------------------

const BLOCKED_STATUS_SET = new Set<string>(BLOCKED_STATUSES);
const AWAITING_REVIEW_STATUS_SET = new Set<string>(AWAITING_REVIEW_STATUSES);

/**
 * Classifies a single `RelayRunEvent` to a `Tone`:
 * - "danger" when the event is a status-change whose target status is a
 *   member of the existing closed `BLOCKED_STATUSES` set.
 * - "warning" when the event is a status-change whose target status is a
 *   member of the existing closed `AWAITING_REVIEW_STATUSES` set.
 * - "neutral" otherwise (including non-status-change events, and
 *   status-change events whose target status is outside both sets or
 *   simply unavailable on `event.details`).
 *
 * Reuses `BLOCKED_STATUSES`/`AWAITING_REVIEW_STATUSES` unchanged; no new
 * classification set is introduced.
 */
export function classifyEventTone(event: RelayRunEvent): Tone {
  if (event.kind !== "status_change") {
    return "neutral";
  }

  const targetStatus = event.details?.status;
  if (typeof targetStatus !== "string") {
    return "neutral";
  }

  if (BLOCKED_STATUS_SET.has(targetStatus)) {
    return "danger";
  }
  if (AWAITING_REVIEW_STATUS_SET.has(targetStatus)) {
    return "warning";
  }

  return "neutral";
}

// ------------------------------------------------------------
// deriveProgressionLog
// ------------------------------------------------------------

// Plain-language fallback label used only when an event has no `message`
// text of its own. `event.message` is otherwise used verbatim, since
// existing event messages already read as past-tense records (e.g.
// "Relay: Received handoff packet").
const KIND_FALLBACK_LABELS: Record<RelayRunEventKind, string> = {
  log: "Log entry recorded",
  status_change: "Status changed",
  artifact_created: "Artifact created",
  validation_run: "Validation run recorded",
  step_transition: "Step transition recorded",
};

function formatEventLabel(event: RelayRunEvent): string {
  const message = event.message?.trim();
  if (message) {
    return message;
  }
  return KIND_FALLBACK_LABELS[event.kind] ?? "Event recorded";
}

/**
 * Derives the Progression_Rail's `ProgressionEntry[]` from a run's existing
 * `RelayRunEvent[]`. Maps each input event 1:1 to a `ProgressionEntry`
 * (`timestamp = event.createdAt`, plain-language `label` from
 * `event.message`/`event.kind`, `tone` from `classifyEventTone`), then
 * returns the result ordered most-recent-first by `timestamp`.
 *
 * Never drops an input event from the output and never synthesizes an
 * entry that does not correspond to a real input event — output length
 * always equals input length. Returns `[]` for empty input.
 */
export function deriveProgressionLog(events: RelayRunEvent[]): ProgressionEntry[] {
  const entries: ProgressionEntry[] = events.map((event) => ({
    id: event.id,
    timestamp: event.createdAt,
    label: formatEventLabel(event),
    tone: classifyEventTone(event),
  }));

  return entries.sort(
    (a, b) => Date.parse(b.timestamp) - Date.parse(a.timestamp),
  );
}
