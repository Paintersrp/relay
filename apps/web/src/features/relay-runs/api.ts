// ============================================================
// Relay API Client Boundary
// ============================================================

import {
  getMockRelayRuns,
  getMockRelayRunById,
  getMockRelayRunArtifacts,
  getMockRelayRunEvents,
} from "./mock-data";
import type {
  RelayRun,
  RelayArtifact,
  RelayRunEvent,
  RelayActionRequest,
  RelayActionResponse,
  PlannerHandoffIntakeRequest,
  PlannerHandoffIntakeResponse,
  RelayApiErrorShape,
  RelayAuditDecisionValue,
} from "./types";

// Custom API Error Class
export class RelayApiError extends Error {
  status: number;
  endpoint: string;
  method: string;
  errorShape?: RelayApiErrorShape;

  constructor(
    message: string,
    status: number,
    endpoint: string,
    method: string,
    errorShape?: RelayApiErrorShape
  ) {
    super(message);
    this.name = "RelayApiError";
    this.status = status;
    this.endpoint = endpoint;
    this.method = method;
    this.errorShape = errorShape;
  }
}

// Read API base URL from Vite environment, default to localhost:8080
export const API_BASE_URL =
  (typeof import.meta !== "undefined" &&
    import.meta.env?.VITE_RELAY_API_BASE_URL) ||
  "http://localhost:8080";

/**
 * Executes a GET request with explicit fallback to mock data on offline,
 * 404, or 501 cases. Throws if the daemon returns invalid JSON.
 */
async function getJson<T>(path: string, mockFallback: () => T): Promise<T> {
  const url = `${API_BASE_URL}${path}`;
  try {
    const res = await fetch(url, {
      headers: {
        Accept: "application/json",
      },
    });

    if (res.status === 404 || res.status === 501) {
      return mockFallback();
    }

    if (!res.ok) {
      throw new RelayApiError(
        `Failed to fetch from GET ${path} (status: ${res.status})`,
        res.status,
        path,
        "GET"
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
        "GET"
      );
    }
  } catch (err: any) {
    if (err instanceof RelayApiError) {
      throw err;
    }
    // Connection refused / network offline. Fallback to mock data.
    return mockFallback();
  }
}

/**
 * Executes a POST request. Strictly forbids mock success; throws descriptive
 * RelayApiError on failure, unavailable daemon, or non-2xx response.
 */
async function postJson<TReq, TRes>(path: string, body?: TReq): Promise<TRes> {
  const url = `${API_BASE_URL}${path}`;
  try {
    const res = await fetch(url, {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        Accept: "application/json",
      },
      body: body ? JSON.stringify(body) : undefined,
    });

    if (!res.ok) {
      let errorShape: RelayApiErrorShape | undefined;
      try {
        const text = await res.text();
        errorShape = JSON.parse(text);
      } catch {
        // Ignore JSON parsing failures for error responses
      }
      throw new RelayApiError(
        `Mutation failed on POST ${path} (status: ${res.status})`,
        res.status,
        path,
        "POST",
        errorShape
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
        "POST"
      );
    }
  } catch (err: any) {
    if (err instanceof RelayApiError) {
      throw err;
    }
    // Daemon unavailable or connection refused
    throw new RelayApiError(
      `Daemon unavailable or connection refused on POST ${path}: ${err.message}`,
      503,
      path,
      "POST"
    );
  }
}

// GET endpoints
export async function getRuns(): Promise<RelayRun[]> {
  return getJson<RelayRun[]>("/api/runs", getMockRelayRuns);
}

// Legacy compatibility alias
export const listRuns = getRuns;

export async function getRun(id: string): Promise<RelayRun | null> {
  return getJson<RelayRun | null>(`/api/runs/${id}`, () => getMockRelayRunById(id) ?? null);
}

export async function getRunArtifacts(id: string): Promise<RelayArtifact[]> {
  return getJson<RelayArtifact[]>(`/api/runs/${id}/artifacts`, () => getMockRelayRunArtifacts(id));
}

export async function getRunEvents(id: string): Promise<RelayRunEvent[]> {
  return getJson<RelayRunEvent[]>(`/api/runs/${id}/events`, () => getMockRelayRunEvents(id));
}

// POST endpoints (mutations)
export async function submitPlannerHandoff(
  req: PlannerHandoffIntakeRequest
): Promise<PlannerHandoffIntakeResponse> {
  return postJson<PlannerHandoffIntakeRequest, PlannerHandoffIntakeResponse>(
    "/api/intake/planner-handoff",
    req
  );
}

export async function approveIntake(
  id: string,
  req: RelayActionRequest
): Promise<RelayActionResponse> {
  return postJson<RelayActionRequest, RelayActionResponse>(
    `/api/runs/${id}/approve-intake`,
    req
  );
}

export async function prepareRun(id: string): Promise<RelayActionResponse> {
  return postJson<undefined, RelayActionResponse>(`/api/runs/${id}/prepare`);
}

export async function renderBrief(id: string): Promise<RelayActionResponse> {
  return postJson<undefined, RelayActionResponse>(`/api/runs/${id}/render-brief`);
}

export async function approveBrief(
  id: string,
  req: RelayActionRequest
): Promise<RelayActionResponse> {
  return postJson<RelayActionRequest, RelayActionResponse>(
    `/api/runs/${id}/approve-brief`,
    req
  );
}

export interface ExecuteActionPayload {
  action: "start" | "cancel" | "recover";
}

export async function executeRun(id: string): Promise<RelayActionResponse> {
  return postJson<ExecuteActionPayload, RelayActionResponse>(`/api/runs/${id}/execute`, { action: "start" });
}

export async function cancelRun(id: string): Promise<RelayActionResponse> {
  return postJson<ExecuteActionPayload, RelayActionResponse>(`/api/runs/${id}/execute`, { action: "cancel" });
}

export async function recoverRun(id: string): Promise<RelayActionResponse> {
  return postJson<ExecuteActionPayload, RelayActionResponse>(`/api/runs/${id}/execute`, { action: "recover" });
}

export async function getArtifactContent(id: string, kind: string): Promise<string> {
  const url = `${API_BASE_URL}/api/runs/${id}/artifacts/${kind}`;
  try {
    const res = await fetch(url, { headers: { Accept: "text/plain, application/json" } });
    if (!res.ok) {
      throw new RelayApiError(`Failed to fetch artifact content from GET /api/runs/${id}/artifacts/${kind} (status: ${res.status})`, res.status, `/api/runs/${id}/artifacts/${kind}`, "GET");
    }
    return await res.text();
  } catch (err: any) {
    if (err instanceof RelayApiError) throw err;
    throw new RelayApiError(`Daemon unavailable fetching artifact content for run ${id} kind ${kind}: ${err.message}`, 503, `/api/runs/${id}/artifacts/${kind}`, "GET");
  }
}

export async function auditRun(id: string): Promise<RelayActionResponse> {
  return postJson<undefined, RelayActionResponse>(`/api/runs/${id}/audit`);
}

export async function approveCloseout(
  id: string,
  req: RelayActionRequest
): Promise<RelayActionResponse> {
  return postJson<RelayActionRequest, RelayActionResponse>(
    `/api/runs/${id}/approve-closeout`,
    req
  );
}

// Step 4: Audit / Close API methods

export interface SubmitManualAuditPayload {
  audit_packet_markdown: string;
  decision: RelayAuditDecisionValue;
  notes?: string;
}

export interface SubmitManualAuditResponse {
  success: boolean;
  runId: string;
  auditPacket: string;
  decision: RelayAuditDecisionValue;
  updatedAt: string;
}

export async function submitManualAuditPacket(
  id: string,
  payload: SubmitManualAuditPayload
): Promise<SubmitManualAuditResponse> {
  return postJson<SubmitManualAuditPayload, SubmitManualAuditResponse>(
    `/api/runs/${id}/audit/submit`,
    payload
  );
}

export interface AuditApprovePayload {
  decision: "accepted" | "accepted_with_warnings";
  notes?: string;
}

export interface AuditActionResponse {
  success: boolean;
  runId: string;
  status: string;
  lifecycleState: string;
  state?: string;
  updatedAt: string;
}

export async function approveAudit(
  id: string,
  payload: AuditApprovePayload
): Promise<AuditActionResponse> {
  return postJson<AuditApprovePayload, AuditActionResponse>(
    `/api/runs/${id}/audit/approve`,
    payload
  );
}

export interface AuditRevisionPayload {
  notes?: string;
  reason?: string;
}

export async function requestAuditRevision(
  id: string,
  payload?: AuditRevisionPayload
): Promise<AuditActionResponse> {
  return postJson<AuditRevisionPayload, AuditActionResponse>(
    `/api/runs/${id}/audit/request-revision`,
    payload || {}
  );
}

export interface PrepareCommitMessageResponse {
  success: boolean;
  runId: string;
  commitMessage: string;
  artifactPath: string;
  artifactKind: string;
}

export async function prepareCommitMessage(
  id: string
): Promise<PrepareCommitMessageResponse> {
  return postJson<undefined, PrepareCommitMessageResponse>(
    `/api/runs/${id}/audit/prepare-commit-message`
  );
}

export async function closeRun(
  id: string
): Promise<AuditActionResponse> {
  return postJson<undefined, AuditActionResponse>(
    `/api/runs/${id}/audit/close`
  );
}
