import { useMemo } from "react";
import { useQueries, useQuery } from "@tanstack/react-query";

import {
  workflowPlanDetailQueryOptions,
  workflowPlansListQueryOptions,
  type WorkflowPlanDetail,
  type WorkflowPlanPass,
  type WorkflowPlanSummary,
} from "@/features/relay-plans";
import {
  workflowProjectsListQueryOptions,
  type WorkflowProject,
} from "@/features/relay-projects";
import {
  workflowRunDetailQueryOptions,
  workflowRunsListQueryOptions,
  type WorkflowRunSummary,
} from "@/features/relay-runs";
import type { AttentionRunInput, AttentionRunSelection } from "./attention";
import { selectAttentionRuns, selectRecentActivity } from "./attention";
import type { RecentEntityCorpora, RecentEntityInput } from "./command";
import type {
  RecentActivityItem,
  ResolvedHierarchy,
  ScopeOption,
  SearchableEntity,
} from "./types";

export interface ShellRunLike {
  id: string;
  title?: string;
  name?: string;
  status: string;
  updatedAt: string;
  projectId?: string;
  projectName?: string;
  planId?: string;
  passId?: string;
  passNumber?: number;
}

export interface ShellPlanLike {
  planId: string;
  title: string;
  updatedAt: string;
  projectId?: string;
  projectName?: string;
}

export interface ShellProjectLike {
  projectId: string;
  name: string;
  updatedAt: string;
}

export interface ShellPassLike {
  passId: string;
  name: string;
  sequence?: number;
}

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
  return name && typeof pass.sequence === "number"
    ? `${pass.sequence}. ${name}`
    : name || pass.passId;
}

function toShellRun(run: WorkflowRunSummary): ShellRunLike {
  return {
    id: run.runId,
    title: run.featureSlug,
    status: run.status,
    updatedAt: run.updatedAt,
    projectId: run.project?.projectId,
    projectName: run.project?.name,
    planId: run.planId,
    passId: run.passId,
    passNumber: run.passNumber,
  };
}

function toShellPlan(plan: WorkflowPlanSummary): ShellPlanLike {
  return {
    planId: plan.planId,
    title: plan.featureSlug,
    updatedAt: plan.updatedAt,
    projectId: plan.project.projectId,
    projectName: plan.project.name,
  };
}

function toShellProject(project: WorkflowProject): ShellProjectLike {
  return {
    projectId: project.projectId,
    name: project.name,
    updatedAt: project.updatedAt,
  };
}

export function buildScopeOptions(
  projects: ShellProjectLike[],
  plans: ShellPlanLike[],
): ScopeOption[] {
  return [
    ...projects.map((project) => ({
      kind: "project" as const,
      id: project.projectId,
      label: projectLabel(project),
      to: "/projects/$projectId",
      params: { projectId: project.projectId },
    })),
    ...plans.map((plan) => ({
      kind: "plan" as const,
      id: plan.planId,
      label: planLabel(plan),
      to: "/plans/$planId",
      params: { planId: plan.planId },
    })),
  ];
}

export interface SearchCorpusInput {
  runs: ShellRunLike[];
  plans: ShellPlanLike[];
  projects: ShellProjectLike[];
  passesByPlanId: Record<string, ShellPassLike[]>;
}

export function buildSearchCorpus(input: SearchCorpusInput): SearchableEntity[] {
  const entities: SearchableEntity[] = [];
  input.projects.forEach((project) => {
    entities.push({
      type: "project",
      id: project.projectId,
      name: projectLabel(project),
      to: "/projects/$projectId",
      params: { projectId: project.projectId },
    });
  });
  input.plans.forEach((plan) => {
    entities.push({
      type: "plan",
      id: plan.planId,
      name: planLabel(plan),
      to: "/plans/$planId",
      params: { planId: plan.planId },
    });
  });
  Object.entries(input.passesByPlanId).forEach(([planId, passes]) => {
    passes.forEach((pass) => {
      entities.push({
        type: "pass",
        id: pass.passId,
        name: passLabel(pass),
        to: "/plans/$planId/passes/$passId",
        params: { planId, passId: pass.passId },
      });
    });
  });
  input.runs.forEach((run) => {
    entities.push({
      type: "run",
      id: run.id,
      name: runLabel(run),
      to: "/runs/$runId",
      params: { runId: run.id },
    });
  });
  return entities;
}

function toRecentInput(id: string, label: string, updatedAt: string): RecentEntityInput {
  return { id, label, updatedAt };
}

export function buildRecentsCorpora(input: {
  runs: ShellRunLike[];
  plans: ShellPlanLike[];
  projects: ShellProjectLike[];
}): RecentEntityCorpora {
  return {
    runs: input.runs.map((run) => toRecentInput(run.id, runLabel(run), run.updatedAt)),
    plans: input.plans.map((plan) =>
      toRecentInput(plan.planId, planLabel(plan), plan.updatedAt),
    ),
    projects: input.projects.map((project) =>
      toRecentInput(project.projectId, projectLabel(project), project.updatedAt),
    ),
  };
}

export function buildAttentionInputs(runs: ShellRunLike[]): AttentionRunInput[] {
  return runs.map((run) => ({
    id: run.id,
    label: runLabel(run),
    status: run.status,
    updatedAt: run.updatedAt,
  }));
}

export function buildRecentActivityItems(input: {
  runs: ShellRunLike[];
  plans: ShellPlanLike[];
  projects: ShellProjectLike[];
}): RecentActivityItem[] {
  return [
    ...input.runs.map((run) => ({
      type: "run" as const,
      id: run.id,
      label: runLabel(run),
      updatedAt: run.updatedAt,
      to: "/runs/$runId",
      params: { runId: run.id },
    })),
    ...input.plans.map((plan) => ({
      type: "plan" as const,
      id: plan.planId,
      label: planLabel(plan),
      updatedAt: plan.updatedAt,
      to: "/plans/$planId",
      params: { planId: plan.planId },
    })),
    ...input.projects.map((project) => ({
      type: "project" as const,
      id: project.projectId,
      label: projectLabel(project),
      updatedAt: project.updatedAt,
      to: "/projects/$projectId",
      params: { projectId: project.projectId },
    })),
  ];
}

export function buildRunHierarchy(
  run: WorkflowRunSummary | null | undefined,
  plans: ShellPlanLike[],
  projects: ShellProjectLike[],
): ResolvedHierarchy {
  if (!run) return {};
  const hierarchy: ResolvedHierarchy = {
    run: { id: run.runId, label: run.featureSlug || run.runId },
  };
  if (run.planId) {
    const plan = plans.find((value) => value.planId === run.planId);
    hierarchy.plan = {
      id: run.planId,
      label: plan?.title || run.planId,
    };
    const projectId = run.project?.projectId || plan?.projectId;
    const project = projectId
      ? projects.find((value) => value.projectId === projectId)
      : undefined;
    if (projectId) {
      hierarchy.project = {
        id: projectId,
        label: run.project?.name || project?.name || projectId,
      };
    }
  }
  if (run.planId && run.passId) {
    hierarchy.pass = {
      id: run.passId,
      label:
        typeof run.passNumber === "number"
          ? `Pass ${run.passNumber}`
          : run.passId,
      sequence: run.passNumber,
    };
  }
  return hierarchy;
}

export interface UseShellDataOptions {
  passPlanIds?: readonly string[];
  limit?: number;
  enabled?: boolean;
}

export interface ShellDataQueryState {
  isLoading: boolean;
  isError: boolean;
  error: unknown;
  refetch: () => void;
}

export interface ShellData {
  scopeOptions: ScopeOption[];
  searchCorpus: SearchableEntity[];
  recents: RecentEntityCorpora;
  attention: AttentionRunSelection;
  recentActivity: RecentActivityItem[];
  runsQuery: ShellDataQueryState;
  plansQuery: ShellDataQueryState;
  projectsQuery: ShellDataQueryState;
}

export function useShellData(options: UseShellDataOptions = {}): ShellData {
  const { passPlanIds, limit = 100, enabled = true } = options;
  const runsResult = useQuery({
    ...workflowRunsListQueryOptions({ limit }),
    enabled,
  });
  const plansResult = useQuery({
    ...workflowPlansListQueryOptions({ limit }),
    enabled,
  });
  const projectsResult = useQuery({
    ...workflowProjectsListQueryOptions({ limit }),
    enabled,
  });
  const uniquePlanIds = useMemo(
    () => Array.from(new Set((passPlanIds ?? []).filter(Boolean))),
    [passPlanIds],
  );
  const passResults = useQueries({
    queries: uniquePlanIds.map((planId) => ({
      ...workflowPlanDetailQueryOptions(planId),
      enabled,
    })),
  });

  const runs = (runsResult.data?.runs ?? []).map(toShellRun);
  const plans = (plansResult.data?.plans ?? []).map(toShellPlan);
  const projects = (projectsResult.data?.projects ?? []).map(toShellProject);
  const passesByPlanId = useMemo(() => {
    const result: Record<string, ShellPassLike[]> = {};
    uniquePlanIds.forEach((planId, index) => {
      const detail = passResults[index]?.data as WorkflowPlanDetail | undefined;
      if (detail?.passes) {
        result[planId] = detail.passes.map((pass: WorkflowPlanPass) => ({
          passId: pass.passId,
          name: pass.name,
          sequence: pass.number,
        }));
      }
    });
    return result;
  }, [passResults, uniquePlanIds]);

  return {
    scopeOptions: buildScopeOptions(projects, plans),
    searchCorpus: buildSearchCorpus({ runs, plans, projects, passesByPlanId }),
    recents: buildRecentsCorpora({ runs, plans, projects }),
    attention: selectAttentionRuns(buildAttentionInputs(runs)),
    recentActivity: selectRecentActivity(
      buildRecentActivityItems({ runs, plans, projects }),
    ),
    runsQuery: {
      isLoading: runsResult.isLoading,
      isError: runsResult.isError,
      error: runsResult.error,
      refetch: () => void runsResult.refetch(),
    },
    plansQuery: {
      isLoading: plansResult.isLoading,
      isError: plansResult.isError,
      error: plansResult.error,
      refetch: () => void plansResult.refetch(),
    },
    projectsQuery: {
      isLoading: projectsResult.isLoading,
      isError: projectsResult.isError,
      error: projectsResult.error,
      refetch: () => void projectsResult.refetch(),
    },
  };
}

export interface UseRunHierarchyResult {
  hierarchy: ResolvedHierarchy;
  isLoading: boolean;
  isError: boolean;
  error: unknown;
}

export function useRunHierarchy(
  runId: string | undefined,
  options: { enabled?: boolean } = {},
): UseRunHierarchyResult {
  const enabled = options.enabled !== false && Boolean(runId);
  const runResult = useQuery({
    ...workflowRunDetailQueryOptions(runId ?? ""),
    enabled,
  });
  const plansResult = useQuery({
    ...workflowPlansListQueryOptions({ limit: 100 }),
    enabled,
  });
  const projectsResult = useQuery({
    ...workflowProjectsListQueryOptions({ limit: 100 }),
    enabled,
  });
  const plans = (plansResult.data?.plans ?? []).map(toShellPlan);
  const projects = (projectsResult.data?.projects ?? []).map(toShellProject);
  const hierarchy = useMemo(
    () => buildRunHierarchy(runResult.data?.run, plans, projects),
    [runResult.data, plans, projects],
  );
  return {
    hierarchy,
    isLoading:
      runResult.isLoading || plansResult.isLoading || projectsResult.isLoading,
    isError: runResult.isError || plansResult.isError || projectsResult.isError,
    error: runResult.error || plansResult.error || projectsResult.error,
  };
}
