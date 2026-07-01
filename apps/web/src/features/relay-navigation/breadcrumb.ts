// ============================================================
// Relay Navigation — Breadcrumb construction
// ============================================================
//
// Pure derivation of the active hierarchy path (Project → Plan → Pass → Run)
// for the Breadcrumb_Trail. This module contains no React and performs no data
// fetching; it derives ordered segments purely from an already-resolved
// hierarchy (`ResolvedHierarchy`), which itself is sourced only from
// API_Contract data (Requirement 2.8).
//
// Behavior (Requirements 2.1, 2.2, 2.3, 2.6, 2.7):
//   - Segments are strictly ordered root-to-leaf by level rank
//     Project < Plan < Pass < Run.
//   - Only levels actually present in the resolved hierarchy are included;
//     absent or unresolvable ancestors are omitted, never fabricated as
//     placeholders. A standalone Run with no Plan/Pass yields no Plan/Pass
//     segments.
//   - Every non-final (ancestor) segment is navigable (defined `to` + `params`).
//   - The final (leaf) segment is non-navigable (no `to`).

import type { BreadcrumbSegment, ResolvedHierarchy } from "./types";

type Level = BreadcrumbSegment["level"];

// Canonical root-to-leaf ordering by level rank.
const LEVEL_ORDER: readonly Level[] = ["project", "plan", "pass", "run"];

interface ResolvedRoute {
  to: string;
  params: Record<string, string>;
}

/**
 * Resolve the navigable route for an ancestor level, matching the existing
 * route inventory (Requirement 10.1). Returns `null` when the level's route
 * cannot be constructed from the available identifiers (treated as an
 * unresolvable ancestor and therefore omitted rather than fabricated).
 */
function resolveRoute(level: Level, resolved: ResolvedHierarchy): ResolvedRoute | null {
  switch (level) {
    case "project": {
      const id = resolved.project?.id;
      if (!id) return null;
      return { to: "/projects/$projectId", params: { projectId: id } };
    }
    case "plan": {
      const id = resolved.plan?.id;
      if (!id) return null;
      return { to: "/plans/$planId", params: { planId: id } };
    }
    case "pass": {
      // The pass route is nested under its plan and requires both identifiers.
      const planId = resolved.plan?.id;
      const passId = resolved.pass?.id;
      if (!planId || !passId) return null;
      return {
        to: "/plans/$planId/passes/$passId",
        params: { planId, passId },
      };
    }
    case "run": {
      const id = resolved.run?.id;
      if (!id) return null;
      return { to: "/runs/$runId", params: { runId: id } };
    }
  }
}

/** Returns the label for a level if that level is present in the hierarchy. */
function levelLabel(level: Level, resolved: ResolvedHierarchy): string | undefined {
  switch (level) {
    case "project":
      return resolved.project?.label;
    case "plan":
      return resolved.plan?.label;
    case "pass":
      return resolved.pass?.label;
    case "run":
      return resolved.run?.label;
  }
}

/**
 * Build ordered root-to-leaf breadcrumb segments from a resolved hierarchy.
 *
 * Ancestor segments carry a navigable `to`/`params`; the final leaf segment is
 * non-navigable. Present levels whose ancestor route cannot be resolved are
 * omitted rather than fabricated.
 */
export function buildBreadcrumbSegments(resolved: ResolvedHierarchy): BreadcrumbSegment[] {
  // Present levels in canonical root-to-leaf order.
  const presentLevels = LEVEL_ORDER.filter(
    (level): level is Level => levelLabel(level, resolved) !== undefined,
  );

  if (presentLevels.length === 0) return [];

  const leafLevel = presentLevels[presentLevels.length - 1];
  const segments: BreadcrumbSegment[] = [];

  for (const level of presentLevels) {
    const label = levelLabel(level, resolved);
    if (label === undefined) continue; // exhaustive guard; present levels only

    if (level === leafLevel) {
      // Final segment: current leaf, non-navigable.
      segments.push({ level, label });
      continue;
    }

    // Ancestor segment: must be navigable. Omit if its route is unresolvable.
    const route = resolveRoute(level, resolved);
    if (route === null) continue;

    segments.push({ level, label, to: route.to, params: route.params });
  }

  return segments;
}
