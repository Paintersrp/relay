// ============================================================
// Relay Refactor Backlog — React Query bindings (PASS-006)
// ============================================================

import { queryOptions } from "@tanstack/react-query";

import {
  getRefactorCandidatePlacementSuggestion,
  listRefactorCandidates,
  listRefactorDiscoveryTasks,
} from "./api";
import type {
  RefactorCandidateListFilters,
  RefactorDiscoveryTaskListFilters,
} from "./types";

export const relayRefactorKeys = {
  all: ["relay-refactors"] as const,
  project: (projectId: string) =>
    [...relayRefactorKeys.all, "project", projectId] as const,
  discoveryList: (
    projectId: string,
    filters: RefactorDiscoveryTaskListFilters = {},
  ) =>
    [...relayRefactorKeys.project(projectId), "discovery", "list", filters] as const,
  discoveryDetail: (projectId: string, taskId: string) =>
    [...relayRefactorKeys.project(projectId), "discovery", "detail", taskId] as const,
  candidateList: (
    projectId: string,
    filters: RefactorCandidateListFilters = {},
  ) =>
    [...relayRefactorKeys.project(projectId), "candidate", "list", filters] as const,
  candidateDetail: (projectId: string, candidateId: string) =>
    [
      ...relayRefactorKeys.project(projectId),
      "candidate",
      "detail",
      candidateId,
    ] as const,
  placement: (projectId: string, candidateId: string, planId: string) =>
    [
      ...relayRefactorKeys.project(projectId),
      "placement",
      candidateId,
      planId,
    ] as const,
};

export function refactorDiscoveryTasksQueryOptions(
  projectId: string,
  filters: RefactorDiscoveryTaskListFilters = {},
) {
  return queryOptions({
    queryKey: relayRefactorKeys.discoveryList(projectId, filters),
    queryFn: () => listRefactorDiscoveryTasks(projectId, filters),
    staleTime: 60 * 1000,
  });
}

export function refactorCandidatesQueryOptions(
  projectId: string,
  filters: RefactorCandidateListFilters = {},
) {
  return queryOptions({
    queryKey: relayRefactorKeys.candidateList(projectId, filters),
    queryFn: () => listRefactorCandidates(projectId, filters),
    staleTime: 60 * 1000,
  });
}

/**
 * Placement suggestion is advisory and user-requested only: it never runs
 * automatically (enabled: false) and is always refetched fresh (staleTime: 0).
 */
export function refactorPlacementSuggestionQueryOptions(
  projectId: string,
  candidateId: string,
  planId: string,
) {
  return queryOptions({
    queryKey: relayRefactorKeys.placement(projectId, candidateId, planId),
    queryFn: () =>
      getRefactorCandidatePlacementSuggestion(projectId, candidateId, planId),
    enabled: false,
    staleTime: 0,
  });
}
