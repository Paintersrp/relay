// @vitest-environment jsdom
import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClientProvider, QueryClient } from "@tanstack/react-query";
import { RelayFeatureWorkspaceDetail } from "./RelayFeatureWorkspaceDetail";
import type { GuidedFeatureDetail } from "@/features/relay-feature-workspaces";
import { RelayApiError } from "@/features/workflow-api";

const mocks = vi.hoisted(() => ({ action: vi.fn() }));

vi.mock("@/features/relay-feature-workspaces", async () => {
  const actual = await vi.importActual<typeof import("@/features/relay-feature-workspaces")>("@/features/relay-feature-workspaces");
  return { ...actual, actOnGuidedFeatureWorkspace: mocks.action };
});

vi.mock("@tanstack/react-router", () => ({
  Link: ({ children, to, params }: any) => <a href={to} data-project-id={params?.projectId ?? ""}>{children}</a>,
}));

function wrapper({ children }: { children: React.ReactNode }) {
  return <QueryClientProvider client={new QueryClient({ defaultOptions: { mutations: { retry: false } } })}>{children}</QueryClientProvider>;
}

const base: GuidedFeatureDetail = {
  workspace: { workspaceId: "workspace-1", featureSlug: "payments", state: "open", version: 2, createdAt: "", updatedAt: "" },
  project: { projectId: "project-1", name: "Relay" },
  discovery: { state: "active", destination: "delivery", rationale: "Clarify the supported payment path.", continuation: "Resolve the remaining discovery questions.", currentness: "current", basis: "closure packet verified", reopenState: "none" },
  authority: { currentRevisionNumber: 1, revisions: [{ revisionNumber: 1, layers: ["requirements", "design"], historical: false }] },
  planning: { readiness: "current", status: "ready", recoveryCategory: "" },
  completion: { gates: [{ name: "authority", ready: true }, { name: "audit", ready: false }], ready: false, recorded: false },
  delivery: { frontier: [{ ticketId: "P5-T1", revisionNumber: 2, externalPriority: 60, repoTarget: "relay", branch: "main" }], selectionState: "none", packageState: "none", runState: "none", auditState: "none", remediationState: "none" },
  prototype: { runState: "none", cleanupState: "none", qaState: "prepared", evidenceState: "none", processOutcome: "" },
  ticketFrontier: { status: "blocked", summary: "Resolve the remaining discovery frontier.", blockers: ["missing evidence"], downstream: [] },
  downstream: { status: "delivery", summary: "Continue after discovery closure." },
  prototypeQA: { status: "role-owned", summary: "Return after prototype and QA.", requiredEvidence: ["approval"] },
  recovery: { blocked: false, summary: "No recovery required.", category: "", actions: ["collect evidence"] },
  handoff: { available: false, instruction: "", returnGuidance: "Return to this workspace." },
  diagnostics: {
    history: { discoveryCurrentness: "current", status: "current_basis" },
    stale: { readiness: "current", owner: "", blockedOperation: "", effect: "", recoveryCategory: "" },
    staleItems: [],
    historical: ["historical_basis_requires_recovery"],
    discovery: { blockers: ["missing evidence"], restorationActions: ["collect evidence"], pendingIntegrations: [], activeOperations: [], routeMaterialOpen: false, requiredEvidence: ["approval"] },
    delivery: ["remediation_open"],
    prototype: ["cleanup_pending"],
    integrity: {
      discovery: {
        currentRevisionId: "discovery-1",
        currentPacket: { closurePacketId: "closure-1", sha256: "sha-closure-1" },
        history: [{ revisionId: "discovery-1", revisionNumber: 1, closurePacketId: "closure-1", packetSha256: "sha-closure-1", predecessorId: "", historical: false }],
        reopenEvents: [],
      },
      authority: [{ authorityRevisionId: "authority-1", revisionNumber: 1, historical: false, layers: [{ kind: "requirements", artifactId: "artifact-1", sha256: "sha-layer-1", sourceClosureId: "closure-source-1" }] }],
      planning: [{ candidateId: "candidate-1", family: "requirements", artifactId: "artifact-candidate-1", sha256: "sha-candidate-1", sizeBytes: 12, historical: false, promoted: true, approvals: ["candidate-approval-1"] }],
      delivery: {
        frontier: [{ ticketId: "P5-T1", revisionNumber: 2 }],
        selection: { selectionId: "selection-1", state: "active", ticketId: "P5-T1", revisionNumber: 2 },
        package: { packageId: "package-1", sha256: "sha-package-1", approvalId: "pkg-approval-1" },
        run: { runId: "run-1", packageId: "package-1", repoTarget: "relay", branch: "main", baseCommit: "base-1" },
        audit: { auditPacketId: "packet-audit-1", auditDecisionId: "audit-1", auditedCommit: "commit-1" },
        remediation: { seedIds: ["remediation-1"] },
      },
      prototype: {
        runId: "prototype-run-1",
        runState: "approved",
        proposalId: "prototype-proposal-1",
        authorizationId: "prototype-authorization-1",
        approvalId: "prototype-approval-1",
        discoveryRevisionId: "discovery-1",
        cleanup: [{ cleanupObligationId: "prototype-cleanup-1", kind: "worktree", status: "complete" }],
        qaPackets: [{ qaPacketId: "prototype-qa-packet-1", status: "admitted", admissionId: "prototype-qa-admission-1", evidence: [{ qaEvidenceId: "prototype-qa-evidence-1", semanticRole: "result-envelope", sha256: "sha-evidence-1", sizeBytes: 4, mediaType: "application/json" }] }],
      },
      diagnostics: [{ domain: "delivery", condition: "unavailable" }, { domain: "delivery.package", condition: "inconsistent" }],
    },
  },
  availableActions: [{ action: "continue_discovery", primary: true, enabled: true, requiresConfirmation: true }],
  primaryAction: "continue_discovery",
};

describe("RelayFeatureWorkspaceDetail", () => {
  it("preserves visible navigation back to the owning project", () => {
    render(<RelayFeatureWorkspaceDetail detail={base} />, { wrapper });
    const projectLinks = screen.getAllByRole("link", { name: /Relay/ });
    expect(projectLinks.length).toBeGreaterThan(0);
    for (const link of projectLinks) expect(link).toHaveAttribute("data-project-id", "project-1");
  });

  it("renders the source-projection semantic sections from discovery through diagnostics", () => {
    render(<RelayFeatureWorkspaceDetail detail={base} />, { wrapper });
    expect(screen.getByRole("heading", { name: "Discovery" })).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "History" })).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Planning and currentness" })).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Delivery" })).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Prototype and QA" })).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Blockers and recovery" })).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Authority" })).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Completion and closing" })).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Handoff and return" })).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Next guided action" })).toBeInTheDocument();
    // Server projection semantics render without client lifecycle derivation.
    expect(screen.getByText("closure packet verified")).toBeInTheDocument();
    expect(screen.getByText("P5-T1 v2 (priority 60, relay @ main)")).toBeInTheDocument();
    expect(screen.getByText("historical_basis_requires_recovery")).toBeInTheDocument();
    expect(screen.getByText("remediation_open")).toBeInTheDocument();
    expect(screen.getByText("cleanup_pending")).toBeInTheDocument();
    expect(screen.getByText("Diagnostics")).toBeInTheDocument();
  });

  it("renders the backend needs-revision count as read-only planning state", () => {
    render(<RelayFeatureWorkspaceDetail detail={{ ...base, planning: { ...base.planning, needsRevision: 2 } }} />, { wrapper });
    const planning = screen.getByRole("region", { name: "Planning and currentness" });
    expect(within(planning).getByText("Needs revision")).toBeInTheDocument();
    expect(within(planning).getByText("2")).toBeInTheDocument();
    expect(within(planning).queryByRole("textbox")).not.toBeInTheDocument();
  });

  it("renders exactly one primary action and no raw workspace input controls", () => {
    render(<RelayFeatureWorkspaceDetail detail={base} />, { wrapper });
    expect(screen.getAllByRole("button")).toHaveLength(1);
    expect(screen.getByRole("button", { name: "Continue discovery" })).toBeInTheDocument();
    expect(screen.queryAllByRole("textbox")).toHaveLength(0);
    expect(screen.queryAllByRole("combobox")).toHaveLength(0);
  });

  it("requires explicit confirmation and sends only the typed guided action", async () => {
    const user = userEvent.setup();
    mocks.action.mockResolvedValueOnce(base);
    render(<RelayFeatureWorkspaceDetail detail={base} />, { wrapper });
    const button = screen.getByRole("button", { name: "Continue discovery" });
    expect(button).toBeDisabled();
    await user.click(button);
    expect(mocks.action).not.toHaveBeenCalled();
    await user.click(screen.getByRole("checkbox", { name: "Confirm guided action" }));
    expect(button).toBeEnabled();
    await user.click(button);
    expect(mocks.action).toHaveBeenCalledWith("workspace-1", { expectedVersion: 2, action: "continue_discovery", confirmation: true });
  });

  it("shows typed mutation feedback for a stale guided action", async () => {
    const user = userEvent.setup();
    mocks.action.mockRejectedValueOnce(new RelayApiError("stale", 409, "/guided/actions", "POST", { error: "VERSION_CONFLICT", message: "stale" }));
    render(<RelayFeatureWorkspaceDetail detail={base} />, { wrapper });
    await user.click(screen.getByRole("checkbox", { name: "Confirm guided action" }));
    await user.click(screen.getByRole("button", { name: "Continue discovery" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(/changed in another session/);
  });

  it("explains a server-blocked primary action with a semantic status", () => {
    const blocked = { ...base, availableActions: [{ action: "continue_discovery" as const, primary: true, enabled: false, requiresConfirmation: true }] };
    render(<RelayFeatureWorkspaceDetail detail={blocked} />, { wrapper });
    expect(screen.getByRole("status")).toHaveTextContent(/blocked by the current guided workspace state/);
    expect(screen.getByRole("button", { name: "Continue discovery" })).toBeDisabled();
  });

  it("renders the owner-composed operation transfer returned by a handoff action", () => {
    const handedOff = {
      ...base,
      availableActions: [{ action: "author_delivery_ticket" as const, primary: true, enabled: true, requiresConfirmation: false, handoff: "Enter the Delivery Ticket authoring operation, then return here when the Ticket is ready for selection." }],
      primaryAction: "author_delivery_ticket" as const,
      handoff: {
        available: true,
        instruction: "Enter the Delivery Ticket authoring operation through the delivery owner.",
        returnGuidance: "Return here when the resulting Ticket is ready for selection.",
        transfer: {
          frontier: [], members: [], authorityLayers: [],
          ticket: { ticketId: "", revisionNumber: 0, readiness: [], operationId: "planner.delivery_ticket" },
          package: undefined, run: undefined, audit: undefined, remediation: undefined, prototype: undefined,
        },
      },
    };
    render(<RelayFeatureWorkspaceDetail detail={handedOff} />, { wrapper });
    expect(screen.getByText("Operation transfer")).toBeInTheDocument();
    expect(screen.getByText(/planner\.delivery_ticket/)).toBeInTheDocument();
    expect(screen.getAllByRole("button")).toHaveLength(1);
    expect(screen.getByRole("button", { name: "Author Delivery Ticket" })).toBeEnabled();
  });

  it("hides non-primary action selectors and posts the primary action only", async () => {
    const user = userEvent.setup();
    const multi = {
      ...base,
      availableActions: [
        { action: "author_requirements" as const, primary: true, enabled: true, requiresConfirmation: true },
        { action: "continue_discovery" as const, primary: false, enabled: true, requiresConfirmation: false },
      ],
      primaryAction: "author_requirements" as const,
    };
    mocks.action.mockResolvedValueOnce(multi);
    render(<RelayFeatureWorkspaceDetail detail={multi} />, { wrapper });
    expect(screen.getAllByRole("button")).toHaveLength(1);
    expect(screen.getByRole("button", { name: "Author Requirements" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Continue discovery" })).not.toBeInTheDocument();
    await user.click(screen.getByRole("checkbox", { name: "Confirm guided action" }));
    await user.click(screen.getByRole("button", { name: "Author Requirements" }));
    expect(mocks.action).toHaveBeenCalledWith("workspace-1", { expectedVersion: 2, action: "author_requirements", confirmation: true });
  });

  it("structurally renders the typed integrity identities under Diagnostics", () => {
    render(<RelayFeatureWorkspaceDetail detail={base} />, { wrapper });
    expect(screen.getByText("Integrity identities")).toBeInTheDocument();
    expect(screen.getByText("Integrity discovery")).toBeInTheDocument();
    expect(screen.getAllByText("discovery-1").length).toBeGreaterThan(0);
    expect(screen.getAllByText("closure-1").length).toBeGreaterThan(0);
    expect(screen.getByText("authority-1")).toBeInTheDocument();
    expect(screen.getByText("artifact-1")).toBeInTheDocument();
    expect(screen.getByText("candidate-1")).toBeInTheDocument();
    expect(screen.getByText("package-1 (sha-package-1)")).toBeInTheDocument();
    expect(screen.getByText("pkg-approval-1")).toBeInTheDocument();
    expect(screen.getByText(/selection-1 \(active; P5-T1 v2\)/)).toBeInTheDocument();
    expect(screen.getByText(/run-1 \(package package-1; relay @ main, base base-1\)/)).toBeInTheDocument();
    expect(screen.getByText((_, element) => element?.tagName === "LI" && element.textContent === "delivery.package: inspection source inconsistent.")).toBeInTheDocument();
    expect(screen.getByText((_, element) => element?.tagName === "LI" && element.textContent === "delivery: inspection source unavailable.")).toBeInTheDocument();
    expect(screen.getByText(/packet packet-audit-1; decision audit-1/)).toBeInTheDocument();
    expect(screen.getByText("prototype-run-1")).toBeInTheDocument();
    expect(screen.getByText(/admission prototype-qa-admission-1/)).toBeInTheDocument();
    // Labels and values render as separate structural fields, not one opaque
    // concatenated integrity string.
    expect(screen.queryByText("current:authority-1")).not.toBeInTheDocument();
    expect(screen.queryByText("P5-T1@2")).not.toBeInTheDocument();
  });

  it("identifies absent integrity records separately from degraded inspection sources", () => {
    const withoutIntegrityDiagnostics: GuidedFeatureDetail = {
      ...base,
      diagnostics: {
        ...base.diagnostics,
        integrity: { ...base.diagnostics.integrity, diagnostics: [] },
      },
    };
    render(<RelayFeatureWorkspaceDetail detail={withoutIntegrityDiagnostics} />, { wrapper });
    expect(screen.getByText("No integrity inspection diagnostics recorded; absent identities are not errors.")).toBeInTheDocument();
    expect(screen.queryByText("inspection source unavailable")).not.toBeInTheDocument();
  });

  it("composes the replacement revision for a confirmed guided reopen without a client digest", async () => {
    const user = userEvent.setup();
    mocks.action.mockClear();
    const reopened = {
      ...base,
      availableActions: [{ action: "reopen_discovery" as const, primary: true, enabled: true, requiresConfirmation: true, handoff: "Revise the closed discovery through the existing reopen owner." }],
      primaryAction: "reopen_discovery" as const,
    };
    mocks.action.mockResolvedValueOnce(reopened);
    render(<RelayFeatureWorkspaceDetail detail={reopened} />, { wrapper });
    const button = screen.getByRole("button", { name: "Reopen discovery" });
    expect(button).toBeDisabled();
    fireEvent.change(screen.getByRole("textbox", { name: "Replacement integrated revision" }), { target: { value: "# Reopened discovery\n" } });
    fireEvent.change(screen.getByRole("textbox", { name: "Reopen cause" }), { target: { value: "new exact evidence" } });
    expect(button).toBeDisabled();
    await user.click(screen.getByRole("checkbox", { name: "Confirm guided action" }));
    expect(button).toBeEnabled();
    await user.click(button);
    expect(mocks.action).toHaveBeenCalledWith("workspace-1", { expectedVersion: 2, action: "reopen_discovery", confirmation: true, cause: "new exact evidence", markdown: "# Reopened discovery\n" });
  });

  it("renders the distinct confirmed abandon secondary only when eligible", async () => {
    const user = userEvent.setup();
    mocks.action.mockClear();
    mocks.action.mockResolvedValueOnce(base);
    const eligible = {
      ...base,
      completion: { gates: [{ name: "authority", ready: true }], ready: true, recorded: false },
      availableActions: [
        { action: "complete_feature" as const, primary: true, enabled: true, requiresConfirmation: true },
        { action: "abandon_feature" as const, primary: false, enabled: true, requiresConfirmation: true },
      ],
      primaryAction: "complete_feature" as const,
    };
    render(<RelayFeatureWorkspaceDetail detail={eligible} />, { wrapper });
    expect(screen.getByRole("button", { name: "Complete feature" })).toBeInTheDocument();
    const abandonButton = screen.getByRole("button", { name: "Abandon feature" });
    expect(abandonButton).toBeDisabled();
    await user.click(abandonButton);
    expect(mocks.action).not.toHaveBeenCalled();
    await user.click(screen.getByRole("checkbox", { name: "Confirm abandon feature" }));
    expect(abandonButton).toBeEnabled();
    await user.click(abandonButton);
    await waitFor(() => expect(mocks.action).toHaveBeenCalledWith("workspace-1", { expectedVersion: 2, action: "abandon_feature", confirmation: true }));
    expect(JSON.stringify(mocks.action.mock.calls[0])).not.toMatch(/(workspaceId|rowId|sha256|completionDecisionId)/i);
  });

  it("shows the server-projected abandoned outcome and the reopen resume path", () => {
    const abandoned = {
      ...base,
      discovery: { ...base.discovery, state: "closed" },
      completion: { gates: [{ name: "authority", ready: true }], ready: true, recorded: true, decision: "abandoned" as const },
      availableActions: [{ action: "reopen_discovery" as const, primary: true, enabled: true, requiresConfirmation: true, handoff: "Revise the closed discovery through the existing reopen owner." }],
      primaryAction: "reopen_discovery" as const,
    };
    render(<RelayFeatureWorkspaceDetail detail={abandoned} />, { wrapper });
    expect(screen.getByText(/This feature workspace was abandoned\./)).toBeInTheDocument();
    expect(screen.getByText("Recorded decision:")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Reopen discovery" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Abandon feature" })).not.toBeInTheDocument();
  });

  it("renders no abandon control when the availability list does not include it", () => {
    render(<RelayFeatureWorkspaceDetail detail={base} />, { wrapper });
    expect(screen.queryByRole("button", { name: "Abandon feature" })).not.toBeInTheDocument();
    expect(screen.queryByRole("checkbox", { name: "Confirm abandon feature" })).not.toBeInTheDocument();
    expect(screen.getAllByRole("button")).toHaveLength(1);
  });

  it("renders and confirms the distinct planning candidate approval action", async () => {
    const user = userEvent.setup();
    mocks.action.mockClear();
    mocks.action.mockResolvedValueOnce(base);
    const approve = {
      ...base,
      availableActions: [{ action: "approve_planning_candidate" as const, primary: true, enabled: true, requiresConfirmation: true, handoff: "Explicitly approve the exact reviewed candidate server-side." }],
      primaryAction: "approve_planning_candidate" as const,
    };
    render(<RelayFeatureWorkspaceDetail detail={approve} />, { wrapper });
    const button = screen.getByRole("button", { name: "Approve planning candidate" });
    expect(button).toBeDisabled();
    expect(screen.getAllByRole("button")).toHaveLength(1);
    await user.click(screen.getByRole("checkbox", { name: "Confirm guided action" }));
    expect(button).toBeEnabled();
    await user.click(button);
    await waitFor(() => expect(mocks.action).toHaveBeenCalledWith("workspace-1", { expectedVersion: 2, action: "approve_planning_candidate", confirmation: true }));
    expect(JSON.stringify(mocks.action.mock.calls[0])).not.toMatch(/(candidateId|approvalId|reviewId|sha256|digest|rowId)/i);
  });

  it("renders a selected Delivery Ticket projection with direct package preparation and no Brief UI", async () => {
    const user = userEvent.setup();
    mocks.action.mockClear();
    mocks.action.mockResolvedValueOnce(base);
    const selected = {
      ...base,
      delivery: { frontier: [{ ticketId: "P5-T1", revisionNumber: 2, externalPriority: 60, repoTarget: "relay", branch: "main" }], selectionState: "active", packageState: "none", runState: "none", auditState: "none", remediationState: "none" },
      availableActions: [{ action: "prepare_package" as const, primary: true, enabled: true, requiresConfirmation: false }],
      primaryAction: "prepare_package" as const,
    };
    render(<RelayFeatureWorkspaceDetail detail={selected} />, { wrapper });
    const button = screen.getByRole("button", { name: "Prepare package" });
    expect(button).toBeEnabled();
    expect(screen.getAllByRole("button")).toHaveLength(1);
    // The retired Ticket Design Brief surface has no label, status, or control.
    expect(screen.queryByText(/Ticket Design Brief/i)).not.toBeInTheDocument();
    await user.click(button);
    await waitFor(() => expect(mocks.action).toHaveBeenCalledWith("workspace-1", { expectedVersion: 2, action: "prepare_package", confirmation: false }));
    expect(JSON.stringify(mocks.action.mock.calls[0])).not.toMatch(/(briefId|selectionId|approvalId|reviewId|sha256|digest|rowId)/i);
  });

  it("sends only semantic fields and never an integrity identity in the action payload", async () => {
    const user = userEvent.setup();
    mocks.action.mockClear();
    mocks.action.mockResolvedValueOnce(base);
    render(<RelayFeatureWorkspaceDetail detail={base} />, { wrapper });
    await user.click(screen.getByRole("checkbox", { name: "Confirm guided action" }));
    await user.click(screen.getByRole("button", { name: "Continue discovery" }));
    // The payload is exactly the semantic fields: expected version, action,
    // and confirmation. No candidate, package, run, audit, digest, or artifact
    // identity from the integrity surface is accepted by the boundary.
    expect(mocks.action).toHaveBeenCalledWith("workspace-1", { expectedVersion: 2, action: "continue_discovery", confirmation: true });
    const payload = JSON.stringify(mocks.action.mock.calls[0]);
    for (const identity of ["candidate-1", "selection-1", "package-1", "run-1", "packet-audit-1", "audit-1", "sha-package-1", "remediation-1", "closure-1", "sha-candidate-1"]) {
      expect(payload).not.toContain(identity);
    }
  });
});
