import { queryOptions } from "@tanstack/react-query";
import { getExecutionPackage, getMutationLease } from "./api";

export const packageKeys = { all: ["execution-packages"] as const, detail: (packageId: string) => [...packageKeys.all, "detail", packageId] as const, lease: (runId: string) => [...packageKeys.all, "lease", runId] as const };
export function executionPackageQueryOptions(packageId: string) { return queryOptions({ queryKey: packageKeys.detail(packageId), queryFn: () => getExecutionPackage(packageId), enabled: packageId.trim().length > 0, staleTime: 10_000 }); }
export function mutationLeaseQueryOptions(runId: string) { return queryOptions({ queryKey: packageKeys.lease(runId), queryFn: () => getMutationLease(runId), enabled: runId.trim().length > 0, staleTime: 2_000 }); }
