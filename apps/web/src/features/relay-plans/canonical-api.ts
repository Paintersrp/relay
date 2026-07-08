import {
  asWorkflowRecord,
  malformedWorkflowResponse,
  optionalWorkflowString,
  requestWorkflowJson,
  requiredWorkflowArray,
  requiredWorkflowBoolean,
  requiredWorkflowInteger,
  requiredWorkflowString,
  type WorkflowHttpMethod,
  type WorkflowJsonRecord,
} from "@/features/workflow-canonical-api";
import type {
  MoveWorkflowPlanRequest,
  SubmitWorkflowPlanRequest,
  SubmitWorkflowPlanResponse,
  WorkflowArtifactReference,
  WorkflowCanonicalValidation,
  WorkflowPlanDetail,
  WorkflowPlanListFilters,
  WorkflowPlanListResponse,
  WorkflowPlanPass,
  WorkflowPlanPassStatus,
  WorkflowPlanRepository,
  WorkflowPlanRunReference,
  WorkflowPlanStatus,
  WorkflowPlanSummary,
  WorkflowProjectReference,
  WorkflowProjectStatus,
  WorkflowRunStage,
} from "./canonical-types";

function enumValue<T extends string>(
  value: unknown,
  allowed: readonly T[],
  method: WorkflowHttpMethod,
  path: string,
  context: string,
): T {
  if (typeof value === "string" && allowed.includes(value as T)) return value as T;
  return malformedWorkflowResponse(
    method,
    path,
    `${context} must be one of: ${allowed.join(", ")}`,
  );
}

function parseProject(
  value: unknown,
  method: WorkflowHttpMethod,
  path: string,
  context: string,
): WorkflowProjectReference {
  const record = asWorkflowRecord(value, method, path, context);
  return {
    projectId: requiredWorkflowString(record, "projectId", method, path, context),
    name: requiredWorkflowString(record, "name", method, path, context),
    status: enumValue<WorkflowProjectStatus>(
      record.status,
      ["active", "archived"],
      method,
      path,
      `${context}.status`,
    ),
  };
}

function parseArtifact(
  value: unknown,
  method: WorkflowHttpMethod,
  path: string,
  context: string,
): WorkflowArtifactReference {
  const record = asWorkflowRecord(value, method, path, context);
  return {
    artifactId: requiredWorkflowString(record, "artifactId", method, path, context),
    ownerType: requiredWorkflowString(record, "ownerType", method, path, context),
    kind: requiredWorkflowString(record, "kind", method, path, context),
    mediaType: requiredWorkflowString(record, "mediaType", method, path, context),
    sha256: requiredWorkflowString(record, "sha256", method, path, context),
    sizeBytes: requiredWorkflowInteger(record, "sizeBytes", method, path, context),
    createdAt: requiredWorkflowString(record, "createdAt", method, path, context),
    contentUrl: requiredWorkflowString(record, "contentUrl", method, path, context),
  };
}

function parseRunReference(
  value: unknown,
  method: WorkflowHttpMethod,
  path: string,
  context: string,
): WorkflowPlanRunReference {
  const record = asWorkflowRecord(value, method, path, context);
  return {
    runId: requiredWorkflowString(record, "runId", method, path, context),
    status: requiredWorkflowString(record, "status", method, path, context),
    stage: enumValue<WorkflowRunStage>(
      record.stage,
      ["specification", "execute", "audit"],
      method,
      path,
      `${context}.stage`,
    ),
    branch: requiredWorkflowString(record, "branch", method, path, context),
    baseCommit: requiredWorkflowString(record, "baseCommit", method, path, context),
    remediatesRunId: optionalWorkflowString(
      record,
      "remediatesRunId",
      method,
      path,
      context,
    ),
    createdAt: requiredWorkflowString(record, "createdAt", method, path, context),
    updatedAt: requiredWorkflowString(record, "updatedAt", method, path, context),
  };
}

function parsePass(
  value: unknown,
  method: WorkflowHttpMethod,
  path: string,
  context: string,
): WorkflowPlanPass {
  const record = asWorkflowRecord(value, method, path, context);
  const dependsOn = requiredWorkflowArray(
    record,
    "dependsOn",
    method,
    path,
    context,
  ).map((entry, index) => {
    if (typeof entry !== "string" || entry.trim().length === 0) {
      return malformedWorkflowResponse(
        method,
        path,
        `${context}.dependsOn[${index}] must be a nonblank string`,
      );
    }
    return entry;
  });
  return {
    passId: requiredWorkflowString(record, "passId", method, path, context),
    number: requiredWorkflowInteger(record, "number", method, path, context, 1),
    name: requiredWorkflowString(record, "name", method, path, context),
    repoTarget: requiredWorkflowString(record, "repoTarget", method, path, context),
    status: enumValue<WorkflowPlanPassStatus>(
      record.status,
      ["planned", "in_progress", "completed"],
      method,
      path,
      `${context}.status`,
    ),
    dependsOn,
    createdAt: requiredWorkflowString(record, "createdAt", method, path, context),
    updatedAt: requiredWorkflowString(record, "updatedAt", method, path, context),
    startedAt: optionalWorkflowString(record, "startedAt", method, path, context),
    completedAt: optionalWorkflowString(record, "completedAt", method, path, context),
    runs: requiredWorkflowArray(record, "runs", method, path, context).map(
      (entry, index) =>
        parseRunReference(entry, method, path, `${context}.runs[${index}]`),
    ),
  };
}

function parseRepository(
  value: unknown,
  method: WorkflowHttpMethod,
  path: string,
  context: string,
): WorkflowPlanRepository {
  const record = asWorkflowRecord(value, method, path, context);
  return {
    repoTarget: requiredWorkflowString(record, "repoTarget", method, path, context),
    branch: requiredWorkflowString(record, "branch", method, path, context),
    planningBaseCommit: requiredWorkflowString(
      record,
      "planningBaseCommit",
      method,
      path,
      context,
    ),
    sequence: requiredWorkflowInteger(record, "sequence", method, path, context),
  };
}

function parsePlanSummary(
  value: unknown,
  method: WorkflowHttpMethod,
  path: string,
  context: string,
): WorkflowPlanSummary {
  const record = asWorkflowRecord(value, method, path, context);
  return {
    planId: requiredWorkflowString(record, "planId", method, path, context),
    project: parseProject(record.project, method, path, `${context}.project`),
    featureSlug: requiredWorkflowString(record, "featureSlug", method, path, context),
    status: enumValue<WorkflowPlanStatus>(
      record.status,
      ["active", "completed"],
      method,
      path,
      `${context}.status`,
    ),
    canonicalSha256: requiredWorkflowString(
      record,
      "canonicalSha256",
      method,
      path,
      context,
    ),
    createdAt: requiredWorkflowString(record, "createdAt", method, path, context),
    updatedAt: requiredWorkflowString(record, "updatedAt", method, path, context),
    completedAt: optionalWorkflowString(record, "completedAt", method, path, context),
    passCount: requiredWorkflowInteger(record, "passCount", method, path, context),
    completedPassCount: requiredWorkflowInteger(
      record,
      "completedPassCount",
      method,
      path,
      context,
    ),
    inProgressPassCount: requiredWorkflowInteger(
      record,
      "inProgressPassCount",
      method,
      path,
      context,
    ),
    plannedPassCount: requiredWorkflowInteger(
      record,
      "plannedPassCount",
      method,
      path,
      context,
    ),
    currentPassId: optionalWorkflowString(
      record,
      "currentPassId",
      method,
      path,
      context,
    ),
  };
}

function parseDiagnostics(
  record: WorkflowJsonRecord,
  field: "diagnostics" | "notices",
  method: WorkflowHttpMethod,
  path: string,
): Record<string, unknown>[] {
  return requiredWorkflowArray(record, field, method, path, "response").map(
    (entry, index) =>
      asWorkflowRecord(entry, method, path, `${field}[${index}]`),
  );
}

function parseSubmittedPlan(
  value: unknown,
  method: WorkflowHttpMethod,
  path: string,
): SubmitWorkflowPlanResponse["plan"] {
  const record = asWorkflowRecord(value, method, path, "plan");
  return {
    planId: requiredWorkflowString(record, "planId", method, path, "plan"),
    featureSlug: requiredWorkflowString(record, "featureSlug", method, path, "plan"),
    status: enumValue<WorkflowPlanStatus>(
      record.status,
      ["active", "completed"],
      method,
      path,
      "plan.status",
    ),
    canonicalSha256: requiredWorkflowString(
      record,
      "canonicalSha256",
      method,
      path,
      "plan",
    ),
    project: parseProject(record.project, method, path, "plan.project"),
    createdAt: requiredWorkflowString(record, "createdAt", method, path, "plan"),
    updatedAt: requiredWorkflowString(record, "updatedAt", method, path, "plan"),
  };
}

export async function listWorkflowPlans(
  filters: WorkflowPlanListFilters = {},
): Promise<WorkflowPlanListResponse> {
  const params = new URLSearchParams();
  if (filters.status) params.set("status", filters.status);
  if (filters.projectId) params.set("projectId", filters.projectId);
  if (filters.limit !== undefined) params.set("limit", String(filters.limit));
  const query = params.toString();
  const path = `/api/plans${query ? `?${query}` : ""}`;
  const record = asWorkflowRecord(
    await requestWorkflowJson<unknown>("GET", path),
    "GET",
    path,
    "response",
  );
  return {
    count: requiredWorkflowInteger(record, "count", "GET", path, "response"),
    plans: requiredWorkflowArray(record, "items", "GET", path, "response").map(
      (entry, index) => parsePlanSummary(entry, "GET", path, `items[${index}]`),
    ),
  };
}

export async function getWorkflowPlan(planId: string): Promise<WorkflowPlanDetail> {
  const path = `/api/plans/${encodeURIComponent(planId)}`;
  const record = asWorkflowRecord(
    await requestWorkflowJson<unknown>("GET", path),
    "GET",
    path,
    "response",
  );
  return {
    plan: parsePlanSummary(record.plan, "GET", path, "plan"),
    repositories: requiredWorkflowArray(
      record,
      "repositories",
      "GET",
      path,
      "response",
    ).map((entry, index) =>
      parseRepository(entry, "GET", path, `repositories[${index}]`),
    ),
    passes: requiredWorkflowArray(record, "passes", "GET", path, "response").map(
      (entry, index) => parsePass(entry, "GET", path, `passes[${index}]`),
    ),
    artifacts: requiredWorkflowArray(
      record,
      "artifacts",
      "GET",
      path,
      "response",
    ).map((entry, index) =>
      parseArtifact(entry, "GET", path, `artifacts[${index}]`),
    ),
  };
}

export async function getWorkflowPlanPass(
  planId: string,
  passId: string,
): Promise<WorkflowPlanPass> {
  const path = `/api/plans/${encodeURIComponent(planId)}/passes/${encodeURIComponent(passId)}`;
  return parsePass(
    await requestWorkflowJson<unknown>("GET", path),
    "GET",
    path,
    "pass",
  );
}

export async function validateWorkflowPlan(
  fileName: string,
  canonicalContent: string,
): Promise<WorkflowCanonicalValidation> {
  const path = "/api/canonical-artifacts/validate";
  const record = asWorkflowRecord(
    await requestWorkflowJson<unknown>("POST", path, {
      fileName,
      canonicalContent,
    }),
    "POST",
    path,
    "response",
  );
  return {
    ok: requiredWorkflowBoolean(record, "ok", "POST", path, "response"),
    status: requiredWorkflowString(record, "status", "POST", path, "response"),
    kind: requiredWorkflowString(record, "kind", "POST", path, "response"),
    sha256: requiredWorkflowString(record, "sha256", "POST", path, "response"),
    diagnostics: parseDiagnostics(record, "diagnostics", "POST", path),
    notices: parseDiagnostics(record, "notices", "POST", path),
  };
}

export async function submitWorkflowPlan(
  request: SubmitWorkflowPlanRequest,
): Promise<SubmitWorkflowPlanResponse> {
  const path = "/api/plans";
  const record = asWorkflowRecord(
    await requestWorkflowJson<unknown>("POST", path, request),
    "POST",
    path,
    "response",
  );
  return {
    plan: parseSubmittedPlan(record.plan, "POST", path),
    passes: requiredWorkflowArray(record, "passes", "POST", path, "response").map(
      (entry, index) => {
        const pass = asWorkflowRecord(entry, "POST", path, `passes[${index}]`);
        return {
          passId: requiredWorkflowString(
            pass,
            "passId",
            "POST",
            path,
            `passes[${index}]`,
          ),
          number: requiredWorkflowInteger(
            pass,
            "number",
            "POST",
            path,
            `passes[${index}]`,
            1,
          ),
          name: requiredWorkflowString(
            pass,
            "name",
            "POST",
            path,
            `passes[${index}]`,
          ),
          repoTarget: requiredWorkflowString(
            pass,
            "repoTarget",
            "POST",
            path,
            `passes[${index}]`,
          ),
          status: enumValue<WorkflowPlanPassStatus>(
            pass.status,
            ["planned", "in_progress", "completed"],
            "POST",
            path,
            `passes[${index}].status`,
          ),
        };
      },
    ),
    artifacts: requiredWorkflowArray(
      record,
      "artifacts",
      "POST",
      path,
      "response",
    ).map((entry, index) =>
      parseArtifact(entry, "POST", path, `artifacts[${index}]`),
    ),
  };
}

export async function moveWorkflowPlan(
  planId: string,
  request: MoveWorkflowPlanRequest,
): Promise<SubmitWorkflowPlanResponse["plan"]> {
  const path = `/api/plans/${encodeURIComponent(planId)}/project`;
  return parseSubmittedPlan(
    await requestWorkflowJson<unknown>("PATCH", path, request),
    "PATCH",
    path,
  );
}
