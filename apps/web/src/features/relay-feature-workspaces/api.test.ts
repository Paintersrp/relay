import { afterEach, describe, expect, it, vi } from "vitest";
import { getFeatureWorkspace, routeFeatureWorkspace } from "./api";

function response(body: unknown, status = 200): Response { return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } }); }
afterEach(() => vi.unstubAllGlobals());

describe("feature workspace transport", () => {
  it("normalizes the restart-safe workspace projection without a vault path", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(response({ workspace: { workspaceId: "workspace-1", featureSlug: "payments", state: "open", version: 2, createdAt: "", updatedAt: "" }, inputs: [], destinations: [], tickets: [], routes: [], authorityRevisions: [], sourceBasis: { status: "not_recorded", investigationCount: 0 } })));
    const detail = await getFeatureWorkspace("workspace-1");
    expect(detail.workspace.version).toBe(2);
    expect(detail.sourceBasis.status).toBe("not_recorded");
    expect(JSON.stringify(detail)).not.toContain("vault");
  });

  it("preserves the typed stale-write conflict for workspace controls", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(response({ error: "VERSION_CONFLICT", message: "reload" }, 409)));
    await expect(routeFeatureWorkspace("workspace-1", { expectedVersion: 1, sequence: 1, state: "ready" })).rejects.toMatchObject({ status: 409, errorShape: { error: "VERSION_CONFLICT" } });
  });
});
