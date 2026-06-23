export type ProjectStatus = "active" | "archived" | string;
export type RepositoryRole = "primary" | "reference" | "contracts" | "docs" | string;

export interface RelayProjectRepository {
  repoId: string;
  role: RepositoryRole;
  localPath: string;
  remoteLabel: string;
  remoteUrl: string;
  defaultBranch: string;
  allowedRoots: string[];
  ignoredGlobs: string[];
  maxFileSizeBytes: number;
  includeUntracked: boolean;
  enabled: boolean;
  createdAt: string;
  updatedAt: string;
}

export interface RelayProject {
  projectId: string;
  name: string;
  description: string;
  status: ProjectStatus;
  defaultRepositoryId: string;
  createdAt: string;
  updatedAt: string;
  repositories?: RelayProjectRepository[];
}

export interface ProjectAPIRequest {
  project_id: string;
  name: string;
  description?: string;
  status?: ProjectStatus;
  default_repository_id?: string;
}

export interface ProjectRepositoryAPIRequest {
  repo_id: string;
  role: RepositoryRole;
  local_path: string;
  remote_label?: string;
  remote_url?: string;
  default_branch?: string;
  allowed_roots: string[];
  ignored_globs: string[];
  max_file_size_bytes?: number;
  include_untracked: boolean;
  enabled: boolean;
}

export interface ProjectRepositoryEnabledAPIRequest {
  enabled: boolean;
}

export interface ProjectValidationIssue {
  field: string;
  code: string;
  message: string;
}

export interface ProjectAPIResponse {
  success: boolean;
  count?: number;
  projects?: RelayProject[];
  project?: RelayProject;
  repository?: RelayProjectRepository;
  repositories?: RelayProjectRepository[];
  validation?: ProjectValidationIssue[];
}

export interface ProjectListFilters {
  limit?: number;
}

export interface ProjectListResponse {
  success: boolean;
  count: number;
  projects: RelayProject[];
}

export interface ProjectDetailResponse {
  success: boolean;
  project: RelayProject;
}

export interface ProjectRepositoryMutationResponse {
  success: boolean;
  repository: RelayProjectRepository;
}
