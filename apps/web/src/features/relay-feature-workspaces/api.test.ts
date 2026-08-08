import { afterEach, describe, expect, it, vi } from "vitest";
import { completeFeatureWorkspace, getFeatureCompletionStatus, getFeatureWorkspace, getGuidedFeatureWorkspace, guidedFeatureWorkspaceAction, recordFeatureAuthorityApproval, routeFeatureWorkspace } from "./api";

function response(body: unknown, status = 200): Response { return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } }); }
afterEach(() => vi.unstubAllGlobals());

const workspace = { workspaceId: "workspace-1", featureSlug: "payments", state: "open", version: 2, createdAt: "", updatedAt: "" };
const guidedBody = {
  guided: {
    workspace,
    project: { projectId: "project-1", name: "Relay" },
    discovery: { state: "closed", destination: "direct_delivery_ticket", rationale: "rationale", continuation: "continue", currentness: "current", basis: "closure packet verified", reopenState: "none" },
    authority: { currentRevisionNumber: 1, layers: ["requirements"] },
    currentness: { readiness: "current", owner: "", blockedOperation: "", effect: "", recoveryCategory: "" },
    planning: { status: "promoted", candidateState: "none", reviewState: "none", approvalState: "none", promotionState: "promoted", candidateCount: 1, awaitingReview: 0, awaitingApproval: 0, awaitingPromotion: 0, promoted: 1, historicalCount: 0 },
    delivery: { frontier: [{ ticketId: "P5-T1", revisionNumber: 2, externalPriority: 60, repoTarget: "relay", branch: "main" }], selectionState: "none", packageState: "none", runState: "none", auditState: "none", remediationState: "none" },
    prototype: { runState: "none", cleanupState: "none", qaState: "none", evidenceState: "none", processOutcome: "" },
    completion: { gates: [{ name: "authority", ready: true }, { name: "audit", ready: false }], ready: false, recorded: false },
    recovery: { state: "none", category: "", available: [] },
    diagnostics: {
      stale: [], historical: [], discovery: [],
      delivery: ["remediation_open"], prototype: ["cleanup_pending"],
    },
    availableActions: [{ action: "select_delivery_ticket", primary: true, enabled: true, requiresConfirmation: true, handoff: "Select the current frontier Delivery Ticket server-side." }],
    primaryAction: "select_delivery_ticket",
    handoff: null,
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

  it("parses the guided projection frontier, closure, prototype, and diagnostics sections", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(response(guidedBody)));
    const detail = await getGuidedFeatureWorkspace("workspace-1");
    expect(detail.discovery.destination).toBe("direct_delivery_ticket");
    expect(detail.discovery.basis).toBe("closure packet verified");
    expect(detail.discovery.reopenState).toBe("none");
    expect(detail.delivery?.frontier).toEqual([{ ticketId: "P5-T1", revisionNumber: 2, externalPriority: 60, repoTarget: "relay", branch: "main" }]);
    expect(detail.ticketFrontier.status).toBe("1 delivery frontier ticket(s) ready");
    expect(detail.prototype?.processOutcome).toBe("");
    expect(detail.diagnostics.delivery).toContain("remediation_open");
    expect(detail.diagnostics.prototype).toContain("cleanup_pending");
    expect(detail.availableActions[0]).toEqual({ action: "select_delivery_ticket", primary: true, enabled: true, requiresConfirmation: true, handoff: "Select the current frontier Delivery Ticket server-side." });
  });

  it("posts only the server-selected primary action with expected version and confirmation", async () => {
    const fetch = vi.fn().mockResolvedValueOnce(response(guidedBody)).mockResolvedValueOnce(response(guidedBody));
    vi.stubGlobal("fetch", fetch);
    await getGuidedFeatureWorkspace("workspace-1");
    await guidedFeatureWorkspaceAction("workspace-1", { expectedVersion: 2, action: "select_delivery_ticket", confirmation: true });
    expect(fetch.mock.calls[1]?.[0]).toContain("/api/feature-workspaces/workspace-1/guided/actions");
    expect(JSON.parse(fetch.mock.calls[1]?.[1]?.body as string)).toEqual({ expectedVersion: 2, action: "select_delivery_ticket", confirmation: true });
    const posted = JSON.parse(fetch.mock.calls[1]?.[1]?.body as string);
    expect(Object.keys(posted).sort()).toEqual(["action", "confirmation", "expectedVersion"]);
    expect(JSON.stringify(posted)).not.toMatch(/(ticketId|packageId|runId|workspaceId|sha256|rowId)/i);
  });

  it("parses the owner-composed handoff transfer after a handoff action", async () => {
    const handoffBody = {
      guided: {
        ...guidedBody.guided,
        delivery: { frontier: [], selectionState: "active", packageState: "none", runState: "none", auditState: "none", remediationState: "none" },
        availableActions: [{ action: "prepare_package", primary: true, enabled: true, requiresConfirmation: false, handoff: "Prepare the execution package through the existing package owner." }],
        primaryAction: "prepare_package",
        handoff: {
          role: "prepare_package", summary: "The selected Delivery Ticket is identified through the delivery owner.", resumeRoute: "/feature-workspaces/workspace-1/guided",
          context: { owner: "execution_package_preparation" },
          transfer: { frontier: [], members: [], authorityLayers: [], route: [], ticket: { ticketId: "P5-T1", revisionNumber: 2, readiness: ["design_admitted"], operationId: "planner.ticket_design_brief" }, package: null, run: null, audit: null, remediation: null, prototype: null },
        },
      },
    };
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(response(handoffBody)));
    const detail = await getGuidedFeatureWorkspace("workspace-1");
    expect(detail.handoff.available).toBe(true);
    expect(detail.handoff.instruction).toContain("selected Delivery Ticket");
    expect(detail.handoff.transfer?.ticket).toEqual({ ticketId: "P5-T1", revisionNumber: 2, readiness: ["design_admitted"], operationId: "planner.ticket_design_brief" });
    expect(detail.handoff.transfer?.package).toBeUndefined();
  });

  it("rejects an unknown guided action from the projection", async () => {
    const invalid = { guided: { ...guidedBody.guided, availableActions: [{ action: "completion_recorded", primary: true, enabled: true, requiresConfirmation: false }], primaryAction: "completion_recorded" } };
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(response(invalid)));
    await expect(getGuidedFeatureWorkspace("workspace-1")).rejects.toMatchObject({ status: 502 });
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
