import { queryOptions } from "@tanstack/react-query";
import {
  getWorkflowPlan,
  getWorkflowPlanPass,
  listWorkflowPlans,
} from "./api";
import type { WorkflowPlanListFilters } from "./types";

export const workflowPlanKeys = {
  all: ["workflow-plans"] as const,
  lists: () => [...workflowPlanKeys.all, "list"] as const,
  list: (filters: WorkflowPlanListFilters = {}) =>
    [...workflowPlanKeys.lists(), filters] as const,
  details: () => [...workflowPlanKeys.all, "detail"] as const,
  detail: (planId: string) => [...workflowPlanKeys.details(), planId] as const,
  pass: (planId: string, passId: string) =>
    [...workflowPlanKeys.detail(planId), "pass", passId] as const,
};

export function workflowPlansListQueryOptions(
  filters: WorkflowPlanListFilters = {},
) {
  return queryOptions({
    queryKey: workflowPlanKeys.list(filters),
    queryFn: () => listWorkflowPlans(filters),
    staleTime: 60 * 1000,
  });
}

export function workflowPlanDetailQueryOptions(planId: string) {
  return queryOptions({
    queryKey: workflowPlanKeys.detail(planId),
    queryFn: () => getWorkflowPlan(planId),
    staleTime: 30 * 1000,
  });
}

export function workflowPlanPassQueryOptions(planId: string, passId: string) {
  return queryOptions({
    queryKey: workflowPlanKeys.pass(planId, passId),
    queryFn: () => getWorkflowPlanPass(planId, passId),
    staleTime: 30 * 1000,
  });
}
