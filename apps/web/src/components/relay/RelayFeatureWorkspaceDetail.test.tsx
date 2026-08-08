// @vitest-environment jsdom
import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
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
  discovery: { state: "active", destination: "delivery", rationale: "Clarify the supported payment path.", continuation: "Resolve the remaining discovery questions.", currentness: "current" },
  authority: { currentRevisionNumber: 1, revisions: [{ revisionNumber: 1, layers: ["requirements", "design"], historical: false }] },
  planning: { readiness: "current", status: "ready", recoveryCategory: "" },
  completion: { gates: [{ name: "authority", ready: true }, { name: "audit", ready: false }], ready: false, recorded: false },
  diagnostics: {
    history: { discoveryCurrentness: "current", historicalIdentity: "none" },
    stale: { readiness: "current", owner: "", blockedOperation: "", effect: "", recoveryCategory: "", basis: "" },
    discovery: { blockers: ["missing evidence"], restorationActions: ["collect evidence"], pendingIntegrations: [], activeOperations: [], routeMaterialOpen: false, requiredEvidence: ["approval"] },
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

  it("renders the guided projection sections and diagnostics separately", () => {
    render(<RelayFeatureWorkspaceDetail detail={base} />, { wrapper });
    expect(screen.getByRole("heading", { name: "Discovery" })).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Authority" })).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Planning and currentness" })).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Completion" })).toBeInTheDocument();
    expect(screen.getByText("Clarify the supported payment path.")).toBeInTheDocument();
    expect(screen.getByText("Diagnostics")).toBeInTheDocument();
    expect(screen.getByText("missing evidence")).toBeInTheDocument();
  });

  it("renders exactly one primary action and no raw workspace input controls", () => {
    render(<RelayFeatureWorkspaceDetail detail={base} />, { wrapper });
    expect(screen.getAllByRole("button")).toHaveLength(1);
    expect(screen.getByRole("button", { name: "Continue discovery" })).toBeInTheDocument();
    expect(screen.queryAllByRole("textbox")).toHaveLength(0);
    expect(screen.queryAllByRole("combobox")).toHaveLength(0);
  });

  it("requires explicit confirmation and sends the typed guided action", async () => {
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
    expect(mocks.action).toHaveBeenCalledWith("workspace-1", { expectedVersion: 2, action: "continue_discovery", confirmation: true, destination: "delivery" });
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
});
