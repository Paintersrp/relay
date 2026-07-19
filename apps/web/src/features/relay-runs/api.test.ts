import { afterEach, describe, expect, it, vi } from "vitest";

import {
  getWorkflowAttempt,
	getWorkflowAuditPacket,
  getWorkflowRun,
	recordWorkflowAuditDecision,
  startWorkflowAttempt,
} from "./api";

const reducedArtifact = {
  artifactId: "artifact-attempt",
  kind: "executor_stdout",
  mediaType: "text/plain",
  sha256: "a".repeat(64),
  sizeBytes: 12,
  createdAt: "2026-07-08T00:00:00Z",
};

const fullArtifact = {
  ...reducedArtifact,
  ownerType: "attempt",
  contentUrl: "/api/artifacts/artifact-attempt/content",
};

const detailedAttempt = {
  attemptId: "attempt-1",
  runId: "run-1",
  attemptNumber: 1,
  adapter: "codex",
  model: "gpt-5.5",
  status: "running",
  result: {},
  createdAt: "2026-07-08T00:00:00Z",
  artifacts: [reducedArtifact],
  liveStdoutTruncated: false,
  liveStderrTruncated: false,
  liveStdoutBytes: 0,
  liveStderrBytes: 0,
};

function response(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("canonical Run attempt transport", () => {
  it("accepts reduced detailed-attempt artifacts and omitted empty live output", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(response(detailedAttempt)));

    const attempt = await getWorkflowAttempt("run-1", "attempt-1");

    expect(attempt.artifacts).toEqual([reducedArtifact]);
    expect(attempt.liveStdout).toBe("");
    expect(attempt.liveStderr).toBe("");
    expect(attempt.artifacts[0]).not.toHaveProperty("ownerType");
    expect(attempt.artifacts[0]).not.toHaveProperty("contentUrl");
  });

  it("accepts the same reduced detailed projection returned after attempt start", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        response({
          success: true,
          preflight: { ok: true },
          attempt: detailedAttempt,
        }, 202),
      ),
    );

    const attempt = await startWorkflowAttempt(
      "run-1",
      "codex",
      "gpt-5.5",
    );

    expect(attempt.liveStdout).toBe("");
    expect(attempt.liveStderr).toBe("");
    expect(attempt.artifacts[0]).toEqual(reducedArtifact);
  });

  it("still requires full artifact links in Run summary attempt projections", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        response({
          run: {
            runId: "run-1",
            featureSlug: "feature",
            repoTarget: "relay",
            status: "executing",
            stage: "execute",
            branch: "feat/simplification",
            baseCommit: "b".repeat(40),
            canonicalSha256: "c".repeat(64),
            createdAt: "2026-07-08T00:00:00Z",
            updatedAt: "2026-07-08T00:00:01Z",
            latestAttempt: {
              attemptId: "attempt-1",
              attemptNumber: 1,
              adapter: "codex",
              model: "gpt-5.5",
              status: "running",
              createdAt: "2026-07-08T00:00:00Z",
              artifacts: [fullArtifact],
            },
          },
          attempts: [
            {
              attemptId: "attempt-1",
              attemptNumber: 1,
              adapter: "codex",
              model: "gpt-5.5",
              status: "running",
              createdAt: "2026-07-08T00:00:00Z",
              artifacts: [fullArtifact],
            },
          ],
          artifacts: [fullArtifact],
        }),
      ),
    );

    const detail = await getWorkflowRun("run-1");

    expect(detail.attempts[0].artifacts[0]).toEqual(fullArtifact);
    expect(detail.run.latestAttempt?.artifacts[0].contentUrl).toBe(
      "/api/artifacts/artifact-attempt/content",
    );
  });

  it("rejects malformed present live output instead of coercing it", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        response({
          ...detailedAttempt,
          liveStdout: 42,
        }),
      ),
    );

    await expect(getWorkflowAttempt("run-1", "attempt-1")).rejects.toThrow(
      /liveStdout/,
    );
  });

  it("accepts exact cleanup-pending lifecycle metadata", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        response({
          ...detailedAttempt,
          result: {
            cleanup_pending: true,
            pending_terminal_status: "cancelled",
            termination_verified: false,
          },
        }),
      ),
    );

    const attempt = await getWorkflowAttempt("run-1", "attempt-1");

    expect(attempt.status).toBe("running");
    expect(attempt.result).toMatchObject({
      cleanup_pending: true,
      pending_terminal_status: "cancelled",
      termination_verified: false,
    });
  });

  it("rejects unsupported attempt lifecycle statuses", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        response({
          ...detailedAttempt,
          status: "cleanup_pending",
        }),
      ),
    );

    await expect(getWorkflowAttempt("run-1", "attempt-1")).rejects.toThrow(
      /execution-attempt status/,
    );
  });

  it("rejects malformed cleanup-pending lifecycle metadata", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        response({
          ...detailedAttempt,
          result: {
            cleanup_pending: "yes",
          },
        }),
      ),
    );

    await expect(getWorkflowAttempt("run-1", "attempt-1")).rejects.toThrow(
      /cleanup_pending/,
    );
  });

  it("rejects nonterminal pending_terminal_status values", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        response({
          ...detailedAttempt,
          result: {
            cleanup_pending: true,
            pending_terminal_status: "running",
          },
        }),
      ),
    );

    await expect(getWorkflowAttempt("run-1", "attempt-1")).rejects.toThrow(
      /pending_terminal_status/,
    );
  });
});

describe("ticket-aware audit transport", () => {
  it("reads exact ticket obligations and submits only a confirmed packet decision", async () => {
    const fetch = vi.fn()
      .mockResolvedValueOnce(response({
        runId: "run-1", runStatus: "audit_ready",
        packet: { auditPacketId: "packet-1", implementationActorKind: "executor", auditedCommit: "b".repeat(40), packetSha256: "c".repeat(64), status: "current", createdAt: "2026-07-19T00:00:00Z" },
        document: { schema_version: "2.0" },
        ticketPackage: { package: { packageId: "package-1", packageSha256: "d".repeat(64), workspaceId: "workspace-1", featureSlug: "payments", selectionId: "selection-1", selectionState: "consumed", authorityRevisionId: "authority-1", authoritySha256: "e".repeat(64), sourceClosureId: "closure-1", sourceCommit: "f".repeat(40) }, tickets: [{ sequence: 1, ticketId: "T1", revisionRowId: 2, revisionNumber: 1, memberSha256: "a".repeat(64), approvalId: "approval-1", approvalBasisSha256: "b".repeat(64), authorityRevisionRowId: 3, sourceClosureRowId: 4, designBrief: { artifactReference: "brief-1", sha256: "c".repeat(64) } }], mutationLeases: [], bundleIntegration: { runId: "run-1", executionPackageId: "package-1", selectionId: "selection-1", selectionState: "consumed", approvedRunStatus: "package_linked" } },
      }))
      .mockResolvedValueOnce(response({
        runId: "run-1", runStatus: "needs_revision",
        packet: { auditPacketId: "packet-1", implementationActorKind: "executor", auditedCommit: "b".repeat(40), packetSha256: "c".repeat(64), status: "current", createdAt: "2026-07-19T00:00:00Z" },
        decision: { auditDecisionId: "decision-1", auditedCommit: "b".repeat(40), packetSha256: "c".repeat(64), decision: "needs_revision", rationale: "missing proof", createdAt: "2026-07-19T00:00:00Z" },
        effects: { ticketRevisionDecisions: [{ auditTicketRevisionDecisionRowId: 1, auditPacketTicketObligationRowId: 2 }], ticketSatisfactions: [], remediationSeeds: [{ remediationSeedId: "seed-1", auditPacketRowId: 3, executionPackageRowId: 4, auditedCommit: "b".repeat(40) }] },
      }));
    vi.stubGlobal("fetch", fetch);

    const packet = await getWorkflowAuditPacket("run-1");
    const result = await recordWorkflowAuditDecision("run-1", { auditPacketId: packet.packet.auditPacketId, packetSha256: packet.packet.packetSha256, auditedCommit: packet.packet.auditedCommit, decision: "needs_revision", rationale: "missing proof", materialFindings: [{ source: "both", summary: "missing proof", evidence: "packet", requiredRemediation: "supply proof" }], observations: [], operatorConfirmed: true });

    expect(packet.ticketPackage?.tickets[0]).toMatchObject({ ticketId: "T1", approvalId: "approval-1" });
    expect(result.effects.remediationSeeds[0]?.remediationSeedId).toBe("seed-1");
    expect(JSON.parse(fetch.mock.calls[1]?.[1]?.body as string)).toMatchObject({ operatorConfirmed: true, materialFindings: [{ requiredRemediation: "supply proof" }] });
  });
});
