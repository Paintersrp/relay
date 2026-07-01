// ============================================================
// Relay Shell — AppShell (redesigned application shell)
// ============================================================
//
// The AppShell composes the redesigned application shell chrome around the
// routed content:
//
//   - the persistent Activity_Rail (left) hosting primary-domain navigation
//     (Projects / Plans / Runs) + the Theme_System control (Req 1.2, 1.3)
//   - a main region containing the Top_Bar (ScopeSwitcher + Global_Search entry
//     + AttentionIndicator, NO primary-domain nav — Req 1.6, 1.7), the
//     Breadcrumb_Trail, and the routed content (`children` — the router Outlet)
//   - the Command_Palette and Global_Search overlays, whose open state this
//     shell owns
//
// AppShell also hosts the global Ctrl/⌘K keyboard listener that toggles the
// Command_Palette (Req 4.1). The workbench next/previous stage shortcuts
// (`[` / `]`) are owned by `useStageShortcuts` within RunWorkbenchLayout
// (task 10.2) and are intentionally NOT duplicated here.
//
// Layout (Req 8.1, 1.1): at >= 1024px the Activity_Rail and the main region
// (Top_Bar + Breadcrumb_Trail + content) render simultaneously via a flex row.
// Below 1024px the Activity_Rail collapses to its own Sheet-backed trigger
// (handled inside ActivityRail).

import * as React from "react";
import { useParams } from "@tanstack/react-router";

import { cn } from "@/lib/utils";
import { ActivityRail } from "@/components/relay/shell/ActivityRail";
import { TopBar } from "@/components/relay/shell/TopBar";
import { BreadcrumbTrail } from "@/components/relay/shell/BreadcrumbTrail";
import { CommandPalette } from "@/components/relay/shell/CommandPalette";
import { GlobalSearch } from "@/components/relay/shell/GlobalSearch";
import { useRunHierarchy } from "@/features/relay-navigation/useShellData";

interface AppShellProps {
  /** Routed content — the TanStack Router `<Outlet />` (wired in task 12.2). */
  children: React.ReactNode;
  className?: string;
}

/**
 * Detect the Ctrl/⌘K command-palette combo. macOS uses ⌘ (metaKey); every other
 * platform uses Ctrl (ctrlKey). Kept pure so the combo check is unit-testable
 * independent of the DOM listener.
 */
function isCommandPaletteCombo(event: KeyboardEvent): boolean {
  return (event.metaKey || event.ctrlKey) && (event.key === "k" || event.key === "K");
}

export function AppShell({ children, className }: AppShellProps) {
  const [commandOpen, setCommandOpen] = React.useState(false);
  const [searchOpen, setSearchOpen] = React.useState(false);

  // Focus restoration (Req 4.7, 9.4). The overlays are opened programmatically
  // (the global Ctrl/⌘K handler and the Top_Bar entry buttons), so there is no
  // Radix `DialogTrigger` for Radix's built-in close-focus to return to — its
  // modal content unconditionally restores focus to a (here absent) trigger,
  // which would otherwise drop focus to <body> on close. We therefore capture
  // the element focused immediately before an overlay opens and restore it when
  // the overlay closes, matching "return focus to the element that was focused
  // before the overlay opened."
  const openerRef = React.useRef<HTMLElement | null>(null);
  // Mirror the palette open state into a ref so the (once-installed) global
  // keydown listener can read the current value without re-subscribing.
  const commandOpenRef = React.useRef(commandOpen);
  commandOpenRef.current = commandOpen;

  const captureOpener = React.useCallback(() => {
    const activeElement = document.activeElement as HTMLElement | null;
    openerRef.current = activeElement;
  }, []);

  const restoreOpener = React.useCallback(() => {
    const element = openerRef.current;
    openerRef.current = null;
    if (element && typeof element.focus === "function" && document.contains(element)) {
      element.focus();
    }
  }, []);

  // Resolve the run-scoped breadcrumb hierarchy from the current route params.
  // `strict: false` lets this read `runId` on run-scoped routes and yields
  // `undefined` elsewhere; `useRunHierarchy` disables its queries when there is
  // no runId, so non-run routes resolve an empty hierarchy and the
  // BreadcrumbTrail renders nothing.
  const params = useParams({ strict: false }) as { runId?: string };
  const { hierarchy } = useRunHierarchy(params.runId);

  // Global Ctrl/⌘K listener toggles the Command_Palette (Req 4.1). preventDefault
  // stops the browser's default behavior for the combo. Stage shortcuts ([ / ])
  // are handled in RunWorkbenchLayout (task 10.2) and are not duplicated here.
  React.useEffect(() => {
    const handleKeyDown = (event: KeyboardEvent) => {
      if (isCommandPaletteCombo(event)) {
        event.preventDefault();
        // Capture the opener at the moment we open (before the overlay mounts
        // and moves focus into itself) so Escape can restore it (Req 4.7, 9.4).
        if (!commandOpenRef.current) {
          captureOpener();
        }
        setCommandOpen((prev) => !prev);
      }
    };
    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, [captureOpener]);

  // Restore focus to the captured opener when either overlay transitions from
  // open to closed (Req 4.7, 9.4).
  const prevCommandOpen = React.useRef(commandOpen);
  React.useEffect(() => {
    if (prevCommandOpen.current && !commandOpen) {
      restoreOpener();
    }
    prevCommandOpen.current = commandOpen;
  }, [commandOpen, restoreOpener]);

  const prevSearchOpen = React.useRef(searchOpen);
  React.useEffect(() => {
    if (prevSearchOpen.current && !searchOpen) {
      restoreOpener();
    }
    prevSearchOpen.current = searchOpen;
  }, [searchOpen, restoreOpener]);

  const openSearch = React.useCallback(() => {
    captureOpener();
    setSearchOpen(true);
  }, [captureOpener]);
  const openCommand = React.useCallback(() => {
    captureOpener();
    setCommandOpen(true);
  }, [captureOpener]);

  return (
    <div
      className={cn(
        "flex h-dvh w-full overflow-hidden bg-[var(--relay-content-bg)] text-foreground",
        className,
      )}
    >
      {/* Persistent Activity_Rail (>= 1024px) / collapsed trigger (< 1024px). */}
      <ActivityRail />

      {/* Main region: Top_Bar + Breadcrumb_Trail + routed content. */}
      <div className="flex min-w-0 flex-1 flex-col overflow-hidden">
        <header className="flex h-14 min-w-0 shrink-0 items-center gap-2 overflow-hidden border-b border-[var(--relay-topbar-border)] bg-[var(--relay-topbar-bg)] px-3 sm:px-4">
          <TopBar
            onOpenSearch={openSearch}
            onOpenCommand={openCommand}
            className="flex-1"
          />
        </header>

        {/* Breadcrumb_Trail — renders nothing when there is no hierarchy. */}
        <BreadcrumbTrail
          resolved={hierarchy}
          className="shrink-0 border-b border-[var(--relay-topbar-border)] bg-[var(--relay-content-bg)] px-4 py-2"
        />

        <main className="flex min-h-0 flex-1 flex-col overflow-hidden">
          {children}
        </main>
      </div>

      {/* Shell-owned overlays (open state hosted here). */}
      <CommandPalette open={commandOpen} onOpenChange={setCommandOpen} />
      <GlobalSearch open={searchOpen} onOpenChange={setSearchOpen} />
    </div>
  );
}
