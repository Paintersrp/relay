export type WorkflowProjectStatus = "active" | "archived";
export type WorkflowPlanStatus = "active" | "completed";
export type WorkflowPlanPassStatus = "planned" | "in_progress" | "completed";
export type WorkflowRunStage = "specification" | "execute" | "audit";

export interface WorkflowProjectReference {
  projectId: string;
  name: string;
  status: WorkflowProjectStatus;
}

export interface WorkflowArtifactReference {
  artifactId: string;
  ownerType: string;
  kind: string;
  mediaType: string;
  sha256: string;
  sizeBytes: number;
  createdAt: string;
  contentUrl: string;
}

export interface WorkflowPlanRunReference {
  runId: string;
  status: string;
  stage: WorkflowRunStage;
  branch: string;
  baseCommit: string;
  remediatesRunId?: string;
  createdAt: string;
  updatedAt: string;
}

export interface WorkflowPlanPass {
  passId: string;
  number: number;
  name: string;
  repoTarget: string;
  status: WorkflowPlanPassStatus;
  dependsOn: string[];
  createdAt: string;
  updatedAt: string;
  startedAt?: string;
  completedAt?: string;
  runs: WorkflowPlanRunReference[];
}

export interface WorkflowPlanRepository {
  repoTarget: string;
  branch: string;
  planningBaseCommit: string;
  sequence: number;
}

export interface WorkflowPlanSummary {
  planId: string;
  project: WorkflowProjectReference;
  featureSlug: string;
  status: WorkflowPlanStatus;
  canonicalSha256: string;
  createdAt: string;
  updatedAt: string;
  completedAt?: string;
  passCount: number;
  completedPassCount: number;
  inProgressPassCount: number;
  plannedPassCount: number;
  currentPassId?: string;
}

export interface WorkflowPlanDetail {
  plan: WorkflowPlanSummary;
  repositories: WorkflowPlanRepository[];
  passes: WorkflowPlanPass[];
  artifacts: WorkflowArtifactReference[];
}

export interface WorkflowPlanListFilters {
  status?: WorkflowPlanStatus;
  projectId?: string;
  limit?: number;
}

export interface WorkflowPlanListResponse {
  count: number;
  plans: WorkflowPlanSummary[];
}

export interface WorkflowCanonicalValidation {
  ok: boolean;
  status: string;
  kind: string;
  sha256: string;
  diagnostics: Record<string, unknown>[];
  notices: Record<string, unknown>[];
}

export interface SubmitWorkflowPlanRequest {
  projectId: string;
  fileName: string;
  canonicalContent: string;
  expectedSha256: string;
}

export interface SubmitWorkflowPlanResponse {
  plan: {
    planId: string;
    featureSlug: string;
    status: WorkflowPlanStatus;
    canonicalSha256: string;
    project: WorkflowProjectReference;
    createdAt: string;
    updatedAt: string;
  };
  passes: Array<{
    passId: string;
    number: number;
    name: string;
    repoTarget: string;
    status: WorkflowPlanPassStatus;
  }>;
  artifacts: WorkflowArtifactReference[];
}

export interface MoveWorkflowPlanRequest {
  projectId: string;
}
