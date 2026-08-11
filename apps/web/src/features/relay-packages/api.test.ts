import { afterEach, describe, expect, it, vi } from "vitest";
import { approveExecutionPackage, prepareExecutionPackage, reconcileMutationLease } from "./api";

function response(body: unknown, status = 200): Response { return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } }); }
function packageResponse() {
  return {
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
}
afterEach(() => vi.unstubAllGlobals());

describe("execution package transport", () => {
  it("prepares execution packages directly with a Brief-free request and parses the Delivery Ticket document", async () => {
    const fetch = vi.fn().mockResolvedValue(response(packageResponse()));
    vi.stubGlobal("fetch", fetch);
    const request = {
      selectionId: "selection-1",
      deterministicOperations: { displayName: "operations.json", expectedSha256: "operations-sha", bytesBase64: "b3Bz" },
    };

    const result = await prepareExecutionPackage(request);

    expect(JSON.parse(fetch.mock.calls[0]?.[1]?.body as string)).toEqual(request);
    expect(result.ticketDocument).toMatchObject({ displayName: "feature.ticket-T1.r1.delivery-ticket.md", relativePath: "tickets/P5-T1/feature.ticket-T1.r1.delivery-ticket.md", sha256: "ticket-sha", sizeBytes: 42 });
    expect(result).not.toHaveProperty("ticketDesignBrief");
    expect(result).not.toHaveProperty("designBriefSha256");
  });

  it("approves execution packages directly", async () => {
    const fetch = vi.fn().mockResolvedValue(response({ run: { runId: "run-1", featureSlug: "payments", repoTarget: "relay", branch: "main", baseCommit: "base-commit", status: "setup_ready" } }));
    vi.stubGlobal("fetch", fetch);
    const request = { expectedPackageSha256: "package-sha", operatorConfirmationEvidence: "Reviewed package basis." };

    await approveExecutionPackage("package-1", request);

    expect(JSON.parse(fetch.mock.calls[0]?.[1]?.body as string)).toEqual(request);
  });

  it("reconciles a mutation lease using its exact required id", async () => {
    const fetch = vi.fn().mockResolvedValue(response({ released: false, lease: null }));
    vi.stubGlobal("fetch", fetch);

    await expect(reconcileMutationLease("run-1", "lease-1")).resolves.toMatchObject({ released: false, lease: null });

    expect(JSON.parse(fetch.mock.calls[0]?.[1]?.body as string)).toEqual({ leaseId: "lease-1" });
  });
});
