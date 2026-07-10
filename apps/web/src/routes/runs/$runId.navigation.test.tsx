// @vitest-environment jsdom

import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import {
  Link,
  Outlet,
  RouterProvider,
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
} from "@tanstack/react-router";
import { describe, expect, it } from "vitest";

import { RunLayout } from "./$runId";

function RootLayout() {
  return <Outlet />;
}

function RunStagePage() {
  return (
    <main>
      <h1>Run stage</h1>
      <Link to="/plans">Plans</Link>
      <Link to="/projects">Projects</Link>
      <Link to="/runs">Runs</Link>
    </main>
  );
}

function PlansPage() {
  return <h1>Plans destination</h1>;
}

function ProjectsPage() {
  return <h1>Projects destination</h1>;
}

function RunsPage() {
  return <h1>Runs destination</h1>;
}

async function renderNavigationRouter() {
  const rootRoute = createRootRoute({
    component: RootLayout,
  });
  const runRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: "/runs/$runId",
    component: RunLayout,
  });
  const runStageRoute = createRoute({
    getParentRoute: () => runRoute,
    path: "/specification",
    component: RunStagePage,
  });
  const plansRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: "/plans",
    component: PlansPage,
  });
  const projectsRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: "/projects",
    component: ProjectsPage,
  });
  const runsRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: "/runs",
    component: RunsPage,
  });
  const routeTree = rootRoute.addChildren([
    runRoute.addChildren([runStageRoute]),
    plansRoute,
    projectsRoute,
    runsRoute,
  ]);
  const history = createMemoryHistory({
    initialEntries: ["/runs/run-1/specification"],
  });
  const router = createRouter({
    routeTree,
    history,
  });

  await router.load();
  render(<RouterProvider router={router} />);
  expect(
    await screen.findByRole("heading", { name: "Run stage" }),
  ).toBeInTheDocument();

  return router;
}

describe("Run layout navigation escape", () => {
  it.each([
    ["Plans", "/plans", "Plans destination"],
    ["Projects", "/projects", "Projects destination"],
    ["Runs", "/runs", "Runs destination"],
  ])(
    "allows the %s navigation destination to leave the active Run",
    async (linkName, pathname, heading) => {
      const user = userEvent.setup();
      const router = await renderNavigationRouter();

      await user.click(screen.getByRole("link", { name: linkName }));

      await waitFor(() => {
        expect(router.state.location.pathname).toBe(pathname);
      });
      expect(
        await screen.findByRole("heading", { name: heading }),
      ).toBeInTheDocument();
    },
  );

  it("allows browser history to leave and re-enter a Run without a competing redirect", async () => {
    const user = userEvent.setup();
    const router = await renderNavigationRouter();

    await user.click(screen.getByRole("link", { name: "Plans" }));
    await waitFor(() => {
      expect(router.state.location.pathname).toBe("/plans");
    });

    await act(async () => {
      router.history.back();
    });
    await waitFor(() => {
      expect(router.state.location.pathname).toBe(
        "/runs/run-1/specification",
      );
    });
    expect(
      await screen.findByRole("heading", { name: "Run stage" }),
    ).toBeInTheDocument();

    await act(async () => {
      router.history.forward();
    });
    await waitFor(() => {
      expect(router.state.location.pathname).toBe("/plans");
    });
    expect(
      await screen.findByRole("heading", { name: "Plans destination" }),
    ).toBeInTheDocument();
  });
});
