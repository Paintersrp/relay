import { API_BASE_URL, RelayApiError } from "@/features/relay-runs";
import type { RelayApiErrorShape } from "@/features/relay-runs/types";

import type {
  PlanAPIContextBudget,
  PlanAPIContextFileRead,
  PlanAPIContextPlan,
  PlanAPIContextSearchTerm,
  PlanAPIPass,
  PlanAPISourceSnapshotRequirements,
  PlanDetailResponse,
  PlanListFilters,
  PlanListResponse,
  PlanPassDetailResponse,
  SubmitPlanRequest,
  SubmitPlanResponse,
  ValidatePlanRequest,
  ValidatePlanResponse,
} from "./types";

function firstNonEmptyString(...values: unknown[]): string | undefined {
  for (const value of values) {
    if (typeof value === "string" && value.trim().length > 0) {
      return value.trim();
    }
  }

  return undefined;
}

function optionalNumber(...values: unknown[]): number | undefined {
  for (const value of values) {
    if (typeof value === "number" && Number.isFinite(value)) {
      return value;
    }

    if (typeof value === "string" && value.trim().length > 0) {
      const parsed = Number(value.trim());
      if (Number.isFinite(parsed)) {
        return parsed;
      }
    }
  }

  return undefined;
}

function normalizeBoolean(value: unknown): boolean | undefined {
  if (typeof value === "boolean") {
    return value;
  }

  if (value === "true") {
    return true;
  }

  if (value === "false") {
    return false;
  }

  return undefined;
}

function normalizeStringArray(value: unknown): string[] {
  if (!Array.isArray(value)) {
    return [];
  }

  return value.filter((item): item is string => typeof item === "string");
}

function normalizeContextSearchTerm(term: any): PlanAPIContextSearchTerm {
  return {
    repoId: firstNonEmptyString(term?.repoId, term?.repo_id) || "",
    query: firstNonEmptyString(term?.query) || "",
    purpose: firstNonEmptyString(term?.purpose) || "",
    required: normalizeBoolean(term?.required),
  };
}

function normalizeContextFileRead(file: any): PlanAPIContextFileRead {
  return {
    repoId: firstNonEmptyString(file?.repoId, file?.repo_id) || "",
    path: firstNonEmptyString(file?.path) || "",
    purpose: firstNonEmptyString(file?.purpose) || "",
    required: normalizeBoolean(file?.required),
  };
}

function normalizeContextPlan(contextPlan: any): PlanAPIContextPlan {
  return {
    requiredRepositories: normalizeStringArray(
      contextPlan?.requiredRepositories ?? contextPlan?.required_repositories,
    ),
    seedSearchTerms: Array.isArray(
      contextPlan?.seedSearchTerms ?? contextPlan?.seed_search_terms,
    )
      ? (contextPlan.seedSearchTerms ?? contextPlan.seed_search_terms).map(
          normalizeContextSearchTerm,
        )
      : [],
    seedFilesToRead: Array.isArray(
      contextPlan?.seedFilesToRead ?? contextPlan?.seed_files_to_read,
    )
      ? (contextPlan.seedFilesToRead ?? contextPlan.seed_files_to_read).map(
          normalizeContextFileRead,
        )
      : [],
    contextCoverageExpectations: normalizeStringArray(
      contextPlan?.contextCoverageExpectations ??
        contextPlan?.context_coverage_expectations,
    ),
    blockedIfMissing: normalizeStringArray(
      contextPlan?.blockedIfMissing ?? contextPlan?.blocked_if_missing,
    ),
  };
}

function normalizeSourceSnapshotRequirements(
  requirements: any,
): PlanAPISourceSnapshotRequirements {
  return {
    requireGitStatus: normalizeBoolean(
      requirements?.requireGitStatus ?? requirements?.require_git_status,
    ),
    requireCommitSha: normalizeBoolean(
      requirements?.requireCommitSha ?? requirements?.require_commit_sha,
    ),
    allowDirtyWorktree: normalizeBoolean(
      requirements?.allowDirtyWorktree ?? requirements?.allow_dirty_worktree,
    ),
  };
}

function normalizeContextBudget(contextBudget: any): PlanAPIContextBudget {
  return {
    maxFiles: optionalNumber(contextBudget?.maxFiles, contextBudget?.max_files),
    maxBytes: optionalNumber(contextBudget?.maxBytes, contextBudget?.max_bytes),
    maxSearchResults: optionalNumber(
      contextBudget?.maxSearchResults,
      contextBudget?.max_search_results,
    ),
    maxContextLines: optionalNumber(
      contextBudget?.maxContextLines,
      contextBudget?.max_context_lines,
    ),
  };
}

function normalizePlanPass(pass: any): PlanAPIPass {
  return {
    ...pass,
    id: String(pass?.id ?? ""),
    planRowId: String(pass?.planRowId ?? pass?.plan_row_id ?? ""),
    passId: firstNonEmptyString(pass?.passId, pass?.pass_id) || "",
    sequence: optionalNumber(pass?.sequence) || 0,
    name: firstNonEmptyString(pass?.name) || "",
    goal: firstNonEmptyString(pass?.goal) || "",
    intendedExecutionScope: normalizeStringArray(
      pass?.intendedExecutionScope ?? pass?.intended_execution_scope,
    ),
    nonGoals: normalizeStringArray(pass?.nonGoals ?? pass?.non_goals),
    dependencies: normalizeStringArray(pass?.dependencies),
    status: firstNonEmptyString(pass?.status) || "planned",
    associatedRunIds: normalizeStringArray(
      pass?.associatedRunIds ?? pass?.associated_run_ids,
    ),
    associatedRuns: Array.isArray(pass?.associatedRuns ?? pass?.associated_runs)
      ? (pass.associatedRuns ?? pass.associated_runs).map((run: any) => ({
          id: String(run?.id ?? ""),
          title: firstNonEmptyString(run?.title) || "",
          status: firstNonEmptyString(run?.status) || "",
          lifecycleState:
            firstNonEmptyString(run?.lifecycleState, run?.lifecycle_state) || "",
          activeStep: firstNonEmptyString(run?.activeStep, run?.active_step) || "",
          workbenchPath:
            firstNonEmptyString(run?.workbenchPath, run?.workbench_path) || "",
          createdAt: firstNonEmptyString(run?.createdAt, run?.created_at) || "",
          updatedAt: firstNonEmptyString(run?.updatedAt, run?.updated_at) || "",
        }))
      : [],
    createdAt: firstNonEmptyString(pass?.createdAt, pass?.created_at) || "",
    updatedAt: firstNonEmptyString(pass?.updatedAt, pass?.updated_at) || "",
    passType: firstNonEmptyString(pass?.passType, pass?.pass_type),
    contextPlan: normalizeContextPlan(pass?.contextPlan ?? pass?.context_plan),
    sourceSnapshotRequirements: normalizeSourceSnapshotRequirements(
      pass?.sourceSnapshotRequirements ?? pass?.source_snapshot_requirements,
    ),
    handoffReadinessCriteria: normalizeStringArray(
      pass?.handoffReadinessCriteria ?? pass?.handoff_readiness_criteria,
    ),
    riskLevel: firstNonEmptyString(pass?.riskLevel, pass?.risk_level),
    contextBudget: normalizeContextBudget(
      pass?.contextBudget ?? pass?.context_budget,
    ),
    contextParseWarnings: normalizeStringArray(
      pass?.contextParseWarnings ?? pass?.context_parse_warnings,
    ),
  };
}

function normalizePlanDetailResponse(response: any): PlanDetailResponse {
  return {
    ...response,
    passes: Array.isArray(response?.passes)
      ? response.passes.map(normalizePlanPass)
      : [],
  };
}

function normalizePlanPassDetailResponse(response: any): PlanPassDetailResponse {
  return {
    ...response,
    pass: response?.pass ? normalizePlanPass(response.pass) : response?.pass,
  };
}

async function getPlanJson<T>(path: string): Promise<T> {
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

async function postPlanJson<TReq, TRes>(path: string, body: TReq): Promise<TRes> {
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

export async function validatePlan(
  request: ValidatePlanRequest,
): Promise<ValidatePlanResponse> {
  return postPlanJson<ValidatePlanRequest, ValidatePlanResponse>(
    "/api/plans/validate",
    request,
  );
}

export async function submitPlan(
  request: SubmitPlanRequest,
): Promise<SubmitPlanResponse> {
  return postPlanJson<SubmitPlanRequest, SubmitPlanResponse>("/api/plans", request);
}

export async function getPlans(
  filters: PlanListFilters = {},
): Promise<PlanListResponse> {
  const params = new URLSearchParams();

  if (filters.status) params.set("status", filters.status);
  if (typeof filters.limit === "number") params.set("limit", String(filters.limit));

  const query = params.toString();
  const response = await getPlanJson<PlanListResponse>(
    `/api/plans${query ? `?${query}` : ""}`,
  );

  return {
    ...response,
    plans: Array.isArray(response?.plans) ? response.plans : [],
  };
}

export async function getPlan(planId: string): Promise<PlanDetailResponse> {
  const response = await getPlanJson<PlanDetailResponse>(
    `/api/plans/${encodeURIComponent(planId)}`,
  );

  return normalizePlanDetailResponse(response);
}

export async function getPlanPass(
  planId: string,
  passId: string,
): Promise<PlanPassDetailResponse> {
  const response = await getPlanJson<PlanPassDetailResponse>(
    `/api/plans/${encodeURIComponent(planId)}/passes/${encodeURIComponent(passId)}`,
  );

  return normalizePlanPassDetailResponse(response);
}
