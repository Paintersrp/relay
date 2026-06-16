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

export async function executeRun(id: string): Promise<RelayActionResponse> {
  return postJson<undefined, RelayActionResponse>(`/api/runs/${id}/execute`);
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
