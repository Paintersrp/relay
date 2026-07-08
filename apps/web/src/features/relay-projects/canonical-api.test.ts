// @vitest-environment jsdom

import { afterAll, beforeEach, describe, expect, it, vi } from "vitest";

import { RelayApiError } from "@/features/relay-runs";
import {
  archiveWorkflowProject,
  attachWorkflowProjectRepository,
  createWorkflowProject,
  createWorkflowProjectNote,
  deleteWorkflowProjectNote,
  detachWorkflowProjectRepository,
  getWorkflowProject,
  listWorkflowProjects,
  listWorkflowRepositoryTargets,
  updateWorkflowProject,
  updateWorkflowProjectNote,
} from "./canonical-api";

function response(status: number, value?: unknown) {
  return {
    ok: status >= 200 && status < 300,
    status,
    text: async () => value === undefined ? "" : JSON.stringify(value),
  };
}

function projectValue(overrides: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    projectId: "project-1",
    name: "Relay",
    description: "Relay work",
    status: "active",
    createdAt: "2026-07-07T00:00:00Z",
    updatedAt: "2026-07-07T01:00:00Z",
    ...overrides,
  };
}

function repositoryTargetValue(overrides: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    repoTarget: "relay",
    localPath: "D:/Code/relay",
    createdAt: "2026-07-07T00:00:00Z",
    updatedAt: "2026-07-07T01:00:00Z",
    ...overrides,
  };
}

function repositoryReferenceValue(overrides: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    repoTarget: "relay",
    createdAt: "2026-07-07T00:00:00Z",
    ...overrides,
  };
}

function noteValue(overrides: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    noteId: "note-1",
    title: "Follow-up",
    body: "Check closeout.",
    status: "open",
    createdAt: "2026-07-07T00:00:00Z",
    updatedAt: "2026-07-07T01:00:00Z",
    ...overrides,
  };
}

function planValue(overrides: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    planId: "plan-1",
    featureSlug: "feature",
    status: "active",
    createdAt: "2026-07-07T00:00:00Z",
    updatedAt: "2026-07-07T01:00:00Z",
    ...overrides,
  };
}

function detailValue(overrides: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    project: projectValue(),
    repositories: [repositoryReferenceValue()],
    notes: [noteValue()],
    plans: [planValue()],
    ...overrides,
  };
}

async function expectMalformedResponse(
  run: () => Promise<unknown>,
  expected: {
    endpoint: string;
    method: string;
    detail: string;
  },
) {
  let caught: unknown;
  try {
    await run();
  } catch (error) {
    caught = error;
  }

  expect(caught).toBeInstanceOf(RelayApiError);
  expect(caught).toMatchObject({
    status: 502,
    endpoint: expected.endpoint,
    method: expected.method,
  });
  expect((caught as Error).message).toContain(expected.detail);
}

describe("canonical relay-projects api", () => {
  const originalFetch = globalThis.fetch;
  const projectDetailPath =
    "/api/projects/project-1?repositoryLimit=100&noteLimit=100&planLimit=100";

  beforeEach(() => {
    vi.restoreAllMocks();
    globalThis.fetch = originalFetch;
  });

  afterAll(() => {
    globalThis.fetch = originalFetch;
  });

  it("lists bounded Projects using the canonical items envelope", async () => {
    const fetchSpy = vi.fn().mockResolvedValue(response(200, {
      items: [
        projectValue({
          projectId: "project-active",
          name: "Active Project",
          description: "Current work",
        }),
      ],
      count: 1,
    }));
    globalThis.fetch = fetchSpy;

    const result = await listWorkflowProjects({ status: "active", limit: 25 });

    expect(fetchSpy).toHaveBeenCalledWith(
      "http://localhost:8080/api/projects?status=active&limit=25",
      expect.objectContaining({ method: "GET" }),
    );
    expect(result.count).toBe(1);
    expect(result.projects[0]?.projectId).toBe("project-active");
  });

  it("rejects malformed required Project list envelopes, counts, and fields", async () => {
    const cases = [
      {
        value: [],
        detail: "response must be an object",
      },
      {
        value: { count: 0 },
        detail: "response.items",
      },
      {
        value: { items: [], count: "0" },
        detail: "response.count",
      },
      {
        value: {
          items: [projectValue({ projectId: "" })],
          count: 1,
        },
        detail: "items[0].projectId",
      },
      {
        value: {
          items: [projectValue({ name: 7 })],
          count: 1,
        },
        detail: "items[0].name",
      },
      {
        value: {
          items: [projectValue({ createdAt: null })],
          count: 1,
        },
        detail: "items[0].createdAt",
      },
    ];

    for (const testCase of cases) {
      globalThis.fetch = vi.fn().mockResolvedValue(response(200, testCase.value));
      await expectMalformedResponse(
        () => listWorkflowProjects(),
        {
          endpoint: "/api/projects",
          method: "GET",
          detail: testCase.detail,
        },
      );
    }
  });

  it("rejects missing and unsupported Project lifecycle states", async () => {
    for (const status of [undefined, "paused"]) {
      const project = projectValue();
      if (status === undefined) {
        delete project.status;
      } else {
        project.status = status;
      }
      globalThis.fetch = vi.fn().mockResolvedValue(response(200, {
        items: [project],
        count: 1,
      }));

      await expectMalformedResponse(
        () => listWorkflowProjects(),
        {
          endpoint: "/api/projects",
          method: "GET",
          detail: "items[0].status",
        },
      );
    }
  });

  it("rejects unsupported Project Note lifecycle state with detail context", async () => {
    globalThis.fetch = vi.fn().mockResolvedValue(response(200, detailValue({
      notes: [noteValue({ status: "queued" })],
    })));

    await expectMalformedResponse(
      () => getWorkflowProject("project-1"),
      {
        endpoint: projectDetailPath,
        method: "GET",
        detail: "notes[0].status",
      },
    );
  });

  it("loads bounded Project detail collections", async () => {
    globalThis.fetch = vi.fn().mockResolvedValue(response(200, detailValue({
      project: projectValue({ status: "archived" }),
      notes: [noteValue({ status: "done" })],
    })));

    const result = await getWorkflowProject("project-1");

    expect(result.project.status).toBe("archived");
    expect(result.repositories[0]?.repoTarget).toBe("relay");
    expect(result.notes[0]?.status).toBe("done");
    expect(result.plans[0]?.featureSlug).toBe("feature");
    expect(globalThis.fetch).toHaveBeenCalledWith(
      `http://localhost:8080${projectDetailPath}`,
      expect.objectContaining({ method: "GET" }),
    );
  });

  it("rejects missing or wrong-type required Project detail envelopes and arrays", async () => {
    globalThis.fetch = vi.fn().mockResolvedValue(response(200, detailValue({
      project: undefined,
    })));
    await expectMalformedResponse(
      () => getWorkflowProject("project-1"),
      {
        endpoint: projectDetailPath,
        method: "GET",
        detail: "project must be an object",
      },
    );

    for (const field of ["repositories", "notes", "plans"] as const) {
      globalThis.fetch = vi.fn().mockResolvedValue(response(200, detailValue({
        [field]: field === "notes" ? undefined : {},
      })));

      await expectMalformedResponse(
        () => getWorkflowProject("project-1"),
        {
          endpoint: projectDetailPath,
          method: "GET",
          detail: `response.${field}`,
        },
      );
    }
  });

  it("rejects malformed repository references, attached Plans, and Notes", async () => {
    const cases = [
      {
        value: detailValue({
          repositories: [repositoryReferenceValue({ repoTarget: "" })],
        }),
        detail: "repositories[0].repoTarget",
      },
      {
        value: detailValue({
          plans: [planValue({ featureSlug: 17 })],
        }),
        detail: "plans[0].featureSlug",
      },
      {
        value: detailValue({
          notes: [noteValue({ body: null })],
        }),
        detail: "notes[0].body",
      },
    ];

    for (const testCase of cases) {
      globalThis.fetch = vi.fn().mockResolvedValue(response(200, testCase.value));
      await expectMalformedResponse(
        () => getWorkflowProject("project-1"),
        {
          endpoint: projectDetailPath,
          method: "GET",
          detail: testCase.detail,
        },
      );
    }
  });

  it("rejects malformed global repository-target envelopes and fields", async () => {
    const cases = [
      {
        value: { count: 0 },
        detail: "response.items",
      },
      {
        value: { items: [], count: null },
        detail: "response.count",
      },
      {
        value: {
          items: [repositoryTargetValue({ localPath: "" })],
          count: 1,
        },
        detail: "items[0].localPath",
      },
    ];

    for (const testCase of cases) {
      globalThis.fetch = vi.fn().mockResolvedValue(response(200, testCase.value));
      await expectMalformedResponse(
        () => listWorkflowRepositoryTargets(),
        {
          endpoint: "/api/repositories",
          method: "GET",
          detail: testCase.detail,
        },
      );
    }
  });

  it("rejects malformed repository-reference mutation responses", async () => {
    globalThis.fetch = vi.fn().mockResolvedValue(response(200, {
      repoTarget: "relay",
      createdAt: 7,
    }));

    await expectMalformedResponse(
      () => attachWorkflowProjectRepository("project-1", "relay"),
      {
        endpoint: "/api/projects/project-1/repositories/relay",
        method: "PUT",
        detail: "repositoryReference.createdAt",
      },
    );
  });

  it("rejects malformed Project Note mutation responses", async () => {
    globalThis.fetch = vi.fn().mockResolvedValue(response(201, noteValue({
      title: "",
    })));

    await expectMalformedResponse(
      () => createWorkflowProjectNote("project-1", {
        title: "Follow-up",
        body: "Check closeout.",
      }),
      {
        endpoint: "/api/projects/project-1/notes",
        method: "POST",
        detail: "note.title",
      },
    );
  });

  it("creates, edits, and archives Projects without client-owned IDs or status", async () => {
    const fetchSpy = vi.fn()
      .mockResolvedValueOnce(response(201, projectValue({
        projectId: "project-generated",
        description: "Primary work",
      })))
      .mockResolvedValueOnce(response(200, projectValue({
        projectId: "project-generated",
        name: "Relay Updated",
        description: "Primary work",
      })))
      .mockResolvedValueOnce(response(200, projectValue({
        projectId: "project-generated",
        name: "Relay Updated",
        description: "Primary work",
        status: "archived",
        updatedAt: "2026-07-07T02:00:00Z",
      })));
    globalThis.fetch = fetchSpy;

    await createWorkflowProject({ name: "Relay", description: "Primary work" });
    await updateWorkflowProject("project-generated", { name: "Relay Updated" });
    const archived = await archiveWorkflowProject("project-generated");

    const createCall = fetchSpy.mock.calls[0];
    expect(createCall?.[1]).toEqual(expect.objectContaining({
      method: "POST",
      body: JSON.stringify({ name: "Relay", description: "Primary work" }),
    }));
    expect(fetchSpy.mock.calls[1]?.[1]).toEqual(expect.objectContaining({
      method: "PATCH",
      body: JSON.stringify({ name: "Relay Updated" }),
    }));
    expect(fetchSpy.mock.calls[2]?.[1]).toEqual(expect.objectContaining({
      method: "POST",
      body: undefined,
    }));
    expect(archived.status).toBe("archived");
  });

  it("uses global repository targets and non-owning Project references", async () => {
    const fetchSpy = vi.fn()
      .mockResolvedValueOnce(response(200, {
        items: [repositoryTargetValue()],
        count: 1,
      }))
      .mockResolvedValueOnce(response(200, repositoryReferenceValue()))
      .mockResolvedValueOnce(response(204));
    globalThis.fetch = fetchSpy;

    const repositories = await listWorkflowRepositoryTargets();
    await attachWorkflowProjectRepository("project-1", "relay");
    await detachWorkflowProjectRepository("project-1", "relay");

    expect(repositories.repositories[0]?.localPath).toBe("D:/Code/relay");
    expect(fetchSpy.mock.calls[1]?.[0]).toBe(
      "http://localhost:8080/api/projects/project-1/repositories/relay",
    );
    expect(fetchSpy.mock.calls[1]?.[1]).toEqual(expect.objectContaining({ method: "PUT" }));
    expect(fetchSpy.mock.calls[2]?.[1]).toEqual(expect.objectContaining({ method: "DELETE" }));
  });

  it("requires exact empty 204 responses for delete mutations", async () => {
    globalThis.fetch = vi.fn().mockResolvedValue(response(200, {}));
    await expectMalformedResponse(
      () => detachWorkflowProjectRepository("project-1", "relay"),
      {
        endpoint: "/api/projects/project-1/repositories/relay",
        method: "DELETE",
        detail: "expected HTTP 204",
      },
    );

    globalThis.fetch = vi.fn().mockResolvedValue(response(204, { unexpected: true }));
    await expectMalformedResponse(
      () => deleteWorkflowProjectNote("project-1", "note-1"),
      {
        endpoint: "/api/projects/project-1/notes/note-1",
        method: "DELETE",
        detail: "must not include a response body",
      },
    );
  });

  it("creates, completes, reopens, and deletes ordinary Project Notes", async () => {
    const note = noteValue();
    const fetchSpy = vi.fn()
      .mockResolvedValueOnce(response(201, note))
      .mockResolvedValueOnce(response(200, { ...note, status: "done" }))
      .mockResolvedValueOnce(response(200, { ...note, status: "open" }))
      .mockResolvedValueOnce(response(204));
    globalThis.fetch = fetchSpy;

    await createWorkflowProjectNote("project-1", {
      title: "Follow-up",
      body: "Check closeout.",
    });
    await updateWorkflowProjectNote("project-1", "note-1", { status: "done" });
    await updateWorkflowProjectNote("project-1", "note-1", { status: "open" });
    await deleteWorkflowProjectNote("project-1", "note-1");

    expect(fetchSpy.mock.calls[0]?.[1]).toEqual(expect.objectContaining({ method: "POST" }));
    expect(fetchSpy.mock.calls[1]?.[1]).toEqual(expect.objectContaining({
      method: "PATCH",
      body: JSON.stringify({ status: "done" }),
    }));
    expect(fetchSpy.mock.calls[3]?.[1]).toEqual(expect.objectContaining({ method: "DELETE" }));
  });

  it("preserves structured recoverable API errors", async () => {
    globalThis.fetch = vi.fn().mockResolvedValue(response(400, {
      error: "BAD_REQUEST",
      message: "Project name is required",
    }));

    await expect(
      createWorkflowProject({ name: "", description: "kept in form" }),
    ).rejects.toMatchObject({
      name: "RelayApiError",
      status: 400,
      errorShape: {
        error: "BAD_REQUEST",
        message: "Project name is required",
      },
    } satisfies Partial<RelayApiError>);
  });
});
