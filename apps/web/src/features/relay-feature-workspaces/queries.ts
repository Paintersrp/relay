import { queryOptions } from "@tanstack/react-query";
import { getFeatureWorkspace } from "./api";

export const featureWorkspaceKeys = { all: ["feature-workspaces"] as const, detail: (workspaceId: string) => [...featureWorkspaceKeys.all, workspaceId] as const };
export function featureWorkspaceDetailQueryOptions(workspaceId: string) { return queryOptions({ queryKey: featureWorkspaceKeys.detail(workspaceId), queryFn: () => getFeatureWorkspace(workspaceId), staleTime: 30_000 }); }
