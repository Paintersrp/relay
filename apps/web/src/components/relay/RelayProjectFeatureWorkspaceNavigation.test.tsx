// @vitest-environment jsdom

import { QueryClient, QueryClientProvider, useQuery } from "@tanstack/react-query";
import { createMemoryHistory, createRootRoute, createRoute, createRouter, Outlet, RouterProvider } from "@tanstack/react-router";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import { RelayFeatureWorkspaceDetail } from "./RelayFeatureWorkspaceDetail";
import { RelayProjectFeatureWorkspacesPanel } from "./RelayProjectFeatureWorkspacesPanel";
import { RelayProjectsRegistry } from "./RelayProjectsRegistry";
import { workflowProjectDetailQueryOptions, workflowProjectsListQueryOptions } from "@/features/relay-projects";
import { featureWorkspaceGuidedQueryOptions } from "@/features/relay-feature-workspaces";

function json(body: unknown): Response {
  return new Response(JSON.stringify(body), { status: 200, headers: { "Content-Type": "application/json" } });
}

const project = { projectId: "project-1", name: "Relay", description: "Current work", status: "active", createdAt: "2026-01-01T00:00:00Z", updatedAt: "2026-01-02T00:00:00Z" } as const;
const workspace = { workspaceId: "workspace-1", featureSlug: "payments", state: "open", version: 2, createdAt: "2026-01-01T00:00:00Z", updatedAt: "2026-01-02T00:00:00Z" } as const;

function ProjectPage() {
  const { projectId } = projectRoute.useParams();
  const detail = useQuery(workflowProjectDetailQueryOptions(projectId));
  return <main><h1>Project page</h1>{detail.data ? <RelayProjectFeatureWorkspacesPanel projectId={projectId} /> : null}</main>;
}
function FeaturePage() {
  const { workspaceId } = FeatureRoute.useParams();
  const query = useQuery(featureWorkspaceGuidedQueryOptions(workspaceId));
  return <main>{query.data ? <RelayFeatureWorkspaceDetail detail={query.data} /> : null}</main>;
}

const root = createRootRoute({ component: () => <Outlet /> });
const projectsRoute = createRoute({ getParentRoute: () => root, path: "/projects", component: () => {
  const query = useQuery(workflowProjectsListQueryOptions({ limit: 100 }));
  return <RelayProjectsRegistry projects={query.data?.projects} isLoading={query.isLoading} error={query.error} />;
} });
const projectRoute = createRoute({ getParentRoute: () => root, path: "/projects/$projectId", component: ProjectPage });
const FeatureRoute = createRoute({ getParentRoute: () => root, path: "/feature-workspaces/$workspaceId", component: FeaturePage });
const routeTree = root.addChildren([projectsRoute, projectRoute, FeatureRoute]);

const guided = {
  workspace,
  project: { projectId: project.projectId, name: project.name },
  discovery: { state: "active", destination: "requirements", rationale: "Clarify the supported path.", continuation: "Resume the requirements route.", currentness: "current" },
  authority: { currentRevisionNumber: 0, revisions: [] },
  planning: { readiness: "current", status: "not_started", recoveryCategory: "" },
  delivery: { frontierCount: 1, selectionState: "none", packageState: "none", runState: "none", auditState: "none", remediationState: "none" },
  prototype: { runState: "none", cleanupState: "none", qaState: "prepared", evidenceState: "none" },
  completion: { gates: [{ name: "authority", ready: false }], ready: false, recorded: false },
  recovery: { state: "none", category: "", available: [] },
  diagnostics: { stale: [], historical: [], discovery: ["requirements frontier"] },
  availableActions: [{ action: "continue_discovery", primary: true, enabled: true, requiresConfirmation: true }],
  primaryAction: "continue_discovery",
};

describe("Project to Feature workspace normal entry", () => {
  it("follows visible project and feature links, renders semantic destinations, and returns to resume", async () => {
    const fetch = vi.fn((input: RequestInfo | URL) => {
      const path = String(input);
      if (path.includes("/api/projects?")) return Promise.resolve(json({ count: 1, items: [project] }));
      if (path.includes("/api/projects/project-1?")) return Promise.resolve(json({ project, repositories: [], notes: [], plans: [] }));
      if (path.includes("/api/projects/project-1/feature-workspaces")) return Promise.resolve(json({ count: 1, items: [{ ...workspace, projectId: project.projectId, progressionSummary: "Discovery is in progress.", resumeSummary: "Continue discovery.", blocked: false }] }));
      if (path.includes("/api/feature-workspaces/workspace-1/guided")) return Promise.resolve(json({ guided }));
      throw new Error(`unexpected fetch ${path}`);
    });
    vi.stubGlobal("fetch", fetch);
    const router = createRouter({ routeTree, history: createMemoryHistory({ initialEntries: ["/projects"] }) });
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(<QueryClientProvider client={queryClient}><RouterProvider router={router} /></QueryClientProvider>);

    const user = userEvent.setup();
    await user.click((await screen.findAllByRole("link", { name: "Open project Relay" }))[0]);
    await user.click(await screen.findByRole("link", { name: /payments/ }));
    expect(await screen.findByRole("heading", { name: "Ticket frontier and downstream" })).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Prototype and QA" })).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Blockers and recovery" })).toBeInTheDocument();
    expect(screen.getAllByText("requirements").length).toBeGreaterThan(0);
    await user.click(screen.getByRole("link", { name: "Return to Relay" }));
    expect(await screen.findByRole("heading", { name: "Feature Workspaces" })).toBeInTheDocument();
    expect(fetch).toHaveBeenCalled();
  });
});
