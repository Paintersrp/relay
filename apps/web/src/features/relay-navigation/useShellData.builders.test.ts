// Unit tests for the pure shell-data composition builders.
//
// These cover the non-authoritative re-shaping logic of the
// Shell_Data_Composition_Layer (useShellData) independently of React / TanStack
// Query: scope options (Req 2.4), the entity search corpus (Req 5.7/5.8),
// recents corpora (Req 4.2 source), attention inputs (Req 3.2), recent-activity
// items (Req 3.3), and run-ancestor resolution with omission-not-fabrication
// (Req 2.7, 2.10).

import { describe, expect, it } from "vitest";

import type { RelayRun } from "@/features/relay-runs";

import {
  buildAttentionInputs,
  buildRecentActivityItems,
  buildRecentsCorpora,
  buildRunHierarchy,
  buildScopeOptions,
  buildSearchCorpus,
  type ShellPlanLike,
  type ShellProjectLike,
  type ShellRunLike,
} from "./useShellData";

const runs: ShellRunLike[] = [
  { id: "run-1", title: "First Run", status: "blocked", updatedAt: "2024-01-03T00:00:00Z" },
  { id: "run-2", name: "Second Run", status: "completed", updatedAt: "2024-01-05T00:00:00Z" },
];

const plans: ShellPlanLike[] = [
  { planId: "plan-1", title: "Alpha Plan", updatedAt: "2024-01-04T00:00:00Z", projectId: "proj-1" },
];

const projects: ShellProjectLike[] = [
  { projectId: "proj-1", name: "Core Project", updatedAt: "2024-01-02T00:00:00Z" },
];

describe("buildScopeOptions", () => {
  it("lists projects first then plans with navigable routes (Req 2.4)", () => {
    const options = buildScopeOptions(projects, plans);
    expect(options).toEqual([
      {
        kind: "project",
        id: "proj-1",
        label: "Core Project",
        to: "/projects/$projectId",
        params: { projectId: "proj-1" },
      },
      {
        kind: "plan",
        id: "plan-1",
        label: "Alpha Plan",
        to: "/plans/$planId",
        params: { planId: "plan-1" },
      },
    ]);
  });

  it("returns an empty list when no scopes exist", () => {
    expect(buildScopeOptions([], [])).toEqual([]);
  });
});

describe("buildSearchCorpus", () => {
  it("includes projects, plans, passes, and runs as searchable entities (Req 5.7)", () => {
    const corpus = buildSearchCorpus({
      runs,
      plans,
      projects,
      passesByPlanId: {
        "plan-1": [{ passId: "pass-1", name: "Bootstrap", sequence: 1 }],
      },
    });

    expect(corpus).toContainEqual({
      type: "project",
      id: "proj-1",
      name: "Core Project",
      to: "/projects/$projectId",
      params: { projectId: "proj-1" },
    });
    expect(corpus).toContainEqual({
      type: "plan",
      id: "plan-1",
      name: "Alpha Plan",
      to: "/plans/$planId",
      params: { planId: "plan-1" },
    });
    expect(corpus).toContainEqual({
      type: "pass",
      id: "pass-1",
      name: "1. Bootstrap",
      to: "/plans/$planId/passes/$passId",
      params: { planId: "plan-1", passId: "pass-1" },
    });
    expect(corpus).toContainEqual({
      type: "run",
      id: "run-1",
      name: "First Run",
      to: "/runs/$runId",
      params: { runId: "run-1" },
    });
  });

  it("omits passes for plans with no resolved detail (never fabricates) (Req 2.10)", () => {
    const corpus = buildSearchCorpus({ runs: [], plans, projects: [], passesByPlanId: {} });
    expect(corpus.some((entity) => entity.type === "pass")).toBe(false);
  });
});

describe("buildRecentsCorpora", () => {
  it("reshapes each entity group into id/label/updatedAt inputs", () => {
    const corpora = buildRecentsCorpora({ runs, plans, projects });
    expect(corpora.runs).toEqual([
      { id: "run-1", label: "First Run", updatedAt: "2024-01-03T00:00:00Z" },
      { id: "run-2", label: "Second Run", updatedAt: "2024-01-05T00:00:00Z" },
    ]);
    expect(corpora.plans).toEqual([
      { id: "plan-1", label: "Alpha Plan", updatedAt: "2024-01-04T00:00:00Z" },
    ]);
    expect(corpora.projects).toEqual([
      { id: "proj-1", label: "Core Project", updatedAt: "2024-01-02T00:00:00Z" },
    ]);
  });
});

describe("buildAttentionInputs", () => {
  it("maps runs to attention inputs preserving canonical status (Req 3.2)", () => {
    expect(buildAttentionInputs(runs)).toEqual([
      { id: "run-1", label: "First Run", status: "blocked", updatedAt: "2024-01-03T00:00:00Z" },
      { id: "run-2", label: "Second Run", status: "completed", updatedAt: "2024-01-05T00:00:00Z" },
    ]);
  });
});

describe("buildRecentActivityItems", () => {
  it("produces navigable items across runs, plans, and projects (Req 3.3)", () => {
    const items = buildRecentActivityItems({ runs, plans, projects });
    expect(items).toHaveLength(4);
    expect(items).toContainEqual({
      type: "run",
      id: "run-2",
      label: "Second Run",
      updatedAt: "2024-01-05T00:00:00Z",
      to: "/runs/$runId",
      params: { runId: "run-2" },
    });
    expect(items).toContainEqual({
      type: "plan",
      id: "plan-1",
      label: "Alpha Plan",
      updatedAt: "2024-01-04T00:00:00Z",
      to: "/plans/$planId",
      params: { planId: "plan-1" },
    });
    expect(items).toContainEqual({
      type: "project",
      id: "proj-1",
      label: "Core Project",
      updatedAt: "2024-01-02T00:00:00Z",
      to: "/projects/$projectId",
      params: { projectId: "proj-1" },
    });
  });
});

describe("buildRunHierarchy", () => {
  function makeRun(overrides: Partial<RelayRun>): RelayRun {
    return { id: "run-1", title: "First Run", name: "First Run", ...overrides } as RelayRun;
  }

  it("returns an empty hierarchy for a missing run", () => {
    expect(buildRunHierarchy(null, plans, projects)).toEqual({});
  });

  it("returns only the run leaf for a standalone run (Req 2.6)", () => {
    const hierarchy = buildRunHierarchy(makeRun({ planContext: undefined }), plans, projects);
    expect(hierarchy).toEqual({ run: { id: "run-1", label: "First Run" } });
  });

  it("resolves plan, pass, and project ancestors from planContext + loaded lists", () => {
    const hierarchy = buildRunHierarchy(
      makeRun({
        planContext: {
          planId: "plan-1",
          planTitle: "Alpha Plan",
          passId: "pass-1",
          passName: "Bootstrap",
          passSequence: 1,
        },
      }),
      plans,
      projects,
    );

    expect(hierarchy.run).toEqual({ id: "run-1", label: "First Run" });
    expect(hierarchy.plan).toEqual({ id: "plan-1", label: "Alpha Plan" });
    expect(hierarchy.pass).toEqual({ id: "pass-1", label: "Bootstrap", sequence: 1 });
    expect(hierarchy.project).toEqual({ id: "proj-1", label: "Core Project" });
  });

  it("omits the project ancestor when the plan/project cannot be resolved (Req 2.7, 2.10)", () => {
    const hierarchy = buildRunHierarchy(
      makeRun({ planContext: { planId: "plan-unknown", planTitle: "Ghost Plan" } }),
      plans,
      projects,
    );

    expect(hierarchy.plan).toEqual({ id: "plan-unknown", label: "Ghost Plan" });
    expect(hierarchy.project).toBeUndefined();
    expect(hierarchy.pass).toBeUndefined();
  });
});
