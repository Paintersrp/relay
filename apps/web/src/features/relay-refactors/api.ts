// ============================================================
// Relay Refactor Backlog — frontend API client (PASS-006)
//
// Consumes the existing project-scoped refactor backlog backend routes under
// /api/projects/{projectId}/refactor/... using the same fetch + RelayApiError
// conventions as relay-projects/api.ts and relay-plans/api.ts.
//
// This module never imports or calls submitPlan, run-creation, executor
// dispatch, MCP, or audit-decision APIs. Generated refactor-only plans are
// review-only artifacts.
// ============================================================

import { API_BASE_URL, RelayApiError } from "@/features/relay-runs";
import type { RelayApiErrorShape } from "@/features/relay-runs/types";
import type {
  GenerateRefactorOnlyPlanRequest,
  GenerateRefactorOnlyPlanResult,
  PromoteRefactorCandidateRequest,
  PromoteRefactorCandidateResult,
  RefactorCandidate,
  RefactorCandidateLifecycleRequest,
  RefactorCandidateListFilters,
  RefactorCandidateRequest,
  RefactorCandidateScheduleRequest,
  RefactorDiscoveryLifecycleRequest,
  RefactorDiscoveryTask,
  RefactorDiscoveryTaskListFilters,
  RefactorDiscoveryTaskRequest,
  RefactorPlacementSuggestion,
  RefactorTargetScope,
  RefactorValidationIssue,
} from "./types";

// ------------------------------------------------------------
// Low-level fetch helpers (mirror relay-projects/api.ts)
// ------------------------------------------------------------

async function getRefactorJson<T>(path: string): Promise<T> {
  const url = `${API_BASE_URL}${path}`;

  try {
    const res = await fetch(url, {
      headers: { Accept: "application/json" },
    });

    if (!res.ok) {
      let errorShape: RelayApiErrorShape | undefined;
      try {
        errorShape = JSON.parse(await res.text());
      } catch {
        // Ignore malformed error response body.
      }
      throw new RelayApiError(
        `Failed to fetch from GET ${path} (status: ${res.status})`,
        res.status,
        path,
        "GET",
        errorShape,
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

async function postRefactorJson<TReq, TRes>(
  path: string,
  body: TReq,
): Promise<TRes> {
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
        errorShape = JSON.parse(await res.text());
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

/**
 * Extracts structured backend validation issues from a thrown error when the
 * backend returned them under `details.validation`. Returns [] otherwise so
 * callers never swallow validation failures as generic success.
 */
export function extractRefactorValidationIssues(
  error: unknown,
): RefactorValidationIssue[] {
  if (!(error instanceof RelayApiError)) {
    return [];
  }
  const raw = error.errorShape?.details?.validation;
  if (!Array.isArray(raw)) {
    return [];
  }
  return raw
    .filter((issue) => issue && typeof issue === "object")
    .map((issue: any) => ({
      field: typeof issue.field === "string" ? issue.field : "",
      code: typeof issue.code === "string" ? issue.code : "",
      message: typeof issue.message === "string" ? issue.message : "",
    }));
}

// ------------------------------------------------------------
// Normalizers (camelCase response → frontend types, tolerant of absent arrays)
// ------------------------------------------------------------

function asStringArray(value: unknown): string[] {
  return Array.isArray(value)
    ? value.filter((item): item is string => typeof item === "string")
    : [];
}

function asStringRecord(value: unknown): Record<string, string> {
  if (!value || typeof value !== "object") {
    return {};
  }
  const out: Record<string, string> = {};
  for (const [key, raw] of Object.entries(value as Record<string, unknown>)) {
    if (typeof raw === "string") {
      out[key] = raw;
    }
  }
  return out;
}

function normalizeTargetScope(scope: any): RefactorTargetScope {
  return {
    kind: typeof scope?.kind === "string" ? scope.kind : "repository",
    values: asStringArray(scope?.values),
  };
}

function normalizeDiscoveryTask(task: any): RefactorDiscoveryTask {
  return {
    discoveryTaskId: task?.discoveryTaskId ?? "",
    projectId: task?.projectId ?? "",
    title: task?.title ?? "",
    analysisPrompt: task?.analysisPrompt ?? "",
    targetScope: normalizeTargetScope(task?.targetScope),
    status: task?.status ?? "open",
    priority: task?.priority ?? "",
    tags: asStringArray(task?.tags),
    createdFrom: task?.createdFrom ?? "",
    metadata: asStringRecord(task?.metadata),
    closureReason: task?.closureReason ?? undefined,
    completedAt: task?.completedAt ?? undefined,
    closedAt: task?.closedAt ?? undefined,
    createdAt: task?.createdAt ?? "",
    updatedAt: task?.updatedAt ?? "",
  };
}

function normalizeCandidate(candidate: any): RefactorCandidate {
  return {
    candidateId: candidate?.candidateId ?? "",
    projectId: candidate?.projectId ?? "",
    title: candidate?.title ?? "",
    problemSummary: candidate?.problemSummary ?? "",
    currentBehavior: candidate?.currentBehavior ?? "",
    desiredBehavior: candidate?.desiredBehavior ?? "",
    rationale: candidate?.rationale ?? "",
    proposedPassName: candidate?.proposedPassName ?? "",
    proposedPassGoal: candidate?.proposedPassGoal ?? "",
    proposedPassScope: asStringArray(candidate?.proposedPassScope),
    nonGoals: asStringArray(candidate?.nonGoals),
    targetFiles: asStringArray(candidate?.targetFiles),
    validationCommands: asStringArray(candidate?.validationCommands),
    auditFocus: asStringArray(candidate?.auditFocus),
    constraints: asStringArray(candidate?.constraints),
    riskLevel: candidate?.riskLevel ?? "medium",
    status: candidate?.status ?? "ready",
    dependencyNotes: candidate?.dependencyNotes ?? undefined,
    deferReason: candidate?.deferReason ?? undefined,
    rejectReason: candidate?.rejectReason ?? undefined,
    supersededByCandidateId: candidate?.supersededByCandidateId ?? undefined,
    supersedeReason: candidate?.supersedeReason ?? undefined,
    metadata: asStringRecord(candidate?.metadata),
    createdAt: candidate?.createdAt ?? "",
    updatedAt: candidate?.updatedAt ?? "",
  };
}

function normalizePlacementSuggestion(raw: any): RefactorPlacementSuggestion {
  return {
    placementReason: raw?.placementReason ?? "no_suggestion",
    afterPassId: raw?.afterPassId ?? "",
    sequenceAfter:
      typeof raw?.sequenceAfter === "number" ? raw.sequenceAfter : 0,
    confidence: raw?.confidence ?? "none",
    matchedPassIds: asStringArray(raw?.matchedPassIds),
    matchedPaths: asStringArray(raw?.matchedPaths),
    warnings: asStringArray(raw?.warnings),
  };
}

function normalizePromotionResult(raw: any): PromoteRefactorCandidateResult {
  const placement = raw?.placement ?? {};
  const schedulingReference = raw?.schedulingReference ?? {};
  return {
    candidateId: raw?.candidateId ?? "",
    planId: raw?.planId ?? "",
    passId: raw?.passId ?? "",
    sequence: typeof raw?.sequence === "number" ? raw.sequence : 0,
    candidateStatus: raw?.candidateStatus ?? "scheduled",
    schedulingReference: {
      planId: schedulingReference?.planId ?? "",
      passId: schedulingReference?.passId ?? "",
      runId: schedulingReference?.runId ?? "",
    },
    placement: {
      placementReason: placement?.placementReason ?? "",
      afterPassId: placement?.afterPassId ?? "",
      warnings: asStringArray(placement?.warnings),
    },
    warnings: asStringArray(raw?.warnings),
  };
}

function normalizeGeneratedPlan(raw: any): GenerateRefactorOnlyPlanResult {
  return {
    projectId: raw?.projectId ?? "",
    planId: raw?.planId ?? "",
    candidateIds: asStringArray(raw?.candidateIds),
    jsonArtifactPath: raw?.jsonArtifactPath ?? "",
    markdownArtifactPath: raw?.markdownArtifactPath ?? "",
    submissionPolicy: raw?.submissionPolicy ?? "",
    warnings: asStringArray(raw?.warnings),
  };
}

function enc(value: string): string {
  return encodeURIComponent(value);
}

// ------------------------------------------------------------
// Discovery task API
// ------------------------------------------------------------

export async function listRefactorDiscoveryTasks(
  projectId: string,
  filters: RefactorDiscoveryTaskListFilters = {},
): Promise<{ success: boolean; count: number; discoveryTasks: RefactorDiscoveryTask[] }> {
  const params = new URLSearchParams();
  if (filters.status) params.set("status", filters.status);
  if (typeof filters.limit === "number") params.set("limit", String(filters.limit));
  const query = params.toString();

  const response = await getRefactorJson<any>(
    `/api/projects/${enc(projectId)}/refactor/discovery-tasks${query ? `?${query}` : ""}`,
  );

  return {
    success: !!response?.success,
    count: response?.count ?? 0,
    discoveryTasks: Array.isArray(response?.discoveryTasks)
      ? response.discoveryTasks.map(normalizeDiscoveryTask)
      : [],
  };
}

export async function createRefactorDiscoveryTask(
  projectId: string,
  request: RefactorDiscoveryTaskRequest,
): Promise<RefactorDiscoveryTask> {
  const response = await postRefactorJson<RefactorDiscoveryTaskRequest, any>(
    `/api/projects/${enc(projectId)}/refactor/discovery-tasks`,
    request,
  );
  return normalizeDiscoveryTask(response?.discoveryTask);
}

export async function getRefactorDiscoveryTask(
  projectId: string,
  taskId: string,
): Promise<RefactorDiscoveryTask> {
  const response = await getRefactorJson<any>(
    `/api/projects/${enc(projectId)}/refactor/discovery-tasks/${enc(taskId)}`,
  );
  return normalizeDiscoveryTask(response?.discoveryTask);
}

export async function updateRefactorDiscoveryTask(
  projectId: string,
  taskId: string,
  request: RefactorDiscoveryTaskRequest,
): Promise<RefactorDiscoveryTask> {
  const response = await postRefactorJson<RefactorDiscoveryTaskRequest, any>(
    `/api/projects/${enc(projectId)}/refactor/discovery-tasks/${enc(taskId)}/update`,
    request,
  );
  return normalizeDiscoveryTask(response?.discoveryTask);
}

export async function completeRefactorDiscoveryTask(
  projectId: string,
  taskId: string,
  request: RefactorDiscoveryLifecycleRequest,
): Promise<RefactorDiscoveryTask> {
  const response = await postRefactorJson<RefactorDiscoveryLifecycleRequest, any>(
    `/api/projects/${enc(projectId)}/refactor/discovery-tasks/${enc(taskId)}/complete`,
    request,
  );
  return normalizeDiscoveryTask(response?.discoveryTask);
}

export async function closeRefactorDiscoveryTask(
  projectId: string,
  taskId: string,
  request: RefactorDiscoveryLifecycleRequest,
): Promise<RefactorDiscoveryTask> {
  const response = await postRefactorJson<RefactorDiscoveryLifecycleRequest, any>(
    `/api/projects/${enc(projectId)}/refactor/discovery-tasks/${enc(taskId)}/close`,
    request,
  );
  return normalizeDiscoveryTask(response?.discoveryTask);
}

export async function supersedeRefactorDiscoveryTask(
  projectId: string,
  taskId: string,
  request: RefactorDiscoveryLifecycleRequest,
): Promise<RefactorDiscoveryTask> {
  const response = await postRefactorJson<RefactorDiscoveryLifecycleRequest, any>(
    `/api/projects/${enc(projectId)}/refactor/discovery-tasks/${enc(taskId)}/supersede`,
    request,
  );
  return normalizeDiscoveryTask(response?.discoveryTask);
}

// ------------------------------------------------------------
// Candidate API
// ------------------------------------------------------------

export async function listRefactorCandidates(
  projectId: string,
  filters: RefactorCandidateListFilters = {},
): Promise<{ success: boolean; count: number; candidates: RefactorCandidate[] }> {
  const params = new URLSearchParams();
  if (filters.status) params.set("status", filters.status);
  if (filters.q) params.set("q", filters.q);
  if (typeof filters.limit === "number") params.set("limit", String(filters.limit));
  const query = params.toString();

  const response = await getRefactorJson<any>(
    `/api/projects/${enc(projectId)}/refactor/candidates${query ? `?${query}` : ""}`,
  );

  return {
    success: !!response?.success,
    count: response?.count ?? 0,
    candidates: Array.isArray(response?.candidates)
      ? response.candidates.map(normalizeCandidate)
      : [],
  };
}

export async function createRefactorCandidate(
  projectId: string,
  request: RefactorCandidateRequest,
): Promise<RefactorCandidate> {
  const response = await postRefactorJson<RefactorCandidateRequest, any>(
    `/api/projects/${enc(projectId)}/refactor/candidates`,
    request,
  );
  return normalizeCandidate(response?.candidate);
}

export async function getRefactorCandidate(
  projectId: string,
  candidateId: string,
): Promise<RefactorCandidate> {
  const response = await getRefactorJson<any>(
    `/api/projects/${enc(projectId)}/refactor/candidates/${enc(candidateId)}`,
  );
  return normalizeCandidate(response?.candidate);
}

export async function updateRefactorCandidate(
  projectId: string,
  candidateId: string,
  request: RefactorCandidateRequest,
): Promise<RefactorCandidate> {
  const response = await postRefactorJson<RefactorCandidateRequest, any>(
    `/api/projects/${enc(projectId)}/refactor/candidates/${enc(candidateId)}/update`,
    request,
  );
  return normalizeCandidate(response?.candidate);
}

export async function deferRefactorCandidate(
  projectId: string,
  candidateId: string,
  request: RefactorCandidateLifecycleRequest,
): Promise<RefactorCandidate> {
  const response = await postRefactorJson<RefactorCandidateLifecycleRequest, any>(
    `/api/projects/${enc(projectId)}/refactor/candidates/${enc(candidateId)}/defer`,
    request,
  );
  return normalizeCandidate(response?.candidate);
}

export async function rejectRefactorCandidate(
  projectId: string,
  candidateId: string,
  request: RefactorCandidateLifecycleRequest,
): Promise<RefactorCandidate> {
  const response = await postRefactorJson<RefactorCandidateLifecycleRequest, any>(
    `/api/projects/${enc(projectId)}/refactor/candidates/${enc(candidateId)}/reject`,
    request,
  );
  return normalizeCandidate(response?.candidate);
}

export async function supersedeRefactorCandidate(
  projectId: string,
  candidateId: string,
  request: RefactorCandidateLifecycleRequest,
): Promise<RefactorCandidate> {
  const response = await postRefactorJson<RefactorCandidateLifecycleRequest, any>(
    `/api/projects/${enc(projectId)}/refactor/candidates/${enc(candidateId)}/supersede`,
    request,
  );
  return normalizeCandidate(response?.candidate);
}

/**
 * MarkScheduled is exposed only to display the existing backend capability.
 * PASS-004 promotion owns scheduling reference creation, so the promotion UI
 * path does not call this.
 */
export async function markScheduledRefactorCandidate(
  projectId: string,
  candidateId: string,
  request: RefactorCandidateScheduleRequest,
): Promise<RefactorCandidate> {
  const response = await postRefactorJson<RefactorCandidateScheduleRequest, any>(
    `/api/projects/${enc(projectId)}/refactor/candidates/${enc(candidateId)}/mark-scheduled`,
    request,
  );
  return normalizeCandidate(response?.candidate);
}

// ------------------------------------------------------------
// Placement / promotion / generated plan API
// ------------------------------------------------------------

export async function getRefactorCandidatePlacementSuggestion(
  projectId: string,
  candidateId: string,
  planId: string,
): Promise<RefactorPlacementSuggestion> {
  const response = await getRefactorJson<any>(
    `/api/projects/${enc(projectId)}/refactor/candidates/${enc(candidateId)}/placement-suggestion?plan_id=${enc(planId)}`,
  );
  // Backend returns the suggestion under `suggestion`; tolerate alternate keys.
  const raw =
    response?.suggestion ?? response?.placementSuggestion ?? response?.result ?? response;
  return normalizePlacementSuggestion(raw);
}

export async function promoteRefactorCandidate(
  projectId: string,
  candidateId: string,
  request: PromoteRefactorCandidateRequest,
): Promise<PromoteRefactorCandidateResult> {
  const response = await postRefactorJson<PromoteRefactorCandidateRequest, any>(
    `/api/projects/${enc(projectId)}/refactor/candidates/${enc(candidateId)}/promote`,
    request,
  );
  // Backend returns a flat result; tolerate `promotion`/`result` wrappers.
  const raw = response?.promotion ?? response?.result ?? response;
  return normalizePromotionResult(raw);
}

export async function generateRefactorOnlyPlan(
  projectId: string,
  request: GenerateRefactorOnlyPlanRequest,
): Promise<GenerateRefactorOnlyPlanResult> {
  const response = await postRefactorJson<GenerateRefactorOnlyPlanRequest, any>(
    `/api/projects/${enc(projectId)}/refactor/plans/generate`,
    request,
  );
  // Backend returns a flat result; tolerate `generatedPlan`/`plan`/`result`.
  const raw = response?.generatedPlan ?? response?.plan ?? response?.result ?? response;
  return normalizeGeneratedPlan(raw);
}
