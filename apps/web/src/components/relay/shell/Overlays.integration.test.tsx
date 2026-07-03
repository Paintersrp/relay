// @vitest-environment jsdom
//
// ============================================================
// Integration — Overlay behavior + focus management (task 13.2)
// ============================================================
//
// Exercises the shell-owned overlays (Command_Palette + Global_Search) through
// the shared DOM harness in `src/test/shell-test-utils.tsx`, asserting the
// keyboard-first / focus-management contract and the Global_Search query
// behaviors:
//
//   Command_Palette
//     - Opens on Ctrl+K and on ⌘K (global keydown handler in AppShell) (Req 4.1)
//     - Selecting an entry executes its navigation AND closes the palette (Req 4.6)
//     - Escape closes the palette and restores focus to the opener       (Req 4.7, 9.4)
//     - Focus is moved into (and trapped within) the overlay             (Req 9.3)
//
//   Global_Search
//     - Input accepts up to 256 characters (maxLength)                   (Req 5.1)
//     - Focus is moved into the overlay                                  (Req 9.3)
//     - Underlying corpus-query failure → error state, query retained    (Req 5.6)
//     - No matching entities → no-results state, query retained          (Req 5.5)
//
// Only DOM presence, focus, and programmatically determinable state are
// asserted here — layout/contrast concerns are out of scope (see task 13.2).

import { describe, it, expect, afterEach, beforeEach, beforeAll, vi } from "vitest";
import { within, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import {
  renderShell,
  type ApiStubFixtures,
  type RenderShellResult,
} from "@/test/shell-test-utils";

let active: RenderShellResult | null = null;

// ------------------------------------------------------------
// jsdom shims required by the overlay primitives (Radix Dialog + cmdk)
// ------------------------------------------------------------
//
// jsdom implements neither `scrollIntoView` (cmdk scrolls the active item into
// view on selection) nor `ResizeObserver` / pointer-capture (used by Radix's
// dialog dismiss layer). Provide inert shims so the overlays can mount and
// operate without throwing. These are test-only environment shims and touch no
// production code.
beforeAll(() => {
  const proto = Element.prototype as unknown as Record<string, unknown>;
  if (!proto.scrollIntoView) {
    proto.scrollIntoView = vi.fn();
  }
  if (!proto.hasPointerCapture) {
    proto.hasPointerCapture = vi.fn(() => false);
    proto.setPointerCapture = vi.fn();
    proto.releasePointerCapture = vi.fn();
  }
  if (!globalThis.ResizeObserver) {
    globalThis.ResizeObserver = class {
      observe() {}
      unobserve() {}
      disconnect() {}
    } as unknown as typeof ResizeObserver;
  }
});

beforeEach(() => {
  // Keep act() noise from async query settling out of the test output, matching
  // the task 13.1 harness convention.
  vi.spyOn(console, "error").mockImplementation(() => {});
});

afterEach(() => {
  active?.restore();
  active = null;
  vi.restoreAllMocks();
});

/** The Global_Search entry button lives in the Top_Bar and opens the overlay. */
function getSearchTrigger(result: RenderShellResult): HTMLElement {
  const header = result.container.querySelector("header");
  if (!header) throw new Error("Top_Bar <header> not found in shell");
  return within(header).getByRole("button", {
    name: /search projects, plans, passes, and runs/i,
  });
}

describe("Command_Palette overlay (task 13.2)", () => {
  it("opens on Ctrl+K (Req 4.1)", async () => {
    const user = userEvent.setup();
    active = await renderShell({ initialPath: "/" });
    const screen = active;

    expect(screen.queryByRole("dialog")).toBeNull();

    await user.keyboard("{Control>}k{/Control}");

    expect(
      await screen.findByRole("dialog", { name: /command palette/i }),
    ).toBeInTheDocument();
  });

  it("opens on ⌘K / metaKey (Req 4.1)", async () => {
    const user = userEvent.setup();
    active = await renderShell({ initialPath: "/" });
    const screen = active;

    expect(screen.queryByRole("dialog")).toBeNull();

    await user.keyboard("{Meta>}k{/Meta}");

    expect(
      await screen.findByRole("dialog", { name: /command palette/i }),
    ).toBeInTheDocument();
  });

  it("moves focus into the overlay and traps it there when open (Req 9.3)", async () => {
    const user = userEvent.setup();
    active = await renderShell({ initialPath: "/" });
    const screen = active;

    await user.keyboard("{Control>}k{/Control}");
    const dialog = await screen.findByRole("dialog", { name: /command palette/i });

    // Focus was moved into the overlay (Radix FocusScope + cmdk input autofocus).
    await waitFor(() => {
      expect(dialog.contains(document.activeElement)).toBe(true);
    });

    // Tab / Shift+Tab keep focus inside the overlay (focus trap).
    await user.tab();
    expect(dialog.contains(document.activeElement)).toBe(true);
    await user.tab({ shift: true });
    expect(dialog.contains(document.activeElement)).toBe(true);
  });

  it("executes an entry's navigation and closes the palette on selection (Req 4.6)", async () => {
    const user = userEvent.setup();
    active = await renderShell({ initialPath: "/" });
    const screen = active;

    await user.keyboard("{Control>}k{/Control}");
    const dialog = await screen.findByRole("dialog", { name: /command palette/i });

    // The Runs primary-domain nav entry is always present (Req 4.2).
    const runsEntry = within(dialog).getByRole("option", { name: "Runs" });
    await user.click(runsEntry);

    // The palette closes …
    await waitFor(() => {
      expect(screen.queryByRole("dialog", { name: /command palette/i })).toBeNull();
    });
    // … and the navigation executed (route changed to /runs).
    await waitFor(() => {
      expect(active!.router.state.location.pathname).toBe("/runs");
    });
  });

  it("closes on Escape and restores focus to the element focused before opening (Req 4.7, 9.4)", async () => {
    const user = userEvent.setup();
    active = await renderShell({ initialPath: "/" });
    const screen = active;

    // Focus a known opener element (the Top_Bar search trigger) before opening.
    const searchTrigger = getSearchTrigger(screen);
    searchTrigger.focus();
    expect(searchTrigger).toHaveFocus();

    await user.keyboard("{Control>}k{/Control}");
    const dialog = await screen.findByRole("dialog", { name: /command palette/i });
    await waitFor(() => {
      expect(dialog.contains(document.activeElement)).toBe(true);
    });

    await user.keyboard("{Escape}");

    // Overlay closed …
    await waitFor(() => {
      expect(screen.queryByRole("dialog", { name: /command palette/i })).toBeNull();
    });
    // … and focus is restored to the previously focused opener (Req 4.7, 9.4).
    await waitFor(() => {
      expect(searchTrigger).toHaveFocus();
    });
  });
});

describe("Global_Search overlay (task 13.2)", () => {
  it("presents a search input that accepts up to 256 characters (Req 5.1)", async () => {
    const user = userEvent.setup();
    active = await renderShell({ initialPath: "/" });
    const screen = active;

    await user.click(getSearchTrigger(screen));
    const dialog = await screen.findByRole("dialog", { name: /global search/i });

    const input = within(dialog).getByRole("searchbox");
    expect(input).toHaveAttribute("maxlength", "256");
  });

  it("moves focus into the overlay when opened (Req 9.3)", async () => {
    const user = userEvent.setup();
    active = await renderShell({ initialPath: "/" });
    const screen = active;

    await user.click(getSearchTrigger(screen));
    const dialog = await screen.findByRole("dialog", { name: /global search/i });

    await waitFor(() => {
      expect(dialog.contains(document.activeElement)).toBe(true);
    });
  });

  it("renders an error state and retains the query when the corpus queries fail (Req 5.6)", async () => {
    const user = userEvent.setup();

    // Simulate the underlying entity corpus failing to load: the `/api/runs`
    // list request rejects, which makes the composed search corpus queries
    // error (GlobalSearch aggregates runs/plans/projects query errors).
    const fixtures: ApiStubFixtures = {
      onRequest: (pathname) => {
        if (pathname === "/api/runs") {
          throw new Error("simulated network failure");
        }
        return undefined;
      },
    };

    active = await renderShell({ initialPath: "/", fixtures });
    const screen = active;

    await user.click(getSearchTrigger(screen));
    const dialog = await screen.findByRole("dialog", { name: /global search/i });

    const input = within(dialog).getByRole("searchbox");
    await user.type(input, "test");

    // The error state surfaces (distinct from empty / no-results) …
    expect(await within(dialog).findByText(/search failed to load/i)).toBeInTheDocument();
    // … and a retry affordance is offered (Req 5.6).
    expect(within(dialog).getByRole("button", { name: /retry/i })).toBeInTheDocument();
    // … and the submitted query is retained for retry.
    expect(input).toHaveValue("test");
  });

  it("renders a no-results state and retains the query when nothing matches (Req 5.5)", async () => {
    const user = userEvent.setup();

    // Empty corpus (default fixtures) → any >= 2-char query yields no matches.
    active = await renderShell({ initialPath: "/" });
    const screen = active;

    await user.click(getSearchTrigger(screen));
    const dialog = await screen.findByRole("dialog", { name: /global search/i });

    const input = within(dialog).getByRole("searchbox");
    await user.type(input, "zzz");

    expect(
      await within(dialog).findByText(/no matching entities/i),
    ).toBeInTheDocument();
    // The submitted query is retained (Req 5.5).
    expect(input).toHaveValue("zzz");
  });
});
