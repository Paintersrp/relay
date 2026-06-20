import * as React from "react";
import { Link, useRouterState } from "@tanstack/react-router";
import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import {
  BookOpen,
  FolderKanban,
  HelpCircle,
  List,
  Plus,
  Settings,
  Zap,
} from "lucide-react";

function isActivePath(pathname: string, href: string): boolean {
  if (href === "/runs") {
    return pathname === "/runs" || pathname.startsWith("/runs/");
  }
  return pathname === href || pathname.startsWith(`${href}/`);
}

const primaryNav = [
  { label: "Runs", href: "/runs", icon: List },
  { label: "Projects", href: "/projects", icon: FolderKanban, disabled: true },
  { label: "Settings", href: "/settings", icon: Settings, disabled: true },
];

const utilityNav = [
  { label: "Docs", href: "/docs", icon: BookOpen, disabled: true },
  { label: "Support", href: "/support", icon: HelpCircle, disabled: true },
];

interface AppShellProps {
  children: React.ReactNode;
  className?: string;
}

export function AppShell({ children, className }: AppShellProps) {
  const routerState = useRouterState();
  const pathname = routerState.location.pathname;

  return (
    <div className={cn("flex h-screen w-full bg-[var(--relay-content-bg)] text-foreground", className)}>
      {/* Left rail */}
      <aside className="w-[11.75rem] shrink-0 border-r border-[var(--relay-border)] bg-[var(--relay-sidebar-bg)] flex flex-col h-full">
        {/* Brand block */}
        <div className="h-14 flex items-center px-4 shrink-0 gap-2 font-semibold">
          <Zap className="w-4 h-4 text-[var(--relay-accent)]" />
          <span className="tracking-tight leading-none pt-0.5">Relay</span>
          <span className="text-muted-foreground font-normal text-[0.65rem] tracking-wider uppercase ml-1 mt-0.5">
            v1.0.4-stable
          </span>
        </div>

        <div className="px-3 pb-4 shrink-0">
          <Button asChild size="sm" className="w-full justify-start h-8 gap-2 bg-[var(--relay-accent)] hover:bg-[var(--relay-accent)]/90 text-[var(--relay-accent-foreground)] shadow-sm">
            <Link to="/runs/new">
              <Plus className="w-3.5 h-3.5" />
              New Run
            </Link>
          </Button>
        </div>

        {/* Primary Nav */}
        <div className="flex-1 overflow-y-auto px-2">
          <nav className="flex flex-col gap-1">
            {primaryNav.map((item) => {
              const Icon = item.icon;
              const isActive = !item.disabled && isActivePath(pathname, item.href);

              if (item.disabled) {
                return (
                  <span
                    key={item.label}
                    className="flex items-center gap-2.5 px-2.5 py-2 text-sm font-medium rounded-md text-muted-foreground opacity-60 cursor-not-allowed select-none"
                  >
                    <Icon className="w-4 h-4" />
                    {item.label}
                  </span>
                );
              }

              return (
                <Link
                  key={item.label}
                  to={item.href}
                  className={cn(
                    "flex items-center gap-2.5 px-2.5 py-2 text-sm font-medium rounded-md transition-colors",
                    isActive
                      ? "bg-[var(--relay-panel-bg)] text-foreground"
                      : "text-muted-foreground hover:bg-[var(--relay-panel-hover-bg)] hover:text-foreground"
                  )}
                >
                  <Icon className="w-4 h-4" />
                  {item.label}
                </Link>
              );
            })}
          </nav>
        </div>

        {/* Utility Nav */}
        <div className="p-2 shrink-0 border-t border-[var(--relay-border)]/50 mt-auto">
          <nav className="flex flex-col gap-1">
            {utilityNav.map((item) => {
              const Icon = item.icon;
              // All utility nav items are currently disabled based on requirements
              return (
                <span
                  key={item.label}
                  className="flex items-center gap-2.5 px-2.5 py-2 text-sm font-medium rounded-md text-muted-foreground opacity-60 cursor-not-allowed select-none"
                >
                  <Icon className="w-4 h-4" />
                  {item.label}
                </span>
              );
            })}
          </nav>
        </div>
      </aside>

      {/* Main content */}
      <main className="min-w-0 flex-1 overflow-hidden flex flex-col">
        {children}
      </main>
    </div>
  );
}
