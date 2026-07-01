// ============================================================
// Relay Shell — CommandPalette overlay (Ctrl/⌘K)
// ============================================================
//
// Keyboard-summoned overlay unifying primary-domain navigation, per-entity
// recents, and the CLOSED action set (New Run + New Plan). Built on the shadcn
// command (`cmdk`) primitive layered over a Radix dialog (`CommandDialog`),
// which provides the focus trap, Escape-to-close, and focus-restore behavior
// this feature relies on.
//
// The palette is a CONTROLLED overlay: the AppShell owns the Ctrl/⌘K handler
// and the `open` state (task 12.1) and passes it in via props.
//
// Requirements:
//   - 4.2  Provide nav entries for each primary domain plus the 5 most recently
//          updated Runs, Plans, and Projects.
//   - 4.3  Provide exactly the closed action set New Run and New Plan.
//   - 4.4  Filter to entries whose visible label contains the query as a
//          case-insensitive substring — driven by `filterCommandEntries` with
//          cmdk's built-in fuzzy filter disabled (`shouldFilter={false}`) so
//          the behavior exactly matches the property-tested selector.
//   - 4.5  On no matches, show a no-results state and keep the palette open.
//   - 4.6  On selection, execute the entry's navigation/action and close.
//   - 4.7  Escape closes and restores focus to the previously focused element
//          (Radix Dialog focus restore) — see also Req 9.4.
//   - 4.10 The palette exposes NO lifecycle-mutating Run action; the entry set
//          comes from `buildCommandEntries`, whose action variant is frozen to
//          New Run / New Plan.
//   - 9.3  Focus is moved into the overlay and trapped until it closes (Radix
//          Dialog focus scope).
//   - 9.4  Escape closes the overlay and returns focus to the opener.

import * as React from "react";
import { useNavigate, type NavigateOptions } from "@tanstack/react-router";
import { ClipboardList, FolderGit2, Play, Plus } from "lucide-react";

import { cn } from "@/lib/utils";
import {
  CommandDialog,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
  CommandSeparator,
} from "@/components/ui/command";
import {
  buildCommandEntries,
  filterCommandEntries,
} from "@/features/relay-navigation/command";
import type { CommandEntry, PrimaryDomain } from "@/features/relay-navigation/types";
import { useShellData } from "@/features/relay-navigation/useShellData";

// ------------------------------------------------------------
// Pure helpers (unit-tested independently of rendering)
// ------------------------------------------------------------

/**
 * Resolve the navigation target for a navigable Command_Palette entry (a
 * primary-domain entry or a recent-entity entry). Action entries carry no
 * navigation target and return `null` (their `run` callback is invoked
 * instead). The returned object is shaped for TanStack Router's `navigate`.
 */
export function resolveCommandNavigation(entry: CommandEntry): NavigateOptions | null {
  switch (entry.kind) {
    case "nav-domain":
      return { to: entry.to } as NavigateOptions;
    case "nav-recent":
      return { to: entry.to, params: entry.params } as NavigateOptions;
    case "action":
      return null;
  }
}

/** A stable, collision-free React key for a command entry. */
export function commandEntryKey(entry: CommandEntry): string {
  switch (entry.kind) {
    case "nav-domain":
      return `nav-domain:${entry.id}`;
    case "nav-recent":
      return `nav-recent:${entry.entity}:${entry.id}`;
    case "action":
      return `action:${entry.id}`;
  }
}

// The cmdk `value` for each item must be unique so keyboard selection maps back
// to exactly one entry. We reuse the stable entry key.
const RECENT_GROUP_LABEL: Record<"run" | "plan" | "project", string> = {
  run: "Recent Runs",
  plan: "Recent Plans",
  project: "Recent Projects",
};

const RECENT_GROUP_ORDER: Array<"run" | "plan" | "project"> = ["run", "plan", "project"];

const DOMAIN_ICON: Record<PrimaryDomain, React.ComponentType<{ className?: string }>> = {
  projects: FolderGit2,
  plans: ClipboardList,
  runs: Play,
};

// ------------------------------------------------------------
// Component
// ------------------------------------------------------------

export interface CommandPaletteProps {
  /** Controlled open state (owned by AppShell, task 12.1). */
  open: boolean;
  /** Controlled open-state setter (also used for execute-and-close). */
  onOpenChange: (open: boolean) => void;
  className?: string;
}

export function CommandPalette({ open, onOpenChange, className }: CommandPaletteProps) {
  const navigate = useNavigate();
  const { recents } = useShellData();
  const [query, setQuery] = React.useState("");

  // The action set is CLOSED to exactly New Run and New Plan (Req 4.3, 4.10).
  // Both navigate to their existing "new" routes; neither mutates run lifecycle.
  const handlers = React.useMemo(
    () => ({
      onNewRun: () => {
        void navigate({ to: "/runs/new", search: { planId: undefined, passId: undefined } });
      },
      onNewPlan: () => {
        void navigate({ to: "/plans/new" });
      },
    }),
    [navigate],
  );

  const entries = React.useMemo(
    () => buildCommandEntries(recents, handlers),
    [recents, handlers],
  );

  // Filter through the property-tested selector (case-insensitive substring on
  // the visible label) rather than cmdk's fuzzy filter (Req 4.4).
  const filtered = React.useMemo(
    () => filterCommandEntries(query, entries),
    [query, entries],
  );

  // Group the filtered entries for rendering while preserving registry order.
  const domainEntries = filtered.filter((entry) => entry.kind === "nav-domain");
  const actionEntries = filtered.filter((entry) => entry.kind === "action");
  const recentsByEntity = React.useMemo(() => {
    const map: Record<"run" | "plan" | "project", CommandEntry[]> = {
      run: [],
      plan: [],
      project: [],
    };
    for (const entry of filtered) {
      if (entry.kind === "nav-recent") {
        map[entry.entity].push(entry);
      }
    }
    return map;
  }, [filtered]);

  const hasResults = filtered.length > 0;

  // Reset the query whenever the palette open state changes so a fresh open
  // always starts empty and a close does not leak a stale query.
  const handleOpenChange = React.useCallback(
    (next: boolean) => {
      if (!next) {
        setQuery("");
      }
      onOpenChange(next);
    },
    [onOpenChange],
  );

  // Execute the entry's navigation/action, then close the palette (Req 4.6).
  const handleSelect = React.useCallback(
    (entry: CommandEntry) => {
      if (entry.kind === "action") {
        entry.run();
      } else {
        const target = resolveCommandNavigation(entry);
        if (target) {
          void navigate(target);
        }
      }
      setQuery("");
      onOpenChange(false);
    },
    [navigate, onOpenChange],
  );

  return (
    <CommandDialog
      open={open}
      onOpenChange={handleOpenChange}
      title="Command Palette"
      description="Navigate to a domain, jump to a recent item, or start a new run or plan."
      className={cn(className)}
      // Disable cmdk's built-in fuzzy filter — we filter through
      // `filterCommandEntries` to match Req 4.4 exactly (Req 4.4).
      commandProps={{ shouldFilter: false }}
    >
      <CommandInput
        placeholder="Search navigation, recents, and actions..."
        value={query}
        onValueChange={setQuery}
      />
      <CommandList>
        {/* No-results state keeps the palette open (Req 4.5). */}
        {!hasResults && <CommandEmpty>No results found.</CommandEmpty>}

        {domainEntries.length > 0 && (
          <CommandGroup heading="Navigation">
            {domainEntries.map((entry) => {
              const Icon = entry.kind === "nav-domain" ? DOMAIN_ICON[entry.id] : undefined;
              return (
                <CommandItem
                  key={commandEntryKey(entry)}
                  value={commandEntryKey(entry)}
                  onSelect={() => handleSelect(entry)}
                >
                  {Icon ? <Icon className="size-4" /> : null}
                  <span>{entry.label}</span>
                </CommandItem>
              );
            })}
          </CommandGroup>
        )}

        {RECENT_GROUP_ORDER.map((entity) => {
          const group = recentsByEntity[entity];
          if (group.length === 0) return null;
          return (
            <React.Fragment key={`recent-group:${entity}`}>
              <CommandSeparator />
              <CommandGroup heading={RECENT_GROUP_LABEL[entity]}>
                {group.map((entry) => (
                  <CommandItem
                    key={commandEntryKey(entry)}
                    value={commandEntryKey(entry)}
                    onSelect={() => handleSelect(entry)}
                  >
                    <span className="truncate">{entry.label}</span>
                  </CommandItem>
                ))}
              </CommandGroup>
            </React.Fragment>
          );
        })}

        {actionEntries.length > 0 && (
          <>
            <CommandSeparator />
            <CommandGroup heading="Actions">
              {actionEntries.map((entry) => (
                <CommandItem
                  key={commandEntryKey(entry)}
                  value={commandEntryKey(entry)}
                  onSelect={() => handleSelect(entry)}
                >
                  <Plus className="size-4" />
                  <span>{entry.label}</span>
                </CommandItem>
              ))}
            </CommandGroup>
          </>
        )}
      </CommandList>
    </CommandDialog>
  );
}
