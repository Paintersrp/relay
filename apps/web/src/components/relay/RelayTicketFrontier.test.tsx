// @vitest-environment jsdom
import * as React from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { RelayTicketFrontier } from "./RelayTicketFrontier";

function response(body: unknown, status = 200): Response { return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } }); }
function wrapper({ children }: { children: React.ReactNode }) {
  return <QueryClientProvider client={new QueryClient()}>{children}</QueryClientProvider>;
}
afterEach(() => vi.unstubAllGlobals());

describe("RelayTicketFrontier", () => {
  it("loads the direct workspace frontier without packet-admission controls", async () => {
    const fetch = vi.fn().mockResolvedValue(response({ workspaceId: "workspace-1", entries: [] }));
    vi.stubGlobal("fetch", fetch);

    render(<RelayTicketFrontier workspaceId="workspace-1" />, { wrapper });

    expect(screen.queryByLabelText("Planner packet ID")).toBeNull();
    expect(screen.queryByLabelText("Operation")).toBeNull();
    expect(screen.queryByText(/packet-admitted/i)).toBeNull();
    await waitFor(() => expect(fetch).toHaveBeenCalledTimes(1));
  });
});
