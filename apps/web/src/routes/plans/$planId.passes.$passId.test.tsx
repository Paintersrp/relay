// @vitest-environment jsdom

import { describe, expect, it } from "vitest";

describe("plan pass detail route", () => {
  it("exports the file route with pass detail component", async () => {
    const { Route } = await import("./$planId.passes.$passId");
    
    expect(Route).toBeDefined();
    expect(Route.options).toBeDefined();
    // The route is configured to use the RelayCanonicalPlanPassDetail component
    expect(typeof Route.options.component).toBe("function");
  });
});