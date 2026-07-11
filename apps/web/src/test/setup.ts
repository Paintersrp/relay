// ============================================================
// Shared Vitest setup — DOM test harness (wave-8 integration tests)
// ============================================================
//
// This setup file is wired into Vitest via `test.setupFiles` in
// `vite.config.ts` and runs before every test file. It is intentionally
// lightweight and safe under BOTH the default `node` environment (pure-logic
// tests) and the `jsdom` environment (component/integration tests):
//
//   - It registers `@testing-library/jest-dom`'s custom matchers
//     (`toBeInTheDocument`, `toHaveAccessibleName`, `toHaveAttribute`, …) onto
//     Vitest's `expect`. Registering matchers does not require a DOM, so this
//     is harmless for node-environment tests.
//   - When a DOM is present (jsdom-environment files, opted in via a
//     `// @vitest-environment jsdom` docblock at the top of the test file), it
//     installs a `matchMedia` shim and auto-cleans the React Testing Library
//     render tree after each test to keep tests isolated.
//
// Integration test files opt into the DOM environment per-file with:
//
//     // @vitest-environment jsdom
//
// so the global default environment stays `node` and existing pure-logic tests
// are unaffected.

import { afterEach, vi } from "vitest";

// Register jest-dom matchers on Vitest's expect. Safe in node and jsdom.
import "@testing-library/jest-dom/vitest";

// Only touch DOM-specific globals when a DOM is actually present. Under the
// default node environment `window` is undefined and this block is skipped.
if (typeof window !== "undefined") {
  // jsdom does not implement `matchMedia`; several shell components read it
  // (responsive breakpoint hooks, theme control). Provide a permissive shim so
  // components can mount. Individual tests may override this with `vi.fn`
  // implementations to assert breakpoint-specific behavior.
  if (!window.matchMedia) {
    Object.defineProperty(window, "matchMedia", {
      writable: true,
      configurable: true,
      value: (query: string): MediaQueryList => ({
        matches: false,
        media: query,
        onchange: null,
        addEventListener: () => {},
        removeEventListener: () => {},
        addListener: () => {},
        removeListener: () => {},
        dispatchEvent: () => false,
      }),
    });
  }

  // jsdom does not implement the Pointer Events capture APIs or
  // `scrollIntoView`. Radix UI's Select (and other popover-based primitives)
  // call these during pointer interaction; without a shim, user-event clicks
  // on Select triggers/options throw inside jsdom. Provide permissive no-op
  // implementations so these components can be exercised under jsdom.
  if (!Element.prototype.hasPointerCapture) {
    Element.prototype.hasPointerCapture = () => false;
  }
  if (!Element.prototype.setPointerCapture) {
    Element.prototype.setPointerCapture = () => {};
  }
  if (!Element.prototype.releasePointerCapture) {
    Element.prototype.releasePointerCapture = () => {};
  }
  if (!Element.prototype.scrollIntoView) {
    Element.prototype.scrollIntoView = () => {};
  }

  // Auto-unmount React trees rendered by @testing-library/react after each
  // test so DOM state never leaks between tests. Imported lazily so node-only
  // test runs never load the React Testing Library.
  afterEach(async () => {
    const { cleanup } = await import("@testing-library/react");
    cleanup();
  });
}

// Keep a reference so `vi` is always considered used even if the DOM block is
// skipped; avoids an unused-import lint under node-only runs.
void vi;
