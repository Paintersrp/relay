import { queryOptions } from "@tanstack/react-query";
import {
  getWorkflowProject,
  listWorkflowProjects,
  listWorkflowRepositoryTargets,
} from "./canonical-api";
import type {
  WorkflowProjectDetailLimits,
  WorkflowProjectListFilters,
} from "./canonical-types";

export const workflowProjectKeys = {
  all: ["workflow-projects"] as const,
  lists: () => [...workflowProjectKeys.all, "list"] as const,
  list: (filters: WorkflowProjectListFilters = {}) =>
    [...workflowProjectKeys.lists(), filters] as const,
  details: () => [...workflowProjectKeys.all, "detail"] as const,
  detail: (projectId: string, limits: WorkflowProjectDetailLimits = {}) =>
    [...workflowProjectKeys.details(), projectId, limits] as const,
  repositories: () => [...workflowProjectKeys.all, "repository-targets"] as const,
};

export function workflowProjectsListQueryOptions(
  filters: WorkflowProjectListFilters = {},
) {
  return queryOptions({
    queryKey: workflowProjectKeys.list(filters),
    queryFn: () => listWorkflowProjects(filters),
    staleTime: 2 * 60 * 1000,
  });
}

export function workflowProjectDetailQueryOptions(
  projectId: string,
  limits: WorkflowProjectDetailLimits = {},
) {
  return queryOptions({
    queryKey: workflowProjectKeys.detail(projectId, limits),
    queryFn: () => getWorkflowProject(projectId, limits),
    staleTime: 60 * 1000,
  });
}

export function workflowRepositoryTargetsQueryOptions() {
  return queryOptions({
    queryKey: workflowProjectKeys.repositories(),
    queryFn: listWorkflowRepositoryTargets,
    staleTime: 2 * 60 * 1000,
  });
}
