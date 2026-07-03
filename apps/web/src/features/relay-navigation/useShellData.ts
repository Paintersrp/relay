// ============================================================
// Relay Navigation — Shell_Data_Composition_Layer (useShellData)
// ============================================================
//
// Non-authoritative presentation / query-composition layer for the redesigned
// application shell. `useShellData` composes EXISTING TanStack Query helpers
// over the existing API_Contract into the shell view models consumed by the
// Scope_Switcher, Global_Search, Command_Palette, and Home_Overview:
//
//   - scope options (Projects + Plans)            → ScopeSwitcher      (Req 2.4)
//   - search corpus (Projects/Plans/Passes/Runs)  → GlobalSearch       (Req 5.7)
//   - recents corpora (Runs/Plans/Projects)       → CommandPalette     (Req 4.2)
//   - attention runs                              → HomeOverview       (Req 3.2)
//   - recent-activity list                        → HomeOverview       (Req 3.3)
//   - resolved run ancestors                      → BreadcrumbTrail    (Req 2.x)
//
// Boundary (Requirements 2.8, 2.9, 2.10, 3.8, 5.7, 10.3, 10.4):
//   - This layer ONLY re-shapes data already owned and fetched by existing
//     query helpers (`runsListQueryOptions`, `plansListQueryOptions` / `getPlans`,
//     `projectsListQueryOptions` / `getProjects`, `planDetailQueryOptions` /
//     `getPlan` for passes, and `runDetailQueryOptions` for run ancestors).
//   - It introduces NO new canonical state, NO new client-side workspace index,
//     NO background synchronization, and NO polling beyond what the composed
//     helpers already perform. It adds NO new backend endpoint (Req 10.3).
//   - It NEVER fabricates a Project, Plan, Pass, or Run that is not present in
//     an API_Contract response. Where an ancestor cannot be resolved it is
//     OMITTED rather than invented, consistent with the breadcrumb's
//     no-fabrication rule (Req 2.7, 2.10).
//   - It relocates NO orchestration / execution / validation / artifact-lifecycle
//     responsibility into the frontend (Req 10.4).

import { useMemo } from "react";
import { useQueries, useQuery } from "@tanstack/react-query";

import { runsListQueryOptions, runDetailQueryOptions } from "@/features/relay-runs";
import type { RelayRun } from "@/features/relay-runs";
import { plansListQueryOptions, planDetailQueryOptions } from "@/features/relay-plans";
import type { PlanAPIPass, PlanAPIReadPlan, PlanDetailResponse } from "@/features/relay-plans";
import { projectsListQueryOptions } from "@/features/relay-projects";
import type { RelayProject } from "@/features/relay-projects";

import type { AttentionRunInput, AttentionRunSelection } from "./attention";
import { selectAttentionRuns, selectRecentActivity } from "./attention";
import type { RecentEntityCorpora, RecentEntityInput } from "./command";
import type {
  RecentActivityItem,
  ResolvedHierarchy,
  ScopeOption,
  SearchableEntity,
} from "./types";

// ------------------------------------------------------------
// Minimal structural input shapes
// ------------------------------------------------------------
//
// The pure builders below accept intentionally small structural subsets of the
// full API_Contract models so they stay decoupled and unit-testable without
// fabricating whole entities.

/** Minimal Run shape the shell composition reads. */
export interface ShellRunLike {
  id: string;
  title?: string;
  name?: string;
  status: string;
  updatedAt: string;
}

/** Minimal Plan shape the shell composition reads. */
export interface ShellPlanLike {
  planId: string;
  title: string;
  updatedAt: string;
  projectId?: string;
}

/** Minimal Project shape the shell composition reads. */
export interface ShellProjectLike {
  projectId: string;
  name: string;
  updatedAt: string;
}

/** Minimal Pass shape the shell composition reads. */
export interface ShellPassLike {
  passId: string;
  name: string;
  sequence?: number;
}

// ------------------------------------------------------------
// Label helpers (never fabricate — fall back to identifier)
// ------------------------------------------------------------

/**
 * Human label for a Run: prefer the display `title`, then `name`, then the
 * identifier. The identifier fallback is the entity's own id (not an invented
 * placeholder), so no fabricated entity is introduced (Req 2.9).
 */
function runLabel(run: ShellRunLike): string {
  return run.title?.trim() || run.name?.trim() || run.id;
}

function planLabel(plan: ShellPlanLike): string {
  return plan.title?.trim() || plan.planId;
}

function projectLabel(project: ShellProjectLike): string {
  return project.name?.trim() || project.projectId;
}

function passLabel(pass: ShellPassLike): string {
  const name = pass.name?.trim();
  if (name && typeof pass.sequence === "number") {
    return `${pass.sequence}. ${name}`;
  }
  return name || pass.passId;
}

// ------------------------------------------------------------
// Pure builders — scope options (Req 2.4)
// ------------------------------------------------------------

/**
 * Build the Scope_Switcher options from the Projects and Plans exposed by the
 * API_Contract (Requirement 2.4). Projects are listed first, then Plans, each
 * carrying its navigable scope route.
 */
export function buildScopeOptions(
  projects: ShellProjectLike[],
  plans: ShellPlanLike[],
): ScopeOption[] {
  const projectOptions: ScopeOption[] = projects.map((project) => ({
    kind: "project",
    id: project.projectId,
    label: projectLabel(project),
    to: "/projects/$projectId",
    params: { projectId: project.projectId },
  }));

  const planOptions: ScopeOption[] = plans.map((plan) => ({
    kind: "plan",
    id: plan.planId,
    label: planLabel(plan),
    to: "/plans/$planId",
    params: { planId: plan.planId },
  }));

  return [...projectOptions, ...planOptions];
}

// ------------------------------------------------------------
// Pure builders — search corpus (Req 5.7, 5.8)
// ------------------------------------------------------------

/**
 * Search corpus inputs. `passesByPlanId` holds the passes resolved (eagerly for
 * in-scope plans, lazily for plans in search results) from plan detail
 * responses. Plans with no resolved passes simply contribute no pass entities —
 * omitted, never fabricated (Req 2.10).
 */
export interface SearchCorpusInput {
  runs: ShellRunLike[];
  plans: ShellPlanLike[];
  projects: ShellProjectLike[];
  passesByPlanId: Record<string, ShellPassLike[]>;
}

/**
 * Build the Global_Search corpus of `SearchableEntity` across Projects, Plans,
 * Passes, and Runs (Requirement 5.7). The corpus carries only entity
 * names/titles/labels and identifiers — never artifact/log/source content —
 * consistent with the entity-search boundary (Requirement 5.8).
 */
export function buildSearchCorpus(input: SearchCorpusInput): SearchableEntity[] {
  const corpus: SearchableEntity[] = [];

  for (const project of input.projects) {
    corpus.push({
      type: "project",
      id: project.projectId,
      name: projectLabel(project),
      to: "/projects/$projectId",
      params: { projectId: project.projectId },
    });
  }

  for (const plan of input.plans) {
    corpus.push({
      type: "plan",
      id: plan.planId,
      name: planLabel(plan),
      to: "/plans/$planId",
      params: { planId: plan.planId },
    });
  }

  for (const [planId, passes] of Object.entries(input.passesByPlanId)) {
    for (const pass of passes) {
      corpus.push({
        type: "pass",
        id: pass.passId,
        name: passLabel(pass),
        to: "/plans/$planId/passes/$passId",
        params: { planId, passId: pass.passId },
      });
    }
  }

  for (const run of input.runs) {
    corpus.push({
      type: "run",
      id: run.id,
      name: runLabel(run),
      to: "/runs/$runId",
      params: { runId: run.id },
    });
  }

  return corpus;
}

// ------------------------------------------------------------
// Pure builders — recents corpora (Req 4.2 source)
// ------------------------------------------------------------

function toRecentInput(id: string, label: string, updatedAt: string): RecentEntityInput {
  return { id, label, updatedAt };
}

/**
 * Build the Command_Palette recents corpora (Runs / Plans / Projects). The
 * palette's own recents builder (`buildRecentEntries`) applies the 5-per-group
 * cap and ordering (Req 4.2); this only supplies the raw per-group inputs.
 */
export function buildRecentsCorpora(input: {
  runs: ShellRunLike[];
  plans: ShellPlanLike[];
  projects: ShellProjectLike[];
}): RecentEntityCorpora {
  return {
    runs: input.runs.map((run) => toRecentInput(run.id, runLabel(run), run.updatedAt)),
    plans: input.plans.map((plan) => toRecentInput(plan.planId, planLabel(plan), plan.updatedAt)),
    projects: input.projects.map((project) =>
      toRecentInput(project.projectId, projectLabel(project), project.updatedAt),
    ),
  };
}

// ------------------------------------------------------------
// Pure builders — attention inputs (Req 3.2)
// ------------------------------------------------------------

/**
 * Map Runs to the {@link AttentionRunInput} shape consumed by
 * `selectAttentionRuns`. Classification itself is owned by the selector against
 * the closed attention set (Req 3.2, 3.11) — this only reshapes.
 */
export function buildAttentionInputs(runs: ShellRunLike[]): AttentionRunInput[] {
  return runs.map((run) => ({
    id: run.id,
    label: runLabel(run),
    status: run.status,
    updatedAt: run.updatedAt,
  }));
}

// ------------------------------------------------------------
// Pure builders — recent-activity items (Req 3.3)
// ------------------------------------------------------------

/**
 * Build the mixed recent-activity item list across Runs, Plans, and Projects.
 * The `selectRecentActivity` selector applies the 10-item cap and `updatedAt`
 * ordering (Req 3.3); this only reshapes each entity into a navigable item.
 */
export function buildRecentActivityItems(input: {
  runs: ShellRunLike[];
  plans: ShellPlanLike[];
  projects: ShellProjectLike[];
}): RecentActivityItem[] {
  const items: RecentActivityItem[] = [];

  for (const run of input.runs) {
    items.push({
      type: "run",
      id: run.id,
      label: runLabel(run),
      updatedAt: run.updatedAt,
      to: "/runs/$runId",
      params: { runId: run.id },
    });
  }

  for (const plan of input.plans) {
    items.push({
      type: "plan",
      id: plan.planId,
      label: planLabel(plan),
      updatedAt: plan.updatedAt,
      to: "/plans/$planId",
      params: { planId: plan.planId },
    });
  }

  for (const project of input.projects) {
    items.push({
      type: "project",
      id: project.projectId,
      label: projectLabel(project),
      updatedAt: project.updatedAt,
      to: "/projects/$projectId",
      params: { projectId: project.projectId },
    });
  }

  return items;
}

// ------------------------------------------------------------
// Pure builder — run ancestors (Req 2.7, 2.8, 2.10)
// ------------------------------------------------------------

/**
 * Compose a {@link ResolvedHierarchy} for a Run from its API_Contract-backed
 * `planContext` plus the already-loaded Plan/Project lists (used only to
 * resolve the Project ancestor's label). Every ancestor that cannot be resolved
 * from available API data is OMITTED, never fabricated (Req 2.7, 2.10). A
 * standalone Run with no plan association yields only the Run leaf (Req 2.6).
 */
export function buildRunHierarchy(
  run: RelayRun | null | undefined,
  plans: ShellPlanLike[],
  projects: ShellProjectLike[],
): ResolvedHierarchy {
  if (!run) return {};

  const hierarchy: ResolvedHierarchy = {
    run: { id: run.id, label: runLabel(run) },
  };

  const planContext = run.planContext;
  const planId = planContext?.planId;

  if (planId) {
    hierarchy.plan = {
      id: planId,
      label: planContext?.planTitle?.trim() || planId,
    };

    // Resolve the Project ancestor only when the plan is present in the loaded
    // plan list AND its project is present in the loaded project list. If
    // either lookup fails, omit the Project ancestor rather than inventing it.
    const matchedPlan = plans.find((plan) => plan.planId === planId);
    const projectId = matchedPlan?.projectId;
    if (projectId) {
      const matchedProject = projects.find((project) => project.projectId === projectId);
      if (matchedProject) {
        hierarchy.project = {
          id: matchedProject.projectId,
          label: projectLabel(matchedProject),
        };
      }
    }
  }

  // A Pass ancestor is only rendered when both plan and pass identifiers exist.
  if (planId && planContext?.passId) {
    hierarchy.pass = {
      id: planContext.passId,
      label: planContext.passName?.trim() || planContext.passId,
      sequence: planContext.passSequence,
    };
  }

  return hierarchy;
}

// ------------------------------------------------------------
// useShellData hook
// ------------------------------------------------------------

export interface UseShellDataOptions {
  /**
   * Plan IDs whose passes should be resolved into the search corpus. Combine
   * in-scope plan(s) and plans appearing in the current search results here.
   * Passes are resolved lazily via the existing `planDetailQueryOptions` /
   * `getPlan` helper — no new endpoint. Plans whose detail cannot be resolved
   * simply contribute no pass entities (omitted, never fabricated).
   */
  passPlanIds?: readonly string[];
  /** Max items requested from the Plans and Projects list endpoints. */
  limit?: number;
  /** When false, all composition queries are disabled (e.g. off-shell routes). */
  enabled?: boolean;
}

/** Loading / error state for one composed data source. */
export interface ShellDataQueryState {
  isLoading: boolean;
  isError: boolean;
  error: unknown;
  /**
   * Re-run the underlying list query. Backs the per-section retry affordance in
   * the Home_Overview load-error state (Req 3.7). Delegates to the existing
   * TanStack Query `refetch`; it introduces no new fetch behavior of its own.
   */
  refetch: () => void;
}

export interface ShellData {
  /** Scope_Switcher options: Projects + Plans (Req 2.4). */
  scopeOptions: ScopeOption[];
  /** Global_Search entity corpus across Projects/Plans/Passes/Runs (Req 5.7). */
  searchCorpus: SearchableEntity[];
  /** Command_Palette recents corpora, per entity group (Req 4.2 source). */
  recents: RecentEntityCorpora;
  /** Home_Overview attention selection (capped list + total count) (Req 3.2). */
  attention: AttentionRunSelection;
  /** Home_Overview recent-activity list (10 most recent) (Req 3.3). */
  recentActivity: RecentActivityItem[];

  /**
   * Per-source query state, so Home_Overview can load its attention section
   * (Runs) and recent-activity section independently and render distinct
   * per-section error states (Req 3.7).
   */
  runsQuery: ShellDataQueryState;
  plansQuery: ShellDataQueryState;
  projectsQuery: ShellDataQueryState;
}

function emptyPlanList(): PlanAPIReadPlan[] {
  return [];
}

/**
 * Compose the shell view models from existing query helpers. This hook is a
 * pure re-shaping layer over data already fetched by the runs/plans/projects
 * features; it owns no canonical state and starts no background sync (Req 2.9).
 */
export function useShellData(options: UseShellDataOptions = {}): ShellData {
  const { passPlanIds, limit = 100, enabled = true } = options;

  // ── List queries (existing helpers, existing endpoints) ──────────────────
  const runsResult = useQuery({ ...runsListQueryOptions, enabled });
  const plansResult = useQuery({ ...plansListQueryOptions({ limit }), enabled });
  const projectsResult = useQuery({ ...projectsListQueryOptions({ limit }), enabled });

  // ── Lazy pass resolution for in-scope / search-result plans ──────────────
  // Deduplicate the requested plan IDs so each plan detail is fetched once.
  const uniquePassPlanIds = useMemo(() => {
    return Array.from(new Set((passPlanIds ?? []).filter((id) => id.length > 0)));
  }, [passPlanIds]);

  const passResults = useQueries({
    queries: uniquePassPlanIds.map((planId) => ({
      ...planDetailQueryOptions(planId),
      enabled,
    })),
  });

  const runs: RelayRun[] = runsResult.data ?? [];
  const plans: PlanAPIReadPlan[] = plansResult.data?.plans ?? emptyPlanList();
  const projects: RelayProject[] = projectsResult.data?.projects ?? [];

  // Map resolved plan detail responses to passes keyed by plan id. Only plans
  // whose detail resolved contribute passes; unresolved plans are omitted.
  const passesByPlanId = useMemo(() => {
    const map: Record<string, ShellPassLike[]> = {};
    uniquePassPlanIds.forEach((planId, index) => {
      const detail = passResults[index]?.data as PlanDetailResponse | undefined;
      const passes: PlanAPIPass[] | undefined = detail?.passes;
      if (passes && passes.length > 0) {
        map[planId] = passes.map((pass) => ({
          passId: pass.passId,
          name: pass.name,
          sequence: pass.sequence,
        }));
      }
    });
    return map;
    // `passResults` identity changes each render; key the memo on the resolved
    // pass data via a stable signature of plan id + resolved pass ids.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [
    uniquePassPlanIds,
    passResults
      .map((r) => (r.data as PlanDetailResponse | undefined)?.passes?.map((p) => p.passId).join(","))
      .join("|"),
  ]);

  // ── Derived view models ──────────────────────────────────────────────────
  const scopeOptions = useMemo(
    () => buildScopeOptions(projects, plans),
    [projects, plans],
  );

  const searchCorpus = useMemo(
    () => buildSearchCorpus({ runs, plans, projects, passesByPlanId }),
    [runs, plans, projects, passesByPlanId],
  );

  const recents = useMemo(
    () => buildRecentsCorpora({ runs, plans, projects }),
    [runs, plans, projects],
  );

  const attention = useMemo(
    () => selectAttentionRuns(buildAttentionInputs(runs)),
    [runs],
  );

  const recentActivity = useMemo(
    () => selectRecentActivity(buildRecentActivityItems({ runs, plans, projects })),
    [runs, plans, projects],
  );

  return {
    scopeOptions,
    searchCorpus,
    recents,
    attention,
    recentActivity,
    runsQuery: {
      isLoading: runsResult.isLoading,
      isError: runsResult.isError,
      error: runsResult.error,
      refetch: () => {
        void runsResult.refetch();
      },
    },
    plansQuery: {
      isLoading: plansResult.isLoading,
      isError: plansResult.isError,
      error: plansResult.error,
      refetch: () => {
        void plansResult.refetch();
      },
    },
    projectsQuery: {
      isLoading: projectsResult.isLoading,
      isError: projectsResult.isError,
      error: projectsResult.error,
      refetch: () => {
        void projectsResult.refetch();
      },
    },
  };
}

// ------------------------------------------------------------
// useRunHierarchy hook (Breadcrumb ancestors via runDetailQueryOptions)
// ------------------------------------------------------------

export interface UseRunHierarchyResult {
  hierarchy: ResolvedHierarchy;
  isLoading: boolean;
  isError: boolean;
  error: unknown;
}

/**
 * Resolve a Run's breadcrumb hierarchy (Project → Plan → Pass → Run) by
 * composing `runDetailQueryOptions` (for the Run's `planContext`) with the
 * already-loaded Plan/Project lists (to resolve the Project ancestor label).
 * Unresolvable ancestors are omitted, never fabricated (Req 2.7, 2.10).
 */
export function useRunHierarchy(
  runId: string | undefined,
  options: { enabled?: boolean } = {},
): UseRunHierarchyResult {
  const { enabled = true } = options;
  const runEnabled = enabled && !!runId;

  const runResult = useQuery({
    ...runDetailQueryOptions(runId ?? ""),
    enabled: runEnabled,
  });
  const plansResult = useQuery({ ...plansListQueryOptions(), enabled: runEnabled });
  const projectsResult = useQuery({ ...projectsListQueryOptions(), enabled: runEnabled });

  const plans: PlanAPIReadPlan[] = plansResult.data?.plans ?? emptyPlanList();
  const projects: RelayProject[] = projectsResult.data?.projects ?? [];

  const hierarchy = useMemo(
    () => buildRunHierarchy(runResult.data ?? null, plans, projects),
    [runResult.data, plans, projects],
  );

  return {
    hierarchy,
    isLoading: runResult.isLoading,
    isError: runResult.isError,
    error: runResult.error,
  };
}
