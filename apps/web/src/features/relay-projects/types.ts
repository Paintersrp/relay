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

export type PlanSeedStatus = "captured" | "planned" | "deferred" | "rejected";
export type PlanSeedSourceType = "manual" | "chat" | "mcp";

export interface RelayPlanSeed {
  seedId: string;
  projectId: string;
  title: string;
  quickContext: string;
  constraints: string[];
  nonGoals: string[];
  tags: string[];
  priority: string;
  status: PlanSeedStatus | string;
  sourceType: PlanSeedSourceType | string;
  sourceLabel: string;
  sourceRefId: string;
  planAttemptId: string;
  managedPlanId: string;
  plannedAt: string;
  deferReason: string;
  rejectReason: string;
  createdAt: string;
  updatedAt: string;
}

export interface PlanSeedAPIRequest {
  title: string;
  quick_context: string;
  priority?: string;
  constraints?: string[];
  non_goals?: string[];
  tags?: string[];
  source_label?: string;
}

export interface PlanSeedUpdateAPIRequest {
  title?: string;
  quick_context?: string;
  priority?: string;
  constraints?: string[];
  non_goals?: string[];
  tags?: string[];
}

export interface PlanSeedLifecycleAPIRequest {
  defer_reason?: string;
  reject_reason?: string;
}

export interface PlanSeedListFilters {
  status?: PlanSeedStatus;
  limit?: number;
}

export interface PlanSeedListResponse {
  success: boolean;
  count: number;
  seeds: RelayPlanSeed[];
}

export interface PlanSeedDetailResponse {
  success: boolean;
  seed: RelayPlanSeed;
}

export interface PlanSeedMutationResponse {
  success: boolean;
  seed: RelayPlanSeed;
}
