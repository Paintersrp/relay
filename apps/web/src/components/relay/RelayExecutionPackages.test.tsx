// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { RelayExecutionPackageCreate, RelayExecutionPackageDetail } from "./RelayExecutionPackages";

const mocks = vi.hoisted(() => ({ prepare: vi.fn(), approve: vi.fn(), digest: vi.fn() }));

vi.mock("@/features/relay-packages", async () => {
  const actual = await vi.importActual<typeof import("@/features/relay-packages")>("@/features/relay-packages");
  return { ...actual, prepareExecutionPackage: mocks.prepare, approveExecutionPackage: mocks.approve };
});

vi.mock("@tanstack/react-router", () => ({
  Link: ({ children, to, params }: any) => (
    <a href={to} data-package-id={params?.packageId ?? ""} data-run-id={params?.runId ?? ""}>{children}</a>
  ),
}));

function wrapper({ children }: { children: React.ReactNode }) {
  return <QueryClientProvider client={new QueryClient({ defaultOptions: { mutations: { retry: false }, queries: { retry: false } } })}>{children}</QueryClientProvider>;
}

function jsonResponse(body: unknown): Response {
  return new Response(JSON.stringify(body), { status: 200, headers: { "Content-Type": "application/json" } });
}

function packageBody() {
  const body: {
    package: {
      packageId: string;
      selectionRowId: number;
      workspaceRowId: number;
      repoTarget: string;
      branch: string;
      baseCommit: string;
      sourceClosureRowId: number;
      authorityRevisionRowId: number;
      packageSha256: string;
      authoritySha256: string;
      sourceSha256: string;
      createdAt: string;
      members: never[];
      approvalBindings: never[];
      ticketDocument: { displayName: string; relativePath: string; sha256: string; sizeBytes: number };
      run: null | { runId: string; featureSlug: string; repoTarget: string; branch: string; baseCommit: string; status: string };
    };
  } = {
    package: {
      packageId: "package-1",
      selectionRowId: 1,
      workspaceRowId: 1,
      repoTarget: "relay",
      branch: "main",
      baseCommit: "base-commit",
      sourceClosureRowId: 1,
      authorityRevisionRowId: 1,
      packageSha256: "package-sha",
      authoritySha256: "authority-sha",
      sourceSha256: "source-sha",
      createdAt: "",
      members: [],
      approvalBindings: [],
      ticketDocument: { displayName: "feature.ticket-T1.r1.delivery-ticket.md", relativePath: "tickets/P5-T1/feature.ticket-T1.r1.delivery-ticket.md", sha256: "ticket-sha", sizeBytes: 42 },
      run: null,
    },
  };
  return body;
}

describe("RelayExecutionPackageCreate", () => {
  beforeEach(() => {
    mocks.prepare.mockReset().mockResolvedValue({ packageId: "package-1" });
    mocks.digest.mockReset().mockResolvedValue(new Uint8Array(32).fill(0xab));
    vi.stubGlobal("crypto", { subtle: { digest: mocks.digest } });
  });

  it("prepares a Brief-free package from the selection with optional Deterministic Operations", async () => {
    const user = userEvent.setup();
    render(<RelayExecutionPackageCreate />, { wrapper });

    expect(screen.getByText("Prepare execution package")).toBeInTheDocument();
    expect(screen.queryByLabelText("Ticket Design Brief")).not.toBeInTheDocument();
    expect(screen.queryByText(/Ticket Design Brief/i)).not.toBeInTheDocument();

    await user.type(screen.getByLabelText("Selection ID"), "selection-1");
    await user.upload(screen.getByLabelText("Deterministic Operations (optional)"), new File(['{"operations":[]}'], "operations.json", { type: "application/json" }));
    await user.click(screen.getByRole("button", { name: "Prepare immutable package" }));

    await waitFor(() => expect(mocks.prepare).toHaveBeenCalledTimes(1));
    expect(mocks.prepare).toHaveBeenCalledWith(expect.objectContaining({
      selectionId: "selection-1",
      deterministicOperations: expect.objectContaining({ displayName: "operations.json", expectedSha256: expect.any(String), bytesBase64: expect.any(String) }),
    }));
    expect(await screen.findByText(/Package prepared\./)).toBeInTheDocument();
  });

  it("prepares a package from just the selection when no Deterministic Operations are supplied", async () => {
    const user = userEvent.setup();
    render(<RelayExecutionPackageCreate />, { wrapper });

    await user.type(screen.getByLabelText("Selection ID"), "selection-1");
    await user.click(screen.getByRole("button", { name: "Prepare immutable package" }));

    await waitFor(() => expect(mocks.prepare).toHaveBeenCalledTimes(1));
    expect(mocks.prepare).toHaveBeenCalledWith({ selectionId: "selection-1", deterministicOperations: undefined });
  });
});

describe("RelayExecutionPackageDetail", () => {
  it("shows the Delivery Ticket document authority with no Brief content and approves the exact package", async () => {
    const fetch = vi.fn().mockResolvedValue(jsonResponse(packageBody()));
    vi.stubGlobal("fetch", fetch);
    mocks.approve.mockReset().mockResolvedValue({ runId: "run-1", featureSlug: "payments", repoTarget: "relay", branch: "main", baseCommit: "base-commit", status: "setup_ready" });
    const user = userEvent.setup();
    render(<RelayExecutionPackageDetail packageId="package-1" />, { wrapper });

    expect(await screen.findByText(/feature\.ticket-T1\.r1\.delivery-ticket\.md/)).toBeInTheDocument();
    expect(screen.getByText("Selected Delivery Ticket document")).toBeInTheDocument();
    expect(screen.getByText(/ticket-sha/)).toBeInTheDocument();
    expect(screen.queryByText(/Ticket Design Brief/i)).not.toBeInTheDocument();
    expect(screen.queryByText(/brief-sha/i)).not.toBeInTheDocument();

    await user.type(screen.getByLabelText("Operator confirmation evidence"), "Reviewed package basis.");
    await user.click(screen.getByRole("button", { name: "Approve exact package and create linked Run" }));

    await waitFor(() => expect(mocks.approve).toHaveBeenCalledTimes(1));
    expect(mocks.approve).toHaveBeenCalledWith("package-1", { expectedPackageSha256: "package-sha", operatorConfirmationEvidence: "Reviewed package basis." });
  });

  it("shows the linked setup-ready Run state when the package is already approved", async () => {
    const body = packageBody();
    body.package.run = { runId: "run-1", featureSlug: "payments", repoTarget: "relay", branch: "main", baseCommit: "base-commit", status: "setup_ready" };
    const fetch = vi.fn().mockResolvedValue(jsonResponse(body));
    vi.stubGlobal("fetch", fetch);
    render(<RelayExecutionPackageDetail packageId="package-1" />, { wrapper });

    expect(await screen.findByText("run-1")).toBeInTheDocument();
    expect(screen.getByText(/One linked setup-ready Run/)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Approve exact package and create linked Run" })).not.toBeInTheDocument();
  });
});
