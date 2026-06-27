/**
 * relayPlanAttemptRequest.ts
 *
 * Pure helper for constructing CreatePlanAttemptWithIntentRequest / RevisePlanAttemptRequest
 * payloads from validated plan and loaded settings state.
 *
 * Constraints enforced here:
 *  - driftReviewMode and modelTier come exclusively from loaded settings; no hard-coded fallbacks.
 *  - rawPlanJson.content is always the caller-supplied plan (current editor parse), never stale state.
 *
 * These helpers have no side effects and no React or API dependencies, making them
 * directly testable with plain Vitest without component-testing infrastructure.
 */

import type {
  CreatePlanAttemptWithIntentRequest,
  PlannerPassPlan,
  PlanReviewSettingsAPI,
  RevisePlanAttemptRequest,
} from "@/features/relay-plans";

// ─── Public Input Shapes ──────────────────────────────────────────────────────

export interface BuildPlanAttemptRequestInput {
  /** The current, freshly-parsed editor plan. Must never be stale cached state. */
  plan: PlannerPassPlan;
  /** Project review settings loaded for the exact current project ID. */
  settings: PlanReviewSettingsAPI;
  planJsonArtifactPath: string;
  planJsonArtifactSha256: string;
  planMarkdownArtifactPath?: string;
  planMarkdownArtifactSha256?: string;
  literalUserRequest: string;
  intentConstraints: readonly string[];
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

function buildArtifactRef(
  path: string,
  sha256: string,
  artifactKind: "planner-pass-plan-json" | "planner-pass-plan-markdown",
) {
  return { path: path.trim(), sha256: sha256.trim(), artifactKind };
}

function buildIntentPacket(
  plan: PlannerPassPlan,
  planJsonArtifactPath: string,
  literalUserRequest: string,
  intentConstraints: readonly string[],
) {
  return {
    summary: plan.source_intent.summary,
    literalUserRequest: literalUserRequest.trim(),
    constraints: [...intentConstraints],
    source: {
      capturedFrom: "planner_chat" as const,
      capturedBy: "relay-plan-review-ui",
      sourceArtifactPath: planJsonArtifactPath.trim(),
    },
    redactionStatus: "verified_no_secrets" as const,
  };
}

// ─── Public Functions ─────────────────────────────────────────────────────────

/**
 * Build a CreatePlanAttemptWithIntentRequest from freshly-parsed plan + loaded settings.
 *
 * driftReviewMode and modelTier are taken directly from `input.settings`; there are
 * no fallback string literals. If settings are not loaded for the current project,
 * the caller must not invoke this function.
 */
export function buildPlanAttemptRequest(
  input: BuildPlanAttemptRequestInput,
): CreatePlanAttemptWithIntentRequest {
  const trimmedJsonPath = input.planJsonArtifactPath.trim();
  const trimmedJsonSha = input.planJsonArtifactSha256.trim();

  const optionalMarkdownRef =
    input.planMarkdownArtifactPath?.trim() && input.planMarkdownArtifactSha256?.trim()
      ? buildArtifactRef(
          input.planMarkdownArtifactPath,
          input.planMarkdownArtifactSha256,
          "planner-pass-plan-markdown",
        )
      : undefined;

  return {
    planArtifactRef: buildArtifactRef(trimmedJsonPath, trimmedJsonSha, "planner-pass-plan-json"),
    optionalMarkdownRef,
    rawPlanJson: {
      content: input.plan,
      contentHash: trimmedJsonSha,
    },
    // Policy fields come from loaded settings only — no "manual"/"standard" fallbacks.
    driftReviewMode: input.settings.driftReviewMode,
    modelTier: input.settings.modelTier,
    intentPacket: buildIntentPacket(
      input.plan,
      trimmedJsonPath,
      input.literalUserRequest,
      input.intentConstraints,
    ),
  };
}

/**
 * Build a RevisePlanAttemptRequest from a freshly-validated current editor plan.
 *
 * Note: revision requests do not carry driftReviewMode / modelTier \u2014 the backend
 * inherits those from the original attempt. Callers must validate the plan before
 * calling this function.
 */
export function buildRevisionRequest(
  input: Omit<BuildPlanAttemptRequestInput, "settings">,
): RevisePlanAttemptRequest {
  const trimmedJsonPath = input.planJsonArtifactPath.trim();
  const trimmedJsonSha = input.planJsonArtifactSha256.trim();

  const optionalMarkdownRef =
    input.planMarkdownArtifactPath?.trim() && input.planMarkdownArtifactSha256?.trim()
      ? buildArtifactRef(
          input.planMarkdownArtifactPath,
          input.planMarkdownArtifactSha256,
          "planner-pass-plan-markdown",
        )
      : undefined;

  return {
    planArtifactRef: buildArtifactRef(trimmedJsonPath, trimmedJsonSha, "planner-pass-plan-json"),
    optionalMarkdownRef,
    rawPlanJson: {
      content: input.plan,
      contentHash: trimmedJsonSha,
    },
    intentPacket: buildIntentPacket(
      input.plan,
      trimmedJsonPath,
      input.literalUserRequest,
      input.intentConstraints,
    ),
  };
}
