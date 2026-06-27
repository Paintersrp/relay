import { API_BASE_URL, RelayApiError } from "@/features/relay-runs";
import type { RelayApiErrorShape } from "@/features/relay-runs/types";

import type {
  ApprovePlanAttemptRequest,
  CreatePlanAttemptWithIntentRequest,
  NextAuditWorkFilters,
  NextAuditWorkResponse,
  NextPassWorkResponse,
  PlanAPIContextBudget,
  PlanAPIContextFileRead,
  PlanAPIContextPlan,
  PlanAPIContextSearchTerm,
  PlanAPIPass,
  PlanAPISourceSnapshotRequirements,
  PlanAttemptAPIResponse,
  PlanAttemptReviewGateAPIResponse,
  PlanDetailResponse,
  PlanListFilters,
  PlanListResponse,
  PlanPassDetailResponse,
  PlanReviewSettingsAPIResponse,
  RevisePlanAttemptRequest,
  RunPlanAttemptDriftReviewRequest,
  SubmitPlanAttemptRequest,
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
  if (filters.projectId) params.set("projectId", filters.projectId);

  const query = params.toString();
  const response = await getPlanJson<PlanListResponse>(
    `/api/plans${query ? `?${query}` : ""}`,
  );

  return {
    ...response,
    plans: Array.isArray(response?.plans) ? response.plans : [],
  };
}

export async function getPlan(
  planId: string,
  options: { projectId?: string } = {},
): Promise<PlanDetailResponse> {
  const params = new URLSearchParams();
  if (options.projectId) params.set("projectId", options.projectId);
  const query = params.toString();

  const response = await getPlanJson<PlanDetailResponse>(
    `/api/plans/${encodeURIComponent(planId)}${query ? `?${query}` : ""}`,
  );

  return normalizePlanDetailResponse(response);
}

export async function getPlanPass(
  planId: string,
  passId: string,
  options: { projectId?: string } = {},
): Promise<PlanPassDetailResponse> {
  const params = new URLSearchParams();
  if (options.projectId) params.set("projectId", options.projectId);
  const query = params.toString();

  const response = await getPlanJson<PlanPassDetailResponse>(
    `/api/plans/${encodeURIComponent(planId)}/passes/${encodeURIComponent(passId)}${query ? `?${query}` : ""}`,
  );

  return normalizePlanPassDetailResponse(response);
}

// Work-packet normalizers

function normalizeWorkBlocker(blocker: any) {
  return {
    code: firstNonEmptyString(blocker?.code) || "",
    message: firstNonEmptyString(blocker?.message) || "",
    recoverable: normalizeBoolean(blocker?.recoverable) ?? false,
  };
}

function normalizeWorkProjectSummary(project: any) {
  return {
    projectId: firstNonEmptyString(project?.projectId, project?.project_id) || "",
    name: firstNonEmptyString(project?.name) || "",
  };
}

function normalizeWorkPlanSummary(plan: any) {
  return {
    planId: firstNonEmptyString(plan?.planId, plan?.plan_id) || "",
    status: firstNonEmptyString(plan?.status) || "",
    title: firstNonEmptyString(plan?.title),
  };
}

function normalizeWorkPassSummary(pass: any) {
  return {
    passId: firstNonEmptyString(pass?.passId, pass?.pass_id) || "",
    sequence: optionalNumber(pass?.sequence) || 0,
    name: firstNonEmptyString(pass?.name) || "",
    status: firstNonEmptyString(pass?.status) || "planned",
    goal: firstNonEmptyString(pass?.goal),
  };
}

function normalizeWorkDependencyStatus(dep: any) {
  return {
    passId: firstNonEmptyString(dep?.passId, dep?.pass_id) || "",
    status: firstNonEmptyString(dep?.status) || "",
    satisfied: normalizeBoolean(dep?.satisfied) ?? false,
  };
}

function normalizeWorkRunSummary(run: any) {
  return {
    runId: firstNonEmptyString(run?.runId, run?.run_id) || "",
    title: firstNonEmptyString(run?.title),
    status: firstNonEmptyString(run?.status) || "",
    lifecycleState: firstNonEmptyString(run?.lifecycleState, run?.lifecycle_state) || "",
    activeStep: firstNonEmptyString(run?.activeStep, run?.active_step) || "",
    workbenchPath: firstNonEmptyString(run?.workbenchPath, run?.workbench_path),
  };
}

function normalizeWorkContextSummary(context: any) {
  return {
    contextPlan: normalizeContextPlan(context?.contextPlan ?? context?.context_plan),
    sourceSnapshotId: firstNonEmptyString(context?.sourceSnapshotId, context?.source_snapshot_id),
    sourceSnapshotStatus: firstNonEmptyString(context?.sourceSnapshotStatus, context?.source_snapshot_status),
    contextPacketId: firstNonEmptyString(context?.contextPacketId, context?.context_packet_id),
    contextPacketStatus: firstNonEmptyString(context?.contextPacketStatus, context?.context_packet_status),
    coverageReportPath: firstNonEmptyString(context?.coverageReportPath, context?.coverage_report_path),
    contextReady: normalizeBoolean(context?.contextReady ?? context?.context_ready) ?? false,
  };
}

function normalizeSuggestedRunSubmission(submission: any) {
  return {
    tool: firstNonEmptyString(submission?.tool) || "create_run_from_planner_handoff",
    arguments: {
      planId: firstNonEmptyString(submission?.arguments?.planId, submission?.arguments?.plan_id) || "",
      passId: firstNonEmptyString(submission?.arguments?.passId, submission?.arguments?.pass_id) || "",
    },
  };
}

function normalizeWorkArtifactReference(artifact: any) {
  return {
    kind: firstNonEmptyString(artifact?.kind) || "",
    label: firstNonEmptyString(artifact?.label) || "",
    filename: firstNonEmptyString(artifact?.filename) || "",
    contentUrl: firstNonEmptyString(artifact?.contentUrl, artifact?.content_url) || "",
    status: firstNonEmptyString(artifact?.status) || "",
    createdAt: firstNonEmptyString(artifact?.createdAt, artifact?.created_at),
  };
}

function normalizeNextPassWorkResponse(response: any): NextPassWorkResponse {
  return {
    ok: normalizeBoolean(response?.ok) ?? false,
    tool: firstNonEmptyString(response?.tool) || "get_next_pass_work",
    project: response?.project ? normalizeWorkProjectSummary(response.project) : undefined,
    plan: response?.plan ? normalizeWorkPlanSummary(response.plan) : undefined,
    selectedPass: response?.selectedPass || response?.selected_pass
      ? normalizeWorkPassSummary(response.selectedPass ?? response.selected_pass)
      : undefined,
    dependencyStatus: Array.isArray(response?.dependencyStatus ?? response?.dependency_status)
      ? (response.dependencyStatus ?? response.dependency_status).map(normalizeWorkDependencyStatus)
      : undefined,
    associatedRuns: Array.isArray(response?.associatedRuns ?? response?.associated_runs)
      ? (response.associatedRuns ?? response.associated_runs).map(normalizeWorkRunSummary)
      : undefined,
    context: response?.context ? normalizeWorkContextSummary(response.context) : undefined,
    handoffReadinessCriteria: normalizeStringArray(
      response?.handoffReadinessCriteria ?? response?.handoff_readiness_criteria
    ),
    suggestedRunSubmission: response?.suggestedRunSubmission || response?.suggested_run_submission
      ? normalizeSuggestedRunSubmission(response.suggestedRunSubmission ?? response.suggested_run_submission)
      : undefined,
    blockers: Array.isArray(response?.blockers)
      ? response.blockers.map(normalizeWorkBlocker)
      : [],
  };
}

function normalizeNextAuditWorkResponse(response: any): NextAuditWorkResponse {
  return {
    ok: normalizeBoolean(response?.ok) ?? false,
    tool: firstNonEmptyString(response?.tool) || "get_next_audit_work",
    project: response?.project ? normalizeWorkProjectSummary(response.project) : undefined,
    plan: response?.plan ? normalizeWorkPlanSummary(response.plan) : undefined,
    selectedPass: response?.selectedPass || response?.selected_pass
      ? normalizeWorkPassSummary(response.selectedPass ?? response.selected_pass)
      : undefined,
    selectedRun: response?.selectedRun || response?.selected_run
      ? normalizeWorkRunSummary(response.selectedRun ?? response.selected_run)
      : undefined,
    executorResultReferences: Array.isArray(response?.executorResultReferences ?? response?.executor_result_references)
      ? (response.executorResultReferences ?? response.executor_result_references).map(normalizeWorkArtifactReference)
      : undefined,
    validationReportReferences: Array.isArray(response?.validationReportReferences ?? response?.validation_report_references)
      ? (response.validationReportReferences ?? response.validation_report_references).map(normalizeWorkArtifactReference)
      : undefined,
    auditPacketReferences: Array.isArray(response?.auditPacketReferences ?? response?.audit_packet_references)
      ? (response.auditPacketReferences ?? response.audit_packet_references).map(normalizeWorkArtifactReference)
      : undefined,
    diffEvidenceReferences: Array.isArray(response?.diffEvidenceReferences ?? response?.diff_evidence_references)
      ? (response.diffEvidenceReferences ?? response.diff_evidence_references).map(normalizeWorkArtifactReference)
      : undefined,
    priorPassContext: response?.priorPassContext || response?.prior_pass_context
      ? {
          priorPasses: Array.isArray((response.priorPassContext ?? response.prior_pass_context)?.priorPasses ?? (response.priorPassContext ?? response.prior_pass_context)?.prior_passes)
            ? ((response.priorPassContext ?? response.prior_pass_context).priorPasses ?? (response.priorPassContext ?? response.prior_pass_context).prior_passes).map(normalizeWorkPassSummary)
            : [],
        }
      : undefined,
    allowedDecisions: normalizeStringArray(response?.allowedDecisions ?? response?.allowed_decisions),
    submitDecisionPayloadGuidance: response?.submitDecisionPayloadGuidance ?? response?.submit_decision_payload_guidance,
    blockers: Array.isArray(response?.blockers)
      ? response.blockers.map(normalizeWorkBlocker)
      : [],
  };
}

// Work-packet API calls

export async function getNextPassWork(
  projectId: string,
  planId: string,
): Promise<NextPassWorkResponse> {
  const response = await getPlanJson<any>(
    `/api/projects/${encodeURIComponent(projectId)}/plans/${encodeURIComponent(planId)}/next-pass-work`,
  );
  return normalizeNextPassWorkResponse(response);
}

export async function getNextAuditWork(
  projectId: string,
  planId: string,
  filters: NextAuditWorkFilters = {},
): Promise<NextAuditWorkResponse> {
  const params = new URLSearchParams();
  if (filters.passId) params.set("passId", filters.passId);
  if (filters.runId) params.set("runId", filters.runId);
  const query = params.toString();

  const response = await getPlanJson<any>(
    `/api/projects/${encodeURIComponent(projectId)}/plans/${encodeURIComponent(planId)}/next-audit-work${query ? `?${query}` : ""}`,
  );
  return normalizeNextAuditWorkResponse(response);
}

// ─── Plan Attempt / Review Gate API (PASS-006) ────────────────────────────────

export async function getPlanReviewSettings(
  projectId: string,
): Promise<PlanReviewSettingsAPIResponse> {
  return getPlanJson<PlanReviewSettingsAPIResponse>(
    `/api/projects/${encodeURIComponent(projectId)}/plan-review-settings`,
  );
}

export async function createPlanAttemptWithIntent(
  projectId: string,
  request: CreatePlanAttemptWithIntentRequest,
): Promise<PlanAttemptAPIResponse> {
  return postPlanJson<CreatePlanAttemptWithIntentRequest, PlanAttemptAPIResponse>(
    `/api/projects/${encodeURIComponent(projectId)}/plan-attempts`,
    request,
  );
}

export async function getPlanAttemptReviewGate(
  projectId: string,
  planAttemptId: string,
): Promise<PlanAttemptReviewGateAPIResponse> {
  return getPlanJson<PlanAttemptReviewGateAPIResponse>(
    `/api/projects/${encodeURIComponent(projectId)}/plan-attempts/${encodeURIComponent(planAttemptId)}/review-gate`,
  );
}

export async function runPlanAttemptDriftReview(
  projectId: string,
  planAttemptId: string,
  request: RunPlanAttemptDriftReviewRequest,
): Promise<PlanAttemptAPIResponse> {
  return postPlanJson<RunPlanAttemptDriftReviewRequest, PlanAttemptAPIResponse>(
    `/api/projects/${encodeURIComponent(projectId)}/plan-attempts/${encodeURIComponent(planAttemptId)}/run-drift-review`,
    request,
  );
}

export async function approvePlanAttempt(
  projectId: string,
  planAttemptId: string,
  request: ApprovePlanAttemptRequest,
): Promise<PlanAttemptAPIResponse> {
  return postPlanJson<ApprovePlanAttemptRequest, PlanAttemptAPIResponse>(
    `/api/projects/${encodeURIComponent(projectId)}/plan-attempts/${encodeURIComponent(planAttemptId)}/approve`,
    request,
  );
}

export async function submitPlanAttempt(
  projectId: string,
  planAttemptId: string,
  request: SubmitPlanAttemptRequest,
): Promise<PlanAttemptAPIResponse> {
  return postPlanJson<SubmitPlanAttemptRequest, PlanAttemptAPIResponse>(
    `/api/projects/${encodeURIComponent(projectId)}/plan-attempts/${encodeURIComponent(planAttemptId)}/submit`,
    request,
  );
}

export async function revisePlanAttempt(
  projectId: string,
  planAttemptId: string,
  request: RevisePlanAttemptRequest,
): Promise<PlanAttemptAPIResponse> {
  return postPlanJson<RevisePlanAttemptRequest, PlanAttemptAPIResponse>(
    `/api/projects/${encodeURIComponent(projectId)}/plan-attempts/${encodeURIComponent(planAttemptId)}/revisions`,
    request,
  );
}

export async function voidPlanAttempt(
  projectId: string,
  planAttemptId: string,
): Promise<PlanAttemptAPIResponse> {
  return postPlanJson<Record<string, never>, PlanAttemptAPIResponse>(
    `/api/projects/${encodeURIComponent(projectId)}/plan-attempts/${encodeURIComponent(planAttemptId)}/void`,
    {},
  );
}
