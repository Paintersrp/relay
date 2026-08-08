export type FeatureWorkspaceState = "open" | "closed";
export type FeatureWorkspaceRoute = "discovery" | "ready" | "blocked" | "resolved" | "closed";
export type DiscoveryTicketState = "open" | "resolved" | "rejected" | "deferred";
export type AuthorityLayerKind = "requirements" | "design" | "transition_plan";

export interface FeatureWorkspace {
  workspaceId: string;
  featureSlug: string;
  state: FeatureWorkspaceState;
  version: number;
  createdAt: string;
  updatedAt: string;
}

export interface DiscoveryTicket {
  ticketId: string;
  ticketKey: string;
  subject: string;
  state: DiscoveryTicketState;
  version: number;
  dependencies: { dependsOnTicketRowId: number; kind: "blocks" | "informs" }[];
  resolutions: DiscoveryResolution[];
  createdAt: string;
  updatedAt: string;
}

export interface DiscoveryResolution {
  resolutionId: string;
  sequence: number;
  kind: "resolved" | "rejected" | "deferred";
  artifactRowId: number | null;
  retainedArtifactRowId: number | null;
  artifactSha256: string;
  sourceClosureRowId: number | null;
  createdAt: string;
}

export interface AuthorityRevision {
  authorityRevisionId: string;
  revisionNumber: number;
  sourceClosureRowId: number | null;
  layers: AuthorityLayer[];
  createdAt: string;
}

export interface AuthorityLayer {
  kind: AuthorityLayerKind;
  sequence: number;
  artifactRowId: number | null;
  retainedArtifactRowId: number | null;
  artifactSha256: string;
  sourceClosureRowId: number | null;
  approvalRowId: number | null;
}

export interface GoverningArtifactApproval {
  approvalId: string;
  workspaceRowId: number;
  artifactRowId: number | null;
  retainedArtifactRowId: number | null;
  family: AuthorityLayerKind;
  artifactSha256: string;
  operatorConfirmationEvidence: string;
  invalidatedByApprovalRowId: number | null;
  supersededByApprovalRowId: number | null;
  createdAt: string;
}

export interface FeatureWorkspaceProject {
  projectId: string;
  name: string;
}

export interface FeatureWorkspaceDetail {
  workspace: FeatureWorkspace;
  project: FeatureWorkspaceProject;
  inputs: unknown[];
  destinations: unknown[];
  tickets: DiscoveryTicket[];
  routes: { routeId: string; sequence: number; workspaceVersion: number; state: FeatureWorkspaceRoute; createdAt: string }[];
  authorityRevisions: AuthorityRevision[];
  sourceBasis: { status: "retained" | "not_recorded"; investigationCount: number };
}

export interface CreateFeatureWorkspaceRequest { projectId: string; featureSlug: string }
export interface CreateDiscoveryTicketRequest { expectedVersion: number; ticketKey: string; subject: string; dependsOnTicketIds?: string[]; dependencyKind?: "blocks" | "informs" }
export interface ResolveDiscoveryTicketRequest { expectedVersion: number; expectedTicketVersion: number; sequence: number; kind: "resolved" | "rejected" | "deferred"; artifactRowId?: number; retainedArtifactRowId?: number; artifactSha256: string; sourceClosureRowId?: number }
export interface RouteFeatureWorkspaceRequest { expectedVersion: number; sequence: number; state: FeatureWorkspaceRoute; ticketId?: string }
export interface PublishAuthorityRequest { expectedVersion: number; sourceClosureRowId?: number; layers: { kind: AuthorityLayerKind; artifactRowId?: number; retainedArtifactRowId?: number; artifactSha256: string; sourceClosureRowId?: number; approvalRowId: number }[] }
export interface RecordAuthorityApprovalRequest { family: AuthorityLayerKind; artifactRowId?: number; retainedArtifactRowId?: number; artifactSha256: string; operatorConfirmationEvidence: string }
export interface FeatureCompletionStatus {
  workspace: FeatureWorkspace;
  gates: Array<{ name: "authority" | "tickets" | "integration" | "transitions" | "remediation" | "audit"; ready: boolean }>;
  currentDecision?: { completionDecisionId: string; authorityRevisionRowId: number; sourceClosureRowId: number; decision: string; createdAt: string };
}
export interface CompleteFeatureWorkspaceRequest {
  expectedVersion: number;
  operatorConfirmed: boolean;
}

export interface ProjectFeatureWorkspaceSummary {
  workspaceId: string;
  projectId: string;
  featureSlug: string;
  state: FeatureWorkspaceState;
  version: number;
  createdAt: string;
  updatedAt: string;
  progressionSummary: string;
  resumeSummary: string;
  blocked: boolean;
  blockedReason?: string;
  recoveryCategory?: string;
}

export interface ProjectFeatureWorkspaceListResponse {
  count: number;
  items: ProjectFeatureWorkspaceSummary[];
}

export type GuidedFeatureAction = "continue_discovery" | "close_discovery" | "author_requirements" | "author_shared_design" | "author_delivery_ticket" | "review_planning_candidate" | "approve_planning_candidate" | "promote_planning_candidate" | "continue_established_route" | "complete_feature" | "legacy_recovery" | "reopen_discovery" | "select_delivery_ticket" | "prepare_package" | "approve_package" | "launch_run" | "prepare_audit" | "record_audit_decision" | "remediate" | "prototype_execute" | "prototype_cleanup" | "prototype_qa";

export interface GuidedFrontierEntry {
  ticketId: string;
  revisionNumber: number;
  externalPriority: number;
  repoTarget: string;
  branch: string;
}

export interface GuidedTicketTransfer {
  ticketId: string;
  revisionNumber: number;
  readiness: string[];
  designBrief: string;
}
export interface GuidedPackageTransfer {
  packageId: string;
  state: string;
}
export interface GuidedRunTransfer {
  runId: string;
  status: string;
  repoTarget: string;
  branch: string;
  baseCommit: string;
  packageId: string;
}
export interface GuidedAuditTransfer {
  runId: string;
  runStatus: string;
  auditState: string;
  auditPacketId: string;
  auditedCommit: string;
}
export interface GuidedRemediationTransfer {
  state: string;
  seedIds: string[];
}
export interface GuidedCleanupTransfer {
  kind: string;
  status: string;
}
export interface GuidedQAPacketTransfer {
  packetId: string;
  status: string;
  evidence: string[];
}
export interface GuidedPrototypeTransfer {
  runId: string;
  runState: string;
  processOutcome: string;
  cleanup: GuidedCleanupTransfer[];
  qaPackets: GuidedQAPacketTransfer[];
}
export interface GuidedOperationTransfer {
  frontier: GuidedFrontierEntry[];
  members: string[];
  authorityLayers: string[];
  ticket?: GuidedTicketTransfer;
  package?: GuidedPackageTransfer;
  run?: GuidedRunTransfer;
  audit?: GuidedAuditTransfer;
  remediation?: GuidedRemediationTransfer;
  prototype?: GuidedPrototypeTransfer;
}

export interface GuidedFeatureDetail {
  workspace: FeatureWorkspace;
  project: FeatureWorkspaceProject;
  discovery: {
    state: string;
    destination: string;
    rationale: string;
    continuation: string;
    currentness: string;
    basis: string;
    reopenState: string;
  };
  authority: {
    currentRevisionNumber: number;
    revisions: Array<{ revisionNumber: number; layers: string[]; historical: boolean }>;
  };
  currentness?: { readiness: string; owner: string; blockedOperation: string; effect: string; recoveryCategory: string };
  planning: { readiness: string; status: string; recoveryCategory: string; candidateState?: string; reviewState?: string; approvalState?: string; promotionState?: string; candidateCount?: number; awaitingReview?: number; awaitingApproval?: number; awaitingPromotion?: number; promoted?: number; historicalCount?: number };
  delivery?: { frontier: GuidedFrontierEntry[]; selectionState: string; packageState: string; runState: string; auditState: string; remediationState: string };
  prototype?: { runState: string; cleanupState: string; qaState: string; evidenceState: string; processOutcome: string };
  completion: { gates: Array<{ name: string; ready: boolean }>; ready: boolean; recorded: boolean };
  ticketFrontier: { status: string; summary: string; blockers: string[]; downstream: string[] };
  downstream: { status: string; summary: string };
  prototypeQA: { status: string; summary: string; requiredEvidence: string[] };
  recovery: { blocked: boolean; summary: string; category: string; actions: string[] };
  handoff: { available: boolean; instruction: string; returnGuidance: string; transfer?: GuidedOperationTransfer };
  diagnostics: {
    history: { discoveryCurrentness: string; status: string };
    stale: { readiness: string; owner: string; blockedOperation: string; effect: string; recoveryCategory: string };
    staleItems: string[];
    historical: string[];
    discovery: {
      blockers: string[];
      restorationActions: string[];
      pendingIntegrations: string[];
      activeOperations: string[];
      routeMaterialOpen: boolean;
      requiredEvidence: string[];
    };
    delivery: string[];
    prototype: string[];
  };
  availableActions: Array<{ action: GuidedFeatureAction; primary: boolean; enabled: boolean; requiresConfirmation: boolean; blockedReason?: string; handoff?: string }>;
  primaryAction: GuidedFeatureAction;
}

export interface GuidedFeatureActionRequest {
  expectedVersion: number;
  action: GuidedFeatureAction;
  confirmation: boolean;
  destination?: string;
  /** Operator-authored replacement integrated revision content for reopen_discovery. */
  cause?: string;
  markdown?: string;
  continuation?: string;
}
