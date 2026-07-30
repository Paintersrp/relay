import { afterEach, describe, expect, it, vi } from "vitest";
import { getTicketFrontier, publishTicketRevision } from "./api";
import type { PublishTicketRevisionRequest } from "./types";

function response(body: unknown, status = 200): Response { return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } }); }
afterEach(() => vi.unstubAllGlobals());

describe("ticket transport", () => {
  it("reads the workspace frontier directly", async () => {
    const fetch = vi.fn().mockResolvedValue(response({ workspaceId: "workspace-1", entries: [] }));
    vi.stubGlobal("fetch", fetch);

    await expect(getTicketFrontier("workspace-1")).resolves.toEqual({ workspaceId: "workspace-1", entries: [] });

    expect(fetch.mock.calls[0]?.[0]).toBe("http://localhost:8080/api/feature-workspaces/workspace-1/tickets/frontier");
    expect(fetch.mock.calls[0]?.[1]).toMatchObject({ method: "GET" });
  });

  it("publishes direct ticket revisions while retaining remediation authoring fields", async () => {
    const fetch = vi.fn().mockResolvedValue(response({}));
    vi.stubGlobal("fetch", fetch);
    const request = {
      externalPriority: 0,
      expectedRevisionNumber: 0,
      revision: {
        repoTarget: "relay",
        branch: "main",
        baseCommit: "base-commit",
        sourceClosureRowId: 1,
        sourcePath: "tickets/payments.md",
        goal: "Publish the payments ticket.",
        context: "Current workspace context.",
        transitionApplicability: "not_required",
        canonicalJson: { ticket: "payments" },
        renderedMarkdown: "# Payments",
        members: [],
        dependencies: [],
      },
      remediationSeedId: "seed-1",
      authoringPacketId: "authoring-1",
      expectedAuthoringPacketSha256: "a".repeat(64),
    } satisfies PublishTicketRevisionRequest;

    await publishTicketRevision("workspace-1", "ticket-1", request);

    expect(JSON.parse(fetch.mock.calls[0]?.[1]?.body as string)).toEqual(request);
  });
});
