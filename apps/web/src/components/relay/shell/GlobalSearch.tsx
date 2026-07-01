// ============================================================
// Relay Shell — Global_Search overlay
// ============================================================
//
// The Top_Bar-summoned search overlay (Requirement 5). Kept under the
// user-facing label "Global Search", the underlying capability is an ENTITY
// search: it matches only the names, titles, labels, and identifiers of
// Projects, Plans, Passes, and Runs exposed by the API_Contract
// (Requirement 5.8). It never searches artifact contents, logs, validation or
// executor output, source files, Planner handoff Markdown, canonical/audit
// packet contents, or repository file contents — the `SearchableEntity` corpus
// supplied by `useShellData` carries no such fields.
//
// Behavior:
//   - Input accepts up to 256 characters (Req 5.1).
//   - Queries >= 2 chars run `searchEntities` over the corpus, capped at 50
//     navigable results (Req 5.2).
//   - Queries < 2 chars issue no search, prompt for a longer query, and retain
//     the entered input (Req 5.3).
//   - Selecting a result navigates to the entity's route by type (Req 5.4).
//   - No matches shows a no-results state that retains the submitted query
//     (Req 5.5).
//   - When the corpus queries fail, an error state with a retry affordance is
//     shown and the submitted query is retained (Req 5.6).
//   - Focus is moved into and trapped within the overlay, and Escape closes it
//     and restores focus to the control that opened it — provided by the Radix
//     `Dialog` primitive (Req 9.3, 9.4).

import * as React from "react";
import { useNavigate } from "@tanstack/react-router";
import {
  ChevronRight,
  ClipboardList,
  FolderGit2,
  Layers,
  Play,
  Search,
} from "lucide-react";

import { cn } from "@/lib/utils";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import { RelayStateSurface } from "@/components/relay/RelayStateSurface";
import {
  MAX_SEARCH_RESULTS,
  checkQueryGate,
  searchEntities,
} from "@/features/relay-navigation/search";
import type { SearchableEntity, SearchResult } from "@/features/relay-navigation/types";
import { useShellData } from "@/features/relay-navigation/useShellData";

/** Maximum characters the search input accepts (Requirement 5.1). */
const MAX_QUERY_LENGTH = 256;

type EntityIcon = React.ComponentType<{ className?: string; "aria-hidden"?: boolean }>;

const ENTITY_ICONS: Record<SearchableEntity["type"], EntityIcon> = {
  project: FolderGit2,
  plan: ClipboardList,
  pass: Layers,
  run: Play,
};

const ENTITY_TYPE_LABEL: Record<SearchableEntity["type"], string> = {
  project: "Project",
  plan: "Plan",
  pass: "Pass",
  run: "Run",
};

const resultRowClassName =
  "flex w-full items-center gap-3 rounded border border-[var(--relay-row-border)] " +
  "bg-[var(--relay-panel-bg)] px-3 py-2.5 text-left transition-colors " +
  "hover:bg-[var(--relay-row-hover-bg)] focus-visible:outline-none " +
  "focus-visible:ring-1 focus-visible:ring-[var(--relay-accent)] group";

export interface GlobalSearchProps {
  /** Controlled open state — the AppShell hosts the open/close state (task 12.1). */
  open: boolean;
  onOpenChange: (open: boolean) => void;
  className?: string;
}

/**
 * The Global_Search overlay. Controlled by the hosting shell via `open` /
 * `onOpenChange`. Results are sourced only from the `searchCorpus` composed by
 * the Shell_Data_Composition_Layer (`useShellData`) over existing API_Contract
 * queries (Req 5.7).
 */
export function GlobalSearch({ open, onOpenChange, className }: GlobalSearchProps) {
  const navigate = useNavigate();
  const { searchCorpus, runsQuery, plansQuery, projectsQuery } = useShellData();

  const [query, setQuery] = React.useState("");

  // The corpus is composed from the runs/plans/projects list queries. If any of
  // those underlying queries fail, the search cannot be satisfied — surface an
  // aggregate error state that retains the query and offers retry (Req 5.6).
  const isError = runsQuery.isError || plansQuery.isError || projectsQuery.isError;

  const retry = React.useCallback(() => {
    runsQuery.refetch();
    plansQuery.refetch();
    projectsQuery.refetch();
  }, [runsQuery, plansQuery, projectsQuery]);

  // Reset the query on close so a fresh open starts clean; the query is never
  // cleared while the overlay stays open, so it is retained across the
  // too-short / no-results / error states (Req 5.3, 5.5, 5.6).
  const handleOpenChange = React.useCallback(
    (next: boolean) => {
      if (!next) setQuery("");
      onOpenChange(next);
    },
    [onOpenChange],
  );

  const gate = checkQueryGate(query);
  const tooShort = gate.kind === "too-short";

  const results = React.useMemo<SearchResult[]>(
    () => (tooShort ? [] : searchEntities(query, searchCorpus)),
    [tooShort, query, searchCorpus],
  );

  const handleSelect = React.useCallback(
    (entity: SearchableEntity) => {
      // Navigate by entity type so the target stays type-checked against the
      // existing route inventory (Req 5.4).
      switch (entity.type) {
        case "run":
          void navigate({ to: "/runs/$runId", params: { runId: entity.id } });
          break;
        case "plan":
          void navigate({ to: "/plans/$planId", params: { planId: entity.id } });
          break;
        case "pass":
          void navigate({
            to: "/plans/$planId/passes/$passId",
            params: { planId: entity.params.planId, passId: entity.id },
          });
          break;
        case "project":
          void navigate({
            to: "/projects/$projectId",
            params: { projectId: entity.id },
          });
          break;
      }
      handleOpenChange(false);
    },
    [navigate, handleOpenChange],
  );

  const atResultCap = results.length >= MAX_SEARCH_RESULTS;

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogContent
        showCloseButton={false}
        className={cn("top-[15%] translate-y-0 gap-3 p-0 sm:max-w-lg", className)}
      >
        <DialogTitle className="sr-only">Global search</DialogTitle>
        <DialogDescription className="sr-only">
          Search across Projects, Plans, Passes, and Runs by name or identifier.
        </DialogDescription>

        {/* Search input (Req 5.1) */}
        <div className="flex items-center gap-2 border-b border-[var(--relay-row-border)] px-3 py-2.5">
          <Search className="size-4 shrink-0 text-muted-foreground" aria-hidden />
          <Input
            type="search"
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            maxLength={MAX_QUERY_LENGTH}
            placeholder="Search projects, plans, passes, runs…"
            aria-label="Search projects, plans, passes, and runs"
            className="h-9 border-0 bg-transparent px-0 focus-visible:border-0 focus-visible:ring-0"
          />
        </div>

        <div className="max-h-[min(60vh,24rem)] overflow-y-auto px-3 pb-3">
          {tooShort ? (
            <RelayStateSurface
              tone="info"
              density="compact"
              title="Type at least 2 characters"
              description="Enter a name or identifier to search Projects, Plans, Passes, and Runs."
            />
          ) : isError ? (
            <RelayStateSurface
              tone="danger"
              density="compact"
              title="Search failed to load"
              description="Relay could not load the data to search. Check the API process and retry."
              action={
                <Button variant="outline" size="sm" onClick={retry}>
                  Retry
                </Button>
              }
            />
          ) : results.length === 0 ? (
            <RelayStateSurface
              tone="empty"
              density="compact"
              title="No matching entities"
              description="No Projects, Plans, Passes, or Runs match your search."
            />
          ) : (
            <div className="flex flex-col gap-2">
              <ul className="flex flex-col gap-2" aria-label="Search results">
                {results.map(({ entity }) => {
                  const Icon = ENTITY_ICONS[entity.type];
                  return (
                    <li key={`${entity.type}-${entity.id}`}>
                      <button
                        type="button"
                        onClick={() => handleSelect(entity)}
                        aria-label={`Open ${ENTITY_TYPE_LABEL[
                          entity.type
                        ].toLowerCase()} ${entity.name}`}
                        className={resultRowClassName}
                      >
                        <Icon
                          className="size-4 shrink-0 text-muted-foreground"
                          aria-hidden
                        />
                        <div className="min-w-0 flex-1">
                          <p className="truncate text-sm font-medium leading-snug text-foreground">
                            {entity.name}
                          </p>
                          <p className="mt-0.5 truncate font-mono text-[11px] text-muted-foreground/60">
                            {entity.id}
                          </p>
                        </div>
                        <span className="shrink-0 rounded-sm border border-[var(--relay-row-border)] px-2 py-0.5 text-[10px] font-medium uppercase tracking-wide text-muted-foreground">
                          {ENTITY_TYPE_LABEL[entity.type]}
                        </span>
                        <ChevronRight className="size-4 shrink-0 text-muted-foreground/30 transition-colors group-hover:text-muted-foreground" />
                      </button>
                    </li>
                  );
                })}
              </ul>
              {atResultCap ? (
                <p className="px-1 text-[11px] text-muted-foreground/60">
                  Showing the first {MAX_SEARCH_RESULTS} matches. Refine your search
                  to narrow results.
                </p>
              ) : null}
            </div>
          )}
        </div>
      </DialogContent>
    </Dialog>
  );
}
