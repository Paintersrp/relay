// ============================================================
// Relay Shell — BreadcrumbTrail
// ============================================================
//
// Presentational Breadcrumb_Trail for the redesigned application shell. It
// renders the active hierarchy path (Project → Plan → Pass → Run) using the
// existing shadcn breadcrumb primitive (`components/ui/breadcrumb.tsx`).
//
// This component is intentionally presentational and testable: it accepts an
// already-resolved hierarchy (`ResolvedHierarchy`) as a prop and derives the
// ordered segments through the pure `buildBreadcrumbSegments` selector. It
// performs no data fetching itself — the hierarchy is composed upstream by the
// Shell_Data_Composition_Layer (`useRunHierarchy` in `useShellData.ts`), which
// sources it only from API_Contract data.
//
// Behavior (Requirements 2.1, 2.2, 2.3, 2.6, 2.7):
//   - Renders segments root-to-leaf (Project → Plan → Pass → Run), only for
//     the levels applicable to the current route.
//   - Ancestor segments (those with a `to`) render as navigable TanStack Router
//     `<Link>`s.
//   - The final segment (no `to`) renders as the non-navigable current page.
//   - Standalone Runs and unresolvable ancestors produce no fabricated
//     placeholder segments (the selector omits them).
//   - When there are no segments, nothing is rendered.

import * as React from "react";
import { Link, type LinkProps } from "@tanstack/react-router";

import {
  Breadcrumb,
  BreadcrumbItem,
  BreadcrumbLink,
  BreadcrumbList,
  BreadcrumbPage,
  BreadcrumbSeparator,
} from "@/components/ui/breadcrumb";
import { cn } from "@/lib/utils";
import { buildBreadcrumbSegments } from "@/features/relay-navigation/breadcrumb";
import type {
  BreadcrumbSegment,
  ResolvedHierarchy,
} from "@/features/relay-navigation/types";

export interface BreadcrumbTrailProps {
  /**
   * The resolved active hierarchy for the current route. Segments are derived
   * from this via `buildBreadcrumbSegments`. Callers that already hold segments
   * may pass them directly through `segments` instead.
   */
  resolved?: ResolvedHierarchy;
  /**
   * Pre-built segments. When provided, these are rendered as-is and `resolved`
   * is ignored. Useful for tests and callers that build segments upstream.
   */
  segments?: BreadcrumbSegment[];
  className?: string;
}

/**
 * A stable key for a segment. Level is unique within a well-formed trail
 * (each hierarchy level appears at most once), so it is a safe React key.
 */
function segmentKey(segment: BreadcrumbSegment): string {
  return segment.level;
}

export function BreadcrumbTrail({
  resolved,
  segments,
  className,
}: BreadcrumbTrailProps): React.JSX.Element | null {
  const items = segments ?? (resolved ? buildBreadcrumbSegments(resolved) : []);

  // No applicable hierarchy for this route — render nothing (Req 2.1).
  if (items.length === 0) {
    return null;
  }

  return (
    <Breadcrumb className={cn("min-w-0", className)}>
      <BreadcrumbList>
        {items.map((segment, index) => {
          const isLast = index === items.length - 1;
          // An ancestor segment is navigable when it carries a route.
          const navigable = segment.to !== undefined;

          return (
            <React.Fragment key={segmentKey(segment)}>
              <BreadcrumbItem className="min-w-0">
                {navigable ? (
                  <BreadcrumbLink asChild className="max-w-48 truncate">
                    <Link
                      // `to`/`params` are dynamic route strings resolved from
                      // API data; the trail is presentational so navigation
                      // props are cast to the router's LinkProps.
                      {...({
                        to: segment.to,
                        params: segment.params,
                      } as LinkProps)}
                      className="truncate"
                    >
                      {segment.label}
                    </Link>
                  </BreadcrumbLink>
                ) : (
                  <BreadcrumbPage className="max-w-64 truncate">
                    {segment.label}
                  </BreadcrumbPage>
                )}
              </BreadcrumbItem>
              {isLast ? null : <BreadcrumbSeparator />}
            </React.Fragment>
          );
        })}
      </BreadcrumbList>
    </Breadcrumb>
  );
}
