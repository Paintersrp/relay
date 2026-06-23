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

function isPlansActive(pathname: string): boolean {
  return pathname === "/plans" || pathname.startsWith("/plans/");
}

function isProjectsActive(pathname: string): boolean {
  return pathname === "/projects" || pathname.startsWith("/projects/");
}

function isNewRunActive(pathname: string): boolean {
  return pathname === "/runs/new";
}

export function AppShell({ children, className }: AppShellProps) {
  const routerState = useRouterState();
  const pathname = routerState.location.pathname;
  const plansActive = isPlansActive(pathname);
  const runsActive = isRunsActive(pathname) && !isNewRunActive(pathname);
  const newRunActive = isNewRunActive(pathname);
  const projectsActive = isProjectsActive(pathname);

  return (
    <div
      className={cn(
        "flex h-dvh w-full flex-col overflow-hidden bg-[var(--relay-content-bg)] text-foreground",
        className,
      )}
    >
      <header className="flex h-14 min-w-0 shrink-0 items-center gap-2 overflow-hidden border-b border-[var(--relay-topbar-border)] bg-[var(--relay-topbar-bg)] px-3 sm:px-4">
        <Link
          to="/runs"
          className="flex min-w-0 items-center gap-2.5 text-[var(--relay-topbar-fg)]"
          aria-label="Relay runs"
        >
          <span className="flex h-7 w-7 items-center justify-center rounded border border-[var(--relay-accent)]/35 bg-[var(--relay-accent)]/10 text-[var(--relay-accent)]">
            <ArrowRight className="h-4 w-4" />
          </span>
          <span className="truncate text-[15px] font-semibold leading-none tracking-tight">
            Relay
          </span>
        </Link>

        <span className="hidden text-xs text-[var(--relay-topbar-muted-fg)] sm:inline">
          ·
        </span>

        <span className="hidden font-mono text-[13px] text-[var(--relay-topbar-muted-fg)] sm:inline">
          v1.0.4-stable
        </span>

        <nav aria-label="Primary" className="ml-auto flex shrink-0 items-center gap-1">
          <Link
            to="/projects"
            aria-current={projectsActive ? "page" : undefined}
            className={cn(
              "rounded px-3 py-1.5 text-[13px] font-medium transition-colors",
              projectsActive
                ? "bg-[var(--relay-topbar-active-bg)] text-[var(--relay-topbar-active-fg)]"
                : "text-[var(--relay-topbar-muted-fg)] hover:bg-[var(--relay-topbar-hover-bg)] hover:text-[var(--relay-topbar-fg)]",
            )}
          >
            Projects
          </Link>
          <Link
            to="/plans"
            aria-current={plansActive ? "page" : undefined}
            className={cn(
              "rounded px-3 py-1.5 text-[13px] font-medium transition-colors",
              plansActive
                ? "bg-[var(--relay-topbar-active-bg)] text-[var(--relay-topbar-active-fg)]"
                : "text-[var(--relay-topbar-muted-fg)] hover:bg-[var(--relay-topbar-hover-bg)] hover:text-[var(--relay-topbar-fg)]",
            )}
          >
            Plans
          </Link>
          <Link
            to="/runs"
            aria-current={runsActive ? "page" : undefined}
            className={cn(
              "rounded px-3 py-1.5 text-[13px] font-medium transition-colors",
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
          variant="outline"
          size="sm"
          className="h-8 shrink-0 gap-1.5 px-2 text-[var(--relay-topbar-fg)] sm:px-3"
        >
          <Link to="/plans/new" aria-label="New Plan">
            <Plus className="h-3.5 w-3.5" />
            <span className="hidden sm:inline">New Plan</span>
          </Link>
        </Button>

        <Button
          asChild
          size="sm"
          className={cn(
            "h-8 shrink-0 gap-1.5 px-2 sm:px-3 bg-[var(--relay-accent)] text-[var(--relay-accent-foreground)] hover:bg-[var(--relay-accent)]/90",
            newRunActive && "ring-1 ring-[var(--relay-accent)]/45",
          )}
        >
          <Link
            to="/runs/new"
            search={{ planId: undefined, passId: undefined }}
            aria-label="New Run"
          >
            <Plus className="h-3.5 w-3.5" />
            <span className="hidden sm:inline">New Run</span>
          </Link>
        </Button>
      </header>

      <main className="flex min-h-0 flex-1 flex-col overflow-hidden">
        {children}
      </main>
    </div>
  );
}
