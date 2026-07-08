import { API_BASE_URL, RelayApiError, type RelayApiErrorShape } from "../workflow-canonical-api";
import type {
  CreateWorkflowProjectNoteRequest,
  CreateWorkflowProjectRequest,
  UpdateWorkflowProjectNoteRequest,
  UpdateWorkflowProjectRequest,
  WorkflowProject,
  WorkflowProjectDetail,
  WorkflowProjectDetailLimits,
  WorkflowProjectListFilters,
  WorkflowProjectListResponse,
  WorkflowProjectNote,
  WorkflowProjectNoteStatus,
  WorkflowProjectPlanSummary,
  WorkflowProjectRepositoryReference,
  WorkflowProjectStatus,
  WorkflowRepositoryTarget,
  WorkflowRepositoryTargetListResponse,
} from "./canonical-types";

type JsonRecord = Record<string, unknown>;
type HttpMethod = "GET" | "POST" | "PATCH" | "PUT" | "DELETE";

function malformedWorkflowResponse(
  method: HttpMethod,
  path: string,
  detail: string,
): never {
  throw new RelayApiError(
    `Malformed JSON response from ${method} ${path}: ${detail}`,
    502,
    path,
    method,
  );
}

function requiredRecord(
  value: unknown,
  method: HttpMethod,
  path: string,
  context: string,
): JsonRecord {
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    return malformedWorkflowResponse(
      method,
      path,
      `${context} must be an object; received ${String(value)}`,
    );
  }
  return value as JsonRecord;
}

function requiredArray(
  record: JsonRecord,
  field: string,
  method: HttpMethod,
  path: string,
  context: string,
): unknown[] {
  const value = record[field];
  if (!Array.isArray(value)) {
    return malformedWorkflowResponse(
      method,
      path,
      `${context}.${field} must be an array; received ${String(value)}`,
    );
  }
  return value;
}

function requiredCount(
  record: JsonRecord,
  method: HttpMethod,
  path: string,
  context: string,
): number {
  const value = record.count;
  if (!Number.isInteger(value) || (value as number) < 0) {
    return malformedWorkflowResponse(
      method,
      path,
      `${context}.count must be a non-negative integer; received ${String(value)}`,
    );
  }
  return value as number;
}

function requiredString(
  record: JsonRecord,
  field: string,
  method: HttpMethod,
  path: string,
  context: string,
  allowEmpty = false,
): string {
  const value = record[field];
  if (typeof value !== "string") {
    return malformedWorkflowResponse(
      method,
      path,
      `${context}.${field} must be a string; received ${String(value)}`,
    );
  }
  if (!allowEmpty && value.trim().length === 0) {
    return malformedWorkflowResponse(
      method,
      path,
      `${context}.${field} must be a non-empty string`,
    );
  }
  return value;
}

function projectStatus(
  value: unknown,
  method: HttpMethod,
  path: string,
  context: string,
): WorkflowProjectStatus {
  if (value === "active" || value === "archived") return value;
  return malformedWorkflowResponse(
    method,
    path,
    `${context}.status must be "active" or "archived"; received ${String(value)}`,
  );
}

function noteStatus(
  value: unknown,
  method: HttpMethod,
  path: string,
  context: string,
): WorkflowProjectNoteStatus {
  if (value === "open" || value === "done") return value;
  return malformedWorkflowResponse(
    method,
    path,
    `${context}.status must be "open" or "done"; received ${String(value)}`,
  );
}

function normalizeProject(
  value: unknown,
  method: HttpMethod,
  path: string,
  context = "project",
): WorkflowProject {
  const record = requiredRecord(value, method, path, context);
  return {
    projectId: requiredString(record, "projectId", method, path, context),
    name: requiredString(record, "name", method, path, context),
    description: requiredString(
      record,
      "description",
      method,
      path,
      context,
      true,
    ),
    status: projectStatus(record.status, method, path, context),
    createdAt: requiredString(record, "createdAt", method, path, context),
    updatedAt: requiredString(record, "updatedAt", method, path, context),
  };
}

function normalizeRepositoryReference(
  value: unknown,
  method: HttpMethod,
  path: string,
  context = "repositoryReference",
): WorkflowProjectRepositoryReference {
  const record = requiredRecord(value, method, path, context);
  return {
    repoTarget: requiredString(record, "repoTarget", method, path, context),
    createdAt: requiredString(record, "createdAt", method, path, context),
  };
}

function normalizeProjectNote(
  value: unknown,
  method: HttpMethod,
  path: string,
  context = "note",
): WorkflowProjectNote {
  const record = requiredRecord(value, method, path, context);
  return {
    noteId: requiredString(record, "noteId", method, path, context),
    title: requiredString(record, "title", method, path, context),
    body: requiredString(record, "body", method, path, context),
    status: noteStatus(record.status, method, path, context),
    createdAt: requiredString(record, "createdAt", method, path, context),
    updatedAt: requiredString(record, "updatedAt", method, path, context),
  };
}

function normalizeProjectPlan(
  value: unknown,
  method: HttpMethod,
  path: string,
  context = "plan",
): WorkflowProjectPlanSummary {
  const record = requiredRecord(value, method, path, context);
  return {
    planId: requiredString(record, "planId", method, path, context),
    featureSlug: requiredString(record, "featureSlug", method, path, context),
    status: requiredString(record, "status", method, path, context),
    createdAt: requiredString(record, "createdAt", method, path, context),
    updatedAt: requiredString(record, "updatedAt", method, path, context),
  };
}

function normalizeRepositoryTarget(
  value: unknown,
  method: HttpMethod,
  path: string,
  context = "repository",
): WorkflowRepositoryTarget {
  const record = requiredRecord(value, method, path, context);
  return {
    repoTarget: requiredString(record, "repoTarget", method, path, context),
    localPath: requiredString(record, "localPath", method, path, context),
    createdAt: requiredString(record, "createdAt", method, path, context),
    updatedAt: requiredString(record, "updatedAt", method, path, context),
  };
}

function parseErrorShape(text: string): RelayApiErrorShape | undefined {
  if (!text) return undefined;
  try {
    const value = JSON.parse(text);
    return value && typeof value === "object" && !Array.isArray(value)
      ? value as RelayApiErrorShape
      : undefined;
  } catch {
    return undefined;
  }
}

async function requestWorkflow(
  method: HttpMethod,
  path: string,
  body?: unknown,
): Promise<{ status: number; text: string }> {
  const headers: Record<string, string> = {
    Accept: "application/json",
  };
  if (body !== undefined) {
    headers["Content-Type"] = "application/json";
  }

  try {
    const response = await fetch(`${API_BASE_URL}${path}`, {
      method,
      headers,
      body: body === undefined ? undefined : JSON.stringify(body),
    });
    const text = await response.text();

    if (!response.ok) {
      const errorShape = parseErrorShape(text);
      throw new RelayApiError(
        errorShape?.message || `${method} ${path} failed with status ${response.status}`,
        response.status,
        path,
        method,
        errorShape,
      );
    }

    return { status: response.status, text };
  } catch (error) {
    if (error instanceof RelayApiError) throw error;
    const message = error instanceof Error ? error.message : "Unknown network error";
    throw new RelayApiError(
      `Network error during ${method} ${path}: ${message}`,
      503,
      path,
      method,
    );
  }
}

async function requestWorkflowJson<T>(
  method: HttpMethod,
  path: string,
  body?: unknown,
): Promise<T> {
  const response = await requestWorkflow(method, path, body);
  if (response.status === 204 || response.text.length === 0) {
    return malformedWorkflowResponse(
      method,
      path,
      `expected a JSON response body; received status ${response.status}`,
    );
  }

  try {
    return JSON.parse(response.text) as T;
  } catch (error) {
    const message = error instanceof Error ? error.message : "Invalid JSON";
    return malformedWorkflowResponse(method, path, message);
  }
}

async function requestWorkflowNoContent(
  method: Extract<HttpMethod, "DELETE">,
  path: string,
): Promise<void> {
  const response = await requestWorkflow(method, path);
  if (response.status !== 204) {
    return malformedWorkflowResponse(
      method,
      path,
      `expected HTTP 204 with no response body; received status ${response.status}`,
    );
  }
  if (response.text.length !== 0) {
    return malformedWorkflowResponse(
      method,
      path,
      "HTTP 204 response must not include a response body",
    );
  }
}

export async function listWorkflowProjects(
  filters: WorkflowProjectListFilters = {},
): Promise<WorkflowProjectListResponse> {
  const params = new URLSearchParams();
  if (filters.status) params.set("status", filters.status);
  if (typeof filters.limit === "number") params.set("limit", String(filters.limit));
  const query = params.toString();
  const path = `/api/projects${query ? `?${query}` : ""}`;
  const response = requiredRecord(
    await requestWorkflowJson<unknown>("GET", path),
    "GET",
    path,
    "response",
  );
  const items = requiredArray(response, "items", "GET", path, "response");
  return {
    count: requiredCount(response, "GET", path, "response"),
    projects: items.map((item, index) =>
      normalizeProject(item, "GET", path, `items[${index}]`)),
  };
}

export async function getWorkflowProject(
  projectId: string,
  limits: WorkflowProjectDetailLimits = {},
): Promise<WorkflowProjectDetail> {
  const params = new URLSearchParams();
  params.set("repositoryLimit", String(limits.repositoryLimit ?? 100));
  params.set("noteLimit", String(limits.noteLimit ?? 100));
  params.set("planLimit", String(limits.planLimit ?? 100));
  const path = `/api/projects/${encodeURIComponent(projectId)}?${params.toString()}`;
  const response = requiredRecord(
    await requestWorkflowJson<unknown>("GET", path),
    "GET",
    path,
    "response",
  );
  const repositories = requiredArray(
    response,
    "repositories",
    "GET",
    path,
    "response",
  );
  const notes = requiredArray(response, "notes", "GET", path, "response");
  const plans = requiredArray(response, "plans", "GET", path, "response");
  return {
    project: normalizeProject(response.project, "GET", path),
    repositories: repositories.map((repository, index) =>
      normalizeRepositoryReference(
        repository,
        "GET",
        path,
        `repositories[${index}]`,
      )),
    notes: notes.map((note, index) =>
      normalizeProjectNote(note, "GET", path, `notes[${index}]`)),
    plans: plans.map((plan, index) =>
      normalizeProjectPlan(plan, "GET", path, `plans[${index}]`)),
  };
}

export async function createWorkflowProject(
  request: CreateWorkflowProjectRequest,
): Promise<WorkflowProject> {
  const path = "/api/projects";
  const response = await requestWorkflowJson<unknown>("POST", path, request);
  return normalizeProject(response, "POST", path);
}

export async function updateWorkflowProject(
  projectId: string,
  request: UpdateWorkflowProjectRequest,
): Promise<WorkflowProject> {
  const path = `/api/projects/${encodeURIComponent(projectId)}`;
  const response = await requestWorkflowJson<unknown>("PATCH", path, request);
  return normalizeProject(response, "PATCH", path);
}

export async function archiveWorkflowProject(projectId: string): Promise<WorkflowProject> {
  const path = `/api/projects/${encodeURIComponent(projectId)}/archive`;
  const response = await requestWorkflowJson<unknown>("POST", path);
  return normalizeProject(response, "POST", path);
}

export async function restoreWorkflowProject(projectId: string): Promise<WorkflowProject> {
  const path = `/api/projects/${encodeURIComponent(projectId)}/restore`;
  const response = await requestWorkflowJson<unknown>("POST", path);
  return normalizeProject(response, "POST", path);
}

export async function listWorkflowRepositoryTargets(): Promise<WorkflowRepositoryTargetListResponse> {
  const path = "/api/repositories";
  const response = requiredRecord(
    await requestWorkflowJson<unknown>("GET", path),
    "GET",
    path,
    "response",
  );
  const items = requiredArray(response, "items", "GET", path, "response");
  return {
    count: requiredCount(response, "GET", path, "response"),
    repositories: items.map((item, index) =>
      normalizeRepositoryTarget(item, "GET", path, `items[${index}]`)),
  };
}

export async function attachWorkflowProjectRepository(
  projectId: string,
  repoTarget: string,
): Promise<WorkflowProjectRepositoryReference> {
  const path = `/api/projects/${encodeURIComponent(projectId)}/repositories/${encodeURIComponent(repoTarget)}`;
  const response = await requestWorkflowJson<unknown>("PUT", path);
  return normalizeRepositoryReference(
    response,
    "PUT",
    path,
    "repositoryReference",
  );
}

export async function detachWorkflowProjectRepository(
  projectId: string,
  repoTarget: string,
): Promise<void> {
  await requestWorkflowNoContent(
    "DELETE",
    `/api/projects/${encodeURIComponent(projectId)}/repositories/${encodeURIComponent(repoTarget)}`,
  );
}

export async function createWorkflowProjectNote(
  projectId: string,
  request: CreateWorkflowProjectNoteRequest,
): Promise<WorkflowProjectNote> {
  const path = `/api/projects/${encodeURIComponent(projectId)}/notes`;
  const response = await requestWorkflowJson<unknown>("POST", path, request);
  return normalizeProjectNote(response, "POST", path);
}

export async function updateWorkflowProjectNote(
  projectId: string,
  noteId: string,
  request: UpdateWorkflowProjectNoteRequest,
): Promise<WorkflowProjectNote> {
  const path = `/api/projects/${encodeURIComponent(projectId)}/notes/${encodeURIComponent(noteId)}`;
  const response = await requestWorkflowJson<unknown>("PATCH", path, request);
  return normalizeProjectNote(response, "PATCH", path);
}

export async function deleteWorkflowProjectNote(
  projectId: string,
  noteId: string,
): Promise<void> {
  await requestWorkflowNoContent(
    "DELETE",
    `/api/projects/${encodeURIComponent(projectId)}/notes/${encodeURIComponent(noteId)}`,
  );
}
