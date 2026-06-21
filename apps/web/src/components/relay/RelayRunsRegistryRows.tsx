import * as React from "react";
import { Link } from "@tanstack/react-router";
import { ChevronRight } from "lucide-react";

import { RelayAttentionBadge } from "@/components/relay/RelayAttentionBadge";
import { RelayMonoText } from "@/components/relay/RelayMeta";
import { RelayStageLabel } from "@/components/relay/RelayStageLabel";
import { StatusBadge } from "@/components/relay/StatusBadge";
import type { RelayAttentionReason } from "@/components/relay/relayVisualState";
import {
  formatRunDate,
  formatRunDateRelative,
  getActiveStepRoute,
  type RelayRun,
} from "@/features/relay-runs";

interface RelayRunRowProps {
  run: RelayRun;
  attentionReason: RelayAttentionReason;
  attentionCountValue?: number;
}

export function RelayRunCompactRow({
  run,
  attentionReason,
  attentionCountValue,
}: RelayRunRowProps) {
  const to = getActiveStepRoute(run);

  return (
    <Link
      to={to}
      aria-label={`Open workbench for ${run.title}`}
      className="rounded border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)] p-3 transition-colors hover:bg-[var(--relay-panel-hover-bg)] focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-[var(--relay-accent)]"
    >
      <div className="flex min-w-0 items-start gap-3">
        <div className="min-w-0 flex-1">
          <p className="truncate text-sm font-medium text-foreground">
            {run.title}
          </p>
          <div className="mt-1 flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1">
            <RelayMonoText className="text-[11px] text-muted-foreground">
              {run.id}
            </RelayMonoText>
            {run.packetId ? (
              <RelayMonoText className="min-w-0 break-words text-[11px] text-muted-foreground">
                {run.packetId}
              </RelayMonoText>
            ) : null}
          </div>
        </div>
        <ChevronRight className="mt-0.5 size-4 shrink-0 text-muted-foreground" />
      </div>

      <div className="mt-3 flex flex-wrap items-center gap-2">
        <StatusBadge status={run.status} />
        <RelayStageLabel step={run.activeStep} />
        <RelayAttentionBadge
          reason={attentionReason}
          compact
          count={attentionCountValue}
        />
      </div>

      <div className="mt-3 grid grid-cols-1 gap-2 sm:grid-cols-3">
        <div className="min-w-0">
          <p className="font-mono text-[10px] uppercase tracking-[0.14em] text-muted-foreground">
            Executor
          </p>
          <RelayMonoText className="mt-1 block break-words text-[11px] text-foreground">
            {run.executor}
          </RelayMonoText>
        </div>
        <div className="min-w-0">
          <p className="font-mono text-[10px] uppercase tracking-[0.14em] text-muted-foreground">
            Updated
          </p>
          <span
            className="mt-1 block text-[11px] text-muted-foreground"
            title={formatRunDate(run.updatedAt)}
          >
            {formatRunDateRelative(run.updatedAt)}
          </span>
        </div>
        <div className="min-w-0">
          <p className="font-mono text-[10px] uppercase tracking-[0.14em] text-muted-foreground">
            Attention
          </p>
          <span className="mt-1 block text-[11px] text-muted-foreground">
            {attentionReason === "none" ? "None" : "Needs review"}
          </span>
        </div>
      </div>
    </Link>
  );
}

interface RelayRunTableRowProps extends RelayRunRowProps {
  columns: string;
  style: React.CSSProperties;
}

export function RelayRunTableRow({
  run,
  attentionReason,
  attentionCountValue,
  columns,
  style,
}: RelayRunTableRowProps) {
  const to = getActiveStepRoute(run);

  return (
    <Link
      to={to}
      aria-label={`Open workbench for ${run.title}`}
      className="absolute left-0 grid w-full items-center border-b border-[var(--relay-row-border)] text-sm transition-colors hover:bg-[var(--relay-panel-hover-bg)] focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-[var(--relay-accent)]"
      style={{
        ...style,
        gridTemplateColumns: columns,
      }}
    >
      <div className="min-w-0 px-4 py-3">
        <div className="min-w-0 space-y-1">
          <p className="truncate font-medium text-foreground">{run.title}</p>
          <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
            <RelayMonoText className="text-[11px] text-muted-foreground">
              {run.id}
            </RelayMonoText>
            {run.packetId ? (
              <>
                <span className="text-[11px] text-muted-foreground">/</span>
                <RelayMonoText className="truncate text-[11px] text-muted-foreground">
                  {run.packetId}
                </RelayMonoText>
              </>
            ) : null}
          </div>
        </div>
      </div>

      <div className="px-4 py-3">
        <StatusBadge status={run.status} />
      </div>

      <div className="px-4 py-3">
        <RelayStageLabel step={run.activeStep} />
      </div>

      <div className="px-4 py-3">
        <RelayMonoText>{run.executor}</RelayMonoText>
      </div>

      <div className="px-4 py-3">
        <span
          className="text-xs text-muted-foreground"
          title={formatRunDate(run.updatedAt)}
        >
          {formatRunDateRelative(run.updatedAt)}
        </span>
      </div>

      <div className="px-4 py-3">
        <RelayAttentionBadge
          reason={attentionReason}
          compact
          count={attentionCountValue}
        />
      </div>

      <div className="flex justify-end px-4 py-3 text-muted-foreground">
        <ChevronRight className="size-4" />
      </div>
    </Link>
  );
}
