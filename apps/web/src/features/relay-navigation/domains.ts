// ============================================================
// Relay Navigation — Primary-domain registry + active-domain resolution
// ============================================================
//
// Pure presentation logic (no React) for the Activity_Rail primary-domain
// navigation. Exposes the ordered Projects/Plans/Runs registry and
// `resolveActiveDomain(pathname)` which marks exactly one primary domain active
// for domain-owned routes and none for non-domain authenticated routes.
//
// Requirements: 1.2 (rail displays Projects/Plans/Runs as navigable
// destinations), 1.4 (exactly one domain active for a domain route, the other
// two not active), 1.5 (no domain active for an authenticated route that
// belongs to no primary domain).

import type { PrimaryDomain } from "./types";

/**
 * Static metadata for a primary domain destination in the Activity_Rail.
 *
 * `basePath` is the domain's root route and doubles as the rail link target.
 * A route is considered owned by this domain when its pathname equals
 * `basePath` or begins with `basePath + "/"`.
 */
export interface PrimaryDomainDescriptor {
  id: PrimaryDomain;
  /** Visible, accessible label for the rail destination. */
  label: string;
  /** Domain root route; also the rail link target. */
  basePath: string;
}

/**
 * Ordered registry of the primary domains shown in the Activity_Rail
 * (Requirement 1.2). Order is the canonical display order Projects → Plans →
 * Runs.
 */
export const PRIMARY_DOMAINS: readonly PrimaryDomainDescriptor[] = [
  { id: "projects", label: "Projects", basePath: "/projects" },
  { id: "plans", label: "Plans", basePath: "/plans" },
  { id: "runs", label: "Runs", basePath: "/runs" },
] as const;

/**
 * Normalize a pathname for prefix comparison:
 * - strips query string and hash fragments,
 * - collapses a trailing slash (except for the root "/").
 */
function normalizePathname(pathname: string): string {
  // Drop query (?...) and hash (#...) so route params/anchors don't affect matching.
  let path = pathname;
  const queryIndex = path.search(/[?#]/);
  if (queryIndex !== -1) {
    path = path.slice(0, queryIndex);
  }

  // Collapse a single trailing slash, but preserve the root "/".
  if (path.length > 1 && path.endsWith("/")) {
    path = path.slice(0, -1);
  }

  return path;
}

/**
 * Returns true when `pathname` is owned by the domain rooted at `basePath`:
 * either the base route itself or any nested route beneath it.
 */
function isPathInDomain(pathname: string, basePath: string): boolean {
  return pathname === basePath || pathname.startsWith(`${basePath}/`);
}

/**
 * Resolve the active primary domain for a route pathname.
 *
 * Returns exactly the one primary domain whose section the pathname belongs to
 * (Projects, Plans, or Runs), and `null` for any pathname — including the
 * application root `/` — that does not belong to a primary domain
 * (Requirements 1.4, 1.5).
 */
export function resolveActiveDomain(pathname: string): PrimaryDomain | null {
  const normalized = normalizePathname(pathname);

  for (const domain of PRIMARY_DOMAINS) {
    if (isPathInDomain(normalized, domain.basePath)) {
      return domain.id;
    }
  }

  return null;
}
