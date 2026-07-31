import { afterEach, describe, expect, it, vi } from "vitest";

import * as planApi from "./api";
import {
  getWorkflowPlan,
  getWorkflowPlanPass,
  listWorkflowPlans,
  validateWorkflowPlan,
} from "./api";

function response(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

const project = {
  projectId: "project-1",
  name: "Relay",
  status: "active",
};

const planSummary = {
  planId: "plan-1",
  project,
  featureSlug: "relay-specification-workflow-pivot",
  status: "active",
  canonicalSha256: "a".repeat(64),
  createdAt: "2026-07-08T00:00:00Z",
  updatedAt: "2026-07-08T00:00:00Z",
  passCount: 1,
  completedPassCount: 0,
  inProgressPassCount: 0,
  plannedPassCount: 1,
  currentPassId: "pass-1",
};

const pass = {
  passId: "pass-1",
  number: 1,
  name: "First",
  repoTarget: "relay",
  status: "planned",
  dependsOn: [],
  createdAt: "2026-07-08T00:00:00Z",
  updatedAt: "2026-07-08T00:00:00Z",
  runs: [],
};

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("canonical Plan API", () => {
  it("parses list, detail, and pass responses with concrete empty collections", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(response({ items: [planSummary], count: 1 }))
      .mockResolvedValueOnce(
        response({
          plan: planSummary,
          repositories: [],
          passes: [pass],
          artifacts: [],
        }),
      )
      .mockResolvedValueOnce(response(pass));
    vi.stubGlobal("fetch", fetchMock);

    const list = await listWorkflowPlans({ limit: 100 });
    const detail = await getWorkflowPlan("plan-1");
    const passDetail = await getWorkflowPlanPass("plan-1", "pass-1");

    expect(list.count).toBe(1);
    expect(list.plans[0]?.featureSlug).toBe("relay-specification-workflow-pivot");
    expect(detail.repositories).toEqual([]);
    expect(detail.artifacts).toEqual([]);
    expect(detail.passes[0]?.dependsOn).toEqual([]);
    expect(detail.passes[0]?.runs).toEqual([]);
    expect(passDetail.dependsOn).toEqual([]);
    expect(passDetail.runs).toEqual([]);
    expect(fetchMock.mock.calls.map(([url]) => String(url))).toEqual([
      expect.stringContaining("/api/plans?limit=100"),
      expect.stringContaining("/api/plans/plan-1"),
      expect.stringContaining("/api/plans/plan-1/passes/pass-1"),
    ]);
  });

  it("rejects null dependency collections instead of silently coercing malformed DTOs", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      response({
        plan: planSummary,
        repositories: [],
        passes: [{ ...pass, dependsOn: null }],
        artifacts: [],
      }),
    );
    vi.stubGlobal("fetch", fetchMock);

    await expect(getWorkflowPlan("plan-1")).rejects.toThrow(/dependsOn/);
  });

  it("uses the canonical validation endpoint without any Plan write endpoint", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      response({
        ok: true,
        status: "valid",
        kind: "plan",
        sha256: "b".repeat(64),
        diagnostics: [],
        notices: [],
      }),
    );
    vi.stubGlobal("fetch", fetchMock);

    const validation = await validateWorkflowPlan("feature.plan.json", "{}");

    expect(validation.sha256).toBe("b".repeat(64));
    expect(fetchMock.mock.calls[0][0]).toContain(
      "/api/canonical-artifacts/validate",
    );
    expect(fetchMock.mock.calls).toHaveLength(1);
  });

  // Legacy Plan write admission is retired: the client exposes no Plan
  // submission or Plan movement operation.
  it("exports no Plan submission or Plan movement operation", () => {
    expect(Object.keys(planApi)).not.toContain("submitWorkflowPlan");
    expect(Object.keys(planApi)).not.toContain("moveWorkflowPlan");
  });
});
