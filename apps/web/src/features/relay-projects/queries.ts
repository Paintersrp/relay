import { queryOptions } from "@tanstack/react-query";
import { getProject, getProjects } from "./api";
import type { ProjectListFilters } from "./types";

export const relayProjectKeys = {
  all: ["relay-projects"] as const,
  lists: () => [...relayProjectKeys.all, "list"] as const,
  list: (filters: ProjectListFilters = {}) =>
    [...relayProjectKeys.lists(), filters] as const,
  details: () => [...relayProjectKeys.all, "detail"] as const,
  detail: (projectId: string) =>
    [...relayProjectKeys.details(), projectId] as const,
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
