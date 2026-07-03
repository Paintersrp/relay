// @vitest-environment jsdom
import { describe, expect, it, vi } from "vitest";

vi.mock("@/features/relay-runs/api", async () => {
  const actual = await vi.importActual<typeof import("@/features/relay-runs/api")>(
    "@/features/relay-runs/api",
  );
  return {
    ...actual,
    approveIntake: vi.fn().mockResolvedValue({ success: true }),
  };
});

import { Route as IntakeRoute } from "./intake";

describe("bisect", () => {
  it("works", () => {
    expect(typeof IntakeRoute).toBe("object");
  });
});
