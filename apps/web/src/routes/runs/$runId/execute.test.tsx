// @vitest-environment jsdom

import { describe, expect, it } from "vitest";

describe("execute route", () => {
  it("exports the file route with workbench component for execute stage", async () => {
    const { Route } = await import("./execute");
    
    expect(Route).toBeDefined();
    expect(Route.options).toBeDefined();
    // The route is configured to use the RelayCanonicalRunWorkbench component
    // which will render with stage="execute"
    expect(typeof Route.options.component).toBe("function");
  });
});
