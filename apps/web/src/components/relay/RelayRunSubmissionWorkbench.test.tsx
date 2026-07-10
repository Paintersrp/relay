// @vitest-environment jsdom

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { RelayRunSubmissionWorkbench } from "./RelayRunSubmissionWorkbench";

const mocks = vi.hoisted(() => ({
  getPlan: vi.fn(),
  createRun: vi.fn(),
  validateSpec: vi.fn(),
  navigate: vi.fn(),
}));

vi.mock("@tanstack/react-router", () => ({
  useNavigate: () => mocks.navigate,
}));

vi.mock("@/features/relay-plans", () => ({
  workflowPlanDetailQueryOptions: (planId: string) => ({
    queryKey: ["workflow-plans", "detail", planId],
    queryFn: mocks.getPlan,
    retry: false,
  }),
}));

vi.mock("@/features/relay-runs", () => ({
  createWorkflowRun: mocks.createRun,
  validateWorkflowExecutionSpec: mocks.validateSpec,
  workflowRunKeys: { all: ["workflow-runs"] },
  RelayApiError: class RelayApiError extends Error {
    errorShape?: { message?: string };
  },
}));

const managedDetail = {
  plan: {
    planId: "plan-1",
    featureSlug: "feature",
    status: "active",
    project: {
      projectId: "project-archived",
      name: "Archived Project",
      status: "archived",
    },
  },
  passes: [
    {
      passId: "pass-1",
      number: 1,
      name: "Pass One",
      repoTarget: "relay",
      status: "planned",
      dependsOn: [],
      createdAt: "2026-07-08T00:00:00Z",
      updatedAt: "2026-07-08T00:00:00Z",
      runs: [],
    },
  ],
  repositories: [],
  artifacts: [],
};

function renderWorkbench(
  props: {
    planId?: string;
    passId?: string;
    passNumber?: number;
    remediatesRunId?: string;
  } = {
    planId: "plan-1",
    passId: "pass-1",
    passNumber: 1,
  },
) {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <RelayRunSubmissionWorkbench {...props} />
    </QueryClientProvider>,
  );
}

describe("RelayRunSubmissionWorkbench", () => {
  beforeEach(() => {
    mocks.getPlan.mockReset();
    mocks.createRun.mockReset();
    mocks.validateSpec.mockReset();
    mocks.navigate.mockReset();
  });

  it("surfaces Managed context failure and retries without fabricating association state", async () => {
    const user = userEvent.setup();
    mocks.getPlan
      .mockRejectedValueOnce(new Error("Plan lookup failed"))
      .mockResolvedValueOnce(managedDetail);

    renderWorkbench();

    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent("Managed Run context failed to load");
    expect(alert).toHaveTextContent(
      "remains unresolved until this recoverable context request succeeds",
    );
    expect(
      screen.getByRole("button", { name: "Create Managed Run" }),
    ).toBeDisabled();

    await user.click(
      screen.getByRole("button", { name: "Retry Managed context" }),
    );

    await waitFor(() => expect(mocks.getPlan).toHaveBeenCalledTimes(2));
    expect(await screen.findByText("Archived Project")).toBeInTheDocument();
  });

  it("retains Plan, pass, repository target, and archived Project context without adding Project metadata to Run creation", async () => {
    const user = userEvent.setup();
    const canonicalContent = '{"feature_slug":"feature"}';
    const sha256 = "c".repeat(64);
    mocks.getPlan.mockResolvedValue(managedDetail);
    mocks.validateSpec.mockResolvedValue({
      ok: true,
      status: "valid",
      kind: "execution_spec",
      sha256,
      diagnostics: [],
      notices: [],
    });
    mocks.createRun.mockResolvedValue({
      run: {
        runId: "run-1",
        featureSlug: "feature",
        repoTarget: "relay",
        status: "created",
        branch: "feat/simplification",
        baseCommit: "a".repeat(40),
        canonicalSha256: sha256,
        createdAt: "2026-07-08T00:00:00Z",
        updatedAt: "2026-07-08T00:00:00Z",
        reviewUrl: "http://localhost:3000/runs/run-1/specification",
      },
      artifacts: [],
    });

    renderWorkbench();

    const context = await screen.findByTestId("managed-run-context");
    expect(within(context).getByText("Archived Project")).toBeInTheDocument();
    expect(within(context).getByText("feature")).toBeInTheDocument();
    expect(within(context).getByText("1. Pass One")).toBeInTheDocument();
    expect(within(context).getByText("relay")).toBeInTheDocument();
    expect(within(context).getByText("Project archived")).toBeInTheDocument();

    fireEvent.change(screen.getByLabelText("Canonical filename"), {
      target: { value: "feature.pass-1.execution-spec.json" },
    });
    fireEvent.change(screen.getByLabelText("Canonical Execution Spec JSON"), {
      target: { value: canonicalContent },
    });
    await user.click(
      screen.getByRole("button", { name: "Validate Execution Spec" }),
    );
    const createButton = screen.getByRole("button", {
      name: "Create Managed Run",
    });
    await waitFor(() => expect(createButton).toBeEnabled());
    await user.click(createButton);

    await waitFor(() =>
      expect(mocks.createRun).toHaveBeenCalledWith({
        fileName: "feature.pass-1.execution-spec.json",
        canonicalContent,
        expectedSha256: sha256,
        planId: "plan-1",
        passNumber: 1,
      }),
    );
    const request = mocks.createRun.mock.calls[0]?.[0] as Record<string, unknown>;
    expect(request).not.toHaveProperty("projectId");
    expect(request).not.toHaveProperty("repoTarget");
    expect(request).not.toHaveProperty("passId");
  });

  it("creates a standalone remediation Run without adding Project, repository, or Plan metadata", async () => {
    const user = userEvent.setup();
    const canonicalContent = '{"feature_slug":"feature"}';
    const sha256 = "d".repeat(64);
    mocks.validateSpec.mockResolvedValue({
      ok: true,
      status: "valid",
      kind: "execution_spec",
      sha256,
      diagnostics: [],
      notices: [],
    });
    mocks.createRun.mockResolvedValue({
      run: {
        runId: "run-remediation",
        featureSlug: "feature",
        repoTarget: "relay",
        status: "created",
        branch: "feat/simplification",
        baseCommit: "a".repeat(40),
        canonicalSha256: sha256,
        createdAt: "2026-07-08T00:00:00Z",
        updatedAt: "2026-07-08T00:00:00Z",
        reviewUrl: "http://localhost:3000/runs/run-remediation/specification",
      },
      artifacts: [],
    });

    renderWorkbench({ remediatesRunId: "run-parent" });

    expect(screen.getByTestId("remediation-run-context")).toHaveTextContent(
      "run-parent",
    );
    fireEvent.change(screen.getByLabelText("Canonical filename"), {
      target: { value: "feature.execution-spec.json" },
    });
    fireEvent.change(screen.getByLabelText("Canonical Execution Spec JSON"), {
      target: { value: canonicalContent },
    });
    await user.click(
      screen.getByRole("button", { name: "Validate Execution Spec" }),
    );
    const createButton = screen.getByRole("button", {
      name: "Create Standalone Run",
    });
    await waitFor(() => expect(createButton).toBeEnabled());
    await user.click(createButton);

    await waitFor(() =>
      expect(mocks.createRun).toHaveBeenCalledWith({
        fileName: "feature.execution-spec.json",
        canonicalContent,
        expectedSha256: sha256,
        remediatesRunId: "run-parent",
      }),
    );
    const request = mocks.createRun.mock.calls[0]?.[0] as Record<string, unknown>;
    expect(request).not.toHaveProperty("projectId");
    expect(request).not.toHaveProperty("repoTarget");
    expect(request).not.toHaveProperty("planId");
    expect(request).not.toHaveProperty("passId");
    expect(request).not.toHaveProperty("passNumber");
  });

  it("keeps the upload control and status announcement accessible and preserves responsive stacking", async () => {
    mocks.getPlan.mockResolvedValue(managedDetail);
    renderWorkbench();

    expect(await screen.findByLabelText("Load Execution Spec file")).toHaveAttribute(
      "type",
      "file",
    );
    expect(screen.getByTestId("run-submission-layout")).toHaveClass(
      "grid-cols-1",
      "lg:grid-cols-[minmax(0,1fr)_24rem]",
    );
    expect(screen.getByRole("status")).toHaveAttribute("aria-live", "polite");
  });
});
