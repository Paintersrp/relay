import * as React from "react";
import { Link } from "@tanstack/react-router";
import { Copy, ExternalLink } from "lucide-react";

import type { RelayRunPlanContext } from "@/features/relay-runs";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";

type RunPlanContextHrefs = {
  planTo?: "/plans/$planId";
  passTo?: "/plans/$planId/passes/$passId";
  planParams?: { planId: string };
  passParams?: { planId: string; passId: string };
};

type CopyState = "idle" | "copied" | "failed";

function present(value?: string | null): value is string {
  return typeof value === "string" && value.trim().length > 0;
}

function formatStatus(status: string): string {
  return status
    .replace(/[_-]+/g, " ")
    .replace(/\b\w/g, (letter) => letter.toUpperCase());
}

function getPlanLabel(context: RelayRunPlanContext): string {
  return context.planTitle || context.planId || "Plan";
}

function getPassLabel(context: RelayRunPlanContext): string {
  if (context.passName) {
    return context.passName;
  }

  if (typeof context.passSequence === "number") {
    return `Pass ${context.passSequence}`;
  }

  return context.passId || "Pass";
}

async function copyText(value: string, onStateChange: (state: CopyState) => void) {
  try {
    if (!navigator.clipboard?.writeText) {
      throw new Error("Clipboard API unavailable");
    }

    await navigator.clipboard.writeText(value);
    onStateChange("copied");
  } catch {
    onStateChange("failed");
  }
}

function CopyIdButton({ label, value }: { label: string; value?: string }) {
  const [copyState, setCopyState] = React.useState<CopyState>("idle");

  if (!value) {
    return null;
  }

  return (
    <Button
      type="button"
      variant="ghost"
      size="sm"
      className="h-6 gap-1 rounded-sm px-1.5 text-[10px] text-muted-foreground"
      onClick={() => copyText(value, setCopyState)}
      title={`Copy ${label}`}
    >
      <Copy className="size-3" />
      <span className="sr-only">Copy {label}</span>
      {copyState !== "idle" ? (
        <span aria-live="polite">
          {copyState === "copied" ? "Copied" : "Copy failed"}
        </span>
      ) : null}
    </Button>
  );
}

export function hasRunPlanContext(
  context?: RelayRunPlanContext | null,
): boolean {
  return Boolean(context && (present(context.planId) || present(context.passId)));
}

export function getRunPlanContextHrefs(
  context?: RelayRunPlanContext | null,
): RunPlanContextHrefs {
  if (!context?.planId) {
    return {};
  }

  const hrefs: RunPlanContextHrefs = {
    planTo: "/plans/$planId",
    planParams: { planId: context.planId },
  };

  if (context.passId) {
    hrefs.passTo = "/plans/$planId/passes/$passId";
    hrefs.passParams = { planId: context.planId, passId: context.passId };
  }

  return hrefs;
}

export function RunPlanContextStatusPill({
  status,
}: {
  status?: string;
}): React.JSX.Element | null {
  if (!present(status)) {
    return null;
  }

  return (
    <Badge variant="outline" className="h-auto rounded-sm px-1.5 py-0 text-[10px]">
      {formatStatus(status)}
    </Badge>
  );
}

export function RunPlanContextHeader({
  context,
}: {
  context?: RelayRunPlanContext | null;
}): React.JSX.Element | null {
  if (!hasRunPlanContext(context)) {
    return null;
  }

  const safeContext = context!;
  const hrefs = getRunPlanContextHrefs(safeContext);

  return (
    <div className="mt-2 flex min-w-0 flex-wrap items-center gap-2 text-xs">
      <span className="text-[10px] font-semibold uppercase tracking-normal text-muted-foreground">
        Plan Context
      </span>
      {hrefs.planTo && hrefs.planParams ? (
        <Button variant="ghost" size="sm" asChild className="h-6 rounded-sm px-1.5">
          <Link
            to={hrefs.planTo}
            params={hrefs.planParams}
            className="max-w-56 truncate font-medium text-[var(--relay-accent)]"
          >
            {getPlanLabel(safeContext)}
            <ExternalLink className="size-3" />
          </Link>
        </Button>
      ) : (
        <span className="max-w-56 truncate font-mono text-muted-foreground">
          {safeContext.planId}
        </span>
      )}
      {!hrefs.planTo ? (
        <CopyIdButton label="plan ID" value={safeContext.planId} />
      ) : null}
      {safeContext.passId ? (
        <>
          <span className="text-muted-foreground/40">/</span>
          {hrefs.passTo && hrefs.passParams ? (
            <Button variant="ghost" size="sm" asChild className="h-6 rounded-sm px-1.5">
              <Link
                to={hrefs.passTo}
                params={hrefs.passParams}
                className="max-w-56 truncate font-medium text-[var(--relay-accent)]"
              >
                {getPassLabel(safeContext)}
                <ExternalLink className="size-3" />
              </Link>
            </Button>
          ) : (
            <span className="max-w-56 truncate font-mono text-muted-foreground">
              {getPassLabel(safeContext)}
            </span>
          )}
          {!hrefs.passTo ? (
            <CopyIdButton label="pass ID" value={safeContext.passId} />
          ) : null}
          <RunPlanContextStatusPill status={safeContext.passStatus} />
        </>
      ) : null}
    </div>
  );
}

function ContextRow({
  label,
  value,
  mono,
  children,
}: {
  label: string;
  value?: React.ReactNode;
  mono?: boolean;
  children?: React.ReactNode;
}) {
  if (!value && !children) {
    return null;
  }

  return (
    <div className="grid min-w-0 grid-cols-[4.5rem_minmax(0,1fr)] gap-2 py-1">
      <dt className="text-[10px] font-medium uppercase tracking-normal text-muted-foreground">
        {label}
      </dt>
      <dd
        className={cn(
          "min-w-0 text-xs text-foreground",
          mono ? "font-mono" : "font-medium",
        )}
      >
        {value}
        {children}
      </dd>
    </div>
  );
}

export function RunPlanContextCard({
  context,
  title = "Plan Context",
  description = "Managed plan/pass association returned for this run.",
}: {
  context?: RelayRunPlanContext | null;
  title?: string;
  description?: string;
}): React.JSX.Element | null {
  if (!hasRunPlanContext(context)) {
    return null;
  }

  const safeContext = context!;
  const hrefs = getRunPlanContextHrefs(safeContext);

  return (
    <section className="rounded-sm border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)]">
      <div className="border-b border-[var(--relay-row-border)] bg-[var(--relay-content-bg)] px-3 py-2">
        <div className="flex min-w-0 items-start justify-between gap-2">
          <div className="min-w-0">
            <h3 className="text-xs font-semibold text-foreground">{title}</h3>
            {description ? (
              <p className="mt-1 text-xs text-muted-foreground">{description}</p>
            ) : null}
          </div>
          <RunPlanContextStatusPill status={safeContext.passStatus} />
        </div>
      </div>

      <dl className="px-3 py-2">
        <ContextRow label="Plan" value={getPlanLabel(safeContext)}>
          {hrefs.planTo && hrefs.planParams ? (
            <Button
              variant="ghost"
              size="sm"
              asChild
              className="ml-1 h-6 rounded-sm px-1.5 text-[var(--relay-accent)]"
            >
              <Link to={hrefs.planTo} params={hrefs.planParams}>
                <ExternalLink className="size-3" />
                <span className="sr-only">Open plan</span>
              </Link>
            </Button>
          ) : (
            <CopyIdButton label="plan ID" value={safeContext.planId} />
          )}
        </ContextRow>
        {safeContext.planId ? (
          <ContextRow label="Plan ID" value={safeContext.planId} mono />
        ) : null}
        {safeContext.passId ? (
          <>
            <ContextRow label="Pass" value={getPassLabel(safeContext)}>
              {hrefs.passTo && hrefs.passParams ? (
                <Button
                  variant="ghost"
                  size="sm"
                  asChild
                  className="ml-1 h-6 rounded-sm px-1.5 text-[var(--relay-accent)]"
                >
                  <Link to={hrefs.passTo} params={hrefs.passParams}>
                    <ExternalLink className="size-3" />
                    <span className="sr-only">Open pass</span>
                  </Link>
                </Button>
              ) : (
                <CopyIdButton label="pass ID" value={safeContext.passId} />
              )}
            </ContextRow>
            <ContextRow label="Pass ID" value={safeContext.passId} mono />
          </>
        ) : null}
      </dl>
    </section>
  );
}
