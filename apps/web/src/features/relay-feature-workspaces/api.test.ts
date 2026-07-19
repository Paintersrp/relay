import { afterEach, describe, expect, it, vi } from "vitest";
import { completeFeatureWorkspace, getFeatureCompletionStatus, getFeatureWorkspace, recordFeatureAuthorityApproval, routeFeatureWorkspace } from "./api";

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

  it("projects completion blockers and sends explicit packet admission", async () => {
    const fetch = vi.fn()
      .mockResolvedValueOnce(response({ workspace: { workspaceId: "workspace-1", featureSlug: "payments", state: "open", version: 2, createdAt: "", updatedAt: "" }, gates: [{ name: "authority", ready: true }, { name: "audit", ready: false }] }))
      .mockResolvedValueOnce(response({ workspace: { workspaceId: "workspace-1", featureSlug: "payments", state: "open", version: 3, createdAt: "", updatedAt: "" }, decision: { completionDecisionId: "completion-1", authorityRevisionRowId: 3, sourceClosureRowId: 4, decision: "completed", createdAt: "" } }));
    vi.stubGlobal("fetch", fetch);

    const status = await getFeatureCompletionStatus("workspace-1");
    await completeFeatureWorkspace("workspace-1", { packetId: "packet-1", operationId: "local_operator.ticket_workflow", requiredDependencies: [{ class: "feature_workspace_completion", key: "workspace:workspace-1:version:2" }], expectedVersion: 2, operatorConfirmed: true });

    expect(status.gates).toContainEqual({ name: "audit", ready: false });
    expect(JSON.parse(fetch.mock.calls[1]?.[1]?.body as string)).toMatchObject({ packetId: "packet-1", operatorConfirmed: true });
  });

  it("records an approval and returns typed fields", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(response({ approval: { approvalId: "ga-approval-1", workspaceRowId: 1, artifactRowId: 10, retainedArtifactRowId: null, family: "requirements", artifactSha256: "a".repeat(64), operatorConfirmationEvidence: "operator confirmed", invalidatedByApprovalRowId: null, supersededByApprovalRowId: null, createdAt: "" }, workspace: { workspaceId: "workspace-1", featureSlug: "payments", state: "open", version: 3, createdAt: "", updatedAt: "" } })));
    const approval = await recordFeatureAuthorityApproval("workspace-1", { family: "requirements", artifactRowId: 10, artifactSha256: "a".repeat(64), operatorConfirmationEvidence: "operator confirmed" });
    expect(approval.approvalId).toBe("ga-approval-1");
    expect(approval.family).toBe("requirements");
    expect(approval.artifactSha256).toBe("a".repeat(64));
  });
});
