import { queryOptions } from "@tanstack/react-query";

import { getPlan, getPlanPass, getPlans, getNextPassWork, getNextAuditWork } from "./api";
import type { PlanListFilters, NextAuditWorkFilters } from "./types";

export const relayPlanKeys = {
  all: ["relay-plans"] as const,
  list: (filters: PlanListFilters = {}) =>
    [...relayPlanKeys.all, "list", filters] as const,
  detail: (planId: string) =>
    [...relayPlanKeys.all, "detail", planId] as const,
  pass: (planId: string, passId: string) =>
    [...relayPlanKeys.all, "detail", planId, "pass", passId] as const,
  nextPassWork: (projectId: string, planId: string) =>
    [...relayPlanKeys.all, "detail", planId, "next-pass-work", projectId] as const,
  nextAuditWork: (projectId: string, planId: string, filters: NextAuditWorkFilters = {}) =>
    [...relayPlanKeys.all, "detail", planId, "next-audit-work", projectId, filters] as const,
};

export function plansListQueryOptions(filters: PlanListFilters = {}) {
  return queryOptions({
    queryKey: relayPlanKeys.list(filters),
    queryFn: () => getPlans(filters),
    staleTime: 2 * 60 * 1000,
  });
}

export function planDetailQueryOptions(planId: string) {
  return queryOptions({
    queryKey: relayPlanKeys.detail(planId),
    queryFn: () => getPlan(planId),
    staleTime: 2 * 60 * 1000,
  });
}

export function planPassDetailQueryOptions(planId: string, passId: string) {
  return queryOptions({
    queryKey: relayPlanKeys.pass(planId, passId),
    queryFn: () => getPlanPass(planId, passId),
    staleTime: 2 * 60 * 1000,
  });
}

export function nextPassWorkQueryOptions(projectId: string, planId: string) {
  return queryOptions({
    queryKey: relayPlanKeys.nextPassWork(projectId, planId),
    queryFn: () => getNextPassWork(projectId, planId),
    enabled: false, // Manual refetch only
    staleTime: 0,
  });
}

export function nextAuditWorkQueryOptions(
  projectId: string,
  planId: string,
  filters: NextAuditWorkFilters = {},
) {
  return queryOptions({
    queryKey: relayPlanKeys.nextAuditWork(projectId, planId, filters),
    queryFn: () => getNextAuditWork(projectId, planId, filters),
    enabled: false, // Manual refetch only
    staleTime: 0,
  });
}
