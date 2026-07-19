export interface PacketAdmissionRequest {
  packetId: string;
  operationId: string;
  requiredDependencies: { class: string; key: string }[];
}

export interface PackageArtifactInput {
  displayName: string;
  expectedSha256: string;
  bytesBase64: string;
}

export interface PackageArtifact {
  displayName: string;
  relativePath: string;
  sha256: string;
  sizeBytes: number;
}

export interface ExecutionPackageRun {
  runId: string;
  featureSlug: string;
  repoTarget: string;
  branch: string;
  baseCommit: string;
  status: string;
}

export interface ExecutionPackageDetail {
  packageId: string;
  selectionRowId: number;
  workspaceRowId: number;
  repoTarget: string;
  branch: string;
  baseCommit: string;
  sourceClosureRowId: number;
  authorityRevisionRowId: number;
  packageSha256: string;
  authoritySha256: string;
  sourceSha256: string;
  designBriefSha256: string;
  executionSpecSha256: string;
  createdAt: string;
  members: { selectionMemberRowId: number; sequence: number; revisionRowId: number; memberSha256: string }[];
  approvalBindings: { packageMemberRowId: number; approvalRowId: number; authorityRevisionRowId: number; sourceClosureRowId: number; approvalBasisSha256: string; createdAt: string }[];
  ticketDesignBriefs: PackageArtifact[];
  executionSpec: PackageArtifact;
  run: ExecutionPackageRun | null;
}

export interface MutationLease {
  leaseId: string;
  runId: string;
  ownerRunId: string;
  repoTarget: string;
  branch: string;
  state: string;
  certainty: string;
  reconciliationState: string;
  acquiredAt: string;
  releasedAt: string | null;
  reconciliationStartedAt: string | null;
  reconciledAt: string | null;
}

export interface MutationLeaseResult {
  released: boolean;
  lease: MutationLease | null;
}
