import { API_BASE_URL, RelayApiError, type RelayApiErrorShape } from "../workflow-api";
import type {
  ConfirmWorkflowRepositoryRequest,
  CreateWorkflowProjectNoteRequest,
  CreateWorkflowProjectRequest,
  InspectWorkflowRepositoryRequest,
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
  WorkflowRepositoryConflictKind,
  WorkflowRepositoryInspection,
  WorkflowRepositoryInspectionState,
  WorkflowRepositoryRegistrationDisposition,
  WorkflowRepositoryRegistrationOutcome,
  WorkflowRepositoryRegistrationResult,
  WorkflowRepositoryRemoteCandidate,
  WorkflowRepositoryTarget,
  WorkflowRepositoryTargetListResponse,
  WorkflowRepositoryTargetOverrideReason,
  WorkflowRepositoryTargetSource,
} from "./types";

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

function repositoryInspectionState(
  value: unknown,
  method: HttpMethod,
  path: string,
  context: string,
): WorkflowRepositoryInspectionState {
  if (
    value === "ready" ||
    value === "needs_remote_selection" ||
    value === "needs_target_override" ||
    value === "conflict"
  ) {
    return value;
  }
  return malformedWorkflowResponse(
    method,
    path,
    `${context}.state has unsupported value ${String(value)}`,
  );
}

function repositoryRegistrationDisposition(
  value: unknown,
  method: HttpMethod,
  path: string,
  context: string,
): WorkflowRepositoryRegistrationDisposition {
  if (value === "create" || value === "reuse") return value;
  return malformedWorkflowResponse(
    method,
    path,
    `${context}.registrationDisposition has unsupported value ${String(value)}`,
  );
}

function repositoryRegistrationOutcome(
  value: unknown,
  method: HttpMethod,
  path: string,
  context: string,
): WorkflowRepositoryRegistrationOutcome {
  if (value === "created" || value === "reused") return value;
  return malformedWorkflowResponse(
    method,
    path,
    `${context}.outcome has unsupported value ${String(value)}`,
  );
}

function repositoryTargetOverrideReason(
  value: unknown,
  method: HttpMethod,
  path: string,
  context: string,
): WorkflowRepositoryTargetOverrideReason {
  if (value === "no_usable_remote" || value === "unsupported_remote") {
    return value;
  }
  return malformedWorkflowResponse(
    method,
    path,
    `${context}.targetOverrideReason has unsupported value ${String(value)}`,
  );
}

function repositoryTargetSource(
  value: unknown,
  method: HttpMethod,
  path: string,
  context: string,
): WorkflowRepositoryTargetSource {
  if (value === "remote_basename" || value === "operator_override") return value;
  return malformedWorkflowResponse(
    method,
    path,
    `${context}.repoTargetSource has unsupported value ${String(value)}`,
  );
}

function repositoryConflictKind(
  value: unknown,
  method: HttpMethod,
  path: string,
  context: string,
): WorkflowRepositoryConflictKind {
  if (value === "target" || value === "path") return value;
  return malformedWorkflowResponse(
    method,
    path,
    `${context}.conflictKind has unsupported value ${String(value)}`,
  );
}

function optionalRepositoryString(
  record: JsonRecord,
  field: string,
  method: HttpMethod,
  path: string,
  context: string,
): string | undefined {
  if (!(field in record)) return undefined;
  return requiredString(record, field, method, path, context);
}

function requireRepositoryFieldsAbsent(
  record: JsonRecord,
  fields: string[],
  method: HttpMethod,
  path: string,
  context: string,
): void {
  for (const field of fields) {
    if (field in record) {
      return malformedWorkflowResponse(
        method,
        path,
        `${context}.${field} must be absent for state ${String(record.state)}`,
      );
    }
  }
}

function normalizeRepositoryRemote(
  value: unknown,
  method: HttpMethod,
  path: string,
  context: string,
): WorkflowRepositoryRemoteCandidate {
  const record = requiredRecord(value, method, path, context);
  return {
    name: requiredString(record, "name", method, path, context),
    url: requiredString(record, "url", method, path, context),
    suggestedRepoTarget: optionalRepositoryString(
      record,
      "suggestedRepoTarget",
      method,
      path,
      context,
    ),
  };
}

function normalizeRepositoryInspection(
  value: unknown,
  method: HttpMethod,
  path: string,
  context = "inspection",
): WorkflowRepositoryInspection {
  const record = requiredRecord(value, method, path, context);
  const state = repositoryInspectionState(record.state, method, path, context);
  const remotes = requiredArray(record, "remotes", method, path, context).map(
    (remote, index) =>
      normalizeRepositoryRemote(
        remote,
        method,
        path,
        `${context}.remotes[${index}]`,
      ),
  );
  const notices = requiredArray(record, "notices", method, path, context).map(
    (notice, index) => {
      if (typeof notice !== "string") {
        return malformedWorkflowResponse(
          method,
          path,
          `${context}.notices[${index}] must be a string`,
        );
      }
      return notice;
    },
  );
  const base = {
    selectedPath: requiredString(record, "selectedPath", method, path, context),
    resolvedLocalPath: requiredString(
      record,
      "resolvedLocalPath",
      method,
      path,
      context,
    ),
    remotes,
    notices,
  };
  const selectedRemote =
    "selectedRemote" in record
      ? normalizeRepositoryRemote(
          record.selectedRemote,
          method,
          path,
          `${context}.selectedRemote`,
        )
      : undefined;
  const suggestedRepoTarget = optionalRepositoryString(
    record,
    "suggestedRepoTarget",
    method,
    path,
    context,
  );

  switch (state) {
    case "ready": {
      requireRepositoryFieldsAbsent(
        record,
        ["targetOverrideReason", "conflictKind"],
        method,
        path,
        context,
      );
      const registrationDisposition = repositoryRegistrationDisposition(
        record.registrationDisposition,
        method,
        path,
        context,
      );
      const existingRepository =
        "existingRepository" in record
          ? normalizeRepositoryTarget(
              record.existingRepository,
              method,
              path,
              `${context}.existingRepository`,
            )
          : undefined;
      if (registrationDisposition === "reuse" && !existingRepository) {
        return malformedWorkflowResponse(
          method,
          path,
          `${context}.existingRepository is required for reuse`,
        );
      }
      if (registrationDisposition === "create" && existingRepository) {
        return malformedWorkflowResponse(
          method,
          path,
          `${context}.existingRepository must be absent for create`,
        );
      }
      return {
        ...base,
        state,
        selectedRemote,
        suggestedRepoTarget,
        repoTarget: requiredString(record, "repoTarget", method, path, context),
        repoTargetSource: repositoryTargetSource(
          record.repoTargetSource,
          method,
          path,
          context,
        ),
        registrationDisposition,
        existingRepository,
        confirmationHash: requiredString(
          record,
          "confirmationHash",
          method,
          path,
          context,
        ),
      };
    }
    case "needs_remote_selection":
      requireRepositoryFieldsAbsent(
        record,
        [
          "selectedRemote",
          "suggestedRepoTarget",
          "targetOverrideReason",
          "repoTarget",
          "repoTargetSource",
          "registrationDisposition",
          "existingRepository",
          "conflictKind",
          "confirmationHash",
        ],
        method,
        path,
        context,
      );
      return { ...base, state };
    case "needs_target_override":
      requireRepositoryFieldsAbsent(
        record,
        [
          "repoTarget",
          "repoTargetSource",
          "registrationDisposition",
          "existingRepository",
          "conflictKind",
          "confirmationHash",
        ],
        method,
        path,
        context,
      );
      return {
        ...base,
        state,
        selectedRemote,
        suggestedRepoTarget,
        targetOverrideReason: repositoryTargetOverrideReason(
          record.targetOverrideReason,
          method,
          path,
          context,
        ),
      };
    case "conflict":
      requireRepositoryFieldsAbsent(
        record,
        ["targetOverrideReason", "registrationDisposition", "confirmationHash"],
        method,
        path,
        context,
      );
      return {
        ...base,
        state,
        selectedRemote,
        suggestedRepoTarget,
        repoTarget: requiredString(record, "repoTarget", method, path, context),
        repoTargetSource: repositoryTargetSource(
          record.repoTargetSource,
          method,
          path,
          context,
        ),
        existingRepository: normalizeRepositoryTarget(
          record.existingRepository,
          method,
          path,
          `${context}.existingRepository`,
        ),
        conflictKind: repositoryConflictKind(
          record.conflictKind,
          method,
          path,
          context,
        ),
      };
  }
}

function normalizeRepositoryRegistration(
  value: unknown,
  method: HttpMethod,
  path: string,
): WorkflowRepositoryRegistrationResult {
  const record = requiredRecord(value, method, path, "registration");
  return {
    outcome: repositoryRegistrationOutcome(
      record.outcome,
      method,
      path,
      "registration",
    ),
    repository: normalizeRepositoryTarget(
      record.repository,
      method,
      path,
      "registration.repository",
    ),
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

export class WorkflowRepositoryConfirmationError extends RelayApiError {
  readonly inspection: WorkflowRepositoryInspection;

  constructor(error: RelayApiError, inspection: WorkflowRepositoryInspection) {
    super(
      error.message,
      error.status,
      error.endpoint,
      error.method,
      error.errorShape,
    );
    this.name = "WorkflowRepositoryConfirmationError";
    this.inspection = inspection;
  }
}

export async function inspectWorkflowRepository(
  request: InspectWorkflowRepositoryRequest,
): Promise<WorkflowRepositoryInspection> {
  const path = "/api/repositories/inspect";
  const response = await requestWorkflowJson<unknown>("POST", path, {
    localPath: request.localPath,
    remoteName: request.remoteName ?? "",
    repoTargetOverride: request.repoTargetOverride ?? "",
  });
  return normalizeRepositoryInspection(response, "POST", path);
}

export async function confirmWorkflowRepository(
  request: ConfirmWorkflowRepositoryRequest,
): Promise<WorkflowRepositoryRegistrationResult> {
  const path = "/api/repositories";
  try {
    const response = await requestWorkflowJson<unknown>("POST", path, {
      localPath: request.localPath,
      remoteName: request.remoteName ?? "",
      repoTargetOverride: request.repoTargetOverride ?? "",
      expectedConfirmationHash: request.expectedConfirmationHash,
    });
    return normalizeRepositoryRegistration(response, "POST", path);
  } catch (error) {
    if (error instanceof RelayApiError) {
      const details = error.errorShape?.details;
      if (details && typeof details === "object" && !Array.isArray(details)) {
        const inspection = (details as JsonRecord).inspection;
        if (inspection !== undefined) {
          throw new WorkflowRepositoryConfirmationError(
            error,
            normalizeRepositoryInspection(
              inspection,
              "POST",
              path,
              "confirmation.details.inspection",
            ),
          );
        }
      }
    }
    throw error;
  }
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
