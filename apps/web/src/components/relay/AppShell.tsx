import * as React from "react";
import { Link, useRouterState } from "@tanstack/react-router";
import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import { List, Plus, Zap } from "lucide-react";

type ShellNavItemConfig = {
  label: string;
  href: string;
  icon: React.ComponentType<{ className?: string }>;
};

function isActivePath(pathname: string, href: string): boolean {
  if (href === "/runs") {
    return pathname === "/runs" || pathname.startsWith("/runs/");
  }
  return pathname === href || pathname.startsWith(`${href}/`);
}

const primaryNav: ShellNavItemConfig[] = [
  { label: "Runs", href: "/runs", icon: List },
];

interface AppShellProps {
  children: React.ReactNode;
  className?: string;
}

function ShellNavItem({
  item,
  pathname,
}: {
  item: ShellNavItemConfig;
  pathname: string;
}) {
  const Icon = item.icon;
  const isActive = isActivePath(pathname, item.href);
  const baseClass =
    "relative flex h-9 items-center gap-2.5 rounded-md px-2.5 text-sm font-medium transition-colors";

  return (
    <Link
      to={item.href}
      aria-current={isActive ? "page" : undefined}
      className={cn(
        baseClass,
        isActive
          ? "bg-[var(--relay-sidebar-active-bg)] text-[var(--relay-sidebar-active-fg)] before:absolute before:left-0 before:top-1.5 before:h-6 before:w-0.5 before:rounded-full before:bg-[var(--relay-sidebar-active-border)]"
          : "text-muted-foreground hover:bg-[var(--relay-sidebar-hover-bg)] hover:text-foreground",
      )}
    >
      <Icon className="h-4 w-4 shrink-0" />
      <span className="truncate">{item.label}</span>
    </Link>
  );
}

export function AppShell({ children, className }: AppShellProps) {
  const routerState = useRouterState();
  const pathname = routerState.location.pathname;

  return (
    <div
      className={cn(
        "flex h-screen w-full bg-[var(--relay-content-bg)] text-foreground",
        className,
      )}
    >
      {/* Left rail */}
      <aside className="flex h-full w-[11.75rem] shrink-0 flex-col border-r border-[var(--relay-border)] bg-[var(--relay-sidebar-bg)]">
        {/* Brand block */}
        <div className="flex h-16 shrink-0 items-center gap-2.5 px-4">
          <div className="flex h-7 w-7 items-center justify-center rounded-md bg-[var(--relay-accent)] text-[var(--relay-accent-foreground)]">
            <Zap className="h-4 w-4" />
          </div>
          <div className="min-w-0">
            <div className="text-sm font-semibold leading-none tracking-tight">
              Relay
            </div>
            <div className="mt-1 font-mono text-[0.625rem] leading-none text-[var(--relay-sidebar-section-fg)]">
              v1.0.4-stable
            </div>
          </div>
        </div>

        <div className="px-3 pb-4 shrink-0">
          <Button
            asChild
            size="sm"
            className="h-8 w-full justify-start gap-2 bg-[var(--relay-accent)] text-[var(--relay-accent-foreground)] shadow-sm hover:bg-[var(--relay-accent)]/90"
          >
            <Link to="/runs/new">
              <Plus className="h-3.5 w-3.5" />
              New Run
            </Link>
          </Button>
        </div>

        {/* Primary Nav */}
        <div className="flex-1 overflow-y-auto px-2">
          <nav aria-label="Primary" className="flex flex-col gap-1">
            {primaryNav.map((item) => (
              <ShellNavItem key={item.label} item={item} pathname={pathname} />
            ))}
          </nav>
        </div>
      </aside>

      {/* Main content */}
      <main className="flex min-w-0 flex-1 flex-col overflow-hidden">
        {children}
      </main>
    </div>
  );
}
