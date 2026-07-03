// ============================================================
// Relay Navigation — Global_Search entity matching + short-query gate
// ============================================================
//
// Pure, presentation-only helpers backing the Global_Search overlay
// (Requirement 5). Global_Search is an ENTITY search: it matches only the
// names, titles, labels, and identifiers of Projects, Plans, Passes, and Runs
// exposed by the API_Contract (Requirement 5.8). It MUST NOT search over
// artifact contents, logs, validation output, executor output, source files,
// Planner handoff Markdown, canonical/audit packet contents, or repository
// file contents. The `SearchableEntity` corpus (see `types.ts`) intentionally
// carries only `name` (display name/title/label) and `id` (identifier) match
// fields to enforce that boundary structurally.

import type { SearchableEntity, SearchResult } from "./types";

/** Maximum number of results returned by {@link searchEntities} (Requirement 5.2). */
export const MAX_SEARCH_RESULTS = 50;

/** Minimum query length (after normalization) required to run a search (Requirement 5.3). */
export const MIN_QUERY_LENGTH = 2;

/**
 * Shared normalization used by BOTH matching and gating: trims surrounding
 * whitespace and lowercases so comparisons are case-insensitive and
 * whitespace-insensitive at the edges.
 */
export function normalizeQuery(query: string): string {
  return query.trim().toLowerCase();
}

/**
 * Case-insensitive substring test. `value` is lowercased; the needle is
 * expected to already be normalized (lowercased) by the caller.
 */
function includesNormalized(value: string, normalizedNeedle: string): boolean {
  return value.toLowerCase().includes(normalizedNeedle);
}

/**
 * Result of the short-query gate (Requirement 5.3). The overlay calls this
 * BEFORE searching so it can prompt for a longer query without scanning the
 * corpus.
 */
export type QueryGateResult =
  | { kind: "too-short" }
  | { kind: "ok"; normalized: string };

/**
 * Short-query gate. Reports `"too-short"` for queries under 2 characters
 * (after trim/normalize) without touching the corpus (Requirement 5.3).
 * On success it returns the normalized query so callers can reuse it.
 */
export function checkQueryGate(query: string): QueryGateResult {
  const normalized = normalizeQuery(query);
  if (normalized.length < MIN_QUERY_LENGTH) {
    return { kind: "too-short" };
  }
  return { kind: "ok", normalized };
}

/**
 * Convenience boolean helper mirroring {@link checkQueryGate} for call sites
 * that only need a yes/no answer.
 */
export function isQueryTooShort(query: string): boolean {
  return normalizeQuery(query).length < MIN_QUERY_LENGTH;
}

/**
 * Entity search (Requirement 5.2 / 5.8).
 *
 * For queries of at least 2 characters (after normalization), returns the
 * entities whose `name` OR `id` contains the query as a case-insensitive
 * substring, capped at {@link MAX_SEARCH_RESULTS}. For queries that fail the
 * short-query gate, returns an empty array WITHOUT scanning the corpus.
 *
 * Matching is confined to `name` and `id` — the only match fields the entity
 * search corpus carries — so no artifact/log/source/content field is ever
 * searched (Requirement 5.8).
 */
export function searchEntities(
  query: string,
  corpus: SearchableEntity[],
): SearchResult[] {
  const gate = checkQueryGate(query);
  if (gate.kind === "too-short") {
    return [];
  }

  const needle = gate.normalized;
  const results: SearchResult[] = [];

  for (const entity of corpus) {
    if (
      includesNormalized(entity.name, needle) ||
      includesNormalized(entity.id, needle)
    ) {
      results.push({ entity });
      if (results.length >= MAX_SEARCH_RESULTS) {
        break;
      }
    }
  }

  return results;
}
