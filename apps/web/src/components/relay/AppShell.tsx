import * as React from "react";
import { Link, useRouterState } from "@tanstack/react-router";
import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import { ArrowRight, Plus } from "lucide-react";

interface AppShellProps {
  children: React.ReactNode;
  className?: string;
}

function isRunsActive(pathname: string): boolean {
  return pathname === "/runs" || pathname.startsWith("/runs/");
}

function isNewRunActive(pathname: string): boolean {
  return pathname === "/runs/new";
}

export function AppShell({ children, className }: AppShellProps) {
  const routerState = useRouterState();
  const pathname = routerState.location.pathname;
  const runsActive = isRunsActive(pathname) && !isNewRunActive(pathname);
  const newRunActive = isNewRunActive(pathname);

  return (
    <div
      className={cn(
        "flex h-screen w-full flex-col bg-[var(--relay-content-bg)] text-foreground",
        className,
      )}
    >
      <header className="flex h-13 shrink-0 items-center border-b border-[var(--relay-topbar-border)] bg-[var(--relay-topbar-bg)] px-4">
        <Link
          to="/runs"
          className="flex min-w-0 items-center gap-2.5 text-[var(--relay-topbar-fg)]"
          aria-label="Relay runs"
        >
          <span className="flex h-6 w-6 items-center justify-center rounded border border-[var(--relay-accent)]/35 bg-[var(--relay-accent)]/10 text-[var(--relay-accent)]">
            <ArrowRight className="h-3.5 w-3.5" />
          </span>
          <span className="text-sm font-semibold leading-none tracking-tight">
            Relay
          </span>
        </Link>

        <span className="mx-2 text-xs text-[var(--relay-topbar-muted-fg)]">
          ·
        </span>

        <span className="hidden font-mono text-xs text-[var(--relay-topbar-muted-fg)] sm:inline">
          v1.0.4-stable
        </span>

        <nav aria-label="Primary" className="ml-auto flex items-center gap-1">
          <Link
            to="/runs"
            aria-current={runsActive ? "page" : undefined}
            className={cn(
              "rounded px-3 py-1.5 text-xs font-medium transition-colors",
              runsActive
                ? "bg-[var(--relay-topbar-active-bg)] text-[var(--relay-topbar-active-fg)]"
                : "text-[var(--relay-topbar-muted-fg)] hover:bg-[var(--relay-topbar-hover-bg)] hover:text-[var(--relay-topbar-fg)]",
            )}
          >
            Runs
          </Link>
        </nav>

        <Button
          asChild
          size="sm"
          className={cn(
            "ml-2 h-8 gap-1.5 bg-[var(--relay-accent)] text-[var(--relay-accent-foreground)] hover:bg-[var(--relay-accent)]/90",
            newRunActive && "ring-1 ring-[var(--relay-accent)]/45",
          )}
        >
          <Link to="/runs/new">
            <Plus className="h-3.5 w-3.5" />
            New Run
          </Link>
        </Button>
      </header>

      <main className="flex min-h-0 flex-1 flex-col overflow-hidden">
        {children}
      </main>
    </div>
  );
}
