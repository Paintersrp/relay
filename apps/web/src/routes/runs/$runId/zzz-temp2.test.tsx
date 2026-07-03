// @vitest-environment jsdom
import { describe, it, expect } from "vitest";
import { Route as IntakeRoute } from "./intake";

describe("temp2", () => {
  it("works", () => {
    expect(typeof IntakeRoute.options.component).toBe("function");
  });
});
