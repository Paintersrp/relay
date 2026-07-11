// @vitest-environment jsdom

import { afterAll, beforeEach, describe, expect, it, vi } from "vitest";

import { RelayApiError } from "@/features/relay-runs";
import {
  confirmWorkflowRepository,
  inspectWorkflowRepository,
  WorkflowRepositoryConfirmationError,
} from "./api";

function response(status: number, value?: unknown) {
  return {
    ok: status >= 200 && status < 300,
    status,
    text: async () => value === undefined ? "" : JSON.stringify(value),
  };
}

function remoteValue(overrides: Record<string, unknown> = {}) {
  return {
    name: "origin",
    url: "git@github.com:Paintersrp/relay.git",
    suggestedRepoTarget: "relay",
    ...overrides,
  };
}

function targetValue(overrides: Record<string, unknown> = {}) {
  return {
    repoTarget: "relay",
    localPath: "D:/Code/relay",
    createdAt: "2026-07-07T00:00:00Z",
    updatedAt: "2026-07-07T01:00:00Z",
    ...overrides,
  };
}

function readyInspection(overrides: Record<string, unknown> = {}) {
  return {
    state: "ready",
    selectedPath: "D:/Code/relay/internal",
    resolvedLocalPath: "D:/Code/relay",
    remotes: [remoteValue()],
    selectedRemote: remoteValue(),
    suggestedRepoTarget: "relay",
    repoTarget: "relay",
    repoTargetSource: "remote_basename",
    registrationDisposition: "create",
    confirmationHash: "a".repeat(64),
    notices: [],
    ...overrides,
  };
}

async function expectMalformed(
  run: () => Promise<unknown>,
  detail: string,
) {
  let caught: unknown;
  try {
    await run();
  } catch (error) {
    caught = error;
  }
  expect(caught).toBeInstanceOf(RelayApiError);
  expect(caught).toMatchObject({ status: 502 });
  expect((caught as Error).message).toContain(detail);
}

describe("repository registration api", () => {
  const originalFetch = globalThis.fetch;

  beforeEach(() => {
    vi.restoreAllMocks();
    globalThis.fetch = originalFetch;
  });

  afterAll(() => {
    globalThis.fetch = originalFetch;
  });

  it("normalizes every valid inspection state with state-dependent fields", async () => {
    const states = [
      {
        value: readyInspection(),
        assert: (inspection: Awaited<ReturnType<typeof inspectWorkflowRepository>>) => {
          expect(inspection.state).toBe("ready");
          if (inspection.state !== "ready") throw new Error("expected ready");
          expect(inspection.repoTarget).toBe("relay");
          expect(inspection.registrationDisposition).toBe("create");
          expect(inspection.confirmationHash).toHaveLength(64);
        },
      },
      {
        value: {
          state: "needs_remote_selection",
          selectedPath: "D:/Code/relay",
          resolvedLocalPath: "D:/Code/relay",
          remotes: [
            remoteValue({ name: "fork", url: "git@example.com:owner/fork.git" }),
            remoteValue({ name: "upstream", url: "git@example.com:owner/upstream.git" }),
          ],
          notices: [],
        },
        assert: (inspection: Awaited<ReturnType<typeof inspectWorkflowRepository>>) => {
          expect(inspection.state).toBe("needs_remote_selection");
          expect(inspection.remotes).toHaveLength(2);
          expect("confirmationHash" in inspection).toBe(false);
        },
      },
      {
        value: {
          state: "needs_target_override",
          selectedPath: "D:/Code/relay",
          resolvedLocalPath: "D:/Code/relay",
          remotes: [remoteValue({ suggestedRepoTarget: undefined })],
          selectedRemote: remoteValue({ suggestedRepoTarget: undefined }),
          targetOverrideReason: "unsupported_remote",
          notices: [
            `Remote "origin" uses an unsupported URL.`,
          ],
        },
        assert: (inspection: Awaited<ReturnType<typeof inspectWorkflowRepository>>) => {
          expect(inspection.state).toBe("needs_target_override");
          if (inspection.state !== "needs_target_override") {
            throw new Error("expected override state");
          }
          expect(inspection.selectedRemote?.name).toBe("origin");
          expect(inspection.targetOverrideReason).toBe("unsupported_remote");
          expect("repoTarget" in inspection).toBe(false);
        },
      },
      {
        value: {
          state: "conflict",
          selectedPath: "D:/Code/relay-copy",
          resolvedLocalPath: "D:/Code/relay-copy",
          remotes: [remoteValue()],
          selectedRemote: remoteValue(),
          suggestedRepoTarget: "relay",
          repoTarget: "relay",
          repoTargetSource: "remote_basename",
          existingRepository: targetValue(),
          conflictKind: "target",
          notices: [],
        },
        assert: (inspection: Awaited<ReturnType<typeof inspectWorkflowRepository>>) => {
          expect(inspection.state).toBe("conflict");
          if (inspection.state !== "conflict") throw new Error("expected conflict");
          expect(inspection.conflictKind).toBe("target");
          expect(inspection.existingRepository.localPath).toBe("D:/Code/relay");
          expect("registrationDisposition" in inspection).toBe(false);
          expect("confirmationHash" in inspection).toBe(false);
        },
      },
    ];

    for (const testCase of states) {
      globalThis.fetch = vi.fn().mockResolvedValue(response(200, testCase.value));
      const result = await inspectWorkflowRepository({ localPath: "D:/Code/relay" });
      testCase.assert(result);
    }
  });

  it("sends optional inspection inputs as explicit empty API fields", async () => {
    const fetchSpy = vi.fn().mockResolvedValue(response(200, readyInspection()));
    globalThis.fetch = fetchSpy;

    await inspectWorkflowRepository({
      localPath: "D:/Code/relay",
      remoteName: "origin",
      repoTargetOverride: "relay-local",
    });

    expect(fetchSpy).toHaveBeenCalledWith(
      "http://localhost:8080/api/repositories/inspect",
      expect.objectContaining({
        method: "POST",
        body: JSON.stringify({
          localPath: "D:/Code/relay",
          remoteName: "origin",
          repoTargetOverride: "relay-local",
        }),
      }),
    );
  });

  it("rejects empty sentinel fields that must be absent outside ready state", async () => {
    const invalidValues = [
      {
        ...readyInspection(),
        state: "needs_remote_selection",
        selectedRemote: undefined,
        suggestedRepoTarget: undefined,
        repoTarget: "",
        repoTargetSource: "",
        registrationDisposition: "",
        confirmationHash: "",
      },
      {
        ...readyInspection(),
        state: "needs_target_override",
        targetOverrideReason: "unsupported_remote",
        repoTarget: "",
        repoTargetSource: "",
        registrationDisposition: "",
        confirmationHash: "",
      },
      {
        state: "conflict",
        selectedPath: "D:/Code/relay-copy",
        resolvedLocalPath: "D:/Code/relay-copy",
        remotes: [remoteValue()],
        repoTarget: "relay",
        repoTargetSource: "remote_basename",
        conflictKind: "target",
        notices: [],
      },
    ];

    for (const value of invalidValues) {
      globalThis.fetch = vi.fn().mockResolvedValue(response(200, value));
      await expectMalformed(
        () => inspectWorkflowRepository({ localPath: "D:/Code/relay" }),
        "inspection.",
      );
    }
  });

  it("rejects malformed ready-state and remote contracts", async () => {
    const invalidValues = [
      readyInspection({ confirmationHash: undefined }),
      readyInspection({ registrationDisposition: "" }),
      readyInspection({ repoTargetSource: "guessed" }),
      readyInspection({ selectedRemote: null }),
      {
        state: "needs_target_override",
        selectedPath: "D:/Code/relay",
        resolvedLocalPath: "D:/Code/relay",
        remotes: [],
        targetOverrideReason: "unknown",
        notices: [],
      },
      readyInspection({
        remotes: [remoteValue({ suggestedRepoTarget: 7 })],
      }),
    ];

    for (const value of invalidValues) {
      globalThis.fetch = vi.fn().mockResolvedValue(response(200, value));
      await expectMalformed(
        () => inspectWorkflowRepository({ localPath: "D:/Code/relay" }),
        "inspection.",
      );
    }
  });

  it("confirms created and reused registrations with strict response normalization", async () => {
    for (const outcome of ["created", "reused"] as const) {
      const fetchSpy = vi.fn().mockResolvedValue(response(
        outcome === "created" ? 201 : 200,
        {
          outcome,
          repository: targetValue(),
        },
      ));
      globalThis.fetch = fetchSpy;

      const result = await confirmWorkflowRepository({
        localPath: "D:/Code/relay",
        expectedConfirmationHash: "a".repeat(64),
      });

      expect(result.outcome).toBe(outcome);
      expect(result.repository.repoTarget).toBe("relay");
      expect(fetchSpy).toHaveBeenCalledWith(
        "http://localhost:8080/api/repositories",
        expect.objectContaining({
          method: "POST",
          body: JSON.stringify({
            localPath: "D:/Code/relay",
            remoteName: "",
            repoTargetOverride: "",
            expectedConfirmationHash: "a".repeat(64),
          }),
        }),
      );
    }
  });

  it("surfaces stale and conflict confirmations with a normalized current inspection", async () => {
    for (const inspection of [
      readyInspection({ confirmationHash: "b".repeat(64) }),
      {
        state: "conflict",
        selectedPath: "D:/Code/relay",
        resolvedLocalPath: "D:/Code/relay",
        remotes: [remoteValue()],
        selectedRemote: remoteValue(),
        repoTarget: "relay",
        repoTargetSource: "remote_basename",
        existingRepository: targetValue({ localPath: "D:/Other/relay" }),
        conflictKind: "target",
        notices: [],
      },
    ]) {
      globalThis.fetch = vi.fn().mockResolvedValue(response(409, {
        error: "CONFIRMATION_REQUIRED",
        message: "Repository inspection must be confirmed again",
        details: { inspection },
      }));

      let caught: unknown;
      try {
        await confirmWorkflowRepository({
          localPath: "D:/Code/relay",
          expectedConfirmationHash: "a".repeat(64),
        });
      } catch (error) {
        caught = error;
      }
      expect(caught).toBeInstanceOf(WorkflowRepositoryConfirmationError);
      expect(
        (caught as WorkflowRepositoryConfirmationError).inspection.state,
      ).toBe(inspection.state);
    }
  });

  it("does not hide malformed confirmation details behind the original API error", async () => {
    globalThis.fetch = vi.fn().mockResolvedValue(response(409, {
      error: "CONFIRMATION_REQUIRED",
      message: "Repository inspection must be confirmed again",
      details: {
        inspection: {
          ...readyInspection(),
          confirmationHash: "",
        },
      },
    }));

    await expectMalformed(
      () => confirmWorkflowRepository({
        localPath: "D:/Code/relay",
        expectedConfirmationHash: "a".repeat(64),
      }),
      "confirmation.details.inspection.confirmationHash",
    );
  });

  it("rejects malformed registration outcomes and repository rows", async () => {
    for (const value of [
      { outcome: "created", repository: targetValue({ localPath: "" }) },
      { outcome: "existing", repository: targetValue() },
    ]) {
      globalThis.fetch = vi.fn().mockResolvedValue(response(200, value));
      await expectMalformed(
        () => confirmWorkflowRepository({
          localPath: "D:/Code/relay",
          expectedConfirmationHash: "a".repeat(64),
        }),
        "registration.",
      );
    }
  });
});
