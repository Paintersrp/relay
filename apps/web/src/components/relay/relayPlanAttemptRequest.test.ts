import { describe, expect, it } from "vitest";
import { buildPlanAttemptRequest, buildRevisionRequest } from "./relayPlanAttemptRequest";
import type { PlannerPassPlan, PlanReviewSettingsAPI } from "@/features/relay-plans";

// ─── Fixtures ─────────────────────────────────────────────────────────────────

function makePlan(overrides?: Partial<PlannerPassPlan["plan_meta"]>): PlannerPassPlan {
  return {
    plan_meta: {
      plan_id: "plan-001",
      schema_version: "1.0.0",
      created_at: "2026-01-01T00:00:00Z",
      title: "Original Title",
      goal: "Test goal",
      repo_target: "owner/repo",
      branch_context: "main",
      status: "active",
      ...overrides,
    },
    source_intent: { summary: "Test summary" },
    passes: [
      {
        pass_id: "PASS-001",
        sequence: 1,
        name: "First Pass",
        goal: "First pass goal",
        intended_execution_scope: ["src/"],
        non_goals: [],
        dependencies: [],
        status: "planned",
      },
    ],
  };
}

function makeSettings(
  overrides?: Partial<PlanReviewSettingsAPI>,
): PlanReviewSettingsAPI {
  return {
    projectId: "proj-abc",
    driftReviewMode: "external",
    modelTier: "high_assurance",
    manualModelCallWarning: "",
    automaticReviewEnabled: false,
    externalReviewSupported: true,
    ...overrides,
  };
}

const BASE_INPUT = {
  planJsonArtifactPath: "handoffs/planner/2026-01-01_plan.json",
  planJsonArtifactSha256: "sha256:aabbcc",
  literalUserRequest: "The user request text",
  intentConstraints: ["Do not change backend APIs"],
} as const;

// ─── buildPlanAttemptRequest ──────────────────────────────────────────────────

describe("buildPlanAttemptRequest", () => {
  it("uses settings driftReviewMode and modelTier exactly as supplied", () => {
    const plan = makePlan();
    const settings = makeSettings({ driftReviewMode: "external", modelTier: "high_assurance" });
    const request = buildPlanAttemptRequest({ plan, settings, ...BASE_INPUT });
    expect(request.driftReviewMode).toBe("external");
    expect(request.modelTier).toBe("high_assurance");
  });

  it("does not fall back to 'manual' or 'standard' when settings have different values", () => {
    const plan = makePlan();
    // Settings explicitly set to non-fallback values; the helper must not override them.
    const settings = makeSettings({ driftReviewMode: "automatic", modelTier: "economy" });
    const request = buildPlanAttemptRequest({ plan, settings, ...BASE_INPUT });
    expect(request.driftReviewMode).not.toBe("manual");
    expect(request.modelTier).not.toBe("standard");
    expect(request.driftReviewMode).toBe("automatic");
    expect(request.modelTier).toBe("economy");
  });

  it("uses the supplied plan in rawPlanJson.content (not a stale reference)", () => {
    const plan = makePlan({ title: "Edited Title" });
    const settings = makeSettings();
    const request = buildPlanAttemptRequest({ plan, settings, ...BASE_INPUT });
    // Access via type assertion: RawPlanJSONAPI.content is typed as unknown
    const content = request.rawPlanJson.content as PlannerPassPlan;
    expect(content.plan_meta.title).toBe("Edited Title");
  });

  it("sets rawPlanJson.contentHash to the supplied sha256", () => {
    const plan = makePlan();
    const settings = makeSettings();
    const request = buildPlanAttemptRequest({ plan, settings, ...BASE_INPUT });
    expect(request.rawPlanJson.contentHash).toBe(BASE_INPUT.planJsonArtifactSha256);
  });

  it("trims planJsonArtifactPath in planArtifactRef.path", () => {
    const plan = makePlan();
    const settings = makeSettings();
    const request = buildPlanAttemptRequest({
      plan,
      settings,
      ...BASE_INPUT,
      planJsonArtifactPath: "  handoffs/plan.json  ",
    });
    expect(request.planArtifactRef.path).toBe("handoffs/plan.json");
  });

  it("omits optionalMarkdownRef when path is empty", () => {
    const plan = makePlan();
    const settings = makeSettings();
    const request = buildPlanAttemptRequest({
      plan,
      settings,
      ...BASE_INPUT,
      planMarkdownArtifactPath: "",
      planMarkdownArtifactSha256: "sha256:xyz",
    });
    expect(request.optionalMarkdownRef).toBeUndefined();
  });

  it("includes optionalMarkdownRef when both path and sha256 are provided", () => {
    const plan = makePlan();
    const settings = makeSettings();
    const request = buildPlanAttemptRequest({
      plan,
      settings,
      ...BASE_INPUT,
      planMarkdownArtifactPath: "handoffs/plan.md",
      planMarkdownArtifactSha256: "sha256:dd",
    });
    expect(request.optionalMarkdownRef).toBeDefined();
    expect(request.optionalMarkdownRef?.path).toBe("handoffs/plan.md");
    expect(request.optionalMarkdownRef?.artifactKind).toBe("planner-pass-plan-markdown");
  });

  it("captures intentPacket.literalUserRequest and constraints", () => {
    const plan = makePlan();
    const settings = makeSettings();
    const request = buildPlanAttemptRequest({ plan, settings, ...BASE_INPUT });
    expect(request.intentPacket.literalUserRequest).toBe("The user request text");
    expect(request.intentPacket.constraints).toEqual(["Do not change backend APIs"]);
  });

  it("sets intentPacket.redactionStatus to verified_no_secrets", () => {
    const plan = makePlan();
    const settings = makeSettings();
    const request = buildPlanAttemptRequest({ plan, settings, ...BASE_INPUT });
    expect(request.intentPacket.redactionStatus).toBe("verified_no_secrets");
  });
});

// ─── buildRevisionRequest ─────────────────────────────────────────────────────

describe("buildRevisionRequest", () => {
  it("uses the edited plan's title in rawPlanJson.content (revision/current-plan regression)", () => {
    // This proves revision requests use the current editor plan, not a stale plan.
    const editedPlan = makePlan({ title: "Edited" });
    const request = buildRevisionRequest({ plan: editedPlan, ...BASE_INPUT });
    const content = request.rawPlanJson.content as PlannerPassPlan;
    expect(content.plan_meta.title).toBe("Edited");
  });

  it("does NOT include driftReviewMode or modelTier (backend inherits from attempt)", () => {
    const plan = makePlan();
    const request = buildRevisionRequest({ plan, ...BASE_INPUT });
    // RevisePlanAttemptRequest type does not include these fields;
    // confirm the returned object doesn't carry them.
    const req = request as any;
    expect(req["driftReviewMode"]).toBeUndefined();
    expect(req["modelTier"]).toBeUndefined();
  });

  it("captures current plan summary in intentPacket", () => {
    const plan = makePlan();
    const request = buildRevisionRequest({ plan, ...BASE_INPUT });
    expect(request.intentPacket.summary).toBe("Test summary");
  });
});

// ─── No-fallback guarantee: round-trip ───────────────────────────────────────

describe("no manual/standard fallback guarantee", () => {
  it("request built with 'disabled'/'economy' settings has no 'manual' or 'standard' string in policy fields", () => {
    const plan = makePlan();
    const settings = makeSettings({ driftReviewMode: "disabled", modelTier: "economy" });
    const request = buildPlanAttemptRequest({ plan, settings, ...BASE_INPUT });
    // Verify neither fallback string appears in the policy fields.
    expect(request.driftReviewMode).not.toBe("manual");
    expect(request.modelTier).not.toBe("standard");
  });

  it("request fields reflect whatever settings supply, not hard-coded fallbacks", () => {
    // Exhaustive spot check for each non-fallback tier value.
    const tiers: Array<PlanReviewSettingsAPI["modelTier"]> = [
      "economy",
      "high_assurance",
      "auto_escalate",
    ];
    for (const tier of tiers) {
      const settings = makeSettings({ driftReviewMode: "automatic", modelTier: tier });
      const plan = makePlan();
      const req = buildPlanAttemptRequest({ plan, settings, ...BASE_INPUT });
      expect(req.modelTier).toBe(tier);
    }
  });
});
