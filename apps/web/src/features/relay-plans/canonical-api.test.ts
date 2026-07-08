import { afterEach, describe, expect, it, vi } from "vitest";

import {
  moveWorkflowPlan,
  submitWorkflowPlan,
  validateWorkflowPlan,
} from "./canonical-api";

function response(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("canonical Plan API", () => {
  it("submits exact canonical content with external Project metadata", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      response(
        {
          plan: {
            planId: "plan-1",
            featureSlug: "feature",
            status: "active",
            canonicalSha256: "a".repeat(64),
            project: {
              projectId: "project-1",
              name: "Relay",
              status: "active",
            },
            createdAt: "2026-07-08T00:00:00Z",
            updatedAt: "2026-07-08T00:00:00Z",
          },
          passes: [
            {
              passId: "pass-1",
              number: 1,
              name: "First",
              repoTarget: "relay",
              status: "planned",
            },
          ],
          artifacts: [],
        },
        201,
      ),
    );
    vi.stubGlobal("fetch", fetchMock);
    const canonicalContent = "{\n  \"schema_version\": \"1.0\"\n}\n";

    await submitWorkflowPlan({
      projectId: "project-1",
      fileName: "feature.plan.json",
      canonicalContent,
      expectedSha256: "a".repeat(64),
    });

    const request = fetchMock.mock.calls[0][1] as RequestInit;
    expect(JSON.parse(String(request.body))).toEqual({
      projectId: "project-1",
      fileName: "feature.plan.json",
      canonicalContent,
      expectedSha256: "a".repeat(64),
    });
  });

  it("uses canonical validation and atomic movement endpoints", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        response({
          ok: true,
          status: "valid",
          kind: "plan",
          sha256: "b".repeat(64),
          diagnostics: [],
          notices: [],
        }),
      )
      .mockResolvedValueOnce(
        response({
          planId: "plan-1",
          featureSlug: "feature",
          status: "active",
          canonicalSha256: "b".repeat(64),
          project: {
            projectId: "project-2",
            name: "Destination",
            status: "active",
          },
          createdAt: "2026-07-08T00:00:00Z",
          updatedAt: "2026-07-08T00:01:00Z",
        }),
      );
    vi.stubGlobal("fetch", fetchMock);

    await validateWorkflowPlan("feature.plan.json", "{}");
    const moved = await moveWorkflowPlan("plan-1", { projectId: "project-2" });

    expect(fetchMock.mock.calls[0][0]).toContain(
      "/api/canonical-artifacts/validate",
    );
    expect(fetchMock.mock.calls[1][0]).toContain(
      "/api/plans/plan-1/project",
    );
    expect((fetchMock.mock.calls[1][1] as RequestInit).method).toBe("PATCH");
    expect(moved.project.projectId).toBe("project-2");
  });
});
