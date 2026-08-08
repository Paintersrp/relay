// @vitest-environment jsdom

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { RelayProjectFeatureWorkspacesPanel } from "./RelayProjectFeatureWorkspacesPanel";
import type { ProjectFeatureWorkspaceListResponse } from "@/features/relay-feature-workspaces";

const mocks = vi.hoisted(() => ({
  listProjectFeatureWorkspaces: vi.fn(),
}));

vi.mock("@tanstack/react-router", () => ({
  Link: ({ children, to, params, search }: any) => (
    <a
      href={to}
      data-workspace-id={params?.workspaceId ?? ""}
      data-search-project-id={search?.projectId ?? ""}
    >
      {children}
    </a>
  ),
}));

vi.mock("@/features/relay-feature-workspaces/api", async () => {
  const actual = await vi.importActual<typeof import("@/features/relay-feature-workspaces/api")>("@/features/relay-feature-workspaces/api");
  return { ...actual, listProjectFeatureWorkspaces: mocks.listProjectFeatureWorkspaces };
});

vi.mock("@/features/relay-feature-workspaces", async () => {
  const actual = await vi.importActual<
    typeof import("@/features/relay-feature-workspaces")
  >("@/features/relay-feature-workspaces");
  return {
    ...actual,
    listProjectFeatureWorkspaces: mocks.listProjectFeatureWorkspaces,
  };
});

function renderPanel(projectId = "project-1") {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <RelayProjectFeatureWorkspacesPanel projectId={projectId} />
    </QueryClientProvider>,
  );
}

describe("RelayProjectFeatureWorkspacesPanel", () => {
  it("has an accessible section heading naming Feature Workspaces", async () => {
    mocks.listProjectFeatureWorkspaces.mockResolvedValue({ count: 0, items: [] } satisfies ProjectFeatureWorkspaceListResponse);
    renderPanel();
    expect(screen.getByRole("heading", { name: "Feature Workspaces" })).toBeInTheDocument();
    await waitFor(() => expect(mocks.listProjectFeatureWorkspaces).toHaveBeenCalledWith("project-1"));
  });

  it("shows an explicit empty state distinct from a load failure", async () => {
    mocks.listProjectFeatureWorkspaces.mockResolvedValue({ count: 0, items: [] } satisfies ProjectFeatureWorkspaceListResponse);
    renderPanel();
    expect(await screen.findByText(/No feature workspaces are attached to this Project yet\./)).toBeInTheDocument();
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });

  it("lists a workspace's slug, state, and version and links to its detail route", async () => {
    mocks.listProjectFeatureWorkspaces.mockResolvedValue({
      count: 1,
      items: [
        {
          workspaceId: "workspace-ccd47919",
          projectId: "project-1",
          featureSlug: "wayfinder-bootstrap",
          state: "open",
          version: 3,
          createdAt: "2026-07-01T00:00:00Z",
          updatedAt: "2026-07-02T00:00:00Z",
        },
      ],
    } satisfies ProjectFeatureWorkspaceListResponse);
    renderPanel();

    const link = await screen.findByRole("link", { name: /wayfinder-bootstrap/ });
    expect(link).toHaveAttribute("data-workspace-id", "workspace-ccd47919");
    expect(link).toHaveTextContent("Resume wayfinder-bootstrap");
    expect(screen.getByText("open")).toBeInTheDocument();
    expect(screen.getByText("Version 3")).toBeInTheDocument();
  });

  it("shows an error state without hiding the rest of the panel when the list fails to load", async () => {
    mocks.listProjectFeatureWorkspaces.mockRejectedValue(new Error("network down"));
    renderPanel();

    expect(await screen.findByRole("alert")).toHaveTextContent(/could not be loaded/);
    // The create action and heading remain visible alongside the error.
    expect(screen.getByRole("heading", { name: "Feature Workspaces" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: /Create workspace/ })).toBeInTheDocument();
  });

  it("carries the current project identity into the creation action", async () => {
    mocks.listProjectFeatureWorkspaces.mockResolvedValue({ count: 0, items: [] } satisfies ProjectFeatureWorkspaceListResponse);
    renderPanel("project-abc");

    const createLink = screen.getByRole("link", { name: /Create workspace/ });
    expect(createLink).toHaveAttribute("data-search-project-id", "project-abc");
  });
});
