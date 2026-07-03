import * as React from "react";
import { Moon, Sun } from "lucide-react";

import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";

// ============================================================
// ThemeToggle — persistent Theme_System control entry
// ============================================================
//
// The Activity_Rail must expose a Theme_System control entry point that stays
// visible and selectable on every authenticated workflow route (Requirement
// 1.3) and carries a programmatically determinable accessible name
// (Requirement 9.5).
//
// Relay ships a single Tokyo Night dark palette (see `styles.css`), where the
// dark tokens live on `:root`. The `.dark` class only sets `color-scheme`, so
// toggling it keeps the Tokyo Night palette active (Requirement 7.2) while
// giving the operator a real, keyboard-operable control rather than an inert
// placeholder. The preference is persisted to `localStorage`.

const STORAGE_KEY = "relay-theme";

type ThemeMode = "dark" | "light";

function applyTheme(mode: ThemeMode): void {
  if (typeof document === "undefined") return;
  document.documentElement.classList.toggle("dark", mode === "dark");
}

function readStoredMode(): ThemeMode {
  if (typeof window === "undefined") return "dark";
  try {
    return window.localStorage.getItem(STORAGE_KEY) === "light" ? "light" : "dark";
  } catch {
    return "dark";
  }
}

export interface ThemeToggleProps {
  /** When true, render an inline text label beside the icon (used in the mobile sheet). */
  showLabel?: boolean;
  className?: string;
}

export function ThemeToggle({ showLabel = false, className }: ThemeToggleProps) {
  // Default to dark on the server and first client render to match the
  // hard-coded `className="dark"` on <html>; reconcile with any stored
  // preference after mount to stay SSR-safe.
  const [mode, setMode] = React.useState<ThemeMode>("dark");

  React.useEffect(() => {
    const stored = readStoredMode();
    setMode(stored);
    applyTheme(stored);
  }, []);

  const toggle = React.useCallback(() => {
    setMode((prev) => {
      const next: ThemeMode = prev === "dark" ? "light" : "dark";
      applyTheme(next);
      try {
        window.localStorage.setItem(STORAGE_KEY, next);
      } catch {
        // Persistence is best-effort; ignore storage failures.
      }
      return next;
    });
  }, []);

  const label = mode === "dark" ? "Switch to light theme" : "Switch to dark theme";

  return (
    <Button
      type="button"
      variant="ghost"
      size={showLabel ? "sm" : "icon"}
      onClick={toggle}
      aria-label={label}
      title={label}
      className={cn(
        "text-sidebar-foreground/70 hover:bg-sidebar-accent hover:text-sidebar-foreground",
        showLabel && "w-full justify-start gap-3",
        className,
      )}
    >
      {mode === "dark" ? (
        <Moon className="size-5" aria-hidden="true" />
      ) : (
        <Sun className="size-5" aria-hidden="true" />
      )}
      {showLabel ? <span>Theme</span> : null}
    </Button>
  );
}
