import { API_BASE_URL, RelayApiError } from "@/features/relay-runs";
import type { RelayApiErrorShape } from "@/features/relay-runs/types";

import type {
  PlanDetailResponse,
  PlanListFilters,
  PlanListResponse,
  PlanPassDetailResponse,
  SubmitPlanRequest,
  SubmitPlanResponse,
  ValidatePlanRequest,
  ValidatePlanResponse,
} from "./types";

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
  return getPlanJson<PlanListResponse>(`/api/plans${query ? `?${query}` : ""}`);
}

export async function getPlan(planId: string): Promise<PlanDetailResponse> {
  return getPlanJson<PlanDetailResponse>(
    `/api/plans/${encodeURIComponent(planId)}`,
  );
}

export async function getPlanPass(
  planId: string,
  passId: string,
): Promise<PlanPassDetailResponse> {
  return getPlanJson<PlanPassDetailResponse>(
    `/api/plans/${encodeURIComponent(planId)}/passes/${encodeURIComponent(passId)}`,
  );
}
