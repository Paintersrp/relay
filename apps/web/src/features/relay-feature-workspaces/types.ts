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
}

export interface FeatureWorkspaceDetail {
  workspace: FeatureWorkspace;
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
export interface PublishAuthorityRequest { expectedVersion: number; sourceClosureRowId?: number; layers: { kind: AuthorityLayerKind; artifactRowId?: number; retainedArtifactRowId?: number; artifactSha256: string; sourceClosureRowId?: number }[] }
