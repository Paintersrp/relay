// @vitest-environment jsdom
//
// ============================================================
// Integration — Shell presence and active-state semantics (task 13.1)
// ============================================================
//
// Renders the recomposed AppShell inside an in-memory TanStack Router (via the
// shared harness in `src/test/shell-test-utils.tsx`) across representative
// routes and asserts the shell's structural + accessibility contract:
//
//   - Activity_Rail + Top_Bar present on every representative route      (Req 1.1)
//   - Rail shows the three primary domains as navigable destinations     (Req 1.2)
//   - Rail exposes a Theme_System control                                (Req 1.3)
//   - Top_Bar shows Scope_Switcher + Global_Search entry + Attention     (Req 1.6)
//   - No primary-domain nav inside the Top_Bar on run-scoped routes      (Req 1.7)
//   - aria-current="page" marks exactly the active domain                (Req 9.6)
//   - Accessible names on rail icon controls and nav landmarks           (Req 9.5)
//
// Rendering/layout/contrast concerns (focus rings, CSS visibility, contrast
// ratios) are out of scope here — this file asserts DOM presence and
// programmatically determinable semantics only.

import { describe, it, expect, afterEach, beforeEach, vi } from "vitest";
import { within } from "@testing-library/react";

import {
  renderShell,
  type RenderShellResult,
} from "@/test/shell-test-utils";

// Representative routes spanning each primary domain + a non-domain root and a
// run-scoped stage route.
const REPRESENTATIVE_ROUTES = [
  "/",
  "/runs",
  "/plans",
  "/projects",
  "/runs/run-1/intake",
] as const;

let active: RenderShellResult | null = null;

afterEach(() => {
  active?.restore();
  active = null;
  vi.restoreAllMocks();
});

/** Locate the Top_Bar header element (the banner region) for scoped queries. */
function getTopBar(result: RenderShellResult): HTMLElement {
  const header = result.container.querySelector("header");
  if (!header) throw new Error("Top_Bar <header> not found in shell");
  return header;
}

describe("Shell presence and active-state semantics (task 13.1)", () => {
  beforeEach(() => {
    // Keep act() noise from async query settling out of the test output.
    vi.spyOn(console, "error").mockImplementation(() => {});
  });

  describe("Activity_Rail + Top_Bar presence on representative routes (Req 1.1)", () => {
    for (const path of REPRESENTATIVE_ROUTES) {
      it(`renders both the rail and top bar on ${path}`, async () => {
        active = await renderShell({ initialPath: path });
        const screen = active;

        // Activity_Rail — the primary navigation landmark (accessible name
        // "Primary"); the collapsed mobile variant's nav is unmounted while its
        // Sheet is closed, so exactly the desktop rail nav is present.
        expect(
          screen.getByRole("navigation", { name: "Primary" }),
        ).toBeInTheDocument();

        // Top_Bar — the banner region hosting global context.
        expect(getTopBar(screen)).toBeInTheDocument();
      });
    }
  });

  it("rail shows the three primary domains as navigable destinations (Req 1.2)", async () => {
    active = await renderShell({ initialPath: "/" });
    const screen = active;
    const rail = screen.getByRole("navigation", { name: "Primary" });

    for (const name of ["Projects", "Plans", "Runs"] as const) {
      const link = within(rail).getByRole("link", { name });
      expect(link).toBeInTheDocument();
      // Navigable destination → carries an href.
      expect(link).toHaveAttribute("href");
    }
  });

  it("rail exposes a Theme_System control that stays selectable (Req 1.3)", async () => {
    active = await renderShell({ initialPath: "/" });
    const screen = active;

    // The theme control's accessible name reflects the toggle action. Both the
    // desktop-rail and the below-breakpoint copies live in the DOM, so assert
    // at least one is present and operable (not disabled).
    const themeControls = screen.getAllByRole("button", {
      name: /switch to (light|dark) theme/i,
    });
    expect(themeControls.length).toBeGreaterThanOrEqual(1);
    expect(themeControls[0]).toBeEnabled();
  });

  it("top bar shows the scope switcher, global search entry, and attention indicator (Req 1.6)", async () => {
    active = await renderShell({ initialPath: "/runs" });
    const screen = active;
    const topBar = within(getTopBar(screen));

    // Scope_Switcher — keyboard-operable control exposing the active scope.
    expect(topBar.getByLabelText("Active scope")).toBeInTheDocument();

    // Global_Search entry point.
    expect(
      topBar.getByRole("button", {
        name: /search projects, plans, passes, and runs/i,
      }),
    ).toBeInTheDocument();

    // Attention indicator (links to the Home_Overview).
    expect(
      topBar.getByRole("link", { name: /item.*attention|no items need attention/i }),
    ).toBeInTheDocument();
  });

  it("keeps primary-domain navigation out of the Top_Bar on a run-scoped route (Req 1.7)", async () => {
    active = await renderShell({ initialPath: "/runs/run-1/intake" });
    const screen = active;
    const topBar = within(getTopBar(screen));

    // None of the primary-domain destinations may appear inside the Top_Bar;
    // they live exclusively in the Activity_Rail.
    for (const name of ["Projects", "Plans", "Runs"] as const) {
      expect(topBar.queryByRole("link", { name })).toBeNull();
    }

    // The rail (outside the header) still carries them — confirms the check
    // above is scoped, not merely absent everywhere.
    const rail = screen.getByRole("navigation", { name: "Primary" });
    expect(within(rail).getByRole("link", { name: "Runs" })).toBeInTheDocument();
  });

  it("marks exactly the active domain with aria-current=page (Req 9.6)", async () => {
    active = await renderShell({ initialPath: "/runs" });
    const screen = active;
    const rail = within(screen.getByRole("navigation", { name: "Primary" }));

    expect(rail.getByRole("link", { name: "Runs" })).toHaveAttribute(
      "aria-current",
      "page",
    );
    expect(rail.getByRole("link", { name: "Projects" })).not.toHaveAttribute(
      "aria-current",
    );
    expect(rail.getByRole("link", { name: "Plans" })).not.toHaveAttribute(
      "aria-current",
    );
  });

  it("marks the Projects domain active on a projects route (Req 9.6)", async () => {
    active = await renderShell({ initialPath: "/projects" });
    const screen = active;
    const rail = within(screen.getByRole("navigation", { name: "Primary" }));

    expect(rail.getByRole("link", { name: "Projects" })).toHaveAttribute(
      "aria-current",
      "page",
    );
    expect(rail.getByRole("link", { name: "Runs" })).not.toHaveAttribute(
      "aria-current",
    );
  });

  it("marks no domain active on the non-domain root route (Req 9.6)", async () => {
    active = await renderShell({ initialPath: "/" });
    const screen = active;
    const rail = within(screen.getByRole("navigation", { name: "Primary" }));

    for (const name of ["Projects", "Plans", "Runs"] as const) {
      expect(rail.getByRole("link", { name })).not.toHaveAttribute("aria-current");
    }
  });

  it("provides accessible names on rail icon controls and the nav landmark (Req 9.5)", async () => {
    active = await renderShell({ initialPath: "/plans" });
    const screen = active;

    // Nav landmark has a programmatically determinable accessible name.
    const rail = screen.getByRole("navigation", { name: "Primary" });
    expect(rail).toHaveAccessibleName("Primary");

    // Each icon-only rail destination exposes an accessible name (its label),
    // not just an icon glyph.
    for (const name of ["Projects", "Plans", "Runs"] as const) {
      expect(within(rail).getByRole("link", { name })).toHaveAccessibleName(name);
    }

    // The icon-only theme control also carries an accessible name.
    const themeControls = screen.getAllByRole("button", {
      name: /switch to (light|dark) theme/i,
    });
    expect(themeControls[0]).toHaveAccessibleName(/switch to (light|dark) theme/i);
  });
});
