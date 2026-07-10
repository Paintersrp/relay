// @vitest-environment jsdom

import type React from "react";
import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { RelayCanonicalPlanPassDetail } from "./RelayCanonicalPlanPassDetail";
import type {
  WorkflowPlanPass,
  WorkflowPlanSummary,
} from "@/features/relay-plans";

vi.mock("@tanstack/react-router", () => ({
  Link: ({ children, to }: { children: React.ReactNode; to: string }) => (
    <a href={to}>{children}</a>
  ),
}));

const plan: WorkflowPlanSummary = {
  planId: "plan-1",
  project: {
    projectId: "project-1",
    name: "Relay",
    status: "active",
  },
  featureSlug: "relay-specification-workflow-pivot",
  status: "active",
  canonicalSha256: "a".repeat(64),
  createdAt: "2026-07-08T00:00:00Z",
  updatedAt: "2026-07-08T00:00:00Z",
  passCount: 1,
  completedPassCount: 0,
  inProgressPassCount: 0,
  plannedPassCount: 1,
  currentPassId: "pass-1",
};

const pass: WorkflowPlanPass = {
  passId: "pass-1",
  number: 1,
  name: "Canonical frontend",
  repoTarget: "relay",
  status: "planned",
  dependsOn: [],
  createdAt: "2026-07-08T00:00:00Z",
  updatedAt: "2026-07-08T00:00:00Z",
  runs: [],
};

describe("RelayCanonicalPlanPassDetail", () => {
  it("renders a pass with empty dependencies and Runs as valid detail state", () => {
    render(<RelayCanonicalPlanPassDetail plan={plan} pass={pass} />);

    expect(
      screen.getByRole("heading", { name: "Pass 1: Canonical frontend" }),
    ).toBeInTheDocument();
    expect(screen.getByText("pass-1 · relay")).toBeInTheDocument();
    expect(screen.getByText("Dependencies: None")).toBeInTheDocument();
    expect(
      screen.getByText("No Runs have been created for this pass."),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("link", { name: /Create Managed Run/ }),
    ).toBeInTheDocument();
  });
});