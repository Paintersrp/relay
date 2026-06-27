import { queryOptions } from "@tanstack/react-query";
import { getPlanSeed, getPlanSeeds, getProject, getProjects } from "./api";
import type { PlanSeedListFilters, ProjectListFilters } from "./types";

export const relayProjectKeys = {
  all: ["relay-projects"] as const,
  lists: () => [...relayProjectKeys.all, "list"] as const,
  list: (filters: ProjectListFilters = {}) =>
    [...relayProjectKeys.lists(), filters] as const,
  details: () => [...relayProjectKeys.all, "detail"] as const,
  detail: (projectId: string) =>
    [...relayProjectKeys.details(), projectId] as const,
  planSeeds: (projectId: string) =>
    [...relayProjectKeys.detail(projectId), "plan-seeds"] as const,
  planSeedList: (projectId: string, filters: PlanSeedListFilters = {}) =>
    [...relayProjectKeys.planSeeds(projectId), "list", filters] as const,
  planSeedDetail: (projectId: string, seedId: string) =>
    [...relayProjectKeys.planSeeds(projectId), "detail", seedId] as const,
};

export function projectsListQueryOptions(filters: ProjectListFilters = {}) {
  return queryOptions({
    queryKey: relayProjectKeys.list(filters),
    queryFn: () => getProjects(filters),
    staleTime: 2 * 60 * 1000,
  });
}

export function projectDetailQueryOptions(projectId: string) {
  return queryOptions({
    queryKey: relayProjectKeys.detail(projectId),
    queryFn: () => getProject(projectId),
    staleTime: 2 * 60 * 1000,
  });
}

export function planSeedsListQueryOptions(
  projectId: string,
  filters: PlanSeedListFilters = {},
) {
  return queryOptions({
    queryKey: relayProjectKeys.planSeedList(projectId, filters),
    queryFn: () => getPlanSeeds(projectId, filters),
    staleTime: 60 * 1000,
  });
}

export function planSeedDetailQueryOptions(projectId: string, seedId: string) {
  return queryOptions({
    queryKey: relayProjectKeys.planSeedDetail(projectId, seedId),
    queryFn: () => getPlanSeed(projectId, seedId),
    staleTime: 60 * 1000,
  });
}
