import { queryOptions } from "@tanstack/react-query";
import {
  getWorkflowArtifactContent,
  getWorkflowAttempt,
  getWorkflowAuditStatus,
  getWorkflowRun,
  getWorkflowSpecification,
  listWorkflowRuns,
} from "./canonical-api";
import type { WorkflowRunListFilters } from "./canonical-types";

export const workflowRunKeys = {
  all: ["workflow-runs"] as const,
  lists: () => [...workflowRunKeys.all, "list"] as const,
  list: (filters: WorkflowRunListFilters = {}) =>
    [...workflowRunKeys.lists(), filters] as const,
  details: () => [...workflowRunKeys.all, "detail"] as const,
  detail: (runId: string) => [...workflowRunKeys.details(), runId] as const,
  specification: (runId: string) =>
    [...workflowRunKeys.detail(runId), "specification"] as const,
  attempt: (runId: string, attemptId: string) =>
    [...workflowRunKeys.detail(runId), "attempt", attemptId] as const,
  audit: (runId: string) =>
    [...workflowRunKeys.detail(runId), "audit"] as const,
  artifact: (contentUrl: string) =>
    [...workflowRunKeys.all, "artifact", contentUrl] as const,
};

export function workflowRunsListQueryOptions(
  filters: WorkflowRunListFilters = {},
) {
  return queryOptions({
    queryKey: workflowRunKeys.list(filters),
    queryFn: () => listWorkflowRuns(filters),
    staleTime: 30 * 1000,
  });
}

export function workflowRunDetailQueryOptions(runId: string) {
  return queryOptions({
    queryKey: workflowRunKeys.detail(runId),
    queryFn: () => getWorkflowRun(runId),
    staleTime: 10 * 1000,
  });
}

export function workflowSpecificationQueryOptions(runId: string) {
  return queryOptions({
    queryKey: workflowRunKeys.specification(runId),
    queryFn: () => getWorkflowSpecification(runId),
    staleTime: 30 * 1000,
  });
}

export function workflowAttemptQueryOptions(runId: string, attemptId: string) {
  return queryOptions({
    queryKey: workflowRunKeys.attempt(runId, attemptId),
    queryFn: () => getWorkflowAttempt(runId, attemptId),
    staleTime: 2 * 1000,
  });
}

export function workflowAuditStatusQueryOptions(runId: string) {
  return queryOptions({
    queryKey: workflowRunKeys.audit(runId),
    queryFn: () => getWorkflowAuditStatus(runId),
    staleTime: 5 * 1000,
  });
}

export function workflowArtifactContentQueryOptions(contentUrl: string) {
  return queryOptions({
    queryKey: workflowRunKeys.artifact(contentUrl),
    queryFn: () => getWorkflowArtifactContent(contentUrl),
    staleTime: Number.POSITIVE_INFINITY,
  });
}
