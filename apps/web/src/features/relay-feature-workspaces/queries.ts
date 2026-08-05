import { queryOptions } from "@tanstack/react-query";
import { getFeatureCompletionStatus, getFeatureWorkspace, listProjectFeatureWorkspaces } from "./api";

export const featureWorkspaceKeys = { all: ["feature-workspaces"] as const, detail: (workspaceId: string) => [...featureWorkspaceKeys.all, workspaceId] as const, completion: (workspaceId: string) => [...featureWorkspaceKeys.all, workspaceId, "completion"] as const, byProject: (projectId: string) => [...featureWorkspaceKeys.all, "by-project", projectId] as const };
export function featureWorkspaceDetailQueryOptions(workspaceId: string) { return queryOptions({ queryKey: featureWorkspaceKeys.detail(workspaceId), queryFn: () => getFeatureWorkspace(workspaceId), staleTime: 30_000 }); }
export function featureWorkspaceCompletionQueryOptions(workspaceId: string) { return queryOptions({ queryKey: featureWorkspaceKeys.completion(workspaceId), queryFn: () => getFeatureCompletionStatus(workspaceId), staleTime: 5_000 }); }
export function projectFeatureWorkspacesQueryOptions(projectId: string) { return queryOptions({ queryKey: featureWorkspaceKeys.byProject(projectId), queryFn: () => listProjectFeatureWorkspaces(projectId), staleTime: 30_000 }); }
