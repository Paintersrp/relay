import { afterEach, describe, expect, it, vi } from "vitest";

import {
  getWorkflowAttempt,
  getWorkflowRun,
  startWorkflowAttempt,
} from "./canonical-api";

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
