export type WorkflowProjectStatus = "active" | "archived";
export type WorkflowProjectNoteStatus = "open" | "done";

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
