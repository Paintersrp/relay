// @vitest-environment jsdom

import { describe, expect, it } from "vitest";

describe("audit route", () => {
  it("exports the file route with workbench component for audit stage", async () => {
    const { Route } = await import("./audit");
    
    expect(Route).toBeDefined();
    expect(Route.options).toBeDefined();
    // The route is configured to use the RelayCanonicalRunWorkbench component
    // which will render with stage="audit"
    expect(typeof Route.options.component).toBe("function");
  });
});
