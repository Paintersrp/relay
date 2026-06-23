import { afterAll, beforeEach, describe, expect, it, vi } from "vitest";

import { RelayApiError } from "@/features/relay-runs";
import {
  getProjects,
  getProject,
  createProject,
} from "./api";

describe("relay-projects api", () => {
  const originalFetch = globalThis.fetch;

  beforeEach(() => {
    vi.restoreAllMocks();
  });

  afterAll(() => {
    globalThis.fetch = originalFetch;
  });

  it("getProjects() normalizes missing projects array", async () => {
    const fetchSpy = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      text: async () => JSON.stringify({ success: true, count: 0 }),
    });
    globalThis.fetch = fetchSpy;

    const res = await getProjects();

    expect(fetchSpy).toHaveBeenCalledTimes(1);
    expect(res.projects).toEqual([]);
  });

  it("getProject() normalizes missing repositories array", async () => {
    const fetchSpy = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      text: async () =>
        JSON.stringify({
          success: true,
          project: {
            projectId: "p1",
            name: "Project 1",
            description: "Desc",
            status: "active",
            defaultRepositoryId: "",
            createdAt: "2026-06-21T00:00:00Z",
            updatedAt: "2026-06-21T00:00:00Z",
          },
        }),
    });
    globalThis.fetch = fetchSpy;

    const res = await getProject("p1");

    expect(res.project.repositories).toEqual([]);
  });

  it("failed mutation with validation details throws RelayApiError with correct shape", async () => {
    const mockValidationDetails = [
      { field: "project_id", code: "required", message: "Project ID is required" }
    ];
    const fetchSpy = vi.fn().mockResolvedValue({
      ok: false,
      status: 400,
      text: async () =>
        JSON.stringify({
          error: "VALIDATION_ERROR",
          message: "Project configuration validation failed",
          details: {
            validation: mockValidationDetails,
          },
        }),
    });
    globalThis.fetch = fetchSpy;

    try {
      await createProject({
        project_id: "",
        name: "Test Project",
      });
      throw new Error("should have failed");
    } catch (err: any) {
      expect(err).toBeInstanceOf(RelayApiError);
      expect(err.errorShape?.error).toBe("VALIDATION_ERROR");
      expect(err.errorShape?.details?.validation).toEqual(mockValidationDetails);
    }
  });
});
