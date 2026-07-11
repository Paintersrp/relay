export type WorkflowProjectStatus = "active" | "archived";
export type WorkflowProjectNoteStatus = "open" | "done";
export type WorkflowRepositoryInspectionState =
  | "ready"
  | "needs_remote_selection"
  | "needs_target_override"
  | "conflict";
export type WorkflowRepositoryRegistrationDisposition = "create" | "reuse";
export type WorkflowRepositoryRegistrationOutcome = "created" | "reused";
export type WorkflowRepositoryTargetSource =
  | "remote_basename"
  | "operator_override";
export type WorkflowRepositoryConflictKind = "target" | "path";
export type WorkflowRepositoryTargetOverrideReason =
  | "no_usable_remote"
  | "unsupported_remote";

export interface WorkflowProject {
  projectId: string;
  name: string;
  description: string;
  status: WorkflowProjectStatus;
  createdAt: string;
  updatedAt: string;
}

export interface WorkflowProjectRepositoryReference {
  repoTarget: string;
  createdAt: string;
}

export interface WorkflowProjectNote {
  noteId: string;
  title: string;
  body: string;
  status: WorkflowProjectNoteStatus;
  createdAt: string;
  updatedAt: string;
}

export interface WorkflowProjectPlanSummary {
  planId: string;
  featureSlug: string;
  status: string;
  createdAt: string;
  updatedAt: string;
}

export interface WorkflowProjectDetail {
  project: WorkflowProject;
  repositories: WorkflowProjectRepositoryReference[];
  notes: WorkflowProjectNote[];
  plans: WorkflowProjectPlanSummary[];
}

export interface WorkflowRepositoryTarget {
  repoTarget: string;
  localPath: string;
  createdAt: string;
  updatedAt: string;
}

export interface WorkflowRepositoryRemoteCandidate {
  name: string;
  url: string;
  suggestedRepoTarget?: string;
}

interface WorkflowRepositoryInspectionBase {
  selectedPath: string;
  resolvedLocalPath: string;
  remotes: WorkflowRepositoryRemoteCandidate[];
  notices: string[];
}

export interface WorkflowRepositoryReadyInspection
  extends WorkflowRepositoryInspectionBase {
  state: "ready";
  selectedRemote?: WorkflowRepositoryRemoteCandidate;
  suggestedRepoTarget?: string;
  repoTarget: string;
  repoTargetSource: WorkflowRepositoryTargetSource;
  registrationDisposition: WorkflowRepositoryRegistrationDisposition;
  existingRepository?: WorkflowRepositoryTarget;
  confirmationHash: string;
}

export interface WorkflowRepositoryRemoteSelectionInspection
  extends WorkflowRepositoryInspectionBase {
  state: "needs_remote_selection";
}

export interface WorkflowRepositoryTargetOverrideInspection
  extends WorkflowRepositoryInspectionBase {
  state: "needs_target_override";
  selectedRemote?: WorkflowRepositoryRemoteCandidate;
  suggestedRepoTarget?: string;
  targetOverrideReason: WorkflowRepositoryTargetOverrideReason;
}

export interface WorkflowRepositoryConflictInspection
  extends WorkflowRepositoryInspectionBase {
  state: "conflict";
  selectedRemote?: WorkflowRepositoryRemoteCandidate;
  suggestedRepoTarget?: string;
  repoTarget: string;
  repoTargetSource: WorkflowRepositoryTargetSource;
  existingRepository: WorkflowRepositoryTarget;
  conflictKind: WorkflowRepositoryConflictKind;
}

export type WorkflowRepositoryInspection =
  | WorkflowRepositoryReadyInspection
  | WorkflowRepositoryRemoteSelectionInspection
  | WorkflowRepositoryTargetOverrideInspection
  | WorkflowRepositoryConflictInspection;

export interface InspectWorkflowRepositoryRequest {
  localPath: string;
  remoteName?: string;
  repoTargetOverride?: string;
}

export interface ConfirmWorkflowRepositoryRequest {
  localPath: string;
  remoteName?: string;
  repoTargetOverride?: string;
  expectedConfirmationHash: string;
}

export interface WorkflowRepositoryRegistrationResult {
  outcome: WorkflowRepositoryRegistrationOutcome;
  repository: WorkflowRepositoryTarget;
}

export interface WorkflowProjectListFilters {
  status?: WorkflowProjectStatus;
  limit?: number;
}

export interface WorkflowProjectDetailLimits {
  repositoryLimit?: number;
  noteLimit?: number;
  planLimit?: number;
}

export interface WorkflowProjectListResponse {
  count: number;
  projects: WorkflowProject[];
}

export interface WorkflowRepositoryTargetListResponse {
  count: number;
  repositories: WorkflowRepositoryTarget[];
}

export interface CreateWorkflowProjectRequest {
  name: string;
  description: string;
}

export interface UpdateWorkflowProjectRequest {
  name?: string;
  description?: string;
}

export interface CreateWorkflowProjectNoteRequest {
  title: string;
  body: string;
}

export interface UpdateWorkflowProjectNoteRequest {
  title?: string;
  body?: string;
  status?: WorkflowProjectNoteStatus;
}
