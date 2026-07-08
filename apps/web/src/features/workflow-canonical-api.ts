import { API_BASE_URL, RelayApiError } from "@/features/relay-runs/api";
import type { RelayApiErrorShape } from "@/features/relay-runs/types";

export type WorkflowHttpMethod = "GET" | "POST" | "PATCH";
export type WorkflowJsonRecord = Record<string, unknown>;

export function workflowApiUrl(path: string): string {
  if (!path.startsWith("/api/")) {
    throw new Error(`Invalid workflow API path: ${path}`);
  }
  return `${API_BASE_URL}${path}`;
}

export function malformedWorkflowResponse(
  method: WorkflowHttpMethod,
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

export function asWorkflowRecord(
  value: unknown,
  method: WorkflowHttpMethod,
  path: string,
  context: string,
): WorkflowJsonRecord {
  if (value && typeof value === "object" && !Array.isArray(value)) {
    return value as WorkflowJsonRecord;
  }
  return malformedWorkflowResponse(method, path, `${context} must be an object`);
}

export function requiredWorkflowString(
  record: WorkflowJsonRecord,
  field: string,
  method: WorkflowHttpMethod,
  path: string,
  context: string,
  allowEmpty = false,
): string {
  const value = record[field];
  if (typeof value !== "string") {
    return malformedWorkflowResponse(
      method,
      path,
      `${context}.${field} must be a string`,
    );
  }
  if (!allowEmpty && value.trim().length === 0) {
    return malformedWorkflowResponse(
      method,
      path,
      `${context}.${field} must not be blank`,
    );
  }
  return value;
}

export function optionalWorkflowString(
  record: WorkflowJsonRecord,
  field: string,
  method: WorkflowHttpMethod,
  path: string,
  context: string,
): string | undefined {
  const value = record[field];
  if (value === undefined) return undefined;
  if (typeof value !== "string" || value.trim().length === 0) {
    return malformedWorkflowResponse(
      method,
      path,
      `${context}.${field} must be a nonblank string when present`,
    );
  }
  return value;
}

export function optionalEmptyWorkflowString(
  record: WorkflowJsonRecord,
  field: string,
  method: WorkflowHttpMethod,
  path: string,
  context: string,
): string {
  const value = record[field];
  if (value === undefined) return "";
  if (typeof value !== "string") {
    return malformedWorkflowResponse(
      method,
      path,
      `${context}.${field} must be a string when present`,
    );
  }
  return value;
}

export function requiredWorkflowInteger(
  record: WorkflowJsonRecord,
  field: string,
  method: WorkflowHttpMethod,
  path: string,
  context: string,
  minimum = 0,
): number {
  const value = record[field];
  if (!Number.isInteger(value) || (value as number) < minimum) {
    return malformedWorkflowResponse(
      method,
      path,
      `${context}.${field} must be an integer greater than or equal to ${minimum}`,
    );
  }
  return value as number;
}

export function requiredWorkflowBoolean(
  record: WorkflowJsonRecord,
  field: string,
  method: WorkflowHttpMethod,
  path: string,
  context: string,
): boolean {
  const value = record[field];
  if (typeof value !== "boolean") {
    return malformedWorkflowResponse(
      method,
      path,
      `${context}.${field} must be a boolean`,
    );
  }
  return value;
}

export function requiredWorkflowArray(
  record: WorkflowJsonRecord,
  field: string,
  method: WorkflowHttpMethod,
  path: string,
  context: string,
): unknown[] {
  const value = record[field];
  if (!Array.isArray(value)) {
    return malformedWorkflowResponse(
      method,
      path,
      `${context}.${field} must be an array`,
    );
  }
  return value;
}

function parseErrorShape(text: string): RelayApiErrorShape | undefined {
  if (!text) return undefined;
  try {
    const value = JSON.parse(text) as unknown;
    if (!value || typeof value !== "object" || Array.isArray(value)) return undefined;
    const record = value as WorkflowJsonRecord;
    if (typeof record.error !== "string" || typeof record.message !== "string") {
      return undefined;
    }
    return value as RelayApiErrorShape;
  } catch {
    return undefined;
  }
}

export async function requestWorkflowJson<T>(
  method: WorkflowHttpMethod,
  path: string,
  body?: unknown,
): Promise<T> {
  const headers: Record<string, string> = { Accept: "application/json" };
  if (body !== undefined) headers["Content-Type"] = "application/json";

  try {
    const response = await fetch(workflowApiUrl(path), {
      method,
      headers,
      body: body === undefined ? undefined : JSON.stringify(body),
    });
    const text = await response.text();

    if (!response.ok) {
      const errorShape = parseErrorShape(text);
      throw new RelayApiError(
        errorShape?.message ||
          `${method} ${path} failed with status ${response.status}`,
        response.status,
        path,
        method,
        errorShape,
      );
    }

    if (text.length === 0) {
      return malformedWorkflowResponse(
        method,
        path,
        `status ${response.status} requires a JSON response body`,
      );
    }

    try {
      return JSON.parse(text) as T;
    } catch (error) {
      const message = error instanceof Error ? error.message : "Invalid JSON";
      throw new RelayApiError(
        `Malformed JSON response from ${method} ${path}: ${message}`,
        response.status,
        path,
        method,
      );
    }
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
