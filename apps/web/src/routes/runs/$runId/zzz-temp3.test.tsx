// @vitest-environment jsdom
import { describe, it, expect } from "vitest";
import { Route as ExecuteRoute } from "./execute";

describe("temp3", () => {
  it("works", () => {
    expect(typeof ExecuteRoute.options.component).toBe("function");
  });
});
