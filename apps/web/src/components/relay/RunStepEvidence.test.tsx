// @vitest-environment jsdom

import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

import { RunStepEvidence } from "./RunStepEvidence";
import type { RelayArtifact, RelayLogPreview, RelayRun } from "@/features/relay-runs";
import type { StepEvidenceSplit } from "@/features/relay-runs/runWorkbenchViews";

// Feature: run-workbench-refinement, Task 7.5 — RunStepEvidence example tests
// Validates: Requirements 5.2, 5.3, 5.6, 5.7, 5.8

function makeArtifact(overrides: Partial<RelayArtifact> = {}): RelayArtifact {
  return {
    id: overrides.id ?? "artifact-1",
    label: overrides.label ?? "Artifact One",
    path: overrides.path ?? "/artifacts/artifact-1",
    kind: overrides.kind ?? "result",
    status: overrides.status ?? "ready",
    filename: overrides.filename ?? "artifact-1.json",
    ...overrides,
  };
}

function makeQueryClient(): QueryClient {
  return new QueryClient({
    defaultOptions: {
      queries: { retry: false, refetchOnWindowFocus: false, gcTime: 0 },
    },
  });
}

function renderWithQueryClient(ui: React.ReactElement) {
  const queryClient = makeQueryClient();
  return render(
    <QueryClientProvider client={queryClient}>{ui}</QueryClientProvider>,
  );
}

const emptyEvidence: StepEvidenceSplit = {
  stepEvidence: [],
  otherArtifacts: [],
};

describe("RunStepEvidence", () => {
  it("renders ValidationPanel and LogPreviewPanel content in the default view without expansion", () => {
    const validationSummary: RelayRun["validationSummary"] = {
      errors: 1,
      warnings: 2,
      passed: 3,
    };
    const logPreview: RelayLogPreview = {
      lines: ["line one", "line two"],
      truncated: false,
    };

    renderWithQueryClient(
      <RunStepEvidence
        runId="run-1"
        currentStep="execute"
        evidence={emptyEvidence}
        validationSummary={validationSummary}
        logPreview={logPreview}
      />,
    );

    // ValidationPanel content
    expect(screen.getByText("Validation Results")).toBeInTheDocument();
    expect(screen.getByText("1")).toBeInTheDocument(); // errors count
    expect(screen.getByText("2")).toBeInTheDocument(); // warnings count
    expect(screen.getByText("3")).toBeInTheDocument(); // passed count

    // LogPreviewPanel content
    expect(screen.getByText("Log Preview")).toBeInTheDocument();
    expect(screen.getByText("line one")).toBeInTheDocument();
    expect(screen.getByText("line two")).toBeInTheDocument();
  });

  it("hides other-artifact cards by default and reveals them after activating the disclosure", async () => {
    const user = userEvent.setup();
    const otherArtifacts = [
      makeArtifact({ id: "other-1", label: "Other Artifact One" }),
      makeArtifact({ id: "other-2", label: "Other Artifact Two" }),
    ];
    const evidence: StepEvidenceSplit = {
      stepEvidence: [],
      otherArtifacts,
    };

    renderWithQueryClient(
      <RunStepEvidence runId="run-1" currentStep="execute" evidence={evidence} />,
    );

    // Disclosure affordance is present.
    const trigger = screen.getByRole("button", {
      name: /show other step artifacts \(2\)/i,
    });
    expect(trigger).toHaveAttribute("aria-expanded", "false");

    // Other-artifact cards are not visible by default.
    expect(screen.queryByText("Other Artifact One")).not.toBeInTheDocument();
    expect(screen.queryByText("Other Artifact Two")).not.toBeInTheDocument();

    await user.click(trigger);

    expect(trigger).toHaveAttribute("aria-expanded", "true");
    expect(screen.getByText("Other Artifact One")).toBeInTheDocument();
    expect(screen.getByText("Other Artifact Two")).toBeInTheDocument();
  });

  it("shows the empty-state indication when there is no validation, logs, or step evidence", () => {
    renderWithQueryClient(
      <RunStepEvidence runId="run-1" currentStep="execute" evidence={emptyEvidence} />,
    );

    expect(screen.getByText("No evidence yet")).toBeInTheDocument();
    expect(
      screen.getByText(
        "Relay has not captured validation, logs, or artifacts for this step yet.",
      ),
    ).toBeInTheDocument();
  });

  it("hides the disclosure affordance entirely when there are no other artifacts", () => {
    const evidence: StepEvidenceSplit = {
      stepEvidence: [makeArtifact({ id: "step-1", label: "Step Artifact" })],
      otherArtifacts: [],
    };
    const validationSummary: RelayRun["validationSummary"] = {
      errors: 0,
      warnings: 0,
      passed: 1,
    };

    renderWithQueryClient(
      <RunStepEvidence
        runId="run-1"
        currentStep="execute"
        evidence={evidence}
        validationSummary={validationSummary}
      />,
    );

    // Step evidence renders inline in the default view.
    expect(screen.getByText("Step Artifact")).toBeInTheDocument();

    // No disclosure affordance at all — not just collapsed.
    expect(
      screen.queryByRole("button", { name: /other step artifacts/i }),
    ).not.toBeInTheDocument();
  });
});
