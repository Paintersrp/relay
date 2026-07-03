// ============================================================
// Relay Shell — TopBar (global context region)
// ============================================================
//
// The horizontal Top_Bar region of the redesigned application shell. It hosts
// global context only:
//
//   - the ScopeSwitcher (active Project/Plan scope)          (Req 1.6)
//   - a Global_Search entry button that opens the overlay    (Req 1.6)
//   - an AttentionIndicator summarizing items needing review (Req 1.6)
//
// The TopBar remains visible on every authenticated workflow route (Req 1.6)
// and MUST NOT contain any primary-domain navigation (Projects / Plans / Runs);
// that navigation lives exclusively in the Activity_Rail (Req 1.7). This file
// intentionally imports no domain navigation links.
//
// The overlays themselves (Global_Search, Command_Palette) are hosted by the
// AppShell (task 12.1), which owns their open state and the global Ctrl/⌘K
// keyboard listener. The TopBar only triggers them through the injected
// `onOpenSearch` / `onOpenCommand` callbacks, so it stays presentational and
// decoupled from overlay state.

import * as React from "react";
import { Link } from "@tanstack/react-router";
import { Bell, Search } from "lucide-react";

import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import { ScopeSwitcher } from "./ScopeSwitcher";
import { useShellData } from "@/features/relay-navigation/useShellData";

// ------------------------------------------------------------
// Keyboard-hint helpers
// ------------------------------------------------------------

/**
 * The visible keyboard-shortcut hint for the Command_Palette entry point. macOS
 * uses the ⌘ (Command) symbol; every other platform uses "Ctrl" (Req 4.1). Kept
 * pure and exported so it is unit-testable independent of rendering.
 */
export function shortcutHintLabel(isMac: boolean): string {
  return isMac ? "⌘K" : "Ctrl K";
}

/**
 * Detect whether the current platform is macOS, SSR-safe. Defaults to `false`
 * on the server and the first client render (matching a Ctrl-based hint) and
 * reconciles to the real platform after mount, so it never causes a hydration
 * mismatch.
 */
function useIsMac(): boolean {
  const [isMac, setIsMac] = React.useState(false);

  React.useEffect(() => {
    if (typeof navigator === "undefined") return;
    const platform =
      // `userAgentData` is not yet in the TS DOM lib; fall back to `platform`.
      (navigator as Navigator & { userAgentData?: { platform?: string } })
        .userAgentData?.platform ?? navigator.platform;
    setIsMac(/mac/i.test(platform ?? ""));
  }, []);

  return isMac;
}

// ------------------------------------------------------------
// AttentionIndicator
// ------------------------------------------------------------

export interface AttentionIndicatorProps {
  /** Total number of items currently needing attention (Home_Overview total). */
  count: number;
  className?: string;
}

/**
 * A compact Shell-level indicator summarizing the items that need the
 * operator's attention (Req 1.6). It links to the Home_Overview at `/`, where
 * the full attention list lives, and surfaces the current attention total as a
 * count badge. The control is always rendered so it stays visible on every
 * authenticated workflow route; when nothing needs attention it renders without
 * a badge and announces the empty state through its accessible name.
 */
export function AttentionIndicator({ count, className }: AttentionIndicatorProps) {
  const hasAttention = count > 0;
  // Cap the visible count so the badge stays compact; the exact total remains
  // available on the Home_Overview.
  const displayCount = count > 99 ? "99+" : String(count);
  const label = hasAttention
    ? `${count} item${count === 1 ? "" : "s"} need attention`
    : "No items need attention";

  return (
    <Button
      asChild
      variant="ghost"
      size="icon"
      aria-label={label}
      title={label}
      className={cn("relative", className)}
    >
      <Link to="/">
        <Bell className="size-5" aria-hidden="true" />
        {hasAttention ? (
          <span
            aria-hidden="true"
            data-testid="attention-count"
            className="absolute -right-0.5 -top-0.5 flex h-4 min-w-4 items-center justify-center rounded-full bg-destructive px-1 text-[10px] font-semibold leading-none text-destructive-foreground"
          >
            {displayCount}
          </span>
        ) : null}
      </Link>
    </Button>
  );
}

// ------------------------------------------------------------
// TopBar
// ------------------------------------------------------------

export interface TopBarProps {
  /** Open the Global_Search overlay (state hosted by AppShell). */
  onOpenSearch: () => void;
  /**
   * Open the Command_Palette overlay (state hosted by AppShell). Optional — when
   * omitted, the ⌘K/Ctrl+K command hint is not rendered.
   */
  onOpenCommand?: () => void;
  className?: string;
}

/**
 * The Top_Bar global-context region (Req 1.6, 1.7). Hosts the ScopeSwitcher, a
 * Global_Search entry button, and the AttentionIndicator. It contains no
 * primary-domain navigation — those destinations live only in the
 * Activity_Rail (Req 1.7).
 */
export function TopBar({ onOpenSearch, onOpenCommand, className }: TopBarProps) {
  const isMac = useIsMac();
  const { attention } = useShellData();

  return (
    <div
      className={cn(
        "flex min-w-0 items-center gap-2 sm:gap-3",
        className,
      )}
    >
      {/* Active scope (Project / Plan) — Req 1.6 */}
      <ScopeSwitcher />

      {/* Global_Search entry — opens the overlay hosted by AppShell (Req 1.6). */}
      <Button
        type="button"
        variant="outline"
        size="sm"
        onClick={onOpenSearch}
        aria-label="Search projects, plans, passes, and runs"
        aria-haspopup="dialog"
        className="min-w-0 gap-2 text-muted-foreground"
      >
        <Search className="size-4 shrink-0" aria-hidden="true" />
        <span className="hidden sm:inline">Search</span>
        {onOpenCommand ? (
          <kbd
            aria-hidden="true"
            className="ml-1 hidden rounded border border-border bg-muted px-1.5 font-mono text-[10px] font-medium text-muted-foreground sm:inline"
          >
            {shortcutHintLabel(isMac)}
          </kbd>
        ) : null}
      </Button>

      <div className="ml-auto flex items-center gap-1 sm:gap-2">
        {/* Command_Palette entry (keyboard-first affordance) — Req 4.1. */}
        {onOpenCommand ? (
          <Button
            type="button"
            variant="ghost"
            size="sm"
            onClick={onOpenCommand}
            aria-label="Open command palette"
            aria-haspopup="dialog"
            className="hidden font-mono text-xs text-muted-foreground lg:inline-flex"
          >
            {shortcutHintLabel(isMac)}
          </Button>
        ) : null}

        {/* Attention indicator → Home_Overview (Req 1.6). */}
        <AttentionIndicator count={attention.totalCount} />
      </div>
    </div>
  );
}
