import { afterEach, describe, expect, it, vi } from "vitest";
import { completeFeatureWorkspace, getFeatureCompletionStatus, getFeatureWorkspace, getGuidedFeatureWorkspace, guidedFeatureWorkspaceAction, recordFeatureAuthorityApproval, routeFeatureWorkspace } from "./api";

function response(body: unknown, status = 200): Response { return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } }); }
afterEach(() => vi.unstubAllGlobals());

const workspace = { workspaceId: "workspace-1", featureSlug: "payments", state: "open", version: 2, createdAt: "", updatedAt: "" };
const guidedBody = {
  guided: {
    workspace,
    project: { projectId: "project-1", name: "Relay" },
    discovery: { state: "active", destination: "delivery", rationale: "rationale", continuation: "continue", currentness: "current" },
    authority: { currentRevisionNumber: 1, revisions: [{ revisionNumber: 1, layers: ["requirements"], historical: false }] },
    planning: { readiness: "current", status: "ready", recoveryCategory: "" },
    completion: { gates: [{ name: "authority", ready: true }], ready: true, recorded: false },
    ticketFrontier: { status: "open", summary: "Continue discovery", blockers: [], downstream: [] },
    downstream: { status: "delivery", summary: "Continue downstream" },
    prototypeQA: { status: "role-owned", summary: "Return after QA", requiredEvidence: [] },
    recovery: { blocked: false, summary: "No recovery", category: "", actions: [] },
    handoff: { available: false, instruction: "", returnGuidance: "Return here" },
    diagnostics: {
      history: { discoveryCurrentness: "current", status: "current_basis" },
      stale: { readiness: "current", owner: "", blockedOperation: "", effect: "", recoveryCategory: "" },
      discovery: { blockers: [], restorationActions: [], pendingIntegrations: [], activeOperations: [], routeMaterialOpen: false, requiredEvidence: [] },
    },
    availableActions: [{ action: "continue_discovery", primary: true, enabled: true, requiresConfirmation: true }],
    primaryAction: "continue_discovery",
  },
};

describe("feature workspace transport", () => {
  it("normalizes the restart-safe workspace projection without a vault path", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(response({ workspace, project: { projectId: "project-1", name: "Relay" }, inputs: [], destinations: [], tickets: [], routes: [], authorityRevisions: [], sourceBasis: { status: "not_recorded", investigationCount: 0 } })));
    const detail = await getFeatureWorkspace("workspace-1");
    expect(detail.workspace.version).toBe(2);
    expect(detail.sourceBasis.status).toBe("not_recorded");
    expect(JSON.stringify(detail)).not.toContain("vault");
  });

  it("parses the guided projection and posts the typed action payload", async () => {
    const fetch = vi.fn().mockResolvedValueOnce(response(guidedBody)).mockResolvedValueOnce(response(guidedBody));
    vi.stubGlobal("fetch", fetch);
    const detail = await getGuidedFeatureWorkspace("workspace-1");
    expect(detail.discovery.destination).toBe("delivery");
    expect(detail.availableActions[0]).toEqual({ action: "continue_discovery", primary: true, enabled: true, requiresConfirmation: true });
    await guidedFeatureWorkspaceAction("workspace-1", { expectedVersion: 2, action: "continue_discovery", confirmation: true, destination: "delivery" });
    expect(fetch.mock.calls[1]?.[0]).toContain("/api/feature-workspaces/workspace-1/guided/actions");
    expect(JSON.parse(fetch.mock.calls[1]?.[1]?.body as string)).toEqual({ expectedVersion: 2, action: "continue_discovery", confirmation: true, destination: "delivery" });
  });

  it("preserves the typed stale-write conflict for workspace controls", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(response({ error: "VERSION_CONFLICT", message: "reload" }, 409)));
    await expect(routeFeatureWorkspace("workspace-1", { expectedVersion: 1, sequence: 1, state: "ready" })).rejects.toMatchObject({ status: 409, errorShape: { error: "VERSION_CONFLICT" } });
  });

  it("projects completion blockers and sends direct expected-version confirmation", async () => {
    const fetch = vi.fn()
      .mockResolvedValueOnce(response({ workspace, gates: [{ name: "authority", ready: true }, { name: "audit", ready: false }] }))
      .mockResolvedValueOnce(response({ workspace: { ...workspace, version: 3 }, decision: { completionDecisionId: "completion-1", authorityRevisionRowId: 3, sourceClosureRowId: 4, decision: "completed", createdAt: "" } }));
    vi.stubGlobal("fetch", fetch);
    const status = await getFeatureCompletionStatus("workspace-1");
    await completeFeatureWorkspace("workspace-1", { expectedVersion: 2, operatorConfirmed: true });
    expect(status.gates).toContainEqual({ name: "audit", ready: false });
    expect(JSON.parse(fetch.mock.calls[1]?.[1]?.body as string)).toEqual({ expectedVersion: 2, operatorConfirmed: true });
  });

  it("records an approval and returns typed fields", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(response({ approval: { approvalId: "ga-approval-1", workspaceRowId: 1, artifactRowId: 10, retainedArtifactRowId: null, family: "requirements", artifactSha256: "a".repeat(64), operatorConfirmationEvidence: "operator confirmed", invalidatedByApprovalRowId: null, supersededByApprovalRowId: null, createdAt: "" }, workspace: { ...workspace, version: 3 } })));
    const approval = await recordFeatureAuthorityApproval("workspace-1", { family: "requirements", artifactRowId: 10, artifactSha256: "a".repeat(64), operatorConfirmationEvidence: "operator confirmed" });
    expect(approval.approvalId).toBe("ga-approval-1");
    expect(approval.family).toBe("requirements");
    expect(approval.artifactSha256).toBe("a".repeat(64));
  });
});
