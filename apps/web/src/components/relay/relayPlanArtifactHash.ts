/**
 * relayPlanArtifactHash.ts
 *
 * Pure browser-side artifact hash helpers for the plan review workbench.
 *
 * These helpers assist with pre-filling the artifact SHA-256 field from the
 * current editor JSON. The backend is always authoritative; client-computed
 * hashes are advisory only.
 */

import type { PlannerPassPlan } from "@/features/relay-plans";

/**
 * Recursively stringify a value with object keys sorted lexicographically.
 * Arrays preserve insertion order. Produces a deterministic JSON string
 * suitable for use as a canonical hash input.
 */
export function stableStringify(value: unknown): string {
  if (value === null || value === undefined) {
    return JSON.stringify(value);
  }

  if (Array.isArray(value)) {
    const items = value.map((item) => stableStringify(item));
    return `[${items.join(",")}]`;
  }

  if (typeof value === "object") {
    const obj = value as Record<string, unknown>;
    const sortedKeys = Object.keys(obj).sort();
    const pairs = sortedKeys.map(
      (key) => `${JSON.stringify(key)}:${stableStringify(obj[key])}`,
    );
    return `{${pairs.join(",")}}`;
  }

  return JSON.stringify(value);
}

/**
 * Compute a sha256 hex digest of the given string using the browser's
 * SubtleCrypto API. Returns a string in the form `sha256:<lowercase-hex>`.
 *
 * This is a UI convenience helper. Backend validation is authoritative.
 */
export async function sha256String(value: string): Promise<string> {
  const encoded = new TextEncoder().encode(value);
  const buffer = await crypto.subtle.digest("SHA-256", encoded);
  const hex = Array.from(new Uint8Array(buffer))
    .map((byte) => byte.toString(16).padStart(2, "0"))
    .join("");
  return `sha256:${hex}`;
}

/**
 * Produce the canonical stable-stringified JSON representation of a
 * PlannerPassPlan for use as a hash input. Keys are sorted lexicographically
 * at every level; arrays preserve order.
 */
export function canonicalPlanJsonForHash(plan: PlannerPassPlan): string {
  return stableStringify(plan);
}

/**
 * Compute the sha256 hash of the canonical stable-stringified Plan JSON.
 * Returns `sha256:<hex>`.
 *
 * Treat the result as advisory; the backend re-validates the hash against
 * the artifact registered in the attempt creation request.
 */
export async function computePlanJsonSha256(
  plan: PlannerPassPlan,
): Promise<string> {
  return sha256String(canonicalPlanJsonForHash(plan));
}
