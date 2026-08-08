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
      delivery: { selectionState: "active" },
      ...primaryAction("prepare_package", false),
    },
    nextButton: "Prepare package",
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
      [{ body: guidedBody({ workspace: { version: 3 }, delivery: { selectionState: "active" }, ...primaryAction("prepare_package", false) }) }],
    );
    const { user } = await renderJourney();
    await user.click(screen.getByRole("checkbox", { name: "Confirm guided action" }));
    await user.click(screen.getByRole("button", { name: "Select delivery ticket" }));
    await screen.findByRole("button", { name: "Prepare package" });
    expect(mocks.postBodies).toHaveLength(1);
    expect(mocks.postBodies[0]).toEqual({ expectedVersion: 2, action: "select_delivery_ticket", confirmation: true });
    expect(Object.keys(mocks.postBodies[0]).sort()).toEqual(["action", "confirmation", "expectedVersion"]);
    expect(JSON.stringify(mocks.postBodies[0])).not.toMatch(/(ticketId|packageId|runId|workspaceId|sha256|rowId)/i);
  });

  it("traverses delivery selection through package, run, audit, and remediation handoffs", async () => {
    const gets = [
      guidedWith("select_delivery_ticket", true, { delivery: { frontier: [frontierEntry] } }),
      // Fresh currentness check: package prepared by the existing package owner.
      guidedWith("approve_package", true, { workspace: { version: 3 }, delivery: { selectionState: "active", packageState: "prepared" } }),
      // Fresh currentness check: approved package created the audit-ready Run.
      guidedWith("prepare_audit", false, { workspace: { version: 4 }, delivery: { selectionState: "consumed", packageState: "approved", runState: "audit_ready", auditState: "awaiting_audit" } }),
      // Fresh currentness check: audit packet recorded by the audit owner.
      guidedWith("record_audit_decision", false, { workspace: { version: 5 }, delivery: { selectionState: "consumed", packageState: "approved", runState: "audit_ready", auditState: "packet_recorded" } }),
    ];
    const posts = [
      { body: guidedBody({ workspace: { version: 3 }, delivery: { selectionState: "active" }, ...primaryAction("prepare_package", false) }) },
      { body: guidedWith("prepare_package", false, { handoff: handoff("prepare_package", { ...emptyTransfer(), ticket: { ticketId: "P5-T1", revisionNumber: 2, readiness: ["design_admitted"], operationId: "planner.ticket_design_brief" } }, "The selected Delivery Ticket is identified through the delivery owner.") }) },
      { body: guidedBody({ workspace: { version: 4 }, delivery: { selectionState: "consumed", packageState: "approved", runState: "setup_ready" }, ...primaryAction("launch_run", false) }) },
      { body: guidedWith("launch_run", false, { handoff: handoff("launch_run", { ...emptyTransfer(), run: { runId: "run-1", status: "setup_ready", repoTarget: "relay", branch: "main", baseCommit: "a".repeat(40), packageId: "package-1" } }, "The package Run is identified through its existing owner.") }) },
      { body: guidedWith("prepare_audit", false, { handoff: handoff("prepare_audit", { ...emptyTransfer(), audit: { runId: "run-1", runStatus: "audit_ready", auditState: "awaiting_audit", auditPacketId: "", auditedCommit: "" } }, "The workflow audit state is identified through the audit owner.") }) },
      // The recorded audit decision opens remediation server-side.
      { body: guidedBody({ workspace: { version: 6 }, delivery: { selectionState: "consumed", packageState: "approved", runState: "completed", auditState: "decision_recorded", remediationState: "open" }, ...primaryAction("remediate", false) }) },
      { body: guidedWith("remediate", false, { handoff: handoff("remediate", { ...emptyTransfer(), remediation: { state: "open", seedIds: ["seed-1", "seed-2"] } }, "The audit remediation seed is identified through the audit owner.") }) },
    ];
    installMock(gets, posts);
    const { user, queryClient } = await renderJourney();

    // Server-selected frontier selection, confirmed by the operator.
    await user.click(screen.getByRole("checkbox", { name: "Confirm guided action" }));
    await user.click(screen.getByRole("button", { name: "Select delivery ticket" }));
    await user.click(await screen.findByRole("button", { name: "Prepare package" }));
    expect(await screen.findByText("Operation transfer")).toBeInTheDocument();
    expect(screen.getByText(/P5-T1 v2/)).toBeInTheDocument();

    // Fresh currentness check: the package owner prepared the package.
    await freshCheckAndResume(user, queryClient);
    await user.click(screen.getByRole("checkbox", { name: "Confirm guided action" }));
    await user.click(await screen.findByRole("button", { name: "Approve package" }));
    await user.click(await screen.findByRole("button", { name: "Launch run" }));
    expect(await screen.findByText(/run-1/)).toBeInTheDocument();
    expect(screen.getByText(/setup_ready/)).toBeInTheDocument();

    // Fresh currentness check: the Run reached audit readiness.
    await freshCheckAndResume(user, queryClient);
    await user.click(await screen.findByRole("button", { name: "Prepare audit" }));
    expect(await screen.findByText(/awaiting_audit/)).toBeInTheDocument();

    // Fresh currentness check: the audit packet is recorded.
    await freshCheckAndResume(user, queryClient);
    await user.click(await screen.findByRole("button", { name: "Record audit decision" }));
    await user.click(await screen.findByRole("button", { name: "Remediate" }));
    expect(await screen.findByText(/seed-1, seed-2/)).toBeInTheDocument();
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
      guidedWith("prepare_package", false, { workspace: { version: 3 }, delivery: { selectionState: "active" } }),
    ];
    const posts = [{ status: 409, body: { error: "VERSION_CONFLICT", message: "stale" } }];
    installMock(gets, posts);
    const { user } = await renderJourney();
    await user.click(screen.getByRole("checkbox", { name: "Confirm guided action" }));
    await user.click(screen.getByRole("button", { name: "Select delivery ticket" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(/changed in another session/);
    expect(await screen.findByRole("button", { name: "Prepare package" })).toBeInTheDocument();
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
});
