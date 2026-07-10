export { workflowApiUrl } from "@/features/workflow-api";

import {
  asWorkflowRecord,
  malformedWorkflowResponse,
  optionalEmptyWorkflowString,
  optionalWorkflowString,
  requestWorkflowJson,
  requiredWorkflowArray,
  requiredWorkflowBoolean,
  requiredWorkflowInteger,
  requiredWorkflowString,
  type WorkflowHttpMethod,
  type WorkflowJsonRecord,
} from "@/features/workflow-api";
import type {
  WorkflowArtifactReference,
  WorkflowCanonicalValidation,
  WorkflowProjectReference,
  WorkflowProjectStatus,
  WorkflowRunStage,
} from "@/features/relay-plans/types";
import type {
  CreateWorkflowRunRequest,
  CreateWorkflowRunResponse,
  PrepareWorkflowAuditResponse,
  WorkflowArtifactContent,
  WorkflowAuditDecision,
  WorkflowAuditPacket,
  WorkflowAuditStatus,
  WorkflowExecutionArtifact,
  WorkflowExecutionAttempt,
  WorkflowExecutionAttemptResult,
  WorkflowExecutionAttemptStatus,
  WorkflowExecutionAttemptSummary,
  WorkflowTerminalExecutionAttemptStatus,
  WorkflowRunDetail,
  WorkflowRunListFilters,
  WorkflowRunListResponse,
  WorkflowRunStatus,
  WorkflowRunSummary,
  WorkflowSpecificationReview,
} from "./types";

const RUN_STATUSES: readonly WorkflowRunStatus[] = [
  "created",
  "setup_ready",
  "executing",
  "execution_failed",
  "cancelled",
  "validating",
  "validation_failed",
  "audit_ready",
  "needs_revision",
  "completed",
];


const ATTEMPT_STATUSES: readonly WorkflowExecutionAttemptStatus[] = [
  "pending",
  "running",
  "succeeded",
  "failed",
  "cancelled",
  "timed_out",
];

const TERMINAL_ATTEMPT_STATUSES: readonly WorkflowTerminalExecutionAttemptStatus[] = [
  "succeeded",
  "failed",
  "cancelled",
  "timed_out",
];

function parseAttemptStatus(
  value: unknown,
  method: WorkflowHttpMethod,
  path: string,
  context: string,
): WorkflowExecutionAttemptStatus {
  if (
    typeof value === "string" &&
    ATTEMPT_STATUSES.includes(value as WorkflowExecutionAttemptStatus)
  ) {
    return value as WorkflowExecutionAttemptStatus;
  }
  return malformedWorkflowResponse(
    method,
    path,
    `${context} is not a supported workflow execution-attempt status`,
  );
}

function parseAttemptResult(
  value: unknown,
  method: WorkflowHttpMethod,
  path: string,
  context: string,
): WorkflowExecutionAttemptResult {
  const record = asWorkflowRecord(value, method, path, context);
  const result: WorkflowExecutionAttemptResult = { ...record };

  if ("cleanup_pending" in record) {
    if (typeof record.cleanup_pending !== "boolean") {
      return malformedWorkflowResponse(
        method,
        path,
        `${context}.cleanup_pending must be a boolean when present`,
      );
    }
    result.cleanup_pending = record.cleanup_pending;
  }

  if ("termination_verified" in record) {
    if (typeof record.termination_verified !== "boolean") {
      return malformedWorkflowResponse(
        method,
        path,
        `${context}.termination_verified must be a boolean when present`,
      );
    }
    result.termination_verified = record.termination_verified;
  }

  if ("pending_terminal_status" in record) {
    if (
      typeof record.pending_terminal_status !== "string" ||
      !TERMINAL_ATTEMPT_STATUSES.includes(
        record.pending_terminal_status as WorkflowTerminalExecutionAttemptStatus,
      )
    ) {
      return malformedWorkflowResponse(
        method,
        path,
        `${context}.pending_terminal_status must be a supported terminal workflow execution-attempt status when present`,
      );
    }
    result.pending_terminal_status =
      record.pending_terminal_status as WorkflowTerminalExecutionAttemptStatus;
  }

  return result;
}

function parseRunStatus(
  value: unknown,
  method: WorkflowHttpMethod,
  path: string,
  context: string,
): WorkflowRunStatus {
  if (typeof value === "string" && RUN_STATUSES.includes(value as WorkflowRunStatus)) {
    return value as WorkflowRunStatus;
  }
  return malformedWorkflowResponse(
    method,
    path,
    `${context} is not a supported workflow Run status`,
  );
}

function parseStage(
  value: unknown,
  method: WorkflowHttpMethod,
  path: string,
  context: string,
): WorkflowRunStage {
  if (value === "specification" || value === "execute" || value === "audit") {
    return value;
  }
  return malformedWorkflowResponse(
    method,
    path,
    `${context} must be "specification", "execute", or "audit"`,
  );
}

function parseProject(
  value: unknown,
  method: WorkflowHttpMethod,
  path: string,
  context: string,
): WorkflowProjectReference {
  const record = asWorkflowRecord(value, method, path, context);
  const status = record.status;
  if (status !== "active" && status !== "archived") {
    return malformedWorkflowResponse(
      method,
      path,
      `${context}.status must be "active" or "archived"`,
    );
  }
  return {
    projectId: requiredWorkflowString(record, "projectId", method, path, context),
    name: requiredWorkflowString(record, "name", method, path, context),
    status: status as WorkflowProjectStatus,
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

function parseExecutionArtifact(
  value: unknown,
  method: WorkflowHttpMethod,
  path: string,
  context: string,
): WorkflowExecutionArtifact {
  const record = asWorkflowRecord(value, method, path, context);
  return {
    artifactId: requiredWorkflowString(record, "artifactId", method, path, context),
    kind: requiredWorkflowString(record, "kind", method, path, context),
    mediaType: requiredWorkflowString(record, "mediaType", method, path, context),
    sha256: requiredWorkflowString(record, "sha256", method, path, context),
    sizeBytes: requiredWorkflowInteger(record, "sizeBytes", method, path, context),
    createdAt: requiredWorkflowString(record, "createdAt", method, path, context),
  };
}

function parseAttemptSummary(
  value: unknown,
  method: WorkflowHttpMethod,
  path: string,
  context: string,
): WorkflowExecutionAttemptSummary {
  const record = asWorkflowRecord(value, method, path, context);
  return {
    attemptId: requiredWorkflowString(record, "attemptId", method, path, context),
    attemptNumber: requiredWorkflowInteger(
      record,
      "attemptNumber",
      method,
      path,
      context,
      1,
    ),
    adapter: requiredWorkflowString(record, "adapter", method, path, context),
    model: requiredWorkflowString(record, "model", method, path, context),
    status: parseAttemptStatus(record.status, method, path, `${context}.status`),
    createdAt: requiredWorkflowString(record, "createdAt", method, path, context),
    startedAt: optionalWorkflowString(record, "startedAt", method, path, context),
    finishedAt: optionalWorkflowString(record, "finishedAt", method, path, context),
    cancellationRequestedAt: optionalWorkflowString(
      record,
      "cancellationRequestedAt",
      method,
      path,
      context,
    ),
    artifacts: requiredWorkflowArray(
      record,
      "artifacts",
      method,
      path,
      context,
    ).map((entry, index) =>
      parseArtifact(entry, method, path, `${context}.artifacts[${index}]`),
    ),
  };
}

function parseDetailedAttempt(
  value: unknown,
  method: WorkflowHttpMethod,
  path: string,
  context: string,
): WorkflowExecutionAttempt {
  const record = asWorkflowRecord(value, method, path, context);
  return {
    attemptId: requiredWorkflowString(record, "attemptId", method, path, context),
    runId: requiredWorkflowString(record, "runId", method, path, context),
    attemptNumber: requiredWorkflowInteger(
      record,
      "attemptNumber",
      method,
      path,
      context,
      1,
    ),
    adapter: requiredWorkflowString(record, "adapter", method, path, context),
    model: requiredWorkflowString(record, "model", method, path, context),
    status: parseAttemptStatus(record.status, method, path, `${context}.status`),
    result: parseAttemptResult(record.result, method, path, `${context}.result`),
    createdAt: requiredWorkflowString(record, "createdAt", method, path, context),
    startedAt: optionalWorkflowString(record, "startedAt", method, path, context),
    finishedAt: optionalWorkflowString(record, "finishedAt", method, path, context),
    cancellationRequestedAt: optionalWorkflowString(
      record,
      "cancellationRequestedAt",
      method,
      path,
      context,
    ),
    artifacts: requiredWorkflowArray(
      record,
      "artifacts",
      method,
      path,
      context,
    ).map((entry, index) =>
      parseExecutionArtifact(
        entry,
        method,
        path,
        `${context}.artifacts[${index}]`,
      ),
    ),
    liveStdout: optionalEmptyWorkflowString(
      record,
      "liveStdout",
      method,
      path,
      context,
    ),
    liveStderr: optionalEmptyWorkflowString(
      record,
      "liveStderr",
      method,
      path,
      context,
    ),
    liveStdoutTruncated: requiredWorkflowBoolean(
      record,
      "liveStdoutTruncated",
      method,
      path,
      context,
    ),
    liveStderrTruncated: requiredWorkflowBoolean(
      record,
      "liveStderrTruncated",
      method,
      path,
      context,
    ),
    liveStdoutBytes: requiredWorkflowInteger(
      record,
      "liveStdoutBytes",
      method,
      path,
      context,
    ),
    liveStderrBytes: requiredWorkflowInteger(
      record,
      "liveStderrBytes",
      method,
      path,
      context,
    ),
  };
}

function parsePacket(
  value: unknown,
  method: WorkflowHttpMethod,
  path: string,
  context: string,
): WorkflowAuditPacket {
  const record = asWorkflowRecord(value, method, path, context);
  return {
    auditPacketId: requiredWorkflowString(
      record,
      "auditPacketId",
      method,
      path,
      context,
    ),
    auditedCommit: requiredWorkflowString(
      record,
      "auditedCommit",
      method,
      path,
      context,
    ),
    packetSha256: requiredWorkflowString(
      record,
      "packetSha256",
      method,
      path,
      context,
    ),
    status: requiredWorkflowString(record, "status", method, path, context),
    staleReason: optionalWorkflowString(
      record,
      "staleReason",
      method,
      path,
      context,
    ),
    createdAt: requiredWorkflowString(record, "createdAt", method, path, context),
    supersededAt: optionalWorkflowString(
      record,
      "supersededAt",
      method,
      path,
      context,
    ),
  };
}

function parseDecision(
  value: unknown,
  method: WorkflowHttpMethod,
  path: string,
  context: string,
): WorkflowAuditDecision {
  const record = asWorkflowRecord(value, method, path, context);
  return {
    auditDecisionId: requiredWorkflowString(
      record,
      "auditDecisionId",
      method,
      path,
      context,
    ),
    auditedCommit: requiredWorkflowString(
      record,
      "auditedCommit",
      method,
      path,
      context,
    ),
    packetSha256: requiredWorkflowString(
      record,
      "packetSha256",
      method,
      path,
      context,
    ),
    decision: requiredWorkflowString(record, "decision", method, path, context),
    rationale: requiredWorkflowString(
      record,
      "rationale",
      method,
      path,
      context,
      true,
    ),
    createdAt: requiredWorkflowString(record, "createdAt", method, path, context),
  };
}

function optionalRecord(
  record: WorkflowJsonRecord,
  field: string,
  method: WorkflowHttpMethod,
  path: string,
  context: string,
): unknown | undefined {
  const value = record[field];
  if (value === undefined) return undefined;
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    return malformedWorkflowResponse(
      method,
      path,
      `${context}.${field} must be an object when present`,
    );
  }
  return value;
}

function parseRun(
  value: unknown,
  method: WorkflowHttpMethod,
  path: string,
  context: string,
): WorkflowRunSummary {
  const record = asWorkflowRecord(value, method, path, context);
  const latestAttempt = optionalRecord(
    record,
    "latestAttempt",
    method,
    path,
    context,
  );
  const currentPacket = optionalRecord(
    record,
    "currentPacket",
    method,
    path,
    context,
  );
  const latestDecision = optionalRecord(
    record,
    "latestDecision",
    method,
    path,
    context,
  );
  const project = optionalRecord(record, "project", method, path, context);
  return {
    runId: requiredWorkflowString(record, "runId", method, path, context),
    featureSlug: requiredWorkflowString(
      record,
      "featureSlug",
      method,
      path,
      context,
    ),
    repoTarget: requiredWorkflowString(record, "repoTarget", method, path, context),
    status: parseRunStatus(record.status, method, path, `${context}.status`),
    stage: parseStage(record.stage, method, path, `${context}.stage`),
    branch: requiredWorkflowString(record, "branch", method, path, context),
    baseCommit: requiredWorkflowString(record, "baseCommit", method, path, context),
    canonicalSha256: requiredWorkflowString(
      record,
      "canonicalSha256",
      method,
      path,
      context,
    ),
    planId: optionalWorkflowString(record, "planId", method, path, context),
    passId: optionalWorkflowString(record, "passId", method, path, context),
    passNumber:
      record.passNumber === undefined
        ? undefined
        : requiredWorkflowInteger(
            record,
            "passNumber",
            method,
            path,
            context,
            1,
          ),
    project: project
      ? parseProject(project, method, path, `${context}.project`)
      : undefined,
    remediatesRunId: optionalWorkflowString(
      record,
      "remediatesRunId",
      method,
      path,
      context,
    ),
    createdAt: requiredWorkflowString(record, "createdAt", method, path, context),
    updatedAt: requiredWorkflowString(record, "updatedAt", method, path, context),
    completedAt: optionalWorkflowString(
      record,
      "completedAt",
      method,
      path,
      context,
    ),
    latestAttempt: latestAttempt
      ? parseAttemptSummary(
          latestAttempt,
          method,
          path,
          `${context}.latestAttempt`,
        )
      : undefined,
    currentPacket: currentPacket
      ? parsePacket(currentPacket, method, path, `${context}.currentPacket`)
      : undefined,
    latestDecision: latestDecision
      ? parseDecision(latestDecision, method, path, `${context}.latestDecision`)
      : undefined,
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

function parseDetailedEnvelope(
  value: unknown,
  method: WorkflowHttpMethod,
  path: string,
): WorkflowExecutionAttempt {
  const record = asWorkflowRecord(value, method, path, "response");
  return parseDetailedAttempt(record.attempt, method, path, "attempt");
}

export async function listWorkflowRuns(
  filters: WorkflowRunListFilters = {},
): Promise<WorkflowRunListResponse> {
  const params = new URLSearchParams();
  if (filters.status) params.set("status", filters.status);
  if (filters.planId) params.set("planId", filters.planId);
  if (filters.passId) params.set("passId", filters.passId);
  if (filters.limit !== undefined) params.set("limit", String(filters.limit));
  const query = params.toString();
  const path = `/api/runs${query ? `?${query}` : ""}`;
  const record = asWorkflowRecord(
    await requestWorkflowJson<unknown>("GET", path),
    "GET",
    path,
    "response",
  );
  return {
    count: requiredWorkflowInteger(record, "count", "GET", path, "response"),
    runs: requiredWorkflowArray(record, "items", "GET", path, "response").map(
      (entry, index) => parseRun(entry, "GET", path, `items[${index}]`),
    ),
  };
}

export async function getWorkflowRun(runId: string): Promise<WorkflowRunDetail> {
  const path = `/api/runs/${encodeURIComponent(runId)}`;
  const record = asWorkflowRecord(
    await requestWorkflowJson<unknown>("GET", path),
    "GET",
    path,
    "response",
  );
  return {
    run: parseRun(record.run, "GET", path, "run"),
    attempts: requiredWorkflowArray(
      record,
      "attempts",
      "GET",
      path,
      "response",
    ).map((entry, index) =>
      parseAttemptSummary(entry, "GET", path, `attempts[${index}]`),
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

export async function getWorkflowSpecification(
  runId: string,
): Promise<WorkflowSpecificationReview> {
  const path = `/api/runs/${encodeURIComponent(runId)}/specification`;
  const record = asWorkflowRecord(
    await requestWorkflowJson<unknown>("GET", path),
    "GET",
    path,
    "response",
  );
  const planValue = optionalRecord(record, "plan", "GET", path, "response");
  const passValue = optionalRecord(record, "pass", "GET", path, "response");
  const plan = planValue
    ? (() => {
        const value = asWorkflowRecord(planValue, "GET", path, "plan");
        return {
          planId: requiredWorkflowString(value, "planId", "GET", path, "plan"),
          featureSlug: requiredWorkflowString(
            value,
            "featureSlug",
            "GET",
            path,
            "plan",
          ),
          status: requiredWorkflowString(value, "status", "GET", path, "plan"),
        };
      })()
    : undefined;
  const pass = passValue
    ? (() => {
        const value = asWorkflowRecord(passValue, "GET", path, "pass");
        return {
          passId: requiredWorkflowString(value, "passId", "GET", path, "pass"),
          number: requiredWorkflowInteger(value, "number", "GET", path, "pass", 1),
          name: requiredWorkflowString(value, "name", "GET", path, "pass"),
          repoTarget: requiredWorkflowString(
            value,
            "repoTarget",
            "GET",
            path,
            "pass",
          ),
          status: requiredWorkflowString(value, "status", "GET", path, "pass"),
        };
      })()
    : undefined;
  return {
    run: parseRun(record.run, "GET", path, "run"),
    executionSpec: parseArtifact(
      record.executionSpec,
      "GET",
      path,
      "executionSpec",
    ),
    executorBrief: parseArtifact(
      record.executorBrief,
      "GET",
      path,
      "executorBrief",
    ),
    plan,
    pass,
    remediatesRunId: optionalWorkflowString(
      record,
      "remediatesRunId",
      "GET",
      path,
      "response",
    ),
  };
}

export async function validateWorkflowExecutionSpec(
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

export async function createWorkflowRun(
  request: CreateWorkflowRunRequest,
): Promise<CreateWorkflowRunResponse> {
  const path = "/api/runs";
  const record = asWorkflowRecord(
    await requestWorkflowJson<unknown>("POST", path, request),
    "POST",
    path,
    "response",
  );
  const run = asWorkflowRecord(record.run, "POST", path, "run");
  return {
    run: {
      runId: requiredWorkflowString(run, "runId", "POST", path, "run"),
      featureSlug: requiredWorkflowString(
        run,
        "featureSlug",
        "POST",
        path,
        "run",
      ),
      repoTarget: requiredWorkflowString(
        run,
        "repoTarget",
        "POST",
        path,
        "run",
      ),
      status: parseRunStatus(run.status, "POST", path, "run.status"),
      branch: requiredWorkflowString(run, "branch", "POST", path, "run"),
      baseCommit: requiredWorkflowString(
        run,
        "baseCommit",
        "POST",
        path,
        "run",
      ),
      canonicalSha256: requiredWorkflowString(
        run,
        "canonicalSha256",
        "POST",
        path,
        "run",
      ),
      createdAt: requiredWorkflowString(
        run,
        "createdAt",
        "POST",
        path,
        "run",
      ),
      updatedAt: requiredWorkflowString(
        run,
        "updatedAt",
        "POST",
        path,
        "run",
      ),
      reviewUrl: requiredWorkflowString(
        run,
        "reviewUrl",
        "POST",
        path,
        "run",
      ),
    },
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

export async function startWorkflowAttempt(
  runId: string,
  adapter: string,
  model: string,
): Promise<WorkflowExecutionAttempt> {
  const path = `/api/runs/${encodeURIComponent(runId)}/attempts`;
  return parseDetailedEnvelope(
    await requestWorkflowJson<unknown>("POST", path, { adapter, model }),
    "POST",
    path,
  );
}

export async function getWorkflowAttempt(
  runId: string,
  attemptId: string,
): Promise<WorkflowExecutionAttempt> {
  const path = `/api/runs/${encodeURIComponent(runId)}/attempts/${encodeURIComponent(attemptId)}`;
  return parseDetailedAttempt(
    await requestWorkflowJson<unknown>("GET", path),
    "GET",
    path,
    "attempt",
  );
}

export async function cancelWorkflowAttempt(
  runId: string,
  attemptId: string,
): Promise<WorkflowExecutionAttempt> {
  const path = `/api/runs/${encodeURIComponent(runId)}/attempts/${encodeURIComponent(attemptId)}/cancel`;
  return parseDetailedEnvelope(
    await requestWorkflowJson<unknown>("POST", path),
    "POST",
    path,
  );
}

export async function reconcileWorkflowAttempt(
  runId: string,
  attemptId: string,
): Promise<WorkflowExecutionAttempt> {
  const path = `/api/runs/${encodeURIComponent(runId)}/attempts/${encodeURIComponent(attemptId)}/reconcile`;
  return parseDetailedEnvelope(
    await requestWorkflowJson<unknown>("POST", path),
    "POST",
    path,
  );
}

export async function getWorkflowAuditStatus(
  runId: string,
): Promise<WorkflowAuditStatus> {
  const path = `/api/runs/${encodeURIComponent(runId)}/audit/status`;
  const record = asWorkflowRecord(
    await requestWorkflowJson<unknown>("GET", path),
    "GET",
    path,
    "response",
  );
  const current = optionalRecord(record, "currentPacket", "GET", path, "response");
  const latest = optionalRecord(record, "latestPacket", "GET", path, "response");
  const decision = optionalRecord(record, "decision", "GET", path, "response");
  return {
    runId: requiredWorkflowString(record, "runId", "GET", path, "response"),
    runStatus: parseRunStatus(
      record.runStatus,
      "GET",
      path,
      "response.runStatus",
    ),
    currentPacket: current
      ? parsePacket(current, "GET", path, "currentPacket")
      : undefined,
    latestPacket: latest
      ? parsePacket(latest, "GET", path, "latestPacket")
      : undefined,
    decision: decision
      ? parseDecision(decision, "GET", path, "decision")
      : undefined,
  };
}

export async function prepareWorkflowAudit(
  runId: string,
  auditedCommit: string,
): Promise<PrepareWorkflowAuditResponse> {
  const path = `/api/runs/${encodeURIComponent(runId)}/audit/prepare`;
  const record = asWorkflowRecord(
    await requestWorkflowJson<unknown>("POST", path, { auditedCommit }),
    "POST",
    path,
    "response",
  );
  const artifact = asWorkflowRecord(record.artifact, "POST", path, "artifact");
  return {
    success: requiredWorkflowBoolean(record, "success", "POST", path, "response"),
    runId: requiredWorkflowString(record, "runId", "POST", path, "response"),
    runStatus: parseRunStatus(
      record.runStatus,
      "POST",
      path,
      "response.runStatus",
    ),
    packet: parsePacket(record.packet, "POST", path, "packet"),
    artifact: {
      artifactId: requiredWorkflowString(
        artifact,
        "artifactId",
        "POST",
        path,
        "artifact",
      ),
      kind: requiredWorkflowString(
        artifact,
        "kind",
        "POST",
        path,
        "artifact",
      ),
      sha256: requiredWorkflowString(
        artifact,
        "sha256",
        "POST",
        path,
        "artifact",
      ),
      sizeBytes: requiredWorkflowInteger(
        artifact,
        "sizeBytes",
        "POST",
        path,
        "artifact",
      ),
      contentUrl: requiredWorkflowString(
        artifact,
        "contentUrl",
        "POST",
        path,
        "artifact",
      ),
    },
  };
}

export async function getWorkflowArtifactContent(
  contentUrl: string,
): Promise<WorkflowArtifactContent> {
  if (!contentUrl.startsWith("/api/artifacts/")) {
    throw new Error("Artifact content URL must use /api/artifacts/.");
  }
  const record = asWorkflowRecord(
    await requestWorkflowJson<unknown>("GET", contentUrl),
    "GET",
    contentUrl,
    "response",
  );
  const encoding = requiredWorkflowString(
    record,
    "encoding",
    "GET",
    contentUrl,
    "response",
  );
  if (encoding !== "utf-8" && encoding !== "base64") {
    return malformedWorkflowResponse(
      "GET",
      contentUrl,
      'response.encoding must be "utf-8" or "base64"',
    );
  }
  return {
    artifact: parseArtifact(record.artifact, "GET", contentUrl, "artifact"),
    offset: requiredWorkflowInteger(
      record,
      "offset",
      "GET",
      contentUrl,
      "response",
    ),
    byteCount: requiredWorkflowInteger(
      record,
      "byteCount",
      "GET",
      contentUrl,
      "response",
    ),
    encoding,
    content: requiredWorkflowString(
      record,
      "content",
      "GET",
      contentUrl,
      "response",
      true,
    ),
    truncated: requiredWorkflowBoolean(
      record,
      "truncated",
      "GET",
      contentUrl,
      "response",
    ),
    nextOffset:
      record.nextOffset === undefined
        ? undefined
        : requiredWorkflowInteger(
            record,
            "nextOffset",
            "GET",
            contentUrl,
            "response",
          ),
  };
}
