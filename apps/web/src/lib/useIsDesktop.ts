import * as React from "react";

/**
 * Desktop breakpoint in CSS pixels. Matches the project's responsive boundary
 * (Tailwind's `lg` breakpoint) used across the shell redesign (Requirement 8).
 */
export const DESKTOP_BREAKPOINT = 1024;

const DESKTOP_MEDIA_QUERY = `(min-width: ${DESKTOP_BREAKPOINT}px)`;

function supportsMatchMedia(): boolean {
  return typeof window !== "undefined" && typeof window.matchMedia === "function";
}

function subscribe(onChange: () => void): () => void {
  if (!supportsMatchMedia()) {
    return () => {};
  }
  const mql = window.matchMedia(DESKTOP_MEDIA_QUERY);
  mql.addEventListener("change", onChange);
  return () => mql.removeEventListener("change", onChange);
}

function getSnapshot(): boolean {
  if (!supportsMatchMedia()) {
    return true;
  }
  return window.matchMedia(DESKTOP_MEDIA_QUERY).matches;
}

// During SSR (and environments without matchMedia) assume desktop so the
// default split-pane layout renders. useSyncExternalStore reconciles to the
// real viewport value immediately after hydration.
function getServerSnapshot(): boolean {
  return true;
}

/**
 * Returns whether the viewport width is at or above the desktop breakpoint
 * (1024 CSS pixels). Below the breakpoint this returns `false`, allowing
 * callers to switch from a side-by-side layout to a stacked one.
 */
export function useIsDesktop(): boolean {
  return React.useSyncExternalStore(subscribe, getSnapshot, getServerSnapshot);
}
