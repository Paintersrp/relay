import * as React from "react";
import { Link, useRouterState } from "@tanstack/react-router";
import { ClipboardList, FolderGit2, Menu, Play } from "lucide-react";

import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import {
  Sheet,
  SheetClose,
  SheetContent,
  SheetHeader,
  SheetTitle,
  SheetTrigger,
} from "@/components/ui/sheet";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import {
  PRIMARY_DOMAINS,
  resolveActiveDomain,
} from "@/features/relay-navigation/domains";
import type { PrimaryDomain } from "@/features/relay-navigation/types";
import { ThemeToggle } from "./ThemeToggle";

// ============================================================
// ActivityRail — persistent primary-domain navigation (VS Code activity bar)
// ============================================================
//
// Renders the three primary domains (Projects, Plans, Runs) as icon `<Link>`
// destinations, each carrying an accessible name and `aria-current="page"` when
// active, plus a persistent Theme_System control entry (Requirements 1.2, 1.3,
// 9.5, 9.6). Exactly one domain is marked active — driven by
// `resolveActiveDomain(pathname)` — and none when the route belongs to no
// primary domain (Requirements 1.4, 1.5).
//
// At >= 1024px (Tailwind `lg`) the slim vertical rail is shown. Below 1024px it
// collapses into a single `Sheet`-backed trigger that reveals the same
// destinations on activation (Requirements 8.2, 8.3); the Theme_System control
// stays visible alongside the trigger so it remains selectable on every route
// (Requirement 1.3).

type DomainIcon = React.ComponentType<{ className?: string; "aria-hidden"?: boolean }>;

const DOMAIN_ICONS: Record<PrimaryDomain, DomainIcon> = {
  projects: FolderGit2,
  plans: ClipboardList,
  runs: Play,
};

export interface ActivityRailProps {
  /**
   * Active primary domain override. When omitted, the active domain is derived
   * from the current router pathname via `resolveActiveDomain`.
   */
  activeDomain?: PrimaryDomain | null;
  className?: string;
}

export function ActivityRail({ activeDomain, className }: ActivityRailProps) {
  const pathname = useRouterState({ select: (state) => state.location.pathname });
  const active =
    activeDomain !== undefined ? activeDomain : resolveActiveDomain(pathname);

  const [mobileOpen, setMobileOpen] = React.useState(false);

  return (
    <>
      {/* Desktop: persistent slim rail (>= 1024px) */}
      <TooltipProvider delayDuration={200}>
        <nav
          aria-label="Primary"
          className={cn(
            "hidden h-full w-14 shrink-0 flex-col items-center gap-1 border-r border-sidebar-border bg-sidebar py-3 lg:flex",
            className,
          )}
        >
          <ul className="flex flex-1 flex-col items-center gap-1">
            {PRIMARY_DOMAINS.map((domain) => {
              const Icon = DOMAIN_ICONS[domain.id];
              const isActive = active === domain.id;
              return (
                <li key={domain.id}>
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <Link
                        to={domain.basePath}
                        aria-label={domain.label}
                        aria-current={isActive ? "page" : undefined}
                        className={cn(
                          "flex size-10 items-center justify-center rounded-md text-sidebar-foreground/70 transition-colors hover:bg-sidebar-accent hover:text-sidebar-foreground focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring",
                          isActive &&
                            "bg-sidebar-primary text-sidebar-primary-foreground hover:bg-sidebar-primary hover:text-sidebar-primary-foreground",
                        )}
                      >
                        <Icon className="size-5" aria-hidden />
                      </Link>
                    </TooltipTrigger>
                    <TooltipContent side="right">{domain.label}</TooltipContent>
                  </Tooltip>
                </li>
              );
            })}
          </ul>

          {/* Persistent Theme_System control entry (Requirement 1.3) */}
          <div className="mt-auto flex flex-col items-center">
            <ThemeToggle />
          </div>
        </nav>
      </TooltipProvider>

      {/* Below 1024px: single collapsed trigger + persistent theme control */}
      <div className="flex items-center gap-1 lg:hidden">
        <Sheet open={mobileOpen} onOpenChange={setMobileOpen}>
          <SheetTrigger asChild>
            <Button
              type="button"
              variant="ghost"
              size="icon"
              aria-label="Open navigation"
            >
              <Menu className="size-5" aria-hidden />
            </Button>
          </SheetTrigger>
          <SheetContent side="left" className="w-64 gap-0">
            <SheetHeader>
              <SheetTitle>Navigation</SheetTitle>
            </SheetHeader>
            <nav aria-label="Primary" className="flex flex-col gap-1 px-2">
              {PRIMARY_DOMAINS.map((domain) => {
                const Icon = DOMAIN_ICONS[domain.id];
                const isActive = active === domain.id;
                return (
                  <SheetClose asChild key={domain.id}>
                    <Link
                      to={domain.basePath}
                      aria-label={domain.label}
                      aria-current={isActive ? "page" : undefined}
                      className={cn(
                        "flex items-center gap-3 rounded-md px-3 py-2 text-sm font-medium text-sidebar-foreground/80 transition-colors hover:bg-sidebar-accent hover:text-sidebar-foreground",
                        isActive &&
                          "bg-sidebar-primary text-sidebar-primary-foreground hover:bg-sidebar-primary hover:text-sidebar-primary-foreground",
                      )}
                    >
                      <Icon className="size-4" aria-hidden />
                      <span>{domain.label}</span>
                    </Link>
                  </SheetClose>
                );
              })}
            </nav>
          </SheetContent>
        </Sheet>

        {/* Theme_System control stays visible below the breakpoint (Requirement 1.3) */}
        <ThemeToggle />
      </div>
    </>
  );
}
