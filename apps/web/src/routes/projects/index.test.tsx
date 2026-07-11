// @vitest-environment jsdom

import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { ProjectsListPage } from "./index";

vi.mock("@tanstack/react-query", () => ({
  useQuery: () => ({
    data: { projects: [] },
    isLoading: false,
    error: null,
  }),
  queryOptions: (options: unknown) => options,
}));

vi.mock("@tanstack/react-router", () => ({
  createFileRoute: () => (configuration: unknown) => configuration,
  Link: ({
    to,
    children,
  }: {
    to: string;
    children: React.ReactNode;
  }) => <a href={to}>{children}</a>,
}));

vi.mock("@/components/relay/AppPageFrame", () => ({
  AppPageFrame: ({
    actions,
    children,
  }: {
    actions: React.ReactNode;
    children: React.ReactNode;
  }) => (
    <section>
      <div>{actions}</div>
      <div>{children}</div>
    </section>
  ),
}));

vi.mock("@/components/relay/RelayProjectsRegistry", () => ({
  RelayProjectsRegistry: () => <div>Project registry</div>,
}));

describe("ProjectsListPage", () => {
  it("links operators to the global repository registry without replacing Project creation", () => {
    render(<ProjectsListPage />);

    expect(screen.getByRole("link", { name: "Repositories" })).toHaveAttribute(
      "href",
      "/repositories",
    );
    expect(screen.getByRole("link", { name: "New Project" })).toHaveAttribute(
      "href",
      "/projects/new",
    );
  });
});
