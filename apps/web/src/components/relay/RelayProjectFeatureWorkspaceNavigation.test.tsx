// @vitest-environment jsdom

import { QueryClient, QueryClientProvider, useQuery } from "@tanstack/react-query";
import { createMemoryHistory, createRootRoute, createRoute, createRouter, Outlet, RouterProvider } from "@tanstack/react-router";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

import { RelayFeatureWorkspaceDetail } from "./RelayFeatureWorkspaceDetail";
import { RelayProjectFeatureWorkspacesPanel } from "./RelayProjectFeatureWorkspacesPanel";
import { RelayProjectsRegistry } from "./RelayProjectsRegistry";
import { workflowProjectDetailQueryOptions, workflowProjectsListQueryOptions } from "@/features/relay-projects";
import { featureWorkspaceGuidedQueryOptions, featureWorkspaceKeys } from "@/features/relay-feature-workspaces";

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

// Raw server projections in the production guidedFeatureProjectionDTO shape.
// The backend mock returns the next valid authoritative projection after each
// mutation or handoff; the client never fabricates progression.
type GuidedPatch = Record<string, unknown>;

const frontierEntry = { ticketId: "P5-T1", revisionNumber: 2, externalPriority: 60, repoTarget: "relay", branch: "main" };

const baseGuided: GuidedPatch = {
  workspace: { ...workspace, state: "closed", version: 2 },
  project: { projectId: project.projectId, name: project.name },
  discovery: { state: "closed", destination: "direct_delivery_ticket", rationale: "Clarify the supported payment path.", continuation: "Resume the delivery route.", currentness: "current", basis: "closure packet verified", reopenState: "none" },
  authority: { currentRevisionNumber: 1, layers: [] },
  currentness: { readiness: "current", owner: "", blockedOperation: "", effect: "", recoveryCategory: "" },
  planning: { status: "ready", candidateState: "none", reviewState: "none", approvalState: "none", promotionState: "none", candidateCount: 0, awaitingReview: 0, awaitingApproval: 0, awaitingPromotion: 0, promoted: 0, historicalCount: 0 },
  delivery: { frontier: [], selectionState: "none", packageState: "none", runState: "none", auditState: "none", remediationState: "none" },
  prototype: { runState: "none", cleanupState: "none", qaState: "none", evidenceState: "none", processOutcome: "" },
  completion: { gates: [{ name: "authority", ready: true }, { name: "tickets", ready: true }, { name: "integration", ready: true }, { name: "transitions", ready: true }, { name: "remediation", ready: true }, { name: "audit", ready: true }], ready: true, recorded: false },
  recovery: { state: "none", category: "", available: [] },
  diagnostics: { stale: [], historical: [], discovery: [], delivery: [], prototype: [] },
  availableActions: [],
  primaryAction: "",
  handoff: null,
};

function mergeJson(base: unknown, patch: unknown): unknown {
  if (patch === undefined || patch === null) return base;
  if (Array.isArray(base) || Array.isArray(patch)) return patch;
  if (typeof base === "object" && typeof patch === "object") {
    const result: Record<string, unknown> = { ...(base as Record<string, unknown>) };
    for (const key of Object.keys(patch as Record<string, unknown>)) result[key] = mergeJson(result[key], (patch as Record<string, unknown>)[key]);
    return result;
  }
  return patch;
}
function guidedBody(patch: GuidedPatch = {}): { guided: unknown } {
  return { guided: mergeJson(baseGuided, patch) };
}
function primaryAction(action: string, requiresConfirmation: boolean, extra: GuidedPatch = {}): GuidedPatch {
  return { availableActions: [{ action, primary: true, enabled: true, requiresConfirmation, ...extra }], primaryAction: action };
}
function guidedWith(action: string, requiresConfirmation: boolean, patch: GuidedPatch = {}): { guided: unknown } {
  return guidedBody({ ...primaryAction(action, requiresConfirmation), ...patch });
}
function handoff(role: string, transfer: GuidedPatch, summary: string): GuidedPatch {
  return { role, summary, resumeRoute: "/feature-workspaces/workspace-1/guided", context: { owner: role }, transfer };
}
function emptyTransfer(): GuidedPatch {
  return { frontier: [], members: [], authorityLayers: [], ticket: null, package: null, run: null, audit: null, remediation: null, prototype: null };
}

function expectStableGuidedIntent(body: Record<string, unknown>, expected: Record<string, unknown>) {
  expect(body).toEqual(expected);
  expect(Object.keys(body).sort()).toEqual(Object.keys(expected).sort());
  expect(JSON.stringify(body)).not.toMatch(/(ticketId|briefId|candidateId|packageId|runId|approvalId|reviewId|selectionId|workspaceId|sha256|digest|rowId)/i);
}

function installMock(gets: Array<{ guided: unknown }>, posts: Array<{ body: unknown; status?: number }>) {
  const postBodies: Array<Record<string, unknown>> = [];
  const fetch = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
    const path = String(input);
    if ((init?.method ?? "GET") === "POST" && path.includes("/guided/actions")) {
      postBodies.push(JSON.parse(String(init?.body)) as Record<string, unknown>);
      const next = posts.shift();
      if (!next) throw new Error(`unexpected guided action POST for ${path}`);
      return Promise.resolve(next.status && next.status !== 200 ? new Response(JSON.stringify(next.body), { status: next.status, headers: { "Content-Type": "application/json" } }) : json(next.body));
    }
    if (path.includes("/api/projects?") && !path.includes("/api/projects/project-1")) return Promise.resolve(json({ count: 1, items: [project] }));
    if (path.includes("/api/projects/project-1?")) return Promise.resolve(json({ project, repositories: [], notes: [], plans: [] }));
    if (path.includes("/api/projects/project-1/feature-workspaces")) return Promise.resolve(json({ count: 1, items: [{ ...workspace, projectId: project.projectId, progressionSummary: "Discovery is in progress.", resumeSummary: "Continue discovery.", blocked: false }] }));
    if (path.includes("/api/feature-workspaces/workspace-1/guided")) {
      const next = gets.shift();
      if (!next) throw new Error(`unexpected guided GET for ${path}`);
      return Promise.resolve(json(next));
    }
    throw new Error(`unexpected fetch ${path}`);
  });
  vi.stubGlobal("fetch", fetch);
  return { fetch, postBodies };
}
afterEach(() => vi.unstubAllGlobals());

async function openFeature(user: ReturnType<typeof userEvent.setup>) {
  await user.click((await screen.findAllByRole("link", { name: "Open project Relay" }))[0]);
  await user.click(await screen.findByRole("link", { name: /payments/ }));
}

async function renderJourney() {
  const router = createRouter({ routeTree, history: createMemoryHistory({ initialEntries: ["/projects"] }) });
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(<QueryClientProvider client={queryClient}><RouterProvider router={router} /></QueryClientProvider>);
  const user = userEvent.setup();
  await openFeature(user);
  return { user, router, queryClient };
}

async function returnProject(user: ReturnType<typeof userEvent.setup>) {
  await user.click(screen.getByRole("link", { name: "Return to Relay" }));
  expect(await screen.findByRole("heading", { name: "Feature Workspaces" })).toBeInTheDocument();
  await user.click(await screen.findByRole("link", { name: /payments/ }));
}

// A fresh currentness check: the operator performs the bounded role work
// through the existing owner, then returns. Evicting the guided cache forces
// the server's authoritative next projection to reload exactly like a stale
// re-entry would, without timing dependence.
async function freshCheckAndResume(user: ReturnType<typeof userEvent.setup>, queryClient: QueryClient) {
  queryClient.removeQueries({ queryKey: featureWorkspaceKeys.guided("workspace-1") });
  await returnProject(user);
}

interface JourneyRow {
  name: string;
  primaryAction: string;
  button: string;
  requiresConfirmation: boolean;
  initial: GuidedPatch;
  post: GuidedPatch;
  nextButton: string;
  handoffEvidence?: string;
  nextText?: string;
}

const journeyRows: JourneyRow[] = [
  {
    name: "no_delivery_work closes the feature and exposes reopening",
    primaryAction: "complete_feature",
    button: "Complete feature",
    requiresConfirmation: true,
    initial: { discovery: { destination: "no_delivery_work", state: "closed" } },
    post: {
      workspace: { version: 3 },
      completion: { recorded: true },
      ...primaryAction("reopen_discovery", true),
    },
    nextButton: "Reopen discovery",
    nextText: "Completion has been recorded.",
  },
  {
    name: "direct_delivery_ticket selects the frontier ticket server-side",
    primaryAction: "select_delivery_ticket",
    button: "Select delivery ticket",
    requiresConfirmation: true,
    initial: { delivery: { frontier: [frontierEntry] } },
    post: {
      workspace: { version: 3 },
      delivery: { selectionState: "active", briefState: "none" },
      ...primaryAction("author_ticket_design_brief", false),
    },
    nextButton: "Author Ticket Design Brief",
    nextText: "Version 3",
  },
  {
    name: "requirements authors through the planning handoff",
    primaryAction: "author_requirements",
    button: "Author Requirements",
    requiresConfirmation: false,
    initial: { discovery: { destination: "requirements" }, planning: { status: "not_started" } },
    post: {
      ...primaryAction("author_requirements", false),
      handoff: handoff("author_requirements", { ...emptyTransfer(), members: ["feature_owner", "planner"], authorityLayers: ["requirements"] }, "Planner authoring and review are prepared through their existing owners."),
    },
    nextButton: "Author Requirements",
    handoffEvidence: "Closure members",
  },
  {
    name: "shared_design authors through the planning handoff",
    primaryAction: "author_shared_design",
    button: "Author Shared Design",
    requiresConfirmation: false,
    initial: { discovery: { destination: "shared_design" }, planning: { status: "not_started" } },
    post: {
      ...primaryAction("author_shared_design", false),
      handoff: handoff("author_shared_design", { ...emptyTransfer(), members: ["feature_owner", "planner"], authorityLayers: ["shared_design"] }, "Planner authoring and review are prepared through their existing owners."),
    },
    nextButton: "Author Shared Design",
    handoffEvidence: "Authority layers",
  },
  {
    name: "requirements_then_shared_design authors requirements first",
    primaryAction: "author_requirements",
    button: "Author Requirements",
    requiresConfirmation: false,
    initial: { discovery: { destination: "requirements_then_shared_design" }, planning: { status: "not_started" } },
    post: {
      ...primaryAction("author_requirements", false),
      handoff: handoff("author_requirements", { ...emptyTransfer(), members: ["feature_owner"], authorityLayers: ["requirements"] }, "Author Requirements, then explicitly approve and promote it before Shared Design."),
    },
    nextButton: "Author Requirements",
    handoffEvidence: "feature_owner",
  },
  {
    name: "existing_route_continuation resumes the established route",
    primaryAction: "continue_established_route",
    button: "Continue established route",
    requiresConfirmation: false,
    initial: { discovery: { destination: "existing_route_continuation", continuation: "Resume the established route." } },
    post: {
      ...primaryAction("continue_established_route", false),
      handoff: handoff("continue_established_route", { ...emptyTransfer(), members: ["feature_owner"], authorityLayers: ["requirements"] }, "Planner authoring and review are prepared through their existing owners."),
    },
    nextButton: "Continue established route",
    handoffEvidence: "Closure members",
  },
];

describe("Project to Feature workspace normal entry journeys", () => {
  it.each(journeyRows)("$name", async (row) => {
    installMock([guidedWith(row.primaryAction, row.requiresConfirmation, row.initial)], [{ body: guidedBody(row.post) }]);
    const { user } = await renderJourney();
    expect(await screen.findByRole("button", { name: row.button })).toBeInTheDocument();
    expect(screen.getAllByRole("button")).toHaveLength(1);
    if (row.requiresConfirmation) await user.click(screen.getByRole("checkbox", { name: "Confirm guided action" }));
    await user.click(screen.getByRole("button", { name: row.button }));
    expect(await screen.findByRole("button", { name: row.nextButton })).toBeInTheDocument();
    if (row.handoffEvidence) expect(screen.getByText(row.handoffEvidence)).toBeInTheDocument();
    await returnProject(user);
    expect(await screen.findByRole("button", { name: row.nextButton })).toBeInTheDocument();
    if (row.nextText) expect(screen.getByText(row.nextText)).toBeInTheDocument();
  });

  it("sends only the server-selected action with expected version and confirmation", async () => {
    const mocks = installMock(
      [guidedWith("select_delivery_ticket", true, { delivery: { frontier: [frontierEntry] } })],
      [{ body: guidedBody({ workspace: { version: 3 }, delivery: { selectionState: "active", briefState: "none" }, ...primaryAction("author_ticket_design_brief", false) }) }],
    );
    const { user } = await renderJourney();
    await user.click(screen.getByRole("checkbox", { name: "Confirm guided action" }));
    await user.click(screen.getByRole("button", { name: "Select delivery ticket" }));
    await screen.findByRole("button", { name: "Author Ticket Design Brief" });
    expect(mocks.postBodies).toHaveLength(1);
    expect(mocks.postBodies[0]).toEqual({ expectedVersion: 2, action: "select_delivery_ticket", confirmation: true });
    expect(Object.keys(mocks.postBodies[0]).sort()).toEqual(["action", "confirmation", "expectedVersion"]);
    expect(JSON.stringify(mocks.postBodies[0])).not.toMatch(/(ticketId|packageId|runId|workspaceId|sha256|rowId)/i);
  });

  it("abandons through the eligible secondary control and shows the abandoned project summary after returning", async () => {
    const postBodies: Array<Record<string, unknown>> = [];
    const fetch = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const path = String(input);
      if ((init?.method ?? "GET") === "POST" && path.includes("/guided/actions")) {
        postBodies.push(JSON.parse(String(init?.body)) as Record<string, unknown>);
        // The refreshed server projection exposes the abandoned outcome and
        // selects the normal existing reopen path.
        return Promise.resolve(json(guidedBody({
          workspace: { version: 3 },
          completion: { recorded: true, decision: "abandoned" },
          ...primaryAction("reopen_discovery", true),
        })));
      }
      if (path.includes("/api/projects?") && !path.includes("/api/projects/project-1")) return Promise.resolve(json({ count: 1, items: [project] }));
      if (path.includes("/api/projects/project-1?")) return Promise.resolve(json({ project, repositories: [], notes: [], plans: [] }));
      // The project summary uses the server-projected decision to say
      // abandoned with the reopen resume route meaning.
      if (path.includes("/api/projects/project-1/feature-workspaces")) return Promise.resolve(json({ count: 1, items: [{ ...workspace, projectId: project.projectId, progressionSummary: "Feature workspace was abandoned.", resumeSummary: "reopen_discovery", blocked: false }] }));
      if (path.includes("/api/feature-workspaces/workspace-1/guided")) {
        return Promise.resolve(json(guidedBody({
          discovery: { destination: "no_delivery_work", state: "closed" },
          completion: { ready: true, recorded: false },
          availableActions: [
            { action: "complete_feature", primary: true, enabled: true, requiresConfirmation: true },
            { action: "abandon_feature", primary: false, enabled: true, requiresConfirmation: true },
          ],
          primaryAction: "complete_feature",
        })));
      }
      throw new Error(`unexpected fetch ${path}`);
    });
    vi.stubGlobal("fetch", fetch);
    const router = createRouter({ routeTree, history: createMemoryHistory({ initialEntries: ["/projects"] }) });
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(<QueryClientProvider client={queryClient}><RouterProvider router={router} /></QueryClientProvider>);
    const user = userEvent.setup();
    await openFeature(user);

    // The visible Project -> Feature journey reaches the guided workspace:
    // the sole primary remains complete_feature and the eligible abandon
    // control renders as a distinct secondary with its own confirmation.
    expect(await screen.findByRole("button", { name: "Complete feature" })).toBeInTheDocument();
    const abandonButton = screen.getByRole("button", { name: "Abandon feature" });
    expect(abandonButton).toBeDisabled();
    await user.click(abandonButton);
    expect(postBodies).toHaveLength(0);
    await user.click(screen.getByRole("checkbox", { name: "Confirm abandon feature" }));
    expect(abandonButton).toBeEnabled();
    await user.click(abandonButton);

    // The refreshed projection shows the abandoned outcome and the normal
    // reopen resume path; the request carried only the semantic guided action
    // plus the expected version, never a lifecycle identity.
    expect(await screen.findByText(/This feature workspace was abandoned\./)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Reopen discovery" })).toBeInTheDocument();
    expect(postBodies).toHaveLength(1);
    expect(postBodies[0]).toEqual({ expectedVersion: 2, action: "abandon_feature", confirmation: true });
    expect(Object.keys(postBodies[0]).sort()).toEqual(["action", "confirmation", "expectedVersion"]);
    expect(JSON.stringify(postBodies[0])).not.toMatch(/(workspaceId|sha256|rowId|completionDecisionId)/i);

    // The existing normal back-to-project route makes the project summary
    // visibly say abandoned with the reopen resume meaning.
    await user.click(screen.getByRole("link", { name: "Return to Relay" }));
    expect(await screen.findByText("Feature workspace was abandoned.")).toBeInTheDocument();
    expect(screen.getAllByText("reopen_discovery").length).toBeGreaterThan(0);
  });

  it("renders no abandon control when server availability says it is not eligible", async () => {
    const fetch = vi.fn((input: RequestInfo | URL) => {
      const path = String(input);
      if (path.includes("/api/projects?") && !path.includes("/api/projects/project-1")) return Promise.resolve(json({ count: 1, items: [project] }));
      if (path.includes("/api/projects/project-1?")) return Promise.resolve(json({ project, repositories: [], notes: [], plans: [] }));
      if (path.includes("/api/projects/project-1/feature-workspaces")) return Promise.resolve(json({ count: 1, items: [{ ...workspace, projectId: project.projectId, progressionSummary: "Discovery is in progress.", resumeSummary: "Continue discovery.", blocked: false }] }));
      if (path.includes("/api/feature-workspaces/workspace-1/guided")) {
        // Gates not ready: completion is the sole disabled primary and no
        // abandonment secondary is advertised by the server.
        return Promise.resolve(json(guidedBody({
          discovery: { destination: "no_delivery_work", state: "closed" },
          completion: { ready: false, recorded: false },
          availableActions: [{ action: "complete_feature", primary: true, enabled: false, requiresConfirmation: true, blockedReason: "Feature completion is blocked by one or more current completion gates." }],
          primaryAction: "complete_feature",
        })));
      }
      throw new Error(`unexpected fetch ${path}`);
    });
    vi.stubGlobal("fetch", fetch);
    const router = createRouter({ routeTree, history: createMemoryHistory({ initialEntries: ["/projects"] }) });
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(<QueryClientProvider client={queryClient}><RouterProvider router={router} /></QueryClientProvider>);
    const user = userEvent.setup();
    await openFeature(user);
    expect(await screen.findByRole("button", { name: "Complete feature" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Abandon feature" })).not.toBeInTheDocument();
    expect(screen.queryByRole("checkbox", { name: "Confirm abandon feature" })).not.toBeInTheDocument();
    expect(screen.getAllByRole("button")).toHaveLength(1);
  });

  it("traverses the direct Delivery Ticket lifecycle through Brief and package approval to Run continuation", async () => {
    const gets = [
      guidedWith("select_delivery_ticket", true, { delivery: { frontier: [frontierEntry] } }),
      // The planner admitted the authored Brief through the delivery owner.
      guidedWith("review_ticket_design_brief", false, { workspace: { version: 3 }, delivery: { selectionState: "active", briefState: "authored" }, diagnostics: { delivery: ["ticket_design_brief=authored"] } }),
      // The external ready review completion armed the distinct explicit
      // approval; the durable brief state remains authored until the confirmed
      // approval consumes the process-local continuation.
      guidedWith("approve_ticket_design_brief", true, { workspace: { version: 3 }, delivery: { selectionState: "active", briefState: "authored" }, diagnostics: { delivery: ["ticket_design_brief=reviewed_ready"] } }),
      // The Run owner advanced the launched Run; guided re-entry presents the
      // source-backed continuation rather than another launch.
      guidedWith("continue_run", false, { workspace: { version: 4 }, delivery: { selectionState: "consumed", briefState: "approved", packageState: "approved", runState: "executing" } }),
    ];
    const posts = [
      // Selecting a frontier ticket makes Brief authoring—not package work—the
      // only valid successor.
      { body: guidedBody({ workspace: { version: 3 }, delivery: { selectionState: "active", briefState: "none" }, ...primaryAction("author_ticket_design_brief", false) }) },
      { body: guidedWith("author_ticket_design_brief", false, { workspace: { version: 3 }, delivery: { selectionState: "active", briefState: "none" }, handoff: handoff("author_ticket_design_brief", { ...emptyTransfer(), ticket: { ticketId: "P5-T1", revisionNumber: 2, readiness: ["brief_required"], operationId: "planner.ticket_design_brief" } }, "Author the selected Ticket Design Brief through its planner operation.") }) },
      { body: guidedWith("review_ticket_design_brief", false, { workspace: { version: 3 }, delivery: { selectionState: "active", briefState: "authored" }, handoff: handoff("review_ticket_design_brief", { ...emptyTransfer(), ticket: { ticketId: "P5-T1", revisionNumber: 2, readiness: ["brief_admitted"], operationId: "auditor.ticket_design_brief_review" } }, "Review the admitted Ticket Design Brief through its auditor operation.") }) },
      // The confirmed explicit approval is the distinct mutation that advances
      // the durable brief state to approved and unlocks package preparation.
      { body: guidedBody({ workspace: { version: 3 }, delivery: { selectionState: "active", briefState: "approved", packageState: "none" }, ...primaryAction("prepare_package", false) }) },
      // Package preparation is an actual package-owner mutation, not a handoff.
      { body: guidedBody({ workspace: { version: 3 }, delivery: { selectionState: "active", briefState: "approved", packageState: "prepared" }, diagnostics: { delivery: ["execution_package_prepared"] }, ...primaryAction("approve_package", true) }) },
      // Confirmed package approval consumes the selection and creates the Run.
      { body: guidedBody({ workspace: { version: 4 }, delivery: { selectionState: "consumed", briefState: "approved", packageState: "approved", runState: "setup_ready" }, ...primaryAction("launch_run", false) }) },
      { body: guidedWith("launch_run", false, { workspace: { version: 4 }, delivery: { selectionState: "consumed", briefState: "approved", packageState: "approved", runState: "setup_ready" }, handoff: handoff("launch_run", { ...emptyTransfer(), run: { runId: "run-1", status: "setup_ready", repoTarget: "relay", branch: "main", baseCommit: "a".repeat(40), packageId: "package-1" } }, "The package Run is identified through its existing owner.") }) },
      { body: guidedWith("continue_run", false, { workspace: { version: 4 }, delivery: { selectionState: "consumed", briefState: "approved", packageState: "approved", runState: "executing" }, handoff: handoff("continue_run", { ...emptyTransfer(), run: { runId: "run-1", status: "executing", repoTarget: "relay", branch: "main", baseCommit: "a".repeat(40), packageId: "package-1" } }, "The active package Run is identified through its existing owner.") }) },
    ];
    const mocks = installMock(gets, posts);
    const { user, queryClient } = await renderJourney();

    // Visible Project -> Feature navigation reaches the source-owned frontier.
    await user.click(screen.getByRole("checkbox", { name: "Confirm guided action" }));
    await user.click(screen.getByRole("button", { name: "Select delivery ticket" }));
    expect(await screen.findByRole("button", { name: "Author Ticket Design Brief" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Prepare package" })).not.toBeInTheDocument();
    expect(screen.getAllByRole("button")).toHaveLength(1);

    // Authoring transfers the selected ticket to the exact planner operation.
    await user.click(screen.getByRole("button", { name: "Author Ticket Design Brief" }));
    expect(await screen.findByText("Operation transfer")).toBeInTheDocument();
    expect(screen.getByText(/planner\.ticket_design_brief/)).toBeInTheDocument();

    // Owner admission produces the next source-valid projection: review, but
    // never approval, is the visible successor.
    await freshCheckAndResume(user, queryClient);
    expect(await screen.findByText("ticket_design_brief=authored")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Review Ticket Design Brief" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Approve Ticket Design Brief" })).not.toBeInTheDocument();
    expect(screen.getAllByRole("button")).toHaveLength(1);
    await user.click(screen.getByRole("button", { name: "Review Ticket Design Brief" }));
    expect(await screen.findByText(/auditor\.ticket_design_brief_review/)).toBeInTheDocument();

    // The external ready review completion armed the distinct explicit
    // approval; resume at the confirmed approve action, never at package work.
    await freshCheckAndResume(user, queryClient);
    expect(await screen.findByRole("button", { name: "Approve Ticket Design Brief" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Prepare package" })).not.toBeInTheDocument();
    expect(screen.getAllByRole("button")).toHaveLength(1);
    await user.click(screen.getByRole("checkbox", { name: "Confirm guided action" }));
    await user.click(screen.getByRole("button", { name: "Approve Ticket Design Brief" }));
    expect(await screen.findByRole("button", { name: "Prepare package" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Approve package" })).not.toBeInTheDocument();
    expect(screen.getAllByRole("button")).toHaveLength(1);

    // The package is prepared by the package owner, then approved explicitly.
    await user.click(screen.getByRole("button", { name: "Prepare package" }));
    expect(await screen.findByText("execution_package_prepared")).toBeInTheDocument();
    await user.click(screen.getByRole("checkbox", { name: "Confirm guided action" }));
    await user.click(screen.getByRole("button", { name: "Approve package" }));
    expect(await screen.findByRole("button", { name: "Launch run" })).toBeInTheDocument();
    expect(screen.getAllByRole("button")).toHaveLength(1);
    await user.click(screen.getByRole("button", { name: "Launch run" }));
    expect(await screen.findByText(/run-1/)).toBeInTheDocument();
    expect(screen.getAllByText(/setup_ready/).length).toBeGreaterThan(0);

    // Return/resume after the Run owner advances the Run exposes continuation,
    // not a duplicate launch.
    await freshCheckAndResume(user, queryClient);
    expect(await screen.findByRole("button", { name: "Continue run" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Launch run" })).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Continue run" }));
    expect((await screen.findAllByText(/executing/)).length).toBeGreaterThan(0);

    const expectedIntents = [
      { expectedVersion: 2, action: "select_delivery_ticket", confirmation: true },
      { expectedVersion: 3, action: "author_ticket_design_brief", confirmation: false },
      { expectedVersion: 3, action: "review_ticket_design_brief", confirmation: false },
      { expectedVersion: 3, action: "approve_ticket_design_brief", confirmation: true },
      { expectedVersion: 3, action: "prepare_package", confirmation: false },
      { expectedVersion: 3, action: "approve_package", confirmation: true },
      { expectedVersion: 4, action: "launch_run", confirmation: false },
      { expectedVersion: 4, action: "continue_run", confirmation: false },
    ];
    expect(mocks.postBodies).toHaveLength(expectedIntents.length);
    mocks.postBodies.forEach((body, index) => expectStableGuidedIntent(body, expectedIntents[index]));
  });

  it("projects a needs_revision Run to the existing remediation continuation without exposing Brief approval", async () => {
    const mocks = installMock([
      guidedWith("remediate", false, {
        workspace: { version: 4 },
        delivery: { selectionState: "consumed", briefState: "approved", packageState: "approved", runState: "needs_revision", remediationState: "open" },
        diagnostics: { delivery: ["run_needs_revision", "remediation_open"] },
      }),
    ], [{ body: guidedWith("remediate", false, { workspace: { version: 4 }, delivery: { selectionState: "consumed", briefState: "approved", packageState: "approved", runState: "needs_revision", remediationState: "open" }, handoff: handoff("remediate", { ...emptyTransfer(), remediation: { state: "open", seedIds: ["seed-1"] } }, "Continue the existing remediation owner for this revision request.") }) }]);
    const { user } = await renderJourney();

    expect(await screen.findByText("run_needs_revision")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Remediate" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Approve Ticket Design Brief" })).not.toBeInTheDocument();
    expect(screen.getAllByRole("button")).toHaveLength(1);
    await user.click(screen.getByRole("button", { name: "Remediate" }));
    expect(await screen.findByText(/seed-1/)).toBeInTheDocument();
    expectStableGuidedIntent(mocks.postBodies[0], { expectedVersion: 4, action: "remediate", confirmation: false });
  });

  it("traverses prototype execution, cleanup, and QA handoffs", async () => {
    const gets = [
      guidedWith("prototype_execute", false, { prototype: { runState: "proposed", cleanupState: "none", qaState: "none", evidenceState: "none" } }),
      // Fresh currentness check: the prototype Run is terminal with cleanup pending.
      guidedWith("prototype_cleanup", false, { prototype: { runState: "cleanup_required", cleanupState: "pending", qaState: "none", evidenceState: "none" } }),
      // Fresh currentness check: cleanup complete, QA evidence still required.
      guidedWith("prototype_qa", false, { prototype: { runState: "closed", cleanupState: "complete", qaState: "prepared", evidenceState: "none" } }),
    ];
    const posts = [
      { body: guidedWith("prototype_execute", false, { handoff: handoff("prototype_execute", { ...emptyTransfer(), prototype: { runId: "proto-run-1", runState: "proposed", processOutcome: "", cleanup: [], qaPackets: [] } }, "The prototype Run is identified through the prototype owner.") }) },
      { body: guidedWith("prototype_cleanup", false, { handoff: handoff("prototype_cleanup", { ...emptyTransfer(), prototype: { runId: "proto-run-1", runState: "cleanup_required", processOutcome: "", cleanup: [{ kind: "workspace", status: "pending" }], qaPackets: [] } }, "Reconcile cleanup through the prototype owner.") }) },
      { body: guidedWith("prototype_qa", false, { handoff: handoff("prototype_qa", { ...emptyTransfer(), prototype: { runId: "proto-run-1", runState: "closed", processOutcome: "verified", cleanup: [{ kind: "workspace", status: "complete" }], qaPackets: [{ packetId: "qa-1", status: "prepared", evidence: ["approval"] }] } }, "Prepare and admit the QA packet through the prototype QA owner.") }) },
    ];
    installMock(gets, posts);
    const { user, queryClient } = await renderJourney();

    await user.click(await screen.findByRole("button", { name: "Execute prototype" }));
    expect(await screen.findByText(/proto-run-1/)).toBeInTheDocument();

    await freshCheckAndResume(user, queryClient);
    await user.click(await screen.findByRole("button", { name: "Clean up prototype" }));
    expect(await screen.findByText(/workspace=pending/)).toBeInTheDocument();

    await freshCheckAndResume(user, queryClient);
    await user.click(await screen.findByRole("button", { name: "Prepare prototype QA" }));
    expect(await screen.findByText(/qa-1 \(prepared, evidence approval\)/)).toBeInTheDocument();
    expect(screen.getByText(/outcome verified/)).toBeInTheDocument();
  });

  it("reopens a recorded completion through the reopen owner and re-closes the replacement", async () => {
    const gets = [
      guidedWith("reopen_discovery", true, { discovery: { destination: "no_delivery_work", state: "closed" }, completion: { recorded: true } }),
      // Fresh currentness check: the reopen owner returned the workspace to
      // active discovery with the replacement revision open.
      guidedWith("close_discovery", true, { workspace: { version: 3, state: "open" }, discovery: { destination: "no_delivery_work", state: "active", reopenState: "reopened" }, completion: { recorded: false } }),
    ];
    const posts = [
      // The reopen is a confirmed server mutation that refreshes the projection
      // with the replacement revision open and awaiting re-closure.
      { body: guidedWith("continue_discovery", false, { workspace: { version: 3, state: "open" }, discovery: { destination: "no_delivery_work", state: "active", reopenState: "reopened" }, currentness: { readiness: "stale", owner: "discovery_closure", blockedOperation: "progression", effect: "current closure packet and revision are incomplete", recoveryCategory: "close_current_discovery" }, recovery: { state: "required", category: "close_current_discovery", available: ["close_current_discovery"] } }) },
      { body: guidedWith("complete_feature", true, { workspace: { version: 4, state: "closed" }, discovery: { destination: "no_delivery_work", state: "closed", reopenState: "reclosed" } }) },
    ];
    const mocks = installMock(gets, posts);
    const { user, queryClient } = await renderJourney();

    await user.type(await screen.findByRole("textbox", { name: "Replacement integrated revision" }), "# Reopened discovery\n");
    await user.type(screen.getByRole("textbox", { name: "Reopen cause" }), "new exact evidence");
    await user.click(screen.getByRole("checkbox", { name: "Confirm guided action" }));
    await user.click(screen.getByRole("button", { name: "Reopen discovery" }));
    expect(await screen.findByText("reopened")).toBeInTheDocument();
    expect(mocks.postBodies[0]).toEqual({ expectedVersion: 2, action: "reopen_discovery", confirmation: true, cause: "new exact evidence", markdown: "# Reopened discovery\n" });
    expect(JSON.stringify(mocks.postBodies[0])).not.toMatch(/sha256/i);

    await freshCheckAndResume(user, queryClient);
    expect(await screen.findByRole("button", { name: "Close discovery" })).toBeInTheDocument();
    expect(screen.getByText("reopened")).toBeInTheDocument();
    await user.click(screen.getByRole("checkbox", { name: "Confirm guided action" }));
    await user.click(screen.getByRole("button", { name: "Close discovery" }));
    expect(await screen.findByRole("button", { name: "Complete feature" })).toBeInTheDocument();
  });

  it("refreshes the projection after a version conflict", async () => {
    const gets = [
      guidedWith("select_delivery_ticket", true, { delivery: { frontier: [frontierEntry] } }),
      // The fresh check after the conflict returns the newer authoritative state.
      guidedWith("author_ticket_design_brief", false, { workspace: { version: 3 }, delivery: { selectionState: "active", briefState: "none" } }),
    ];
    const posts = [{ status: 409, body: { error: "VERSION_CONFLICT", message: "stale" } }];
    installMock(gets, posts);
    const { user } = await renderJourney();
    await user.click(screen.getByRole("checkbox", { name: "Confirm guided action" }));
    await user.click(screen.getByRole("button", { name: "Select delivery ticket" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(/changed in another session/);
    expect(await screen.findByRole("button", { name: "Author Ticket Design Brief" })).toBeInTheDocument();
    expect(screen.getByText("Version 3")).toBeInTheDocument();
  });

  it("renders the server-selected blocked action for a stale projection", async () => {
    const stale = guidedWith("continue_discovery", false, {
      currentness: { readiness: "stale", owner: "feature_workspace", blockedOperation: "progression", effect: "The workspace basis is stale.", recoveryCategory: "replace_current_closure" },
      recovery: { state: "required", category: "replace_current_closure", available: ["replace_current_closure"] },
      diagnostics: { stale: ["stale_owner", "stale_effect"], historical: ["historical_basis_requires_recovery"] },
      availableActions: [{ action: "continue_discovery", primary: true, enabled: false, requiresConfirmation: false, blockedReason: "This workspace basis is stale. Follow the displayed recovery guidance before any progression." }],
      primaryAction: "continue_discovery",
    });
    const staleMocks = installMock([stale], []);
    await renderJourney();
    expect(await screen.findByRole("button", { name: "Continue discovery" })).toBeDisabled();
    expect(screen.getAllByRole("status").some((node) => node.textContent?.includes("stale"))).toBe(true);
    expect(screen.getByText(/historical_basis_requires_recovery/)).toBeInTheDocument();
    expect(staleMocks.postBodies).toHaveLength(0);
  });

  it("follows visible project and feature links, renders semantic destinations, and returns to resume", async () => {
    const guided = {
      workspace,
      project: { projectId: project.projectId, name: project.name },
      discovery: { state: "active", destination: "requirements", rationale: "Clarify the supported path.", continuation: "Resume the requirements route.", currentness: "current", basis: "", reopenState: "none" },
      authority: { currentRevisionNumber: 0, layers: [] },
      currentness: { readiness: "current", owner: "", blockedOperation: "", effect: "", recoveryCategory: "" },
      planning: { status: "not_started", candidateState: "none", reviewState: "none", approvalState: "none", promotionState: "none", candidateCount: 0, awaitingReview: 0, awaitingApproval: 0, awaitingPromotion: 0, promoted: 0, historicalCount: 0 },
      delivery: { frontier: [], selectionState: "none", packageState: "none", runState: "none", auditState: "none", remediationState: "none" },
      prototype: { runState: "none", cleanupState: "none", qaState: "none", evidenceState: "none", processOutcome: "" },
      completion: { gates: [{ name: "authority", ready: false }], ready: false, recorded: false },
      recovery: { state: "none", category: "", available: [] },
      diagnostics: { stale: [], historical: [], discovery: ["requirements frontier"], delivery: [], prototype: [] },
      availableActions: [{ action: "continue_discovery", primary: true, enabled: true, requiresConfirmation: true }],
      primaryAction: "continue_discovery",
      handoff: null,
    };
    installMock([{ guided }], []);
    const { user } = await renderJourney();
    expect(await screen.findByRole("heading", { name: "Delivery" })).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Prototype and QA" })).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Blockers and recovery" })).toBeInTheDocument();
    expect(screen.getAllByText("requirements").length).toBeGreaterThan(0);
    await user.click(screen.getByRole("link", { name: "Return to Relay" }));
    expect(await screen.findByRole("heading", { name: "Feature Workspaces" })).toBeInTheDocument();
  });

  it("advances the Requirements journey through review, explicit approval, and promotion to the Delivery Ticket", async () => {
    // Every server projection retains the closed Requirements destination; the
    // client never fabricates progression.
    const destination = { discovery: { destination: "requirements" } };
    const gets = [
      // Planner authoring starts the normal current candidate journey.
      guidedWith("author_requirements", false, { ...destination, planning: { status: "not_started", candidateCount: 0, awaitingReview: 0, awaitingPromotion: 0, promoted: 0 } }),
      // The planner's replacement candidate is admitted through the owner, so
      // the read-only review handoff is now primary.
      guidedWith("review_planning_candidate", false, { ...destination, planning: { status: "in_progress", candidateState: "admitted", reviewState: "awaiting_review", approvalState: "none", promotionState: "none", candidateCount: 1, awaitingReview: 1, awaitingApproval: 0, awaitingPromotion: 0, promoted: 0 } }),
      // The external ready review completion armed the distinct explicit
      // approval; re-entry resumes at the confirmed approve action.
      guidedWith("approve_planning_candidate", true, { ...destination, planning: { status: "in_progress", candidateState: "reviewed", reviewState: "reviewed", approvalState: "awaiting_approval", promotionState: "none", candidateCount: 1, awaitingReview: 0, awaitingApproval: 1, awaitingPromotion: 0, promoted: 0 } }),
    ];
    const posts = [
      { body: guidedWith("author_requirements", false, { ...destination, handoff: handoff("author_requirements", { ...emptyTransfer(), members: ["feature_owner", "planner"], authorityLayers: ["requirements"] }, "Planner authoring and review are prepared through their existing owners.") }) },
      { body: guidedWith("review_planning_candidate", false, { ...destination, handoff: handoff("review_planning_candidate", { ...emptyTransfer(), members: ["feature_owner"], authorityLayers: ["requirements"] }, "The auditor review surface is prepared through its existing owner envelope.") }) },
      // The confirmed explicit approval is the distinct mutation that advances
      // the reviewed candidate to the durable promotion stage.
      { body: guidedWith("promote_planning_candidate", false, { ...destination, planning: { status: "in_progress", candidateState: "reviewed", reviewState: "reviewed", approvalState: "approved", promotionState: "awaiting_promotion", candidateCount: 1, awaitingReview: 0, awaitingApproval: 0, awaitingPromotion: 1, promoted: 0 } }) },
      // Promotion publishes the Requirements authority server-side; the next
      // stage is the Delivery Ticket authoring surface.
      { body: guidedWith("author_delivery_ticket", false, { ...destination, workspace: { version: 3 }, authority: { currentRevisionNumber: 2, layers: ["requirements"] }, planning: { status: "promoted", candidateState: "promoted", reviewState: "ready_for_approval", approvalState: "approved", promotionState: "promoted", candidateCount: 1, awaitingReview: 0, awaitingApproval: 0, awaitingPromotion: 0, promoted: 1 } }) },
    ];
    const mocks = installMock(gets, posts);
    const { user, queryClient } = await renderJourney();

    // Planner authoring begins the candidate journey without exposing approval.
    expect(await screen.findByRole("button", { name: "Author Requirements" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Approve planning candidate" })).not.toBeInTheDocument();
    await user.click(await screen.findByRole("button", { name: "Author Requirements" }));
    expect(await screen.findByText("Closure members")).toBeInTheDocument();
    expect(screen.getByText(/feature_owner, planner/)).toBeInTheDocument();

    // Successive projection: the planner admitted the candidate; the read-only
    // auditor review handoff is primary.
    await freshCheckAndResume(user, queryClient);
    await user.click(await screen.findByRole("button", { name: "Review planning candidate" }));
    expect(await screen.findByText("Authority layers")).toBeInTheDocument();
    expect(screen.getByText("feature_owner")).toBeInTheDocument();

    // External ready-review completion armed the distinct explicit approval;
    // re-entry resumes at the confirmed approve action, never at promotion.
    await freshCheckAndResume(user, queryClient);
    expect(screen.getByRole("button", { name: "Approve planning candidate" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Promote planning candidate" })).not.toBeInTheDocument();
    expect(screen.getAllByRole("button")).toHaveLength(1);
    await user.click(screen.getByRole("checkbox", { name: "Confirm guided action" }));
    await user.click(screen.getByRole("button", { name: "Approve planning candidate" }));
    expect(await screen.findByRole("button", { name: "Promote planning candidate" })).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Promote planning candidate" }));
    expect(await screen.findByRole("button", { name: "Author Delivery Ticket" })).toBeInTheDocument();
    expect(mocks.postBodies[2]).toEqual({ expectedVersion: 2, action: "approve_planning_candidate", confirmation: true });
    expect(mocks.postBodies[3]).toEqual({ expectedVersion: 2, action: "promote_planning_candidate", confirmation: false });
    expect(JSON.stringify(mocks.postBodies[3])).not.toMatch(/(candidateId|approvalId|reviewId|workspaceId|sha256|rowId)/i);

    // Promotion publishes the Requirements authority; the next stage is the
    // Delivery Ticket authoring surface.
    expect(screen.getAllByText("requirements").length).toBeGreaterThan(0);
  });

  it("gates Shared Design behind Requirements promotion and advances through review, approval, and promotion to delivery", async () => {
    // Every server projection retains the closed requirements_then_shared_design
    // destination.
    const destination = { discovery: { destination: "requirements_then_shared_design" } };
    const requirementsReviewed = { planning: { status: "in_progress", candidateState: "reviewed", reviewState: "reviewed", approvalState: "awaiting_approval", promotionState: "none", candidateCount: 1, awaitingReview: 0, awaitingApproval: 1, awaitingPromotion: 0, promoted: 0 } };
    const requirementsPromoted = { authority: { currentRevisionNumber: 2, layers: ["requirements"] }, planning: { status: "in_progress", candidateState: "reviewed", reviewState: "reviewed", approvalState: "approved", promotionState: "awaiting_promotion", candidateCount: 1, awaitingReview: 0, awaitingApproval: 0, awaitingPromotion: 1, promoted: 0 } };
    const sharedDesignAdmitted = { planning: { status: "in_progress", candidateState: "admitted", reviewState: "awaiting_review", approvalState: "approved", promotionState: "awaiting_promotion", candidateCount: 2, awaitingReview: 1, awaitingApproval: 0, awaitingPromotion: 0, promoted: 1 } };
    const sharedDesignReviewed = { planning: { status: "in_progress", candidateState: "reviewed", reviewState: "reviewed", approvalState: "awaiting_approval", promotionState: "awaiting_promotion", candidateCount: 2, awaitingReview: 0, awaitingApproval: 1, awaitingPromotion: 0, promoted: 1 } };
    const sharedDesignApproved = { planning: { status: "in_progress", candidateState: "reviewed", reviewState: "reviewed", approvalState: "approved", promotionState: "awaiting_promotion", candidateCount: 2, awaitingReview: 0, awaitingApproval: 0, awaitingPromotion: 1, promoted: 1 } };
    const bothPromoted = { workspace: { version: 4 }, authority: { currentRevisionNumber: 3, layers: ["requirements", "shared_design"] }, planning: { status: "promoted", candidateState: "promoted", reviewState: "reviewed", approvalState: "approved", promotionState: "promoted", candidateCount: 2, awaitingReview: 0, awaitingApproval: 0, awaitingPromotion: 0, promoted: 2 } };

    const gets = [
      // Requirements authoring precedes all Shared Design work.
      guidedWith("author_requirements", false, { ...destination, planning: { status: "not_started", candidateCount: 0, awaitingReview: 0, awaitingPromotion: 0, promoted: 0 } }),
      guidedWith("review_planning_candidate", false, { ...destination, planning: { status: "in_progress", candidateState: "admitted", reviewState: "awaiting_review", approvalState: "none", promotionState: "none", candidateCount: 1, awaitingReview: 1, awaitingApproval: 0, awaitingPromotion: 0, promoted: 0 } }),
      // The ready review armed the distinct explicit Requirements approval.
      guidedWith("approve_planning_candidate", true, { ...destination, ...requirementsReviewed }),
      // Successive projection: the shared design candidate was admitted.
      guidedWith("review_planning_candidate", false, { ...destination, ...sharedDesignAdmitted }),
      // The ready review armed the distinct explicit Shared Design approval.
      guidedWith("approve_planning_candidate", true, { ...destination, ...sharedDesignReviewed }),
      guidedWith("author_delivery_ticket", false, { ...destination, ...bothPromoted }),
    ];
    const posts = [
      { body: guidedWith("author_requirements", false, { ...destination, handoff: handoff("author_requirements", { ...emptyTransfer(), members: ["feature_owner", "planner"], authorityLayers: ["requirements"] }, "Planner authoring and review are prepared through their existing owners.") }) },
      { body: guidedWith("review_planning_candidate", false, { ...destination, handoff: handoff("review_planning_candidate", { ...emptyTransfer(), members: ["feature_owner"], authorityLayers: ["requirements"] }, "The auditor review surface is prepared through its existing owner envelope.") }) },
      // The confirmed Requirements approval advances to its promotion.
      { body: guidedWith("promote_planning_candidate", false, { ...destination, ...requirementsPromoted }) },
      // After Requirements promotion the next stage is Author Shared Design.
      { body: guidedWith("author_shared_design", false, { ...destination, ...requirementsPromoted }) },
      { body: guidedWith("author_shared_design", false, { ...destination, handoff: handoff("author_shared_design", { ...emptyTransfer(), members: ["feature_owner", "planner"], authorityLayers: ["shared_design"] }, "Planner authoring and review are prepared through their existing owners.") }) },
      { body: guidedWith("review_planning_candidate", false, { ...destination, handoff: handoff("review_planning_candidate", { ...emptyTransfer(), members: ["feature_owner"], authorityLayers: ["shared_design"] }, "The auditor review surface is prepared through its existing owner envelope.") }) },
      // The confirmed Shared Design approval advances to its promotion.
      { body: guidedWith("promote_planning_candidate", false, { ...destination, ...sharedDesignApproved }) },
      { body: guidedWith("author_delivery_ticket", false, { ...destination, ...bothPromoted }) },
    ];
    installMock(gets, posts);
    const { user, queryClient } = await renderJourney();

    // Requirements authoring and review complete before external ready
    // completion arms the distinct explicit approval for the guided resume.
    expect(await screen.findByRole("button", { name: "Author Requirements" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Approve planning candidate" })).not.toBeInTheDocument();
    await user.click(await screen.findByRole("button", { name: "Author Requirements" }));
    expect(await screen.findByText(/feature_owner, planner/)).toBeInTheDocument();
    await freshCheckAndResume(user, queryClient);
    await user.click(await screen.findByRole("button", { name: "Review planning candidate" }));
    expect(await screen.findByText("Closure members")).toBeInTheDocument();
    await freshCheckAndResume(user, queryClient);
    await user.click(screen.getByRole("checkbox", { name: "Confirm guided action" }));
    await user.click(await screen.findByRole("button", { name: "Approve planning candidate" }));
    expect(await screen.findByRole("button", { name: "Promote planning candidate" })).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Promote planning candidate" }));

    // Only after Requirements promotion does Shared Design appear.
    expect(await screen.findByRole("button", { name: "Author Shared Design" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Review planning candidate" })).not.toBeInTheDocument();

    // Shared Design: author, admit, review, explicit approval, promotion.
    await user.click(await screen.findByRole("button", { name: "Author Shared Design" }));
    expect(await screen.findByText("shared_design")).toBeInTheDocument();
    await freshCheckAndResume(user, queryClient);
    await user.click(await screen.findByRole("button", { name: "Review planning candidate" }));
    expect(await screen.findByText("Closure members")).toBeInTheDocument();
    await freshCheckAndResume(user, queryClient);
    await user.click(screen.getByRole("checkbox", { name: "Confirm guided action" }));
    await user.click(await screen.findByRole("button", { name: "Approve planning candidate" }));
    expect(await screen.findByRole("button", { name: "Promote planning candidate" })).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Promote planning candidate" }));

    // Only after both families are promoted does the Delivery-stage action
    // appear; return/resume still reaches the final state.
    expect(await screen.findByRole("button", { name: "Author Delivery Ticket" })).toBeInTheDocument();
    await returnProject(user);
    expect(await screen.findByRole("button", { name: "Author Delivery Ticket" })).toBeInTheDocument();
  });

  it("covers the AC6 normal-entry journey from Delivery Ticket authoring through both explicit approvals to Run continuation and visible resume", async () => {
    // Every projection is server-emittable. The frontier appears only after
    // the owner-backed production transition (promote_planning_candidate)
    // publishes the produced Ticket; the client never manufactures it.
    const gets = [
      // The closed direct-delivery workspace waits for Delivery Ticket authoring.
      guidedWith("author_delivery_ticket", false, { planning: { status: "not_started", candidateState: "none", reviewState: "none", approvalState: "none", promotionState: "none", candidateCount: 0, awaitingReview: 0, awaitingApproval: 0, awaitingPromotion: 0, promoted: 0 } }),
      // The planner admitted the Delivery Ticket candidate through the owner.
      guidedWith("review_planning_candidate", false, { planning: { status: "in_progress", candidateState: "admitted", reviewState: "awaiting_review", approvalState: "none", promotionState: "none", candidateCount: 1, awaitingReview: 1, awaitingApproval: 0, awaitingPromotion: 0, promoted: 0 } }),
      // The ready review armed the distinct explicit planning approval.
      guidedWith("approve_planning_candidate", true, { planning: { status: "in_progress", candidateState: "reviewed", reviewState: "reviewed", approvalState: "awaiting_approval", promotionState: "none", candidateCount: 1, awaitingReview: 0, awaitingApproval: 1, awaitingPromotion: 0, promoted: 0 } }),
      // The planner admitted the authored Brief through the delivery owner.
      guidedWith("review_ticket_design_brief", false, { workspace: { version: 3 }, delivery: { selectionState: "active", briefState: "authored" }, diagnostics: { delivery: ["ticket_design_brief=authored"] } }),
      // The ready brief review armed the distinct explicit brief approval.
      guidedWith("approve_ticket_design_brief", true, { workspace: { version: 3 }, delivery: { selectionState: "active", briefState: "authored" }, diagnostics: { delivery: ["ticket_design_brief=reviewed_ready"] } }),
      // The Run owner advanced the launched Run to its source-backed continuation.
      guidedWith("continue_run", false, { workspace: { version: 4 }, delivery: { selectionState: "consumed", briefState: "approved", packageState: "approved", runState: "executing" } }),
      // Visible Project -> Feature resume presents the same continuation.
      guidedWith("continue_run", false, { workspace: { version: 4 }, delivery: { selectionState: "consumed", briefState: "approved", packageState: "approved", runState: "executing" } }),
    ];
    const posts = [
      // Authoring transfers the Delivery Ticket to the planner.delivery_ticket operation.
      { body: guidedWith("author_delivery_ticket", false, { handoff: handoff("author_delivery_ticket", { ...emptyTransfer(), ticket: { ticketId: "", revisionNumber: 0, readiness: [], operationId: "planner.delivery_ticket" } }, "Enter the Delivery Ticket authoring operation (planner.delivery_ticket), then return here when the resulting Ticket is ready for selection.") }) },
      // The read-only auditor review handoff for the admitted candidate.
      { body: guidedWith("review_planning_candidate", false, { handoff: handoff("review_planning_candidate", { ...emptyTransfer(), members: ["feature_owner"], authorityLayers: ["delivery_ticket"] }, "The current Delivery Ticket candidate is prepared for the exact read-only auditor.delivery_ticket_review handoff.") }) },
      // The confirmed planning candidate approval advances to production.
      { body: guidedWith("promote_planning_candidate", false, { planning: { status: "in_progress", candidateState: "reviewed", reviewState: "reviewed", approvalState: "approved", promotionState: "awaiting_promotion", candidateCount: 1, awaitingReview: 0, awaitingApproval: 0, awaitingPromotion: 1, promoted: 0 } }) },
      // The owner-backed production publishes the produced Ticket into the frontier.
      { body: guidedBody({ workspace: { version: 3 }, delivery: { frontier: [frontierEntry], selectionState: "none" }, ...primaryAction("select_delivery_ticket", true) }) },
      // Selecting the frontier ticket makes Brief authoring the only successor.
      { body: guidedBody({ workspace: { version: 3 }, delivery: { selectionState: "active", briefState: "none" }, ...primaryAction("author_ticket_design_brief", false) }) },
      { body: guidedWith("author_ticket_design_brief", false, { workspace: { version: 3 }, delivery: { selectionState: "active", briefState: "none" }, handoff: handoff("author_ticket_design_brief", { ...emptyTransfer(), ticket: { ticketId: "P5-T1", revisionNumber: 2, readiness: ["brief_required"], operationId: "planner.ticket_design_brief" } }, "Author the selected Ticket Design Brief through its planner operation.") }) },
      { body: guidedWith("review_ticket_design_brief", false, { workspace: { version: 3 }, delivery: { selectionState: "active", briefState: "authored" }, handoff: handoff("review_ticket_design_brief", { ...emptyTransfer(), ticket: { ticketId: "P5-T1", revisionNumber: 2, readiness: ["brief_admitted"], operationId: "auditor.ticket_design_brief_review" } }, "Review the admissible Ticket Design Brief for the selected Delivery Ticket through the auditor.ticket_design_brief_review surface.") }) },
      // The confirmed brief approval is the distinct mutation advancing to package preparation.
      { body: guidedBody({ workspace: { version: 3 }, delivery: { selectionState: "active", briefState: "approved", packageState: "none" }, ...primaryAction("prepare_package", false) }) },
      { body: guidedBody({ workspace: { version: 3 }, delivery: { selectionState: "active", briefState: "approved", packageState: "prepared" }, diagnostics: { delivery: ["execution_package_prepared"] }, ...primaryAction("approve_package", true) }) },
      { body: guidedBody({ workspace: { version: 4 }, delivery: { selectionState: "consumed", briefState: "approved", packageState: "approved", runState: "setup_ready" }, ...primaryAction("launch_run", false) }) },
      { body: guidedWith("launch_run", false, { workspace: { version: 4 }, delivery: { selectionState: "consumed", briefState: "approved", packageState: "approved", runState: "setup_ready" }, handoff: handoff("launch_run", { ...emptyTransfer(), run: { runId: "run-1", status: "setup_ready", repoTarget: "relay", branch: "main", baseCommit: "a".repeat(40), packageId: "package-1" } }, "The package Run is identified through its existing owner.") }) },
      { body: guidedWith("continue_run", false, { workspace: { version: 4 }, delivery: { selectionState: "consumed", briefState: "approved", packageState: "approved", runState: "executing" }, handoff: handoff("continue_run", { ...emptyTransfer(), run: { runId: "run-1", status: "executing", repoTarget: "relay", branch: "main", baseCommit: "a".repeat(40), packageId: "package-1" } }, "The active package Run is identified through its existing owner.") }) },
    ];
    const mocks = installMock(gets, posts);
    const { user, queryClient } = await renderJourney();

    // Author Delivery Ticket hands off to the planner.delivery_ticket operation.
    expect(await screen.findByRole("button", { name: "Author Delivery Ticket" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Approve planning candidate" })).not.toBeInTheDocument();
    expect(screen.getAllByRole("button")).toHaveLength(1);
    await user.click(screen.getByRole("button", { name: "Author Delivery Ticket" }));
    expect((await screen.findAllByText(/planner\.delivery_ticket/)).length).toBeGreaterThan(0);

    // The planner admitted the candidate; the read-only auditor review handoff
    // is primary and no approval is exposed yet.
    await freshCheckAndResume(user, queryClient);
    await user.click(await screen.findByRole("button", { name: "Review planning candidate" }));
    expect(await screen.findByText(/auditor\.delivery_ticket_review/)).toBeInTheDocument();

    // The ready review armed the distinct explicit confirmed planning approval.
    await freshCheckAndResume(user, queryClient);
    expect(screen.getByRole("button", { name: "Approve planning candidate" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Promote planning candidate" })).not.toBeInTheDocument();
    expect(screen.getAllByRole("button")).toHaveLength(1);
    await user.click(screen.getByRole("checkbox", { name: "Confirm guided action" }));
    await user.click(screen.getByRole("button", { name: "Approve planning candidate" }));
    expect(await screen.findByRole("button", { name: "Promote planning candidate" })).toBeInTheDocument();

    // The owner-backed production publishes the produced Ticket into the
    // frontier; only this transition makes selection visible.
    await user.click(screen.getByRole("button", { name: "Promote planning candidate" }));
    expect(await screen.findByRole("button", { name: "Select delivery ticket" })).toBeInTheDocument();
    expect(screen.getByText("P5-T1 v2 (priority 60, relay @ main)")).toBeInTheDocument();

    // Select the frontier ticket server-side, then author the Brief.
    await user.click(screen.getByRole("checkbox", { name: "Confirm guided action" }));
    await user.click(screen.getByRole("button", { name: "Select delivery ticket" }));
    expect(await screen.findByRole("button", { name: "Author Ticket Design Brief" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Prepare package" })).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Author Ticket Design Brief" }));
    expect(await screen.findByText(/planner\.ticket_design_brief/)).toBeInTheDocument();

    // The planner admitted the Brief; the read-only auditor review handoff is
    // primary and the explicit approval is not exposed yet.
    await freshCheckAndResume(user, queryClient);
    await user.click(await screen.findByRole("button", { name: "Review Ticket Design Brief" }));
    expect((await screen.findAllByText(/auditor\.ticket_design_brief_review/)).length).toBeGreaterThan(0);

    // The ready brief review armed the distinct explicit confirmed approval.
    await freshCheckAndResume(user, queryClient);
    expect(screen.getByRole("button", { name: "Approve Ticket Design Brief" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Prepare package" })).not.toBeInTheDocument();
    expect(screen.getAllByRole("button")).toHaveLength(1);
    await user.click(screen.getByRole("checkbox", { name: "Confirm guided action" }));
    await user.click(screen.getByRole("button", { name: "Approve Ticket Design Brief" }));
    expect(await screen.findByRole("button", { name: "Prepare package" })).toBeInTheDocument();

    // Prepare the execution package, then approve it explicitly.
    await user.click(screen.getByRole("button", { name: "Prepare package" }));
    expect(await screen.findByText("execution_package_prepared")).toBeInTheDocument();
    await user.click(screen.getByRole("checkbox", { name: "Confirm guided action" }));
    await user.click(screen.getByRole("button", { name: "Approve package" }));
    expect(await screen.findByRole("button", { name: "Launch run" })).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Launch run" }));
    expect(await screen.findByText(/run-1/)).toBeInTheDocument();

    // The Run owner advanced the launched Run; continue, not relaunch.
    await freshCheckAndResume(user, queryClient);
    expect(await screen.findByRole("button", { name: "Continue run" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Launch run" })).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Continue run" }));
    expect((await screen.findAllByText(/executing/)).length).toBeGreaterThan(0);

    // Return to the Project and resume the Feature through visible navigation.
    await returnProject(user);
    expect(await screen.findByRole("button", { name: "Continue run" })).toBeInTheDocument();

    const expectedIntents = [
      { expectedVersion: 2, action: "author_delivery_ticket", confirmation: false },
      { expectedVersion: 2, action: "review_planning_candidate", confirmation: false },
      { expectedVersion: 2, action: "approve_planning_candidate", confirmation: true },
      { expectedVersion: 2, action: "promote_planning_candidate", confirmation: false },
      { expectedVersion: 3, action: "select_delivery_ticket", confirmation: true },
      { expectedVersion: 3, action: "author_ticket_design_brief", confirmation: false },
      { expectedVersion: 3, action: "review_ticket_design_brief", confirmation: false },
      { expectedVersion: 3, action: "approve_ticket_design_brief", confirmation: true },
      { expectedVersion: 3, action: "prepare_package", confirmation: false },
      { expectedVersion: 3, action: "approve_package", confirmation: true },
      { expectedVersion: 4, action: "launch_run", confirmation: false },
      { expectedVersion: 4, action: "continue_run", confirmation: false },
    ];
    expect(mocks.postBodies).toHaveLength(expectedIntents.length);
    mocks.postBodies.forEach((body, index) => expectStableGuidedIntent(body, expectedIntents[index]));
  });
});
