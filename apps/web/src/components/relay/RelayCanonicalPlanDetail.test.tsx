// @vitest-environment jsdom

import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { RelayCanonicalPlanDetail } from "./RelayCanonicalPlanDetail";
import type { WorkflowPlanDetail } from "@/features/relay-plans";

vi.mock("@tanstack/react-router", () => ({
  Link: ({ children, to }: any) => <a href={to}>{children}</a>,
}));

const detail: WorkflowPlanDetail = {
  plan: {
    planId: "plan-1",
    project: {
      projectId: "project-1",
      name: "Current Project",
      status: "active",
    },
    featureSlug: "feature",
    status: "active",
    canonicalSha256: "a".repeat(64),
    createdAt: "2026-07-08T00:00:00Z",
    updatedAt: "2026-07-08T00:00:00Z",
    passCount: 1,
    completedPassCount: 0,
    inProgressPassCount: 0,
    plannedPassCount: 1,
    currentPassId: "pass-1",
  },
  repositories: [
    {
      repoTarget: "relay",
      branch: "feat/simplification",
      planningBaseCommit: "b".repeat(40),
      sequence: 1,
    },
  ],
  passes: [
    {
      passId: "pass-1",
      number: 1,
      name: "First",
      repoTarget: "relay",
      status: "planned",
      dependsOn: [],
      createdAt: "2026-07-08T00:00:00Z",
      updatedAt: "2026-07-08T00:00:00Z",
      runs: [],
    },
  ],
  artifacts: [],
};

describe("RelayCanonicalPlanDetail", () => {
  it("renders the normalized ordered pass collection", () => {
    render(<RelayCanonicalPlanDetail detail={detail} />);

    expect(screen.getByText("Pass 1: First")).toBeInTheDocument();
    expect(screen.getByText("pass-1 · relay · planned")).toBeInTheDocument();
  });

  // Legacy Plan writes are retired: this presentation surface exposes no Plan
  // mutation control and no control that authorizes execution.
  it("exposes no Plan mutation control and authorizes no execution", () => {
    render(<RelayCanonicalPlanDetail detail={detail} />);

    expect(screen.queryByRole("button", { name: /Move Plan/i })).toBeNull();
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(screen.queryByRole("link", { name: /Create Managed Run/i })).toBeNull();
    for (const link of screen.queryAllByRole("link")) {
      expect(link.getAttribute("href") ?? "").not.toContain(
        "/execution-packages/new",
      );
    }
  });
});
