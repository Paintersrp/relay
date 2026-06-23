import { API_BASE_URL, RelayApiError } from "@/features/relay-runs";
import type { RelayApiErrorShape } from "@/features/relay-runs/types";
import type {
  ProjectAPIRequest,
  ProjectDetailResponse,
  ProjectListFilters,
  ProjectListResponse,
  ProjectRepositoryAPIRequest,
  ProjectRepositoryMutationResponse,
  RelayProject,
  RelayProjectRepository,
} from "./types";

function normalizeProjectRepository(repo: any): RelayProjectRepository {
  return {
    repoId: repo?.repoId ?? "",
    role: repo?.role ?? "primary",
    localPath: repo?.localPath ?? "",
    remoteLabel: repo?.remoteLabel ?? "",
    remoteUrl: repo?.remoteUrl ?? "",
    defaultBranch: repo?.defaultBranch ?? "main",
    allowedRoots: Array.isArray(repo?.allowedRoots) ? repo.allowedRoots : [],
    ignoredGlobs: Array.isArray(repo?.ignoredGlobs) ? repo.ignoredGlobs : [],
    maxFileSizeBytes: typeof repo?.maxFileSizeBytes === "number" ? repo.maxFileSizeBytes : 262144,
    includeUntracked: !!repo?.includeUntracked,
    enabled: !!repo?.enabled,
    createdAt: repo?.createdAt ?? "",
    updatedAt: repo?.updatedAt ?? "",
  };
}

function normalizeProject(project: any): RelayProject {
  return {
    projectId: project?.projectId ?? "",
    name: project?.name ?? "",
    description: project?.description ?? "",
    status: project?.status ?? "active",
    defaultRepositoryId: project?.defaultRepositoryId ?? "",
    createdAt: project?.createdAt ?? "",
    updatedAt: project?.updatedAt ?? "",
    repositories: Array.isArray(project?.repositories)
      ? project.repositories.map(normalizeProjectRepository)
      : [],
  };
}

async function getProjectJson<T>(path: string): Promise<T> {
  const url = `${API_BASE_URL}${path}`;

  try {
    const res = await fetch(url, {
      headers: { Accept: "application/json" },
    });

    if (!res.ok) {
      throw new RelayApiError(
        `Failed to fetch from GET ${path} (status: ${res.status})`,
        res.status,
        path,
        "GET",
      );
    }

    const text = await res.text();
    try {
      return JSON.parse(text) as T;
    } catch (err: any) {
      throw new RelayApiError(
        `Malformed JSON response from GET ${path}: ${err.message}`,
        res.status,
        path,
        "GET",
      );
    }
  } catch (err: any) {
    if (err instanceof RelayApiError) throw err;

    throw new RelayApiError(
      `Network error fetching from GET ${path}: ${err.message}`,
      503,
      path,
      "GET",
    );
  }
}

async function postProjectJson<TReq, TRes>(path: string, body: TReq): Promise<TRes> {
  const url = `${API_BASE_URL}${path}`;

  try {
    const res = await fetch(url, {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        Accept: "application/json",
      },
      body: JSON.stringify(body),
    });

    if (!res.ok) {
      let errorShape: RelayApiErrorShape | undefined;
      try {
        const text = await res.text();
        errorShape = JSON.parse(text);
      } catch {
        // Ignore malformed error response body.
      }

      throw new RelayApiError(
        `Mutation failed on POST ${path} (status: ${res.status})`,
        res.status,
        path,
        "POST",
        errorShape,
      );
    }

    const text = await res.text();
    try {
      return JSON.parse(text) as TRes;
    } catch (err: any) {
      throw new RelayApiError(
        `Malformed JSON response from POST ${path}: ${err.message}`,
        res.status,
        path,
        "POST",
      );
    }
  } catch (err: any) {
    if (err instanceof RelayApiError) throw err;

    throw new RelayApiError(
      `Daemon unavailable or connection refused on POST ${path}: ${err.message}`,
      503,
      path,
      "POST",
    );
  }
}

export async function getProjects(
  filters: ProjectListFilters = {},
): Promise<ProjectListResponse> {
  const params = new URLSearchParams();
  if (typeof filters.limit === "number") {
    params.set("limit", String(filters.limit));
  }
  const query = params.toString();
  const response = await getProjectJson<any>(`/api/projects${query ? `?${query}` : ""}`);

  return {
    success: !!response?.success,
    count: response?.count ?? 0,
    projects: Array.isArray(response?.projects)
      ? response.projects.map(normalizeProject)
      : [],
  };
}

export async function getProject(projectId: string): Promise<ProjectDetailResponse> {
  const response = await getProjectJson<any>(
    `/api/projects/${encodeURIComponent(projectId)}`,
  );

  return {
    success: !!response?.success,
    project: normalizeProject(response?.project),
  };
}

export async function createProject(
  request: ProjectAPIRequest,
): Promise<ProjectDetailResponse> {
  const response = await postProjectJson<ProjectAPIRequest, any>(
    "/api/projects",
    request,
  );

  return {
    success: !!response?.success,
    project: normalizeProject(response?.project),
  };
}

export async function upsertProjectRepository(
  projectId: string,
  request: ProjectRepositoryAPIRequest,
): Promise<ProjectRepositoryMutationResponse> {
  const response = await postProjectJson<ProjectRepositoryAPIRequest, any>(
    `/api/projects/${encodeURIComponent(projectId)}/repositories`,
    request,
  );

  return {
    success: !!response?.success,
    repository: normalizeProjectRepository(response?.repository),
  };
}

export async function updateProjectRepository(
  projectId: string,
  repoId: string,
  request: ProjectRepositoryAPIRequest,
): Promise<ProjectRepositoryMutationResponse> {
  const response = await postProjectJson<ProjectRepositoryAPIRequest, any>(
    `/api/projects/${encodeURIComponent(projectId)}/repositories/${encodeURIComponent(repoId)}/update`,
    request,
  );

  return {
    success: !!response?.success,
    repository: normalizeProjectRepository(response?.repository),
  };
}

export async function setProjectRepositoryEnabled(
  projectId: string,
  repoId: string,
  enabled: boolean,
): Promise<ProjectRepositoryMutationResponse> {
  const response = await postProjectJson<{ enabled: boolean }, any>(
    `/api/projects/${encodeURIComponent(projectId)}/repositories/${encodeURIComponent(repoId)}/set-enabled`,
    { enabled },
  );

  return {
    success: !!response?.success,
    repository: normalizeProjectRepository(response?.repository),
  };
}
