// @vitest-environment jsdom

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { CutoverPage } from "./CutoverPage";

function createTestQueryClient() {
  return new QueryClient({
    defaultOptions: {
      queries: { retry: false, refetchOnWindowFocus: false, gcTime: 0 },
    },
  });
}

function stubCutoverApi(fixtures: { state?: unknown; history?: unknown } = {}) {
  const originalFetch = globalThis.fetch;
  globalThis.fetch = vi.fn(async (input: RequestInfo | URL): Promise<Response> => {
    const url = typeof input === "string" ? input : input.toString();
    const pathname = (() => { try { return new URL(url).pathname; } catch { return url; } })();

    if (pathname === "/api/cutover/state") {
      return new Response(JSON.stringify(fixtures.state ?? { active: false }), { status: 200, headers: { "Content-Type": "application/json" } });
    }
    if (pathname === "/api/cutover/history") {
      return new Response(JSON.stringify(fixtures.history ?? { items: [], count: 0 }), { status: 200, headers: { "Content-Type": "application/json" } });
    }
    return new Response(JSON.stringify({}), { status: 200, headers: { "Content-Type": "application/json" } });
  }) as unknown as typeof fetch;
  return () => { globalThis.fetch = originalFetch; };
}

describe("CutoverPage", () => {
  let restore: () => void;

  beforeEach(() => {
    restore = stubCutoverApi();
  });

  afterEach(() => {
    restore();
  });

  it("renders inactive state when no cutover is active", async () => {
    const queryClient = createTestQueryClient();
    const { findByText } = render(
      <QueryClientProvider client={queryClient}>
        <CutoverPage />
      </QueryClientProvider>,
    );
    expect(await findByText("Current Mode")).toBeDefined();
  });
});
