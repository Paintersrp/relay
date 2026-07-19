// @vitest-environment jsdom
import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { QueryClientProvider, QueryClient } from "@tanstack/react-query";
import { RelayFeatureWorkspaceDetail } from "./RelayFeatureWorkspaceDetail";
import type { FeatureWorkspaceDetail } from "@/features/relay-feature-workspaces";

function wrapper({ children }: { children: React.ReactNode }) {
  return <QueryClientProvider client={new QueryClient()}>{children}</QueryClientProvider>;
}

const base: FeatureWorkspaceDetail = {
  workspace: { workspaceId: "workspace-1", featureSlug: "payments", state: "open", version: 2, createdAt: "", updatedAt: "" },
  inputs: [],
  destinations: [],
  tickets: [],
  routes: [],
  authorityRevisions: [{
    authorityRevisionId: "authority-1",
    revisionNumber: 1,
    sourceClosureRowId: null,
    layers: [{ kind: "requirements", sequence: 1, artifactRowId: 10, retainedArtifactRowId: null, artifactSha256: "a".repeat(64), sourceClosureRowId: null, approvalRowId: 5 }],
    createdAt: "",
  }],
  sourceBasis: { status: "not_recorded", investigationCount: 0 },
};

describe("RelayFeatureWorkspaceDetail", () => {
  it("renders separate record approval and publish approved authority controls", () => {
    render(<RelayFeatureWorkspaceDetail detail={base} />, { wrapper });
    expect(screen.getByRole("button", { name: "Record approval" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Publish approved authority" })).toBeTruthy();
  });

  it("shows approval row ids in authority history", () => {
    render(<RelayFeatureWorkspaceDetail detail={base} />, { wrapper });
    expect(screen.getByText(/approval row 5/)).toBeTruthy();
  });

  it("renders discovery, route, source basis sections", () => {
    render(<RelayFeatureWorkspaceDetail detail={base} />, { wrapper });
    expect(screen.getByText(/no retained source evidence recorded/)).toBeTruthy();
    expect(screen.getByText("Discovery")).toBeTruthy();
    expect(screen.getByText("Route workspace")).toBeTruthy();
  });
});
