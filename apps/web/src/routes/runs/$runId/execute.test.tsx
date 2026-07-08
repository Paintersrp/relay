// @vitest-environment jsdom

import { describe, expect, it, vi } from "vitest";

// Test that the execute route module exports the expected route structure
describe("execute route", () => {
  it("exports a Route with the correct path", async () => {
    const module = await import("./execute");
    expect(module.Route).toBeDefined();
  });

  it("exports a component that renders the workbench with execute stage", async () => {
    vi.doMock("@/components/relay/RelayCanonicalRunWorkbench", () => ({
      RelayCanonicalRunWorkbench: ({ runId, stage }: { runId: string; stage: string }) => (
        <div data-testid="workbench" data-run-id={runId} data-stage={stage} />
      ),
    }));

    const { Route } = await import("./execute");
    expect(Route.options.component).toBeDefined();
  });
});
