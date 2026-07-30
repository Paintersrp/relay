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
      designBriefSha256: "brief-sha",
      createdAt: "",
      members: [],
      approvalBindings: [],
      ticketDesignBrief: { displayName: "brief.md", relativePath: "brief.md", sha256: "brief-sha", sizeBytes: 1 },
      run: null,
    },
  };
}
afterEach(() => vi.unstubAllGlobals());

describe("execution package transport", () => {
  it("prepares execution packages directly", async () => {
    const fetch = vi.fn().mockResolvedValue(response(packageResponse()));
    vi.stubGlobal("fetch", fetch);
    const request = {
      selectionId: "selection-1",
      ticketDesignBrief: { displayName: "brief.md", expectedSha256: "brief-sha", bytesBase64: "Yg==" },
      deterministicOperations: { displayName: "operations.json", expectedSha256: "operations-sha", bytesBase64: "b3Bz" },
    };

    await prepareExecutionPackage(request);

    expect(JSON.parse(fetch.mock.calls[0]?.[1]?.body as string)).toEqual(request);
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
