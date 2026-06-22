import * as React from "react";
import { Link } from "@tanstack/react-router";
import { ChevronRight } from "lucide-react";

import { RelayAttentionBadge } from "@/components/relay/RelayAttentionBadge";
import { RelayStageLabel } from "@/components/relay/RelayStageLabel";
import { StatusBadge } from "@/components/relay/StatusBadge";
import type { RelayAttentionReason } from "@/components/relay/relayVisualState";
import { cn } from "@/lib/utils";
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
  const hasAttention = attentionReason !== "none";

  return (
    <Link
      to={to}
      aria-label={`Open workbench for ${run.title}`}
      className="rounded border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)] p-3 transition-colors hover:bg-[var(--relay-row-hover-bg)] focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-[var(--relay-accent)] group"
    >
      <div className="flex min-w-0 items-start gap-3">
        <div className="min-w-0 flex-1">
          <p
            className={cn(
              "truncate text-sm font-medium leading-snug",
              hasAttention ? "text-foreground" : "text-muted-foreground"
            )}
          >
            {run.title}
          </p>
          <div className="mt-0.5 flex items-center flex-wrap gap-1 font-mono text-[10px] text-muted-foreground/30">
            <span>{run.id}</span>
            {run.packetId ? (
              <>
                <span className="text-muted-foreground/20">/</span>
                <span>{run.packetId}</span>
              </>
            ) : null}
            {run.repo ? (
              <>
                <span className="text-muted-foreground/20">/</span>
                <span>{run.repo}</span>
              </>
            ) : null}
            {run.branch ? (
              <>
                <span className="text-muted-foreground/20">/</span>
                <span>{run.branch}</span>
              </>
            ) : null}
          </div>
        </div>
        <ChevronRight className="mt-0.5 size-4 shrink-0 text-muted-foreground/30 group-hover:text-muted-foreground transition-colors" />
      </div>

      <div className="mt-3 flex flex-wrap items-center gap-2">
        <StatusBadge status={run.status} />
        <RelayStageLabel
          step={run.activeStep}
          className={cn(
            "font-mono text-[10px] tracking-widest",
            hasAttention ? "text-muted-foreground" : "text-muted-foreground/60"
          )}
        />
        <RelayAttentionBadge
          reason={attentionReason}
          compact
          count={attentionCountValue}
        />
      </div>

      <div className="mt-3 grid grid-cols-1 gap-2 sm:grid-cols-3">
        <div className="min-w-0">
          <p className="text-[10px] font-medium uppercase tracking-[0.06em] text-muted-foreground">
            Executor
          </p>
          <span className="mt-1 block font-mono text-[11px] text-muted-foreground/80 break-words">
            {run.executor}
          </span>
        </div>
        <div className="min-w-0">
          <p className="text-[10px] font-medium uppercase tracking-[0.06em] text-muted-foreground">
            Updated
          </p>
          <span
            className="mt-1 block text-[11px] text-muted-foreground/60 whitespace-nowrap"
            title={formatRunDate(run.updatedAt)}
          >
            {formatRunDateRelative(run.updatedAt)}
          </span>
        </div>
        <div className="min-w-0">
          <p className="text-[10px] font-medium uppercase tracking-[0.06em] text-muted-foreground">
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
  const hasAttention = attentionReason !== "none";

  return (
    <Link
      to={to}
      aria-label={`Open workbench for ${run.title}`}
      className="absolute left-0 grid w-full items-center border-b border-[var(--relay-row-border)] text-sm transition-colors hover:bg-[var(--relay-row-hover-bg)] focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-[var(--relay-accent)] group"
      style={{
        ...style,
        gridTemplateColumns: columns,
      }}
    >
      {/* Run title + compact meta */}
      <div className="min-w-0 px-6 py-3.5 pr-3">
        <div className="min-w-0">
          <p
            className={cn(
              "truncate font-medium text-sm leading-snug",
              hasAttention ? "text-foreground" : "text-muted-foreground"
            )}
          >
            {run.title}
          </p>
          <div className="mt-0.5 flex items-center gap-1 font-mono text-[10px] text-muted-foreground/30">
            <span>{run.id}</span>
            {run.packetId ? (
              <>
                <span className="text-muted-foreground/20">/</span>
                <span>{run.packetId}</span>
              </>
            ) : null}
            {run.repo ? (
              <>
                <span className="text-muted-foreground/20">/</span>
                <span>{run.repo}</span>
              </>
            ) : null}
            {run.branch ? (
              <>
                <span className="text-muted-foreground/20">/</span>
                <span>{run.branch}</span>
              </>
            ) : null}
          </div>
        </div>
      </div>

      {/* Status */}
      <div className="px-4 py-3.5">
        <StatusBadge status={run.status} />
      </div>

      {/* Stage */}
      <div className="px-4 py-3.5">
        <RelayStageLabel
          step={run.activeStep}
          className={cn(
            "font-mono text-[10px] tracking-widest",
            hasAttention ? "text-muted-foreground" : "text-muted-foreground/60"
          )}
        />
      </div>

      {/* Executor */}
      <div className="px-4 py-3.5">
        <span className="font-mono text-[11px] text-muted-foreground/80">{run.executor}</span>
      </div>

      {/* Updated */}
      <div className="px-4 py-3.5">
        <span
          className="text-[11px] text-muted-foreground/60 whitespace-nowrap"
          title={formatRunDate(run.updatedAt)}
        >
          {formatRunDateRelative(run.updatedAt)}
        </span>
      </div>

      {/* Attention */}
      <div className="px-4 py-3.5">
        <RelayAttentionBadge
          reason={attentionReason}
          compact
          count={attentionCountValue}
        />
      </div>

      {/* Open chevron */}
      <div className="px-3 py-3.5 text-right">
        <ChevronRight
          size={13}
          className="text-muted-foreground/30 group-hover:text-muted-foreground transition-colors inline-block"
        />
      </div>
    </Link>
  );
}
