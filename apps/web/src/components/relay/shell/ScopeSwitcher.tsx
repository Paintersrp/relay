// ============================================================
// Relay Navigation — ScopeSwitcher (Top_Bar scope control)
// ============================================================
//
// Displays the active scope label and, on activation, presents the Projects and
// Plans exposed by the API_Contract for selection; selecting a scope navigates
// to it.
//
// Requirements:
//   - 2.4  On activation, present Projects and Plans for selection.
//   - 2.5  On selection, navigate to the selected scope.
//   - 8.4  Below 1024px, remain a keyboard-operable control that displays the
//          active scope label. The label is never hidden at narrow widths.
//
// The control is built on the shadcn `Select` primitive (Radix Select), which
// is keyboard-operable by default (open/close, arrow navigation, type-ahead,
// Enter to select, Escape to dismiss) and satisfies Req 9.1 for this control.

import * as React from "react";
import { useNavigate, useParams } from "@tanstack/react-router";
import { Layers } from "lucide-react";

import { cn } from "@/lib/utils";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectLabel,
  SelectTrigger,
} from "@/components/ui/select";
import { useShellData } from "@/features/relay-navigation/useShellData";
import type { ScopeOption } from "@/features/relay-navigation/types";

const DEFAULT_SCOPE_LABEL = "Select scope";

/**
 * Encode a stable `Select` item value for a scope option. The `kind` prefix
 * keeps Project and Plan identifiers from colliding in the value space.
 */
export function scopeOptionValue(option: Pick<ScopeOption, "kind" | "id">): string {
  return `${option.kind}:${option.id}`;
}

/** The scope currently active according to the route params. */
export interface ActiveScope {
  value: string;
  label: string;
}

/**
 * Resolve the active scope from the current route params and the available
 * scope options. A route carrying a `projectId` resolves to that Project; a
 * route carrying a `planId` (including pass routes) resolves to that Plan.
 *
 * The label prefers the resolved option's label; when the option list has not
 * yet loaded it falls back to the raw identifier (never a fabricated
 * placeholder). Returns `null` when no scope is active for the current route.
 */
export function resolveActiveScope(
  params: { projectId?: string; planId?: string },
  scopeOptions: ScopeOption[],
): ActiveScope | null {
  const active: { kind: ScopeOption["kind"]; id: string } | null = params.projectId
    ? { kind: "project", id: params.projectId }
    : params.planId
      ? { kind: "plan", id: params.planId }
      : null;

  if (!active) return null;

  const matched = scopeOptions.find(
    (option) => option.kind === active.kind && option.id === active.id,
  );

  return {
    value: scopeOptionValue(active),
    label: matched?.label ?? active.id,
  };
}

export interface ScopeSwitcherProps {
  className?: string;
}

export function ScopeSwitcher({ className }: ScopeSwitcherProps) {
  const navigate = useNavigate();
  // Shell-level control: read whatever hierarchy params the current route match
  // exposes without being bound to a single route (`strict: false`).
  const params = useParams({ strict: false }) as {
    projectId?: string;
    planId?: string;
  };
  const { scopeOptions } = useShellData();

  const projectOptions = React.useMemo(
    () => scopeOptions.filter((option) => option.kind === "project"),
    [scopeOptions],
  );
  const planOptions = React.useMemo(
    () => scopeOptions.filter((option) => option.kind === "plan"),
    [scopeOptions],
  );

  const optionByValue = React.useMemo(() => {
    const map = new Map<string, ScopeOption>();
    for (const option of scopeOptions) {
      map.set(scopeOptionValue(option), option);
    }
    return map;
  }, [scopeOptions]);

  const activeScope = React.useMemo(
    () => resolveActiveScope(params, scopeOptions),
    [params, scopeOptions],
  );

  const activeLabel = activeScope?.label ?? DEFAULT_SCOPE_LABEL;
  const hasOptions = scopeOptions.length > 0;

  const handleValueChange = React.useCallback(
    (value: string) => {
      const option = optionByValue.get(value);
      if (!option) return;

      // Narrow on `kind` so the navigation target stays type-checked against
      // the existing route inventory (Req 2.5).
      if (option.kind === "project") {
        void navigate({
          to: "/projects/$projectId",
          params: { projectId: option.id },
        });
      } else {
        void navigate({
          to: "/plans/$planId",
          params: { planId: option.id },
        });
      }
    },
    [navigate, optionByValue],
  );

  return (
    <Select
      value={activeScope?.value ?? undefined}
      onValueChange={handleValueChange}
      disabled={!hasOptions}
    >
      <SelectTrigger
        aria-label="Active scope"
        // The label region stays visible at every width (Req 8.4): it is never
        // hidden with responsive utilities. The trigger only constrains its max
        // width on narrow viewports and truncates instead of hiding the label.
        className={cn(
          "max-w-[9rem] gap-2 sm:max-w-[16rem] lg:max-w-[20rem]",
          className,
        )}
      >
        <span className="flex min-w-0 items-center gap-1.5">
          <Layers className="size-4 shrink-0 text-muted-foreground" aria-hidden="true" />
          <span className="truncate">{activeLabel}</span>
        </span>
      </SelectTrigger>
      <SelectContent>
        {projectOptions.length > 0 && (
          <SelectGroup>
            <SelectLabel>Projects</SelectLabel>
            {projectOptions.map((option) => (
              <SelectItem key={scopeOptionValue(option)} value={scopeOptionValue(option)}>
                {option.label}
              </SelectItem>
            ))}
          </SelectGroup>
        )}
        {planOptions.length > 0 && (
          <SelectGroup>
            <SelectLabel>Plans</SelectLabel>
            {planOptions.map((option) => (
              <SelectItem key={scopeOptionValue(option)} value={scopeOptionValue(option)}>
                {option.label}
              </SelectItem>
            ))}
          </SelectGroup>
        )}
      </SelectContent>
    </Select>
  );
}
