import { asWorkflowRecord, malformedWorkflowResponse, requestWorkflowJson, requiredWorkflowArray, requiredWorkflowInteger, requiredWorkflowString, type WorkflowHttpMethod, type WorkflowJsonRecord } from "@/features/workflow-api";
import type { ExecutionPackageDetail, ExecutionPackageRun, MutationLease, MutationLeaseResult, PackageArtifact, PackageArtifactInput, PacketAdmissionRequest } from "./types";

function record(value: unknown, method: WorkflowHttpMethod, path: string, context: string): WorkflowJsonRecord { return asWorkflowRecord(value, method, path, context); }
function nullableString(value: unknown, method: WorkflowHttpMethod, path: string, field: string): string | null { if (value === null || value === undefined) return null; if (typeof value !== "string") return malformedWorkflowResponse(method, path, `${field} must be a string or null`); return value; }
function artifact(value: unknown, method: WorkflowHttpMethod, path: string): PackageArtifact { const item = record(value, method, path, "package artifact"); return { displayName: requiredWorkflowString(item, "displayName", method, path, "package artifact"), relativePath: requiredWorkflowString(item, "relativePath", method, path, "package artifact"), sha256: requiredWorkflowString(item, "sha256", method, path, "package artifact"), sizeBytes: requiredWorkflowInteger(item, "sizeBytes", method, path, "package artifact", 0) }; }
function run(value: unknown, method: WorkflowHttpMethod, path: string): ExecutionPackageRun { const item = record(value, method, path, "package-linked Run"); return { runId: requiredWorkflowString(item, "runId", method, path, "package-linked Run"), featureSlug: requiredWorkflowString(item, "featureSlug", method, path, "package-linked Run"), repoTarget: requiredWorkflowString(item, "repoTarget", method, path, "package-linked Run"), branch: requiredWorkflowString(item, "branch", method, path, "package-linked Run"), baseCommit: requiredWorkflowString(item, "baseCommit", method, path, "package-linked Run"), status: requiredWorkflowString(item, "status", method, path, "package-linked Run") }; }
function detail(value: unknown, method: WorkflowHttpMethod, path: string): ExecutionPackageDetail {
  const item = record(record(value, method, path, "package response").package, method, path, "execution package");
  const executionSpec = artifact(item.executionSpec, method, path);
  const linked = item.run === null || item.run === undefined ? null : run(item.run, method, path);
  const packageApprovalId: string | undefined = typeof item.packageApprovalId === "string" ? item.packageApprovalId : undefined;
  return {
    packageId: requiredWorkflowString(item, "packageId", method, path, "execution package"),
    selectionRowId: requiredWorkflowInteger(item, "selectionRowId", method, path, "execution package", 1),
    workspaceRowId: requiredWorkflowInteger(item, "workspaceRowId", method, path, "execution package", 1),
    repoTarget: requiredWorkflowString(item, "repoTarget", method, path, "execution package"),
    branch: requiredWorkflowString(item, "branch", method, path, "execution package"),
    baseCommit: requiredWorkflowString(item, "baseCommit", method, path, "execution package"),
    sourceClosureRowId: requiredWorkflowInteger(item, "sourceClosureRowId", method, path, "execution package", 1),
    authorityRevisionRowId: requiredWorkflowInteger(item, "authorityRevisionRowId", method, path, "execution package", 1),
    packageSha256: requiredWorkflowString(item, "packageSha256", method, path, "execution package"),
    authoritySha256: requiredWorkflowString(item, "authoritySha256", method, path, "execution package"),
    sourceSha256: requiredWorkflowString(item, "sourceSha256", method, path, "execution package"),
    designBriefSha256: requiredWorkflowString(item, "designBriefSha256", method, path, "execution package"),
    executionSpecSha256: requiredWorkflowString(item, "executionSpecSha256", method, path, "execution package"),
    createdAt: requiredWorkflowString(item, "createdAt", method, path, "execution package", true),
    packageApprovalId,
    members: requiredWorkflowArray(item, "members", method, path, "package members").map((member) => {
      const m = record(member, method, path, "package member");
      return { selectionMemberRowId: requiredWorkflowInteger(m, "selectionMemberRowId", method, path, "package member", 1), sequence: requiredWorkflowInteger(m, "sequence", method, path, "package member", 1), revisionRowId: requiredWorkflowInteger(m, "revisionRowId", method, path, "package member", 1), memberSha256: requiredWorkflowString(m, "memberSha256", method, path, "package member") };
    }),
    approvalBindings: requiredWorkflowArray(item, "approvalBindings", method, path, "approval bindings").map((binding) => {
      const b = record(binding, method, path, "approval binding");
      return { packageMemberRowId: requiredWorkflowInteger(b, "packageMemberRowId", method, path, "approval binding", 1), approvalRowId: requiredWorkflowInteger(b, "approvalRowId", method, path, "approval binding", 1), authorityRevisionRowId: requiredWorkflowInteger(b, "authorityRevisionRowId", method, path, "approval binding", 1), sourceClosureRowId: requiredWorkflowInteger(b, "sourceClosureRowId", method, path, "approval binding", 1), approvalBasisSha256: requiredWorkflowString(b, "approvalBasisSha256", method, path, "approval binding"), createdAt: requiredWorkflowString(b, "createdAt", method, path, "approval binding") };
    }),
    ticketDesignBriefs: requiredWorkflowArray(item, "ticketDesignBriefs", method, path, "design briefs").map((value) => artifact(value, method, path)),
    executionSpec,
    run: linked
  };
}
function lease(value: unknown, method: WorkflowHttpMethod, path: string): MutationLease | null { const item = record(value, method, path, "mutation lease response"); if (item.lease === null || item.lease === undefined) return null; const result = record(item.lease, method, path, "mutation lease"); return { leaseId: requiredWorkflowString(result, "leaseId", method, path, "mutation lease"), runId: requiredWorkflowString(result, "runId", method, path, "mutation lease"), ownerRunId: requiredWorkflowString(result, "ownerRunId", method, path, "mutation lease"), repoTarget: requiredWorkflowString(result, "repoTarget", method, path, "mutation lease"), branch: requiredWorkflowString(result, "branch", method, path, "mutation lease"), state: requiredWorkflowString(result, "state", method, path, "mutation lease"), certainty: requiredWorkflowString(result, "certainty", method, path, "mutation lease"), reconciliationState: requiredWorkflowString(result, "reconciliationState", method, path, "mutation lease"), acquiredAt: requiredWorkflowString(result, "acquiredAt", method, path, "mutation lease", true), releasedAt: nullableString(result.releasedAt, method, path, "releasedAt"), reconciliationStartedAt: nullableString(result.reconciliationStartedAt, method, path, "reconciliationStartedAt"), reconciledAt: nullableString(result.reconciledAt, method, path, "reconciledAt") }; }

export async function getExecutionPackage(packageId: string): Promise<ExecutionPackageDetail> { const path = `/api/execution-packages/${encodeURIComponent(packageId)}`; return detail(await requestWorkflowJson<unknown>("GET", path), "GET", path); }
export async function prepareExecutionPackage(request: PacketAdmissionRequest & { selectionId: string; ticketDesignBriefs: PackageArtifactInput[]; executionSpec: PackageArtifactInput }): Promise<ExecutionPackageDetail> { const path = "/api/execution-packages"; return detail(await requestWorkflowJson<unknown>("POST", path, request), "POST", path); }
export async function approveExecutionPackage(packageId: string, request: PacketAdmissionRequest): Promise<ExecutionPackageRun> { const path = `/api/execution-packages/${encodeURIComponent(packageId)}/approvals`; const response = record(await requestWorkflowJson<unknown>("POST", path, request), "POST", path, "package approval"); const result = run(response.run, "POST", path); if (typeof response.packageApprovalId === "string") result.packageApprovalId = response.packageApprovalId; return result; }
export async function getMutationLease(runId: string): Promise<MutationLease | null> { const path = `/api/runs/${encodeURIComponent(runId)}/mutation-lease`; return lease(await requestWorkflowJson<unknown>("GET", path), "GET", path); }
export async function reconcileMutationLease(runId: string, request: PacketAdmissionRequest): Promise<MutationLeaseResult> { const path = `/api/runs/${encodeURIComponent(runId)}/mutation-lease/reconcile`; const value = record(await requestWorkflowJson<unknown>("POST", path, request), "POST", path, "mutation lease reconciliation"); if (typeof value.released !== "boolean") return malformedWorkflowResponse("POST", path, "released must be a boolean"); return { released: value.released, lease: lease(value, "POST", path) }; }
